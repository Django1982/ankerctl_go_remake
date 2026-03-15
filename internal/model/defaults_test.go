package model

import (
	"os"
	"testing"
)

func TestDefaultUploadRateMbps(t *testing.T) {
	if DefaultUploadRateMbps != 10 {
		t.Errorf("DefaultUploadRateMbps = %d, want 10", DefaultUploadRateMbps)
	}
}

func TestDefaultTimelapseConfig_PureDefaults(t *testing.T) {
	for _, key := range []string{
		"TIMELAPSE_INTERVAL_SEC", "TIMELAPSE_MAX_VIDEOS", "TIMELAPSE_ENABLED",
		"TIMELAPSE_SAVE_PERSISTENT", "TIMELAPSE_CAPTURES_DIR", "TIMELAPSE_LIGHT",
	} {
		os.Unsetenv(key)
	}

	cfg := DefaultTimelapseConfig()

	if cfg.Interval != 30 {
		t.Errorf("Interval = %d, want 30", cfg.Interval)
	}
	if cfg.MaxVideos != 10 {
		t.Errorf("MaxVideos = %d, want 10", cfg.MaxVideos)
	}
	if cfg.Enabled {
		t.Error("Enabled = true, want false")
	}
	if !cfg.SavePersistent {
		t.Error("SavePersistent = false, want true")
	}
	// Default should be user config dir based, not hardcoded /captures.
	expected := defaultCapturesDir()
	if cfg.OutputDir != expected {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, expected)
	}
	if cfg.Light != nil {
		t.Errorf("Light = %v, want nil", cfg.Light)
	}
}

func TestDefaultTimelapseConfig_LightEnvSet(t *testing.T) {
	os.Setenv("TIMELAPSE_LIGHT", "on")
	defer os.Unsetenv("TIMELAPSE_LIGHT")

	cfg := DefaultTimelapseConfig()
	if cfg.Light == nil {
		t.Fatal("Light = nil, want non-nil pointer")
	}
	if *cfg.Light != "on" {
		t.Errorf("*Light = %q, want %q", *cfg.Light, "on")
	}
}

func TestDefaultHomeAssistantConfig_Defaults(t *testing.T) {
	for _, key := range []string{
		"HA_MQTT_ENABLED", "HA_MQTT_HOST", "HA_MQTT_PORT",
		"HA_MQTT_USER", "HA_MQTT_PASSWORD", "HA_MQTT_DISCOVERY_PREFIX",
	} {
		os.Unsetenv(key)
	}

	cfg := DefaultHomeAssistantConfig()

	if cfg.Enabled {
		t.Error("Enabled = true, want false")
	}
	if cfg.MQTTHost != "localhost" {
		t.Errorf("MQTTHost = %q, want %q", cfg.MQTTHost, "localhost")
	}
	if cfg.MQTTPort != 1883 {
		t.Errorf("MQTTPort = %d, want 1883", cfg.MQTTPort)
	}
	if cfg.DiscoveryPrefix != "homeassistant" {
		t.Errorf("DiscoveryPrefix = %q, want %q", cfg.DiscoveryPrefix, "homeassistant")
	}
	if cfg.NodeID != "ankermake_m5" {
		t.Errorf("NodeID = %q, want %q", cfg.NodeID, "ankermake_m5")
	}
}

func TestEnvBool_TrueValues(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE", "1", "yes", "Yes", "YES"} {
		t.Run(v, func(t *testing.T) {
			os.Setenv("TEST_BOOL_KEY", v)
			defer os.Unsetenv("TEST_BOOL_KEY")
			if !envBool("TEST_BOOL_KEY", false) {
				t.Errorf("envBool(%q) = false, want true", v)
			}
		})
	}
}

func TestEnvBool_FalseValues(t *testing.T) {
	for _, v := range []string{"false", "False", "FALSE", "0", "no", "No", "NO", "off"} {
		t.Run(v, func(t *testing.T) {
			os.Setenv("TEST_BOOL_KEY", v)
			defer os.Unsetenv("TEST_BOOL_KEY")
			if envBool("TEST_BOOL_KEY", true) {
				t.Errorf("envBool(%q) = true, want false", v)
			}
		})
	}
}

func TestEnvBool_Empty_ReturnsDefault(t *testing.T) {
	os.Unsetenv("TEST_BOOL_KEY")
	if !envBool("TEST_BOOL_KEY", true) {
		t.Error("envBool(unset, default=true) = false, want true")
	}
	if envBool("TEST_BOOL_KEY", false) {
		t.Error("envBool(unset, default=false) = true, want false")
	}
}

func TestEnvInt_ValidNumber(t *testing.T) {
	os.Setenv("TEST_INT_KEY", "42")
	defer os.Unsetenv("TEST_INT_KEY")
	if got := envInt("TEST_INT_KEY", 0); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
}

func TestEnvInt_InvalidString_ReturnsDefault(t *testing.T) {
	os.Setenv("TEST_INT_KEY", "abc")
	defer os.Unsetenv("TEST_INT_KEY")
	if got := envInt("TEST_INT_KEY", 99); got != 99 {
		t.Errorf("envInt(%q) = %d, want 99 (default)", "abc", got)
	}
}

