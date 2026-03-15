package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/django1982/ankerctl/internal/model"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeToken struct{ err error }

func (t fakeToken) WaitTimeout(timeout time.Duration) bool { return true }
func (t fakeToken) Error() error                           { return t.err }

// fakeTimeoutToken simulates a token that never completes (WaitTimeout returns false).
type fakeTimeoutToken struct{}

func (t fakeTimeoutToken) WaitTimeout(timeout time.Duration) bool { return false }
func (t fakeTimeoutToken) Error() error                           { return nil }

// fakeErrorToken simulates a token that completes with an error.
type fakeErrorToken struct{ err error }

func (t fakeErrorToken) WaitTimeout(timeout time.Duration) bool { return true }
func (t fakeErrorToken) Error() error                           { return t.err }

type publishedMsg struct {
	topic    string
	payload  string
	retained bool
}

type fakeHAClient struct {
	mu          sync.Mutex
	connected   bool
	onSubscribe map[string]paho.MessageHandler
	published   []publishedMsg

	// Override connect behaviour.
	connectToken HomeAssistantToken
}

func newFakeHAClient() *fakeHAClient {
	return &fakeHAClient{connected: true, onSubscribe: make(map[string]paho.MessageHandler)}
}

func (c *fakeHAClient) Connect() HomeAssistantToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connectToken != nil {
		return c.connectToken
	}
	return fakeToken{}
}
func (c *fakeHAClient) Disconnect(quiesce uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
}
func (c *fakeHAClient) Publish(topic string, qos byte, retained bool, payload interface{}) HomeAssistantToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.published = append(c.published, publishedMsg{topic: topic, payload: asString(payload), retained: retained})
	return fakeToken{}
}
func (c *fakeHAClient) Subscribe(topic string, qos byte, callback paho.MessageHandler) HomeAssistantToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onSubscribe[topic] = callback
	return fakeToken{}
}
func (c *fakeHAClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *fakeHAClient) getPublished() []publishedMsg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]publishedMsg(nil), c.published...)
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return ""
	}
}

type fakeMessage struct {
	topic   string
	payload []byte
}

func (m *fakeMessage) Duplicate() bool   { return false }
func (m *fakeMessage) Qos() byte         { return 1 }
func (m *fakeMessage) Retained() bool    { return false }
func (m *fakeMessage) Topic() string     { return m.topic }
func (m *fakeMessage) MessageID() uint16 { return 1 }
func (m *fakeMessage) Payload() []byte   { return m.payload }
func (m *fakeMessage) Ack()              {}

type fakeLight struct {
	mu    sync.Mutex
	calls []bool
}

func (l *fakeLight) SetLight(ctx context.Context, on bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, on)
	return nil
}

func (l *fakeLight) lastCall() (bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.calls) == 0 {
		return false, false
	}
	return l.calls[len(l.calls)-1], true
}

func (l *fakeLight) getCalls() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bool(nil), l.calls...)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultHACfg() model.HomeAssistantConfig {
	return model.HomeAssistantConfig{
		Enabled:         true,
		MQTTHost:        "ha.local",
		MQTTPort:        1883,
		DiscoveryPrefix: "homeassistant",
	}
}

