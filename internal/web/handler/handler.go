package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/service"
)

// PrinterSummary is a view-model entry used in the printer selector dropdown.
// It carries the per-printer Supported flag so templates can render disabled
// items for unsupported devices without needing to call model.IsPrinterSupported
// from inside the template.
type PrinterSummary struct {
	Index   int
	Name    string
	SN      string
	Model   string
	IPAddr  string
	Supported bool
}

// TemplateData holds the common variables for rendering the web UI.
type TemplateData struct {
	Printers            []model.Printer
	PrinterList         []PrinterSummary // enriched list for the selector dropdown
	ActivePrinterIndex  int
	Printer             *model.Printer
	PrinterIndexLocked  bool
	VideoSupported      bool
	UnsupportedDevice   bool // true when active printer is a non-3D-printer device
	Configure           bool
	DebugMode           bool
	Flashes             []Flash
	VideoProfiles       []VideoProfile
	VideoProfileDefault string

	// Request context (used in instructions tab)
	RequestHost string
	RequestPort string

	// Setup specific
	ConfigExistingEmail string
	CountryCodes        string
	CurrentCountry      string
	LoginFilePath       string
	AnkerConfig         string
	UploadRateChoices   []int
	UploadRateMbps      int
	UploadRateEnv       bool
	UploadRateConfig    int
	UploadRateSource    string
	AccentColor         string
}

type Flash struct {
	Category string
	Message  string
}

type VideoProfile struct {
	ID    string
	Label string
	Live  bool
}

// RenderFunc is the function signature for template rendering.
type RenderFunc func(w http.ResponseWriter, name string, data any) error

// StateReloader is implemented by the Server to refresh in-memory login state
// from disk after a login or logout without a full process restart.
type StateReloader interface {
	ReloadState()
}

// VideoSupportChecker reports whether the active printer has camera support.
// Implemented by the Server which tracks the current printer model.
type VideoSupportChecker interface {
	VideoSupported() bool
}

// UnsupportedDeviceChecker reports whether the active printer is an unsupported
// device (e.g. the eufyMake E1 UV Printer, model V8260).
type UnsupportedDeviceChecker interface {
	IsUnsupportedDevice() bool
}

// Handler bundles shared dependencies used by HTTP handlers.
type Handler struct {
	cfg                *config.Manager
	db                 *db.DB
	svc                *service.ServiceManager
	log                *slog.Logger
	devMode            bool
	render             RenderFunc
	stateReloader      StateReloader
	videoChecker       VideoSupportChecker
	unsupportedChecker UnsupportedDeviceChecker
	logRing            *logging.RingBuffer
	logDir             string // resolved once at startup; empty means no disk log dir available
	version            string
	releases           *releaseCache
	shutdownTrigger    ShutdownTrigger
	uploadMaxBytes     int64

	// lanDiscoveryFunc overrides ppppclient.DiscoverLANIP for testing.
	// If nil, the real ppppclient.DiscoverLANIP is used.
	lanDiscoveryFunc func(ctx context.Context, duid string) (net.IP, error)
}

// New creates a handler bundle.
func New(cfg *config.Manager, database *db.DB, svc *service.ServiceManager, log *slog.Logger, devMode bool, render RenderFunc) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{cfg: cfg, db: database, svc: svc, log: log, devMode: devMode, render: render}
}

// WithStateReloader sets the StateReloader used by ServerReload and ConfigLogout.
func (h *Handler) WithStateReloader(r StateReloader) {
	h.stateReloader = r
}

// WithVideoChecker sets the VideoSupportChecker used to determine whether the
// active printer has camera/video support, so templates can hide video UI.
func (h *Handler) WithVideoChecker(vc VideoSupportChecker) {
	h.videoChecker = vc
}

// WithUnsupportedChecker sets the checker used to determine whether the active
// printer is an unsupported device. Used by templates and the selector handler.
func (h *Handler) WithUnsupportedChecker(uc UnsupportedDeviceChecker) {
	h.unsupportedChecker = uc
}

// WithLogRing attaches an in-memory log ring buffer so the debug log viewer
// can serve recent log output as "live.log" without requiring log files.
func (h *Handler) WithLogRing(ring *logging.RingBuffer) {
	h.logRing = ring
}

// WithLogDir sets the disk log directory for the debug log viewer.
// Resolved once at startup: set to empty string if no directory is available.
func (h *Handler) WithLogDir(dir string) {
	h.logDir = dir
}

