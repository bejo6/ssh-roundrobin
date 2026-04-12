package sshroundrobin

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type AuthMethod int

const (
	AuthMethodKey AuthMethod = iota
	AuthMethodPassword
	AuthMethodProxyCommand
)

type SSHServer struct {
	Host         string
	Port         int
	Username     string
	AuthMethod   AuthMethod
	KeyPath      string
	Password     string
	ProxyCommand string
}

func (s *SSHServer) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

func (a AuthMethod) String() string {
	switch a {
	case AuthMethodKey:
		return "key"
	case AuthMethodPassword:
		return "password"
	case AuthMethodProxyCommand:
		return "proxycommand"
	default:
		return "unknown"
	}
}

type SSHClient struct {
	server  *SSHServer
	client  *ssh.Client
	mu      sync.Mutex
	healthy bool
	lastErr string

	selectedCount    uint64
	reconnectCount   uint64
	healthcheckCount uint64
	lastSelectedAt   int64
	lastCheckedAt    int64
}

type SSHClientStats struct {
	Addr             string
	Mode             string
	Healthy          bool
	SelectedCount    uint64
	ReconnectCount   uint64
	HealthcheckCount uint64
	LastSelectedAt   time.Time
	LastCheckedAt    time.Time
	LastError        string
}

type commandConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
	remote net.Addr
	mu     sync.Mutex
	closed bool
}

func (c *commandConn) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *commandConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *commandConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	_ = c.stdin.Close()
	_ = c.stdout.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}

	return nil
}

func (c *commandConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
}

func (c *commandConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *commandConn) SetDeadline(_ time.Time) error {
	return nil
}

func (c *commandConn) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c *commandConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

func NewSSHClient(server *SSHServer) (*SSHClient, error) {
	client, err := connect(server)
	if err != nil {
		return nil, err
	}

	return &SSHClient{
		server:  server,
		client:  client,
		healthy: true,
	}, nil
}

func NewSSHClientLazy(server *SSHServer) *SSHClient {
	return &SSHClient{
		server:  server,
		client:  nil,
		healthy: false,
		lastErr: "lazy: not connected yet",
	}
}

func connect(server *SSHServer) (*ssh.Client, error) {
	config, err := server.Config()
	if err != nil {
		return nil, err
	}

	if server.AuthMethod == AuthMethodProxyCommand {
		client, err := dialViaProxyCommand(server, config)
		if err == nil {
			return client, nil
		}

		fallbackClient, fallbackErr := ssh.Dial("tcp", server.Addr(), config)
		if fallbackErr != nil {
			return nil, fmt.Errorf("proxy command failed: %v; direct fallback failed: %w", err, fallbackErr)
		}

		return fallbackClient, nil
	}

	client, err := ssh.Dial("tcp", server.Addr(), config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func dialViaProxyCommand(server *SSHServer, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	proxyCommand := strings.TrimSpace(server.ProxyCommand)
	if proxyCommand == "" {
		return nil, fmt.Errorf("empty proxy command")
	}

	// Accept OpenSSH-style value like "ProxyCommand cloudflared access ssh --hostname %h"
	if strings.HasPrefix(strings.ToLower(proxyCommand), "proxycommand ") {
		proxyCommand = strings.TrimSpace(proxyCommand[len("ProxyCommand "):])
	}

	replaced := strings.ReplaceAll(proxyCommand, "%h", server.Host)
	replaced = strings.ReplaceAll(replaced, "%p", fmt.Sprintf("%d", server.Port))

	parts := strings.Fields(replaced)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid proxy command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start proxy command %q: %w", replaced, err)
	}

	conn := &commandConn{
		stdin:  stdin,
		stdout: stdout,
		cmd:    cmd,
		remote: &net.TCPAddr{IP: net.ParseIP(server.Host), Port: server.Port},
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, server.Addr(), cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy ssh handshake failed: %w", err)
	}

	return ssh.NewClient(clientConn, chans, reqs), nil
}

func (s *SSHServer) Config() (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	switch s.AuthMethod {
	case AuthMethodKey:
		keyData, err := os.ReadFile(s.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}

		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}

		authMethods = []ssh.AuthMethod{ssh.PublicKeys(signer)}

	case AuthMethodPassword:
		authMethods = []ssh.AuthMethod{ssh.Password(s.Password)}

	case AuthMethodProxyCommand:
		if s.KeyPath != "" {
			keyData, err := os.ReadFile(s.KeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read key file: %w", err)
			}

			signer, err := ssh.ParsePrivateKey(keyData)
			if err != nil {
				return nil, fmt.Errorf("failed to parse private key: %w", err)
			}

			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}

		if s.Password != "" {
			authMethods = append(authMethods, ssh.Password(s.Password))
		}

	default:
		return nil, fmt.Errorf("unknown auth method")
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no auth methods configured")
	}

	return &ssh.ClientConfig{
		User:            s.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}, nil
}

func (c *SSHClient) Client() *ssh.Client {
	return c.client
}

func (c *SSHClient) MarkSelected() uint64 {
	atomic.StoreInt64(&c.lastSelectedAt, time.Now().Unix())
	return atomic.AddUint64(&c.selectedCount, 1)
}

func (c *SSHClient) SelectionCount() uint64 {
	return atomic.LoadUint64(&c.selectedCount)
}

func (c *SSHClient) EnsureConnected() error {
	if c.IsConnected() {
		return nil
	}
	return c.Reconnect()
}

func (c *SSHClient) ServerAddr() string {
	if c.server == nil {
		return "unknown"
	}
	return c.server.Addr()
}

func (c *SSHClient) ServerMode() string {
	if c.server == nil {
		return "unknown"
	}
	return c.server.AuthMethod.String()
}

func (c *SSHClient) Close() error {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *SSHClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		c.healthy = false
		return false
	}
	return c.client.Conn != nil
}

func (c *SSHClient) IsHealthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.healthy
}

func (c *SSHClient) HealthCheck() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddUint64(&c.healthcheckCount, 1)
	atomic.StoreInt64(&c.lastCheckedAt, time.Now().Unix())

	if c.client == nil || c.client.Conn == nil {
		c.healthy = false
		c.lastErr = "ssh client not connected"
		return fmt.Errorf("ssh client not connected")
	}

	_, _, err := c.client.Conn.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		c.healthy = false
		c.lastErr = err.Error()
		_ = c.client.Close()
		c.client = nil
		return err
	}

	c.healthy = true
	c.lastErr = ""
	return nil
}

func (c *SSHClient) Stats() SSHClientStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := SSHClientStats{
		Healthy:          c.healthy,
		SelectedCount:    atomic.LoadUint64(&c.selectedCount),
		ReconnectCount:   atomic.LoadUint64(&c.reconnectCount),
		HealthcheckCount: atomic.LoadUint64(&c.healthcheckCount),
		LastError:        c.lastErr,
	}

	if c.server != nil {
		stats.Addr = c.server.Addr()
		stats.Mode = c.server.AuthMethod.String()
	}

	if ts := atomic.LoadInt64(&c.lastSelectedAt); ts > 0 {
		stats.LastSelectedAt = time.Unix(ts, 0)
	}
	if ts := atomic.LoadInt64(&c.lastCheckedAt); ts > 0 {
		stats.LastCheckedAt = time.Unix(ts, 0)
	}

	return stats
}

func (c *SSHClient) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Close()
	}

	client, err := connect(c.server)
	if err != nil {
		c.healthy = false
		c.lastErr = err.Error()
		return err
	}

	c.client = client
	c.healthy = true
	c.lastErr = ""
	atomic.AddUint64(&c.reconnectCount, 1)
	return nil
}
