package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/notifications"
	"github.com/django1982/ankerctl/internal/service"
	"github.com/django1982/ankerctl/internal/web"
	"github.com/spf13/cobra"
)

// Version is injected at build time via -ldflags "-X main.Version=<tag>".
// Falls back to "dev" for local builds.
var Version = "dev"

var (
	configDir    string
	devMode      bool
	printerIdx   int
	serverListen string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "ankerctl",
		Short: "AnkerMake M5 3D Printer Control CLI",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	rootCmd.PersistentFlags().StringVar(&configDir, "config", defaultDir(), "Configuration directory")
	rootCmd.PersistentFlags().BoolVar(&devMode, "dev", false, "Enable development mode")

	webCmd := newWebserverCmd()
	webCmd.Flags().IntVar(&printerIdx, "printer-index", 0, "Index of the printer to monitor (0-based)")
	webCmd.Flags().StringVar(&serverListen, "listen", "", "Listen address, e.g. 0.0.0.0:4470 (env: ANKERCTL_HOST / ANKERCTL_PORT)")
	rootCmd.AddCommand(webCmd)

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newVersionCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ankerctl"
	}
	return filepath.Join(home, ".ankerctl")
}

func newWebserverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "webserver",
		Short: "Manage the web interface",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Env/flag override takes precedence; otherwise restore last UI selection.
			if envIdx := os.Getenv("PRINTER_INDEX"); envIdx != "" {
				if parsed, err := strconv.Atoi(envIdx); err == nil {
					printerIdx = parsed
				}
			} else if !cmd.Flags().Changed("printer-index") {
				if cfgMgr, err := config.NewManager(configDir); err == nil {
					if cfg, err := cfgMgr.Load(); err == nil && cfg != nil {
						printerIdx = cfg.ActivePrinterIndex
					}
				}
			}
			return runWebserver()
		},
	}
}

// globalLogRing is the in-memory ring buffer capturing the last 2000 log lines.
// It is initialised in runWebserver and shared with the web handler layer via
// web.WithLogRing so the debug log viewer can serve it as "live.log".
var globalLogRing = logging.NewRingBuffer(2000)

