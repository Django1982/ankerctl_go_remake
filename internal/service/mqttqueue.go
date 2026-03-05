package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	mqttclient "github.com/django1982/ankerctl/internal/mqtt/client"
	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

const (
	mqttStateIdle     = 0
	mqttStatePrinting = 1
	mqttStatePaused   = 2
	mqttStateAborted  = 8
)

const (
	printControlRestart = 0
	printControlPause   = 2
	printControlResume  = 3
	printControlStop    = 4
)

var mqttBrokerByRegion = map[string]string{
	"eu": "make-mqtt-eu.ankermake.com",
	"us": "make-mqtt.ankermake.com",
}

type mqttClient interface {
	Fetch() []mqttclient.DecodedMessage
	Command(ctx context.Context, msg any) error
	Query(ctx context.Context, msg any) error
	Disconnect(quiesce time.Duration)
}

type mqttClientFactory func(ctx context.Context) (mqttClient, error)

type eventSink interface {
	Notify(data any)
}

// MqttQueue is the core printer MQTT event service.
type MqttQueue struct {
	BaseWorker

	log *slog.Logger

	history *db.DB

	mu                 sync.Mutex
	client             mqttClient
	clientFactory      mqttClientFactory
	lastQuery          time.Time
	queryInterval      time.Duration
	pollInterval       time.Duration
	printActive        bool
	pendingHistory     bool
	lastFilename       string
	currentPrinterStat int
	stopRequested      bool
	gcodeLayerCount    int
	debugLogging       bool
	lastStatePayload   map[string]any

	homeAssistant eventSink
	timelapse     eventSink
}

// NewMqttQueue creates a MqttQueue service.
func NewMqttQueue(cfg *config.Manager, printerIndex int, history *db.DB, ha eventSink, timelapse eventSink) *MqttQueue {
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		log:                slog.With("service", "mqttqueue"),
		history:            history,
		queryInterval:      10 * time.Second,
		pollInterval:       100 * time.Millisecond,
		currentPrinterStat: -1,
		homeAssistant:      ha,
		timelapse:          timelapse,
	}
	q.clientFactory = defaultMQTTClientFactory(cfg, printerIndex)
	q.BindHooks(q)
	return q
}

func defaultMQTTClientFactory(cfgMgr *config.Manager, printerIndex int) mqttClientFactory {
	return func(ctx context.Context) (mqttClient, error) {
		if cfgMgr == nil {
			return nil, errors.New("mqttqueue: config manager is nil")
		}

		cfg, err := cfgMgr.Load()
		if err != nil {
			return nil, fmt.Errorf("mqttqueue: load config: %w", err)
		}
		if cfg == nil || cfg.Account == nil || len(cfg.Printers) == 0 {
			return nil, errors.New("mqttqueue: printer/account config missing")
		}
		if printerIndex < 0 || printerIndex >= len(cfg.Printers) {
			return nil, fmt.Errorf("mqttqueue: printer index out of range: %d", printerIndex)
		}
		printer := cfg.Printers[printerIndex]
		acct := cfg.Account

		broker := mqttBrokerByRegion[acct.Region]
		if broker == "" {
			broker = mqttBrokerByRegion["us"]
		}

		transport, err := mqttclient.NewPahoTransport(mqttclient.PahoConfig{
			Broker:   broker,
			Port:     mqttclient.DefaultBrokerPort,
			ClientID: "ankerctl-" + uuid.NewString(),
			Username: acct.MQTTUsername(),
			Password: acct.MQTTPassword(),
		})
		if err != nil {
			return nil, fmt.Errorf("mqttqueue: create transport: %w", err)
		}

		c, err := mqttclient.New(printer.SN, printer.MQTTKey, mqttclient.Config{
			Broker:    broker,
			Port:      mqttclient.DefaultBrokerPort,
			Transport: transport,
			Logger:    slog.With("service", "mqttqueue", "component", "mqtt-client"),
		})
		if err != nil {
			return nil, fmt.Errorf("mqttqueue: create client: %w", err)
		}
		if err := c.Connect(ctx); err != nil {
			return nil, fmt.Errorf("mqttqueue: connect client: %w", err)
		}
		return c, nil
	}
}

