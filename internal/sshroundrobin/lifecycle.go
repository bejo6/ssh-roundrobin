package sshroundrobin

import (
	"fmt"
	"sync/atomic"
	"time"
)

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

func (c *SSHClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthy = false
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
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
	oldClient := c.client
	c.client = nil
	c.healthy = false
	c.mu.Unlock()

	if oldClient != nil {
		_ = oldClient.Close()
	}

	client, err := connect(c.server)
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.mu.Unlock()
		return err
	}

	c.mu.Lock()
	c.client = client
	c.healthy = true
	c.lastErr = ""
	c.mu.Unlock()

	atomic.AddUint64(&c.reconnectCount, 1)
	return nil
}
