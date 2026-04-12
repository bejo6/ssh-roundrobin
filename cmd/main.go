package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ssh-roundrobin/internal/config"
	"ssh-roundrobin/internal/sshroundrobin"
)

func tunnelBidirectional(left net.Conn, right net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(left, right)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(right, left)
	}()

	wg.Wait()
}

const (
	socksVersion5       = 0x05
	socksCmdConnect     = 0x01
	socksAtypIPv4       = 0x01
	socksAtypDomainName = 0x03
	socksAtypIPv6       = 0x04
	socksReplySucceeded = 0x00
	socksReplyGeneral   = 0x01
	socksReplyCmdUnsup  = 0x07
	socksReplyAddrUnsup = 0x08
)

func handleConnection(conn net.Conn, rr *sshroundrobin.RoundRobin, targetHost string, targetPort int) {
	defer conn.Close()

	client, err := rr.Get()
	if err != nil {
		log.Printf("Failed to get server: %v", err)
		return
	}
	log.Printf("Upstream selected %s (mode=%s hits=%d)", client.ServerAddr(), client.ServerMode(), client.SelectionCount())

	sshConn := client.Client()

	targetAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)
	targetConn, err := sshConn.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	log.Printf("Forwarding connection to %s", targetAddr)
	tunnelBidirectional(conn, targetConn)
}

func writeSocks5Reply(conn net.Conn, reply byte) {
	_, _ = conn.Write([]byte{socksVersion5, reply, 0x00, socksAtypIPv4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

func readSocks5Target(conn net.Conn) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", fmt.Errorf("failed to read socks5 request header: %w", err)
	}

	if header[0] != socksVersion5 {
		return "", fmt.Errorf("unsupported socks version: %d", header[0])
	}

	if header[1] != socksCmdConnect {
		writeSocks5Reply(conn, socksReplyCmdUnsup)
		return "", fmt.Errorf("unsupported socks command: %d", header[1])
	}

	var host string
	switch header[3] {
	case socksAtypIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", fmt.Errorf("failed to read ipv4 address: %w", err)
		}
		host = net.IP(addr).String()
	case socksAtypDomainName:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", fmt.Errorf("failed to read domain length: %w", err)
		}
		domain := make([]byte, int(lenBuf[0]))
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", fmt.Errorf("failed to read domain: %w", err)
		}
		host = string(domain)
	case socksAtypIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", fmt.Errorf("failed to read ipv6 address: %w", err)
		}
		host = net.IP(addr).String()
	default:
		writeSocks5Reply(conn, socksReplyAddrUnsup)
		return "", fmt.Errorf("unsupported address type: %d", header[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", fmt.Errorf("failed to read destination port: %w", err)
	}
	port := binary.BigEndian.Uint16(portBuf)

	return fmt.Sprintf("%s:%d", host, port), nil
}

