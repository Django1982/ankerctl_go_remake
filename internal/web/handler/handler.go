package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/service"
)

// Handler bundles shared dependencies used by HTTP handlers.
type Handler struct {
	cfg     *config.Manager
	db      *db.DB
	svc     *service.ServiceManager
	log     *slog.Logger
	devMode bool
}

// New creates a handler bundle.
func New(cfg *config.Manager, database *db.DB, svc *service.ServiceManager, log *slog.Logger, devMode bool) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{cfg: cfg, db: database, svc: svc, log: log, devMode: devMode}
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
