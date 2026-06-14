package sshroundrobin

import (
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

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

	return ssh.Dial("tcp", server.Addr(), config)
}

func (c *SSHClient) Client() *ssh.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

func (c *SSHClient) Dial(network, addr string) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, fmt.Errorf("ssh client not connected to %s", c.ServerAddr())
	}
	return c.client.Dial(network, addr)
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
