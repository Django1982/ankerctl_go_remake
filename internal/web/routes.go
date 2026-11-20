package web

import (
	"net/http"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/django1982/ankerctl/internal/web/handler"
	mw "github.com/django1982/ankerctl/internal/web/middleware"
	"github.com/django1982/ankerctl/internal/web/ws"
)

func (s *Server) registerRoutes() {
	r := s.router

	rf := func(w http.ResponseWriter, name string, data any) error {
		return s.templates.Render(w, name, data)
	}

	h := handler.New(s.config, s.database, s.services, s.logger, s.devMode, rf)
	h.WithStateReloader(s)
	h.WithVideoChecker(s)
	h.WithUnsupportedChecker(s)
	h.WithShutdownTrigger(s)
	h.WithUploadMaxBytes(s.maxUploadBytes)
	if s.logRing != nil {
		h.WithLogRing(s.logRing)
	}
	h.WithLogDir(handler.ResolveLogDir())
	h.WithVersion(s.appVersion)

	// Attach GCode archiver when a config directory is available.
	if s.config != nil {
		h.WithGCodeArchiver(service.NewGCodeArchiver(s.config.ConfigDir()))
	}

	// Static files — vendor assets get long-lived caching; our own JS/CSS
	// must not be cached because they change with every rebuild (embed.FS
	// always reports ModTime=0, so the browser may serve stale content).
	r.Handle("/static/*", noCacheAppAssets(http.FileServer(http.FS(staticFS))))
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/img/logo.svg", http.StatusMovedPermanently)
	})

	// Browser login (ANKERCTL_WEB_PASSWORD_HASH) - see middleware.WebAuthGate
	// for why this exists instead of HTTP Basic Auth in a reverse proxy.
	loginAttempts, lockout := webAuthLoginLimits()
	lh := mw.NewLoginHandlers(s, loginAttempts, lockout, func(w http.ResponseWriter, next, errMsg string, status int) {
		w.WriteHeader(status)
		_ = s.templates.Render(w, "login.html", map[string]any{
			"Next":  next,
			"Error": errMsg,
		})
	})
	r.Get("/login", lh.ShowLogin)
	r.Post("/login", lh.HandleLogin)
	r.Post("/logout", lh.HandleLogout)

	// Page routes
	r.Get("/", h.Root)
	r.Get("/video", h.Video)

	// General API
	r.Get("/api/health", h.Health)
	r.Get("/api/version", h.Version)
	r.Get("/api/ankerctl/version", h.AppVersion)
	// /api/snapshot: captures from VideoQueue and archives to the Snapshots tab.
	r.Get("/api/snapshot", h.SnapshotCapture)
	// Console log viewer (B6): always registered — path is in protectedGETPaths.
	r.Get("/api/console/logs", h.ConsoleLogs)

	// Config
	r.Post("/api/ankerctl/config/upload", h.ConfigUpload)
	r.Post("/api/ankerctl/config/import-slicer", h.ImportSlicer)
	r.Post("/api/ankerctl/config/login", h.ConfigLogin)
	r.Post("/api/ankerctl/config/logout", h.ConfigLogout)
	r.Get("/api/ankerctl/server/reload", h.ServerReload)
	r.Post("/api/ankerctl/server/shutdown", h.ServerShutdown)
	r.Post("/api/ankerctl/config/upload-rate", h.UploadRateUpdate)

	// Printer / selector
	r.Get("/api/printers", h.PrintersList)
	r.Post("/api/printers/active", h.PrintersSwitch)
	r.Post("/api/printers/lan-search", h.LANSearch)
	r.Post("/api/printer/gcode", h.PrinterGCode)
	r.Post("/api/printer/control", h.PrinterControl)
	r.Post("/api/printer/autolevel", h.PrinterAutolevel)
	r.Post("/api/printer/home", h.PrinterHome)
	r.Get("/api/printer/runtime-state", h.PrinterRuntimeState)
	r.Get("/api/printer/settings-summary", h.PrinterSettingsSummary)
	r.Get("/api/printer/alerts", h.PrinterAlerts)
	r.Get("/api/printer/bed-leveling", h.BedLevelingLive)
	r.Get("/api/printer/bed-leveling/last", h.BedLevelingLast)
	r.Get("/api/printer/z-offset", h.ZOffsetGet)
	r.Post("/api/printer/z-offset", h.ZOffsetSet)
	r.Post("/api/printer/z-offset/nudge", h.ZOffsetNudge)
	r.Post("/api/printer/z-offset/refresh", h.ZOffsetRefresh)

	// Upload
	r.Post("/api/files/local", h.SlicerUpload)
	r.Get("/api/files/printer", h.PrinterFilesList)
	r.Get("/api/files/printer/thumbnail", h.PrinterFileThumbnail)
	r.Post("/api/files/printer/print", h.PrinterFilePrint)

	// Notifications
	r.Get("/api/notifications/settings", h.NotificationsGet)
	r.Post("/api/notifications/settings", h.NotificationsUpdate)
	r.Post("/api/notifications/test", h.NotificationsTest)

	// Settings
	r.Get("/api/settings/timelapse", h.SettingsTimelapseGet)
	r.Post("/api/settings/timelapse", h.SettingsTimelapseUpdate)
	r.Get("/api/settings/mqtt", h.SettingsMQTTGet)
	r.Post("/api/settings/mqtt", h.SettingsMQTTUpdate)
	r.Post("/api/settings/launcher-bat", h.SettingsLauncherBat)
	r.Get("/api/settings/filament-service", h.SettingsFilamentServiceGet)
	r.Post("/api/settings/filament-service", h.SettingsFilamentServiceUpdate)
	r.Get("/api/settings/filament-service/advanced", h.SettingsFilamentServiceAdvancedGet)
	r.Post("/api/settings/filament-service/advanced/open", h.SettingsFilamentServiceAdvancedOpen)
	r.Get("/api/settings/appearance", h.SettingsAppearanceGet)
	r.Post("/api/settings/appearance", h.SettingsAppearanceUpdate)
	r.Get("/api/settings/camera", h.SettingsCameraGet)
	r.Post("/api/settings/camera", h.SettingsCameraUpdate)

	// Camera streaming (B5)
	r.Get("/api/camera/frame", h.CameraFrame)
	r.Get("/api/camera/stream", h.CameraStream)

	// History
	r.Get("/api/history", h.HistoryList)
	r.Delete("/api/history", h.HistoryClear)
	r.Post("/api/history/delete", h.HistoryDeleteSelected)
	r.Post("/api/history/{id}/reprint", h.HistoryReprint)
	r.Get("/api/history/{id}/thumbnail", h.HistoryThumbnail)

	// Filaments
	r.Get("/api/filaments", h.FilamentList)
	r.Post("/api/filaments", h.FilamentCreate)
	r.Put("/api/filaments/{id}", h.FilamentUpdate)
	r.Delete("/api/filaments/{id}", h.FilamentDelete)
	r.Post("/api/filaments/{id}/apply", h.FilamentApply)
	r.Post("/api/filaments/{id}/duplicate", h.FilamentDuplicate)

	// Filament Service
	r.Get("/api/filaments/service/swap", h.FilamentServiceSwapState)
	r.Post("/api/filaments/service/preheat", h.FilamentServicePreheat)
	r.Post("/api/filaments/service/move", h.FilamentServiceMove)
	r.Post("/api/filaments/service/swap/start", h.FilamentServiceSwapStart)
	r.Post("/api/filaments/service/swap/confirm", h.FilamentServiceSwapConfirm)
	r.Post("/api/filaments/service/swap/cancel", h.FilamentServiceSwapCancel)

	// Timelapses
	r.Get("/api/timelapses", h.TimelapseList)
	r.Get("/api/timelapse/{filename}", h.TimelapseDownload)
	r.Delete("/api/timelapse/{filename}", h.TimelapseDelete)

	// Timelapse snapshot gallery (B3)
	r.Get("/api/timelapse-snapshots", h.TimelapseSnapshotsList)
	r.Get("/api/timelapse-snapshot/{collection_id}/{filename}", h.TimelapseSnapshotDownload)
	r.Delete("/api/timelapse-snapshot/{collection_id}", h.TimelapseSnapshotCollectionDelete)
	r.Delete("/api/timelapse-snapshot/{collection_id}/{filename}", h.TimelapseSnapshotDelete)

	// Manual timelapse controls (B3)
	r.Post("/api/timelapse/current/start", h.TimelapseCurrentStart)
	r.Post("/api/timelapse/current/dismiss", h.TimelapseCurrentDismiss)
	r.Post("/api/timelapse/current/pause", h.TimelapseCurrentPause)
	r.Post("/api/timelapse/current/resume", h.TimelapseCurrentResume)
	r.Post("/api/timelapse/current/stop", h.TimelapseCurrentStop)

	// Debug routes are only mounted when dev mode is on.
	if s.devMode {
		r.Get("/api/debug/state", h.DebugState)
		r.Post("/api/debug/config", h.DebugConfig)
		r.Post("/api/debug/simulate", h.DebugSimulate)
		r.Get("/api/debug/logs", h.DebugLogsList)
		r.Get("/api/debug/logs/{filename}", h.DebugLogsContent)
		r.Get("/api/debug/services", h.DebugServices)
		r.Get("/api/debug/video/stats", h.DebugVideoStats)
		r.Post("/api/debug/services/{name}/restart", h.DebugServiceRestart)
		r.Post("/api/debug/services/{name}/test", h.DebugServiceTest)
		r.Post("/api/debug/pppp/discover", h.DiscoverPrinterIP)
		r.Post("/api/debug/pppp/reconnect", h.PPPPReconnect)
		r.Get("/api/debug/bed-leveling", h.BedLevelingLive)
	}

	// WebSocket placeholders (Phase 11)
	wsh := ws.New(s.services, s, s.logger)
	r.Get("/ws/mqtt", wsh.MQTT)
	r.Get("/ws/video", wsh.Video)
	r.Get("/ws/pppp-state", wsh.PPPPState)
	r.Get("/ws/upload", wsh.Upload)
	r.Get("/ws/ctrl", wsh.Ctrl)
}
