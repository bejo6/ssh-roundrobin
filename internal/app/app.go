package app

import (
	"fmt"
	"log"
	"net"
	"os"

	"ssh-roundrobin/internal/config"
	"ssh-roundrobin/internal/daemon"
	"ssh-roundrobin/internal/proxy"
	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

// App holds all initialized components for the running proxy.
type App struct {
	cfg        *config.Config
	rr         *sshroundrobin.RoundRobin
	tracker    *status.ServerStatusTracker
	listener   net.Listener
	connSem    chan struct{}
	shutdownCh chan struct{}
	healthStop chan struct{}
	logFile    *os.File
}

// New creates a new App from config. Does not connect or listen yet.
func New(cfg *config.Config) *App {
	maxConns := cfg.MaxConnections
	if maxConns <= 0 {
		maxConns = 100
	}
	return &App{
		cfg:        cfg,
		connSem:    make(chan struct{}, maxConns),
		shutdownCh: make(chan struct{}),
		healthStop: make(chan struct{}),
	}
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
		a.logFile = f
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

func (a *App) acceptLoop() {
	for {
		select {
		case <-a.shutdownCh:
			return
		default:
		}
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.shutdownCh:
				return
			default:
			}
			log.Printf("Accept error: %v", err)
			continue
		}
		select {
		case a.connSem <- struct{}{}:
			go func() {
				defer func() { <-a.connSem }()
				if a.cfg.Mode == "socks5" {
					proxy.HandleSocks5Connection(conn, a.rr, a.cfg.TargetRetryUpstreams, a.cfg.TargetFailThreshold, a.cfg.TargetFailTTL, a.tracker)
				} else {
					proxy.HandleConnection(conn, a.rr, a.cfg.TargetHost, a.cfg.TargetPort, a.cfg.TargetRetryUpstreams, a.cfg.TargetFailThreshold, a.cfg.TargetFailTTL, a.tracker)
				}
			}()
		default:
			conn.Close()
			log.Printf("Connection rejected: max connections (%d) reached", cap(a.connSem))
		}
	}
}
