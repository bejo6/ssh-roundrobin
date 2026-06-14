package sshroundrobin

import (
	"fmt"
	"sync/atomic"
	"time"
)

func (rr *RoundRobin) getLoadBalanceLocked(targetAddr string, exclude map[string]struct{}) (*SSHClient, error) {
	connectedCount := rr.connectedCountLocked()
	startIdx := atomic.AddUint32(&rr.index, 1) - 1
	checked := 0
	now := time.Now()

	for checked < len(rr.servers) {
		idx := int(startIdx+uint32(checked)) % len(rr.servers)
		client := rr.servers[idx]
		if exclude != nil {
			if _, ok := exclude[client.ServerAddr()]; ok {
				checked++
				continue
			}
		}
		if rr.isTargetBlockedLocked(client, targetAddr, now) {
			checked++
			continue
		}

		if client.IsConnected() {
			client.MarkSelected()
			return client, nil
		}

		if connectedCount < rr.maxActive {
			if err := client.EnsureConnected(); err == nil {
				connectedCount++
				client.MarkSelected()
				return client, nil
			} else {
				rr.notifyConnectionError(client.ServerAddr(), err)
			}
		}

		checked++
	}

	return nil, fmt.Errorf("no available servers")
}

func (rr *RoundRobin) getFailoverLocked(targetAddr string, exclude map[string]struct{}) (*SSHClient, error) {
	now := time.Now()
	if rr.hasActive && rr.activeIdx >= 0 && rr.activeIdx < len(rr.servers) {
		active := rr.servers[rr.activeIdx]
		if exclude != nil {
			if _, ok := exclude[active.ServerAddr()]; ok {
				goto scanNext
			}
		}
		if rr.isTargetBlockedLocked(active, targetAddr, now) {
			goto scanNext
		}
		if active.IsConnected() {
			active.MarkSelected()
			return active, nil
		}
		if err := active.EnsureConnected(); err == nil {
			active.MarkSelected()
			return active, nil
		} else {
			rr.notifyConnectionError(active.ServerAddr(), err)
		}
	}

scanNext:
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
		if exclude != nil {
			if _, ok := exclude[client.ServerAddr()]; ok {
				continue
			}
		}
		if rr.isTargetBlockedLocked(client, targetAddr, now) {
			continue
		}
		if client.IsConnected() {
			rr.activeIdx = idx
			rr.hasActive = true
			client.MarkSelected()
			return client, nil
		}
		if err := client.EnsureConnected(); err == nil {
			rr.activeIdx = idx
			rr.hasActive = true
			client.MarkSelected()
			return client, nil
		} else {
			rr.notifyConnectionError(client.ServerAddr(), err)
		}
	}

	rr.hasActive = false
	rr.activeIdx = 0
	return nil, fmt.Errorf("no available servers")
}
