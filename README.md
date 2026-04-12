# SSH Round-Robin Proxy

SSH tunnel load balancer dengan round-robin failover support.

## Features

- **Round-robin**: Load balancing antar multiple SSH server
- **Auto-reconnect**: Otomatis reconnect saat server down
- **Multiple auth**: SSH private key atau password dari file
- **ProxyCommand**: Support Cloudflare Zero Trust
- **Config**: Via flag, environment variable, atau .env file

## Installation

```bash
make install
```

## Configuration

### servers.txt Format

```
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
| `TARGET_HOST` | `-target-host` | `127.0.0.1` | Target host |
| `TARGET_PORT` | `-target-port` | `80` | Target port |
| `PROXY_COMMAND` | `-proxy-command` | - | Proxy command |
| `RETRY_COUNT` | `-retry` | `3` | Retry count |
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

### Custom Target

```bash
TARGET_HOST=10.0.0.100 TARGET_PORT=3306 ./bin/ssh-roundrobin
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