func startHAService(t *testing.T, cfg model.HomeAssistantConfig, sn string, light *fakeLight) (*HomeAssistantService, *fakeHAClient) {
	t.Helper()
	svc := NewHomeAssistantService(cfg, sn, "TestPrinter", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient { return client }
	svc.heartbeatInterval = 20 * time.Millisecond
	svc.connectTimeout = 500 * time.Millisecond

	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
	return svc, client
}

// ---------------------------------------------------------------------------
// Existing Tests (preserved)
// ---------------------------------------------------------------------------

func TestHomeAssistantPublishesDiscoveryAndHeartbeat(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN1", light)
	defer svc.Shutdown()

	svc.onConnected()
	time.Sleep(80 * time.Millisecond)

	published := client.getPublished()
	if len(published) == 0 {
		t.Fatal("expected discovery/availability publishes")
	}
	foundAvail := false
	for _, pub := range published {
		if strings.Contains(pub.topic, "/availability") {
			foundAvail = true
			break
		}
	}
	if !foundAvail {
		t.Fatal("expected availability heartbeat publish")
	}
}

func TestHomeAssistantLightCommand(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN2", light)
	defer svc.Shutdown()
	svc.onConnected()

	cmdTopic := "ankerctl/SN2/light/set"
	client.mu.Lock()
	h, ok := client.onSubscribe[cmdTopic]
	client.mu.Unlock()
	if !ok {
		t.Fatalf("expected subscription for %s", cmdTopic)
	}

	h(nil, &fakeMessage{topic: cmdTopic, payload: []byte("ON")})
	time.Sleep(60 * time.Millisecond)
	last, ok := light.lastCall()
	if !ok || !last {
		t.Fatalf("expected ON light call")
	}
}

// ---------------------------------------------------------------------------
// New Tests
// ---------------------------------------------------------------------------

func TestHomeAssistantConnectTimeout(t *testing.T) {
	cfg := defaultHACfg()
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_TIMEOUT", "Printer", light)
	client := newFakeHAClient()
	client.connectToken = fakeTimeoutToken{}
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient { return client }
	svc.connectTimeout = 50 * time.Millisecond

	// WorkerStart should return a connect timeout error.
	// The BaseWorker loop will transition to Stopped (with holdoff) and retry.
	svc.Start(context.Background())
	// Give the loop time to attempt start, fail, and remain in stopped state.
	time.Sleep(200 * time.Millisecond)

	// Service should not be running because connect timed out.
	state := svc.State()
	if state == StateRunning {
		t.Fatal("service should not reach Running when connect times out")
	}
	svc.Shutdown()
}

func TestHomeAssistantConnectError(t *testing.T) {
	cfg := defaultHACfg()
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_ERR", "Printer", light)
	client := newFakeHAClient()
	client.connectToken = fakeErrorToken{err: errors.New("connection refused")}
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient { return client }
	svc.connectTimeout = 50 * time.Millisecond

	svc.Start(context.Background())
	time.Sleep(200 * time.Millisecond)

	state := svc.State()
	if state == StateRunning {
		t.Fatal("service should not reach Running when connect returns error")
	}
	svc.Shutdown()
}

func TestHomeAssistantDisabledDoesNotConnect(t *testing.T) {
	cfg := defaultHACfg()
	cfg.Enabled = false
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_DISABLED", "Printer", light)

	clientCreated := false
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		clientCreated = true
		return newFakeHAClient()
	}

	svc.Start(context.Background())
	// When disabled, WorkerStart returns nil immediately without creating a client.
	// The WorkerRun loop will run but no MQTT operations occur.
	waitForState(t, svc, StateRunning, 2*time.Second)
	defer svc.Shutdown()

	if clientCreated {
		t.Fatal("MQTT client should not be created when service is disabled")
	}
}

func TestHomeAssistantHeartbeatCadence(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_HB", light)
	defer svc.Shutdown()
	svc.onConnected()

	// Count availability publishes over time.
	// heartbeatInterval is 20ms; after ~100ms we should see multiple heartbeats.
	time.Sleep(120 * time.Millisecond)

	published := client.getPublished()
	availCount := 0
	for _, pub := range published {
		if strings.Contains(pub.topic, "/availability") && pub.payload == "online" {
			availCount++
		}
	}
	// onConnected() itself publishes one "online", plus ticker should fire several times in 120ms.
	if availCount < 3 {
		t.Fatalf("expected at least 3 availability heartbeats, got %d", availCount)
	}
}

