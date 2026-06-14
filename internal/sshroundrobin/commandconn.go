package sshroundrobin

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

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

func (c *commandConn) SetDeadline(t time.Time) error {
	if t.IsZero() {
		return nil
	}
	time.AfterFunc(time.Until(t), func() { c.Close() })
	return nil
}

func (c *commandConn) SetReadDeadline(t time.Time) error {
	if t.IsZero() {
		return nil
	}
	time.AfterFunc(time.Until(t), func() { c.Close() })
	return nil
}

func (c *commandConn) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		return nil
	}
	time.AfterFunc(time.Until(t), func() { c.Close() })
	return nil
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
