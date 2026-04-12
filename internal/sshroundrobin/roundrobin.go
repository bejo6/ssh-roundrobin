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
	activeIdx int
	hasActive bool
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

func NewRoundRobin(strategy ...string) *RoundRobin {
	selected := StrategyFailover
	if len(strategy) > 0 {
		selected = normalizeStrategy(strategy[0])
	}

	return &RoundRobin{
		servers:   make([]*SSHClient, 0),
		index:     0,
		strategy:  selected,
		activeIdx: 0,
		hasActive: false,
	}
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

	startIdx := atomic.AddUint32(&rr.index, 1) - 1
	checked := 0

	for checked < len(rr.servers) {
		idx := int(startIdx+uint32(checked)) % len(rr.servers)
		client := rr.servers[idx]

		if client.IsConnected() {
			return client, nil
		}

		checked++
	}

	for _, client := range rr.servers {
		if err := client.Reconnect(); err == nil {
			return client, nil
		}
	}

	return nil, fmt.Errorf("no available servers")
}

func (rr *RoundRobin) getFailoverLocked() (*SSHClient, error) {
	if rr.hasActive && rr.activeIdx >= 0 && rr.activeIdx < len(rr.servers) {
		active := rr.servers[rr.activeIdx]
		if active.IsConnected() {
			return active, nil
		}
		if err := active.Reconnect(); err == nil {
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
		if client.IsConnected() {
			rr.activeIdx = idx
			rr.hasActive = true
			return client, nil
		}

		if err := client.Reconnect(); err == nil {
			rr.activeIdx = idx
			rr.hasActive = true
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
