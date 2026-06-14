package sshroundrobin

import (
	"testing"
	"time"
)

func TestNewSSHClientLazy(t *testing.T) {
	server := &SSHServer{
		Host:       "10.0.0.1",
		Port:       22,
		Username:   "testuser",
		AuthMethod: AuthMethodKey,
	}
	client := NewSSHClientLazy(server)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.IsConnected() {
		t.Error("lazy client should not be connected")
	}
	if client.IsHealthy() {
		t.Error("lazy client should not be healthy")
	}
}

func TestSSHClient_ServerAddr(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "example.com",
		Port: 2222,
	})
	if addr := client.ServerAddr(); addr != "example.com:2222" {
		t.Errorf("ServerAddr = %q, want example.com:2222", addr)
	}
}

func TestSSHClient_ServerAddr_NilServer(t *testing.T) {
	client := &SSHClient{server: nil}
	if addr := client.ServerAddr(); addr != "unknown" {
		t.Errorf("ServerAddr = %q, want unknown", addr)
	}
}

func TestSSHClient_ServerMode(t *testing.T) {
	tests := []struct {
		method AuthMethod
		want   string
	}{
		{AuthMethodKey, "key"},
		{AuthMethodPassword, "password"},
		{AuthMethodProxyCommand, "proxycommand"},
	}
	for _, tt := range tests {
		client := NewSSHClientLazy(&SSHServer{
			Host:       "h",
			Port:       22,
			AuthMethod: tt.method,
		})
		if got := client.ServerMode(); got != tt.want {
			t.Errorf("AuthMethod %d: ServerMode = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestSSHClient_ServerMode_NilServer(t *testing.T) {
	client := &SSHClient{server: nil}
	if mode := client.ServerMode(); mode != "unknown" {
		t.Errorf("ServerMode = %q, want unknown", mode)
	}
}

func TestSSHClient_IsConnected_NilClient(t *testing.T) {
	client := &SSHClient{client: nil}
	if client.IsConnected() {
		t.Error("should not be connected with nil ssh client")
	}
}

func TestSSHClient_IsHealthy_Lazy(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "h", Port: 22, AuthMethod: AuthMethodKey,
	})
	if client.IsHealthy() {
		t.Error("lazy client should not be healthy")
	}
}

func TestSSHClient_Close_NilSSHClient(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "h", Port: 22, AuthMethod: AuthMethodKey,
	})
	if err := client.Close(); err != nil {
		t.Errorf("Close on lazy client should not error, got: %v", err)
	}
	if client.IsHealthy() {
		t.Error("should not be healthy after close")
	}
}

func TestSSHClient_MarkSelected(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "h", Port: 22, AuthMethod: AuthMethodKey,
	})
	if client.SelectionCount() != 0 {
		t.Errorf("initial SelectionCount = %d, want 0", client.SelectionCount())
	}

	n := client.MarkSelected()
	if n != 1 {
		t.Errorf("MarkSelected returned %d, want 1", n)
	}
	if client.SelectionCount() != 1 {
		t.Errorf("SelectionCount = %d, want 1", client.SelectionCount())
	}

	client.MarkSelected()
	if client.SelectionCount() != 2 {
		t.Errorf("SelectionCount = %d, want 2", client.SelectionCount())
	}
}

func TestSSHClient_Stats_Lazy(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "10.0.0.1",
		Port: 22,
	})

	client.MarkSelected()
	client.MarkSelected()

	stats := client.Stats()
	if stats.Addr != "10.0.0.1:22" {
		t.Errorf("Stats.Addr = %q, want 10.0.0.1:22", stats.Addr)
	}
	if stats.SelectedCount != 2 {
		t.Errorf("Stats.SelectedCount = %d, want 2", stats.SelectedCount)
	}
	if stats.Healthy {
		t.Error("lazy client Stats.Healthy should be false")
	}
	if stats.LastError != "lazy: not connected yet" {
		t.Errorf("Stats.LastError = %q", stats.LastError)
	}
	if stats.LastSelectedAt.IsZero() {
		t.Error("Stats.LastSelectedAt should be set after selection")
	}
}

func TestSSHClient_Stats_NilServer(t *testing.T) {
	client := &SSHClient{server: nil}
	stats := client.Stats()
	if stats.Addr != "" {
		t.Errorf("expected empty addr for nil server, got %q", stats.Addr)
	}
	if stats.Mode != "" {
		t.Errorf("expected empty mode for nil server, got %q", stats.Mode)
	}
}

func TestSSHClient_EnsureConnected_Lazy(t *testing.T) {
	// Use a local address that's unlikely to have an SSH server
	// with a short dial timeout to keep the test fast
	client := NewSSHClientLazy(&SSHServer{
		Host:       "127.0.0.1",
		Port:       1, // unlikely to have a service
		AuthMethod: AuthMethodPassword,
		Password:   "test",
	})
	// Should fail because no real SSH server on port 1
	err := client.EnsureConnected()
	if err == nil {
		t.Error("expected error connecting to non-existent SSH server")
	}
}

func TestSSHClient_HealthCheck_NotConnected(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "h", Port: 22, AuthMethod: AuthMethodKey,
	})
	err := client.HealthCheck()
	if err == nil {
		t.Error("expected error for health check on disconnected client")
	}
	if client.IsHealthy() {
		t.Error("should not be healthy after failed health check")
	}
}

func TestSSHServer_Addr(t *testing.T) {
	s := &SSHServer{Host: "myhost", Port: 2222}
	if addr := s.Addr(); addr != "myhost:2222" {
		t.Errorf("Addr = %q, want myhost:2222", addr)
	}
}

func TestAuthMethod_String(t *testing.T) {
	tests := []struct {
		m    AuthMethod
		want string
	}{
		{AuthMethodKey, "key"},
		{AuthMethodPassword, "password"},
		{AuthMethodProxyCommand, "proxycommand"},
		{AuthMethod(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("AuthMethod(%d).String() = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestSSHClient_Stats_Timestamps(t *testing.T) {
	client := NewSSHClientLazy(&SSHServer{
		Host: "h", Port: 22, AuthMethod: AuthMethodKey,
	})

	client.MarkSelected()
	before := time.Now().Add(-1 * time.Second)

	stats := client.Stats()
	if stats.LastSelectedAt.Before(before) {
		t.Error("LastSelectedAt should be recent")
	}
}
