package status

import (
	"log"
	"sync"
	"time"
)

type ServerStatusEntry struct {
	Addr            string    `json:"addr"`
	Status          string    `json:"status"` // "success" | "fail"
	LastChecked     time.Time `json:"last_checked"`
	ConsecutiveOK   int       `json:"consecutive_ok"`
	ConsecutiveFail int       `json:"consecutive_fail"`
	TotalSuccess    uint64    `json:"total_success"`
	TotalFail       uint64    `json:"total_fail"`
	LastError       string    `json:"last_error,omitempty"`
}

type statusFile struct {
	UpdatedAt time.Time                     `json:"updated_at"`
	Servers   map[string]*ServerStatusEntry `json:"servers"`
}

type ServerStatusTracker struct {
	mu            sync.Mutex
	entries       map[string]*ServerStatusEntry
	filePath      string
	logChanges    bool
	flushInterval time.Duration
	stopCh        chan struct{}
}

func NewServerStatusTracker(filePath string, logChanges bool, flushInterval time.Duration) *ServerStatusTracker {
	if flushInterval <= 0 {
		flushInterval = 30 * time.Second
	}
	return &ServerStatusTracker{
		entries:       make(map[string]*ServerStatusEntry),
		filePath:      filePath,
		logChanges:    logChanges,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
	}
}

func (t *ServerStatusTracker) RecordSuccess(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.getOrCreateEntry(addr)
	prevStatus := entry.Status

	entry.Status = "success"
	entry.LastChecked = time.Now().UTC()
	entry.ConsecutiveOK++
	entry.ConsecutiveFail = 0
	entry.TotalSuccess++
	entry.LastError = ""

	if t.logChanges && prevStatus == "fail" {
		log.Printf("Server %s: fail->success (consecutive_ok=%d)", addr, entry.ConsecutiveOK)
	}
}

func (t *ServerStatusTracker) RecordFail(addr string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.getOrCreateEntry(addr)
	prevStatus := entry.Status

	entry.Status = "fail"
	entry.LastChecked = time.Now().UTC()
	entry.ConsecutiveFail++
	entry.ConsecutiveOK = 0
	entry.TotalFail++
	if err != nil {
		entry.LastError = err.Error()
	}

	if t.logChanges && prevStatus != "fail" {
		log.Printf("Server %s: %s->fail (consecutive_fail=%d)", addr, prevStatus, entry.ConsecutiveFail)
	}
}

func (t *ServerStatusTracker) GetDeprioritized(threshold int) []string {
	if threshold <= 0 {
		threshold = 5
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	var addrs []string
	for addr, entry := range t.entries {
		if entry.ConsecutiveFail >= threshold {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func (t *ServerStatusTracker) GetEntry(addr string) *ServerStatusEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[addr]
	if !ok {
		return nil
	}
	copy := *entry
	return &copy
}

func (t *ServerStatusTracker) Snapshot() map[string]*ServerStatusEntry {
	t.mu.Lock()
	defer t.mu.Unlock()

	snap := make(map[string]*ServerStatusEntry, len(t.entries))
	for addr, entry := range t.entries {
		copy := *entry
		snap[addr] = &copy
	}
	return snap
}

func (t *ServerStatusTracker) SeedTargetFailures(failThreshold int) map[string]int {
	if failThreshold <= 0 {
		failThreshold = 5
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string]int)
	for addr, entry := range t.entries {
		if entry.ConsecutiveFail >= failThreshold {
			result[addr] = entry.ConsecutiveFail
		}
	}
	return result
}

func (t *ServerStatusTracker) getOrCreateEntry(addr string) *ServerStatusEntry {
	entry, ok := t.entries[addr]
	if !ok {
		entry = &ServerStatusEntry{
			Addr:   addr,
			Status: "success",
		}
		t.entries[addr] = entry
	}
	return entry
}
