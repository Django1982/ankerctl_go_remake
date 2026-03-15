package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

var errBoom = errors.New("boom")

// ---------------------------------------------------------------------------
// handleIncoming — corrupted MQTT payloads
// ---------------------------------------------------------------------------

func TestHandleIncoming_CorruptedChecksum(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32-byte AES key

	c := &Client{printerSN: "SN001", key: key, guid: "test-guid"}

	pkt := protocol.NewPacket(c.guid, []byte(`{"commandType":1000,"value":1}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Flip the checksum byte.
	wire[len(wire)-1] ^= 0xFF

	err = c.handleIncoming(protocol.TopicNotice(c.printerSN), wire)
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum in error, got: %v", err)
	}

	// Queue must remain empty after the failure.
	if q := c.Fetch(); len(q) != 0 {
		t.Fatalf("expected empty queue after checksum failure, got %d entries", len(q))
	}
}

func TestHandleIncoming_CorruptedCiphertextBody(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN002", key: key, guid: "test-guid"}

	pkt := protocol.NewPacket(c.guid, []byte(`{"commandType":1000}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Corrupt a byte inside the encrypted body (after the 64-byte M5 header,
	// before the final checksum byte) and recompute checksum so it passes.
	headerLen := 64
	if len(wire) > headerLen+2 {
		wire[headerLen] ^= 0xFF
		var xor byte
		for _, b := range wire[:len(wire)-1] {
			xor ^= b
		}
		wire[len(wire)-1] = xor
	}

	// Decrypt will produce garbled plaintext; PKCS7 unpad may or may not fail.
	// We do not assert on the error here — the important thing is no panic.
	_ = c.handleIncoming(protocol.TopicNotice(c.printerSN), wire)
}

func TestHandleIncoming_EmptyPayload(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN003", key: key, guid: "test-guid"}

	err := c.handleIncoming(protocol.TopicNotice(c.printerSN), []byte{})
	if err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

func TestHandleIncoming_TruncatedHeader(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN004", key: key, guid: "test-guid"}

	// Build an 11-byte payload + checksum (12 bytes total).
	// After checksum removal: 11 bytes — below the 12-byte minimum.
	raw := make([]byte, 11)
	var xor byte
	for _, b := range raw {
		xor ^= b
	}
	raw = append(raw, xor)

	err := c.handleIncoming(protocol.TopicNotice(c.printerSN), raw)
	if err == nil {
		t.Fatal("expected error for truncated header, got nil")
	}
}

func TestHandleIncoming_InvalidSignatureBytes(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN005", key: key, guid: "test-guid"}

	// Build a frame with wrong MQTT signature bytes (not 'M','A').
	raw := make([]byte, 24)
	raw[0] = 0xDE // should be 'M'
	raw[1] = 0xAD // should be 'A'
	raw[6] = 2    // m5=2 (valid format byte) so packetHeaderLen passes
	var xor byte
	for _, b := range raw {
		xor ^= b
	}
	raw = append(raw, xor)

	err := c.handleIncoming(protocol.TopicNotice(c.printerSN), raw)
	if err == nil {
		t.Fatal("expected error for bad signature, got nil")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// handleIncoming — invalid JSON in decrypted body
// ---------------------------------------------------------------------------

func TestHandleIncoming_InvalidJSONBody(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN006", key: key, guid: "test-guid"}

	// Encrypt a payload that is not valid JSON.
	pkt := protocol.NewPacket(c.guid, []byte(`NOT_VALID_JSON_{{`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	err = c.handleIncoming(protocol.TopicNotice(c.printerSN), wire)
	if err == nil {
		t.Fatal("expected error for invalid JSON body, got nil")
	}
}

// ---------------------------------------------------------------------------
// handleIncoming — unknown commandType queued without error
// ---------------------------------------------------------------------------

func TestHandleIncoming_UnknownCommandTypeQueued(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN007", key: key, guid: "test-guid"}

	// ct=9999 is not in the MqttMsgType enum — but the client layer does not
	// filter by command type; the message must be queued successfully.
	pkt := protocol.NewPacket(c.guid, []byte(`{"commandType":9999,"value":123}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	if err := c.handleIncoming(protocol.TopicNotice(c.printerSN), wire); err != nil {
		t.Fatalf("handleIncoming: %v", err)
	}

	q := c.Fetch()
	if len(q) != 1 {
		t.Fatalf("expected 1 queued message for unknown ct, got %d", len(q))
	}
	ct, ok := q[0].Objects[0]["commandType"].(float64)
	if !ok || int(ct) != 9999 {
		t.Fatalf("expected commandType=9999, got %v", q[0].Objects[0]["commandType"])
	}
}

// ---------------------------------------------------------------------------
// handleIncoming — JSON array body (multiple objects in one packet)
// ---------------------------------------------------------------------------

func TestHandleIncoming_JSONArrayBody(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN008", key: key, guid: "test-guid"}

	body := []byte(`[{"commandType":1000,"value":1},{"commandType":1001,"progress":5000}]`)
	pkt := protocol.NewPacket(c.guid, body)
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	if err := c.handleIncoming(protocol.TopicNotice(c.printerSN), wire); err != nil {
		t.Fatalf("handleIncoming: %v", err)
	}

	q := c.Fetch()
	if len(q) != 1 {
		t.Fatalf("expected 1 queue entry, got %d", len(q))
	}
	if len(q[0].Objects) != 2 {
		t.Fatalf("expected 2 objects in message, got %d", len(q[0].Objects))
	}
}

// ---------------------------------------------------------------------------
// decodePayloadObjects — edge cases
// ---------------------------------------------------------------------------

func TestDecodePayloadObjects_EmptyJSONObject(t *testing.T) {
	objs, err := decodePayloadObjects([]byte(`{}`))
	if err != nil {
		t.Fatalf("expected success for empty object, got %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("expected 1 object, got %d", len(objs))
	}
}

func TestDecodePayloadObjects_BareString(t *testing.T) {
	_, err := decodePayloadObjects([]byte(`"hello"`))
	if err == nil {
		t.Fatal("expected error for bare JSON string, got nil")
	}
}

func TestDecodePayloadObjects_NullValue(t *testing.T) {
	// json.Unmarshal of null into map[string]any or []map[string]any
	// succeeds in Go (returns empty map / empty slice). The function therefore
	// returns a single empty-map entry rather than an error. Verify that
	// behaviour here so the test documents the actual contract.
	objs, err := decodePayloadObjects([]byte(`null`))
	if err != nil {
		t.Fatalf("unexpected error for null JSON: %v", err)
	}
	// Go unmarshals null into a nil map[string]any, which
	// decodePayloadObjects wraps in a one-element slice.
	if len(objs) != 1 {
		t.Fatalf("expected 1 object (nil map) for null JSON, got %d", len(objs))
	}
}

func TestDecodePayloadObjects_TotallyInvalidJSON(t *testing.T) {
	_, err := decodePayloadObjects([]byte(`{{{garbage`))
	if err == nil {
		t.Fatal("expected error for garbage JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Connect — subscribe error propagation
// ---------------------------------------------------------------------------

func TestConnect_SubscribeErrorPropagated(t *testing.T) {
	tr := &mockTransport{subscribeErr: errBoom}
	c, err := New("SN-sub-err", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "test-guid",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("expected subscribe error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Send / Command / Query — publish error propagation
// ---------------------------------------------------------------------------

func TestCommand_PublishError(t *testing.T) {
	tr := &mockTransport{publishErr: errBoom}
	c, err := New("SN-pub-err", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "test-guid",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Command(context.Background(), map[string]any{"commandType": 1000}); err == nil {
		t.Fatal("expected publish error for Command, got nil")
	}
}

func TestQuery_PublishError(t *testing.T) {
	tr := &mockTransport{publishErr: errBoom}
	c, err := New("SN-q-err", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "test-guid",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Query(context.Background(), map[string]any{"commandType": 1000}); err == nil {
		t.Fatal("expected publish error for Query, got nil")
	}
}

func TestSend_UnmarshallablePayload(t *testing.T) {
	tr := &mockTransport{}
	c, err := New("SN-marshal-err", []byte("0123456789abcdef"), Config{
		Broker:    "broker.local",
		Transport: tr,
		GUID:      "test-guid",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Functions cannot be marshalled to JSON.
	if err := c.Send(context.Background(), "/device/maker/test/command", func() {}); err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Fetch — drains queue, second call returns empty
// ---------------------------------------------------------------------------

func TestFetch_ClearsQueue(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c := &Client{printerSN: "SN-fetch", key: key, guid: "test-guid"}

	for i := 0; i < 3; i++ {
		pkt := protocol.NewPacket(c.guid, []byte(`{"commandType":1000}`))
		wire, _ := pkt.MarshalBinary(key)
		_ = c.handleIncoming(protocol.TopicNotice(c.printerSN), wire)
	}

	first := c.Fetch()
	if len(first) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(first))
	}

	// Second Fetch must return empty — queue was drained.
	second := c.Fetch()
	if len(second) != 0 {
		t.Fatalf("expected empty queue after fetch, got %d", len(second))
	}
}
