package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWritePID_Empty(t *testing.T) {
	if err := WritePID(""); err != nil {
		t.Errorf("empty path should be no-op, got: %v", err)
	}
}

func TestWritePID_WritesCurrentPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("invalid PID in file: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestReadPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	expected := 12345
	if err := os.WriteFile(path, []byte(strconv.Itoa(expected)+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}
	if pid != expected {
		t.Errorf("expected %d, got %d", expected, pid)
	}
}

func TestReadPID_NonExistent(t *testing.T) {
	_, err := ReadPID("/tmp/nonexistent_pid_file_12345.pid")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadPID_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadPID(path)
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
}

func TestRemovePID_Empty(t *testing.T) {
	if err := RemovePID(""); err != nil {
		t.Errorf("empty path should be no-op, got: %v", err)
	}
}

func TestRemovePID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	if err := os.WriteFile(path, []byte("123\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := RemovePID(path); err != nil {
		t.Fatalf("RemovePID failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

func TestRemovePID_NonExistent(t *testing.T) {
	if err := RemovePID("/tmp/nonexistent_pid_file_12345.pid"); err != nil {
		t.Errorf("removing nonexistent PID should not error, got: %v", err)
	}
}

func TestIsRunning_InvalidPIDFile(t *testing.T) {
	running, _, err := IsRunning("/tmp/nonexistent_pid_file_12345.pid")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if running {
		t.Error("should not be running with bad file")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	running, pid, err := IsRunning(path)
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if !running {
		t.Error("current process should be running")
	}
	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestStatus_DelegatesToIsRunning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	running, pid, err := Status(path)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !running {
		t.Error("expected running=true")
	}
	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}
