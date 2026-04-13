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

## ⚡ Quick Start

```bash
# 1. Build binary
make build

# 2. Konfigurasi upstream
echo "server-anda.com:22:password" > servers.txt

# 3. Jalankan
make run
```

## 📦 Instalasi

### Prasyarat
- Go 1.25 atau versi terbaru
- Make (opsional, untuk shortcut)

### Opsi Build
- **Build Lokal**: `make build` (hasil di `build/ssh-roundrobin-<os>-<arch>`)
- **Jalankan Langsung**: `make run`
- **Install ke GOBIN**: `make install`
- **Build Multi-Platform**: `make all`

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
| `TARGET_RETRY_UPSTREAMS`| `-target-retry-upstreams`| `3` | Maksimal upstream per request |
| `TARGET_FAIL_THRESHOLD` | `-target-fail-threshold`| `1` | Batas gagal sebelum blokir sementara |
| `TARGET_FAIL_TTL` | `-target-fail-ttl` | `10m` | Durasi blokir untuk upstream bermasalah |
| `SHOW_UPSTREAM_STATS`| `-show-upstream-stats` | `true` | Tampilkan ringkasan statistik di log |
| `PROXY_COMMAND` | `-proxy-command` | - | Custom SSH ProxyCommand |

> Lihat `.env.example` untuk daftar lengkap parameter tuning lainnya.

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

## 📄 Lisensi

Didistribusikan di bawah Lisensi MIT.

---
Crafted with focus and caffeine by ⚡ **GitHub Copilot**.
