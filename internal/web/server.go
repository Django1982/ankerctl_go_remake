package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/service"
	mw "github.com/django1982/ankerctl/internal/web/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

const (
	// DefaultHost is the default bind host for the web server.
	DefaultHost = "127.0.0.1"
	// DefaultPort is the default bind port for the web server.
	DefaultPort        = 4470
	defaultUploadMaxMB = 2048
)

var defaultUnsupportedPrinters = map[string]struct{}{}

var defaultPrintersWithoutCamera = map[string]struct{}{}

// Server is the phase-4 HTTP server with middleware stack and stub routes.
type Server struct {
	router     chi.Router
	httpServer *http.Server
	config     *config.Manager
	database   *db.DB
	services   *service.ServiceManager
	logger     *slog.Logger
	logRing    *logging.RingBuffer

	sessionManager *mw.SessionManager
	templates      *Templates

	mu sync.RWMutex

	apiKey             string
	login              bool
	printerIndex       int
	printerIndexLocked bool
	videoSupported     bool
	unsupportedDevice  bool
	host               string
	port               int
	maxUploadBytes     int64
	devMode            bool

	hostSet    bool
	portSet    bool
	apiKeySet  bool
	devModeSet bool

	appVersion string
}

// WithAppVersion injects the build-time version string.
func WithAppVersion(v string) Option {
	return func(s *Server) {
		s.appVersion = v
	}
}

// Option customizes server construction.
type Option func(*Server)

// WithHost sets the bind host.
func WithHost(host string) Option {
	return func(s *Server) {
		s.host = host
		s.hostSet = true
	}
}

// WithPort sets the bind port.
func WithPort(port int) Option {
	return func(s *Server) {
		s.port = port
		s.portSet = true
	}
}

// WithListen parses a "host:port" address and sets both host and port.
// Invalid addresses are silently ignored (env vars / defaults apply).
func WithListen(addr string) Option {
	return func(s *Server) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			return
		}
		s.host = host
		s.port = port
		s.hostSet = true
		s.portSet = true
	}
}

// WithAPIKey force-sets the API key.
func WithAPIKey(key string) Option {
	return func(s *Server) {
		s.apiKey = key
		s.apiKeySet = true
	}
}

// WithInsecure disables API-key checks by clearing the configured key.
func WithInsecure(insecure bool) Option {
	return func(s *Server) {
		if insecure {
			s.apiKey = ""
			s.apiKeySet = true
		}
	}
}

// NewServer creates a new phase-4 server.
func NewServer(cfg *config.Manager, opts ...Option) *Server {
	s := &Server{
		config: cfg,
		logger: slog.With("component", "web"),
		host:   DefaultHost,
		port:   DefaultPort,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithDatabase injects the persistence layer used by web handlers.
func WithDatabase(database *db.DB) Option {
	return func(s *Server) {
		s.database = database
	}
}

// WithServiceManager injects the service registry used by web handlers.
func WithServiceManager(svc *service.ServiceManager) Option {
	return func(s *Server) {
		s.services = svc
	}
}

// WithDevMode enables or disables development mode.
func WithDevMode(dev bool) Option {
	return func(s *Server) {
		s.devMode = dev
		s.devModeSet = true
	}
}

// WithLogRing attaches an in-memory ring buffer to the server so the debug
// log viewer can serve recent structured log output without requiring log files.
func WithLogRing(ring *logging.RingBuffer) Option {
	return func(s *Server) {
		s.logRing = ring
	}
}

// Start initializes and starts the HTTP server. It stops on context cancellation.
func (s *Server) Start(ctx context.Context) error {
	if err := s.initialize(); err != nil {
		return err
	}

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(mw.AccessLogger(s.logger))
	r.Use(mw.SecurityHeaders)
	r.Use(mw.RateLimit(100, time.Minute))
	r.Use(mw.BodySizeLimit(s.maxUploadBytes))
	r.Use(mw.RequirePrinter(s))
	r.Use(mw.BlockUnsupportedDevice(s))
	r.Use(mw.Auth(s))

	s.router = r
	s.registerRoutes()

	hs := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.Host(), s.Port()),
		Handler: s.router,
	}
	s.mu.Lock()
	s.httpServer = hs
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		err := hs.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	hs := s.httpServer
	s.mu.RUnlock()
	if hs == nil {
		return nil
	}
	return hs.Shutdown(ctx)
}

// APIKey returns the configured API key.
func (s *Server) APIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiKey
}

// IsLoggedIn reports whether a printer configuration is loaded.
func (s *Server) IsLoggedIn() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.login
}

// IsUnsupportedDevice reports whether the active printer is unsupported.
func (s *Server) IsUnsupportedDevice() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unsupportedDevice
}

// VideoSupported reports whether the active printer has camera/video support.
func (s *Server) VideoSupported() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.videoSupported
}

// ReloadState re-reads the config from disk and updates login/printer state.
// Called after login or logout so WebSocket handlers see the current state
// without requiring a full process restart.
func (s *Server) ReloadState() {
	if s.config == nil {
		return
	}
	cfg, err := s.config.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || cfg == nil || !cfg.IsConfigured() {
		s.login = false
		s.unsupportedDevice = false
		s.videoSupported = false
		return
	}
	s.login = true
	if !s.printerIndexLocked {
		s.printerIndex = cfg.ActivePrinterIndex
	}
	if p := printerAtIndex(cfg, s.printerIndex); p != nil {
		unsupported := makeModelSet(defaultUnsupportedPrinters, os.Getenv("ANKERCTL_UNSUPPORTED_PRINTERS"))
		noCamera := makeModelSet(defaultPrintersWithoutCamera, os.Getenv("ANKERCTL_PRINTERS_WITHOUT_CAMERA"))
		s.unsupportedDevice = modelInSet(p.Model, unsupported)
		s.videoSupported = !modelInSet(p.Model, noCamera)
	}
}

