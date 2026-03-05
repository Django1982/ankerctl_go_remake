package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var knownMQTTKey = []byte{
	0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe,
	0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
}

const knownMQTTKeyHex = "deadbeefcafebabe0123456789abcdef"

func makePrinter() Printer {
	return Printer{
		ID:         "printer-id-1",
		SN:         "SN123456789",
		Name:       "My AnkerMake M5",
		Model:      "AnkerMake M5",
		CreateTime: time.Unix(1700000000, 0).UTC(),
		UpdateTime: time.Unix(1700000100, 0).UTC(),
		WifiMAC:    "aa:bb:cc:dd:ee:ff",
		IPAddr:     "192.168.1.10",
		MQTTKey:    knownMQTTKey,
		APIHosts:   "api.example.com",
		P2PHosts:   "p2p.example.com",
		P2PDUID:    "duid-abc",
		P2PKey:     "p2p-secret",
		P2PDID:     "did-xyz",
	}
}

func TestPrinter_MarshalUnmarshal_Roundtrip(t *testing.T) {
	original := makePrinter()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Printer
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"ID", restored.ID, original.ID},
		{"SN", restored.SN, original.SN},
		{"Name", restored.Name, original.Name},
		{"Model", restored.Model, original.Model},
		{"WifiMAC", restored.WifiMAC, original.WifiMAC},
		{"IPAddr", restored.IPAddr, original.IPAddr},
		{"APIHosts", restored.APIHosts, original.APIHosts},
		{"P2PHosts", restored.P2PHosts, original.P2PHosts},
		{"P2PDUID", restored.P2PDUID, original.P2PDUID},
		{"P2PKey", restored.P2PKey, original.P2PKey},
		{"P2PDID", restored.P2PDID, original.P2PDID},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestPrinter_MQTTKey_HexRoundtrip(t *testing.T) {
	original := Printer{MQTTKey: knownMQTTKey, SN: "SN000"}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), knownMQTTKeyHex) {
		t.Errorf("JSON does not contain expected hex key %q: %s", knownMQTTKeyHex, data)
	}

	var restored Printer
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(restored.MQTTKey) != len(knownMQTTKey) {
		t.Fatalf("MQTTKey length = %d, want %d", len(restored.MQTTKey), len(knownMQTTKey))
	}
	for i, b := range knownMQTTKey {
		if restored.MQTTKey[i] != b {
			t.Errorf("MQTTKey[%d] = 0x%02x, want 0x%02x", i, restored.MQTTKey[i], b)
		}
	}
}

func TestPrinter_MQTTKey_InvalidHex_ReturnsError(t *testing.T) {
	jsonData := `{
		"id":"p1","sn":"SN1","name":"P","model":"M",
		"create_time":0,"update_time":0,
		"wifi_mac":"","ip_addr":"",
		"mqtt_key":"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
		"api_hosts":"","p2p_hosts":"","p2p_duid":"","p2p_key":""
	}`
	var p Printer
	if err := json.Unmarshal([]byte(jsonData), &p); err == nil {
		t.Error("expected error for invalid hex mqtt_key, got nil")
	}
}

func TestPrinter_Marshal_TypeField(t *testing.T) {
	p := makePrinter()

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	typ, ok := raw["__type__"]
	if !ok {
		t.Fatal("__type__ field missing from Printer JSON output")
	}
	if typ != "Printer" {
		t.Errorf("__type__ = %q, want %q", typ, "Printer")
	}
}

func TestPrinter_Timestamp_UnixFloat_Precision(t *testing.T) {
	// Use .5 seconds — well within float64 precision
	baseTime := time.Unix(1700000000, 500_000_000).UTC()

	p := Printer{
		SN:         "SN_TS",
		CreateTime: baseTime,
		UpdateTime: baseTime,
		MQTTKey:    []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Printer
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.CreateTime.Unix() != baseTime.Unix() {
		t.Errorf("CreateTime.Unix() = %d, want %d", restored.CreateTime.Unix(), baseTime.Unix())
	}
	origMs := baseTime.UnixMilli()
	restoredMs := restored.CreateTime.UnixMilli()
	if origMs != restoredMs {
		t.Errorf("CreateTime milliseconds = %d, want %d", restoredMs, origMs)
	}
}

func TestPrinter_Timestamp_Zero_Roundtrip(t *testing.T) {
	p := Printer{
		SN:         "SN_ZERO_TS",
		CreateTime: time.Unix(0, 0).UTC(),
		UpdateTime: time.Unix(0, 0).UTC(),
		MQTTKey:    make([]byte, 16),
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Printer
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.CreateTime.Unix() != 0 {
		t.Errorf("CreateTime.Unix() = %d, want 0", restored.CreateTime.Unix())
	}
}

func TestPrinter_EmptyMQTTKey_Roundtrip(t *testing.T) {
	p := Printer{SN: "SN_EMPTY", MQTTKey: []byte{}}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Printer
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(restored.MQTTKey) != 0 {
		t.Errorf("MQTTKey len = %d, want 0", len(restored.MQTTKey))
	}
}

func TestTimeFromUnixFloat_IntegerTimestamp(t *testing.T) {
	result := timeFromUnixFloat(1700000000.0)
	if result.Unix() != 1700000000 {
		t.Errorf("Unix() = %d, want 1700000000", result.Unix())
	}
	if result.Nanosecond() != 0 {
		t.Errorf("Nanosecond() = %d, want 0", result.Nanosecond())
	}
}

func TestTimeFromUnixFloat_FractionalTimestamp(t *testing.T) {
	result := timeFromUnixFloat(1700000000.5)
	if result.Unix() != 1700000000 {
		t.Errorf("Unix() = %d, want 1700000000", result.Unix())
	}
	nsec := result.Nanosecond()
	if nsec < 499_000_000 || nsec > 501_000_000 {
		t.Errorf("Nanosecond() = %d, want ~500000000", nsec)
	}
}