func TestHomeAssistantSensorValueTypes(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_VALS", light)
	defer svc.Shutdown()
	svc.onConnected()

	tests := []struct {
		key   string
		value any
	}{
		{"nozzle_temp", 215.5},
		{"nozzle_temp_target", 220},
		{"bed_temp", 60.0},
		{"bed_temp_target", 65},
		{"print_progress", 42},
		{"print_speed", 150},
		{"print_status", "printing"},
		{"print_filename", "benchy.gcode"},
		{"print_layer", "12/100"},
		{"time_elapsed", 3600},
		{"time_remaining", 1800},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			svc.UpdateState(map[string]any{tc.key: tc.value})
			time.Sleep(60 * time.Millisecond)

			published := client.getPublished()
			stateTopic := fmt.Sprintf("ankerctl/SN_VALS/state")
			var lastState map[string]any
			for i := len(published) - 1; i >= 0; i-- {
				if published[i].topic == stateTopic {
					if err := json.Unmarshal([]byte(published[i].payload), &lastState); err != nil {
						t.Fatalf("unmarshal state: %v", err)
					}
					break
				}
			}
			if lastState == nil {
				t.Fatal("no state message found")
			}

			got, ok := lastState[tc.key]
			if !ok {
				t.Fatalf("key %q not in state payload", tc.key)
			}

			// JSON numbers are float64; strings are strings.
			switch expected := tc.value.(type) {
			case string:
				if got != expected {
					t.Fatalf("expected %q, got %v", expected, got)
				}
			case int:
				if got != float64(expected) {
					t.Fatalf("expected %v, got %v", expected, got)
				}
			case float64:
				if got != expected {
					t.Fatalf("expected %v, got %v", expected, got)
				}
			}
		})
	}
}

func TestHomeAssistantLightCommandOnOff(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantOn  bool
	}{
		{"ON uppercase", "ON", true},
		{"on lowercase", "on", true},
		{"On mixed", "On", true},
		{"OFF uppercase", "OFF", false},
		{"off lowercase", "off", false},
		{"Off mixed", "Off", false},
		{"empty", "", false},
		{"whitespace ON", "  ON  ", true}, // TrimSpace strips whitespace, then EqualFold("on") matches
		{"random text", "toggle", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			light := &fakeLight{}
			svc, client := startHAService(t, defaultHACfg(), "SN_CMD", light)
			defer svc.Shutdown()
			svc.onConnected()

			cmdTopic := "ankerctl/SN_CMD/light/set"
			client.mu.Lock()
			h, ok := client.onSubscribe[cmdTopic]
			client.mu.Unlock()
			if !ok {
				t.Fatalf("expected subscription for %s", cmdTopic)
			}

			h(nil, &fakeMessage{topic: cmdTopic, payload: []byte(tc.payload)})
			time.Sleep(60 * time.Millisecond)

			calls := light.getCalls()
			if len(calls) != 1 {
				t.Fatalf("expected 1 light call, got %d", len(calls))
			}
			if calls[0] != tc.wantOn {
				t.Fatalf("expected light=%v for payload %q, got %v", tc.wantOn, tc.payload, calls[0])
			}
		})
	}
}

func TestHomeAssistantLightCommandNilController(t *testing.T) {
	// Service with nil light controller should not panic.
	svc, client := startHAService(t, defaultHACfg(), "SN_NIL", nil)
	defer svc.Shutdown()
	svc.light = nil // explicitly nil
	svc.onConnected()

	cmdTopic := "ankerctl/SN_NIL/light/set"
	client.mu.Lock()
	h, ok := client.onSubscribe[cmdTopic]
	client.mu.Unlock()
	if !ok {
		t.Fatalf("expected subscription for %s", cmdTopic)
	}

	// Should not panic when light controller is nil.
	h(nil, &fakeMessage{topic: cmdTopic, payload: []byte("ON")})
	time.Sleep(60 * time.Millisecond)
}

