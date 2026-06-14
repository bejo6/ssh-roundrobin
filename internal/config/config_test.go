package config

import (
	"os"
	"path/filepath"
	"testing"

	"ssh-roundrobin/internal/sshroundrobin"
)

func TestExpandPath_NoTilde(t *testing.T) {
	got, err := expandPath("/absolute/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %s", got)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	got, err := expandPath("~/some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "~/some/path" {
		t.Error("tilde was not expanded")
	}
	if got == "" {
		t.Error("expanded path is empty")
	}
}

func TestGetEnv(t *testing.T) {
	t.Setenv("TEST_GETENV_KEY", "testvalue")
	if got := getEnv("TEST_GETENV_KEY", "default"); got != "testvalue" {
		t.Errorf("getEnv with set var = %q, want testvalue", got)
	}
}

func TestGetEnv_Default(t *testing.T) {
	os.Unsetenv("TEST_GETENV_MISSING_KEY")
	if got := getEnv("TEST_GETENV_MISSING_KEY", "default"); got != "default" {
		t.Errorf("getEnv default = %q, want default", got)
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Setenv("TEST_GETENVINT_KEY", "42")
	if got := getEnvInt("TEST_GETENVINT_KEY", 0); got != 42 {
		t.Errorf("getEnvInt = %d, want 42", got)
	}
}

func TestGetEnvInt_Default(t *testing.T) {
	os.Unsetenv("TEST_GETENVINT_MISSING")
	if got := getEnvInt("TEST_GETENVINT_MISSING", 7); got != 7 {
		t.Errorf("getEnvInt default = %d, want 7", got)
	}
}

func TestGetEnvInt_Invalid(t *testing.T) {
	t.Setenv("TEST_GETENVINT_BAD", "notanumber")
	if got := getEnvInt("TEST_GETENVINT_BAD", 99); got != 99 {
		t.Errorf("getEnvInt invalid = %d, want 99", got)
	}
}

func TestGetEnvBool_True(t *testing.T) {
	t.Setenv("TEST_GETENVBOOL_KEY", "true")
	if got := getEnvBool("TEST_GETENVBOOL_KEY", false); got != true {
		t.Errorf("getEnvBool(true) = %v, want true", got)
	}
}

func TestGetEnvBool_DefaultTrue(t *testing.T) {
	os.Unsetenv("TEST_GETENVBOOL_MISSING")
	if got := getEnvBool("TEST_GETENVBOOL_MISSING", true); got != true {
		t.Errorf("getEnvBool default true = %v, want true", got)
	}
}

func TestGetEnvBool_DefaultFalse(t *testing.T) {
	os.Unsetenv("TEST_GETENVBOOL_MISSING2")
	if got := getEnvBool("TEST_GETENVBOOL_MISSING2", false); got != false {
		t.Errorf("getEnvBool default false = %v, want false", got)
	}
}

func TestGetEnvBool_False(t *testing.T) {
	t.Setenv("TEST_GETENVBOOL_FALSE", "false")
	if got := getEnvBool("TEST_GETENVBOOL_FALSE", true); got != false {
		t.Errorf("getEnvBool('false') = %v, want false", got)
	}
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestParseServersFile_HostOnly(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "192.168.1.1\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Host != "192.168.1.1" {
		t.Errorf("host = %q", servers[0].Host)
	}
	if servers[0].Port != 22 {
		t.Errorf("port = %d, want 22", servers[0].Port)
	}
	if servers[0].Username != "root" {
		t.Errorf("username = %q", servers[0].Username)
	}
	if servers[0].AuthMethod != sshroundrobin.AuthMethodKey {
		t.Errorf("auth method = %v, want key", servers[0].AuthMethod)
	}
}

func TestParseServersFile_HostPort(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1:2222\n")

	servers, err := ParseServersFile(serversFile, "admin", keyFile, "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if servers[0].Host != "10.0.0.1" {
		t.Errorf("host = %q", servers[0].Host)
	}
	if servers[0].Port != 2222 {
		t.Errorf("port = %d, want 2222", servers[0].Port)
	}
}

func TestParseServersFile_HostPortPassword(t *testing.T) {
	dir := t.TempDir()
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1:22:mypass\n")

	servers, err := ParseServersFile(serversFile, "root", "", "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if servers[0].Password != "mypass" {
		t.Errorf("password = %q", servers[0].Password)
	}
	if servers[0].AuthMethod != sshroundrobin.AuthMethodPassword {
		t.Errorf("auth = %v, want password", servers[0].AuthMethod)
	}
}

func TestParseServersFile_DashPassword(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1:22:-\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if servers[0].Password != "" {
		t.Errorf("dash password should be empty, got %q", servers[0].Password)
	}
}

func TestParseServersFile_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "# comment\n\n10.0.0.1\n# another comment\n10.0.0.2\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestParseServersFile_MultipleServers(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1\n10.0.0.2:2222\n10.0.0.3:22:pass\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if len(servers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(servers))
	}
}

func TestParseServersFile_CloudflaredMode(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "cloudflared access ssh --hostname %h", true)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if servers[0].AuthMethod != sshroundrobin.AuthMethodProxyCommand {
		t.Errorf("auth = %v, want proxycommand", servers[0].AuthMethod)
	}
	if servers[0].ProxyCommand != "cloudflared access ssh --hostname %h" {
		t.Errorf("proxyCommand = %q", servers[0].ProxyCommand)
	}
}

func TestParseServersFile_CloudflaredNoProxyCommand(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1\n")

	_, err := ParseServersFile(serversFile, "root", keyFile, "", true)
	if err == nil {
		t.Error("expected error for cloudflared mode without proxy command")
	}
}

func TestParseServersFile_ProxyCommandWithoutCloudflared(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeTempFile(t, dir, "id_rsa", "fake-key")
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1\n")

	servers, err := ParseServersFile(serversFile, "root", keyFile, "some-proxy %h %p", false)
	if err != nil {
		t.Fatalf("ParseServersFile failed: %v", err)
	}
	if servers[0].AuthMethod != sshroundrobin.AuthMethodProxyCommand {
		t.Errorf("auth = %v, want proxycommand", servers[0].AuthMethod)
	}
}

func TestParseServersFile_NonExistent(t *testing.T) {
	_, err := ParseServersFile("/tmp/nonexistent_servers_file_12345.txt", "root", "", "", false)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseServersFile_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1:abc\n")

	_, err := ParseServersFile(serversFile, "root", "", "", false)
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestParseServersFile_EmptyHost(t *testing.T) {
	dir := t.TempDir()
	serversFile := writeTempFile(t, dir, "servers.txt", ":22\n")

	_, err := ParseServersFile(serversFile, "root", "", "", false)
	if err == nil {
		t.Error("expected error for empty host")
	}
}

func TestParseServersFile_TooManyFields(t *testing.T) {
	dir := t.TempDir()
	serversFile := writeTempFile(t, dir, "servers.txt", "10.0.0.1:22:pass:extra\n")

	_, err := ParseServersFile(serversFile, "root", "", "", false)
	if err == nil {
		t.Error("expected error for too many fields")
	}
}