func TestEnvInt_MixedString_ReturnsDefault(t *testing.T) {
	os.Setenv("TEST_INT_KEY", "42abc")
	defer os.Unsetenv("TEST_INT_KEY")
	if got := envInt("TEST_INT_KEY", 7); got != 7 {
		t.Errorf("envInt(%q) = %d, want 7 (default)", "42abc", got)
	}
}

func TestEnvInt_Empty_ReturnsDefault(t *testing.T) {
	os.Unsetenv("TEST_INT_KEY")
	if got := envInt("TEST_INT_KEY", 55); got != 55 {
		t.Errorf("envInt(unset) = %d, want 55", got)
	}
}

func TestEnvInt_Zero(t *testing.T) {
	os.Setenv("TEST_INT_KEY", "0")
	defer os.Unsetenv("TEST_INT_KEY")
	if got := envInt("TEST_INT_KEY", 99); got != 0 {
		t.Errorf("envInt(%q) = %d, want 0", "0", got)
	}
}

func TestEnvString_Set(t *testing.T) {
	os.Setenv("TEST_STR_KEY", "hello")
	defer os.Unsetenv("TEST_STR_KEY")
	if got := envString("TEST_STR_KEY", "default"); got != "hello" {
		t.Errorf("envString = %q, want %q", got, "hello")
	}
}

func TestEnvString_Empty_ReturnsDefault(t *testing.T) {
	os.Unsetenv("TEST_STR_KEY")
	if got := envString("TEST_STR_KEY", "default"); got != "default" {
		t.Errorf("envString(unset) = %q, want %q", got, "default")
	}
}

func TestDefaultAppriseConfig_Events_AllEnabled(t *testing.T) {
	cfg := DefaultAppriseConfig()
	if !cfg.Events.PrintStarted {
		t.Error("Events.PrintStarted = false, want true")
	}
	if !cfg.Events.PrintFinished {
		t.Error("Events.PrintFinished = false, want true")
	}
	if !cfg.Events.PrintFailed {
		t.Error("Events.PrintFailed = false, want true")
	}
	if !cfg.Events.PrintPaused {
		t.Error("Events.PrintPaused = false, want true")
	}
	if !cfg.Events.PrintResumed {
		t.Error("Events.PrintResumed = false, want true")
	}
	if !cfg.Events.GcodeUploaded {
		t.Error("Events.GcodeUploaded = false, want true")
	}
	if !cfg.Events.PrintProgress {
		t.Error("Events.PrintProgress = false, want true")
	}
}

func TestDefaultAppriseConfig_NotEnabled(t *testing.T) {
	cfg := DefaultAppriseConfig()
	if cfg.Enabled {
		t.Error("Apprise Enabled = true by default, want false")
	}
}

func TestDefaultAppriseConfig_Progress_Interval(t *testing.T) {
	cfg := DefaultAppriseConfig()
	if cfg.Progress.IntervalPercent != 25 {
		t.Errorf("Progress.IntervalPercent = %d, want 25", cfg.Progress.IntervalPercent)
	}
}

func TestDefaultFilamentServiceConfig_PureDefaults(t *testing.T) {
	os.Unsetenv("FILAMENT_ALLOW_LEGACY_SWAP")
	os.Unsetenv("FILAMENT_MANUAL_SWAP_PREHEAT_TEMP_C")

	cfg := DefaultFilamentServiceConfig()

	if cfg.AllowLegacySwap {
		t.Error("AllowLegacySwap = true by default, want false")
	}
	if cfg.ManualSwapPreheatTempC != 140 {
		t.Errorf("ManualSwapPreheatTempC = %d, want 140", cfg.ManualSwapPreheatTempC)
	}
}

func TestDefaultFilamentServiceConfig_EnvOverride(t *testing.T) {
	os.Setenv("FILAMENT_ALLOW_LEGACY_SWAP", "true")
	os.Setenv("FILAMENT_MANUAL_SWAP_PREHEAT_TEMP_C", "145")
	defer func() {
		os.Unsetenv("FILAMENT_ALLOW_LEGACY_SWAP")
		os.Unsetenv("FILAMENT_MANUAL_SWAP_PREHEAT_TEMP_C")
	}()

	cfg := DefaultFilamentServiceConfig()

	if !cfg.AllowLegacySwap {
		t.Error("AllowLegacySwap = false, want true (from env)")
	}
	if cfg.ManualSwapPreheatTempC != 145 {
		t.Errorf("ManualSwapPreheatTempC = %d, want 145", cfg.ManualSwapPreheatTempC)
	}
}

func TestClampManualSwapPreheatTempC(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{100, 130}, // below min → min
		{130, 130}, // at min
		{140, 140}, // default
		{150, 150}, // at max
		{200, 150}, // above max → max
		{0, 130},   // zero → min
	}
	for _, tt := range tests {
		got := ClampManualSwapPreheatTempC(tt.input)
		if got != tt.want {
			t.Errorf("ClampManualSwapPreheatTempC(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDefaultFilamentServiceConfig_PreheatClampedFromEnv(t *testing.T) {
	os.Setenv("FILAMENT_MANUAL_SWAP_PREHEAT_TEMP_C", "200") // above max
	defer os.Unsetenv("FILAMENT_MANUAL_SWAP_PREHEAT_TEMP_C")

	cfg := DefaultFilamentServiceConfig()
	if cfg.ManualSwapPreheatTempC != 150 {
		t.Errorf("ManualSwapPreheatTempC = %d, want 150 (clamped)", cfg.ManualSwapPreheatTempC)
	}
}