func runWebserver() error {
	// Build base handler: text to stderr (debug-level in dev mode, info otherwise).
	level := slog.LevelInfo
	if devMode {
		level = slog.LevelDebug
	}
	baseHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	ringHandler := logging.NewRingBufferHandler(baseHandler, globalLogRing)
	logger := slog.New(ringHandler)
	slog.SetDefault(logger)

	// 1. Config
	cfgMgr, err := config.NewManager(configDir)
	if err != nil {
		return fmt.Errorf("config manager: %w", err)
	}

	// 2. Database
	dbPath := filepath.Join(configDir, "ankerctl.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	// 3. Service Manager
	sm := service.NewServiceManager()

	// Determine whether the active printer is a supported device before starting
	// any services. Unsupported devices (e.g. eufyMake E1 UV Printer, V8260) use
	// an incompatible MQTT format; starting services for them causes unbounded
	// "mqtt decode failed" errors with no useful behaviour.
	activePrinterSupported := true
	if startupCfg, err := cfgMgr.Load(); err == nil && startupCfg != nil {
		if printerIdx < len(startupCfg.Printers) {
			activePrinterModel := startupCfg.Printers[printerIdx].Model
			if !model.IsPrinterSupported(activePrinterModel) {
				activePrinterSupported = false
				slog.Warn("active printer is not supported by ankerctl — all services suppressed",
					"printer_index", printerIdx,
					"model", activePrinterModel,
					"hint", "switch to a supported printer in the web UI")
			}
		}
	}

	// 4. Services
	// Background services monitor the printer specified by printerIdx.
	// All services are skipped when the active device is unsupported.
	pppp := service.NewPPPPServiceWithDB(cfgMgr, printerIdx, database)
	sm.Register(pppp)

	video := service.NewVideoQueue(pppp, pppp)
	sm.Register(video)

	timelapse := service.NewTimelapseService(filepath.Join(configDir, "captures"), video)
	sm.Register(timelapse)

	// Apply saved timelapse config at startup so it is active without requiring a settings save.
	if startupCfg, err := cfgMgr.Load(); err == nil && startupCfg != nil {
		printerSN := ""
		if printerIdx < len(startupCfg.Printers) {
			printerSN = startupCfg.Printers[printerIdx].SN
		}
		timelapse.Configure(startupCfg.Timelapse, printerSN)
	}

	// Instantiate HA service with saved config so it starts automatically when enabled.
	var ha *service.HomeAssistantService
	if startupCfg, err := cfgMgr.Load(); err == nil && startupCfg != nil {
		printerSN, printerName := "", ""
		if printerIdx < len(startupCfg.Printers) {
			printerSN = startupCfg.Printers[printerIdx].SN
			printerName = startupCfg.Printers[printerIdx].Name
		}
		ha = service.NewHomeAssistantService(startupCfg.HomeAssistant, printerSN, printerName, pppp)
		sm.Register(ha)
	}

	mqtt := service.NewMqttQueue(cfgMgr, printerIdx, database, ha, timelapse)
	sm.Register(mqtt)

	notif := notifications.NewNotificationService(cfgMgr, mqtt, video)
	sm.Register(notif)

	ft := service.NewFileTransferService(pppp, mqtt)
	sm.Register(ft)

	// Auto-start always-on services only for supported printers.
	// For unsupported devices the service objects are registered (so the web layer
	// can query them), but never started — they remain dormant.
	if activePrinterSupported {
		if _, err := sm.Borrow("notifications"); err != nil {
			slog.Warn("failed to start notification service", "err", err)
		}
		if _, err := sm.Borrow("timelapse"); err != nil {
			slog.Warn("failed to start timelapse service", "err", err)
		}
	} else {
		slog.Info("services not started: active printer is unsupported")
	}

	// 5. Startup config validation + auto-repair (background, non-blocking).
	checkAndRepairConfig(cfgMgr, printerIdx, database)

	// 6. Web Server
	webOpts := []web.Option{
		web.WithDatabase(database),
		web.WithServiceManager(sm),
		web.WithDevMode(devMode),
		web.WithLogRing(globalLogRing),
	}
	if serverListen != "" {
		webOpts = append(webOpts, web.WithListen(serverListen))
	}
	webOpts = append(webOpts, web.WithAppVersion(Version))
	srv := web.NewServer(cfgMgr, webOpts...)
	emitStartupBanner(os.Stderr, startupBanner{
		ConfigDir:    configDir,
		DBPath:       dbPath,
		DevMode:      devMode,
		PrinterIndex: printerIdx,
		Host:         resolvedListenHost(serverListen),
		Port:         resolvedListenPort(serverListen),
		Config:       mustLoadConfig(cfgMgr),
		APIKeySet:    hasAPIKey(cfgMgr),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down...")
		sm.Shutdown()
		// Wait a bit for server to stop
		time.Sleep(500 * time.Millisecond)
		return nil
	case err := <-errCh:
		return err
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgMgr, err := config.NewManager(configDir)
			if err != nil {
				return err
			}
			cfg, err := cfgMgr.Load()
			if err != nil {
				return err
			}
			if cfg == nil {
				fmt.Println("No configuration found.")
				return nil
			}
			// Redact secrets before printing
			if cfg.Account != nil {
				cfg.Account.AuthToken = "[REDACTED]"
			}
			for i := range cfg.Printers {
				cfg.Printers[i].MQTTKey = []byte("[REDACTED]")
			}

			// Simple display
			fmt.Printf("Config Directory: %s\n", configDir)
			if cfg.Account != nil {
				fmt.Printf("Account: %s\n", cfg.Account.Email)
			}
			fmt.Printf("Printers: %d\n", len(cfg.Printers))
			for i, p := range cfg.Printers {
				fmt.Printf("  [%d] %s (SN: %s, Model: %s, IP: %s)\n", i, p.Name, p.SN, p.Model, p.IPAddr)
			}
			return nil
		},
	})

	cmd.AddCommand(newAPIKeyCmd())

	return cmd
}

