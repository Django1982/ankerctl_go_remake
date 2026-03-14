package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	ppppcrypto "github.com/django1982/ankerctl/internal/pppp/crypto"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

const (
	PPPPLANPort = 32108    // UDP port for LAN discovery (LanSearch broadcast / PunchPkt)
	PPPPPort    = 32100    // UDP port for PPPP session (file upload, camera, remote control)
	PPPPWANPort = PPPPPort // kept for compatibility
)

// State represents PPPP connection lifecycle state.
type State int

const (
	StateIdle State = iota + 1
	StateConnecting
	StateConnected
	StateDisconnected
)

type udpConn interface {
	SetReadDeadline(t time.Time) error
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	Close() error
}

// Client manages a PPPP UDP session with 8 logical channels.
type Client struct {
	conn udpConn
	duid protocol.Duid
	addr *net.UDPAddr

	mu    sync.RWMutex
	state State

	chans [8]*protocol.Channel

	running bool
	wg      sync.WaitGroup
}

// NewClient creates a client around an existing UDP connection.
func NewClient(conn udpConn, duid protocol.Duid, addr *net.UDPAddr) *Client {
	c := &Client{conn: conn, duid: duid, addr: addr, state: StateIdle}
	for i := range c.chans {
		c.chans[i] = protocol.NewChannel(uint8(i))
	}
	return c
}

// Open creates a client for an explicit host:port.
func Open(duid protocol.Duid, host string, port int) (*Client, error) {
	raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("pppp: resolve udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("pppp: listen udp: %w", err)
	}
	return NewClient(conn, duid, raddr), nil
}

// OpenLAN opens a direct LAN PPPP client on the PPPP session port (32100).
func OpenLAN(duid protocol.Duid, host string) (*Client, error) {
	return Open(duid, host, PPPPPort)
}

// OpenBroadcastLAN opens a broadcast-capable client for the full LAN handshake.
// Unlike OpenBroadcast (discovery only), this sets the real DUID and starts in
// StateConnecting so the client can complete the LanSearch→PunchPkt→P2pRdy
// handshake. After PunchPkt is received, the remote addr is automatically
// updated to the printer's IP on PPPPPort (32100) for the session.
func OpenBroadcastLAN(duid protocol.Duid) (*Client, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("pppp: listen udp: %w", err)
	}
	rawConn, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: get raw conn: %w", err)
	}
	var setSockOptErr error
	if ctrlErr := rawConn.Control(func(fd uintptr) {
		setSockOptErr = setSockOptBroadcast(fd)
	}); ctrlErr != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: control raw conn: %w", ctrlErr)
	}
	if setSockOptErr != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: set SO_BROADCAST: %w", setSockOptErr)
	}
	addr := &net.UDPAddr{IP: net.IPv4bcast, Port: PPPPLANPort}
	c := NewClient(conn, duid, addr)
	// StateConnecting: handshake begins after ConnectLANSearch.
	return c, nil
}

// OpenWAN opens a WAN PPPP client.
func OpenWAN(duid protocol.Duid, host string) (*Client, error) {
	return Open(duid, host, PPPPWANPort)
}

// OpenBroadcast opens a broadcast client for LAN search.
// SO_BROADCAST must be set explicitly on Linux; without it WriteTo to
// 255.255.255.255 returns EACCES.
func OpenBroadcast() (*Client, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("pppp: listen udp: %w", err)
	}

	rawConn, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: get raw conn: %w", err)
	}
	var setSockOptErr error
	if ctrlErr := rawConn.Control(func(fd uintptr) {
		setSockOptErr = setSockOptBroadcast(fd)
	}); ctrlErr != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: control raw conn: %w", ctrlErr)
	}
	if setSockOptErr != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: set SO_BROADCAST: %w", setSockOptErr)
	}

	if err := conn.SetWriteBuffer(1 << 20); err != nil {
		conn.Close()
		return nil, fmt.Errorf("pppp: set write buffer: %w", err)
	}
	addr := &net.UDPAddr{IP: net.IPv4bcast, Port: PPPPLANPort}
	c := NewClient(conn, protocol.Duid{}, addr)
	c.state = StateConnected
	return c, nil
}

// Close stops the run loop and closes socket.
func (c *Client) Close() error {
	c.mu.Lock()
	c.running = false
	c.state = StateDisconnected
	c.mu.Unlock()
	c.wg.Wait()
	return c.conn.Close()
}

// State returns current lifecycle state.
func (c *Client) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) setState(s State) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

func (c *Client) remoteAddr() *net.UDPAddr {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.addr == nil {
		return nil
	}
	cp := *c.addr
	return &cp
}

// RemoteIP returns the printer's IP address as discovered via PunchPkt.
// Returns nil if the handshake has not yet completed.
func (c *Client) RemoteIP() net.IP {
	if addr := c.remoteAddr(); addr != nil {
		return addr.IP
	}
	return nil
}

