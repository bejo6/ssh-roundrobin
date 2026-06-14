package proxy

import (
	"fmt"
	"log"
	"net"
	"time"

	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

// HandleConnection handles a single TCP forwarding connection,
// dialing the target through the round-robin upstream pool.
func HandleConnection(conn net.Conn, rr *sshroundrobin.RoundRobin,
	targetHost string, targetPort int, retryUpstreams int,
	failThreshold int, failTTL time.Duration,
	tracker *status.ServerStatusTracker) {

	defer conn.Close()

	targetAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)
	targetConn, client, err := DialTargetWithRetries(rr, targetAddr, retryUpstreams, failThreshold, failTTL, tracker)
	if err != nil {
		log.Printf("Failed to connect to target %s after retries: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	log.Printf("Forwarding connection to %s", targetAddr)
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