func TestHomeAssistantDiscoveryPayloadCompleteness(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_DISC", light)
	defer svc.Shutdown()
	svc.onConnected()

	time.Sleep(60 * time.Millisecond)
	published := client.getPublished()

	nodeID := "ankerctl_SN_DISC"
	prefix := "homeassistant"

	// Expected discovery topics: 11 sensors, 2 binary_sensor, 1 switch, 1 camera = 15 total.
	expectedSensors := []string{
		"print_progress", "print_status", "print_filename", "print_layer",
		"print_speed", "nozzle_temp", "bed_temp", "nozzle_temp_target",
		"bed_temp_target", "time_elapsed", "time_remaining",
	}
	expectedBinary := []string{"mqtt_connected", "pppp_connected"}
	expectedSwitch := []string{"light"}
	expectedCamera := []string{"camera"}

	// Build set of expected config topics.
	expectedTopics := make(map[string]string) // topic -> entity_type
	for _, id := range expectedSensors {
		topic := fmt.Sprintf("%s/sensor/%s/%s/config", prefix, nodeID, id)
		expectedTopics[topic] = "sensor"
	}
	for _, id := range expectedBinary {
		topic := fmt.Sprintf("%s/binary_sensor/%s/%s/config", prefix, nodeID, id)
		expectedTopics[topic] = "binary_sensor"
	}
	for _, id := range expectedSwitch {
		topic := fmt.Sprintf("%s/switch/%s/%s/config", prefix, nodeID, id)
		expectedTopics[topic] = "switch"
	}
	for _, id := range expectedCamera {
		topic := fmt.Sprintf("%s/camera/%s/%s/config", prefix, nodeID, id)
		expectedTopics[topic] = "camera"
	}

	foundTopics := make(map[string]bool)
	for _, pub := range published {
		if _, ok := expectedTopics[pub.topic]; ok {
			foundTopics[pub.topic] = true
		}
	}

	for topic, entityType := range expectedTopics {
		if !foundTopics[topic] {
			t.Errorf("missing discovery topic for %s: %s", entityType, topic)
		}
	}

	if t.Failed() {
		return
	}

	// Validate sensor discovery payloads have required HA fields.
	for _, pub := range published {
		entityType, ok := expectedTopics[pub.topic]
		if !ok {
			continue
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(pub.payload), &cfg); err != nil {
			t.Fatalf("unmarshal %s: %v", pub.topic, err)
		}

		// All entities must have: name, unique_id, device, availability.
		for _, field := range []string{"name", "unique_id", "device", "availability"} {
			if _, ok := cfg[field]; !ok {
				t.Errorf("%s: missing field %q", pub.topic, field)
			}
		}

		// Device must have identifiers, name, manufacturer, model.
		if dev, ok := cfg["device"].(map[string]any); ok {
			for _, df := range []string{"identifiers", "name", "manufacturer", "model"} {
				if _, ok := dev[df]; !ok {
					t.Errorf("%s: device missing field %q", pub.topic, df)
				}
			}
		}

		// Binary sensors must have device_class, payload_on, payload_off.
		if entityType == "binary_sensor" {
			for _, field := range []string{"device_class", "payload_on", "payload_off"} {
				if _, ok := cfg[field]; !ok {
					t.Errorf("%s: binary_sensor missing field %q", pub.topic, field)
				}
			}
			if cfg["device_class"] != "connectivity" {
				t.Errorf("%s: device_class should be 'connectivity', got %v", pub.topic, cfg["device_class"])
			}
		}

		// Switch must have command_topic, payload_on, payload_off.
		if entityType == "switch" {
			for _, field := range []string{"command_topic", "payload_on", "payload_off"} {
				if _, ok := cfg[field]; !ok {
					t.Errorf("%s: switch missing field %q", pub.topic, field)
				}
			}
		}

		// Sensors with units must have unit_of_measurement.
		if entityType == "sensor" {
			unitsExpected := map[string]string{
				"print_progress":    "%",
				"print_speed":       "mm/s",
				"nozzle_temp":       "\u00b0C",
				"bed_temp":          "\u00b0C",
				"nozzle_temp_target": "\u00b0C",
				"bed_temp_target":   "\u00b0C",
				"time_elapsed":      "s",
				"time_remaining":    "s",
			}
			// Extract sensor ID from topic.
			parts := strings.Split(pub.topic, "/")
			sensorID := parts[len(parts)-2]
			if expectedUnit, ok := unitsExpected[sensorID]; ok {
				if cfg["unit_of_measurement"] != expectedUnit {
					t.Errorf("%s: unit_of_measurement should be %q, got %v", pub.topic, expectedUnit, cfg["unit_of_measurement"])
				}
			}
		}
	}
}