// newAPIKeyCmd returns the "config api-key" sub-command with set/get/unset children.
func newAPIKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-key",
		Short: "Manage the API key used to authenticate web requests",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key>",
		Short: "Save an API key to the config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if err := config.ValidateAPIKey(key); err != nil {
				return err
			}
			cfgMgr, err := config.NewManager(configDir)
			if err != nil {
				return fmt.Errorf("config manager: %w", err)
			}
			if err := cfgMgr.SetAPIKey(key); err != nil {
				return fmt.Errorf("save api key: %w", err)
			}
			fmt.Println("API key saved.")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Print the currently configured API key (redacted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgMgr, err := config.NewManager(configDir)
			if err != nil {
				return fmt.Errorf("config manager: %w", err)
			}
			key, err := cfgMgr.ResolveAPIKey()
			if err != nil {
				return fmt.Errorf("resolve api key: %w", err)
			}
			if key == "" {
				fmt.Println("No API key configured.")
				return nil
			}
			// Show first 4 chars + *** so the user can confirm which key is active
			// without revealing the full secret.
			runes := []rune(key)
			const keep = 4
			var display string
			if len(runes) <= keep {
				display = strings.Repeat("*", len(runes))
			} else {
				display = string(runes[:keep]) + strings.Repeat("*", len(runes)-keep)
			}
			source := "config file"
			if strings.TrimSpace(os.Getenv("ANKERCTL_API_KEY")) != "" {
				source = "environment variable ANKERCTL_API_KEY"
			}
			fmt.Printf("API key: %s  (source: %s)\n", display, source)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Generate a random API key, save it, and print it once",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgMgr, err := config.NewManager(configDir)
			if err != nil {
				return fmt.Errorf("config manager: %w", err)
			}
			raw := make([]byte, 24) // 24 bytes → 32 base64url chars
			if _, err := rand.Read(raw); err != nil {
				return fmt.Errorf("generate key: %w", err)
			}
			key := base64.RawURLEncoding.EncodeToString(raw)
			if err := cfgMgr.SetAPIKey(key); err != nil {
				return fmt.Errorf("save api key: %w", err)
			}
			fmt.Printf("API key: %s\n", key)
			fmt.Println("Save this key — it will not be shown again.")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "unset",
		Short: "Remove the API key from the config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgMgr, err := config.NewManager(configDir)
			if err != nil {
				return fmt.Errorf("config manager: %w", err)
			}
			if err := cfgMgr.RemoveAPIKey(); err != nil {
				return fmt.Errorf("remove api key: %w", err)
			}
			fmt.Println("API key removed.")
			return nil
		},
	})

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("ankerctl-go %s\n", Version)
		},
	}
}

type startupBanner struct {
	ConfigDir    string
	DBPath       string
	DevMode      bool
	PrinterIndex int
	Host         string
	Port         int
	Config       *model.Config
	APIKeySet    bool
}

