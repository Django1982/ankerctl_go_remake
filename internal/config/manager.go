package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/django1982/ankerctl/internal/model"
)

const (
	// APIKeyMinLength is the minimum allowed API key length.
	APIKeyMinLength = 16

	// defaultConfigDirName is the application config directory name.
	defaultConfigDirName = "ankerctl"

	// defaultConfigFileName is the default config file name (without extension).
	defaultConfigFileName = "default"

	// apiKeyFileName is the API key config file name (without extension).
	apiKeyFileName = "api_key"

	// configDirPermissions is the permission mode for the config directory.
	configDirPermissions = 0o700
)

// apiKeyPattern validates API key format: letters, digits, dashes, underscores.
var apiKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Manager handles reading and writing the ankerctl configuration.
// It is safe for concurrent use.
type Manager struct {
	mu        sync.RWMutex
	configDir string
	logger    *slog.Logger
}

// NewManager creates a new config Manager with the given config directory.
// The directory is created with 0700 permissions if it does not exist.
func NewManager(configDir string) (*Manager, error) {
	if err := os.MkdirAll(configDir, configDirPermissions); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	// Ensure permissions even if dir already existed
	if err := os.Chmod(configDir, configDirPermissions); err != nil {
		return nil, fmt.Errorf("chmod config directory: %w", err)
	}

	return &Manager{
		configDir: configDir,
		logger:    slog.With("component", "config"),
	}, nil
}

// NewDefaultManager creates a Manager using the default config path:
// ~/.config/ankerctl/
func NewDefaultManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}
	configDir := filepath.Join(home, ".config", defaultConfigDirName)
	return NewManager(configDir)
}

// ConfigDir returns the path to the configuration directory.
func (m *Manager) ConfigDir() string {
	return m.configDir
}

// configPath returns the full path for a config file by name.
func (m *Manager) configPath(name string) string {
	return filepath.Join(m.configDir, name+".json")
}

// Load reads and parses the default configuration file.
// Returns nil (not an error) if the file does not exist.
func (m *Manager) Load() (*model.Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.loadLocked()
}

// loadLocked reads config without acquiring the lock (caller must hold it).
func (m *Manager) loadLocked() (*model.Config, error) {
	path := m.configPath(defaultConfigFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg model.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return &cfg, nil
}

// Delete removes the default configuration file, effectively logging out.
// Returns nil if the file did not exist.
func (m *Manager) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.configPath(defaultConfigFileName)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete config: %w", err)
	}
	return nil
}

// Save writes the configuration to the default config file.
func (m *Manager) Save(cfg *model.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveLocked(cfg)
}

// saveLocked writes config without acquiring the lock (caller must hold it).
func (m *Manager) saveLocked(cfg *model.Config) error {
	path := m.configPath(defaultConfigFileName)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Append newline to match Python behavior
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// Modify loads the config, passes it to the modifier function, and saves the result.
// The modifier receives the current config (may be nil) and returns the modified config.
// This is the Go equivalent of Python's context manager pattern.
func (m *Manager) Modify(modifier func(*model.Config) (*model.Config, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.loadLocked()
	if err != nil {
		return err
	}

	modified, err := modifier(cfg)
	if err != nil {
		return err
	}

	return m.saveLocked(modified)
}

// LoadOrDefault reads the config, returning a default empty config if not found.
// This mirrors Python's config.open() which uses Config(account=None, printers=[]).
func (m *Manager) LoadOrDefault() (*model.Config, error) {
	cfg, err := m.Load()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return model.NewConfig(nil, []model.Printer{}), nil
	}
	return cfg, nil
}

// apiKeyData is the JSON structure for the API key file.
type apiKeyData struct {
	Key string `json:"key"`
}

// GetAPIKey loads the API key from config. Returns empty string if not set.
func (m *Manager) GetAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.configPath(apiKeyFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var akd apiKeyData
	if err := json.Unmarshal(data, &akd); err != nil {
		return ""
	}

	return akd.Key
}

// SetAPIKey saves the API key to its config file.
func (m *Manager) SetAPIKey(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.configPath(apiKeyFileName)
	data, err := json.MarshalIndent(apiKeyData{Key: key}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal api key: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

// RemoveAPIKey deletes the API key config file.
func (m *Manager) RemoveAPIKey() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.configPath(apiKeyFileName)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove api key: %w", err)
	}
	return nil
}

// ValidateAPIKey checks if an API key meets format requirements.
// Returns an error describing the problem, or nil if valid.
func ValidateAPIKey(key string) error {
	if len(key) < APIKeyMinLength {
		return fmt.Errorf("API key must be at least %d characters (got %d)", APIKeyMinLength, len(key))
	}
	if !apiKeyPattern.MatchString(key) {
		return fmt.Errorf("API key may only contain letters, digits, dashes and underscores [a-zA-Z0-9_-]")
	}
	return nil
}

// ResolveAPIKey resolves the effective API key.
// The ANKERCTL_API_KEY environment variable takes precedence over the config file.
// Returns empty string if no key is configured.
func (m *Manager) ResolveAPIKey() (string, error) {
	envKey := os.Getenv("ANKERCTL_API_KEY")
	if envKey != "" {
		if err := ValidateAPIKey(envKey); err != nil {
			return "", fmt.Errorf("ANKERCTL_API_KEY environment variable is invalid: %w", err)
		}
		return envKey, nil
	}
	return m.GetAPIKey(), nil
}

// MergeConfigPreferences merges user preferences from an existing config into
// a new config. This preserves settings like upload_rate_mbps, notifications,
// timelapse configuration, and Home Assistant settings when re-importing from
// the Anker API. All four preference fields represent user choices that are
// independent of the printer/account data returned by the cloud API.
func MergeConfigPreferences(existing, newConfig *model.Config) *model.Config {
	if newConfig == nil {
		return newConfig
	}
	if existing == nil {
		return newConfig
	}

	newConfig.UploadRateMbps = existing.UploadRateMbps
	newConfig.Notifications = existing.Notifications
	newConfig.Timelapse = existing.Timelapse
	newConfig.HomeAssistant = existing.HomeAssistant

	return newConfig
}

// GetPrinterIPs extracts a map of serial number -> IP address from the config.
func GetPrinterIPs(cfg *model.Config) map[string]string {
	ips := make(map[string]string)
	if cfg == nil {
		return ips
	}
	for _, p := range cfg.Printers {
		if p.IPAddr != "" {
			ips[p.SN] = p.IPAddr
		}
	}
	return ips
}

// UpdateEmptyPrinterIPs fills in missing IP addresses from a previously saved map.
func UpdateEmptyPrinterIPs(cfg *model.Config, ips map[string]string) {
	if cfg == nil {
		return
	}
	for i := range cfg.Printers {
		if cfg.Printers[i].IPAddr == "" {
			if ip, ok := ips[cfg.Printers[i].SN]; ok {
				cfg.Printers[i].IPAddr = ip
			}
		}
	}
}
