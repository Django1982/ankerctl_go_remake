package handler

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/django1982/ankerctl/internal/httpapi"
	"github.com/django1982/ankerctl/internal/model"
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

	loginData, err := passport.Login(ctx, email, password, nil, nil)
	if err != nil {
		slog.Warn("cloud login failed", "error", err)
		h.writeError(w, http.StatusUnauthorized, "cloud login failed: "+err.Error())
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

	// Step 4: Build and save config.
	cfg := buildConfigFromLogin(loginMap, fdmData, region)

	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Save(cfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to persist config")
		return
	}

	slog.Info("cloud login successful", "email", email, "region", region)
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "region": region})
}

// buildConfigFromLogin constructs a Config from login and FDM list responses.
func buildConfigFromLogin(loginMap map[string]any, fdmData any, region string) *model.Config {
	authToken, _ := loginMap["auth_token"].(string)
	userID, _ := loginMap["user_id"].(string)
	email, _ := loginMap["email"].(string)
	country, _ := loginMap["country"].(string)

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
				SN:   stringVal(p, "station_sn"),
				Name: stringVal(p, "station_name"),
			}
			if mqttKeyHex, ok := p["secret_key"].(string); ok {
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

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// ServerReload restarts all registered services.
func (h *Handler) ServerReload(w http.ResponseWriter, _ *http.Request) {
	if h.svc != nil {
		h.svc.RestartAll()
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "upload_rate_mbps": rate})
}
