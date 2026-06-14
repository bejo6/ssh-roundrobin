package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Daemonize forks the current process into a background daemon.
// The parent process exits with code 0 after the child is confirmed alive.
// The child continues execution. Returns error only if fork fails.
func Daemonize(pidFile, logFile string) error {
	// Resolve absolute path of current executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable symlink: %w", err)
	}

	// Build args: re-invoke ourselves with -fg to prevent recursive fork
	args := []string{execPath}
	args = append(args, os.Args[1:]...)
	hasFg := false
	for _, a := range os.Args[1:] {
		if a == "-fg" {
			hasFg = true
			break
		}
	}
	if !hasFg {
		args = append(args, "-fg")
	}

	// Prepare child environment
	env := os.Environ()

	// Set up stdout/stderr to log file or /dev/null
	var logF *os.File
	if logFile != "" {
		logF, err = os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file for daemon: %w", err)
		}
	} else {
		logF, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open /dev/null: %w", err)
		}
	}
	defer logF.Close()

	// Fork
	attr := &syscall.ProcAttr{
		Dir:   ".",
		Env:   env,
		Files: []uintptr{0, logF.Fd(), logF.Fd()}, // stdin, stdout, stderr
	}

	pid, forkErr := syscall.ForkExec(args[0], args, attr)
	if forkErr != nil {
		return fmt.Errorf("failed to fork daemon: %w", forkErr)
	}

	// Parent writes PID file and exits
	if pidFile != "" {
		data := []byte(strconv.Itoa(pid) + "\n")
		if err := os.WriteFile(pidFile, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
		}
	}

	return nil
}

func WritePID(pidFile string) error {
	if pidFile == "" {
		return nil
	}

	pid := os.Getpid()
	data := []byte(strconv.Itoa(pid) + "\n")
	if err := os.WriteFile(pidFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

func RemovePID(pidFile string) error {
	if pidFile == "" {
		return nil
	}
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

func ReadPID(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

func IsRunning(pidFile string) (bool, int, error) {
	pid, err := ReadPID(pidFile)
	if err != nil {
		return false, 0, err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, pid, nil
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return false, pid, nil
	}

	return true, pid, nil
}

func Stop(pidFile string) error {
	running, pid, err := IsRunning(pidFile)
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if !running {
		return fmt.Errorf("daemon is not running (PID file: %s)", pidFile)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to PID %d: %w", pid, err)
	}

	// Wait for the process to actually exit so the port is released.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process is gone.
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process %d did not exit within 10s after SIGTERM", pid)
}

func Status(pidFile string) (running bool, pid int, err error) {
	return IsRunning(pidFile)
}