func TestHomeAssistantDisconnectReconnect(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_RECONN", light)
	defer svc.Shutdown()
	svc.onConnected()

	time.Sleep(40 * time.Millisecond)

	// Simulate disconnect: set connected=false, trigger ConnectionLost callback.
	client.mu.Lock()
	client.connected = false
	client.mu.Unlock()
	svc.setOnline(false)

	// Publish should be a no-op when disconnected.
	prevCount := len(client.getPublished())
	svc.UpdateState(map[string]any{"print_progress": 50})
	time.Sleep(40 * time.Millisecond)
	afterCount := len(client.getPublished())
	if afterCount != prevCount {
		t.Fatalf("expected no publishes while disconnected, but got %d new messages", afterCount-prevCount)
	}

	// Simulate reconnect.
	client.mu.Lock()
	client.connected = true
	client.mu.Unlock()
	svc.onConnected()

	// Now updates should publish again.
	svc.UpdateState(map[string]any{"print_progress": 75})
	time.Sleep(60 * time.Millisecond)

	published := client.getPublished()
	foundState := false
	for _, pub := range published {
		if pub.topic == "ankerctl/SN_RECONN/state" && strings.Contains(pub.payload, "75") {
			foundState = true
			break
		}
	}
	if !foundState {
		t.Fatal("expected state publish after reconnect")
	}
}

func TestHomeAssistantUpdateStateDropsOnFullQueue(t *testing.T) {
	light := &fakeLight{}
	svc := NewHomeAssistantService(defaultHACfg(), "SN_DROP", "Printer", light)

	// Fill the updateQ buffer without starting the service (nobody drains).
	for i := 0; i < 64; i++ {
		svc.UpdateState(map[string]any{"print_progress": i})
	}

	// The 65th update should be silently dropped (non-blocking select).
	svc.UpdateState(map[string]any{"print_progress": 999})

	// Queue should be exactly at capacity.
	if len(svc.updateQ) != 64 {
		t.Fatalf("expected queue at capacity (64), got %d", len(svc.updateQ))
	}
}

func TestHomeAssistantStateTopic(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "SN_TOPIC", "Printer", nil)
	got := svc.stateTopic()
	want := "ankerctl/SN_TOPIC/state"
	if got != want {
		t.Fatalf("stateTopic() = %q, want %q", got, want)
	}
}

func TestHomeAssistantAvailabilityTopic(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "SN_TOPIC", "Printer", nil)
	got := svc.availabilityTopic()
	want := "ankerctl/SN_TOPIC/availability"
	if got != want {
		t.Fatalf("availabilityTopic() = %q, want %q", got, want)
	}
}

func TestHomeAssistantDefaultNodeID(t *testing.T) {
	cfg := defaultHACfg()
	cfg.NodeID = ""
	svc := NewHomeAssistantService(cfg, "MYSERIAL", "Printer", nil)
	if svc.nodeID != "ankerctl_MYSERIAL" {
		t.Fatalf("nodeID = %q, want %q", svc.nodeID, "ankerctl_MYSERIAL")
	}
}

func TestHomeAssistantCustomNodeID(t *testing.T) {
	cfg := defaultHACfg()
	cfg.NodeID = "custom_node"
	svc := NewHomeAssistantService(cfg, "MYSERIAL", "Printer", nil)
	if svc.nodeID != "custom_node" {
		t.Fatalf("nodeID = %q, want %q", svc.nodeID, "custom_node")
	}
}

func TestHomeAssistantEmptySerialFallback(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "", "Printer", nil)
	if svc.printerSN != "ankerctl" {
		t.Fatalf("printerSN = %q, want %q", svc.printerSN, "ankerctl")
	}
}

func TestHomeAssistantEmptyNameFallback(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "SN", "", nil)
	if svc.printerName != "AnkerMake M5" {
		t.Fatalf("printerName = %q, want %q", svc.printerName, "AnkerMake M5")
	}
}

