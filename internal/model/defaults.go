package model

import "os"

// DefaultUploadRateMbps is the default upload speed limit.
const DefaultUploadRateMbps = 10

// UploadRateMbpsChoices are the valid upload rate options.
var UploadRateMbpsChoices = []int{5, 10, 25, 50, 100}

// AppriseEvents holds the enabled/disabled state for each notification event.
type AppriseEvents struct {
	PrintStarted  bool `json:"print_started"`
	PrintFinished bool `json:"print_finished"`
	PrintFailed   bool `json:"print_failed"`
	PrintPaused   bool `json:"print_paused"`
	PrintResumed  bool `json:"print_resumed"`
	GcodeUploaded bool `json:"gcode_uploaded"`
	PrintProgress bool `json:"print_progress"`
}

// AppriseProgress holds progress notification settings.
type AppriseProgress struct {
	IntervalPercent  int    `json:"interval_percent"`
	IncludeImage     bool   `json:"include_image"`
	SnapshotQuality  string `json:"snapshot_quality"`
	SnapshotFallback bool   `json:"snapshot_fallback"`
}

// AppriseTemplates holds notification message templates.
type AppriseTemplates struct {
	PrintStarted  string `json:"print_started"`
	PrintFinished string `json:"print_finished"`
	PrintFailed   string `json:"print_failed"`
	PrintPaused   string `json:"print_paused"`
	PrintResumed  string `json:"print_resumed"`
	GcodeUploaded string `json:"gcode_uploaded"`
	PrintProgress string `json:"print_progress"`
}

// AppriseConfig holds all Apprise notification settings.
type AppriseConfig struct {
	Enabled   bool             `json:"enabled"`
	ServerURL string           `json:"server_url"`
	Key       string           `json:"key"`
	Tag       string           `json:"tag"`
	Events    AppriseEvents    `json:"events"`
	Progress  AppriseProgress  `json:"progress"`
	Templates AppriseTemplates `json:"templates"`
}

// NotificationsConfig wraps notification provider configs.
type NotificationsConfig struct {
	Apprise AppriseConfig `json:"apprise"`
}

// TimelapseConfig holds timelapse recording settings.
type TimelapseConfig struct {
	Enabled        bool    `json:"enabled"`
	Interval       int     `json:"interval"`
	MaxVideos      int     `json:"max_videos"`
	SavePersistent bool    `json:"save_persistent"`
	OutputDir      string  `json:"output_dir"`
	Light          *string `json:"light"` // nil = not set
}

// HomeAssistantConfig holds Home Assistant MQTT discovery settings.
type HomeAssistantConfig struct {
	Enabled         bool   `json:"enabled"`
	MQTTHost        string `json:"mqtt_host"`
	MQTTPort        int    `json:"mqtt_port"`
	MQTTUsername    string `json:"mqtt_username"`
	MQTTPassword    string `json:"mqtt_password"`
	DiscoveryPrefix string `json:"discovery_prefix"`
	NodeID          string `json:"node_id"`
}

// DefaultAppriseConfig returns the default Apprise notification configuration.
func DefaultAppriseConfig() AppriseConfig {
	return AppriseConfig{
		Enabled:   false,
		ServerURL: "",
		Key:       "",
		Tag:       "",
		Events: AppriseEvents{
			PrintStarted:  true,
			PrintFinished: true,
			PrintFailed:   true,
			PrintPaused:   true,
			PrintResumed:  true,
			GcodeUploaded: true,
			PrintProgress: true,
		},
		Progress: AppriseProgress{
			IntervalPercent:  25,
			IncludeImage:     false,
			SnapshotQuality:  "hd",
			SnapshotFallback: true,
		},
		Templates: AppriseTemplates{
			PrintStarted:  "Print started: {filename}",
			PrintFinished: "Print finished: {filename} ({duration})",
			PrintFailed:   "Print failed: {filename} ({reason})",
			PrintPaused:   "Print paused: {filename}",
			PrintResumed:  "Print resumed: {filename}",
			GcodeUploaded: "Upload complete: {filename} ({size})",
			PrintProgress: "Progress: {percent}% - {filename}",
		},
	}
}

// DefaultNotificationsConfig returns the default notifications configuration.
func DefaultNotificationsConfig() NotificationsConfig {
	return NotificationsConfig{
		Apprise: DefaultAppriseConfig(),
	}
}

// DefaultTimelapseConfig returns the default timelapse configuration,
// reading overrides from environment variables.
func DefaultTimelapseConfig() TimelapseConfig {
	light := os.Getenv("TIMELAPSE_LIGHT")
	var lightPtr *string
	if light != "" {
		lightPtr = &light
	}

	return TimelapseConfig{
		Enabled:        envBool("TIMELAPSE_ENABLED", false),
		Interval:       envInt("TIMELAPSE_INTERVAL_SEC", 30),
		MaxVideos:      envInt("TIMELAPSE_MAX_VIDEOS", 10),
		SavePersistent: envBool("TIMELAPSE_SAVE_PERSISTENT", true),
		OutputDir:      envString("TIMELAPSE_CAPTURES_DIR", "/captures"),
		Light:          lightPtr,
	}
}

// DefaultHomeAssistantConfig returns the default Home Assistant configuration,
// reading overrides from environment variables.
func DefaultHomeAssistantConfig() HomeAssistantConfig {
	return HomeAssistantConfig{
		Enabled:         envBool("HA_MQTT_ENABLED", false),
		MQTTHost:        envString("HA_MQTT_HOST", "localhost"),
		MQTTPort:        envInt("HA_MQTT_PORT", 1883),
		MQTTUsername:    envString("HA_MQTT_USER", ""),
		MQTTPassword:    envString("HA_MQTT_PASSWORD", ""),
		DiscoveryPrefix: envString("HA_MQTT_DISCOVERY_PREFIX", "homeassistant"),
		NodeID:          "ankermake_m5",
	}
}

// envBool reads an environment variable as a boolean.
// Recognizes "true", "1", "yes" (case-insensitive) as true.
func envBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	switch val {
	case "true", "True", "TRUE", "1", "yes", "Yes", "YES":
		return true
	}
	return false
}

// envInt reads an environment variable as an integer.
func envInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var result int
	for _, c := range val {
		if c < '0' || c > '9' {
			return defaultVal
		}
		result = result*10 + int(c-'0')
	}
	return result
}

// envString reads an environment variable with a default fallback.
func envString(key, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return val
}
