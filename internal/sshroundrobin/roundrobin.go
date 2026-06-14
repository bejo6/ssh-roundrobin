package sshroundrobin

import (
	"fmt"
	"strings"
	"sync"
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
	servers           []*SSHClient
	mu                sync.RWMutex
	index             uint32
	strategy          string
	maxActive         int
	activeIdx         int
	hasActive         bool
	targetFailures    map[string]targetFailureState
	OnConnectionError func(addr string, err error)
}

type targetFailureState struct {
	FailCount    int
	BlockedUntil time.Time
	LastError    string
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
		servers:        make([]*SSHClient, 0),
		index:          0,
		strategy:       selected,
		maxActive:      maxActive,
		activeIdx:      0,
		hasActive:      false,
		targetFailures: make(map[string]targetFailureState),
	}
}

func (rr *RoundRobin) notifyConnectionError(addr string, err error) {
	if rr.OnConnectionError != nil {
		rr.OnConnectionError(addr, err)
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
	return rr.GetForTarget("", nil)
}

func (rr *RoundRobin) GetForTarget(targetAddr string, exclude map[string]struct{}) (*SSHClient, error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	if len(rr.servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	if rr.strategy == StrategyFailover {
		return rr.getFailoverLocked(targetAddr, exclude)
	}

	return rr.getLoadBalanceLocked(targetAddr, exclude)
}

func (rr *RoundRobin) GetWithRetry(maxRetries int, retryDelay time.Duration) (*SSHClient, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		client, err := rr.GetForTarget("", nil)
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
