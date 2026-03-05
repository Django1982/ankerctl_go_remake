package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfig_MarshalUnmarshal_Roundtrip(t *testing.T) {
	account := &Account{
		AuthToken: "tok-abc123",
		Region:    "eu",
		UserID:    "user-42",
		Email:     "user@example.com",
		Country:   "DE",
	}
	printers := []Printer{
		{
			ID:      "printer-id-1",
			SN:      "SN123456789",
			Name:    "My Printer",
			Model:   "AnkerMake M5",
			WifiMAC: "aa:bb:cc:dd:ee:ff",
			IPAddr:  "192.168.1.100",
			MQTTKey: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
			APIHosts: "api.example.com",
			P2PHosts: "p2p.example.com",
			P2PDUID:  "duid-xyz",
			P2PKey:   "p2pkey",
			P2PDID:   "did-123",
		},
	}
	original := NewConfig(account, printers)
	original.UploadRateMbps = 25

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Account == nil {
		t.Fatal("account is nil after roundtrip")
	}
	if restored.Account.UserID != account.UserID {
		t.Errorf("account.UserID = %q, want %q", restored.Account.UserID, account.UserID)
	}
	if restored.Account.Email != account.Email {
		t.Errorf("account.Email = %q, want %q", restored.Account.Email, account.Email)
	}
	if len(restored.Printers) != 1 {
		t.Fatalf("printers len = %d, want 1", len(restored.Printers))
	}
	if restored.Printers[0].SN != printers[0].SN {
		t.Errorf("printer SN = %q, want %q", restored.Printers[0].SN, printers[0].SN)
	}
	if restored.UploadRateMbps != 25 {
		t.Errorf("UploadRateMbps = %d, want 25", restored.UploadRateMbps)
	}
}

func TestConfig_Marshal_TypeField(t *testing.T) {
	cfg := NewConfig(nil, []Printer{})

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	typ, ok := raw["__type__"]
	if !ok {
		t.Fatal("__type__ field missing from JSON output")
	}
	if typ != "Config" {
		t.Errorf("__type__ = %q, want %q", typ, "Config")
	}
}

