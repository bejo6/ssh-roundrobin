package sshroundrobin

import (
	"errors"
	"testing"
	"time"
)

func newTestClient(host string, port int) *SSHClient {
	return NewSSHClientLazy(&SSHServer{
		Host:       host,
		Port:       port,
		Username:   "test",
		AuthMethod: AuthMethodKey,
	})
}

func TestNormalizeStrategy(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", StrategyFailover},
		{"failover", StrategyFailover},
		{"FAILOVER", StrategyFailover},
		{"loadbalance", StrategyLoadBalance},
		{"LOADBALANCE", StrategyLoadBalance},
		{"  loadbalance  ", StrategyLoadBalance},
		{"round-robin", StrategyFailover},
		{"invalid", StrategyFailover},
	}
	for _, tt := range tests {
		got := normalizeStrategy(tt.input)
		if got != tt.want {
			t.Errorf("normalizeStrategy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNewRoundRobin(t *testing.T) {
	rr := NewRoundRobin("failover", 2)
	if rr == nil {
		t.Fatal("expected non-nil")
	}
	if rr.strategy != StrategyFailover {
		t.Errorf("strategy = %q, want %q", rr.strategy, StrategyFailover)
	}
	if rr.maxActive != 2 {
		t.Errorf("maxActive = %d, want 2", rr.maxActive)
	}
	if rr.Len() != 0 {
		t.Errorf("Len = %d, want 0", rr.Len())
	}
}

func TestNewRoundRobin_Defaults(t *testing.T) {
	rr := NewRoundRobin("", 0)
	if rr.strategy != StrategyFailover {
		t.Errorf("default strategy = %q, want %q", rr.strategy, StrategyFailover)
	}
	if rr.maxActive != 1 {
		t.Errorf("default maxActive = %d, want 1", rr.maxActive)
	}
}

func TestAddAndLen(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	if rr.Len() != 0 {
		t.Fatalf("initial Len = %d, want 0", rr.Len())
	}

	rr.Add(newTestClient("a", 22))
	if rr.Len() != 1 {
		t.Errorf("after add 1: Len = %d, want 1", rr.Len())
	}

	rr.Add(newTestClient("b", 22))
	rr.Add(newTestClient("c", 22))
	if rr.Len() != 3 {
		t.Errorf("after add 3: Len = %d, want 3", rr.Len())
	}
}

func TestGet_NoServers(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	_, err := rr.Get()
	if err == nil {
		t.Error("expected error with no servers")
	}
}

func TestGetForTarget_NoServers(t *testing.T) {
	rr := NewRoundRobin("loadbalance", 1)
	_, err := rr.GetForTarget("target:80", nil)
	if err == nil {
		t.Error("expected error with no servers")
	}
}

func TestGet_NoConnectedServers(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	rr.Add(newTestClient("a", 22))
	rr.Add(newTestClient("b", 22))

	// All lazy clients are disconnected, so Get should fail
	_, err := rr.Get()
	if err == nil {
		t.Error("expected error when no servers are connected")
	}
}

func TestRemove(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c1 := newTestClient("a", 22)
	c2 := newTestClient("b", 22)
	c3 := newTestClient("c", 22)
	rr.Add(c1)
	rr.Add(c2)
	rr.Add(c3)

	rr.Remove(c2)
	if rr.Len() != 2 {
		t.Errorf("after remove: Len = %d, want 2", rr.Len())
	}
}

func TestRemove_ActiveServer(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c1 := newTestClient("a", 22)
	rr.Add(c1)
	rr.hasActive = true
	rr.activeIdx = 0

	rr.Remove(c1)
	if rr.Len() != 0 {
		t.Errorf("after remove last: Len = %d, want 0", rr.Len())
	}
	if rr.hasActive {
		t.Error("hasActive should be false after removing active server")
	}
}

func TestRemove_AdjustsActiveIdx(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c1 := newTestClient("a", 22)
	c2 := newTestClient("b", 22)
	c3 := newTestClient("c", 22)
	rr.Add(c1)
	rr.Add(c2)
	rr.Add(c3)
	rr.hasActive = true
	rr.activeIdx = 2 // c3 is active

	rr.Remove(c1) // remove c1 (index 0), active should shift to 1
	if rr.activeIdx != 1 {
		t.Errorf("activeIdx = %d, want 1", rr.activeIdx)
	}
}

func TestCloseAll(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	rr.Add(newTestClient("a", 22))
	rr.Add(newTestClient("b", 22))

	rr.CloseAll()
	if rr.Len() != 0 {
		t.Errorf("after CloseAll: Len = %d, want 0", rr.Len())
	}
}

func TestReportTargetFailure(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	rr.Add(c)

	rr.ReportTargetFailure(c, "target:80", 2, 10*time.Minute, errors.New("dial failed"))

	// After 1 failure (below threshold of 2), state exists but not blocked
	rr.mu.RLock()
	key := targetFailureKey("a:22", "target:80")
	state, ok := rr.targetFailures[key]
	rr.mu.RUnlock()
	if !ok || state.FailCount != 1 {
		t.Errorf("expected 1 failure, got ok=%v count=%d", ok, state.FailCount)
	}

	rr.ReportTargetFailure(c, "target:80", 2, 10*time.Minute, errors.New("dial failed"))

	// After 2 failures (at threshold), should be blocked
	blocked := rr.isTargetBlockedLocked(c, "target:80", time.Now())
	if !blocked {
		t.Error("should be blocked at threshold")
	}
}

func TestReportTargetSuccess_ClearsBlockage(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	rr.Add(c)

	rr.ReportTargetFailure(c, "target:80", 1, 10*time.Minute, nil)
	rr.ReportTargetSuccess(c, "target:80")

	blocked := rr.isTargetBlockedLocked(c, "target:80", time.Now())
	if blocked {
		t.Error("should not be blocked after success")
	}
}

func TestReportTargetFailure_NilClient(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	// Should not panic
	rr.ReportTargetFailure(nil, "target:80", 1, 10*time.Minute, nil)
}

func TestReportTargetFailure_EmptyTarget(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	// Should not panic
	rr.ReportTargetFailure(c, "", 1, 10*time.Minute, nil)
}

func TestCleanupExpiredTargets(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	rr.Add(c)

	rr.ReportTargetFailure(c, "target:80", 1, 1*time.Millisecond, nil)
	time.Sleep(5 * time.Millisecond)

	rr.CleanupExpiredTargets()

	rr.mu.RLock()
	_, exists := rr.targetFailures[targetFailureKey("a:22", "target:80")]
	rr.mu.RUnlock()
	if exists {
		t.Error("expired target failure should have been cleaned up")
	}
}

func TestIsTargetBlockedLocked_NilClient(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	blocked := rr.isTargetBlockedLocked(nil, "target:80", time.Now())
	if blocked {
		t.Error("nil client should never be blocked")
	}
}

func TestIsTargetBlockedLocked_EmptyTarget(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	blocked := rr.isTargetBlockedLocked(c, "", time.Now())
	if blocked {
		t.Error("empty target should never be blocked")
	}
}

func TestGetForTarget_ExcludeMap(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	c := newTestClient("a", 22)
	rr.Add(c)

	exclude := map[string]struct{}{"a:22": {}}
	_, err := rr.GetForTarget("", exclude)
	if err == nil {
		t.Error("expected error when all servers excluded")
	}
}

func TestStatsSnapshot_Empty(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	stats := rr.StatsSnapshot()
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d", len(stats))
	}
}

func TestStatsSummary_Empty(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	summary := rr.StatsSummary()
	if summary != "\n  - no upstreams" {
		t.Errorf("unexpected empty summary: %q", summary)
	}
}

func TestStatsSnapshot_WithServers(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	rr.Add(newTestClient("a", 22))
	rr.Add(newTestClient("b", 22))

	stats := rr.StatsSnapshot()
	if len(stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(stats))
	}
	if stats[0].Addr != "a:22" {
		t.Errorf("stats[0].Addr = %q, want a:22", stats[0].Addr)
	}
}

func TestTargetFailureKey(t *testing.T) {
	key := targetFailureKey("upstream:22", "target:80")
	if key != "upstream:22|target:80" {
		t.Errorf("unexpected key: %q", key)
	}
}

func TestOnConnectionErrorCallback(t *testing.T) {
	rr := NewRoundRobin("failover", 1)
	var called bool
	var calledAddr string
	rr.OnConnectionError = func(addr string, err error) {
		called = true
		calledAddr = addr
	}

	c := newTestClient("a", 22)
	rr.Add(c)

	// Try to get - will fail because lazy client isn't connected
	rr.Get()

	// The callback is called inside getFailoverLocked when EnsureConnected fails
	if called {
		if calledAddr != "a:22" {
			t.Errorf("callback addr = %q, want a:22", calledAddr)
		}
	}
}
