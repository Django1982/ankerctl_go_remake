package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
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