func (q *MqttQueue) resetPrintStateLocked() {
	q.printActive = false
	q.pendingHistory = false
	q.lastFilename = ""
	q.stopRequested = false
	q.currentPrinterStat = -1
	q.gcodeLayerCount = 0
	q.lastStatePayload = nil
}

// WorkerInit resets internal state.
func (q *MqttQueue) WorkerInit() {
	q.mu.Lock()
	q.resetPrintStateLocked()
	q.mu.Unlock()
}

// WorkerStart opens and connects an MQTT client.
func (q *MqttQueue) WorkerStart() error {
	c, err := q.clientFactory(context.Background())
	if err != nil {
		return err
	}
	q.mu.Lock()
	q.client = c
	q.lastQuery = time.Time{}
	q.resetPrintStateLocked()
	q.mu.Unlock()
	return nil
}

// WorkerRun polls MQTT messages and dispatches by commandType.
func (q *MqttQueue) WorkerRun(ctx context.Context) error {
	ticker := time.NewTicker(q.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := q.maybeQueryStatus(ctx); err != nil {
				return ErrServiceRestartSignal
			}
			for _, msg := range q.currentClientFetch() {
				for _, obj := range msg.Objects {
					q.handlePayload(obj)
				}
			}
		}
	}
}

// WorkerStop disconnects MQTT client.
func (q *MqttQueue) WorkerStop() {
	q.mu.Lock()
	c := q.client
	q.client = nil
	q.resetPrintStateLocked()
	q.mu.Unlock()
	if c != nil {
		c.Disconnect(250 * time.Millisecond)
	}
}

func (q *MqttQueue) maybeQueryStatus(ctx context.Context) error {
	q.mu.Lock()
	c := q.client
	last := q.lastQuery
	interval := q.queryInterval
	if c == nil {
		q.mu.Unlock()
		return errors.New("mqttqueue: missing mqtt client")
	}
	if !last.IsZero() && time.Since(last) < interval {
		q.mu.Unlock()
		return nil
	}
	q.lastQuery = time.Now()
	q.mu.Unlock()

	cmd := map[string]any{
		"commandType": int(protocol.MqttCmdAppQueryStatus),
		"value":       0,
	}
	if err := c.Query(ctx, cmd); err != nil {
		q.log.Warn("status query failed", "err", err)
		return err
	}
	return nil
}

func (q *MqttQueue) currentClientFetch() []mqttclient.DecodedMessage {
	q.mu.Lock()
	c := q.client
	q.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Fetch()
}

func (q *MqttQueue) handlePayload(obj map[string]any) {
	ct, ok := asInt(obj["commandType"])
	if !ok {
		q.Notify(obj)
		return
	}

	normalized := cloneMap(obj)
	if progress, ok := extractProgress(obj); ok {
		normalized["progress"] = normalizeProgress(progress)
	}
	if q.debugLogging {
		q.log.Debug("mqtt payload", "payload", logging.Redact(normalized))
	}

	q.Notify(normalized)

	switch ct {
	case int(protocol.MqttCmdModelDLProcess): // ct=1044
		q.handleCT1044(normalized)
	case int(protocol.MqttCmdEventNotify): // ct=1000
		q.mu.Lock()
		q.lastStatePayload = cloneMap(normalized)
		q.mu.Unlock()
		q.handleCT1000(normalized)
	}

	if isForwardRelevant(ct, normalized) {
		q.forward(normalized)
	}
}

func (q *MqttQueue) handleCT1044(payload map[string]any) {
	filename := extractFilename(payload)
	if filename == "" {
		filePath, _ := payload["filePath"].(string)
		if filePath != "" {
			filename = filepath.Base(filePath)
		}
	}
	if filename == "" {
		return
	}

	q.mu.Lock()
	q.lastFilename = filename
	shouldRecord := q.printActive && q.pendingHistory
	q.pendingHistory = false
	q.mu.Unlock()

	if shouldRecord && q.history != nil {
		if _, err := q.history.RecordStart(filename, ""); err != nil {
			q.log.Warn("history record start failed", "filename", filename, "err", err)
		}
	}
}

