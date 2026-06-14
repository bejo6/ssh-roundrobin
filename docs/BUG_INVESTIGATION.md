# Bug Investigation Report — Memory Leak, Race Condition, Crash/Panic

> **Date:** 2026-06-14
> **Commit:** c55d43a (main)
> **Scope:** All Go source files in cmd/, internal/
> **Total findings:** 14 (4 Critical, 5 High, 5 Medium)

---

## Summary Table

| #  | Category    | Severity | File                               | Short Description                                      |
|----|-------------|----------|------------------------------------|--------------------------------------------------------|
| 1  | Race        | CRITICAL | `sshclient.go:288-289`             | `Client()` exposes raw pointer — concurrent nil by HealthCheck |
| 2  | Race        | CRITICAL | `sshclient.go:322-329`             | `Close()` reads `c.client` outside mutex               |
| 3  | Crash       | CRITICAL | `dial.go:42-43`                    | Nil pointer dereference on `sshConn.Dial()`            |
| 4  | Memory      | CRITICAL | `roundrobin.go:28-31`              | `targetFailures` map grows without bound               |
| 5  | Crash       | HIGH     | `sshclient.go:406-407`             | `Reconnect()` reads old `c.client` then accesses under deferred unlock |
| 6  | Memory      | HIGH     | `app.go:185-197`                   | No connection limit — goroutine exhaustion DoS         |
| 7  | Leak        | HIGH     | `socks5.go` / `forward.go`         | Zombie proxy commands from orphaned `commandConn`      |
| 8  | Crash       | HIGH     | `app.go:170-182`                   | `os.Exit(0)` in signal handler skips all defers        |
| 9  | Race        | HIGH     | `roundrobin.go:169-186`            | `GetForTarget` TOCTOU — client state changes after selection |
| 10 | Leak        | MEDIUM   | `sshclient.go:122-132`             | `commandConn` deadline no-ops — hung commands block forever |
| 11 | Crash       | MEDIUM   | `daemon.go:58-76`                  | Race between parent PID write and child `writePID()`   |
| 12 | Leak        | MEDIUM   | `status.go:203-219`                | `StartPeriodicFlush` goroutine leaks if `Stop()` never called |
| 13 | Memory      | MEDIUM   | `status.go:242-252`                | `entries` map grows with every unique upstream address  |
| 14 | Crash       | MEDIUM   | `app.go:44-63`                     | Log file opened but never closed                       |

---

## CRITICAL Findings

### 1. `Client()` Exposes Raw SSH Client Pointer (Race → Crash)

**File:** `internal/sshroundrobin/sshclient.go:288-289`

```go
func (c *SSHClient) Client() *ssh.Client {
    return c.client
}
```

**Problem:** `Client()` returns the raw `*ssh.Client` pointer **without any locking**. The caller (`dial.go:42`) uses it to dial:

```go
sshConn := client.Client()     // unlocked read
targetConn, dialErr := sshConn.Dial("tcp", targetAddr)  // use after potential nil
```

Between these two lines, `HealthCheck()` (called from the health-check goroutine) can set `c.client = nil`:

```go
// sshclient.go:366 — under c.mu lock
c.client = nil  // ← this makes the pointer returned by Client() invalid
```

This is a classic **use-after-free** pattern in Go — the pointer is valid when returned but becomes nil before use.

**Fix:** Replace `Client()` with a method that dials under lock, or return a cloned reference:

```go
// Option A: Dial-under-lock (recommended)
func (c *SSHClient) Dial(network, addr string) (net.Conn, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.client == nil {
        return nil, fmt.Errorf("ssh client not connected")
    }
    return c.client.Dial(network, addr)
}

// Option B: Reference counting (complex, not recommended for this codebase)
```

---

### 2. `Close()` Reads `c.client` Outside Mutex (Race → Crash)

**File:** `internal/sshroundrobin/sshclient.go:322-329`

