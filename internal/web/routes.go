package web

import (
	"net/http"

	"github.com/django1982/ankerctl/internal/web/handler"
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
	if s.logRing != nil {
		h.WithLogRing(s.logRing)
	}
	h.WithLogDir(handler.ResolveLogDir())
	h.WithVersion(s.appVersion)

	// Static files — vendor assets get long-lived caching; our own JS/CSS
	// must not be cached because they change with every rebuild (embed.FS
	// always reports ModTime=0, so the browser may serve stale content).
	r.Handle("/static/*", noCacheAppAssets(http.FileServer(http.FS(staticFS))))
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/img/logo.svg", http.StatusMovedPermanently)
	})

	// Page routes
	r.Get("/", h.Root)
	r.Get("/video", h.Video)

	// General API
	r.Get("/api/health", h.Health)
	r.Get("/api/version", h.Version)
	r.Get("/api/ankerctl/version", h.AppVersion)
	r.Get("/api/snapshot", h.Snapshot)

	// Config
	r.Post("/api/ankerctl/config/upload", h.ConfigUpload)
	r.Post("/api/ankerctl/config/login", h.ConfigLogin)
	r.Post("/api/ankerctl/config/logout", h.ConfigLogout)
	r.Get("/api/ankerctl/server/reload", h.ServerReload)
	r.Post("/api/ankerctl/config/upload-rate", h.UploadRateUpdate)

	// Printer / selector
	r.Get("/api/printers", h.PrintersList)
	r.Post("/api/printers/active", h.PrintersSwitch)
	r.Post("/api/printers/lan-search", h.LANSearch)
	r.Post("/api/printer/gcode", h.PrinterGCode)
	r.Post("/api/printer/control", h.PrinterControl)
	r.Post("/api/printer/autolevel", h.PrinterAutolevel)
	r.Get("/api/printer/bed-leveling", h.BedLevelingLive)
	r.Get("/api/printer/bed-leveling/last", h.BedLevelingLast)
	r.Get("/api/printer/z-offset", h.ZOffsetGet)
	r.Post("/api/printer/z-offset", h.ZOffsetSet)
	r.Post("/api/printer/z-offset/nudge", h.ZOffsetNudge)

	// Upload
	r.Post("/api/files/local", h.SlicerUpload)

	// Notifications
	r.Get("/api/notifications/settings", h.NotificationsGet)
	r.Post("/api/notifications/settings", h.NotificationsUpdate)
	r.Post("/api/notifications/test", h.NotificationsTest)

	// Settings
	r.Get("/api/settings/timelapse", h.SettingsTimelapseGet)
	r.Post("/api/settings/timelapse", h.SettingsTimelapseUpdate)
	r.Get("/api/settings/mqtt", h.SettingsMQTTGet)
	r.Post("/api/settings/mqtt", h.SettingsMQTTUpdate)

	// History
	r.Get("/api/history", h.HistoryList)
	r.Delete("/api/history", h.HistoryClear)

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
