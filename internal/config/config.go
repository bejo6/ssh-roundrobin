package config

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ssh-roundrobin/internal/sshroundrobin"

	"github.com/joho/godotenv"
)

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		homeUser, err := user.Current()
		if err != nil {
			return "", err
		}
		return strings.Replace(path, "~", homeUser.HomeDir, 1), nil
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
	TargetHost     string
	TargetPort     int
	HealthCheck    bool
	HealthInterval time.Duration
	RetryCount     int
	RetryDelay     time.Duration
	Mode           string
	EnvFile        string
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

func getEnvBool(key string, defaultValue bool) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	if defaultValue {
		return "true"
	}
	return "false"
}

func ParseConfig() *Config {
	cfg := &Config{}
	cloudflaredDefault := getEnvBool("CLOUDFLARED", false) == "true"
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
	flag.StringVar(&cfg.TargetHost, "target-host", getEnv("TARGET_HOST", "127.0.0.1"), "Target host to forward to")
	flag.IntVar(&cfg.TargetPort, "target-port", getEnvInt("TARGET_PORT", 80), "Target port to forward to")
	flag.BoolVar(&cfg.HealthCheck, "health-check", getEnvBool("HEALTH_CHECK", true) == "true", "Enable health check")
	flag.DurationVar(&cfg.HealthInterval, "health-interval", 30*time.Second, "Health check interval")
	flag.IntVar(&cfg.RetryCount, "retry", getEnvInt("RETRY_COUNT", 3), "Number of retries")
	flag.DurationVar(&cfg.RetryDelay, "retry-delay", 1*time.Second, "Delay between retries")
	flag.StringVar(&cfg.Mode, "mode", getEnv("MODE", "socks5"), "Proxy mode: socks5 or tcp-forward")
	flag.StringVar(&cfg.EnvFile, "env-file", envFile, "Path to .env file")

	flag.Parse()
	cfg.Cloudflared = cloudflaredShort || cloudflaredLong

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

	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "socks5":
		cfg.Mode = "socks5"
	case "tcp-forward", "tcp", "forward", "static", "static-forward", "http":
		cfg.Mode = "tcp-forward"
	default:
		fmt.Fprintf(os.Stderr, "invalid mode %q: must be socks5 or tcp-forward\n", cfg.Mode)
		os.Exit(2)
	}

	cfg.TargetPort = getEnvInt("TARGET_PORT", cfg.TargetPort)

	// Expand ~ in paths
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

func ParseServersFile(path string, username, keyFile, proxyCommand string, forceCloudflared bool) ([]*sshroundrobin.SSHServer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open servers file: %w", err)
	}
	defer file.Close()

	if forceCloudflared && proxyCommand == "" {
		return nil, fmt.Errorf("-cf/-cloudflared mode requires PROXY_COMMAND or -proxy-command")
	}

	servers := make([]*sshroundrobin.SSHServer, 0)
	scanner := bufio.NewScanner(file)

	for lineNum := 1; scanner.Scan(); lineNum++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, ":")
		if len(fields) < 1 || len(fields) > 3 {
			return nil, fmt.Errorf("invalid line %d: %s (format: host, host:port, or host:port:password)", lineNum, line)
		}

		host := fields[0]
		if host == "" {
			return nil, fmt.Errorf("invalid line %d: empty host", lineNum)
		}

		// Default SSH port is 22
		port := 22
		if len(fields) >= 2 && fields[1] != "" {
			p, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("invalid port at line %d: %s", lineNum, fields[1])
			}
			port = p
		}

		// Password is only set if explicitly provided in format host:port:password
		password := ""
		if len(fields) == 3 {
			password = fields[2]
		}

		server := &sshroundrobin.SSHServer{
			Host:     host,
			Port:     port,
			Username: username,
			KeyPath:  keyFile,
		}

		if password != "" && password != "-" {
			server.Password = password
		}

		useProxyCommand := forceCloudflared || proxyCommand != ""
		if useProxyCommand {
			server.AuthMethod = sshroundrobin.AuthMethodProxyCommand
			server.ProxyCommand = proxyCommand

			if server.KeyPath == "" && server.Password == "" {
				currentUser, err := user.Current()
				if err != nil {
					return nil, fmt.Errorf("failed to get current user: %w", err)
				}
				defaultKeyPath := filepath.Join(currentUser.HomeDir, ".ssh", "id_rsa")
				if _, err := os.Stat(defaultKeyPath); err == nil {
					server.KeyPath = defaultKeyPath
				}
			}
		} else if server.KeyPath != "" {
			server.AuthMethod = sshroundrobin.AuthMethodKey
		} else if server.Password != "" {
			server.AuthMethod = sshroundrobin.AuthMethodPassword
		} else {
			// Default to ~/.ssh/id_rsa
			currentUser, err := user.Current()
			if err != nil {
				return nil, fmt.Errorf("failed to get current user: %w", err)
			}
			defaultKeyPath := filepath.Join(currentUser.HomeDir, ".ssh", "id_rsa")
			if _, err := os.Stat(defaultKeyPath); err == nil {
				server.AuthMethod = sshroundrobin.AuthMethodKey
				server.KeyPath = defaultKeyPath
			} else {
				return nil, fmt.Errorf("no auth method specified and default key %s not found for server %s at line %d", defaultKeyPath, host, lineNum)
			}
		}

		servers = append(servers, server)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading servers file: %w", err)
	}

	return servers, nil
}
