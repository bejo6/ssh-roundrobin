package app

import (
	"log"
	"time"

	"ssh-roundrobin/internal/config"
	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

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
