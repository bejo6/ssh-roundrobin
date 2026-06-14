package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

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

// HandleSocks5Connection handles a single SOCKS5 proxy connection,
// performing the handshake, resolving the target, and tunneling data.
func HandleSocks5Connection(conn net.Conn, rr *sshroundrobin.RoundRobin,
	retryUpstreams int, failThreshold int, failTTL time.Duration,
	tracker *status.ServerStatusTracker) {

	defer conn.Close()

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

	targetConn, client, err := DialTargetWithRetries(rr, targetAddr, retryUpstreams, failThreshold, failTTL, tracker)
	if err != nil {
		writeSocks5Reply(conn, socksReplyGeneral)
		log.Printf("Failed to connect target via SSH %s after retries: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	writeSocks5Reply(conn, socksReplySucceeded)
	log.Printf("SOCKS5 forwarding to %s", targetAddr)
	start := time.Now()
	rightToLeft, leftToRight := TunnelBidirectional(conn, targetConn)

	if IsLikelyTargetBlocked(start, rightToLeft, leftToRight) {
		err = fmt.Errorf("tunnel ended too quickly without upstream response")
		rr.ReportTargetFailure(client, targetAddr, failThreshold, failTTL, err)
		log.Printf("Marked upstream-target as failed after short tunnel: upstream=%s target=%s c2u=%d u2c=%d elapsed=%s", client.ServerAddr(), targetAddr, leftToRight, rightToLeft, time.Since(start).Round(time.Millisecond))
	} else {
		rr.ReportTargetSuccess(client, targetAddr)
	}
}