func (c *Client) setRemoteAddr(addr *net.UDPAddr) {
	c.mu.Lock()
	c.addr = addr
	c.mu.Unlock()
}

// ConnectLANSearch starts handshake by broadcasting LAN_SEARCH.
func (c *Client) ConnectLANSearch() error {
	c.setState(StateConnecting)
	return c.SendPacket(protocol.LanSearch{}, nil)
}

// Run starts the recv/process/retransmit loop.
// On exit (context cancellation, connection close, or remote Close packet),
// it sends a Close packet to the peer — matching Python ppppapi.py run().
func (c *Client) Run(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errors.New("pppp: client already running")
	}
	c.running = true
	if c.state == StateIdle {
		c.state = StateConnecting
	}
	c.mu.Unlock()

	c.wg.Add(1)
	defer c.wg.Done()

	// Send Close packet on exit, matching Python's run() which always
	// calls self.send(PktClose()) after the loop.
	defer func() {
		_ = c.sendClose()
		c.setState(StateDisconnected)
	}()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Check if we were disconnected by a remote Close packet.
			if c.State() == StateDisconnected {
				return ErrConnectionReset
			}

			for _, ch := range c.chans {
				for _, pkt := range ch.Poll(time.Now()) {
					_ = c.SendPacket(pkt, nil)
				}
			}

			// Drain as many inbound UDP packets as available in this cycle.
			// Reading only one datagram per 25ms tick throttles video heavily
			// and creates multi-second/minute latency under sustained frame rate.
			for i := 0; i < 256; i++ {
				msg, addr, err := c.Recv(1 * time.Millisecond)
				if err == nil {
					c.setRemoteAddr(addr)
					c.process(msg)
					continue
				}
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					break
				}
				if errors.Is(err, context.DeadlineExceeded) {
					break
				}
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				// Log unexpected decode/read errors at WARN level for diagnostics.
				slog.Warn("pppp: recv/decode error", "err", err)
				break
			}
		}
	}
}

// ErrConnectionReset is returned by Run when a remote Close packet is received.
var ErrConnectionReset = errors.New("pppp: connection reset by remote")

func (c *Client) process(msg any) {
	switch m := msg.(type) {
	case protocol.PunchPkt:
		// LAN handshake step 2: printer responds to LanSearch with PunchPkt.
		// While connecting, send Close then P2pRdy to advance the handshake.
		// Python reference: ppppapi.py process() PUNCH_PKT handler.
		//
		// DUID check: if this client has a non-zero DUID, only accept PunchPkt
		// from the matching printer (important when multiple AnkerMake devices
		// are on the network and we're using broadcast LanSearch).
		//
		// We reply to the printer's PunchPkt source address (the ephemeral port
		// it sent from). Python ppppapi.py does the same — it never switches to
		// a different port for the handshake. Port 32100 is for WAN (cloud relay)
		// or the post-handshake data phase; the handshake itself uses whatever
		// addr the printer sent PunchPkt from.
		if c.State() == StateConnecting {
			emptyDUID := protocol.Duid{}
			if c.duid == emptyDUID || m.DUID.String() == c.duid.String() {
				_ = c.SendPacket(protocol.Close{}, nil)
				_ = c.SendPacket(protocol.P2pRdy{DUID: c.duid}, nil)
			}
		}
	case protocol.P2pRdy:
		// LAN handshake step 4: printer confirms with P2pRdy.
		// Send P2pRdyAck and transition to Connected.
		host := protocol.Host{AFamily: 2, Port: uint16(PPPPLANPort), Addr: net.IPv4zero}
		_ = c.SendPacket(protocol.P2pRdyAck{DUID: c.duid, Host: host}, nil)
		c.setState(StateConnected)
	case protocol.Hello:
		host := protocol.Host{AFamily: 2, Port: uint16(PPPPLANPort), Addr: net.IPv4zero}
		_ = c.SendPacket(protocol.HelloAck{Host: host}, nil)
	case protocol.PingReq:
		_ = c.SendPacket(protocol.PingResp{}, nil)
	case protocol.PingResp:
		// ALIVE_ACK — no action needed, matches Python.
	case protocol.Drw:
		_ = c.SendPacket(protocol.DrwAck{Chan: m.Chan, Acks: []uint16{m.Index}}, nil)
		if int(m.Chan) < len(c.chans) {
			c.chans[m.Chan].RXDrw(m.Index, m.Data)
		}
	case protocol.DrwAck:
		if int(m.Chan) < len(c.chans) {
			c.chans[m.Chan].RXAck(m.Acks)
		}
	case protocol.Close:
		c.setState(StateDisconnected)
	case protocol.Message:
		// Fallback for unhandled typed packets that decode to raw Message.
		switch m.Type {
		case protocol.TypeClose:
			// Defensive fallback: if Close arrives as raw Message instead of
			// the typed protocol.Close, still handle it.
			c.setState(StateDisconnected)
		case protocol.TypeDevLgnCRC:
			// Printer device login with CRC — respond with DevLgnAckCrc.
			// Python reference: ppppapi.py process() DEV_LGN_CRC handler.
			_ = c.sendDevLgnAckCrc()
		}
	}
}