// SessionManager returns the session manager used by auth middleware.
func (s *Server) SessionManager() *mw.SessionManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionManager
}

// Host returns the bound server host.
func (s *Server) Host() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.host
}

// Port returns the bound server port.
func (s *Server) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

// ProbePPPP performs an out-of-band PPPP reachability probe for the active printer.
func (s *Server) ProbePPPP(ctx context.Context) bool {
	s.mu.RLock()
	cfg := s.config
	database := s.database
	printerIndex := s.printerIndex
	s.mu.RUnlock()
	return service.ProbePPPP(ctx, cfg, printerIndex, database)
}

func (s *Server) initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.devModeSet {
		s.devMode = envBool("ANKERCTL_DEV_MODE", false)
	}
	if !s.hostSet {
		s.host = firstNonEmpty(os.Getenv("ANKERCTL_HOST"), os.Getenv("FLASK_HOST"), DefaultHost)
	}
	if !s.portSet {
		s.port = envIntWithFallback(DefaultPort, "ANKERCTL_PORT", "FLASK_PORT")
	}
	envPrinterIndex, envPrinterIndexSet := envInt("PRINTER_INDEX")
	// Lock printer index whenever the env var is present and valid.
	// envInt returns (0, false) on parse failure, so printerIndexLocked
	// stays false for malformed values — intentional: bad values are ignored.
	if envPrinterIndexSet {
		s.printerIndexLocked = true
	}
	uploadMaxMB := envIntWithFallback(defaultUploadMaxMB, "UPLOAD_MAX_MB")
	if uploadMaxMB <= 0 {
		uploadMaxMB = defaultUploadMaxMB
	}
	s.maxUploadBytes = int64(uploadMaxMB) * 1024 * 1024

	if s.config != nil {
		cfg, err := s.config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if cfg != nil && cfg.IsConfigured() {
			s.login = true
			s.printerIndex = cfg.ActivePrinterIndex
			if envPrinterIndexSet {
				s.printerIndex = envPrinterIndex
			}
			if p := printerAtIndex(cfg, s.printerIndex); p != nil {
				unsupported := makeModelSet(defaultUnsupportedPrinters, os.Getenv("ANKERCTL_UNSUPPORTED_PRINTERS"))
				noCamera := makeModelSet(defaultPrintersWithoutCamera, os.Getenv("ANKERCTL_PRINTERS_WITHOUT_CAMERA"))
				s.unsupportedDevice = modelInSet(p.Model, unsupported)
				s.videoSupported = !modelInSet(p.Model, noCamera)
			}
		}

		if !s.apiKeySet {
			apiKey, err := s.config.ResolveAPIKey()
			if err != nil {
				return fmt.Errorf("resolve API key: %w", err)
			}
			s.apiKey = apiKey
		}
	}

	secret, err := s.loadOrCreateSecretKey()
	if err != nil {
		return err
	}
	s.sessionManager = mw.NewSessionManager(secret)

	tmpls, err := newTemplates()
	if err != nil {
		return fmt.Errorf("init templates: %w", err)
	}
	s.templates = tmpls

	return nil
}

func (s *Server) loadOrCreateSecretKey() ([]byte, error) {
	if fromEnv := os.Getenv("FLASK_SECRET_KEY"); fromEnv != "" {
		return []byte(fromEnv), nil
	}

	configRoot, err := s.configRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create config root: %w", err)
	}
	if err := os.Chmod(configRoot, 0o700); err != nil {
		return nil, fmt.Errorf("chmod config root: %w", err)
	}

	secretFile := filepath.Join(configRoot, "flask_secret.key")
	if data, readErr := os.ReadFile(secretFile); readErr == nil {
		secret := strings.TrimSpace(string(data))
		if secret != "" {
			return []byte(secret), nil
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	secret := hex.EncodeToString(raw)
	if err := os.WriteFile(secretFile, []byte(secret+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("persist secret key: %w", err)
	}
	return []byte(secret), nil
}

func (s *Server) configRoot() (string, error) {
	if s.config != nil {
		return s.config.ConfigDir(), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "ankerctl"), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func envIntWithFallback(defaultValue int, keys ...string) int {
	for _, key := range keys {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				return parsed
			}
		}
	}
	return defaultValue
}

func envBool(key string, defaultVal bool) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if val == "" {
		return defaultVal
	}
	return val == "1" || val == "true" || val == "yes"
}

func envInt(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func makeModelSet(base map[string]struct{}, overrideCSV string) map[string]struct{} {
	result := make(map[string]struct{}, len(base))
	for k := range base {
		result[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	if strings.TrimSpace(overrideCSV) == "" {
		return result
	}
	for _, item := range strings.Split(overrideCSV, ",") {
		model := strings.ToLower(strings.TrimSpace(item))
		if model == "" {
			continue
		}
		result[model] = struct{}{}
	}
	return result
}

func modelInSet(model string, set map[string]struct{}) bool {
	_, ok := set[strings.ToLower(strings.TrimSpace(model))]
	return ok
}

func printerAtIndex(cfg *model.Config, idx int) *model.Printer {
	if cfg == nil || len(cfg.Printers) == 0 {
		return nil
	}
	if idx < 0 || idx >= len(cfg.Printers) {
		idx = 0
	}
	return &cfg.Printers[idx]
}
