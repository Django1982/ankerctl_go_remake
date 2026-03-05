package httpapi

import (
	"fmt"
	"log/slog"
	"net"
	"time"
)

const regionConnectTimeout = 5 * time.Second

// Dialer is the function signature for creating TCP connections.
// Defaults to net.DialTimeout. Can be overridden for testing.
var Dialer = func(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// GuessRegion determines the closest Anker cloud region by measuring
// TCP connect time to each host. Returns the region key ("eu" or "us").
// Python: AnkerHTTPApi.guess_region()
func GuessRegion() string {
	type result struct {
		region   string
		duration time.Duration
	}

	results := make([]result, 0, len(hostsByRegion))
	for region, host := range hostsByRegion {
		d := measureConnectTime(host, 443)
		results = append(results, result{region: region, duration: d})
		slog.Debug("region probe", "region", region, "host", host, "latency", d)
	}

	best := results[0]
	for _, r := range results[1:] {
		if r.duration < best.duration {
			best = r
		}
	}

	slog.Info("detected closest region", "region", best.region, "latency", best.duration)
	return best.region
}

// measureConnectTime measures the TCP connect time to a host:port.
func measureConnectTime(host string, port int) time.Duration {
	addr := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()
	conn, err := Dialer("tcp", addr, regionConnectTimeout)
	elapsed := time.Since(start)
	if err != nil {
		slog.Warn("region probe failed", "host", host, "error", err)
		return regionConnectTimeout // Use max timeout as penalty.
	}
	conn.Close()
	return elapsed
}
