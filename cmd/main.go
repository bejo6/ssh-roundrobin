package main

import (
	"fmt"
	"os"

	"ssh-roundrobin/internal/app"
	"ssh-roundrobin/internal/config"
	"ssh-roundrobin/internal/daemon"
)

func main() {
	cfg := config.ParseConfig()

	if cfg.StopDaemon {
		if err := daemon.Stop(cfg.PIDFile); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stop daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Daemon stopped")
		os.Exit(0)
	}
	if cfg.StatusDaemon {
		running, pid, err := daemon.Status(cfg.PIDFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to check status: %v\n", err)
			os.Exit(1)
		}
		if running {
			fmt.Printf("Daemon running (PID %d)\n", pid)
		} else {
			fmt.Println("Daemon not running")
		}
		os.Exit(0)
	}
	if !cfg.Foreground {
		if err := daemon.Daemonize(cfg.PIDFile, cfg.LogFile); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to daemonize: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	defer daemon.RemovePID(cfg.PIDFile)
	app.New(cfg).Run()
}
