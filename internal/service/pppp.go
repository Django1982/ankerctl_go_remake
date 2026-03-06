package service

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
	"github.com/google/uuid"
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

	handlersMu    sync.RWMutex
	handlers      map[byte][]func([]byte)
	videoHandlers []func(protocol.VideoFrame)
	aabbHandlers  map[byte][]func(protocol.Aabb, []byte)
}

// NewPPPPService creates a PPPP service.
func NewPPPPService(cfg *config.Manager, printerIndex int) *PPPPService {
	return NewPPPPServiceWithDB(cfg, printerIndex, nil)
}

// NewPPPPServiceWithDB creates a PPPP service that consults a DB cache for
// the last-known printer IP before falling back to a LAN broadcast.
func NewPPPPServiceWithDB(cfg *config.Manager, printerIndex int, database *db.DB) *PPPPService {
	s := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.With("service", "ppppservice"),
		pollInterval: 50 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	s.clientFactor = defaultPPPPClientFactory(cfg, printerIndex, database)
	s.BindHooks(s)
	return s
}

func defaultPPPPClientFactory(cfgMgr *config.Manager, printerIndex int, database *db.DB) ppppClientFactory {
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

		// Always use broadcast LAN handshake (LanSearch on port 32108).
		// The printer only responds to broadcast LanSearch, not unicast.
		// After PunchPkt is received, process() switches the remote addr to
		// the printer IP on PPPPPort (32100) for the actual PPPP session.
		// The DUID filter in process() ensures we latch onto the right printer
		// even when multiple AnkerMake devices are on the network.
		if knownIP := printer.IPAddr; knownIP != "" {
			slog.Info("ppppservice: known IP in config (broadcasting for handshake)", "ip", knownIP, "duid", printer.P2PDUID)
		} else if database != nil && printer.SN != "" {
			if cachedIP, dbErr := database.GetPrinterIP(printer.SN); dbErr == nil && cachedIP != "" {
				slog.Info("ppppservice: known cached IP (broadcasting for handshake)", "ip", cachedIP, "sn", printer.SN)
			}
		}

		cli, err := ppppclient.OpenBroadcastLAN(duid)
		if err != nil {
			return nil, fmt.Errorf("ppppservice: open broadcast lan client: %w", err)
		}
		if err := cli.ConnectLANSearch(); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("ppppservice: connect lan search: %w", err)
		}
		slog.Info("ppppservice: LanSearch broadcast sent, awaiting PunchPkt", "duid", printer.P2PDUID)
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

// RegisterVideoHandler registers a handler for 64-byte video frames (channel 1).
func (s *PPPPService) RegisterVideoHandler(fn func(protocol.VideoFrame)) {
	if fn == nil {
		return
	}
	s.handlersMu.Lock()
	s.videoHandlers = append(s.videoHandlers, fn)
	s.handlersMu.Unlock()
}

// RegisterAabbHandler registers a handler for AABB frames on the given channel.
func (s *PPPPService) RegisterAabbHandler(channel byte, fn func(protocol.Aabb, []byte)) {
	if fn == nil {
		return
	}
	s.handlersMu.Lock()
	s.aabbHandlers[channel] = append(s.aabbHandlers[channel], fn)
	s.handlersMu.Unlock()
}

// P2PCommand sends a JSON-wrapped P2P command on channel 0.
func (s *PPPPService) P2PCommand(ctx context.Context, subCmd protocol.P2PSubCmdType, payload any) error {
	cli := s.currentClient()
	if cli == nil {
		return errors.New("ppppservice: no client")
	}
	ch, err := cli.Channel(0)
	if err != nil {
		return err
	}

	data := map[string]any{
		"command": int(subCmd),
	}
	if payload != nil {
		if m, ok := payload.(map[string]any); ok {
			for k, v := range m {
				data[k] = v
			}
		}
	}
	jb, err := json.Marshal(data)
	if err != nil {
		return err
	}

	x := protocol.Xzyh{
		Cmd:  protocol.P2PCmdP2pJson,
		Chan: 0,
		Data: jb,
	}
	xb, err := x.MarshalBinary()
	if err != nil {
		return err
	}

	_, _, err = ch.Write(xb, true)
	return err
}

