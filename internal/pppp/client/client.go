package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

const (
	PPPPLANPort = 32108
	PPPPWANPort = 32100
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

// OpenLAN opens a direct LAN PPPP client.
func OpenLAN(duid protocol.Duid, host string) (*Client, error) {
	return Open(duid, host, PPPPLANPort)
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
		setSockOptErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
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

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.setState(StateDisconnected)
			return nil
		case <-ticker.C:
			for _, ch := range c.chans {
				for _, pkt := range ch.Poll(time.Now()) {
					_ = c.SendPacket(pkt, nil)
				}
			}
			msg, addr, err := c.Recv(5 * time.Millisecond)
			if err == nil {
				c.setRemoteAddr(addr)
				c.process(msg)
			} else if !errors.Is(err, context.DeadlineExceeded) {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
			}
		}
	}
}

func (c *Client) process(msg any) {
	switch m := msg.(type) {
	case protocol.PingReq:
		_ = c.SendPacket(protocol.PingResp{}, nil)
	case protocol.Drw:
		_ = c.SendPacket(protocol.DrwAck{Chan: m.Chan, Acks: []uint16{m.Index}}, nil)
		if int(m.Chan) < len(c.chans) {
			c.chans[m.Chan].RXDrw(m.Index, m.Data)
		}
	case protocol.DrwAck:
		if int(m.Chan) < len(c.chans) {
			c.chans[m.Chan].RXAck(m.Acks)
		}
	case protocol.Hello:
		host := protocol.Host{AFamily: 2, Port: uint16(PPPPLANPort), Addr: net.IPv4zero}
		_ = c.SendPacket(protocol.HelloAck{Host: host}, nil)
	case protocol.P2pRdy:
		c.setState(StateConnected)
		host := protocol.Host{AFamily: 2, Port: uint16(PPPPLANPort), Addr: net.IPv4zero}
		_ = c.SendPacket(protocol.P2pRdyAck{DUID: c.duid, Host: host}, nil)
	case protocol.Message:
		if m.Type == protocol.TypeClose {
			c.setState(StateDisconnected)
		}
	}
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
