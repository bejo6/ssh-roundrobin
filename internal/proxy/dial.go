package proxy

import (
	"fmt"
	"log"
	"net"
	"time"

	"ssh-roundrobin/internal/sshroundrobin"
	"ssh-roundrobin/internal/status"
)

// DialTargetWithRetries attempts to connect to a target address through
// multiple upstream SSH servers, retrying across different upstreams
// on failure. Reports successes/failures to the status tracker.
func DialTargetWithRetries(rr *sshroundrobin.RoundRobin, targetAddr string,
	maxUpstreams int, failThreshold int, failTTL time.Duration,
	tracker *status.ServerStatusTracker) (net.Conn, *sshroundrobin.SSHClient, error) {

	excluded := make(map[string]struct{})
	if maxUpstreams <= 0 {
		maxUpstreams = rr.Len()
		if maxUpstreams <= 0 {
			maxUpstreams = 1
		}
	}

	var lastErr error
	for i := 0; i < maxUpstreams; i++ {
		client, err := rr.GetForTarget(targetAddr, excluded)
		if err != nil {
			if lastErr != nil {
				return nil, nil, fmt.Errorf("%w; last dial error: %v", err, lastErr)
			}
			return nil, nil, err
		}

		upstreamAddr := client.ServerAddr()
		excluded[upstreamAddr] = struct{}{}
		log.Printf("Upstream selected %s (mode=%s hits=%d target=%s)", upstreamAddr, client.ServerMode(), client.SelectionCount(), targetAddr)

		sshConn := client.Client()
		if sshConn == nil {
			rr.ReportTargetFailure(client, targetAddr, failThreshold, failTTL, fmt.Errorf("ssh client is nil"))
			lastErr = fmt.Errorf("%s: ssh client is nil", upstreamAddr)
			continue
		}
		targetConn, dialErr := sshConn.Dial("tcp", targetAddr)
		if dialErr == nil {
			if tracker != nil {
				tracker.RecordSuccess(upstreamAddr)
			}
			return targetConn, client, nil
		}

		if tracker != nil {
			tracker.RecordFail(upstreamAddr, dialErr)
		}

		rr.ReportTargetFailure(client, targetAddr, failThreshold, failTTL, dialErr)
		log.Printf("Dial target failed, trying next upstream: upstream=%s target=%s err=%v", upstreamAddr, targetAddr, dialErr)
		lastErr = fmt.Errorf("%s failed target %s: %w", upstreamAddr, targetAddr, dialErr)
	}

	if lastErr == nil {
		return nil, nil, fmt.Errorf("no upstream attempts made")
	}

	if maxUpstreams > 1 {
		log.Printf("Rotation wrap-around: tried %d/%d upstreams for target %s, all failed", len(excluded), maxUpstreams, targetAddr)
	}

	return nil, nil, lastErr
}