// sendDevLgnAckCrc sends a DEV_LGN_ACK_CRC packet (type 0x13).
// The payload is 4 zero bytes, curse-encrypted.
// Python reference: pppp.py PktDevLgnAckCrc.
func (c *Client) sendDevLgnAckCrc() error {
	payload := ppppcrypto.Curse(make([]byte, 4))
	raw := protocol.Message{Type: protocol.TypeDevLgnAckCRC, Payload: payload}
	data := raw.MarshalBinary()
	addr := c.remoteAddr()
	if addr == nil {
		return errors.New("pppp: missing remote address")
	}
	_, err := c.conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("pppp: udp send dev_lgn_ack_crc: %w", err)
	}
	return nil
}

// sendClose sends a Close packet to the remote peer.
// It is best-effort and ignores errors since we are shutting down.
func (c *Client) sendClose() error {
	addr := c.remoteAddr()
	if addr == nil {
		return nil // nowhere to send
	}
	raw, err := protocol.EncodePacket(protocol.Close{})
	if err != nil {
		return err
	}
	_, err = c.conn.WriteToUDP(raw, addr)
	return err
}

// Recv reads one UDP datagram and decodes PPPP packet.
func (c *Client) Recv(timeout time.Duration) (any, *net.UDPAddr, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, nil, fmt.Errorf("pppp: set deadline: %w", err)
	}
	buf := make([]byte, 4096)
	n, addr, err := c.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, err
	}
	pkt, err := protocol.DecodePacket(buf[:n])
	if err != nil {
		return nil, nil, fmt.Errorf("pppp: decode packet: %w", err)
	}
	return pkt, addr, nil
}

// SendPacket encodes and sends one PPPP packet.
func (c *Client) SendPacket(pkt protocol.Packet, addr *net.UDPAddr) error {
	raw, err := protocol.EncodePacket(pkt)
	if err != nil {
		return fmt.Errorf("pppp: encode packet: %w", err)
	}
	if addr == nil {
		addr = c.remoteAddr()
	}
	if addr == nil {
		return errors.New("pppp: missing remote address")
	}
	if _, err := c.conn.WriteToUDP(raw, addr); err != nil {
		return fmt.Errorf("pppp: udp send: %w", err)
	}
	return nil
}

// Channel returns one logical channel by index.
func (c *Client) Channel(index int) (*protocol.Channel, error) {
	if index < 0 || index >= len(c.chans) {
		return nil, fmt.Errorf("pppp: channel index out of range: %d", index)
	}
	return c.chans[index], nil
}

// DiscoverLANIP sends LAN_SEARCH and returns source IP for matching DUID.
func DiscoverLANIP(ctx context.Context, expectedDUID string) (net.IP, error) {
	c, err := OpenBroadcast()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return discoverLANIPWithConn(ctx, c, expectedDUID)
}

// LANDiscoveryResult holds a single LAN search result.
type LANDiscoveryResult struct {
	DUID string
	IP   net.IP
}

// DiscoverLANAll sends a LAN_SEARCH broadcast and collects all responding
// printers until the context deadline or cancellation. Returns all unique
// DUID+IP pairs found. This mirrors Python's cli.pppp.lan_search().
func DiscoverLANAll(ctx context.Context) ([]LANDiscoveryResult, error) {
	c, err := OpenBroadcast()
	if err != nil {
		return nil, err
	}
	defer c.Close()

	if err := c.SendPacket(protocol.LanSearch{}, nil); err != nil {
		return nil, err
	}

	type seenKey struct{ duid, ip string }
	seen := make(map[seenKey]bool)
	var results []LANDiscoveryResult

	for {
		if err := ctx.Err(); err != nil {
			break
		}
		pkt, addr, err := c.Recv(100 * time.Millisecond)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			break
		}
		punch, ok := pkt.(protocol.PunchPkt)
		if !ok {
			continue
		}
		key := seenKey{duid: punch.DUID.String(), ip: addr.IP.String()}
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, LANDiscoveryResult{
			DUID: punch.DUID.String(),
			IP:   addr.IP,
		})
	}
	return results, nil
}

func discoverLANIPWithConn(ctx context.Context, c *Client, expectedDUID string) (net.IP, error) {
	if err := c.SendPacket(protocol.LanSearch{}, nil); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pkt, addr, err := c.Recv(100 * time.Millisecond)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return nil, err
		}
		punch, ok := pkt.(protocol.PunchPkt)
		if !ok {
			continue
		}
		if expectedDUID == "" || punch.DUID.String() == expectedDUID {
			return addr.IP, nil
		}
	}
}
