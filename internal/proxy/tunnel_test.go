package proxy

import (
	"io"
	"net"
	"testing"
	"time"
)

// discardConn wraps a net.Conn so writes don't block (go to io.Discard),
// while reads still come from the underlying connection.
type discardConn struct {
	net.Conn
}

func (d *discardConn) Write(p []byte) (int, error) {
	return io.Discard.Write(p)
}

func TestTunnelBidirectional(t *testing.T) {
	left, right := net.Pipe()

	// Close both sides immediately - TunnelBidirectional should return
	left.Close()
	right.Close()

	TunnelBidirectional(left, right)
}

func TestTunnelBidirectional_TransfersData(t *testing.T) {
	lLeft, lRight := net.Pipe()
	rLeft, rRight := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		TunnelBidirectional(lRight, rRight)
	}()

	// Send data from left
	lLeft.Write([]byte("hello"))
	lLeft.Close()

	// Read from right what was sent
	buf := make([]byte, 64)
	rLeft.Read(buf)
	rLeft.Close()

	<-done
}

func TestIsLikelyTargetBlocked_Blocked(t *testing.T) {
	start := time.Now().Add(-500 * time.Millisecond)
	// Client sent data but received nothing back within 2s
	blocked := IsLikelyTargetBlocked(start, 0, 100)
	if !blocked {
		t.Error("expected blocked: sent data, got nothing, under 2s")
	}
}

func TestIsLikelyTargetBlocked_NotBlocked_ResponseReceived(t *testing.T) {
	start := time.Now().Add(-500 * time.Millisecond)
	// Got data back
	blocked := IsLikelyTargetBlocked(start, 100, 100)
	if blocked {
		t.Error("should not be blocked when upstream responded")
	}
}

func TestIsLikelyTargetBlocked_NotBlocked_NothingSent(t *testing.T) {
	start := time.Now().Add(-500 * time.Millisecond)
	// Client sent nothing
	blocked := IsLikelyTargetBlocked(start, 0, 0)
	if blocked {
		t.Error("should not be blocked when nothing was sent")
	}
}

func TestIsLikelyTargetBlocked_NotBlocked_SlowConnection(t *testing.T) {
	start := time.Now().Add(-5 * time.Second)
	// Over 2s threshold
	blocked := IsLikelyTargetBlocked(start, 0, 100)
	if blocked {
		t.Error("should not be blocked when elapsed > 2s")
	}
}

func TestReadSocks5Target_IPv4(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// VER CMD RSV ATYP IPv4(4bytes) PORT(2bytes)
		req := []byte{
			0x05, 0x01, 0x00, // VER CONNECT RSV
			socksAtypIPv4,         // IPv4
			192, 168, 1, 1,        // 192.168.1.1
			0x00, 0x50,            // port 80
		}
		client.Write(req)
	}()

	target, err := readSocks5Target(server)
	if err != nil {
		t.Fatalf("readSocks5Target failed: %v", err)
	}
	if target != "192.168.1.1:80" {
		t.Errorf("expected 192.168.1.1:80, got %s", target)
	}
}

func TestReadSocks5Target_Domain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		domain := "example.com"
		req := []byte{
			0x05, 0x01, 0x00,
			socksAtypDomainName,
			byte(len(domain)),
		}
		req = append(req, []byte(domain)...)
		req = append(req, 0x01, 0xBB) // port 443
		client.Write(req)
	}()

	target, err := readSocks5Target(server)
	if err != nil {
		t.Fatalf("readSocks5Target failed: %v", err)
	}
	if target != "example.com:443" {
		t.Errorf("expected example.com:443, got %s", target)
	}
}

func TestReadSocks5Target_IPv6(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		req := []byte{
			0x05, 0x01, 0x00,
			socksAtypIPv6,
		}
		// ::1 (loopback)
		req = append(req, make([]byte, 15)...)
		req = append(req, 0x01)
		req = append(req, 0x00, 0x50) // port 80
		client.Write(req)
	}()

	target, err := readSocks5Target(server)
	if err != nil {
		t.Fatalf("readSocks5Target failed: %v", err)
	}
	if target != "::1:80" {
		t.Errorf("expected ::1:80, got %s", target)
	}
}

func TestReadSocks5Target_BadVersion(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		req := []byte{0x04, 0x01, 0x00, socksAtypIPv4, 127, 0, 0, 1, 0x00, 0x50}
		client.Write(req)
		// Drain reply (may or may not be sent for bad version)
		buf := make([]byte, 10)
		client.Read(buf)
	}()

	_, err := readSocks5Target(server)
	if err == nil {
		t.Error("expected error for wrong SOCKS version")
	}
}

func TestReadSocks5Target_UnsupportedCommand(t *testing.T) {
	pr, pw := net.Pipe()
	defer pr.Close()
	defer pw.Close()

	// Wrap pw so writes don't block
	wrapper := &discardConn{Conn: pr}

	go func() {
		req := []byte{0x05, 0x02, 0x00, socksAtypIPv4, 127, 0, 0, 1, 0x00, 0x50}
		pw.Write(req)
	}()

	_, err := readSocks5Target(wrapper)
	if err == nil {
		t.Error("expected error for unsupported command")
	}
}

func TestReadSocks5Target_UnsupportedAddrType(t *testing.T) {
	pr, pw := net.Pipe()
	defer pr.Close()
	defer pw.Close()

	wrapper := &discardConn{Conn: pr}

	go func() {
		req := []byte{0x05, 0x01, 0x00, 0x02, 127, 0, 0, 1, 0x00, 0x50}
		pw.Write(req)
	}()

	_, err := readSocks5Target(wrapper)
	if err == nil {
		t.Error("expected error for unsupported address type")
	}
}

func TestReadSocks5Target_PrematureEOF(t *testing.T) {
	client, server := net.Pipe()

	go func() {
		// Send only 2 bytes then close
		client.Write([]byte{0x05, 0x01})
		client.Close()
	}()

	_, err := readSocks5Target(server)
	if err == nil {
		t.Error("expected error on premature EOF")
	}
}

func TestWriteSocks5Reply(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		writeSocks5Reply(server, socksReplySucceeded)
		server.Close()
	}()

	buf := make([]byte, 10)
	n, err := io.ReadFull(client, buf)
	if err != nil {
		t.Fatalf("failed to read reply: %v", err)
	}
	if n != 10 {
		t.Errorf("expected 10 bytes, got %d", n)
	}
	if buf[0] != socksVersion5 {
		t.Errorf("expected version 0x05, got 0x%02x", buf[0])
	}
	if buf[1] != socksReplySucceeded {
		t.Errorf("expected reply 0x00, got 0x%02x", buf[1])
	}
}
