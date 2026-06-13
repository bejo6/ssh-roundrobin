package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewServerStatusTracker(t *testing.T) {
	tracker := NewServerStatusTracker("/tmp/test_status.json", false, 10*time.Second)
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestRecordSuccessAndFail(t *testing.T) {
	tracker := NewServerStatusTracker("", false, 0)

	tracker.RecordSuccess("192.168.1.1:22")
	entry := tracker.GetEntry("192.168.1.1:22")
	if entry == nil {
		t.Fatal("expected entry")
	}
	if entry.Status != "success" {
		t.Errorf("expected status success, got %s", entry.Status)
	}
	if entry.ConsecutiveOK != 1 {
		t.Errorf("expected consecutive_ok=1, got %d", entry.ConsecutiveOK)
	}
	if entry.TotalSuccess != 1 {
		t.Errorf("expected total_success=1, got %d", entry.TotalSuccess)
	}

	tracker.RecordFail("192.168.1.1:22", os.ErrNotExist)
	entry = tracker.GetEntry("192.168.1.1:22")
	if entry.Status != "fail" {
		t.Errorf("expected status fail, got %s", entry.Status)
	}
	if entry.ConsecutiveFail != 1 {
		t.Errorf("expected consecutive_fail=1, got %d", entry.ConsecutiveFail)
	}
	if entry.ConsecutiveOK != 0 {
		t.Errorf("expected consecutive_ok=0 after fail, got %d", entry.ConsecutiveOK)
	}

	tracker.RecordFail("192.168.1.1:22", nil)
	entry = tracker.GetEntry("192.168.1.1:22")
	if entry.ConsecutiveFail != 2 {
		t.Errorf("expected consecutive_fail=2, got %d", entry.ConsecutiveFail)
	}

	tracker.RecordSuccess("192.168.1.1:22")
	entry = tracker.GetEntry("192.168.1.1:22")
	if entry.ConsecutiveFail != 0 {
		t.Errorf("expected consecutive_fail=0 after success, got %d", entry.ConsecutiveFail)
	}
	if entry.ConsecutiveOK != 1 {
		t.Errorf("expected consecutive_ok=1 after success, got %d", entry.ConsecutiveOK)
	}
}

func TestFlushAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server_status.json")

	tracker := NewServerStatusTracker(path, false, 0)
	tracker.RecordSuccess("10.0.0.1:22")
	tracker.RecordFail("10.0.0.2:22", os.ErrPermission)

	if err := tracker.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected status file to exist after flush")
	}

	tracker2 := NewServerStatusTracker(path, false, 0)
	if err := tracker2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	entry1 := tracker2.GetEntry("10.0.0.1:22")
	if entry1 == nil || entry1.TotalSuccess != 1 {
		t.Errorf("expected server1 total_success=1, got %v", entry1)
	}

	entry2 := tracker2.GetEntry("10.0.0.2:22")
	if entry2 == nil || entry2.TotalFail != 1 {
		t.Errorf("expected server2 total_fail=1, got %v", entry2)
	}
}

func TestLoadNonExistent(t *testing.T) {
	tracker := NewServerStatusTracker("/tmp/nonexistent_status_file_12345.json", false, 0)
	if err := tracker.Load(); err != nil {
		t.Errorf("load of nonexistent file should not error, got: %v", err)
	}
}

func TestGetDeprioritized(t *testing.T) {
	tracker := NewServerStatusTracker("", false, 0)

	for i := 0; i < 5; i++ {
		tracker.RecordFail("bad-server:22", nil)
	}
	tracker.RecordFail("ok-server:22", nil)

	deprioritized := tracker.GetDeprioritized(5)
	if len(deprioritized) != 1 || deprioritized[0] != "bad-server:22" {
		t.Errorf("expected [bad-server:22], got %v", deprioritized)
	}

	tracker.RecordSuccess("bad-server:22")
	deprioritized = tracker.GetDeprioritized(5)
	if len(deprioritized) != 0 {
		t.Errorf("expected empty after recovery, got %v", deprioritized)
	}
}

func TestSeedTargetFailures(t *testing.T) {
	tracker := NewServerStatusTracker("", false, 0)
	for i := 0; i < 3; i++ {
		tracker.RecordFail("down-server:22", nil)
	}

	seed := tracker.SeedTargetFailures(3)
	if count, ok := seed["down-server:22"]; !ok || count != 3 {
		t.Errorf("expected down-server:22 with count 3, got %v", seed)
	}
}

func TestSnapshot(t *testing.T) {
	tracker := NewServerStatusTracker("", false, 0)
	tracker.RecordSuccess("a:22")
	tracker.RecordFail("b:22", nil)

	snap := tracker.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 entries, got %d", len(snap))
	}
}
