package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

type ppppConn interface {
	ConnectLANSearch() error
	Run(ctx context.Context) error
	Close() error
	State() ppppclient.State
	Channel(index int) (*protocol.Channel, error)
}

type ppppClientFactory func(ctx context.Context) (ppppConn, error)

// PPPPService manages the LAN PPPP connection and XZYH dispatch.
type PPPPService struct {
	BaseWorker

	log          *slog.Logger
	client       ppppConn
	clientMu     sync.Mutex
	clientFactor ppppClientFactory
	pollInterval time.Duration

	handlersMu sync.RWMutex
	handlers   map[byte][]func([]byte)
}

// NewPPPPService creates a PPPP service.
func NewPPPPService(cfg *config.Manager, printerIndex int) *PPPPService {
	s := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.With("service", "ppppservice"),
		pollInterval: 50 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
	}
	s.clientFactor = defaultPPPPClientFactory(cfg, printerIndex)
	s.BindHooks(s)
	return s
}

func defaultPPPPClientFactory(cfgMgr *config.Manager, printerIndex int) ppppClientFactory {
	return func(ctx context.Context) (ppppConn, error) {
		if cfgMgr == nil {
			return nil, errors.New("ppppservice: config manager is nil")
		}
		cfg, err := cfgMgr.Load()
		if err != nil {
			return nil, fmt.Errorf("ppppservice: load config: %w", err)
		}
		if cfg == nil || len(cfg.Printers) == 0 {
			return nil, errors.New("ppppservice: no printers configured")
		}
		if printerIndex < 0 || printerIndex >= len(cfg.Printers) {
			return nil, fmt.Errorf("ppppservice: printer index out of range: %d", printerIndex)
		}

		printer := cfg.Printers[printerIndex]
		duid, err := protocol.ParseDuidString(printer.P2PDUID)
		if err != nil {
			return nil, fmt.Errorf("ppppservice: parse p2p duid: %w", err)
		}

		host := printer.IPAddr
		if host == "" {
			ip, err := ppppclient.DiscoverLANIP(ctx, printer.P2PDUID)
			if err != nil {
				return nil, fmt.Errorf("ppppservice: discover printer ip: %w", err)
			}
			host = ip.String()
		}

		cli, err := ppppclient.OpenLAN(duid, host)
		if err != nil {
			return nil, fmt.Errorf("ppppservice: open lan client: %w", err)
		}
		if err := cli.ConnectLANSearch(); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("ppppservice: connect lan search: %w", err)
		}
		return cli, nil
	}
}

// RegisterXzyhHandler registers a handler for XZYH frames on the given channel.
func (s *PPPPService) RegisterXzyhHandler(channel byte, fn func([]byte)) {
	if fn == nil {
		return
	}
	s.handlersMu.Lock()
	s.handlers[channel] = append(s.handlers[channel], fn)
	s.handlersMu.Unlock()
}

// WorkerStart establishes the PPPP client.
func (s *PPPPService) WorkerStart() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := s.clientFactor(ctx)
	if err != nil {
		return err
	}
	s.clientMu.Lock()
	s.client = cli
	s.clientMu.Unlock()
	return nil
}

// WorkerRun blocks while PPPP is running and dispatches XZYH payloads.
func (s *PPPPService) WorkerRun(ctx context.Context) error {
	cli := s.currentClient()
	if cli == nil {
		return errors.New("ppppservice: no client")
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- cli.Run(ctx)
	}()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-runErrCh:
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				s.log.Warn("pppp run loop failed", "err", err)
			}
			return ErrServiceRestartSignal
		case <-ticker.C:
			if cli.State() == ppppclient.StateDisconnected {
				return ErrServiceRestartSignal
			}
			if err := s.drainAllXzyh(cli); err != nil {
				s.log.Warn("xzyh drain failed", "err", err)
				return ErrServiceRestartSignal
			}
		}
	}
}

// WorkerStop closes PPPP client.
func (s *PPPPService) WorkerStop() {
	s.clientMu.Lock()
	cli := s.client
	s.client = nil
	s.clientMu.Unlock()
	if cli != nil {
		if err := cli.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Warn("pppp close failed", "err", err)
		}
	}
}

func (s *PPPPService) currentClient() ppppConn {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	return s.client
}

// IsConnected reports whether the current PPPP client handshake is connected.
func (s *PPPPService) IsConnected() bool {
	cli := s.currentClient()
	if cli == nil {
		return false
	}
	return cli.State() == ppppclient.StateConnected
}

func (s *PPPPService) drainAllXzyh(cli ppppConn) error {
	for ch := 0; ch < 8; ch++ {
		wire, err := cli.Channel(ch)
		if err != nil {
			return fmt.Errorf("get channel %d: %w", ch, err)
		}
		if err := s.drainXzyh(byte(ch), wire); err != nil {
			return fmt.Errorf("drain channel %d: %w", ch, err)
		}
	}
	return nil
}

func (s *PPPPService) drainXzyh(channel byte, ch *protocol.Channel) error {
	for {
		header := ch.Peek(16, 0)
		if len(header) == 0 {
			return nil
		}
		if string(header[:4]) != "XZYH" {
			return nil
		}
		sz := int(binary.LittleEndian.Uint32(header[6:10]))
		frame := ch.Read(16+sz, 0)
		if len(frame) == 0 {
			return nil
		}
		x, err := protocol.ParseXzyh(frame)
		if err != nil {
			return err
		}
		s.dispatchXzyh(channel, x.Data)
	}
}

func (s *PPPPService) dispatchXzyh(channel byte, payload []byte) {
	s.handlersMu.RLock()
	handlers := append([]func([]byte){}, s.handlers[channel]...)
	s.handlersMu.RUnlock()

	for _, h := range handlers {
		data := append([]byte(nil), payload...)
		h(data)
	}
}
