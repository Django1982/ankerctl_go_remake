package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/django1982/ankerctl/internal/httpapi"
	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	ppppcrypto "github.com/django1982/ankerctl/internal/pppp/crypto"
)

// ConfigUpload imports config JSON from multipart upload.
func (h *Handler) ConfigUpload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("login_file")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "No file found")
		return
	}
	defer file.Close()

	var cfg model.Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid config json")
		return
	}
	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Save(&cfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to persist config")
		return
	}
	// Kick off background LAN discovery for any printer that has a P2P DUID
	// but no IP address yet. Discovered IPs are persisted to config + DB so the
	// PPPP service can connect immediately on first start.
	go h.discoverAndPersistPrinterIPs(cfg.Printers)
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConfigLogin performs cloud login, fetches printer list, and saves config.
func (h *Handler) ConfigLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	email := r.FormValue("login_email")
	password := r.FormValue("login_password")
	country := r.FormValue("login_country")
	captchaID := r.FormValue("login_captcha_id")
	captchaText := r.FormValue("login_captcha_text")
	if email == "" || password == "" || country == "" {
		h.writeError(w, http.StatusBadRequest, "missing login parameters")
		return
	}

	ctx := r.Context()

	// Step 1: Detect region if not explicitly provided.
	region := country
	if region != "eu" && region != "us" {
		region = httpapi.GuessRegion()
	}

	// Step 2: Login via ECDH-encrypted API.
	passportCfg := httpapi.ClientConfig{Region: region}
	passport, err := httpapi.NewPassportV2(passportCfg)
	if err != nil {
		slog.Error("httpapi: create passport client", "error", err)
		h.writeError(w, http.StatusInternalServerError, "login client setup failed")
		return
	}

	loginData, err := passport.Login(ctx, email, password, nonEmptyPtr(captchaID), nonEmptyPtr(captchaText))
	if err != nil {
		// Check if the API is requesting a CAPTCHA (code 100032).
		// Return 200 with captcha_id + captcha_url so the JS can display it.
		if apiErr, ok := err.(*httpapi.APIError); ok && apiErr.JSON != nil {
			if data, ok := apiErr.JSON["data"].(map[string]any); ok {
				if cid, ok := data["captcha_id"].(string); ok && cid != "" {
					img, _ := data["item"].(string)
					h.writeJSON(w, http.StatusOK, map[string]string{
						"captcha_id":  cid,
						"captcha_url": img,
					})
					return
				}
			}
		}
		slog.Warn("cloud login failed", "error", err)
		h.writeJSON(w, http.StatusOK, map[string]string{"error": "Login failed: " + err.Error()})
		return
	}

	loginMap, ok := loginData.(map[string]any)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "unexpected login response format")
		return
	}

	authToken, _ := loginMap["auth_token"].(string)
	userID, _ := loginMap["user_id"].(string)
	if authToken == "" || userID == "" {
		h.writeError(w, http.StatusInternalServerError, "missing auth_token or user_id in response")
		return
	}

	// Step 2b: Fetch the account profile so persisted account data matches the
	// Python importer. In particular, country is sourced from profile.country.code.
	profileCountry := strings.ToUpper(strings.TrimSpace(country))
	profileCfg := httpapi.ClientConfig{
		Region:    region,
		AuthToken: authToken,
		UserID:    userID,
	}
	if passportV1, err := httpapi.NewPassportV1(profileCfg); err == nil {
		if profileData, err := passportV1.Profile(ctx); err == nil {
			applyProfileFallbacks(loginMap, profileData, profileCountry)
		} else {
			loginMap["country"] = profileCountry
		}
	} else {
		loginMap["country"] = profileCountry
	}

	// Step 3: Fetch printer list.
	appCfg := httpapi.ClientConfig{
		Region:    region,
		AuthToken: authToken,
		UserID:    userID,
	}
	app, err := httpapi.NewAppV1(appCfg)
	if err != nil {
		slog.Error("httpapi: create app client", "error", err)
		h.writeError(w, http.StatusInternalServerError, "app client setup failed")
		return
	}

	fdmData, err := app.QueryFDMList(ctx)
	if err != nil {
		slog.Warn("query_fdm_list failed", "error", err)
		// Non-fatal: login succeeded but could not fetch printers.
	}
	if h.devMode {
		if raw, err2 := json.Marshal(fdmData); err2 == nil {
			slog.Debug("fdm_list raw response", "json", string(raw))
		}
	}

	// Step 3b: Collect SNs and fetch DSK keys (p2p_key per printer).
	sns := fdmSNs(fdmData)
	var dskKeys map[string]string // keyed by station_sn
	if len(sns) > 0 {
		dskData, dskErr := app.EquipmentGetDSKKeys(ctx, sns, nil)
		if dskErr != nil {
			slog.Warn("equipment_get_dsk_keys failed", "error", dskErr)
		} else {
			dskKeys = parseDSKKeys(dskData)
		}
		if h.devMode {
			if raw, err2 := json.Marshal(dskData); err2 == nil {
				slog.Debug("dsk_keys raw response", "json", string(raw))
			}
		}
	}

	// Step 4: Build config and apply DSK keys.
	cfg := buildConfigFromLogin(loginMap, fdmData, region)
	for i := range cfg.Printers {
		if key, ok := dskKeys[cfg.Printers[i].SN]; ok && key != "" {
			cfg.Printers[i].P2PKey = key
		}
	}

	// Step 4b: Preserve IPs from existing config (API may omit ip_addr
	// when the printer is offline — mirrors Python update_empty_printer_ips).
	// Fall back to the printer_cache DB so IPs survive logout/login cycles.
	if h.cfg != nil {
		if existing, loadErr := h.cfg.Load(); loadErr == nil && existing != nil {
			existingIPs := make(map[string]string)
			for _, p := range existing.Printers {
				if p.SN != "" && p.IPAddr != "" {
					existingIPs[p.SN] = p.IPAddr
				}
			}
			for i := range cfg.Printers {
				if cfg.Printers[i].IPAddr == "" {
					if ip, ok := existingIPs[cfg.Printers[i].SN]; ok {
						cfg.Printers[i].IPAddr = ip
					}
				}
			}
		}
	}
	// DB fallback: restore IPs for any printer still missing one.
	if h.db != nil {
		for i := range cfg.Printers {
			if cfg.Printers[i].IPAddr == "" && cfg.Printers[i].SN != "" {
				if cachedIP, dbErr := h.db.GetPrinterIP(cfg.Printers[i].SN); dbErr == nil && cachedIP != "" {
					cfg.Printers[i].IPAddr = cachedIP
					slog.Info("restored printer IP from cache", "sn", cfg.Printers[i].SN, "ip", cachedIP)
				}
			}
		}
	}
	// Write all known IPs into the cache for future logins.
	if h.db != nil {
		for _, p := range cfg.Printers {
			if p.SN != "" && p.IPAddr != "" {
				_ = h.db.SetPrinterIP(p.SN, p.IPAddr)
			}
		}
	}

	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Save(cfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to persist config")
		return
	}

	// Background discovery for any printer that still has no IP after all
	// the existing fallback logic (existing config, DB cache).
	go h.discoverAndPersistPrinterIPs(cfg.Printers)

	slog.Info("cloud login successful", "email", logging.RedactEmail(email), "region", region)
	h.writeJSON(w, http.StatusOK, map[string]string{"redirect": "/api/ankerctl/server/reload"})
}

