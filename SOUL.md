# SOUL.md — Go Coding Style Guide

> This document defines the coding conventions and philosophy for this project.
> Every AI agent working on this codebase MUST read and follow this file.

---

## 1. Code Must Be Optimal

If code starts getting too long, think about making it modular/split.
Do not dump all logic into one file.

**Rule of thumb:**
- `cmd/` main.go: **~100 lines max** — orchestration only
- `internal/` files: **~200 lines max** per file
- If a function exceeds 50 lines, split it

---

## 2. Main Function is an Orchestrator

`cmd/main.go` should ONLY do:
1. Parse config and flags
2. Handle utility modes (`-stop`, `-status`)
3. Daemonize if needed
4. Initialize components (servers, tracker, listener)
5. Start health check goroutine
6. Handle signals
7. Accept loop

ALL proxy logic lives in `internal/`. If main.go is doing the work, it's wrong.

---

## 3. Minimize External Dependencies

Use stdlib `flag` first. Only add external packages if they solve a critical
problem that stdlib cannot handle. Every dependency is a liability — maintenance
burden, version conflicts, attack surface.

```go
// Stdlib flag is enough for most cases:
flag.StringVar(&target, "target", "", "Target address")
flag.Parse()
```

---

## 4. Daemon Lifecycle

Use `syscall.ForkExec` to fork to background. The child re-invokes itself
with `-fg` flag to prevent recursive fork.

```go
func Daemonize(pidFile, logFile string) error {
    args := append([]string{execPath}, os.Args[1:]...)
    // Add -fg to prevent recursive fork
    args = append(args, "-fg")

    attr := &syscall.ProcAttr{
        Files: []uintptr{0, logF.Fd(), logF.Fd()},
    }
    pid, _ := syscall.ForkExec(args[0], args, attr)
    // Parent writes PID and exits
    return nil
}
```

Always include:
- PID file management (write on start, remove on exit)
- Signal handler (SIGTERM, SIGINT) for graceful shutdown
- Log file redirection in daemon mode

---

## 5. Study Reference Code First

Before implementing ANY feature:
1. Read the existing codebase thoroughly
2. Understand WHY it was written that way
3. Understand the CONSTRAINTS behind the design
4. Then implement

Never assume a pattern is "wrong" without understanding the context.

---

## 6. Always Use Makefile for Build

Do NOT run `go build` directly. Always use the Makefile.

```bash
# Build for current platform
make build

# Build and run
make run

# Build for all platforms
make all

# Clean build artifacts
make clean
```

The Makefile handles build flags, cross-compilation, and output paths.
Running `go build` directly bypasses these and produces inconsistent results.


## 7. Respect Deployment Target

Do NOT upgrade Go/language version just because "newer is better."
If the binary works, don't touch it.
Version upgrades introduce risk without value for deployed tools.

**"If it ain't broke, don't fix it."**
