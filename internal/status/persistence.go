package status

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func (t *ServerStatusTracker) Load() error {
	if t.filePath == "" {
		return nil
	}

	data, err := os.ReadFile(t.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read status file: %w", err)
	}

	var sf statusFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("failed to parse status file: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for addr, entry := range sf.Servers {
		t.entries[addr] = entry
	}
	return nil
}

func (t *ServerStatusTracker) Flush() error {
	if t.filePath == "" {
		return nil
	}

	t.mu.Lock()
	sf := statusFile{
		UpdatedAt: time.Now().UTC(),
		Servers:   make(map[string]*ServerStatusEntry, len(t.entries)),
	}
	for addr, entry := range t.entries {
		copy := *entry
		sf.Servers[addr] = &copy
	}
	t.mu.Unlock()

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	dir := filepath.Dir(t.filePath)
	tmp, err := os.CreateTemp(dir, ".server_status_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write status: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, t.filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename status file: %w", err)
	}

	return nil
}

func (t *ServerStatusTracker) StartPeriodicFlush() {
	go func() {
		ticker := time.NewTicker(t.flushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := t.Flush(); err != nil {
					log.Printf("Status flush error: %v", err)
				}
			case <-t.stopCh:
				return
			}
		}
	}()
}

func (t *ServerStatusTracker) Stop() {
	close(t.stopCh)
}
