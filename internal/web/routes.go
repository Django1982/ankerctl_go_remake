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

	// Static files
	r.Handle("/static/*", http.FileServer(http.FS(staticFS)))

	// Page routes
	r.Get("/", h.Root)
	r.Get("/video", h.Video)

	// General API
	r.Get("/api/health", h.Health)
	r.Get("/api/version", h.Version)
	r.Get("/api/snapshot", h.Snapshot)

	// Config
	r.Post("/api/ankerctl/config/upload", h.ConfigUpload)
	r.Post("/api/ankerctl/config/login", h.ConfigLogin)
	r.Get("/api/ankerctl/server/reload", h.ServerReload)
	r.Post("/api/ankerctl/config/upload-rate", h.UploadRateUpdate)

	// Printer / selector
	r.Get("/api/printers", h.PrintersList)
	r.Post("/api/printers/active", h.PrintersSwitch)
	r.Post("/api/printer/gcode", h.PrinterGCode)
	r.Post("/api/printer/control", h.PrinterControl)
	r.Post("/api/printer/autolevel", h.PrinterAutolevel)
	r.Get("/api/printer/bed-leveling", h.BedLevelingLive)
	r.Get("/api/printer/bed-leveling/last", h.BedLevelingLast)

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
		r.Post("/api/debug/services/{name}/restart", h.DebugServiceRestart)
		r.Post("/api/debug/services/{name}/test", h.DebugServiceTest)
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
