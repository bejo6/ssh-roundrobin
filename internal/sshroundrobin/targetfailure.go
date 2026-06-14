package sshroundrobin

import "time"

func targetFailureKey(upstreamAddr, targetAddr string) string {
	return upstreamAddr + "|" + targetAddr
}

func (rr *RoundRobin) isTargetBlockedLocked(client *SSHClient, targetAddr string, now time.Time) bool {
	if targetAddr == "" || client == nil {
		return false
	}

	state, ok := rr.targetFailures[targetFailureKey(client.ServerAddr(), targetAddr)]
	if !ok {
		return false
	}

	if state.BlockedUntil.After(now) {
		return true
	}

	delete(rr.targetFailures, targetFailureKey(client.ServerAddr(), targetAddr))
	return false
}

func (rr *RoundRobin) ReportTargetFailure(client *SSHClient, targetAddr string, threshold int, ttl time.Duration, err error) {
	if client == nil || targetAddr == "" {
		return
	}
	if threshold <= 0 {
		threshold = 1
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()

	key := targetFailureKey(client.ServerAddr(), targetAddr)
	state := rr.targetFailures[key]
	state.FailCount++
	if err != nil {
		state.LastError = err.Error()
	}
	if state.FailCount >= threshold {
		state.BlockedUntil = time.Now().Add(ttl)
	}
	rr.targetFailures[key] = state
}

func (rr *RoundRobin) ReportTargetSuccess(client *SSHClient, targetAddr string) {
	if client == nil || targetAddr == "" {
		return
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()
	delete(rr.targetFailures, targetFailureKey(client.ServerAddr(), targetAddr))
}

func (rr *RoundRobin) CleanupExpiredTargets() {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	now := time.Now()
	for key, state := range rr.targetFailures {
		if state.BlockedUntil.Before(now) {
			delete(rr.targetFailures, key)
		}
	}
}
