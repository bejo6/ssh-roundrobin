package sshroundrobin

import (
	"fmt"
	"os"
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

func (s *SSHServer) Config() (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	switch s.AuthMethod {
	case AuthMethodKey:
		signer, err := loadPrivateKey(s.KeyPath)
		if err != nil {
			return nil, err
		}
		authMethods = []ssh.AuthMethod{ssh.PublicKeys(signer)}

	case AuthMethodPassword:
		authMethods = []ssh.AuthMethod{ssh.Password(s.Password)}

	case AuthMethodProxyCommand:
		if s.KeyPath != "" {
			signer, err := loadPrivateKey(s.KeyPath)
			if err != nil {
				return nil, err
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

func loadPrivateKey(path string) (ssh.Signer, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	return signer, nil
}
