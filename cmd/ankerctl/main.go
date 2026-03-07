package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
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
			// Allow overriding via environment variable
			if envIdx := os.Getenv("PRINTER_INDEX"); envIdx != "" {
				if parsed, err := strconv.Atoi(envIdx); err == nil {
					printerIdx = parsed
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

	// 4. Services
	// Background services monitor the printer specified by printerIdx.
	pppp := service.NewPPPPServiceWithDB(cfgMgr, printerIdx, database)
	sm.Register(pppp)

	video := service.NewVideoQueue(pppp, pppp)
	sm.Register(video)

	timelapse := service.NewTimelapseService(filepath.Join(configDir, "captures"), video)
	sm.Register(timelapse)

	mqtt := service.NewMqttQueue(cfgMgr, printerIdx, database, nil, timelapse)
	sm.Register(mqtt)

	notif := notifications.NewNotificationService(cfgMgr, mqtt, video)
	sm.Register(notif)

	ft := service.NewFileTransferService(pppp, mqtt)
	sm.Register(ft)

	// 5. Web Server
	webOpts := []web.Option{
		web.WithDatabase(database),
		web.WithServiceManager(sm),
		web.WithDevMode(devMode),
		web.WithLogRing(globalLogRing),
	}
	if serverListen != "" {
		webOpts = append(webOpts, web.WithListen(serverListen))
	}
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

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("ankerctl-go v0.1.0")
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

	fmt.Fprintln(w, "    _    _   _ _  _______ ____ _____ _     ")
	fmt.Fprintln(w, "   / \\  | \\ | | |/ / ____/ ___|_   _| |    ")
	fmt.Fprintln(w, "  / _ \\ |  \\| | ' /|  _|| |     | | | |    ")
	fmt.Fprintln(w, " / ___ \\| |\\  | . \\| |__| |___  | | | |___ ")
	fmt.Fprintln(w, "/_/   \\_\\_| \\_|_|\\_\\_____\\____| |_| |_____|")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "mode: webserver  dev=%t  printer-index=%d\n", b.DevMode, b.PrinterIndex)
	fmt.Fprintf(w, "paths: config=%s  db=%s\n", b.ConfigDir, b.DBPath)
	fmt.Fprintf(w, "listen: %s:%d\n", host, port)
	for _, url := range bannerURLs(host, port) {
		fmt.Fprintf(w, "url: %s\n", url)
	}
	fmt.Fprintf(w, "api-key: %s\n", boolLabel(b.APIKeySet, "configured", "not set"))

	cfg := b.Config
	if cfg == nil || !cfg.IsConfigured() {
		fmt.Fprintln(w, "config: not configured")
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintf(w, "config: configured  printers=%d  active=%d\n", len(cfg.Printers), b.PrinterIndex)
	if b.DevMode && cfg.Account != nil {
		fmt.Fprintf(
			w,
			"account: region=%s country=%s email=%s user=%s token=%s\n",
			emptyDash(cfg.Account.Region),
			emptyDash(cfg.Account.Country),
			redactEmail(cfg.Account.Email),
			redactValue(cfg.Account.UserID, 0, 4),
			redactedLength(cfg.Account.AuthToken),
		)
	}
	for i, p := range cfg.Printers {
		activeMark := " "
		if i == b.PrinterIndex {
			activeMark = "*"
		}
		fmt.Fprintf(
			w,
			"printer[%d]%s: %s  sn=%s  model=%s  ip=%s\n",
			i,
			activeMark,
			emptyDash(p.Name),
			redactValue(p.SN, 0, 5),
			emptyDash(p.Model),
			emptyDash(p.IPAddr),
		)
		if b.DevMode {
			fmt.Fprintf(
				w,
				"           p2p_duid=%s mqtt_key=%s\n",
				redactValue(p.P2PDUID, 0, 6),
				redactedBytesLength(p.MQTTKey),
			)
		}
	}
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

func bannerURLs(host string, port int) []string {
	if port <= 0 {
		return nil
	}
	if host == "" {
		host = web.DefaultHost
	}
	if host != "0.0.0.0" && host != "::" && host != "[::]" {
		return []string{fmt.Sprintf("http://%s:%d/", host, port)}
	}

	urls := []string{fmt.Sprintf("http://127.0.0.1:%d/", port)}
	seen := map[string]struct{}{urls[0]: {}}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}
			url := fmt.Sprintf("http://%s:%d/", ip.String(), port)
			if _, ok := seen[url]; ok {
				continue
			}
			seen[url] = struct{}{}
			urls = append(urls, url)
		}
	}
	sort.Strings(urls[1:])
	return urls
}

func redactEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return redactValue(email, 1, 1)
	}
	return fmt.Sprintf("%s@%s", redactValue(parts[0], 1, 0), redactValue(parts[1], 1, 0))
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

func redactedLength(value string) string {
	if strings.TrimSpace(value) == "" {
		return "not set"
	}
	return fmt.Sprintf("[REDACTED len=%d]", len(value))
}

func redactedBytesLength(value []byte) string {
	if len(value) == 0 {
		return "not set"
	}
	return fmt.Sprintf("[REDACTED len=%d]", len(value))
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
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