func (q *MqttQueue) handleCT1000(payload map[string]any) {
	state, ok := asInt(payload["value"])
	if !ok {
		return
	}

	var (
		changed      bool
		shouldRecord bool
		filename     string
	)

	q.mu.Lock()
	changed = state != q.currentPrinterStat
	q.currentPrinterStat = state
	switch state {
	case mqttStatePrinting:
		if !q.printActive {
			q.printActive = true
			if q.lastFilename != "" {
				shouldRecord = true
				filename = q.lastFilename
			} else {
				q.pendingHistory = true
			}
		}
	case mqttStateIdle, mqttStateAborted:
		q.printActive = false
		q.pendingHistory = false
		q.stopRequested = false
	}
	q.mu.Unlock()

	if changed {
		stateEvt := map[string]any{
			"event": "print_state",
			"state": state,
		}
		q.Notify(stateEvt)
		q.forward(stateEvt)
	}

	if shouldRecord && q.history != nil {
		if _, err := q.history.RecordStart(filename, ""); err != nil {
			q.log.Warn("history record start failed", "filename", filename, "err", err)
		}
	}
}

func (q *MqttQueue) forward(data any) {
	if q.homeAssistant != nil {
		q.homeAssistant.Notify(data)
	}
	if q.timelapse != nil {
		q.timelapse.Notify(data)
	}
}

// SendPrintControl sends a print control command over MQTT.
func (q *MqttQueue) SendPrintControl(ctx context.Context, value int) error {
	q.mu.Lock()
	c := q.client
	if value == printControlStop {
		q.stopRequested = true
	}
	q.mu.Unlock()
	if c == nil {
		return errors.New("mqttqueue: mqtt client not connected")
	}
	cmd := map[string]any{
		"commandType": int(protocol.MqttCmdPrintControl),
		"value":       value,
	}
	if err := c.Command(ctx, cmd); err != nil {
		return fmt.Errorf("mqttqueue: send print control: %w", err)
	}
	return nil
}

// RestartPrint sends print-control=restart (0).
func (q *MqttQueue) RestartPrint(ctx context.Context) error {
	return q.SendPrintControl(ctx, printControlRestart)
}

// PausePrint sends print-control=pause (2).
func (q *MqttQueue) PausePrint(ctx context.Context) error {
	return q.SendPrintControl(ctx, printControlPause)
}

// ResumePrint sends print-control=resume (3).
func (q *MqttQueue) ResumePrint(ctx context.Context) error {
	return q.SendPrintControl(ctx, printControlResume)
}

// StopPrint sends print-control=stop (4).
func (q *MqttQueue) StopPrint(ctx context.Context) error {
	return q.SendPrintControl(ctx, printControlStop)
}

// SendGCode sends raw GCode command text to the printer.
func (q *MqttQueue) SendGCode(ctx context.Context, gcode string) error {
	q.mu.Lock()
	c := q.client
	q.mu.Unlock()
	if c == nil {
		return errors.New("mqttqueue: mqtt client not connected")
	}
	cmd := map[string]any{
		"commandType": int(protocol.MqttCmdGcodeCommand),
		"cmdData":     gcode,
		"gcode":       gcode,
	}
	if err := c.Command(ctx, cmd); err != nil {
		return fmt.Errorf("mqttqueue: send gcode: %w", err)
	}
	return nil
}

// SendAutoLeveling requests printer auto-leveling.
func (q *MqttQueue) SendAutoLeveling(ctx context.Context) error {
	q.mu.Lock()
	c := q.client
	q.mu.Unlock()
	if c == nil {
		return errors.New("mqttqueue: mqtt client not connected")
	}
	cmd := map[string]any{
		"commandType": int(protocol.MqttCmdAutoLeveling),
		"value":       1,
	}
	if err := c.Command(ctx, cmd); err != nil {
		return fmt.Errorf("mqttqueue: send auto-leveling: %w", err)
	}
	return nil
}