func emitStartupBanner(w io.Writer, b startupBanner) {
	if w == nil {
		return
	}
	host := b.Host
	if host == "" {
		host = web.DefaultHost
	}
	port := b.Port
	if port <= 0 {
		port = web.DefaultPort
	}

	fmt.Fprintln(w, "####################################################################################")
	fmt.Fprintln(w, "##     .d8b.  d8b   db db   dD d88888b d8888b.  .o88b. d888888b d8888b. db        ##")
	fmt.Fprintln(w, "##    d8' `8b 888o  88 88 ,8P' 88'     88  `8D d8P  Y8 `~~88~~' 88  `8D 88        ##")
	fmt.Fprintln(w, "##    88ooo88 88V8o 88 88,8P   88ooooo 88oobY' 8P         88    88oobY' 88        ##")
	fmt.Fprintln(w, "##    88~~~88 88 V8o88 88`8b   88~~~~~ 88`8b   8b         88    88`8b   88        ##")
	fmt.Fprintln(w, "##    88   88 88  V888 88 `88. 88.     88 `88. Y8b  d8    88    88 `88. 88booo.   ##")
	fmt.Fprintln(w, "##    YP   YP VP   V8P YP   YD Y88888P 88   YD  `Y88P'    YP    88   YD Y88888P   ##")
	fmt.Fprintln(w, "####################################################################################")
	fmt.Fprintln(w, "##                           The solution for the                                 ##")
	fmt.Fprintln(w, "##                                 Slicer                                         ##")
	fmt.Fprintln(w, "##                                  YOU                                           ##")
	fmt.Fprintln(w, "##                                 choose                                         ##")
	fmt.Fprintln(w, "####################################################################################")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---- server ----")
	fmt.Fprintf(w, "mode: webserver  dev=%t  printer-index=%d\n", b.DevMode, b.PrinterIndex)
	fmt.Fprintf(w, "bind: %s\n", formatBind(host, port))
	for _, line := range bannerAccessLines(host, port) {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "config-dir: %s\n", emptyDash(b.ConfigDir))
	fmt.Fprintf(w, "api-key: %s\n", boolLabel(b.APIKeySet, "configured", "not set"))

	cfg := b.Config
	fmt.Fprintln(w, "---- config ----")
	if cfg == nil || !cfg.IsConfigured() {
		fmt.Fprintln(w, "state: not configured")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "---- runtime log ----")
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintf(w, "state: configured  printers=%d\n", len(cfg.Printers))
	fmt.Fprintf(w, "db: %s\n", emptyDash(b.DBPath))
	if b.DevMode && cfg.Account != nil {
		fmt.Fprintf(
			w,
			"account: region=%s  country=%s  token=%s\n",
			emptyDash(cfg.Account.Region),
			emptyLabel(cfg.Account.Country, "unset"),
			boolLabel(strings.TrimSpace(cfg.Account.AuthToken) != "", "set", "not set"),
		)
		fmt.Fprintf(
			w,
			"         email=%s  user=%s\n",
			redactEmail(cfg.Account.Email),
			shortRedaction(cfg.Account.UserID, 4),
		)
	}
	for i, p := range cfg.Printers {
		activeMark := " "
		if i == b.PrinterIndex {
			activeMark = "*"
		}
		fmt.Fprintf(
			w,
			"printer[%d]%s: name=%s  model=%s  sn=%s  ip=%s\n",
			i,
			activeMark,
			emptyDash(p.Name),
			emptyDash(p.Model),
			shortRedaction(p.SN, 4),
			emptyDash(p.IPAddr),
		)
		if b.DevMode {
			fmt.Fprintf(
				w,
				"           p2p_duid=%s  mqtt_key=%s\n",
				shortRedaction(p.P2PDUID, 6),
				boolLabel(len(p.MQTTKey) > 0, "set", "not set"),
			)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---- runtime log ----")
	fmt.Fprintln(w)
}

func mustLoadConfig(cfgMgr *config.Manager) *model.Config {
	if cfgMgr == nil {
		return nil
	}
	cfg, err := cfgMgr.Load()
	if err != nil {
		return nil
	}
	return cfg
}

func hasAPIKey(cfgMgr *config.Manager) bool {
	if cfgMgr == nil {
		return strings.TrimSpace(os.Getenv("ANKERCTL_API_KEY")) != ""
	}
	key, err := cfgMgr.ResolveAPIKey()
	return err == nil && strings.TrimSpace(key) != ""
}

func resolvedListenHost(listen string) string {
	if host, _, ok := parseListen(listen); ok {
		if host == "" {
			return "0.0.0.0"
		}
		return host
	}
	return firstNonEmpty(os.Getenv("ANKERCTL_HOST"), os.Getenv("FLASK_HOST"), web.DefaultHost)
}

func resolvedListenPort(listen string) int {
	if _, port, ok := parseListen(listen); ok {
		return port
	}
	for _, key := range []string{"ANKERCTL_PORT", "FLASK_PORT"} {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			if port, err := strconv.Atoi(raw); err == nil && port > 0 {
				return port
			}
		}
	}
	return web.DefaultPort
}

func parseListen(listen string) (string, int, bool) {
	if strings.TrimSpace(listen) == "" {
		return "", 0, false
	}
	host, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 {
		return "", 0, false
	}
	return host, port, true
}

func redactEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return shortRedaction(email, 1)
	}
	return fmt.Sprintf("%s@%s", shortRedaction(parts[0], 1), shortRedaction(parts[1], 1))
}

func redactValue(value string, keepStart, keepEnd int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if keepStart < 0 {
		keepStart = 0
	}
	if keepEnd < 0 {
		keepEnd = 0
	}
	runes := []rune(value)
	if keepStart+keepEnd >= len(runes) {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:keepStart]) + strings.Repeat("*", len(runes)-keepStart-keepEnd) + string(runes[len(runes)-keepEnd:])
}

func shortRedaction(value string, keepEnd int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	runes := []rune(value)
	if keepEnd <= 0 {
		return "..."
	}
	if keepEnd >= len(runes) {
		return value
	}
	return "..." + string(runes[len(runes)-keepEnd:])
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func emptyLabel(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolLabel(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatBind(host string, port int) string {
	if host == "" {
		host = web.DefaultHost
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func bannerAccessLines(host string, port int) []string {
	if port <= 0 {
		return nil
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = web.DefaultHost
	}

	switch host {
	case "0.0.0.0":
		return []string{
			fmt.Sprintf("local:   http://127.0.0.1:%d/", port),
			"exposed: all IPv4 interfaces",
		}
	case "::", "[::]":
		return []string{
			fmt.Sprintf("local4:  http://127.0.0.1:%d/", port),
			fmt.Sprintf("local6:  http://[::1]:%d/", port),
			"exposed: all IPv6 interfaces",
		}
	default:
		if isIPv6Literal(host) {
			return []string{fmt.Sprintf("local6:  http://[%s]:%d/", trimIPv6Brackets(host), port)}
		}
		return []string{fmt.Sprintf("local:   http://%s:%d/", host, port)}
	}
}

func isIPv6Literal(host string) bool {
	host = trimIPv6Brackets(host)
	return strings.Contains(host, ":")
}

func trimIPv6Brackets(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}
