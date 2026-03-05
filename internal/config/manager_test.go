package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/django1982/ankerctl/internal/model"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func makeTestConfig() *model.Config {
	account := &model.Account{
		AuthToken: "tok-test",
		Region:    "eu",
		UserID:    "user-test",
		Email:     "test@example.com",
		Country:   "DE",
	}
	printers := []model.Printer{
		{ID: "p1", SN: "SN0001", Name: "Test Printer", Model: "AnkerMake M5", MQTTKey: make([]byte, 16)},
	}
	return model.NewConfig(account, printers)
}

func TestManager_LoadSave_Roundtrip(t *testing.T) {
	m := newTestManager(t)
	original := makeTestConfig()
	original.UploadRateMbps = 25

	if err := m.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil, want config")
	}
	if loaded.UploadRateMbps != 25 {
		t.Errorf("UploadRateMbps = %d, want 25", loaded.UploadRateMbps)
	}
	if loaded.Account == nil {
		t.Fatal("Account is nil after roundtrip")
	}
	if loaded.Account.UserID != original.Account.UserID {
		t.Errorf("Account.UserID = %q, want %q", loaded.Account.UserID, original.Account.UserID)
	}
	if len(loaded.Printers) != 1 {
		t.Fatalf("Printers len = %d, want 1", len(loaded.Printers))
	}
	if loaded.Printers[0].SN != "SN0001" {
		t.Errorf("Printer SN = %q, want %q", loaded.Printers[0].SN, "SN0001")
	}
}

func TestManager_Load_MissingFile_ReturnsNilNoError(t *testing.T) {
	m := newTestManager(t)
	cfg, err := m.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg != nil {
		t.Errorf("Load on missing file = %v, want nil", cfg)
	}
}

func TestManager_LoadOrDefault_MissingFile_ReturnsDefault(t *testing.T) {
	m := newTestManager(t)
	cfg, err := m.LoadOrDefault()
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadOrDefault returned nil")
	}
	if cfg.IsConfigured() {
		t.Error("default config should not be configured")
	}
}

func TestManager_Save_CreatesFileWithNewline(t *testing.T) {
	m := newTestManager(t)
	if err := m.Save(makeTestConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(m.ConfigDir(), "default.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("saved file is empty")
	}
	if data[len(data)-1] != '\n' {
		t.Error("saved file does not end with newline")
	}
}

func TestManager_Save_ValidJSON(t *testing.T) {
	m := newTestManager(t)
	if err := m.Save(makeTestConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(m.ConfigDir(), "default.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("saved file is not valid JSON: %v", err)
	}
}

func TestManager_ConfigDir_HasCorrectPermissions(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "ankerctl-test")
	m, err := NewManager(configDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_ = m

	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("config dir permissions = %04o, want 0700", mode)
	}
}

func TestManager_SetGetAPIKey(t *testing.T) {
	m := newTestManager(t)
	const key = "my-test-api-key-0123"
	if err := m.SetAPIKey(key); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}
	if got := m.GetAPIKey(); got != key {
		t.Errorf("GetAPIKey = %q, want %q", got, key)
	}
}

func TestManager_GetAPIKey_NoFile_ReturnsEmpty(t *testing.T) {
	m := newTestManager(t)
	if got := m.GetAPIKey(); got != "" {
		t.Errorf("GetAPIKey with no file = %q, want empty", got)
	}
}

func TestManager_RemoveAPIKey_ExistingKey(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetAPIKey("valid-api-key-12345"); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}
	if err := m.RemoveAPIKey(); err != nil {
		t.Fatalf("RemoveAPIKey: %v", err)
	}
	if got := m.GetAPIKey(); got != "" {
		t.Errorf("GetAPIKey after remove = %q, want empty", got)
	}
}

func TestManager_RemoveAPIKey_NonExistent_NoError(t *testing.T) {
	m := newTestManager(t)
	if err := m.RemoveAPIKey(); err != nil {
		t.Errorf("RemoveAPIKey on missing file: %v", err)
	}
}

func TestManager_SetAPIKey_FilePermissions(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetAPIKey("valid-api-key-12345"); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}

	info, err := os.Stat(filepath.Join(m.ConfigDir(), "api_key.json"))
	if err != nil {
		t.Fatalf("stat api key file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("api key file permissions = %04o, want 0600", mode)
	}
}

func TestValidateAPIKey_TooShort(t *testing.T) {
	if err := ValidateAPIKey("short"); err == nil {
		t.Error("expected error for short key, got nil")
	}
}

func TestValidateAPIKey_ExactMinLength(t *testing.T) {
	if err := ValidateAPIKey("1234567890123456"); err != nil {
		t.Errorf("ValidateAPIKey on 16-char key: %v", err)
	}
}

func TestValidateAPIKey_InvalidCharacters(t *testing.T) {
	for _, key := range []string{
		"valid-key-but-has-space ",
		"valid-key-has-@-sign@@@@",
		"valid-key-has-dot.......",
	} {
		if err := ValidateAPIKey(key); err == nil {
			t.Errorf("ValidateAPIKey(%q): expected error, got nil", key)
		}
	}
}