// fdmSNs returns the list of station_sn values from the FDM list response.
func fdmSNs(fdmData any) []string {
	list, ok := fdmData.([]any)
	if !ok {
		return nil
	}
	var sns []string
	for _, item := range list {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if sn := stringVal(p, "station_sn"); sn != "" {
			sns = append(sns, sn)
		}
	}
	return sns
}

// parseDSKKeys extracts a map[station_sn]dsk_key from the equipment_get_dsk_keys response.
// Response shape: {"dsk_keys": [{"station_sn": "...", "dsk_key": "..."}, ...]}
func parseDSKKeys(data any) map[string]string {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := m["dsk_keys"].([]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sn := stringVal(entry, "station_sn")
		key := stringVal(entry, "dsk_key")
		if sn != "" {
			result[sn] = key
		}
	}
	return result
}

func applyProfileFallbacks(loginMap map[string]any, profileData any, fallbackCountry string) {
	if loginMap == nil {
		return
	}
	if profile, ok := profileData.(map[string]any); ok {
		if email := stringVal(profile, "email"); email != "" && stringVal(loginMap, "email") == "" {
			loginMap["email"] = email
		}
		if userID := stringVal(profile, "user_id"); userID != "" && stringVal(loginMap, "user_id") == "" {
			loginMap["user_id"] = userID
		}
		if countryCode := profileCountryCode(profile); countryCode != "" {
			loginMap["country"] = countryCode
			return
		}
	}
	if fallbackCountry != "" {
		loginMap["country"] = strings.ToUpper(strings.TrimSpace(fallbackCountry))
	}
}

func profileCountryCode(profile map[string]any) string {
	if profile == nil {
		return ""
	}
	if country := stringVal(profile, "country"); country != "" {
		return strings.ToUpper(strings.TrimSpace(country))
	}
	countryMap, _ := profile["country"].(map[string]any)
	return strings.ToUpper(strings.TrimSpace(stringVal(countryMap, "code")))
}

