package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"ssh-roundrobin/internal/sshroundrobin"

	"github.com/joho/godotenv"
)

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		homeUser, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return strings.Replace(path, "~", homeUser, 1), nil
	}
	return path, nil
}

type Config struct {
	BindAddr       string
	ServersFile    string
	KeyFile        string
	Username       string
	ProxyCommand   string
	Cloudflared    bool
	Strategy       string
	MaxActiveUpstreams int
	MaxConnections int
	TargetHost     string
	TargetPort     int
	HealthCheck    bool
	ShowUpstreamStats bool
	HealthInterval time.Duration
	RetryCount     int
	RetryDelay     time.Duration
	TargetRetryUpstreams int
	TargetFailThreshold int
	TargetFailTTL time.Duration
	Mode           string
	EnvFile        string
	StatusFile     string
	StatusLog      bool
	StatusFlushSec int
	PIDFile        string
	LogFile        string
	Foreground     bool
	StopDaemon     bool
	StatusDaemon   bool
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return strings.EqualFold(value, "true")
	}
	return defaultValue
}

func ParseConfig() *Config {
	cfg := &Config{}
	cloudflaredDefault := getEnvBool("CLOUDFLARED", false)
	showStatsDefault := getEnvBool("SHOW_UPSTREAM_STATS", false)
	var cloudflaredShort bool
	var cloudflaredLong bool

	envFile := getEnv("ENV_FILE", ".env")
	if _, err := os.Stat(envFile); err == nil {
		godotenv.Load(envFile)
	}

	flag.StringVar(&cfg.BindAddr, "bind", getEnv("BIND_ADDR", "127.0.0.1:6465"), "Local bind address")
	flag.StringVar(&cfg.ServersFile, "servers", getEnv("SERVERS_FILE", "servers.txt"), "Path to servers list file")
	flag.StringVar(&cfg.KeyFile, "key", getEnv("KEY_FILE", ""), "SSH private key path")
	flag.StringVar(&cfg.Username, "user", getEnv("SSH_USER", "root"), "SSH username")
	flag.StringVar(&cfg.ProxyCommand, "proxy-command", getEnv("PROXY_COMMAND", ""), "Proxy command (e.g., for Cloudflare Zero Trust)")
	flag.BoolVar(&cloudflaredShort, "cf", cloudflaredDefault, "Force Cloudflare proxy command mode")
	flag.BoolVar(&cloudflaredLong, "cloudflared", cloudflaredDefault, "Force Cloudflare proxy command mode")
	flag.StringVar(&cfg.Strategy, "strategy", getEnv("SELECT_STRATEGY", sshroundrobin.StrategyFailover), "Server selection strategy: failover or loadbalance")
	flag.IntVar(&cfg.MaxActiveUpstreams, "max-active-upstreams", getEnvInt("MAX_ACTIVE_UPSTREAMS", 2), "Max simultaneously connected upstreams in loadbalance mode")
	flag.IntVar(&cfg.MaxConnections, "max-connections", getEnvInt("MAX_CONNECTIONS", 100), "Max concurrent connections (0 = unlimited)")
	flag.StringVar(&cfg.TargetHost, "target-host", getEnv("TARGET_HOST", "127.0.0.1"), "Target host to forward to")
	flag.IntVar(&cfg.TargetPort, "target-port", getEnvInt("TARGET_PORT", 80), "Target port to forward to")
	flag.BoolVar(&cfg.HealthCheck, "health-check", getEnvBool("HEALTH_CHECK", true), "Enable health check")
	flag.BoolVar(&cfg.ShowUpstreamStats, "upstream-stats", showStatsDefault, "Show periodic and final upstream stats")
	flag.DurationVar(&cfg.HealthInterval, "health-interval", 30*time.Second, "Health check interval")
	flag.IntVar(&cfg.RetryCount, "retry", getEnvInt("RETRY_COUNT", 3), "Number of retries")
	flag.DurationVar(&cfg.RetryDelay, "retry-delay", 1*time.Second, "Delay between retries")
	flag.IntVar(&cfg.TargetRetryUpstreams, "target-retry-upstreams", getEnvInt("TARGET_RETRY_UPSTREAMS", 0), "Max upstreams to try per target request (0 = try all available)")
	flag.IntVar(&cfg.TargetFailThreshold, "target-fail-threshold", getEnvInt("TARGET_FAIL_THRESHOLD", 1), "Failure count before upstream-target pair is temporarily blocked")
	flag.DurationVar(&cfg.TargetFailTTL, "target-fail-ttl", 10*time.Minute, "Block duration for upstream-target pair after repeated failures")
	flag.StringVar(&cfg.Mode, "mode", getEnv("MODE", "socks5"), "Proxy mode: socks5 or tcp-forward")
	flag.StringVar(&cfg.EnvFile, "env-file", envFile, "Path to .env file")
	flag.StringVar(&cfg.StatusFile, "status-file", getEnv("STATUS_FILE", "server_status.json"), "Path to server status JSON file")
	flag.BoolVar(&cfg.StatusLog, "status-log", getEnvBool("STATUS_LOG", true), "Log server status changes")
	flag.IntVar(&cfg.StatusFlushSec, "status-flush-sec", getEnvInt("STATUS_FLUSH_SEC", 30), "Seconds between status file flushes")
	flag.StringVar(&cfg.PIDFile, "pid-file", getEnv("PID_FILE", "ssh-roundrobin.pid"), "PID file path")
	flag.StringVar(&cfg.LogFile, "log-file", getEnv("LOG_FILE", ""), "Log file path (empty = stderr)")
	flag.BoolVar(&cfg.Foreground, "fg", getEnvBool("FOREGROUND", false), "Run in foreground (default: daemon/background)")
	flag.BoolVar(&cfg.StopDaemon, "stop", false, "Stop running daemon")
	flag.BoolVar(&cfg.StatusDaemon, "status", false, "Check daemon status")

	flag.Parse()
	cfg.Cloudflared = cloudflaredShort || cloudflaredLong

	validateStrategy(cfg)
	validateMode(cfg)

	if cfg.MaxActiveUpstreams <= 0 {
		cfg.MaxActiveUpstreams = 1
	}
	if cfg.TargetRetryUpstreams < 0 {
		cfg.TargetRetryUpstreams = 0
	}
	if cfg.TargetFailThreshold <= 0 {
		cfg.TargetFailThreshold = 1
	}
	if cfg.TargetFailTTL <= 0 {
		cfg.TargetFailTTL = 10 * time.Minute
	}

	cfg.TargetPort = getEnvInt("TARGET_PORT", cfg.TargetPort)

	if cfg.KeyFile != "" {
		expanded, err := expandPath(cfg.KeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to expand KeyFile path: %v\n", err)
		} else {
			cfg.KeyFile = expanded
		}
	}

	return cfg
}

func validateStrategy(cfg *Config) {
	switch strings.ToLower(strings.TrimSpace(cfg.Strategy)) {
	case "":
		cfg.Strategy = sshroundrobin.StrategyFailover
	case sshroundrobin.StrategyLoadBalance:
		cfg.Strategy = sshroundrobin.StrategyLoadBalance
	case sshroundrobin.StrategyFailover:
		cfg.Strategy = sshroundrobin.StrategyFailover
	default:
		fmt.Fprintf(os.Stderr, "invalid strategy %q: must be %s or %s\n", cfg.Strategy, sshroundrobin.StrategyLoadBalance, sshroundrobin.StrategyFailover)
		os.Exit(2)
	}
}

func validateMode(cfg *Config) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "socks5":
		cfg.Mode = "socks5"
	case "tcp-forward", "tcp", "forward", "static", "static-forward", "http":
		cfg.Mode = "tcp-forward"
	default:
		fmt.Fprintf(os.Stderr, "invalid mode %q: must be socks5 or tcp-forward\n", cfg.Mode)
		os.Exit(2)
	}
}
