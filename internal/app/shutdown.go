package app

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// startHealthCheck launches the periodic health check goroutine.
// It stops when a.healthStop is closed.
func (a *App) startHealthCheck() {
	if !a.cfg.HealthCheck {
		return
	}

	go func() {
		ticker := time.NewTicker(a.cfg.HealthInterval)
		defer ticker.Stop()

		for {
			select {
			case <-a.healthStop:
				return
			case <-ticker.C:
			}

			a.runHealthCheckCycle()
		}
	}()
}

func (a *App) runHealthCheckCycle() {
	a.rr.CleanupExpiredTargets()
	report := a.rr.RunHealthChecks()
	if report.Checked == 0 {
		return
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

// handleSignals listens for SIGINT/SIGTERM and performs graceful shutdown.
func (a *App) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		signal.Stop(sigChan)
		log.Println("Shutting down...")

		// Stop health check first to prevent it from using resources we're about to close.
		close(a.healthStop)

		if a.cfg.ShowUpstreamStats {
			log.Printf("Final upstream stats: %s", a.rr.StatsSummary())
		}
		if err := a.tracker.Flush(); err != nil {
			log.Printf("Warning: failed to flush status file: %v", err)
		}
		a.tracker.Stop()
		a.listener.Close()
		a.rr.CloseAll()

		// Close log file last so shutdown messages are captured.
		if a.logFile != nil {
			a.logFile.Close()
		}
		close(a.shutdownCh)
	}()
}
