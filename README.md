# SSH Round-Robin Proxy

SSH tunnel proxy dengan dukungan SOCKS5, TCP forward, load balancing, dan failover.

## Features

- **Strategy-based selection**: `failover` untuk auto-switch saat mati, `loadbalance` untuk sebar traffic
- **Auto-reconnect**: Otomatis reconnect saat server down
- **Periodic health check**: Probe background untuk deteksi upstream mati lebih cepat
- **Per-upstream stats**: Hit counter, reconnect counter, dan status health di log runtime
- **Multiple auth**: SSH private key atau password dari file
- **ProxyCommand**: Support Cloudflare Zero Trust
- **Config**: Via flag, environment variable, atau `.env` file

## Installation

```bash
make install
```

## Configuration

### servers.txt Format

```
host
host:port
host:port:password
```

Contoh:
```
10.0.0.1:22:password123
10.0.0.2:22:password456
```

### Environment Variables / Flags

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `BIND_ADDR` | `-bind` | `127.0.0.1:2222` | Local bind address |
| `SERVERS_FILE` | `-servers` | `servers.txt` | Path ke servers.txt |
| `SSH_USER` | `-user` | `root` | SSH username |
| `KEY_FILE` | `-key` | - | SSH private key path |
| `SELECT_STRATEGY` | `-strategy` | `failover` | `failover` atau `loadbalance` |
| `TARGET_HOST` | `-target-host` | `127.0.0.1` | Target host untuk mode `tcp-forward` |
| `TARGET_PORT` | `-target-port` | `80` | Target port untuk mode `tcp-forward` |
| `MODE` | `-mode` | `socks5` | `socks5` atau `tcp-forward` |
| `PROXY_COMMAND` | `-proxy-command` | - | Proxy command |
| `RETRY_COUNT` | `-retry` | `3` | Retry count |
| `TARGET_RETRY_UPSTREAMS` | `-target-retry-upstreams` | `3` | Jumlah upstream maksimum per request target |
| `TARGET_FAIL_THRESHOLD` | `-target-fail-threshold` | `1` | Jumlah gagal sebelum upstream-target pair di-block sementara |
| `TARGET_FAIL_TTL` | `-target-fail-ttl` | `10m` | Durasi block upstream-target pair setelah threshold tercapai |
| `SHOW_UPSTREAM_STATS` | `-show-upstream-stats` | `true` | Tampilkan ringkasan stats upstream di log |
| `ENV_FILE` | `-env-file` | `.env` | Path ke .env file |

## Usage

### Password Auth

```bash
./bin/ssh-roundrobin -servers servers.txt -user root
```

### Private Key Auth

```bash
./bin/ssh-roundrobin -servers servers.txt -key ~/.ssh/id_rsa -user root
```

### Via .env

```bash
cp .env.example .env
# Edit .env
./bin/ssh-roundrobin
```

### Cloudflare Zero Trust

```bash
./bin/ssh-roundrobin -servers servers.txt -proxy-command "cloudflared access ssh --hostname %h" -user root
```

### SOCKS5 Proxy

```bash
./bin/ssh-roundrobin -mode socks5
```

### TCP Forward

```bash
./bin/ssh-roundrobin -mode tcp-forward -target-host 10.0.0.100 -target-port 3306
```

### Load Balancer Mode

```bash
./bin/ssh-roundrobin -strategy loadbalance
```

### Failover Mode

```bash
./bin/ssh-roundrobin -strategy failover
```

## Quick Start

```bash
# Clone & build
make build

# Create servers.txt
echo "your-server:22:password" > servers.txt

# Run
make run
```

## License

MIT