// buildConfigFromLogin constructs a Config from login and FDM list responses.
func buildConfigFromLogin(loginMap map[string]any, fdmData any, region string) *model.Config {
	authToken, _ := loginMap["auth_token"].(string)
	userID, _ := loginMap["user_id"].(string)
	email, _ := loginMap["email"].(string)
	country := strings.ToUpper(strings.TrimSpace(stringVal(loginMap, "country")))

	cfg := &model.Config{}
	cfg.Account = &model.Account{
		AuthToken: authToken,
		UserID:    userID,
		Email:     email,
		Country:   country,
		Region:    region,
	}

	// Parse printers from FDM list.
	if fdmList, ok := fdmData.([]any); ok {
		for _, item := range fdmList {
			p, ok := item.(map[string]any)
			if !ok {
				continue
			}
			printer := model.Printer{
				ID:      stringVal(p, "station_id"),
				SN:      stringVal(p, "station_sn"),
				Name:    stringVal(p, "station_name"),
				Model:   stringVal(p, "station_model"),
				WifiMAC: stringVal(p, "wifi_mac"),
				IPAddr:  stringVal(p, "ip_addr"),
				P2PDUID: stringVal(p, "p2p_did"),
			}
			if ct, ok := p["create_time"].(float64); ok {
				printer.CreateTime = time.Unix(int64(ct), 0)
			}
			if ut, ok := p["update_time"].(float64); ok {
				printer.UpdateTime = time.Unix(int64(ut), 0)
			}
			if hosts, err := ppppcrypto.DecodeInitString(stringVal(p, "app_conn")); err == nil {
				printer.APIHosts = strings.Join(hosts, ",")
			}
			if hosts, err := ppppcrypto.DecodeInitString(stringVal(p, "p2p_conn")); err == nil {
				printer.P2PHosts = strings.Join(hosts, ",")
			}
			if mqttKeyHex := stringVal(p, "secret_key"); mqttKeyHex != "" {
				if keyBytes, err := hex.DecodeString(mqttKeyHex); err == nil {
					printer.MQTTKey = keyBytes
				}
			}
			if printer.SN != "" {
				cfg.Printers = append(cfg.Printers, printer)
			}
		}
	}

	return cfg
}

// stringVal extracts a string from a JSON-decoded map. It handles both
// string values and JSON numbers (which json.Unmarshal decodes as float64).
// nonEmptyPtr returns a pointer to s if non-empty, otherwise nil.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringVal(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		// Integer-valued float → no decimal point.
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

// ConfigLogout deletes the stored credentials and stops all services,
// then redirects to root (which will show the setup tab).
func (h *Handler) ConfigLogout(w http.ResponseWriter, r *http.Request) {
	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Delete(); err != nil {
		h.writeError(w, http.StatusInternalServerError, "logout failed: "+err.Error())
		return
	}
	if h.stateReloader != nil {
		h.stateReloader.ReloadState()
	}
	if h.svc != nil {
		h.svc.RestartAll()
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ServerReload restarts all registered services and redirects to root.
func (h *Handler) ServerReload(w http.ResponseWriter, r *http.Request) {
	if h.stateReloader != nil {
		h.stateReloader.ReloadState()
	}
	if h.svc != nil {
		h.svc.RestartAll()
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// UploadRateUpdate updates config.upload_rate_mbps.
func (h *Handler) UploadRateUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	rateRaw := r.FormValue("upload_rate_mbps")
	if rateRaw == "" {
		h.writeError(w, http.StatusBadRequest, "upload_rate_mbps missing")
		return
	}
	rate, err := strconv.Atoi(rateRaw)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "upload_rate_mbps must be an integer")
		return
	}

	valid := false
	for _, v := range model.UploadRateMbpsChoices {
		if v == rate {
			valid = true
			break
		}
	}
	if !valid {
		h.writeError(w, http.StatusBadRequest, "invalid upload_rate_mbps")
		return
	}

	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return nil, nil
		}
		cfg.UploadRateMbps = rate
		return cfg, nil
	}); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update upload rate")
		return
	}
	// Python parity: return both the stored rate and the effective rate (which
	// may differ due to env override).
	effectiveRate, effectiveSource := model.ResolveUploadRateMbpsWithSource(rate, 0)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":                       "ok",
		"upload_rate_mbps":             rate,
		"effective_upload_rate_mbps":    effectiveRate,
		"effective_upload_rate_source":  effectiveSource,
	})
}

// discoverAndPersistPrinterIPs runs LAN broadcast discovery for each printer
// that has a P2P DUID but no known IP, and writes the result back to the
// config file and the DB cache. Designed to be called in a background goroutine
// after ConfigUpload or ConfigLogin.
func (h *Handler) discoverAndPersistPrinterIPs(printers []model.Printer) {
	for _, p := range printers {
		if p.P2PDUID == "" || p.IPAddr != "" {
			continue
		}
		p := p // capture for goroutine
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			ip, err := ppppclient.DiscoverLANIP(ctx, p.P2PDUID)
			if err != nil {
				slog.Warn("background IP discovery failed", "duid", logging.RedactID(p.P2PDUID, 4), "error", err)
				return
			}
			ipStr := ip.String()
			slog.Info("background IP discovery succeeded", "duid", logging.RedactID(p.P2PDUID, 4), "sn", p.SN, "ip", ipStr)
			if h.cfg != nil {
				_ = h.cfg.Modify(func(saved *model.Config) (*model.Config, error) {
					if saved == nil {
						return nil, nil
					}
					for i := range saved.Printers {
						if saved.Printers[i].SN == p.SN && saved.Printers[i].IPAddr == "" {
							saved.Printers[i].IPAddr = ipStr
						}
					}
					return saved, nil
				})
			}
			if h.db != nil && p.SN != "" {
				_ = h.db.SetPrinterIP(p.SN, ipStr)
			}
		}()
	}
}
