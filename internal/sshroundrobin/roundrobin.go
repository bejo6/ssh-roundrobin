package sshroundrobin

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ServerStatus string

const (
	StatusOnline        ServerStatus = "online"
	StatusOffline       ServerStatus = "offline"
	StrategyLoadBalance              = "loadbalance"
	StrategyFailover                 = "failover"
)

type RoundRobin struct {
	servers   []*SSHClient
	mu        sync.RWMutex
	index     uint32
	strategy  string
	maxActive int
	activeIdx int
	hasActive bool
}

type UpstreamStat struct {
	Addr             string
	Mode             string
	Healthy          bool
	Active           bool
	SelectedCount    uint64
	ReconnectCount   uint64
	HealthcheckCount uint64
	LastSelectedAt   time.Time
	LastCheckedAt    time.Time
	LastError        string
}

type HealthCheckReport struct {
	Checked      int
	Recovered    []string
	Failed       []string
	SwitchedFrom string
	SwitchedTo   string
}

func normalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "":
		return StrategyFailover
	case StrategyLoadBalance:
		return StrategyLoadBalance
	case StrategyFailover:
		return StrategyFailover
	default:
		return StrategyFailover
	}
}

func NewRoundRobin(strategy string, maxActive int) *RoundRobin {
	selected := normalizeStrategy(strategy)
	if maxActive <= 0 {
		maxActive = 1
	}

	return &RoundRobin{
		servers:   make([]*SSHClient, 0),
		index:     0,
		strategy:  selected,
		maxActive: maxActive,
		activeIdx: 0,
		hasActive: false,
	}
}

func (rr *RoundRobin) connectedCountLocked() int {
	count := 0
	for _, client := range rr.servers {
		if client.IsConnected() {
			count++
		}
	}
	return count
}

func (rr *RoundRobin) Add(client *SSHClient) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.servers = append(rr.servers, client)
}

func (rr *RoundRobin) Get() (*SSHClient, error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	if len(rr.servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	if rr.strategy == StrategyFailover {
		return rr.getFailoverLocked()
	}

	return rr.getLoadBalanceLocked()
}

func (rr *RoundRobin) getLoadBalanceLocked() (*SSHClient, error) {
	connectedCount := rr.connectedCountLocked()
	startIdx := atomic.AddUint32(&rr.index, 1) - 1
	checked := 0

	for checked < len(rr.servers) {
		idx := int(startIdx+uint32(checked)) % len(rr.servers)
		client := rr.servers[idx]

		if client.IsConnected() {
			client.MarkSelected()
			return client, nil
		}

		if connectedCount < rr.maxActive {
			if err := client.EnsureConnected(); err == nil {
				connectedCount++
				client.MarkSelected()
				return client, nil
			}
		}

		checked++
	}

	return nil, fmt.Errorf("no available servers")
}

func (rr *RoundRobin) getFailoverLocked() (*SSHClient, error) {
	if rr.hasActive && rr.activeIdx >= 0 && rr.activeIdx < len(rr.servers) {
		active := rr.servers[rr.activeIdx]
		if active.IsConnected() || active.EnsureConnected() == nil {
			active.MarkSelected()
			return active, nil
		}
	}

	startIdx := 0
	if rr.hasActive && len(rr.servers) > 0 {
		startIdx = (rr.activeIdx + 1) % len(rr.servers)
	} else {
		startIdx = int(atomic.AddUint32(&rr.index, 1)-1) % len(rr.servers)
	}

	for checked := 0; checked < len(rr.servers); checked++ {
		idx := (startIdx + checked) % len(rr.servers)
		if rr.hasActive && idx == rr.activeIdx {
			continue
		}

		client := rr.servers[idx]
		if client.IsConnected() || client.EnsureConnected() == nil {
			rr.activeIdx = idx
			rr.hasActive = true
			client.MarkSelected()
			return client, nil
		}
	}

	rr.hasActive = false
	rr.activeIdx = 0
	return nil, fmt.Errorf("no available servers")
}

func (rr *RoundRobin) GetWithRetry(maxRetries int, retryDelay time.Duration) (*SSHClient, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		client, err := rr.Get()
		if err == nil {
			return client, nil
		}
		lastErr = err
		time.Sleep(retryDelay)
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func (rr *RoundRobin) Remove(client *SSHClient) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	for i, c := range rr.servers {
		if c == client {
			c.Close()
			rr.servers = append(rr.servers[:i], rr.servers[i+1:]...)
			if len(rr.servers) == 0 {
				rr.hasActive = false
				rr.activeIdx = 0
				return
			}

			if rr.hasActive {
				if i == rr.activeIdx {
					rr.hasActive = false
					rr.activeIdx = 0
				} else if i < rr.activeIdx {
					rr.activeIdx--
				}
			}
			return
		}
	}
}

func (rr *RoundRobin) CloseAll() {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	for _, client := range rr.servers {
		client.Close()
	}
	rr.servers = nil
	rr.hasActive = false
	rr.activeIdx = 0
}

func (rr *RoundRobin) Len() int {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return len(rr.servers)
}

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