func TestValidateAPIKey_ValidFormats(t *testing.T) {
	for _, key := range []string{
		"abcdefghijklmnop",
		"ABCDEFGHIJKLMNOP",
		"1234567890123456",
		"my-api-key-value",
		"my_api_key_value",
		"MyApiKey-1234_xyz-abcdef",
	} {
		if err := ValidateAPIKey(key); err != nil {
			t.Errorf("ValidateAPIKey(%q): unexpected error: %v", key, err)
		}
	}
}

func TestManager_ResolveAPIKey_FileKey(t *testing.T) {
	m := newTestManager(t)
	os.Unsetenv("ANKERCTL_API_KEY")

	const key = "file-api-key-12345"
	if err := m.SetAPIKey(key); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}
	got, err := m.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if got != key {
		t.Errorf("ResolveAPIKey = %q, want %q", got, key)
	}
}

func TestManager_ResolveAPIKey_EnvOverridesFile(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetAPIKey("file-api-key-12345"); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}

	os.Setenv("ANKERCTL_API_KEY", "env-api-key-67890x")
	defer os.Unsetenv("ANKERCTL_API_KEY")

	got, err := m.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if got != "env-api-key-67890x" {
		t.Errorf("ResolveAPIKey = %q, want env key", got)
	}
}

func TestManager_ResolveAPIKey_InvalidEnvKey_ReturnsError(t *testing.T) {
	m := newTestManager(t)
	os.Setenv("ANKERCTL_API_KEY", "short")
	defer os.Unsetenv("ANKERCTL_API_KEY")

	if _, err := m.ResolveAPIKey(); err == nil {
		t.Error("ResolveAPIKey with invalid env key: expected error, got nil")
	}
}

func TestManager_ResolveAPIKey_NoKeyAnywhere_ReturnsEmpty(t *testing.T) {
	m := newTestManager(t)
	os.Unsetenv("ANKERCTL_API_KEY")

	got, err := m.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey with no key: %v", err)
	}
	if got != "" {
		t.Errorf("ResolveAPIKey with no key = %q, want empty", got)
	}
}

func TestMergeConfigPreferences_NilNew_ReturnsNil(t *testing.T) {
	if result := MergeConfigPreferences(makeTestConfig(), nil); result != nil {
		t.Errorf("MergeConfigPreferences(existing, nil) = %v, want nil", result)
	}
}

func TestMergeConfigPreferences_NilExisting_ReturnsNew(t *testing.T) {
	newCfg := makeTestConfig()
	if result := MergeConfigPreferences(nil, newCfg); result != newCfg {
		t.Error("MergeConfigPreferences(nil, new) should return new unchanged")
	}
}

func TestMergeConfigPreferences_PreservesUploadRate(t *testing.T) {
	existing := makeTestConfig()
	existing.UploadRateMbps = 50
	newCfg := makeTestConfig()
	newCfg.UploadRateMbps = 10

	result := MergeConfigPreferences(existing, newCfg)
	if result.UploadRateMbps != 50 {
		t.Errorf("UploadRateMbps = %d, want 50", result.UploadRateMbps)
	}
}

func TestMergeConfigPreferences_PreservesNotifications(t *testing.T) {
	existing := makeTestConfig()
	existing.Notifications.Apprise.Enabled = true
	existing.Notifications.Apprise.ServerURL = "https://notify.example.com"

	newCfg := makeTestConfig()
	newCfg.Notifications = model.DefaultNotificationsConfig()

	result := MergeConfigPreferences(existing, newCfg)
	if !result.Notifications.Apprise.Enabled {
		t.Error("Notifications.Apprise.Enabled = false, want true (from existing)")
	}
	if result.Notifications.Apprise.ServerURL != "https://notify.example.com" {
		t.Errorf("Notifications.Apprise.ServerURL = %q, want preserved value",
			result.Notifications.Apprise.ServerURL)
	}
}

func TestMergeConfigPreferences_PreservesTimelapse(t *testing.T) {
	existing := makeTestConfig()
	existing.Timelapse.Enabled = true
	existing.Timelapse.Interval = 60
	existing.Timelapse.MaxVideos = 5

	newCfg := makeTestConfig()
	newCfg.Timelapse = model.DefaultTimelapseConfig()

	result := MergeConfigPreferences(existing, newCfg)
	if !result.Timelapse.Enabled {
		t.Error("Timelapse.Enabled = false, want true (from existing)")
	}
	if result.Timelapse.Interval != 60 {
		t.Errorf("Timelapse.Interval = %d, want 60", result.Timelapse.Interval)
	}
	if result.Timelapse.MaxVideos != 5 {
		t.Errorf("Timelapse.MaxVideos = %d, want 5", result.Timelapse.MaxVideos)
	}
}

