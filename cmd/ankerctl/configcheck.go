package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
)

// checkAndRepairConfig validates the active printer's configuration fields at
// startup and triggers background repairs where possible.
//
// For fields that cannot be auto-repaired (auth_token, mqtt_key, p2p_duid),
// a clear warning is logged so operators know what is missing.
//
// For the printer IP: if it is absent (or stale), a background LAN broadcast
// is issued and the discovered address is written back to default.json and
// the DB so the PPPP service can use it immediately on next restart.
func checkAndRepairConfig(cfgMgr *config.Manager, printerIndex int, database *db.DB) {
	if cfgMgr == nil {
		return
	}
	cfg, err := cfgMgr.Load()
	if err != nil || cfg == nil {
		// No config yet — user hasn't logged in. Nothing to check.
		return
	}

	// Account-level checks.
	if cfg.Account == nil {
		slog.Warn("startup config check: no account configured — login required")
		return
	}
	if cfg.Account.AuthToken == "" {
		slog.Warn("startup config check: auth_token is missing — re-login required")
	}

	if printerIndex < 0 || printerIndex >= len(cfg.Printers) {
		slog.Warn("startup config check: printer index out of range", "index", printerIndex, "total", len(cfg.Printers))
		return
	}
	p := cfg.Printers[printerIndex]

	// Log missing required fields — these cannot be auto-fixed.
	if p.SN == "" {
		slog.Warn("startup config check: printer SN missing")
	}
	if p.P2PDUID == "" {
		slog.Warn("startup config check: p2p_duid missing — PPPP connection impossible")
	}
	if len(p.MQTTKey) == 0 {
		slog.Warn("startup config check: mqtt_key missing — MQTT connection will fail")
	}
	if p.P2PKey == "" {
		slog.Warn("startup config check: p2p_key missing")
	}

	// Printer IP: if absent, run background LAN discovery to find and persist it.
	if p.IPAddr == "" {
		if p.P2PDUID == "" {
			slog.Warn("startup config check: cannot discover printer IP — p2p_duid missing")
			return
		}
		slog.Info("startup config check: printer IP missing — starting background LAN discovery",
			"duid", logging.RedactID(p.P2PDUID, 4))
		go func(duid, sn string, idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ip, err := ppppclient.DiscoverLANIP(ctx, duid)
			if err != nil {
				slog.Warn("startup config check: LAN discovery failed", "duid", logging.RedactID(duid, 4), "err", err)
				return
			}
			ipStr := ip.String()
			slog.Info("startup config check: discovered printer IP", "ip", ipStr)
			if err := cfgMgr.Modify(func(saved *model.Config) (*model.Config, error) {
				if saved == nil || idx < 0 || idx >= len(saved.Printers) {
					return saved, nil
				}
				saved.Printers[idx].IPAddr = ipStr
				return saved, nil
			}); err != nil {
				slog.Warn("startup config check: failed to persist IP to config", "ip", ipStr, "err", err)
			}
			if database != nil && sn != "" {
				if err := database.SetPrinterIP(sn, ipStr); err != nil {
					slog.Warn("startup config check: failed to persist IP to db", "ip", ipStr, "err", err)
				}
			}
		}(p.P2PDUID, p.SN, printerIndex)
	} else {
		slog.Debug("startup config check: printer IP present", "ip", p.IPAddr)
	}
}
