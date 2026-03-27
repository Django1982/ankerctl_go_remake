package model

import (
	"encoding/json"
	"fmt"
)

// Config is the top-level configuration structure.
// It holds account info, printer list, and feature settings.
type Config struct {
	Account            *Account               `json:"-"`
	Printers           []Printer              `json:"-"`
	UploadRateMbps     int                    `json:"-"`
	ActivePrinterIndex int                    `json:"-"`
	Notifications      NotificationsConfig    `json:"-"`
	Timelapse          TimelapseConfig        `json:"-"`
	HomeAssistant      HomeAssistantConfig    `json:"-"`
	FilamentService    FilamentServiceConfig  `json:"-"`
}

// configJSON is the JSON wire format for Config.
type configJSON struct {
	Type               string          `json:"__type__,omitempty"`
	Account            json.RawMessage `json:"account"`
	Printers           json.RawMessage `json:"printers"`
	UploadRateMbps     int             `json:"upload_rate_mbps"`
	ActivePrinterIndex int             `json:"active_printer_index"`
	Notifications      json.RawMessage `json:"notifications,omitempty"`
	Timelapse          json.RawMessage `json:"timelapse,omitempty"`
	HomeAssistant      json.RawMessage `json:"home_assistant,omitempty"`
	FilamentService    json.RawMessage `json:"filament_service,omitempty"`
}

// NewConfig creates a Config with default settings.
func NewConfig(account *Account, printers []Printer) *Config {
	return &Config{
		Account:            account,
		Printers:           printers,
		UploadRateMbps:     DefaultUploadRateMbps,
		ActivePrinterIndex: 0,
		Notifications:      DefaultNotificationsConfig(),
		Timelapse:          DefaultTimelapseConfig(),
		HomeAssistant:      DefaultHomeAssistantConfig(),
		FilamentService:    DefaultFilamentServiceConfig(),
	}
}

// ActivePrinter returns the currently active printer, or nil if none configured.
func (c *Config) ActivePrinter() *Printer {
	if c == nil || len(c.Printers) == 0 {
		return nil
	}
	idx := c.ActivePrinterIndex
	if idx < 0 || idx >= len(c.Printers) {
		idx = 0
	}
	return &c.Printers[idx]
}

// IsConfigured returns true if the config has an account set.
// Mirrors Python's Config.__bool__().
func (c *Config) IsConfigured() bool {
	return c != nil && c.Account != nil
}

// MarshalJSON implements json.Marshaler.
func (c Config) MarshalJSON() ([]byte, error) {
	accountData, err := json.Marshal(c.Account)
	if err != nil {
		return nil, fmt.Errorf("marshal account: %w", err)
	}

	printersData, err := json.Marshal(c.Printers)
	if err != nil {
		return nil, fmt.Errorf("marshal printers: %w", err)
	}

	notifData, err := json.Marshal(c.Notifications)
	if err != nil {
		return nil, fmt.Errorf("marshal notifications: %w", err)
	}

	timelapseData, err := json.Marshal(c.Timelapse)
	if err != nil {
		return nil, fmt.Errorf("marshal timelapse: %w", err)
	}

	haData, err := json.Marshal(c.HomeAssistant)
	if err != nil {
		return nil, fmt.Errorf("marshal home_assistant: %w", err)
	}

	fsData, err := json.Marshal(c.FilamentService)
	if err != nil {
		return nil, fmt.Errorf("marshal filament_service: %w", err)
	}

	return json.Marshal(configJSON{
		Type:               "Config",
		Account:            accountData,
		Printers:           printersData,
		UploadRateMbps:     c.UploadRateMbps,
		ActivePrinterIndex: c.ActivePrinterIndex,
		Notifications:      notifData,
		Timelapse:          timelapseData,
		HomeAssistant:      haData,
		FilamentService:    fsData,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
// It handles the Python-compatible __type__ field and applies defaults
// for missing optional fields, matching the Python Config.from_dict() logic.
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	// Account
	if raw.Account != nil && string(raw.Account) != "null" {
		var account Account
		if err := json.Unmarshal(raw.Account, &account); err != nil {
			return fmt.Errorf("unmarshal account: %w", err)
		}
		c.Account = &account
	}

	// Printers
	if raw.Printers != nil && string(raw.Printers) != "null" {
		if err := json.Unmarshal(raw.Printers, &c.Printers); err != nil {
			return fmt.Errorf("unmarshal printers: %w", err)
		}
	}
	if c.Printers == nil {
		c.Printers = []Printer{}
	}

	// Upload rate with default
	if raw.UploadRateMbps > 0 {
		c.UploadRateMbps = raw.UploadRateMbps
	} else {
		c.UploadRateMbps = DefaultUploadRateMbps
	}

	// Active printer index with bounds validation
	c.ActivePrinterIndex = raw.ActivePrinterIndex
	if c.ActivePrinterIndex < 0 {
		c.ActivePrinterIndex = 0
	}

	// Notifications with defaults merge
	c.Notifications = DefaultNotificationsConfig()
	if raw.Notifications != nil && string(raw.Notifications) != "null" {
		if err := json.Unmarshal(raw.Notifications, &c.Notifications); err != nil {
			// On error, keep defaults
			c.Notifications = DefaultNotificationsConfig()
		}
	}

	// Timelapse with defaults merge
	c.Timelapse = DefaultTimelapseConfig()
	if raw.Timelapse != nil && string(raw.Timelapse) != "null" {
		if err := json.Unmarshal(raw.Timelapse, &c.Timelapse); err != nil {
			c.Timelapse = DefaultTimelapseConfig()
		}
	}

	// Home Assistant with defaults merge
	c.HomeAssistant = DefaultHomeAssistantConfig()
	if raw.HomeAssistant != nil && string(raw.HomeAssistant) != "null" {
		if err := json.Unmarshal(raw.HomeAssistant, &c.HomeAssistant); err != nil {
			c.HomeAssistant = DefaultHomeAssistantConfig()
		}
	}

	// Filament service with defaults merge
	c.FilamentService = DefaultFilamentServiceConfig()
	if raw.FilamentService != nil && string(raw.FilamentService) != "null" {
		if err := json.Unmarshal(raw.FilamentService, &c.FilamentService); err != nil {
			c.FilamentService = DefaultFilamentServiceConfig()
		}
		// Always clamp preheat temp after deserialization.
		c.FilamentService.ManualSwapPreheatTempC = ClampManualSwapPreheatTempC(c.FilamentService.ManualSwapPreheatTempC)
	}

	return nil
}