func TestHomeAssistantDefaultPort(t *testing.T) {
	cfg := defaultHACfg()
	cfg.MQTTPort = 0

	var capturedOpts *paho.ClientOptions
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_PORT", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		capturedOpts = opts
		return client
	}

	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
	defer svc.Shutdown()

	if capturedOpts == nil {
		t.Fatal("client options not captured")
	}
	// The options should contain the default port 1883.
	servers := capturedOpts.Servers
	if len(servers) == 0 {
		t.Fatal("no servers configured")
	}
	if !strings.Contains(servers[0].String(), "1883") {
		t.Fatalf("expected default port 1883, got %s", servers[0].String())
	}
}

func TestHomeAssistantDefaultHost(t *testing.T) {
	cfg := defaultHACfg()
	cfg.MQTTHost = ""

	var capturedOpts *paho.ClientOptions
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_HOST", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		capturedOpts = opts
		return client
	}

	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
	defer svc.Shutdown()

	if capturedOpts == nil {
		t.Fatal("client options not captured")
	}
	servers := capturedOpts.Servers
	if len(servers) == 0 {
		t.Fatal("no servers configured")
	}
	if !strings.Contains(servers[0].String(), "localhost") {
		t.Fatalf("expected default host 'localhost', got %s", servers[0].String())
	}
}

func TestHomeAssistantMQTTCredentials(t *testing.T) {
	cfg := defaultHACfg()
	cfg.MQTTUsername = "user1"
	cfg.MQTTPassword = "pass1"

	var capturedOpts *paho.ClientOptions
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_CRED", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		capturedOpts = opts
		return client
	}

	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
	defer svc.Shutdown()

	if capturedOpts == nil {
		t.Fatal("client options not captured")
	}
	if capturedOpts.Username != "user1" {
		t.Fatalf("username = %q, want %q", capturedOpts.Username, "user1")
	}
	if capturedOpts.Password != "pass1" {
		t.Fatalf("password = %q, want %q", capturedOpts.Password, "pass1")
	}
}

func TestHomeAssistantWillMessage(t *testing.T) {
	cfg := defaultHACfg()

	var capturedOpts *paho.ClientOptions
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN_WILL", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		capturedOpts = opts
		return client
	}

	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
	defer svc.Shutdown()

	if capturedOpts == nil {
		t.Fatal("client options not captured")
	}
	if !capturedOpts.WillEnabled {
		t.Fatal("will message should be enabled")
	}
	wantTopic := "ankerctl/SN_WILL/availability"
	if capturedOpts.WillTopic != wantTopic {
		t.Fatalf("will topic = %q, want %q", capturedOpts.WillTopic, wantTopic)
	}
	willPayload := string(capturedOpts.WillPayload)
	if willPayload != "offline" {
		t.Fatalf("will payload = %q, want %q", willPayload, "offline")
	}
}

func TestHomeAssistantPublishNoopWhenDisconnected(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_NOOP", light)
	defer svc.Shutdown()
	svc.onConnected()
	time.Sleep(40 * time.Millisecond)

	// Disconnect the client.
	client.mu.Lock()
	prevCount := len(client.published)
	client.connected = false
	client.mu.Unlock()

	// Updates should be silently dropped (publish checks IsConnected).
	svc.UpdateState(map[string]any{"print_progress": 99})
	time.Sleep(60 * time.Millisecond)

	client.mu.Lock()
	newCount := len(client.published)
	client.mu.Unlock()

	if newCount != prevCount {
		t.Fatalf("expected no new publishes when disconnected, got %d", newCount-prevCount)
	}
}

func TestHomeAssistantPublishNoopWhenClientNil(t *testing.T) {
	light := &fakeLight{}
	svc := NewHomeAssistantService(defaultHACfg(), "SN_NIL2", "Printer", light)
	// client is nil, publish should not panic.
	svc.publish("test/topic", []byte("payload"), false)
}

func TestHomeAssistantSetOnline(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "SN_ONLINE", "Printer", nil)

	svc.setOnline(true)
	svc.mu.RLock()
	v := svc.state["mqtt_connected"]
	svc.mu.RUnlock()
	if v != true {
		t.Fatalf("expected mqtt_connected=true, got %v", v)
	}

	svc.setOnline(false)
	svc.mu.RLock()
	v = svc.state["mqtt_connected"]
	svc.mu.RUnlock()
	if v != false {
		t.Fatalf("expected mqtt_connected=false, got %v", v)
	}
}

