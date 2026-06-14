package config

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"ssh-roundrobin/internal/sshroundrobin"
)

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

		server, err := parseServerLine(line, lineNum, username, keyFile, proxyCommand, forceCloudflared)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading servers file: %w", err)
	}

	return servers, nil
}

func parseServerLine(line string, lineNum int, username, keyFile, proxyCommand string, forceCloudflared bool) (*sshroundrobin.SSHServer, error) {
	fields := strings.Split(line, ":")
	if len(fields) < 1 || len(fields) > 3 {
		return nil, fmt.Errorf("invalid line %d: %s (format: host, host:port, or host:port:password)", lineNum, line)
	}

	host := fields[0]
	if host == "" {
		return nil, fmt.Errorf("invalid line %d: empty host", lineNum)
	}

	port := 22
	if len(fields) >= 2 && fields[1] != "" {
		p, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid port at line %d: %s", lineNum, fields[1])
		}
		port = p
	}

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
			defaultKeyPath, err := defaultSSHKeyPath()
			if err != nil {
				return nil, err
			}
			if _, err := os.Stat(defaultKeyPath); err == nil {
				server.KeyPath = defaultKeyPath
			}
		}
	} else if server.KeyPath != "" {
		server.AuthMethod = sshroundrobin.AuthMethodKey
	} else if server.Password != "" {
		server.AuthMethod = sshroundrobin.AuthMethodPassword
	} else {
		defaultKeyPath, err := defaultSSHKeyPath()
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(defaultKeyPath); err == nil {
			server.AuthMethod = sshroundrobin.AuthMethodKey
			server.KeyPath = defaultKeyPath
		} else {
			return nil, fmt.Errorf("no auth method specified and default key %s not found for server %s at line %d", defaultKeyPath, host, lineNum)
		}
	}

	return server, nil
}

func defaultSSHKeyPath() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	return filepath.Join(currentUser.HomeDir, ".ssh", "id_rsa"), nil
}
