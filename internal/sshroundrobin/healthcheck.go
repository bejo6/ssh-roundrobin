package sshroundrobin

import (
	"fmt"
	"strings"
)

func (rr *RoundRobin) StatsSnapshot() []UpstreamStat {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	stats := make([]UpstreamStat, 0, len(rr.servers))
	for idx, client := range rr.servers {
		clientStats := client.Stats()
		stats = append(stats, UpstreamStat{
			Addr:             clientStats.Addr,
			Mode:             clientStats.Mode,
			Healthy:          clientStats.Healthy,
			Active:           rr.hasActive && idx == rr.activeIdx,
			SelectedCount:    clientStats.SelectedCount,
			ReconnectCount:   clientStats.ReconnectCount,
			HealthcheckCount: clientStats.HealthcheckCount,
			LastSelectedAt:   clientStats.LastSelectedAt,
			LastCheckedAt:    clientStats.LastCheckedAt,
			LastError:        clientStats.LastError,
		})
	}

	return stats
}

func (rr *RoundRobin) StatsSummary() string {
	stats := rr.StatsSnapshot()
	if len(stats) == 0 {
		return "\n  - no upstreams"
	}

	const (
		ansiReset   = "\033[0m"
		ansiRed     = "\033[31m"
		ansiGreen   = "\033[32m"
		ansiYellow  = "\033[33m"
		ansiBlue    = "\033[34m"
		ansiMagenta = "\033[35m"
		ansiCyan    = "\033[36m"
		ansiWhite   = "\033[37m"
	)

	upstreamColors := []string{ansiCyan, ansiBlue, ansiMagenta, ansiWhite}
	lines := make([]string, 0, len(stats))

	for idx, stat := range stats {
		upstreamColor := upstreamColors[idx%len(upstreamColors)]
		addr := fmt.Sprintf("%s%s%s", upstreamColor, stat.Addr, ansiReset)

		health := fmt.Sprintf("%sdown%s", ansiRed, ansiReset)
		if stat.Healthy {
			health = fmt.Sprintf("%sup%s", ansiGreen, ansiReset)
		}

		active := "no"
		if stat.Active {
			active = fmt.Sprintf("%syes%s", ansiYellow, ansiReset)
		}

		line := fmt.Sprintf("  - %s mode=%s health=%s active=%s hits=%d reconnects=%d checks=%d", addr, stat.Mode, health, active, stat.SelectedCount, stat.ReconnectCount, stat.HealthcheckCount)
		if stat.LastError != "" {
			line += fmt.Sprintf(" err=%q", stat.LastError)
		}
		lines = append(lines, line)
	}

	return "\n" + strings.Join(lines, "\n")
}

func (rr *RoundRobin) RunHealthChecks() HealthCheckReport {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	report := HealthCheckReport{}
	if len(rr.servers) == 0 {
		return report
	}

	healthy := make([]bool, len(rr.servers))
	for idx, client := range rr.servers {
		if !client.IsConnected() {
			healthy[idx] = false
			continue
		}

		report.Checked++
		wasHealthy := client.IsHealthy()
		err := client.HealthCheck()
		if err != nil {
			healthy[idx] = false
			if wasHealthy {
				report.Failed = append(report.Failed, client.ServerAddr())
			}
			continue
		}

		healthy[idx] = true
		if !wasHealthy {
			report.Recovered = append(report.Recovered, client.ServerAddr())
		}
	}

	if rr.strategy != StrategyFailover {
		return report
	}

	if rr.hasActive && rr.activeIdx >= 0 && rr.activeIdx < len(healthy) && healthy[rr.activeIdx] {
		return report
	}

	if rr.hasActive && rr.activeIdx >= 0 && rr.activeIdx < len(rr.servers) {
		report.SwitchedFrom = rr.servers[rr.activeIdx].ServerAddr()
	}

	for idx, ok := range healthy {
		if !ok {
			continue
		}
		rr.activeIdx = idx
		rr.hasActive = true
		report.SwitchedTo = rr.servers[idx].ServerAddr()
		return report
	}

	for idx, client := range rr.servers {
		if client.EnsureConnected() == nil {
			rr.activeIdx = idx
			rr.hasActive = true
			report.SwitchedTo = client.ServerAddr()
			return report
		}
	}

	rr.activeIdx = 0
	rr.hasActive = false
	return report
}