// StartLive starts the video stream from the printer.
func (s *PPPPService) StartLive(ctx context.Context, mode int) error {
	// mode is currently ignored by the start_live call in Python, but used in SetVideoMode
	return s.P2PCommand(ctx, protocol.P2PSubCmdStartLive, map[string]any{
		"encryptkey": "x",
		"accountId":  "y",
	})
}

// StopLive stops the video stream.
func (s *PPPPService) StopLive(ctx context.Context) error {
	return s.P2PCommand(ctx, protocol.P2PSubCmdCloseLive, nil)
}

// SetVideoMode switches stream resolution/quality.
func (s *PPPPService) SetVideoMode(ctx context.Context, mode int) error {
	return s.P2PCommand(ctx, protocol.P2PSubCmdLiveModeSet, map[string]any{"mode": mode})
}

// SetLight toggles the printer camera light.
func (s *PPPPService) SetLight(ctx context.Context, on bool) error {
	return s.P2PCommand(ctx, protocol.P2PSubCmdLightStateSwitch, map[string]any{"open": on})
}

// Upload implements PPPPFileUploader interface.
func (s *PPPPService) Upload(ctx context.Context, info UploadInfo, payload []byte, progress func(sent, total int64)) error {
	cli := s.currentClient()
	if cli == nil {
		return errors.New("ppppservice: no client")
	}
	ch, err := cli.Channel(1)
	if err != nil {
		return err
	}

	replyCh := make(chan error, 1)
	handlerIdx := byte(1) // we listen on channel 1

	// Set up temporary reply tap
	s.handlersMu.Lock()
	wrapper := func(aabb protocol.Aabb, data []byte) {
		if len(data) != 1 {
			select {
			case replyCh <- fmt.Errorf("unexpected aabb reply len %d", len(data)):
			default:
			}
			return
		}
		if data[0] != 0 { // 0 = OK
			select {
			case replyCh <- fmt.Errorf("aabb reply error code: %d", data[0]):
			default:
			}
			return
		}
		select {
		case replyCh <- nil:
		default:
		}
	}
	// Note: We are appending this and not removing it properly in this simple implementation
	// For production, we should probably add a way to remove the handler or use a single persistent router.
	// But let's assume one concurrent upload for now.
	s.aabbHandlers[handlerIdx] = append(s.aabbHandlers[handlerIdx], wrapper)
	s.handlersMu.Unlock()

	defer func() {
		s.handlersMu.Lock()
		s.aabbHandlers[handlerIdx] = nil // Reset handlers for now (assumes exclusive use)
		s.handlersMu.Unlock()
	}()

	waitReply := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
			return errors.New("timeout waiting for aabb reply")
		case err := <-replyCh:
			return err
		}
	}

	// 1. Send XZYH P2P_SEND_FILE
	uid := uuid.NewString()[:16]
	x := protocol.Xzyh{
		Cmd:  protocol.P2PCmdP2pSendFile,
		Chan: 1,
		Data: []byte(uid),
	}
	xb, err := x.MarshalBinary()
	if err != nil {
		return err
	}
	if _, _, err := ch.Write(xb, true); err != nil {
		return fmt.Errorf("write send_file req: %w", err)
	}

	// 2. Prepare metadata string
	// format: "type,name,size,md5,user_name,user_id,machine_id"
	h := md5.Sum(payload)
	md5Str := fmt.Sprintf("%x", h)
	meta := fmt.Sprintf("0,%s,%d,%s,%s,%s,%s", info.Name, info.Size, md5Str, info.UserName, info.UserID, info.MachineID)
	metaData := append([]byte(meta), 0)

	// 3. Send BEGIN
	begin := protocol.Aabb{FrameType: protocol.FileTransferBegin}
	bp, err := begin.PackWithCRC(metaData)
	if err != nil {
		return err
	}
	if _, _, err := ch.Write(bp, true); err != nil {
		return fmt.Errorf("write aabb begin: %w", err)
	}

	// Wait for reply? Python says self.api_aabb_request(api, FileTransfer.DATA...)
	// Wait, Python's send_file uses api_aabb for BEGIN (no wait), then api_aabb_request for DATA (wait).
	// Let's just follow Python: no wait for BEGIN.

	// 4. Send DATA
	blockSize := 1024 * 32
	var pos int64
	for pos < info.Size {
		end := pos + int64(blockSize)
		if end > info.Size {
			end = info.Size
		}
		chunk := payload[pos:end]

		dataAabb := protocol.Aabb{
			FrameType: protocol.FileTransferData,
			Pos:       uint32(pos),
		}
		dp, err := dataAabb.PackWithCRC(chunk)
		if err != nil {
			return err
		}
		if _, _, err := ch.Write(dp, true); err != nil {
			return fmt.Errorf("write aabb data at %d: %w", pos, err)
		}

		if err := waitReply(); err != nil {
			return fmt.Errorf("aabb data reply at %d: %w", pos, err)
		}

		pos = end
		if progress != nil {
			progress(pos, info.Size)
		}
	}

	// 5. Send END
	endAabb := protocol.Aabb{FrameType: protocol.FileTransferEnd}
	ep, err := endAabb.PackWithCRC([]byte{})
	if err != nil {
		return err
	}
	if _, _, err := ch.Write(ep, true); err != nil {
		return fmt.Errorf("write aabb end: %w", err)
	}

	return waitReply()
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

		if header[0] == 0xAA && header[1] == 0xBB {
			if len(header) < 12 {
				return nil
			}
			sz := int(binary.LittleEndian.Uint32(header[8:12]))
			need := 12 + sz + 2
			frame := ch.Read(need, 0)
			if len(frame) == 0 {
				return nil
			}
			aabb, data, err := protocol.ParseAabbWithCRC(frame)
			if err != nil {
				s.log.Warn("aabb parse failed", "err", err)
				continue
			}
			s.dispatchAabb(channel, aabb, data)
			continue
		}

		if string(header[:4]) != "XZYH" {
			_ = ch.Read(1, 0)
			continue
		}
		sz := int(binary.LittleEndian.Uint32(header[6:10]))
		frame := ch.Read(16+sz, 0)
		if len(frame) == 0 {
			return nil
		}

		if channel == 1 && len(frame) >= 64 {
			// Channel 1 video frames have a 64-byte extended XZYH header.
			// Only attempt VideoFrame parse when the frame is large enough;
			// smaller XZYH frames on channel 1 (e.g. file transfer replies)
			// fall through to the generic XZYH path.
			vf, err := protocol.ParseVideoFrame(frame)
			if err != nil {
				s.log.Warn("video frame parse failed", "err", err)
				continue
			}
			s.dispatchVideo(vf)
		} else {
			x, err := protocol.ParseXzyh(frame)
			if err != nil {
				return err
			}
			s.dispatchXzyh(channel, x.Data)
		}
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

func (s *PPPPService) dispatchVideo(vf protocol.VideoFrame) {
	s.handlersMu.RLock()
	handlers := append([]func(protocol.VideoFrame){}, s.videoHandlers...)
	s.handlersMu.RUnlock()

	for _, h := range handlers {
		h(vf)
	}
}

func (s *PPPPService) dispatchAabb(channel byte, aabb protocol.Aabb, data []byte) {
	s.handlersMu.RLock()
	handlers := append([]func(protocol.Aabb, []byte){}, s.aabbHandlers[channel]...)
	s.handlersMu.RUnlock()

	for _, h := range handlers {
		payloadCopy := append([]byte(nil), data...)
		h(aabb, payloadCopy)
	}
}