```go
func (c *SSHClient) Close() error {
    c.mu.Lock()
    c.healthy = false
    c.mu.Unlock()          // lock released here
    if c.client != nil {   // ← RACE: c.client read outside lock
        return c.client.Close()
    }
    return nil
}
```

**Problem:** After `c.mu.Unlock()`, another goroutine (e.g., `Reconnect()` or `HealthCheck()`) can set `c.client = nil`. The subsequent `c.client != nil` check and `c.client.Close()` call both race against that.

**Fix:**

```go
func (c *SSHClient) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.healthy = false
    if c.client != nil {
        err := c.client.Close()
        c.client = nil
        return err
    }
    return nil
}
```

---

### 3. Nil Pointer Dereference in `DialTargetWithRetries` (Crash → Panic)

**File:** `internal/proxy/dial.go:42-43`

```go
sshConn := client.Client()                      // can return nil
targetConn, dialErr := sshConn.Dial("tcp", targetAddr)  // PANIC if nil
```

**Problem:** When `client` is a lazy upstream that failed to connect (`EnsureConnected()` returned error but `getFailoverLocked` / `getLoadBalanceLocked` returned it anyway — see Finding #9), or after `HealthCheck()` nils out `c.client`, `sshConn` is nil. Calling `.Dial()` on nil panics.

This is the **most likely crash in production** — it combines with Finding #1.

**Fix:** Nil-check after `Client()`:

```go
sshConn := client.Client()
if sshConn == nil {
    rr.ReportTargetFailure(client, targetAddr, failThreshold, failTTL, fmt.Errorf("ssh client is nil"))
    continue // try next upstream
}
targetConn, dialErr := sshConn.Dial("tcp", targetAddr)
```

Or better: replace `Client()` with the `Dial()` method from Finding #1's fix.

---

### 4. `targetFailures` Map Unbounded Growth (Memory Leak)

**File:** `internal/sshroundrobin/roundrobin.go:28-31`

```go
type RoundRobin struct {
    ...
    targetFailures map[string]targetFailureState
    ...
}
```

**Problem:** Every unique `upstreamAddr|targetAddr` combination that fails gets an entry in `targetFailures`. Entries are only cleaned up when `isTargetBlockedLocked()` is called for the same key AND the TTL has expired. Entries for targets that are never retried are **never cleaned up**.

In SOCKS5 mode where clients connect to many unique destinations (browsing, apps), this map grows continuously:

- 1,000 unique targets × 5 upstreams = 5,000 entries
- Each entry ≈ 80 bytes (two strings + int + time) ≈ 400KB
- Over hours/days: tens of thousands of entries

**Fix:** Add periodic cleanup:

```go
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
```

Call from the health-check ticker in `app.go`:

```go
rr.CleanupExpiredTargets()  // every health check cycle
```

Also consider capping the map size (e.g., 10,000 entries) and evicting oldest entries.

---

## HIGH Findings

### 5. `Reconnect()` Double-Closes Old Client (Crash)

**File:** `internal/sshroundrobin/sshclient.go:402-422`

```go
func (c *SSHClient) Reconnect() error {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.client != nil {
        c.client.Close()   // close old connection
    }

    client, err := connect(c.server)  // can block 15s (SSH timeout)
    ...
```

**Problem:** While `connect()` blocks for up to 15 seconds, `c.mu` is held. This blocks ALL other operations on this SSHClient: `IsConnected()`, `HealthCheck()`, `Stats()`, `MarkSelected()`. If multiple goroutines try to reconnect the same client, they serialize on the mutex but each waits 15s.

Additionally, if `connect()` succeeds but the old `c.client.Close()` had already triggered a reconnect in another goroutine via SSH keepalive, the old client may already be in an inconsistent state.

**Fix:** Release the lock during the dial, re-acquire after:

```go
func (c *SSHClient) Reconnect() error {
    c.mu.Lock()
    oldClient := c.client
    c.client = nil  // mark as disconnected while reconnecting
    c.mu.Unlock()

    if oldClient != nil {
        oldClient.Close()
    }

    client, err := connect(c.server)
    if err != nil {
        c.mu.Lock()
        c.healthy = false
        c.lastErr = err.Error()
        c.mu.Unlock()
        return err
    }

    c.mu.Lock()
    c.client = client
    c.healthy = true
    c.lastErr = ""
    c.mu.Unlock()

    atomic.AddUint64(&c.reconnectCount, 1)
    return nil
}
```

---

### 6. No Connection Limit — Goroutine Exhaustion (Memory / DoS)

**File:** `internal/app/app.go:185-197`

```go
func (a *App) acceptLoop() {
    for {
        conn, err := a.listener.Accept()
        ...
        go proxy.HandleSocks5Connection(conn, ...)  // unbounded goroutine
    }
}
```

**Problem:** Every incoming TCP connection spawns a new goroutine with no upper limit. Each goroutine:
- Allocates ~8KB initial stack
- Holds an open file descriptor (TCP socket)
- Holds an SSH channel (another file descriptor)

Under connection flood (deliberate or from buggy client), the process runs out of file descriptors or memory. Typical `ulimit -n` is 1024 — after 500 connections the process crashes with "too many open files".

**Fix:** Add a semaphore (bounded channel):

```go
type App struct {
    ...
    connSem chan struct{} // connection semaphore
}

func New(cfg *config.Config) *App {
    maxConns := cfg.MaxConnections
    if maxConns <= 0 {
        maxConns = 100
    }
    return &App{cfg: cfg, connSem: make(chan struct{}, maxConns)}
}

func (a *App) acceptLoop() {
    for {
        conn, err := a.listener.Accept()
        if err != nil {
            log.Printf("Accept error: %v", err)
            continue
        }
        select {
        case a.connSem <- struct{}{}:
            go func() {
                defer func() { <-a.connSem }()
                proxy.HandleSocks5Connection(conn, ...)
            }()
        default:
            conn.Close() // reject when at capacity
            log.Printf("Connection rejected: max connections reached")
        }
    }
}
```

---

### 7. Zombie Proxy Commands (Resource Leak)

**File:** `internal/sshroundrobin/sshclient.go:184-231`, `internal/proxy/socks5.go`, `internal/proxy/forward.go`

**Problem:** When using `AuthMethodProxyCommand`, each SSH connection spawns a child process (e.g., `cloudflared`). The `commandConn.Close()` properly kills the process, BUT:

1. If the SSH connection is dropped unexpectedly (network failure, upstream dies), `Close()` may never be called on the `commandConn`
2. The `ssh.Client` wraps the `commandConn` internally — when `c.client.Close()` is called on the SSH client, it closes the underlying connection which should close `commandConn`. BUT if `c.client` is just GC'd without explicit Close, the process leaks
3. `Reconnect()` calls `c.client.Close()` which should propagate, but if Close errors are swallowed, the child process survives

Over time with unstable upstreams, zombie `cloudflared` processes accumulate.

**Fix:** Add process reaping in `commandConn`:

```go
func newCommandConn(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser) *commandConn {
    cc := &commandConn{
        stdin:  stdin,
        stdout: stdout,
        cmd:    cmd,
    }
    // Ensure process cleanup even if Close() is never called
    go func() {
        cmd.Wait()  // blocks until process exits; reaps zombie
    }()
    return cc
}
```

Also add a timeout to `commandConn` so hung processes are killed:

```go
func (c *commandConn) SetDeadline(t time.Time) error {
    if !t.IsZero() {
        time.AfterFunc(time.Until(t), func() { c.Close() })
    }
    return nil
}
```

---

### 8. `os.Exit(0)` in Signal Handler Skips Defers (Resource Leak)

**File:** `internal/app/app.go:170-182`

```go
go func() {
    <-sigChan
    log.Println("Shutting down...")
    ...
    a.rr.CloseAll()
    os.Exit(0)    // ← skips ALL deferred functions
}()
```

**Problem:** `os.Exit(0)` terminates the process immediately without running deferred functions. The following defers are skipped:

- `cmd/main.go:44`: `defer daemon.RemovePID(cfg.PIDFile)` — **PID file not cleaned up**
- `app.go:53`: log file is never closed

After SIGTERM, the PID file remains on disk. Next startup may fail with "daemon already running" if the PID hasn't been reused, or worse, send SIGTERM to an unrelated process if the PID was recycled.

**Fix:** Use a channel/flag instead of `os.Exit()`:

```go
// In App struct
type App struct {
    ...
    shutdownCh chan struct{}
}

// In handleSignals
go func() {
    <-sigChan
    log.Println("Shutting down...")
    if a.cfg.ShowUpstreamStats {
        log.Printf("Final upstream stats: %s", a.rr.StatsSummary())
    }
    if err := a.tracker.Flush(); err != nil {
        log.Printf("Warning: failed to flush status file: %v", err)
    }
    a.tracker.Stop()
    a.rr.CloseAll()
    close(a.shutdownCh)
}()

// In acceptLoop — check shutdown
func (a *App) acceptLoop() {
    for {
        select {
        case <-a.shutdownCh:
            return  // clean exit, defers run
        default:
        }
        conn, err := a.listener.Accept()
        ...
    }
}
```

---

### 9. TOCTOU Race in Server Selection (Race → Crash)

**File:** `internal/sshroundrobin/roundrobin.go:169-186`

```go
func (rr *RoundRobin) GetForTarget(...) (*SSHClient, error) {
    rr.mu.Lock()
    defer rr.mu.Unlock()
    ...
    if client.IsConnected() {   // check
        client.MarkSelected()
        return client, nil      // return — client state may change after this
    }
    ...
}
```

**Problem:** `GetForTarget` checks `IsConnected()` under `rr.mu` lock, but NOT under `c.mu` lock (the SSHClient's own lock). Between the check and the returned client being used in `dial.go:42-43`:

1. `GetForTarget` checks `client.IsConnected()` → true (under `rr.mu`)
2. `rr.mu` released
3. Health check goroutine: `client.HealthCheck()` fails → sets `c.client = nil` (under `c.mu`)
4. `dial.go:43`: `sshConn.Dial(...)` → **PANIC** (sshConn is nil)

The `rr.mu` lock and `c.mu` lock are independent — holding one doesn't protect against changes under the other.

**Fix:** This is fixed by implementing Finding #1's `Dial()` method — the actual connection attempt happens under `c.mu`:

```go
// In dial.go
targetConn, dialErr := client.Dial("tcp", targetAddr)  // lock inside
```

---

## MEDIUM Findings

### 10. `commandConn` Deadline No-Ops (Hang)

**File:** `internal/sshroundrobin/sshclient.go:122-132`

```go
func (c *commandConn) SetDeadline(_ time.Time) error    { return nil }
func (c *commandConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *commandConn) SetWriteDeadline(_ time.Time) error { return nil }
```

**Problem:** All deadline methods are no-ops. The SSH library may call `SetReadDeadline` during handshake or keepalive. With no deadline, a hung proxy command causes the connection to block forever, leaking goroutines and file descriptors.

**Fix:** Implement at least basic deadline support:

```go
func (c *commandConn) SetReadDeadline(t time.Time) error {
    if t.IsZero() {
        return nil
    }
    time.AfterFunc(time.Until(t), func() {
        c.Close() // force-close on timeout
    })
    return nil
}
```

---

### 11. PID File Race Between Parent and Child

**File:** `internal/daemon/daemon.go:71-76`, `internal/app/app.go:65-69`

**Problem:** Timeline:
1. Parent calls `Daemonize()` → forks child → writes child PID to file → returns
2. Child starts → `app.writePID()` → writes own (child) PID to same file

The child PID in step 1 and step 2 should be the same (child re-invokes itself). BUT there's a window between the fork and the child's `writePID()` where:
- If parent writes PID file but crashes before child starts → stale PID file
- If another process starts during this window → PID file corruption

This is a minor issue in practice since the window is very small.

**Fix:** Have only the child write the PID file (remove PID write from parent):

```go
func Daemonize(pidFile, logFile string) error {
    // ... fork logic ...
    // Don't write PID here — the child will do it via WritePID()
    return nil
}
```

---

### 12. `StartPeriodicFlush` Goroutine Leak

**File:** `internal/status/serverstatus.go:203-219`

```go
func (t *ServerStatusTracker) StartPeriodicFlush() {
    go func() {
        ticker := time.NewTicker(t.flushInterval)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C: ...
            case <-t.stopCh:
                return
            }
        }
    }()
}
```

**Problem:** If `Stop()` is never called (e.g., panic recovery, abnormal exit path), this goroutine leaks. The `stopCh` channel is never closed, and the goroutine runs forever with the ticker.

Low risk in practice since the app either runs until SIGTERM (which calls Stop) or panics (which kills everything).

**Fix:** Add timeout-based self-cleanup, or ensure Stop() is always called:

```go
// In App.Run(), ensure Stop is called on any exit:
defer a.tracker.Stop()
```

---

### 13. `entries` Map Unbounded Growth

**File:** `internal/status/serverstatus.go:242-252`

**Problem:** `t.entries` accumulates an entry for every unique upstream address that succeeds or fails. Unlike `targetFailures` (Finding #4), this is bounded by the number of configured servers, which is typically small (5-20). Low risk unless servers are dynamically added (not currently supported).

**Fix:** Low priority — no action needed unless dynamic server addition is planned.

---

### 14. Log File Opened But Never Closed

**File:** `internal/app/app.go:44-63`

```go
func (a *App) setupLogging() {
    if a.cfg.LogFile != "" {
        f, err := os.OpenFile(a.cfg.LogFile, ...)
        ...
        log.SetOutput(f)
    }
}
```

**Problem:** The file handle `f` is assigned to `log.SetOutput()` but never stored or closed. Since the app runs for the process lifetime, this isn't a practical leak. But `os.Exit(0)` (Finding #8) means the file is never properly closed even on shutdown.

**Fix:** Store the file handle and close on shutdown:

```go
type App struct {
    ...
    logFile *os.File
}

func (a *App) setupLogging() {
    if a.cfg.LogFile != "" {
        f, err := os.OpenFile(a.cfg.LogFile, ...)
        a.logFile = f
        log.SetOutput(f)
    }
}

// In handleSignals shutdown:
if a.logFile != nil {
    a.logFile.Close()
}
```

---

## Recommended Fix Priority

### Phase 1 — Critical crash prevention (fix immediately)
1. **#3** Nil guard in `DialTargetWithRetries` (quick fix, prevents most panics)
2. **#1 + #2 + #9** Replace `Client()` with `Dial()` method (fixes root cause of races)
3. **#8** Remove `os.Exit(0)` from signal handler (prevents PID file corruption)

### Phase 2 — Resource management (fix before production scaling)
4. **#4** Add `CleanupExpiredTargets()` to health check cycle
5. **#6** Add connection semaphore
6. **#5** Fix `Reconnect()` lock scope

### Phase 3 — Robustness (fix when time permits)
7. **#7** Zombie process reaping for proxy commands
8. **#10** Implement `commandConn` deadlines
9. **#11**, **#12**, **#13**, **#14** — Low risk, fix opportunistically

---

## How to Verify Fixes

After applying fixes, run these checks:

```bash
# 1. Race detector (run with multiple connections)
go build -race ./cmd && ./ssh-roundrobin-linux-amd64 -fg &
for i in $(seq 1 50); do curl -x socks5://127.0.0.1:6465 https://example.com & done
wait

# 2. Memory profiling (run for extended period)
# Add import _ "net/http/pprof" temporarily
go tool pprof http://localhost:6060/debug/pprof/heap

# 3. Goroutine leak detection
go tool pprof http://localhost:6060/debug/pprof/goroutine

# 4. Stress test — rapid connect/disconnect
for i in $(seq 1 1000); do
  nc -w 1 127.0.0.1 6465 < /dev/null
done
# Check: no panics in log, memory stable, goroutine count stable
```
