package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/django1982/ankerctl/internal/model"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
)

// LANSearch broadcasts a LAN search, persists matching printer IPs into
// the config, and reports findings. Mirrors Python POST /api/printers/lan-search.
func (h *Handler) LANSearch(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil || len(cfg.Printers) == 0 {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	_, activeIdx, _ := h.activePrinter(cfg)

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	discovered, err := ppppclient.DiscoverLANAll(ctx)
	if err != nil {
		h.log.Warn("LAN search failed", "error", err)
	}

	if len(discovered) == 0 {
		h.writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "No printers responded within timeout. Are you connected to the same network as the printer?",
		})
		return
	}

	// Build DUID -> printer index map for matching.
	duidIndex := make(map[string]int)
	for i, p := range cfg.Printers {
		if p.P2PDUID != "" {
			duidIndex[p.P2PDUID] = i
		}
	}

	type resultEntry struct {
		DUID      string `json:"duid"`
		IPAddr    string `json:"ip_addr"`
		Persisted bool   `json:"persisted"`
	}

	var results []resultEntry
	savedCount := 0
	for _, d := range discovered {
		entry := resultEntry{
			DUID:   d.DUID,
			IPAddr: d.IP.String(),
		}
		if idx, ok := duidIndex[d.DUID]; ok {
			// Persist to config.
			ipStr := d.IP.String()
			if h.cfg != nil {
				if saveErr := h.cfg.Modify(func(saved *model.Config) (*model.Config, error) {
					if saved == nil || idx >= len(saved.Printers) {
						return saved, nil
					}
					if saved.Printers[idx].IPAddr == ipStr {
						return saved, nil // no change needed
					}
					saved.Printers[idx].IPAddr = ipStr
					return saved, nil
				}); saveErr != nil {
					h.log.Warn("lan-search: failed to persist IP", "duid", d.DUID, "error", saveErr)
				} else {
					entry.Persisted = true
					savedCount++
				}
			}
			// Also persist to DB cache.
			if h.db != nil && idx < len(cfg.Printers) && cfg.Printers[idx].SN != "" {
				if dbErr := h.db.SetPrinterIP(cfg.Printers[idx].SN, ipStr); dbErr != nil {
					h.log.Warn("lan-search: failed to cache IP in db", "error", dbErr)
				}
			}
		}
		results = append(results, entry)
	}

	// Build active printer info.
	var activePrinter map[string]any
	if activeIdx >= 0 && activeIdx < len(cfg.Printers) {
		ap := cfg.Printers[activeIdx]
		activeIP := ap.IPAddr
		updated := false
		for _, d := range discovered {
			if d.DUID == ap.P2PDUID {
				activeIP = d.IP.String()
				updated = true
				break
			}
		}
		activePrinter = map[string]any{
			"name":    ap.Name,
			"duid":    ap.P2PDUID,
			"ip_addr": activeIP,
			"updated": updated,
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"discovered":     results,
		"saved_count":    savedCount,
		"active_printer": activePrinter,
	})
}
