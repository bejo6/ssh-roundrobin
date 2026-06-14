package proxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// TunnelBidirectional copies data bidirectionally between left and right connections.
// Returns bytes transferred from right-to-left and left-to-right.
func TunnelBidirectional(left net.Conn, right net.Conn) (int64, int64) {
	var wg sync.WaitGroup
	var rightToLeft int64
	var leftToRight int64
	wg.Add(2)

	go func() {
		defer wg.Done()
		n, err := io.Copy(left, right)
		rightToLeft = n
		if err != nil && !isClosedOrEOF(err) {
			log.Printf("Tunnel right->left copy error: %v (bytes=%d)", err, n)
		}
	}()

	go func() {
		defer wg.Done()
		n, err := io.Copy(right, left)
		leftToRight = n
		if err != nil && !isClosedOrEOF(err) {
			log.Printf("Tunnel left->right copy error: %v (bytes=%d)", err, n)
		}
	}()

	wg.Wait()
	return rightToLeft, leftToRight
}

// isClosedOrEOF returns true for errors that indicate a normal connection close.
func isClosedOrEOF(err error) bool {
	if err == io.EOF {
		return true
	}
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "use of closed network connection"
	}
	return false
}

// IsLikelyTargetBlocked detects if a tunnel ended suspiciously fast with
// data sent but nothing received — likely indicating the upstream target
// blocked or dropped the connection.
func IsLikelyTargetBlocked(start time.Time, upstreamToClient int64, clientToUpstream int64) bool {
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		return false
	}
	if clientToUpstream == 0 {
		return false
	}
	return upstreamToClient == 0
}