func handleSocks5Connection(conn net.Conn, rr *sshroundrobin.RoundRobin) {
	defer conn.Close()

	client, err := rr.Get()
	if err != nil {
		log.Printf("Failed to get server: %v", err)
		return
	}
	log.Printf("Upstream selected %s (mode=%s hits=%d)", client.ServerAddr(), client.ServerMode(), client.SelectionCount())

	sshConn := client.Client()

	hello := make([]byte, 2)
	if _, err := io.ReadFull(conn, hello); err != nil {
		log.Printf("SOCKS5 handshake read failed: %v", err)
		return
	}

	if hello[0] != socksVersion5 {
		log.Printf("Unsupported SOCKS version: %d", hello[0])
		return
	}

	methodCount := int(hello[1])
	if methodCount == 0 {
		log.Printf("SOCKS5 method count is zero")
		return
	}

	methods := make([]byte, methodCount)
	if _, err := io.ReadFull(conn, methods); err != nil {
		log.Printf("SOCKS5 methods read failed: %v", err)
		return
	}

	// No authentication (0x00)
	if _, err := conn.Write([]byte{socksVersion5, 0x00}); err != nil {
		log.Printf("SOCKS5 method selection write failed: %v", err)
		return
	}

	targetAddr, err := readSocks5Target(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("SOCKS5 request parse failed: %v", err)
		}
		return
	}

	targetConn, err := sshConn.Dial("tcp", targetAddr)
	if err != nil {
		writeSocks5Reply(conn, socksReplyGeneral)
		log.Printf("Failed to connect target via SSH %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	writeSocks5Reply(conn, socksReplySucceeded)
	log.Printf("SOCKS5 forwarding to %s", targetAddr)
	tunnelBidirectional(conn, targetConn)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(os.Stderr)

	log.Println("Starting SSH Round-Robin Proxy...")

	cfg := config.ParseConfig()
	log.Printf("Config loaded - Bind: %s, Servers: %s, Strategy: %s, MaxActiveUpstreams: %d, Cloudflared force: %t, ProxyCommand set: %t", cfg.BindAddr, cfg.ServersFile, cfg.Strategy, cfg.MaxActiveUpstreams, cfg.Cloudflared, cfg.ProxyCommand != "")
	if cfg.Mode == "socks5" && (cfg.TargetHost != "127.0.0.1" || cfg.TargetPort != 80) {
		log.Printf("Ignoring target %s:%d because MODE=socks5 uses client-requested destinations", cfg.TargetHost, cfg.TargetPort)
	}

	servers, err := config.ParseServersFile(cfg.ServersFile, cfg.Username, cfg.KeyFile, cfg.ProxyCommand, cfg.Cloudflared)
	if err != nil {
		log.Fatalf("Failed to parse servers: %v", err)
	}

	if len(servers) == 0 {
		log.Fatal("No servers configured")
	}

	rr := sshroundrobin.NewRoundRobin(cfg.Strategy, cfg.MaxActiveUpstreams)

	eagerLimit := cfg.MaxActiveUpstreams
	if cfg.Strategy == sshroundrobin.StrategyFailover {
		eagerLimit = 1
	}
	connectedAtStartup := 0

	for _, server := range servers {
		if connectedAtStartup < eagerLimit {
			client, err := sshroundrobin.NewSSHClient(server)
			if err == nil {
				rr.Add(client)
				connectedAtStartup++
				log.Printf("Connected to %s (mode=%s)", server.Addr(), server.AuthMethod.String())
				continue
			}

			log.Printf("Startup connect failed for %s (mode=%s), queued as lazy upstream: %v", server.Addr(), server.AuthMethod.String(), err)
		}

		rr.Add(sshroundrobin.NewSSHClientLazy(server))
		log.Printf("Registered lazy upstream %s (mode=%s)", server.Addr(), server.AuthMethod.String())
	}

	if rr.Len() == 0 {
		log.Fatal("No servers available")
	}

	listener, err := net.Listen("tcp", cfg.BindAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.BindAddr, err)
	}
	defer listener.Close()

	if cfg.Mode == "socks5" {
		log.Printf("SSH Round-Robin SOCKS5 listening on %s", cfg.BindAddr)
	} else {
		targetAddr := fmt.Sprintf("%s:%d", cfg.TargetHost, cfg.TargetPort)
		log.Printf("SSH Round-Robin TCP forward listening on %s -> %s", cfg.BindAddr, targetAddr)
	}

	if cfg.HealthCheck {
		go func() {
			ticker := time.NewTicker(cfg.HealthInterval)
			defer ticker.Stop()

			for range ticker.C {
				report := rr.RunHealthChecks()
				if report.Checked == 0 {
					continue
				}

				if report.SwitchedFrom != "" || report.SwitchedTo != "" {
					log.Printf("Health check switch: from=%s to=%s", report.SwitchedFrom, report.SwitchedTo)
				}
				if len(report.Failed) > 0 {
					log.Printf("Health check failed upstreams: %v", report.Failed)
				}
				if len(report.Recovered) > 0 {
					log.Printf("Health check recovered upstreams: %v", report.Recovered)
				}
				log.Printf("Upstream stats: %s", rr.StatsSummary())
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		log.Printf("Final upstream stats: %s", rr.StatsSummary())
		rr.CloseAll()
		os.Exit(0)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		if cfg.Mode == "socks5" {
			go handleSocks5Connection(conn, rr)
		} else {
			go handleConnection(conn, rr, cfg.TargetHost, cfg.TargetPort)
		}
	}
}
