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
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
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
	RemoteIP() net.IP
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

	// cfgMgr and database are used to persist the discovered printer IP
	// back to default.json and the DB cache on every successful LAN connection.
	cfgMgr     *config.Manager
	database   *db.DB
	printerSN  string

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
	// Pre-load the printer SN for IP persistence (best-effort; empty SN disables persistence).
	printerSN := ""
	if cfg != nil {
		if loaded, err := cfg.Load(); err == nil && loaded != nil && printerIndex >= 0 && printerIndex < len(loaded.Printers) {
			printerSN = loaded.Printers[printerIndex].SN
		}
	}

	s := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.With("service", "ppppservice"),
		pollInterval: 50 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
		cfgMgr:       cfg,
		database:     database,
		printerSN:    printerSN,
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
		// After PunchPkt is received the client replies to the printer's
		// source address; the DUID filter ensures we latch onto the right
		// printer even when multiple AnkerMake devices are on the network.
		if knownIP := printer.IPAddr; knownIP != "" {
			slog.Info("ppppservice: known IP in config (broadcasting for handshake)", "ip", knownIP, "duid", logging.RedactID(printer.P2PDUID, 4))
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
		slog.Info("ppppservice: LanSearch broadcast sent, awaiting PunchPkt", "duid", logging.RedactID(printer.P2PDUID, 4))
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
		"commandType": int(subCmd),
	}
	if payload != nil {
		data["data"] = payload
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

	_, _, err = ch.Write(xb, false)
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

// waitConnected blocks until the PPPP client reaches StateConnected or the
// context / a 10-second deadline expires. This mirrors Python's pppp_open()
// which polls until api.state == PPPPState.Connected before uploading.
func (s *PPPPService) waitConnected(ctx context.Context) (ppppConn, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		cli := s.currentClient()
		if cli != nil && cli.State() == ppppclient.StateConnected {
			return cli, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("ppppservice: connection timeout waiting for handshake")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Upload implements PPPPFileUploader interface.
func (s *PPPPService) Upload(ctx context.Context, info UploadInfo, payload []byte, progress func(sent, total int64)) error {
	cli, err := s.waitConnected(ctx)
	if err != nil {
		return err
	}
	ch, err := cli.Channel(1)
	if err != nil {
		return err
	}

	replyCh := make(chan error, 1)
	handlerIdx := byte(1) // we listen on channel 1

	// Set up temporary reply tap
	s.handlersMu.Lock()
	aabbPos := len(s.aabbHandlers[handlerIdx])
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
	s.aabbHandlers[handlerIdx] = append(s.aabbHandlers[handlerIdx], wrapper)
	s.handlersMu.Unlock()

	defer func() {
		s.handlersMu.Lock()
		handlers := s.aabbHandlers[handlerIdx]
		if aabbPos >= 0 && aabbPos < len(handlers) {
			handlers = append(handlers[:aabbPos], handlers[aabbPos+1:]...)
			s.aabbHandlers[handlerIdx] = handlers
		}
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

	writeXzyh := func(ch *protocol.Channel, cmd protocol.P2PCmdType, chanID byte, data []byte) error {
		x := protocol.Xzyh{
			Cmd:  cmd,
			Chan: chanID,
			Data: data,
		}
		xb, err := x.MarshalBinary()
		if err != nil {
			return err
		}
		if _, _, err := ch.WriteContext(ctx, xb, true); err != nil {
			return err
		}
		return nil
	}

	ch0, err := cli.Channel(0)
	if err != nil {
		return err
	}

	fileUUID := strings.TrimSpace(info.MachineID)
	if fileUUID == "" || fileUUID == "-" {
		fileUUID = strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	}
	legacyID := fileUUID
	if len(legacyID) < 16 {
		legacyID += strings.Repeat("0", 16-len(legacyID))
	}
	legacyID = legacyID[:16]

	// 1. Send XZYH P2P_SEND_FILE (JSON handshake on channel 0, Python parity).
	xzyhReplyCh := make(chan []byte, 1)
	s.handlersMu.Lock()
	xzyhPos := len(s.handlers[0])
	s.handlers[0] = append(s.handlers[0], func(data []byte) {
		select {
		case xzyhReplyCh <- append([]byte(nil), data...):
		default:
		}
	})
	s.handlersMu.Unlock()
	defer func() {
		s.handlersMu.Lock()
		handlers := s.handlers[0]
		if xzyhPos >= 0 && xzyhPos < len(handlers) {
			handlers = append(handlers[:xzyhPos], handlers[xzyhPos+1:]...)
			s.handlers[0] = handlers
		}
		s.handlersMu.Unlock()
	}()

	jsonPayload := map[string]any{
		"uuid":          fileUUID,
		"device":        "ankerctl",
		"flag":          0,
		"random":        time.Now().UnixNano(),
		"timeout":       40,
		"total_timeout": 120,
	}
	jb, err := json.Marshal(jsonPayload)
	if err != nil {
		return err
	}
	if err := writeXzyh(ch0, protocol.P2PCmdP2pSendFile, 0, jb); err != nil {
		return fmt.Errorf("write send_file json handshake: %w", err)
	}

	useLegacy := false
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-xzyhReplyCh:
		if len(resp) >= 4 {
			code := binary.LittleEndian.Uint32(resp[:4])
			useLegacy = code != 0
		} else {
			useLegacy = true
		}
	case <-time.After(2 * time.Second):
		useLegacy = true
	}

	if useLegacy {
		if err := writeXzyh(ch0, protocol.P2PCmdP2pSendFile, 0, []byte(legacyID)); err != nil {
			return fmt.Errorf("write send_file legacy handshake: %w", err)
		}
	}

	// 2. Prepare metadata string
	// format: "type,name,size,md5,user_name,user_id,machine_id"
	h := md5.Sum(payload)
	md5Str := fmt.Sprintf("%x", h)
	meta := fmt.Sprintf("0,%s,%d,%s,%s,%s,%s", info.Name, info.Size, md5Str, info.UserName, info.UserID, fileUUID)
	metaData := append([]byte(meta), 0)

	// 3. Send BEGIN and wait for ACK (Python: aabb_request waits after each frame).
	begin := protocol.Aabb{FrameType: protocol.FileTransferBegin}
	bp, err := begin.PackWithCRC(metaData)
	if err != nil {
		return err
	}
	if _, _, err := ch.WriteContext(ctx, bp, true); err != nil {
		return fmt.Errorf("write aabb begin: %w", err)
	}
	if err := waitReply(); err != nil {
		return fmt.Errorf("aabb begin reply: %w", err)
	}

	// 4. Send DATA — 128KB blocks matches Python blocksize.
	blockSize := 1024 * 128
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
		if _, _, err := ch.WriteContext(ctx, dp, true); err != nil {
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

	// 5. Optionally send END to trigger print start.
	if !info.StartPrint {
		return nil
	}
	endAabb := protocol.Aabb{FrameType: protocol.FileTransferEnd}
	ep, err := endAabb.PackWithCRC([]byte{})
	if err != nil {
		return err
	}
	if _, _, err := ch.WriteContext(ctx, ep, true); err != nil {
		return fmt.Errorf("write aabb end: %w", err)
	}

	// END ACK is best-effort: the printer has already queued the job and starts
	// preheating regardless. If the ACK is lost (UDP) or the printer does not
	// send one, we log a warning but treat the upload as successful.
	if err := waitReply(); err != nil {
		s.log.Warn("ppppservice: no END ACK from printer (print job already accepted)", "err", err)
	}
	return nil
}

// WorkerStart establishes the PPPP client.
func (s *PPPPService) WorkerStart() error {
	ctx, cancel := context.WithTimeout(s.LoopContext(), 5*time.Second)
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

// connectTimeout is how long WorkerRun waits for the initial PPPP handshake
// (PunchPkt → StateConnected) before giving up and restarting.
// Python's pppp_open uses a 10-second deadline for the same wait.
const connectTimeout = 10 * time.Second

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

	connectDeadline := time.Now().Add(connectTimeout)
	ipPersisted := false // persist discovered IP once per connection

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
				// Give the printer time to reply to LanSearch before restarting.
				// Python waits up to 10 s for StateConnected; we do the same.
				if time.Now().After(connectDeadline) {
					s.log.Warn("ppppservice: connection timeout, restarting")
					return ErrServiceRestartSignal
				}
				continue
			}
			// Once connected, reset deadline (not used further, but clean).
			connectDeadline = time.Time{}
			// Persist discovered printer IP on first successful connection.
			if !ipPersisted {
				if ip := cli.RemoteIP(); ip != nil {
					s.persistPrinterIP(ip.String())
					ipPersisted = true
				}
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

// persistPrinterIP saves the discovered LAN IP back to default.json and the DB.
// Called once per connection after the PPPP handshake completes.
func (s *PPPPService) persistPrinterIP(ipStr string) {
	if s.cfgMgr != nil && s.printerSN != "" {
		if err := s.cfgMgr.Modify(func(saved *model.Config) (*model.Config, error) {
			for i := range saved.Printers {
				if saved.Printers[i].SN == s.printerSN {
					saved.Printers[i].IPAddr = ipStr
				}
			}
			return saved, nil
		}); err != nil {
			s.log.Warn("ppppservice: failed to persist printer IP to config", "ip", ipStr, "err", err)
		}
	}
	if s.database != nil && s.printerSN != "" {
		if err := s.database.SetPrinterIP(s.printerSN, ipStr); err != nil {
			s.log.Warn("ppppservice: failed to persist printer IP to db", "ip", ipStr, "err", err)
		}
	}
	s.log.Info("ppppservice: persisted printer IP", "ip", ipStr)
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

// ProbePPPP performs a lightweight PPPP connectivity probe without touching the
// long-lived service registry instance used by the web UI and video pipeline.
func ProbePPPP(ctx context.Context, cfg *config.Manager, printerIndex int, database *db.DB) bool {
	return probePPPPWithFactory(ctx, defaultPPPPClientFactory(cfg, printerIndex, database))
}

func probePPPPWithFactory(ctx context.Context, factory ppppClientFactory) bool {
	if factory == nil {
		return false
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cli, err := factory(probeCtx)
	if err != nil {
		return false
	}
	defer func() { _ = cli.Close() }()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- cli.Run(probeCtx)
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if cli.State() == ppppclient.StateConnected {
			return true
		}

		select {
		case <-probeCtx.Done():
			return cli.State() == ppppclient.StateConnected
		case <-ticker.C:
		case <-runErrCh:
			return cli.State() == ppppclient.StateConnected
		}
	}
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
		// Peek 12 bytes: enough to identify both AABB (Len at [8:12]) and
		// XZYH (Len at [6:10]) frames. Using 16 would miss short AABB replies
		// (e.g. 1-byte ACK = 12+1+2 = 15 bytes total).
		header := ch.Peek(12, 0)
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
