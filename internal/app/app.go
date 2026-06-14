package app

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ssh-roundrobin/internal/config"
	"ssh-roundrobin/internal/daemon"
	"ssh-roundrobin/internal/proxy"
	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

// App holds all initialized components for the running proxy.
type App struct {
	cfg     *config.Config
	rr      *sshroundrobin.RoundRobin
	tracker *status.ServerStatusTracker
	listener net.Listener
}

// New creates a new App from config. Does not connect or listen yet.
func New(cfg *config.Config) *App {
	return &App{cfg: cfg}
}

// Run initializes everything and runs the accept loop.
// This blocks until the process exits.
func (a *App) Run() {
	a.setupLogging()
	a.writePID()
	a.connectServers()
	a.startListener()
	a.startHealthCheck()
	a.handleSignals()
	a.acceptLoop()
}

func (a *App) setupLogging() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if a.cfg.LogFile != "" {
		f, err := os.OpenFile(a.cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file %s: %v\n", a.cfg.LogFile, err)
			os.Exit(1)
		}
		log.SetOutput(f)
	} else {
		log.SetOutput(os.Stderr)
	}

	log.Println("Starting SSH Round-Robin Proxy...")
	log.Printf("Config loaded - Bind: %s, Servers: %s, Strategy: %s, MaxActiveUpstreams: %d, TargetRetryUpstreams: %d, TargetFailThreshold: %d, TargetFailTTL: %s, Cloudflared force: %t, ProxyCommand set: %t, UpstreamStats: %t", a.cfg.BindAddr, a.cfg.ServersFile, a.cfg.Strategy, a.cfg.MaxActiveUpstreams, a.cfg.TargetRetryUpstreams, a.cfg.TargetFailThreshold, a.cfg.TargetFailTTL, a.cfg.Cloudflared, a.cfg.ProxyCommand != "", a.cfg.ShowUpstreamStats)
	if a.cfg.Mode == "socks5" && (a.cfg.TargetHost != "127.0.0.1" || a.cfg.TargetPort != 80) {
		log.Printf("Ignoring target %s:%d because MODE=socks5 uses client-requested destinations", a.cfg.TargetHost, a.cfg.TargetPort)
	}
}

func (a *App) writePID() {
	if err := daemon.WritePID(a.cfg.PIDFile); err != nil {
		log.Printf("Warning: failed to write PID file: %v", err)
	}
}

func (a *App) connectServers() {
	servers, err := config.ParseServersFile(a.cfg.ServersFile, a.cfg.Username, a.cfg.KeyFile, a.cfg.ProxyCommand, a.cfg.Cloudflared)
	if err != nil {
		log.Fatalf("Failed to parse servers: %v", err)
	}
	if len(servers) == 0 {
		log.Fatal("No servers configured")
	}

	a.rr = sshroundrobin.NewRoundRobin(a.cfg.Strategy, a.cfg.MaxActiveUpstreams)
	a.rr.OnConnectionError = func(addr string, err error) {
		if a.tracker != nil {
			a.tracker.RecordFail(addr, err)
		}
	}

	a.tracker = status.NewServerStatusTracker(a.cfg.StatusFile, a.cfg.StatusLog, time.Duration(a.cfg.StatusFlushSec)*time.Second)
	if err := a.tracker.Load(); err != nil {
		log.Printf("Warning: failed to load status file: %v", err)
	}
	a.tracker.StartPeriodicFlush()

	eagerLimit := a.cfg.MaxActiveUpstreams
	if a.cfg.Strategy == sshroundrobin.StrategyFailover {
		eagerLimit = 1
	}
	connected := 0

	for _, server := range servers {
		if connected < eagerLimit {
			client, err := sshroundrobin.NewSSHClient(server)
			if err == nil {
				a.rr.Add(client)
				connected++
				log.Printf("Connected to %s (mode=%s)", server.Addr(), server.AuthMethod.String())
				continue
			}
			log.Printf("Startup connect failed for %s (mode=%s), queued as lazy upstream: %v", server.Addr(), server.AuthMethod.String(), err)
			if a.tracker != nil {
				a.tracker.RecordFail(server.Addr(), err)
			}
		}
		a.rr.Add(sshroundrobin.NewSSHClientLazy(server))
		log.Printf("Registered lazy upstream %s (mode=%s)", server.Addr(), server.AuthMethod.String())
	}

	if a.rr.Len() == 0 {
		log.Fatal("No servers available")
	}
}

func (a *App) startListener() {
	listener, err := net.Listen("tcp", a.cfg.BindAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", a.cfg.BindAddr, err)
	}
	a.listener = listener

	if a.cfg.Mode == "socks5" {
		log.Printf("SSH Round-Robin SOCKS5 listening on %s", a.cfg.BindAddr)
	} else {
		log.Printf("SSH Round-Robin TCP forward listening on %s -> %s:%d", a.cfg.BindAddr, a.cfg.TargetHost, a.cfg.TargetPort)
	}
}

func (a *App) startHealthCheck() {
	if !a.cfg.HealthCheck {
		return
	}

	go func() {
		ticker := time.NewTicker(a.cfg.HealthInterval)
		defer ticker.Stop()

		for range ticker.C {
			report := a.rr.RunHealthChecks()
			if report.Checked == 0 {
				continue
			}
			if report.SwitchedFrom != "" || report.SwitchedTo != "" {
				log.Printf("Health check switch: from=%s to=%s", report.SwitchedFrom, report.SwitchedTo)
			}
			if len(report.Failed) > 0 {
				log.Printf("Health check failed upstreams: %v", report.Failed)
			}
			if len(report.Recovered) > 0 {
				log.Printf("Health check recovered upstreams: %v", report.Recovered)
			}
			if a.cfg.ShowUpstreamStats {
				log.Printf("Upstream stats: %s", a.rr.StatsSummary())
			}
		}
	}()
}

func (a *App) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		if a.cfg.ShowUpstreamStats {
			log.Printf("Final upstream stats: %s", a.rr.StatsSummary())
		}
		if err := a.tracker.Flush(); err != nil {
			log.Printf("Warning: failed to flush status file: %v", err)
		}
		a.tracker.Stop()
		a.rr.CloseAll()
		os.Exit(0)
	}()
}

func (a *App) acceptLoop() {
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		if a.cfg.Mode == "socks5" {
			go proxy.HandleSocks5Connection(conn, a.rr, a.cfg.TargetRetryUpstreams, a.cfg.TargetFailThreshold, a.cfg.TargetFailTTL, a.tracker)
		} else {
			go proxy.HandleConnection(conn, a.rr, a.cfg.TargetHost, a.cfg.TargetPort, a.cfg.TargetRetryUpstreams, a.cfg.TargetFailThreshold, a.cfg.TargetFailTTL, a.tracker)
		}
	}
}
