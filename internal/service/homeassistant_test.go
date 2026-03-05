package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/django1982/ankerctl/internal/model"
)

type fakeToken struct{ err error }

func (t fakeToken) WaitTimeout(timeout time.Duration) bool { return true }
func (t fakeToken) Error() error                           { return t.err }

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
}

func newFakeHAClient() *fakeHAClient {
	return &fakeHAClient{connected: true, onSubscribe: make(map[string]paho.MessageHandler)}
}

func (c *fakeHAClient) Connect() HomeAssistantToken { return fakeToken{} }
func (c *fakeHAClient) Disconnect(quiesce uint)     { c.connected = false }
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
func (c *fakeHAClient) IsConnected() bool { return c.connected }

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

func TestHomeAssistantPublishesDiscoveryAndHeartbeat(t *testing.T) {
	cfg := model.HomeAssistantConfig{Enabled: true, MQTTHost: "ha.local", MQTTPort: 1883, DiscoveryPrefix: "homeassistant"}
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN1", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient {
		return client
	}
	svc.heartbeatInterval = 20 * time.Millisecond

	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	// Simulate paho on-connect callback.
	svc.onConnected()
	time.Sleep(80 * time.Millisecond)

	client.mu.Lock()
	published := append([]publishedMsg(nil), client.published...)
	client.mu.Unlock()

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
	cfg := model.HomeAssistantConfig{Enabled: true, MQTTHost: "ha.local", MQTTPort: 1883, DiscoveryPrefix: "homeassistant"}
	light := &fakeLight{}
	svc := NewHomeAssistantService(cfg, "SN2", "Printer", light)
	client := newFakeHAClient()
	svc.newClient = func(opts *paho.ClientOptions) HomeAssistantMQTTClient { return client }

	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)
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