func TestHomeAssistantInitialState(t *testing.T) {
	svc := NewHomeAssistantService(defaultHACfg(), "SN_INIT", "Printer", nil)

	expectedKeys := []string{
		"print_progress", "print_status", "print_filename", "print_layer",
		"print_speed", "nozzle_temp", "nozzle_temp_target", "bed_temp",
		"bed_temp_target", "time_elapsed", "time_remaining",
		"mqtt_connected", "pppp_connected", "light",
	}

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	for _, key := range expectedKeys {
		if _, ok := svc.state[key]; !ok {
			t.Errorf("missing initial state key: %s", key)
		}
	}
	if len(svc.state) != len(expectedKeys) {
		t.Errorf("expected %d initial state keys, got %d", len(expectedKeys), len(svc.state))
	}
}

func TestHomeAssistantConfigure(t *testing.T) {
	light := &fakeLight{}
	svc, _ := startHAService(t, defaultHACfg(), "SN_CFG", light)
	defer svc.Shutdown()

	// Reconfigure should update config.
	newCfg := defaultHACfg()
	newCfg.MQTTHost = "new-broker.local"
	newCfg.Enabled = false
	svc.Configure(newCfg)

	svc.mu.RLock()
	host := svc.cfg.MQTTHost
	enabled := svc.enabled
	svc.mu.RUnlock()

	if host != "new-broker.local" {
		t.Fatalf("cfg.MQTTHost = %q, want %q", host, "new-broker.local")
	}
	if enabled {
		t.Fatal("expected enabled=false after Configure")
	}
}

func TestHomeAssistantStopPublishesOffline(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_STOP", light)
	svc.onConnected()
	time.Sleep(40 * time.Millisecond)

	svc.Shutdown()

	published := client.getPublished()
	// The last availability publish should be "offline".
	var lastAvail string
	for _, pub := range published {
		if strings.Contains(pub.topic, "/availability") {
			lastAvail = pub.payload
		}
	}
	if lastAvail != "offline" {
		t.Fatalf("expected last availability to be 'offline', got %q", lastAvail)
	}
}

func TestHomeAssistantMultipleStateUpdates(t *testing.T) {
	light := &fakeLight{}
	svc, client := startHAService(t, defaultHACfg(), "SN_MULTI", light)
	defer svc.Shutdown()
	svc.onConnected()

	// Send multiple updates, each should merge into the state.
	svc.UpdateState(map[string]any{"nozzle_temp": 100})
	svc.UpdateState(map[string]any{"bed_temp": 50, "print_status": "printing"})
	time.Sleep(80 * time.Millisecond)

	published := client.getPublished()
	stateTopic := "ankerctl/SN_MULTI/state"
	var lastState map[string]any
	for i := len(published) - 1; i >= 0; i-- {
		if published[i].topic == stateTopic {
			if err := json.Unmarshal([]byte(published[i].payload), &lastState); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			break
		}
	}
	if lastState == nil {
		t.Fatal("no state message found")
	}

	// All values should be present in the merged state.
	if lastState["nozzle_temp"] != float64(100) {
		t.Errorf("nozzle_temp = %v, want 100", lastState["nozzle_temp"])
	}
	if lastState["bed_temp"] != float64(50) {
		t.Errorf("bed_temp = %v, want 50", lastState["bed_temp"])
	}
	if lastState["print_status"] != "printing" {
		t.Errorf("print_status = %v, want 'printing'", lastState["print_status"])
	}
}

func TestNonEmpty(t *testing.T) {
	tests := []struct {
		v, fallback, want string
	}{
		{"hello", "default", "hello"},
		{"", "default", "default"},
		{"  ", "default", "default"},
		{"\t", "default", "default"},
		{" a ", "default", " a "},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q->%q", tc.v, tc.want), func(t *testing.T) {
			got := nonEmpty(tc.v, tc.fallback)
			if got != tc.want {
				t.Fatalf("nonEmpty(%q, %q) = %q, want %q", tc.v, tc.fallback, got, tc.want)
			}
		})
	}
}
