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

## ⚡ Quick Start

```bash
# 1. Build the binary
make build

# 2. Configure upstreams
echo "your-server.com:22:password" > servers.txt

# 3. Run it
make run
```

## 📦 Installation

### Prerequisites
- Go 1.25 or later
- Make (optional, for shortcuts)

### Build Options
- **Local Build**: `make build` (outputs to `build/ssh-roundrobin-<os>-<arch>`)
- **Run Directly**: `make run`
- **Install to GOBIN**: `make install`
- **Multi-Platform Build**: `make all`

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
| `TARGET_RETRY_UPSTREAMS`| `-target-retry-upstreams`| `3` | Max upstreams per request |
| `TARGET_FAIL_THRESHOLD` | `-target-fail-threshold`| `1` | Failures before temporary block |
| `TARGET_FAIL_TTL` | `-target-fail-ttl` | `10m` | Block duration for bad upstreams |
| `SHOW_UPSTREAM_STATS`| `-show-upstream-stats` | `true` | Show stats summary in logs |
| `PROXY_COMMAND` | `-proxy-command` | - | Custom SSH ProxyCommand |

> See `.env.example` for the full list of advanced tuning parameters.

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

## 📄 License

Distributed under the MIT License.

---
Crafted with focus and caffeine by ⚡ **GitHub Copilot**.
