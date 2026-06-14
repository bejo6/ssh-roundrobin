# SSH Round-Robin Proxy 🚀

SSH tunnel proxy dengan dukungan SOCKS5, TCP forwarding, load balancing dinamis, dan fitur failover.

> **English Version**: see [`README.md`](README.md).

## ✨ Fitur Utama

- **🎯 Strategi Seleksi**: Mendukung `failover` (pindah otomatis saat mati) dan `loadbalance` (sebar trafik).
- **🔄 Auto-Reconnect**: Otomatis menyambung kembali koneksi SSH yang terputus.
- **🏥 Health Checks**: Probe background periodik untuk deteksi upstream mati lebih cepat.
- **📊 Statistik Runtime**: Counter hit, pelacakan reconnect, dan status kesehatan di log.
- **🔐 Autentikasi Fleksibel**: Mendukung SSH Private Key maupun Password.
- **☁️ Integrasi Cloudflare**: Dukungan `ProxyCommand` native untuk Cloudflare Zero Trust.
- **🛠️ Konfigurasi Mudah**: Atur via flag, environment variable, atau file `.env`.
- **🔌 Batasan Koneksi**: Semaphore koneksi untuk mencegah kehabisan resource.
- **🖥️ Mode Daemon**: Jalankan sebagai daemon background dengan manajemen PID.
- **📈 Pelacakan Status**: Status kesehatan server tersimpan dan persisten.

## ⚡ Quick Start

```bash
# 1. Build binary
make build

# 2. Konfigurasi upstream
echo "server-anda.com:22:password" > servers.txt

# 3. Jalankan (mode foreground)
./build/ssh-roundrobin -fg
```

## 📦 Instalasi

### Prasyarat
- Go 1.25 atau versi terbaru
- Make

### Build

```bash
# Build untuk OS/arch host saat ini → build/ssh-roundrobin-<os>-<arch>
make build

# Build dan jalankan langsung
make run

# Build untuk semua platform (linux, darwin, freebsd, openbsd × semua arch)
make all

# Install ke $GOPATH/bin
make install
```

### Cleanup

```bash
# Hapus semua artifact build
make clean
```

### Daftar Make Target

| Target | Deskripsi |
|--------|-----------|
| `make build` | Build untuk OS/arch host saat ini |
| `make run` | Build dan jalankan |
| `make install` | Install ke `$GOPATH/bin` |
| `make all` | Cross-compile untuk semua platform |
| `make build-linux-amd64` | Build untuk Linux amd64 saja |
| `make build-darwin-arm64` | Build untuk macOS arm64 saja |
| `make clean` | Hapus direktori `build/` |

## ⚙️ Konfigurasi

### Format `servers.txt`
Daftar server ditulis satu per baris:
```text
host
host:port
host:port:password
```

### Environment Variables & Flags

| Variable | Flag | Default | Deskripsi |
|----------|------|---------|-----------|
| `BIND_ADDR` | `-bind` | `127.0.0.1:6465` | Alamat bind lokal |
| `SERVERS_FILE` | `-servers` | `servers.txt` | Path ke daftar upstream |
| `SSH_USER` | `-user` | `root` | Username SSH |
| `KEY_FILE` | `-key` | - | Path ke SSH private key |
| `SELECT_STRATEGY` | `-strategy` | `failover` | `failover` atau `loadbalance` |
| `MODE` | `-mode` | `socks5` | `socks5` atau `tcp-forward` |
| `TARGET_HOST` | `-target-host` | `127.0.0.1` | Target host (mode tcp-forward) |
| `TARGET_PORT` | `-target-port` | `80` | Target port (mode tcp-forward) |
| `HEALTH_CHECK` | `-health-check` | `true` | Aktifkan pengecekan kesehatan berkala |
| `RETRY_COUNT` | `-retry` | `3` | Jumlah percobaan ulang global |
| `TARGET_RETRY_UPSTREAMS`| `-target-retry-upstreams`| `0` | Maksimal upstream per request (0 = coba semua) |
| `TARGET_FAIL_THRESHOLD` | `-target-fail-threshold`| `1` | Batas gagal sebelum blokir sementara |
| `TARGET_FAIL_TTL` | `-target-fail-ttl` | `10m` | Durasi blokir untuk upstream bermasalah |
| `SHOW_UPSTREAM_STATS`| `-show-upstream-stats` | `true` | Tampilkan ringkasan statistik di log |
| `PROXY_COMMAND` | `-proxy-command` | - | Custom SSH ProxyCommand |
| `MAX_ACTIVE_UPSTREAMS` | `-max-active-upstreams` | `1` | Maksimal koneksi SSH aktif bersamaan |
| `MAX_CONNECTIONS` | `-max-connections` | `100` | Maksimal koneksi client bersamaan |

> Lihat `.env.example` untuk daftar lengkap parameter tuning lainnya.

## 🖥️ Mode Daemon

Secara default, ssh-roundrobin berjalan sebagai daemon background:

```bash
# Mulai sebagai daemon (default)
./build/ssh-roundrobin

# Jalankan di foreground
./build/ssh-roundrobin -fg

# Cek status daemon
./build/ssh-roundrobin -status

# Hentikan daemon
./build/ssh-roundrobin -stop
```

### Opsi Daemon

| Variable | Flag | Default | Deskripsi |
|----------|------|---------|-----------|
| `FOREGROUND` | `-fg` | `false` | Jalankan di foreground (default: daemon) |
| `PID_FILE` | `-pid-file` | `ssh-roundrobin.pid` | Path file PID |
| `LOG_FILE` | `-log-file` | - | Path file log (otomatis di mode daemon) |

## 📈 Pelacakan Status Server

Status kesehatan server dilacak dan disimpan untuk bertahan restart:

| Variable | Flag | Default | Deskripsi |
|----------|------|---------|-----------|
| `STATUS_FILE` | `-status-file` | `server_status.json` | File penyimpanan status |
| `STATUS_LOG` | `-status-log` | `true` | Log perubahan status |
| `STATUS_FLUSH_SEC` | `-status-flush-sec` | `30` | Detik antara flush file status |

## 🚀 Contoh Penggunaan

### SOCKS5 Proxy (Default)
```bash
./build/ssh-roundrobin -mode socks5 -strategy loadbalance
```

### TCP Forwarding
```bash
./build/ssh-roundrobin -mode tcp-forward -target-host 1.1.1.1 -target-port 443
```

### Menggunakan Private Key
```bash
./build/ssh-roundrobin -key ~/.ssh/id_rsa -user admin
```

### Loadbalance dengan Banyak Upstream Aktif
```bash
./build/ssh-roundrobin -strategy loadbalance -max-active-upstreams 5
```

### Dengan Cloudflare ProxyCommand
```bash
./build/ssh-roundrobin -proxy-command "cloudflared access ssh --hostname %h"
```

## 🏗️ Arsitektur

```
cmd/main.go              → Entry point (~80 baris)
internal/
├── app/                 → Bootstrap aplikasi dan lifecycle
│   ├── app.go           → Accept loop, semaphore koneksi
│   ├── connect.go       → Inisialisasi server
│   └── shutdown.go      → Penanganan sinyal, health check
├── config/              → Parsing konfigurasi
├── daemon/              → Fork daemon, manajemen PID
├── proxy/               → SOCKS5, TCP forwarding, tunnel, dial
├── sshroundrobin/       → Seleksi round-robin, SSH client, health check
└── status/              → Pelacakan dan penyimpanan status server
```

## 🧪 Testing

```bash
# Jalankan semua test
make test
```

## 📄 Lisensi

Didistribusikan di bawah Lisensi MIT.
