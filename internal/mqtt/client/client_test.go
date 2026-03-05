package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

func TestDecodePayloadObjects_Single(t *testing.T) {
	objs, err := decodePayloadObjects([]byte(`{"commandType":1000,"value":1}`))
	if err != nil {
		t.Fatalf("decodePayloadObjects: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("len = %d, want 1", len(objs))
	}
	if got, ok := objs[0]["commandType"].(float64); !ok || int(got) != 1000 {
		t.Fatalf("commandType = %v, want 1000", objs[0]["commandType"])
	}
}

func TestDecodePayloadObjects_List(t *testing.T) {
	objs, err := decodePayloadObjects([]byte(`[{"commandType":1000},{"commandType":1001}]`))
	if err != nil {
		t.Fatalf("decodePayloadObjects: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("len = %d, want 2", len(objs))
	}
}

func TestHandleIncoming_QueuesMessage(t *testing.T) {
	key := []byte("0123456789abcdef")
	c := &Client{printerSN: "SN123", key: key, guid: "123e4567-e89b-12d3-a456-426614174000"}

	pkt := protocol.NewPacket(c.guid, []byte(`{"commandType":1000,"value":1}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	if err := c.handleIncoming(protocol.TopicNotice(c.printerSN), wire); err != nil {
		t.Fatalf("handleIncoming: %v", err)
	}

	q := c.Fetch()
	if len(q) != 1 {
		t.Fatalf("queue len = %d, want 1", len(q))
	}
	if q[0].Topic != protocol.TopicNotice(c.printerSN) {
		t.Fatalf("topic = %q", q[0].Topic)
	}
	if len(q[0].Objects) != 1 {
		t.Fatalf("objects len = %d, want 1", len(q[0].Objects))
	}
}

func TestConnectSubscribesAllTopics(t *testing.T) {
	tr := &mockTransport{}
	c, err := New("SN0001", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "123e4567-e89b-12d3-a456-426614174000",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if !tr.connected {
		t.Fatal("transport not connected")
	}
	if len(tr.subscriptions) != 3 {
		t.Fatalf("subscriptions = %d, want 3", len(tr.subscriptions))
	}
}

func TestSendPublishesEncryptedPacket(t *testing.T) {
	tr := &mockTransport{}
	c, err := New("SN0001", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "123e4567-e89b-12d3-a456-426614174000",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := c.Command(context.Background(), map[string]any{"commandType": 1000}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(tr.published) != 1 {
		t.Fatalf("published = %d, want 1", len(tr.published))
	}
	if tr.published[0].topic != "/device/maker/SN0001/command" {
		t.Fatalf("topic = %q", tr.published[0].topic)
	}
	if len(tr.published[0].payload) == 0 {
		t.Fatal("payload is empty")
	}
}

func TestNewValidation(t *testing.T) {
	_, err := New("", []byte("0123456789abcdef"), Config{Broker: "x", Transport: &mockTransport{}})
	if err == nil {
		t.Fatal("expected error for empty serial")
	}
	_, err = New("SN", nil, Config{Broker: "x", Transport: &mockTransport{}})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	_, err = New("SN", []byte("k"), Config{Broker: "", Transport: &mockTransport{}})
	if err == nil {
		t.Fatal("expected error for empty broker")
	}
	_, err = New("SN", []byte("k"), Config{Broker: "x"})
	if err == nil {
		t.Fatal("expected error for nil transport")
	}
}

type publishCall struct {
	topic   string
	payload []byte
}

type mockTransport struct {
	connected     bool
	subscriptions map[string]MessageHandler
	published     []publishCall

	connectErr   error
	subscribeErr error
	publishErr   error
}

func (m *mockTransport) Connect(_ context.Context) error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockTransport) Disconnect(_ time.Duration) {
	m.connected = false
}

func (m *mockTransport) Publish(_ context.Context, topic string, payload []byte) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, publishCall{topic: topic, payload: append([]byte(nil), payload...)})
	return nil
}

func (m *mockTransport) Subscribe(_ context.Context, topic string, handler MessageHandler) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	if m.subscriptions == nil {
		m.subscriptions = make(map[string]MessageHandler)
	}
	m.subscriptions[topic] = handler
	return nil
}

func TestConnectErrorPropagation(t *testing.T) {
	tr := &mockTransport{connectErr: errors.New("boom")}
	c, err := New("SN", []byte("0123456789abcdef"), Config{Broker: "x", Transport: tr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("expected connect error")
	}
}