func TestConfig_Unmarshal_TypeFieldIgnored(t *testing.T) {
	jsonData := `{
		"__type__": "Config",
		"account": null,
		"printers": [],
		"upload_rate_mbps": 10,
		"active_printer_index": 0
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("unmarshal with __type__: %v", err)
	}
	if cfg.UploadRateMbps != 10 {
		t.Errorf("UploadRateMbps = %d, want 10", cfg.UploadRateMbps)
	}
}

func TestConfig_Unmarshal_DefaultsApplied(t *testing.T) {
	jsonData := `{
		"__type__": "Config",
		"account": null,
		"printers": [],
		"upload_rate_mbps": 10,
		"active_printer_index": 0
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	defaultNotif := DefaultNotificationsConfig()
	if cfg.Notifications.Apprise.Progress.IntervalPercent != defaultNotif.Apprise.Progress.IntervalPercent {
		t.Errorf("Notifications.Apprise.Progress.IntervalPercent = %d, want %d",
			cfg.Notifications.Apprise.Progress.IntervalPercent,
			defaultNotif.Apprise.Progress.IntervalPercent)
	}

	defaultTL := DefaultTimelapseConfig()
	if cfg.Timelapse.Interval != defaultTL.Interval {
		t.Errorf("Timelapse.Interval = %d, want %d", cfg.Timelapse.Interval, defaultTL.Interval)
	}
	if cfg.Timelapse.MaxVideos != defaultTL.MaxVideos {
		t.Errorf("Timelapse.MaxVideos = %d, want %d", cfg.Timelapse.MaxVideos, defaultTL.MaxVideos)
	}

	defaultHA := DefaultHomeAssistantConfig()
	if cfg.HomeAssistant.DiscoveryPrefix != defaultHA.DiscoveryPrefix {
		t.Errorf("HomeAssistant.DiscoveryPrefix = %q, want %q",
			cfg.HomeAssistant.DiscoveryPrefix, defaultHA.DiscoveryPrefix)
	}
}

func TestConfig_Unmarshal_ZeroUploadRate_GetsDefault(t *testing.T) {
	jsonData := `{"account": null, "printers": [], "upload_rate_mbps": 0, "active_printer_index": 0}`
	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.UploadRateMbps != DefaultUploadRateMbps {
		t.Errorf("UploadRateMbps = %d, want default %d", cfg.UploadRateMbps, DefaultUploadRateMbps)
	}
}

func TestConfig_Unmarshal_NegativeActivePrinterIndex_ClampedToZero(t *testing.T) {
	jsonData := `{"account": null, "printers": [], "upload_rate_mbps": 10, "active_printer_index": -5}`
	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.ActivePrinterIndex != 0 {
		t.Errorf("ActivePrinterIndex = %d, want 0 (clamped)", cfg.ActivePrinterIndex)
	}
}

func TestConfig_ActivePrinter_NilConfig(t *testing.T) {
	var cfg *Config
	if p := cfg.ActivePrinter(); p != nil {
		t.Errorf("ActivePrinter on nil Config = %v, want nil", p)
	}
}

func TestConfig_ActivePrinter_EmptyList(t *testing.T) {
	cfg := NewConfig(nil, []Printer{})
	if p := cfg.ActivePrinter(); p != nil {
		t.Errorf("ActivePrinter on empty list = %v, want nil", p)
	}
}

func TestConfig_ActivePrinter_ValidIndex(t *testing.T) {
	printers := []Printer{{SN: "SN0001"}, {SN: "SN0002"}}
	cfg := NewConfig(nil, printers)
	cfg.ActivePrinterIndex = 1

	p := cfg.ActivePrinter()
	if p == nil {
		t.Fatal("ActivePrinter returned nil, want SN0002")
	}
	if p.SN != "SN0002" {
		t.Errorf("ActivePrinter.SN = %q, want %q", p.SN, "SN0002")
	}
}

func TestConfig_ActivePrinter_IndexBelowZero_ReturnsFirst(t *testing.T) {
	printers := []Printer{{SN: "SN0001"}, {SN: "SN0002"}}
	cfg := NewConfig(nil, printers)
	cfg.ActivePrinterIndex = -1

	p := cfg.ActivePrinter()
	if p == nil {
		t.Fatal("ActivePrinter returned nil")
	}
	if p.SN != "SN0001" {
		t.Errorf("ActivePrinter.SN = %q, want %q", p.SN, "SN0001")
	}
}

func TestConfig_ActivePrinter_IndexBeyondLen_ReturnsFirst(t *testing.T) {
	printers := []Printer{{SN: "SN0001"}}
	cfg := NewConfig(nil, printers)
	cfg.ActivePrinterIndex = 99

	p := cfg.ActivePrinter()
	if p == nil {
		t.Fatal("ActivePrinter returned nil")
	}
	if p.SN != "SN0001" {
		t.Errorf("ActivePrinter.SN = %q, want %q", p.SN, "SN0001")
	}
}

func TestConfig_IsConfigured_Nil(t *testing.T) {
	var cfg *Config
	if cfg.IsConfigured() {
		t.Error("IsConfigured on nil Config = true, want false")
	}
}

func TestConfig_IsConfigured_NilAccount(t *testing.T) {
	cfg := NewConfig(nil, []Printer{})
	if cfg.IsConfigured() {
		t.Error("IsConfigured with nil account = true, want false")
	}
}

func TestConfig_IsConfigured_WithAccount(t *testing.T) {
	account := &Account{UserID: "user-1", Email: "user@example.com"}
	cfg := NewConfig(account, []Printer{})
	if !cfg.IsConfigured() {
		t.Error("IsConfigured with valid account = false, want true")
	}
}

func TestConfig_Unmarshal_InvalidJSON_ReturnsError(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{invalid`), &cfg); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestConfig_Marshal_NullAccount_RoundtripOk(t *testing.T) {
	cfg := NewConfig(nil, []Printer{})

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"account":null`) {
		t.Errorf("JSON does not contain null account: %s", data)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.Account != nil {
		t.Errorf("Account = %+v, want nil", restored.Account)
	}
}