// WithShutdownTrigger sets the ShutdownTrigger used by ServerShutdown to
// initiate a graceful process shutdown via the API.
func (h *Handler) WithShutdownTrigger(t ShutdownTrigger) {
	h.shutdownTrigger = t
}

func (h *Handler) WithUploadMaxBytes(n int64) {
	h.uploadMaxBytes = n
}

// ResolveLogDir determines the log directory once at startup.
// Honour ANKERCTL_LOG_DIR env var, fall back to "/logs" only if it exists as a
// directory, otherwise return empty string (no disk log dir available).
func ResolveLogDir() string {
	if dir := strings.TrimSpace(os.Getenv("ANKERCTL_LOG_DIR")); dir != "" {
		return dir
	}
	if info, err := os.Stat("/logs"); err == nil && info.IsDir() {
		return "/logs"
	}
	return ""
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": msg})
}

func mergeIntoStruct[T any](dst *T, patch map[string]any) {
	if dst == nil || patch == nil {
		return
	}
	baseJSON, err := json.Marshal(dst)
	if err != nil {
		return
	}
	var merged map[string]any
	if err := json.Unmarshal(baseJSON, &merged); err != nil {
		return
	}
	for k, v := range patch {
		merged[k] = v
	}
	outJSON, err := json.Marshal(merged)
	if err != nil {
		return
	}
	_ = json.Unmarshal(outJSON, dst)
}

func (h *Handler) loadConfig() (*model.Config, error) {
	if h.cfg == nil {
		return nil, errors.New("config manager unavailable")
	}
	cfg, err := h.cfg.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func (h *Handler) activePrinter(cfg *model.Config) (*model.Printer, int, bool) {
	if cfg == nil || len(cfg.Printers) == 0 {
		return nil, 0, false
	}
	if env := strings.TrimSpace(os.Getenv("PRINTER_INDEX")); env != "" {
		if idx, err := strconv.Atoi(env); err == nil && idx >= 0 && idx < len(cfg.Printers) {
			return &cfg.Printers[idx], idx, true
		}
	}
	idx := cfg.ActivePrinterIndex
	if idx < 0 || idx >= len(cfg.Printers) {
		idx = 0
	}
	return &cfg.Printers[idx], idx, false
}

func (h *Handler) serviceByName(name string) (service.Service, bool) {
	if h.svc == nil {
		return nil, false
	}
	return h.svc.Get(name)
}

func (h *Handler) mqttQueue() (*service.MqttQueue, bool) {
	svc, ok := h.serviceByName("mqttqueue")
	if !ok {
		return nil, false
	}
	q, ok := svc.(*service.MqttQueue)
	return q, ok
}

func (h *Handler) videoQueue() (*service.VideoQueue, bool) {
	svc, ok := h.serviceByName("videoqueue")
	if !ok {
		return nil, false
	}
	q, ok := svc.(*service.VideoQueue)
	return q, ok
}

func (h *Handler) fileTransfer() (*service.FileTransferService, bool) {
	svc, ok := h.serviceByName("filetransfer")
	if !ok {
		return nil, false
	}
	q, ok := svc.(*service.FileTransferService)
	return q, ok
}

func (h *Handler) timelapse() (*service.TimelapseService, bool) {
	svc, ok := h.serviceByName("timelapse")
	if !ok {
		return nil, false
	}
	q, ok := svc.(*service.TimelapseService)
	return q, ok
}

func (h *Handler) homeAssistant() (*service.HomeAssistantService, bool) {
	svc, ok := h.serviceByName("homeassistant")
	if !ok {
		return nil, false
	}
	q, ok := svc.(*service.HomeAssistantService)
	return q, ok
}

// videoSupported returns whether the active printer supports video.
// Falls back to true (default) when no checker is configured.
func (h *Handler) videoSupported() bool {
	if h.videoChecker != nil {
		return h.videoChecker.VideoSupported()
	}
	return true
}

// isUnsupportedDevice returns whether the active printer is an unsupported
// device (e.g. eufyMake E1 UV Printer). Falls back to false when no checker
// is configured — fail-open is the correct default here (assume supported).
func (h *Handler) isUnsupportedDevice() bool {
	if h.unsupportedChecker != nil {
		return h.unsupportedChecker.IsUnsupportedDevice()
	}
	return false
}