// SetLight toggles printer light.
func (q *MqttQueue) SetLight(ctx context.Context, on bool) error {
	q.mu.Lock()
	c := q.client
	q.mu.Unlock()
	if c == nil {
		return errors.New("mqttqueue: mqtt client not connected")
	}
	v := 0
	if on {
		v = 1
	}
	cmd := map[string]any{
		"commandType": int(protocol.MqttCmdOnOffModal),
		"value":       v,
	}
	if err := c.Command(ctx, cmd); err != nil {
		return fmt.Errorf("mqttqueue: set light: %w", err)
	}
	return nil
}

// SetGCodeLayerCount stores parsed layer count from uploaded gcode.
func (q *MqttQueue) SetGCodeLayerCount(layerCount int) {
	q.mu.Lock()
	q.gcodeLayerCount = layerCount
	q.mu.Unlock()
}

// IsPrinting reports whether the print state is active.
func (q *MqttQueue) IsPrinting() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.printActive || q.currentPrinterStat == mqttStatePrinting || q.currentPrinterStat == mqttStatePaused
}

// SetDebugLogging enables/disables verbose payload logging.
func (q *MqttQueue) SetDebugLogging(enabled bool) {
	q.mu.Lock()
	q.debugLogging = enabled
	q.mu.Unlock()
}

// SimulateEvent emits a synthetic event to subscribers and forwarding sinks.
func (q *MqttQueue) SimulateEvent(eventType string, payload map[string]any) {
	sim := map[string]any{
		"type":    eventType,
		"payload": cloneMap(payload),
	}
	q.Notify(sim)
	q.forward(sim)
}

// SnapshotState returns a best-effort current state payload for debug API.
func (q *MqttQueue) SnapshotState() map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := map[string]any{
		"is_printing":       q.printActive,
		"state":             q.currentPrinterStat,
		"pending_history":   q.pendingHistory,
		"last_filename":     q.lastFilename,
		"stop_requested":    q.stopRequested,
		"gcode_layer_count": q.gcodeLayerCount,
	}
	if q.lastStatePayload != nil {
		out["last_event"] = cloneMap(q.lastStatePayload)
	}
	return out
}

func isForwardRelevant(commandType int, payload map[string]any) bool {
	switch commandType {
	case int(protocol.MqttCmdEventNotify),
		int(protocol.MqttCmdPrintSchedule),
		int(protocol.MqttCmdModelDLProcess),
		int(protocol.MqttCmdNozzleTemp),
		int(protocol.MqttCmdHotbedTemp),
		int(protocol.MqttCmdPrintSpeed),
		int(protocol.MqttCmdModelLayer):
		return true
	}
	_, ok := payload["progress"]
	return ok
}

func normalizeProgress(raw int) int {
	switch {
	case raw < 0:
		return 0
	case raw <= 100:
		return raw
	case raw <= 10000:
		return raw / 100
	default:
		return 100
	}
}

func extractProgress(payload map[string]any) (int, bool) {
	if v, ok := asInt(payload["progress"]); ok {
		return v, true
	}
	for k, v := range payload {
		if k == "progress" {
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			if p, ok := asInt(nested["progress"]); ok {
				return p, true
			}
		}
	}
	return 0, false
}

func extractFilename(payload map[string]any) string {
	for _, key := range []string{"name", "fileName", "filename", "file_name", "gcode", "gcode_name"} {
		if v, ok := payload[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if m, ok := v.(map[string]any); ok {
			out[k] = cloneMap(m)
			continue
		}
		if b, ok := v.([]byte); ok {
			cp := make([]byte, len(b))
			copy(cp, b)
			out[k] = cp
			continue
		}
		out[k] = v
	}
	return out
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int8:
		return int(x), true
	case int16:
		return int(x), true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case uint:
		return int(x), true
	case uint8:
		return int(x), true
	case uint16:
		return int(x), true
	case uint32:
		return int(x), true
	case uint64:
		return int(x), true
	case float32:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
