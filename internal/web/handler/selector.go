package handler

import (
	"encoding/json"
	"net/http"

	"github.com/django1982/ankerctl/internal/model"
)

// PrintersList lists configured printers and active index.
func (h *Handler) PrintersList(w http.ResponseWriter, _ *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"printers": []any{}, "active_index": 0, "locked": false})
		return
	}
	_, active, locked := h.activePrinter(cfg)
	printers := make([]map[string]any, 0, len(cfg.Printers))
	for i, p := range cfg.Printers {
		printers = append(printers, map[string]any{
			"index":     i,
			"name":      p.Name,
			"sn":        p.SN,
			"model":     p.Model,
			"ip_addr":   p.IPAddr,
			"supported": model.IsPrinterSupported(p.Model),
		})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"printers": printers, "active_index": active, "locked": locked})
}

// PrintersSwitch sets the active printer index in config and restarts services.
func (h *Handler) PrintersSwitch(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	_, _, locked := h.activePrinter(cfg)
	if locked {
		h.writeError(w, http.StatusForbidden, "Printer selection locked by PRINTER_INDEX environment variable")
		return
	}

	var payload struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Missing or invalid 'index' parameter")
		return
	}
	if payload.Index < 0 || payload.Index >= len(cfg.Printers) {
		h.writeError(w, http.StatusBadRequest, "Printer index out of range")
		return
	}

	mqtt, ok := h.mqttQueue()
	if ok && mqtt.IsPrinting() {
		h.writeError(w, http.StatusConflict, "Cannot switch printer during an active print")
		return
	}

	newIdx := payload.Index
	oldIdx := cfg.ActivePrinterIndex
	if oldIdx == newIdx {
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "Already active"})
		return
	}

	err = h.cfg.Modify(func(current *model.Config) (*model.Config, error) {
		if current == nil {
			return current, nil
		}
		current.ActivePrinterIndex = newIdx
		return current, nil
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update active printer")
		return
	}
	if h.svc != nil {
		h.svc.RestartAll()
	}
	printer := cfg.Printers[newIdx]
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "printer": map[string]any{"index": newIdx, "name": printer.Name, "sn": printer.SN}})
}
