# SSH Round-Robin Proxy 🚀

An SSH tunnel proxy with SOCKS5 support, TCP forwarding, dynamic load balancing, and failover capabilities.

> **Bahasa Indonesia**: lihat [`README-id.md`](README-id.md).

## ✨ Features

- **🎯 Selection Strategies**: Supports `failover` (auto-switch on failure) and `loadbalance` (traffic distribution).
- **🔄 Auto-Reconnect**: Automatically restores broken SSH connections.
- **🏥 Health Checks**: Periodic background probes to detect dead upstreams instantly.
- **📊 Runtime Stats**: Real-time hit counters, reconnect tracking, and health status in logs.
- **🔐 Flexible Auth**: Support for SSH Private Keys or Passwords.
- **☁️ Cloudflare Integration**: Native `ProxyCommand` support for Cloudflare Zero Trust.
- **🛠️ Easy Config**: Manage via flags, environment variables, or `.env` files.
- **🔌 Connection Limits**: Bounded concurrent connections to prevent resource exhaustion.
- **🖥️ Daemon Mode**: Run as background daemon with PID management.
- **📈 Status Tracking**: Persistent server health tracking across restarts.

## ⚡ Quick Start

```bash
# 1. Build the binary
make build

# 2. Configure upstreams
echo "your-server.com:22:password" > servers.txt

# 3. Run it (foreground mode)
./build/ssh-roundrobin -fg
```

## 📦 Installation

### Prerequisites
- Go 1.25 or later
- Make

### Build

```bash
# Build for current host OS/arch → build/ssh-roundrobin-<os>-<arch>
make build

# Build and run immediately
make run

# Build for all platforms (linux, darwin, freebsd, openbsd × all arches)
make all

# Install to $GOPATH/bin
make install
```

### Cleanup

```bash
# Remove all build artifacts
make clean
```

### Available Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build for current host OS/arch |
| `make run` | Build and run |
| `make install` | Install to `$GOPATH/bin` |
| `make all` | Cross-compile for all platforms |
| `make build-linux-amd64` | Build for Linux amd64 only |
| `make build-darwin-arm64` | Build for macOS arm64 only |
| `make clean` | Remove `build/` directory |

## ⚙️ Configuration

### `servers.txt` Format
Upstreams are defined one per line:
```text
host
host:port
host:port:password
```

### Environment Variables & Flags

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `BIND_ADDR` | `-bind` | `127.0.0.1:6465` | Local address to bind |
| `SERVERS_FILE` | `-servers` | `servers.txt` | Path to upstream list |
| `SSH_USER` | `-user` | `root` | SSH username |
| `KEY_FILE` | `-key` | - | Path to SSH private key |
| `SELECT_STRATEGY` | `-strategy` | `failover` | `failover` or `loadbalance` |
| `MODE` | `-mode` | `socks5` | `socks5` or `tcp-forward` |
| `TARGET_HOST` | `-target-host` | `127.0.0.1` | Target host (tcp-forward only) |
| `TARGET_PORT` | `-target-port` | `80` | Target port (tcp-forward only) |
| `HEALTH_CHECK` | `-health-check` | `true` | Enable periodic health probes |
| `RETRY_COUNT` | `-retry` | `3` | Global retry attempts |
| `TARGET_RETRY_UPSTREAMS`| `-target-retry-upstreams`| `0` | Max upstreams per request (0 = try all) |
| `TARGET_FAIL_THRESHOLD` | `-target-fail-threshold`| `1` | Failures before temporary block |
| `TARGET_FAIL_TTL` | `-target-fail-ttl` | `10m` | Block duration for bad upstreams |
| `SHOW_UPSTREAM_STATS`| `-show-upstream-stats` | `true` | Show stats summary in logs |
| `PROXY_COMMAND` | `-proxy-command` | - | Custom SSH ProxyCommand |
| `MAX_ACTIVE_UPSTREAMS` | `-max-active-upstreams` | `1` | Max concurrent active SSH connections |
| `MAX_CONNECTIONS` | `-max-connections` | `100` | Max concurrent client connections |

> See `.env.example` for the full list of advanced tuning parameters.

## 🖥️ Daemon Mode

By default, ssh-roundrobin runs as a background daemon:

```bash
# Start as daemon (default)
./build/ssh-roundrobin

# Run in foreground
./build/ssh-roundrobin -fg

# Check daemon status
./build/ssh-roundrobin -status

# Stop daemon
./build/ssh-roundrobin -stop
```

### Daemon Options

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `FOREGROUND` | `-fg` | `false` | Run in foreground (default: daemon) |
| `PID_FILE` | `-pid-file` | `ssh-roundrobin.pid` | PID file path |
| `LOG_FILE` | `-log-file` | - | Log file path (auto in daemon mode) |

## 📈 Server Status Tracking

Server health is tracked and persisted to survive restarts:

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `STATUS_FILE` | `-status-file` | `server_status.json` | Status persistence file |
| `STATUS_LOG` | `-status-log` | `true` | Log status changes |
| `STATUS_FLUSH_SEC` | `-status-flush-sec` | `30` | Seconds between status file flushes |

## 🚀 Usage Examples

### SOCKS5 Proxy (Default)
```bash
./build/ssh-roundrobin -mode socks5 -strategy loadbalance
```

### TCP Forwarding
```bash
./build/ssh-roundrobin -mode tcp-forward -target-host 1.1.1.1 -target-port 443
```

### Using Private Key
```bash
./build/ssh-roundrobin -key ~/.ssh/id_rsa -user admin
```

### Loadbalanced with Multiple Active Upstreams
```bash
./build/ssh-roundrobin -strategy loadbalance -max-active-upstreams 5
```

### With Cloudflare ProxyCommand
```bash
./build/ssh-roundrobin -proxy-command "cloudflared access ssh --hostname %h"
```

## 🏗️ Architecture

```
cmd/main.go              → Entry point (~80 lines)
internal/
├── app/                 → Application bootstrap and lifecycle
│   ├── app.go           → Accept loop, connection semaphore
│   ├── connect.go       → Server initialization
│   └── shutdown.go      → Signal handling, health checks
├── config/              → Configuration parsing
├── daemon/              → Daemon fork, PID management
├── proxy/               → SOCKS5, TCP forwarding, tunnel, dial
├── sshroundrobin/       → Round-robin selection, SSH client, health checks
└── status/              → Server health tracking and persistence
```

## 🧪 Testing

```bash
# Run all tests
make test
```

## 📄 License

Distributed under the MIT License.
