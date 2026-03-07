package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"time"

	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

type discovery struct {
	IP   string
	Port int
	DUID string
}

func main() {
	timeout := flag.Duration("timeout", 2*time.Second, "how long to wait for LAN_SEARCH replies")
	flag.Parse()

	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be > 0")
		os.Exit(2)
	}

	client, err := ppppclient.OpenBroadcast()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open broadcast client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	if err := client.SendPacket(protocol.LanSearch{}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "send LAN_SEARCH: %v\n", err)
		os.Exit(1)
	}

	deadline := time.Now().Add(*timeout)
	results := make(map[string]discovery)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		recvTimeout := minDuration(250*time.Millisecond, remaining)
		pkt, addr, err := client.Recv(recvTimeout)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			fmt.Fprintf(os.Stderr, "recv: %v\n", err)
			os.Exit(1)
		}

		punch, ok := pkt.(protocol.PunchPkt)
		if !ok {
			continue
		}
		key := addr.IP.String() + "|" + punch.DUID.String()
		results[key] = discovery{
			IP:   addr.IP.String(),
			Port: addr.Port,
			DUID: punch.DUID.String(),
		}
	}

	if len(results) == 0 {
		fmt.Println("no Anker PPPP replies received")
		os.Exit(1)
	}

	out := make([]discovery, 0, len(results))
	for _, item := range results {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IP != out[j].IP {
			return out[i].IP < out[j].IP
		}
		return out[i].DUID < out[j].DUID
	})

	for _, item := range out {
		fmt.Printf("printer=%s ip=%s src_port=%d\n", item.DUID, item.IP, item.Port)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