func TestMergeConfigPreferences_PreservesHomeAssistant(t *testing.T) {
	existing := makeTestConfig()
	existing.HomeAssistant.Enabled = true
	existing.HomeAssistant.MQTTHost = "ha.local"
	existing.HomeAssistant.MQTTPort = 1884

	newCfg := makeTestConfig()
	newCfg.HomeAssistant = model.DefaultHomeAssistantConfig()

	result := MergeConfigPreferences(existing, newCfg)
	if !result.HomeAssistant.Enabled {
		t.Error("HomeAssistant.Enabled = false, want true (from existing)")
	}
	if result.HomeAssistant.MQTTHost != "ha.local" {
		t.Errorf("HomeAssistant.MQTTHost = %q, want %q", result.HomeAssistant.MQTTHost, "ha.local")
	}
	if result.HomeAssistant.MQTTPort != 1884 {
		t.Errorf("HomeAssistant.MQTTPort = %d, want 1884", result.HomeAssistant.MQTTPort)
	}
}

func TestMergeConfigPreferences_PrintersFromNew(t *testing.T) {
	existing := makeTestConfig()
	existing.Printers = []model.Printer{{SN: "OLD-SN"}}

	newCfg := makeTestConfig()
	newCfg.Printers = []model.Printer{{SN: "NEW-SN-1"}, {SN: "NEW-SN-2"}}

	result := MergeConfigPreferences(existing, newCfg)
	if len(result.Printers) != 2 {
		t.Fatalf("Printers len = %d, want 2", len(result.Printers))
	}
	if result.Printers[0].SN != "NEW-SN-1" {
		t.Errorf("Printers[0].SN = %q, want NEW-SN-1", result.Printers[0].SN)
	}
}

func TestGetPrinterIPs_NilConfig_ReturnsEmptyMap(t *testing.T) {
	if ips := GetPrinterIPs(nil); len(ips) != 0 {
		t.Errorf("GetPrinterIPs(nil) = %v, want empty map", ips)
	}
}

func TestGetPrinterIPs_ExtractsNonEmptyIPs(t *testing.T) {
	cfg := &model.Config{
		Printers: []model.Printer{
			{SN: "SN001", IPAddr: "192.168.1.1"},
			{SN: "SN002", IPAddr: ""},
			{SN: "SN003", IPAddr: "192.168.1.3"},
		},
	}
	ips := GetPrinterIPs(cfg)
	if len(ips) != 2 {
		t.Fatalf("len = %d, want 2", len(ips))
	}
	if ips["SN001"] != "192.168.1.1" {
		t.Errorf("ips[SN001] = %q, want 192.168.1.1", ips["SN001"])
	}
	if ips["SN003"] != "192.168.1.3" {
		t.Errorf("ips[SN003] = %q, want 192.168.1.3", ips["SN003"])
	}
	if _, ok := ips["SN002"]; ok {
		t.Error("ips[SN002] should not exist (empty IP)")
	}
}

func TestUpdateEmptyPrinterIPs_FillsMissingIPs(t *testing.T) {
	cfg := &model.Config{
		Printers: []model.Printer{
			{SN: "SN001", IPAddr: ""},
			{SN: "SN002", IPAddr: "192.168.1.2"},
			{SN: "SN003", IPAddr: ""},
		},
	}
	UpdateEmptyPrinterIPs(cfg, map[string]string{
		"SN001": "10.0.0.1",
		"SN003": "10.0.0.3",
		"SN099": "10.0.0.99",
	})

	if cfg.Printers[0].IPAddr != "10.0.0.1" {
		t.Errorf("Printers[0].IPAddr = %q, want 10.0.0.1", cfg.Printers[0].IPAddr)
	}
	if cfg.Printers[1].IPAddr != "192.168.1.2" {
		t.Errorf("Printers[1].IPAddr = %q, want 192.168.1.2 (unchanged)", cfg.Printers[1].IPAddr)
	}
	if cfg.Printers[2].IPAddr != "10.0.0.3" {
		t.Errorf("Printers[2].IPAddr = %q, want 10.0.0.3", cfg.Printers[2].IPAddr)
	}
}

func TestUpdateEmptyPrinterIPs_NilConfig_NoOp(t *testing.T) {
	// Must not panic
	UpdateEmptyPrinterIPs(nil, map[string]string{"SN": "1.2.3.4"})
}

func TestUpdateEmptyPrinterIPs_DoesNotOverwriteExistingIP(t *testing.T) {
	cfg := &model.Config{
		Printers: []model.Printer{{SN: "SN001", IPAddr: "192.168.1.1"}},
	}
	UpdateEmptyPrinterIPs(cfg, map[string]string{"SN001": "10.0.0.1"})
	if cfg.Printers[0].IPAddr != "192.168.1.1" {
		t.Errorf("IPAddr was overwritten: got %q, want 192.168.1.1", cfg.Printers[0].IPAddr)
	}
}

func TestManager_Modify_UpdatesConfig(t *testing.T) {
	m := newTestManager(t)
	original := makeTestConfig()
	original.UploadRateMbps = 10
	if err := m.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	err := m.Modify(func(cfg *model.Config) (*model.Config, error) {
		cfg.UploadRateMbps = 100
		return cfg, nil
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load after Modify: %v", err)
	}
	if loaded.UploadRateMbps != 100 {
		t.Errorf("UploadRateMbps after Modify = %d, want 100", loaded.UploadRateMbps)
	}
}
