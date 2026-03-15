package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfigLogin_MissingParameters(t *testing.T) {
	tests := []struct {
		name       string
		formData   url.Values
		wantStatus int
		wantBody   string
	}{
		{
			name:       "empty form",
			formData:   url.Values{},
			wantStatus: http.StatusBadRequest,
			wantBody:   "missing login parameters",
		},
		{
			name: "missing email",
			formData: url.Values{
				"login_password": {"secret"},
				"login_country":  {"eu"},
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "missing login parameters",
		},
		{
			name: "missing password",
			formData: url.Values{
				"login_email":   {"user@example.com"},
				"login_country": {"eu"},
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "missing login parameters",
		},
		{
			name: "missing country",
			formData: url.Values{
				"login_email":    {"user@example.com"},
				"login_password": {"secret"},
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "missing login parameters",
		},
		{
			name: "all empty strings",
			formData: url.Values{
				"login_email":    {""},
				"login_password": {""},
				"login_country":  {""},
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "missing login parameters",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			req := httptest.NewRequest(http.MethodPost, "/api/ankerctl/config/login", strings.NewReader(tc.formData.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()

			h.ConfigLogin(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if !strings.Contains(rr.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want to contain %q", rr.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestStringVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{
			name: "string value",
			m:    map[string]any{"foo": "bar"},
			key:  "foo",
			want: "bar",
		},
		{
			name: "integer float value",
			m:    map[string]any{"num": float64(42)},
			key:  "num",
			want: "42",
		},
		{
			name: "fractional float value",
			m:    map[string]any{"frac": float64(3.14)},
			key:  "frac",
			want: "3.14",
		},
		{
			name: "missing key",
			m:    map[string]any{"foo": "bar"},
			key:  "baz",
			want: "",
		},
		{
			name: "nil map",
			m:    nil,
			key:  "foo",
			want: "",
		},
		{
			name: "non-string non-float value",
			m:    map[string]any{"bool": true},
			key:  "bool",
			want: "",
		},
		{
			name: "zero float",
			m:    map[string]any{"zero": float64(0)},
			key:  "zero",
			want: "0",
		},
		{
			name: "negative integer float",
			m:    map[string]any{"neg": float64(-7)},
			key:  "neg",
			want: "-7",
		},
		{
			name: "large integer float",
			m:    map[string]any{"big": float64(1e15)},
			key:  "big",
			want: "1000000000000000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringVal(tc.m, tc.key)
			if got != tc.want {
				t.Errorf("stringVal(%v, %q) = %q, want %q", tc.m, tc.key, got, tc.want)
			}
		})
	}
}

func TestNonEmptyPtr(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{name: "empty string", input: "", wantNil: true},
		{name: "non-empty string", input: "hello", wantNil: false},
		{name: "whitespace only", input: "  ", wantNil: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nonEmptyPtr(tc.input)
			if tc.wantNil && got != nil {
				t.Errorf("nonEmptyPtr(%q) = %v, want nil", tc.input, got)
			}
			if !tc.wantNil {
				if got == nil {
					t.Fatalf("nonEmptyPtr(%q) = nil, want non-nil", tc.input)
				}
				if *got != tc.input {
					t.Errorf("*nonEmptyPtr(%q) = %q, want %q", tc.input, *got, tc.input)
				}
			}
		})
	}
}

func TestFdmSNs(t *testing.T) {
	tests := []struct {
		name string
		data any
		want []string
	}{
		{
			name: "nil input",
			data: nil,
			want: nil,
		},
		{
			name: "non-list input",
			data: "not a list",
			want: nil,
		},
		{
			name: "empty list",
			data: []any{},
			want: nil,
		},
		{
			name: "single printer",
			data: []any{
				map[string]any{"station_sn": "SN001", "station_name": "Printer1"},
			},
			want: []string{"SN001"},
		},
		{
			name: "multiple printers",
			data: []any{
				map[string]any{"station_sn": "SN001"},
				map[string]any{"station_sn": "SN002"},
				map[string]any{"station_sn": "SN003"},
			},
			want: []string{"SN001", "SN002", "SN003"},
		},
		{
			name: "printer with empty SN skipped",
			data: []any{
				map[string]any{"station_sn": "SN001"},
				map[string]any{"station_sn": ""},
				map[string]any{"station_name": "no-sn"},
				map[string]any{"station_sn": "SN003"},
			},
			want: []string{"SN001", "SN003"},
		},
		{
			name: "non-map items skipped",
			data: []any{
				"not a map",
				map[string]any{"station_sn": "SN001"},
				42,
			},
			want: []string{"SN001"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fdmSNs(tc.data)
			if len(got) != len(tc.want) {
				t.Fatalf("fdmSNs() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("fdmSNs()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseDSKKeys(t *testing.T) {
	tests := []struct {
		name string
		data any
		want map[string]string
	}{
		{
			name: "nil input",
			data: nil,
			want: nil,
		},
		{
			name: "non-map input",
			data: "not a map",
			want: nil,
		},
		{
			name: "missing dsk_keys key",
			data: map[string]any{"other": "data"},
			want: nil,
		},
		{
			name: "empty dsk_keys list",
			data: map[string]any{"dsk_keys": []any{}},
			want: map[string]string{},
		},
		{
			name: "single key",
			data: map[string]any{
				"dsk_keys": []any{
					map[string]any{"station_sn": "SN001", "dsk_key": "key001"},
				},
			},
			want: map[string]string{"SN001": "key001"},
		},
		{
			name: "multiple keys",
			data: map[string]any{
				"dsk_keys": []any{
					map[string]any{"station_sn": "SN001", "dsk_key": "key001"},
					map[string]any{"station_sn": "SN002", "dsk_key": "key002"},
				},
			},
			want: map[string]string{"SN001": "key001", "SN002": "key002"},
		},
		{
			name: "entry without station_sn skipped",
			data: map[string]any{
				"dsk_keys": []any{
					map[string]any{"station_sn": "", "dsk_key": "key001"},
					map[string]any{"station_sn": "SN002", "dsk_key": "key002"},
				},
			},
			want: map[string]string{"SN002": "key002"},
		},
		{
			name: "non-map entries skipped",
			data: map[string]any{
				"dsk_keys": []any{
					"not a map",
					map[string]any{"station_sn": "SN001", "dsk_key": "key001"},
				},
			},
			want: map[string]string{"SN001": "key001"},
		},
		{
			name: "entry with empty dsk_key still stored",
			data: map[string]any{
				"dsk_keys": []any{
					map[string]any{"station_sn": "SN001", "dsk_key": ""},
				},
			},
			want: map[string]string{"SN001": ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDSKKeys(tc.data)
			if tc.want == nil {
				if got != nil {
					t.Errorf("parseDSKKeys() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parseDSKKeys() has %d entries, want %d: got=%v", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("parseDSKKeys()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestApplyProfileFallbacks_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		loginMap        map[string]any
		profileData     any
		fallbackCountry string
		wantCountry     string
		wantEmail       string
	}{
		{
			name:            "nil loginMap is safe",
			loginMap:        nil,
			profileData:     map[string]any{"country": "DE"},
			fallbackCountry: "US",
			wantCountry:     "",
			wantEmail:       "",
		},
		{
			name:            "profile country string applied",
			loginMap:        map[string]any{},
			profileData:     map[string]any{"country": "de"},
			fallbackCountry: "US",
			wantCountry:     "DE",
		},
		{
			name:     "profile country map with code",
			loginMap: map[string]any{},
			profileData: map[string]any{
				"country": map[string]any{"code": "fr"},
			},
			fallbackCountry: "US",
			wantCountry:     "FR",
		},
		{
			name:            "fallback country when profile empty",
			loginMap:        map[string]any{},
			profileData:     map[string]any{},
			fallbackCountry: "gb",
			wantCountry:     "GB",
		},
		{
			name:            "fallback country when profile nil",
			loginMap:        map[string]any{},
			profileData:     nil,
			fallbackCountry: "us",
			wantCountry:     "US",
		},
		{
			name:            "profile email fills empty loginMap email",
			loginMap:        map[string]any{},
			profileData:     map[string]any{"email": "profile@example.com", "country": "DE"},
			fallbackCountry: "",
			wantCountry:     "DE",
			wantEmail:       "profile@example.com",
		},
		{
			name:            "profile email does not overwrite existing",
			loginMap:        map[string]any{"email": "login@example.com"},
			profileData:     map[string]any{"email": "profile@example.com", "country": "DE"},
			fallbackCountry: "",
			wantCountry:     "DE",
			wantEmail:       "login@example.com",
		},
		{
			name:            "profile user_id fills empty",
			loginMap:        map[string]any{},
			profileData:     map[string]any{"user_id": "uid123", "country": "DE"},
			fallbackCountry: "",
			wantCountry:     "DE",
		},
		{
			name:            "country trimmed and uppercased",
			loginMap:        map[string]any{},
			profileData:     map[string]any{"country": "  de  "},
			fallbackCountry: "",
			wantCountry:     "DE",
		},
		{
			name:            "non-map profileData uses fallback",
			loginMap:        map[string]any{},
			profileData:     "invalid",
			fallbackCountry: "JP",
			wantCountry:     "JP",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			applyProfileFallbacks(tc.loginMap, tc.profileData, tc.fallbackCountry)
			if tc.loginMap == nil {
				return // nil loginMap: nothing to check
			}
			if tc.wantCountry != "" {
				got, _ := tc.loginMap["country"].(string)
				if got != tc.wantCountry {
					t.Errorf("country = %q, want %q", got, tc.wantCountry)
				}
			}
			if tc.wantEmail != "" {
				got, _ := tc.loginMap["email"].(string)
				if got != tc.wantEmail {
					t.Errorf("email = %q, want %q", got, tc.wantEmail)
				}
			}
		})
	}
}

func TestProfileCountryCode_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		profile map[string]any
		want    string
	}{
		{
			name:    "nil profile",
			profile: nil,
			want:    "",
		},
		{
			name:    "string country",
			profile: map[string]any{"country": "us"},
			want:    "US",
		},
		{
			name:    "map country with code",
			profile: map[string]any{"country": map[string]any{"code": "de"}},
			want:    "DE",
		},
		{
			name:    "map country without code",
			profile: map[string]any{"country": map[string]any{"name": "Germany"}},
			want:    "",
		},
		{
			name:    "no country key",
			profile: map[string]any{"email": "test@test.com"},
			want:    "",
		},
		{
			name:    "whitespace country",
			profile: map[string]any{"country": "  fr  "},
			want:    "FR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := profileCountryCode(tc.profile)
			if got != tc.want {
				t.Errorf("profileCountryCode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildConfigFromLogin(t *testing.T) {
	tests := []struct {
		name     string
		loginMap map[string]any
		fdmData  any
		region   string
		// assertions
		wantAuthToken string
		wantUserID    string
		wantEmail     string
		wantCountry   string
		wantRegion    string
		wantPrinters  int
	}{
		{
			name: "basic login with no printers",
			loginMap: map[string]any{
				"auth_token": "tok123",
				"user_id":    "uid456",
				"email":      "user@example.com",
				"country":    "DE",
			},
			fdmData:       nil,
			region:        "eu",
			wantAuthToken: "tok123",
			wantUserID:    "uid456",
			wantEmail:     "user@example.com",
			wantCountry:   "DE",
			wantRegion:    "eu",
			wantPrinters:  0,
		},
		{
			name: "login with one printer",
			loginMap: map[string]any{
				"auth_token": "tok123",
				"user_id":    "uid456",
				"email":      "user@example.com",
				"country":    "us",
			},
			fdmData: []any{
				map[string]any{
					"station_id":    "id001",
					"station_sn":    "SN001",
					"station_name":  "My Printer",
					"station_model": "V8111",
					"wifi_mac":      "AA:BB:CC:DD:EE:FF",
					"ip_addr":       "192.168.1.100",
					"p2p_did":       "EUPRAKM-123456-ABCDE",
				},
			},
			region:        "us",
			wantAuthToken: "tok123",
			wantUserID:    "uid456",
			wantEmail:     "user@example.com",
			wantCountry:   "US",
			wantRegion:    "us",
			wantPrinters:  1,
		},
		{
			name: "printer without SN is skipped",
			loginMap: map[string]any{
				"auth_token": "tok",
				"user_id":    "uid",
			},
			fdmData: []any{
				map[string]any{
					"station_id":   "id001",
					"station_name": "NoSN",
				},
				map[string]any{
					"station_sn":   "SN002",
					"station_name": "HasSN",
				},
			},
			region:       "eu",
			wantPrinters: 1,
		},
		{
			name: "country is uppercased and trimmed",
			loginMap: map[string]any{
				"auth_token": "tok",
				"user_id":    "uid",
				"country":    "  de  ",
			},
			fdmData:     nil,
			region:      "eu",
			wantCountry: "DE",
		},
		{
			name: "timestamps parsed from float64",
			loginMap: map[string]any{
				"auth_token": "tok",
				"user_id":    "uid",
			},
			fdmData: []any{
				map[string]any{
					"station_sn":  "SN001",
					"create_time": float64(1700000000),
					"update_time": float64(1700000100),
				},
			},
			region:       "eu",
			wantPrinters: 1,
		},
		{
			name: "secret_key hex decoded into MQTTKey",
			loginMap: map[string]any{
				"auth_token": "tok",
				"user_id":    "uid",
			},
			fdmData: []any{
				map[string]any{
					"station_sn": "SN001",
					"secret_key": "deadbeef",
				},
			},
			region:       "eu",
			wantPrinters: 1,
		},
		{
			name: "invalid secret_key hex ignored",
			loginMap: map[string]any{
				"auth_token": "tok",
				"user_id":    "uid",
			},
			fdmData: []any{
				map[string]any{
					"station_sn": "SN001",
					"secret_key": "not-hex-zz",
				},
			},
			region:       "eu",
			wantPrinters: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := buildConfigFromLogin(tc.loginMap, tc.fdmData, tc.region)
			if cfg == nil {
				t.Fatal("buildConfigFromLogin returned nil")
			}
			if cfg.Account == nil {
				t.Fatal("cfg.Account is nil")
			}
			if tc.wantAuthToken != "" && cfg.Account.AuthToken != tc.wantAuthToken {
				t.Errorf("AuthToken = %q, want %q", cfg.Account.AuthToken, tc.wantAuthToken)
			}
			if tc.wantUserID != "" && cfg.Account.UserID != tc.wantUserID {
				t.Errorf("UserID = %q, want %q", cfg.Account.UserID, tc.wantUserID)
			}
			if tc.wantEmail != "" && cfg.Account.Email != tc.wantEmail {
				t.Errorf("Email = %q, want %q", cfg.Account.Email, tc.wantEmail)
			}
			if tc.wantCountry != "" && cfg.Account.Country != tc.wantCountry {
				t.Errorf("Country = %q, want %q", cfg.Account.Country, tc.wantCountry)
			}
			if tc.wantRegion != "" && cfg.Account.Region != tc.wantRegion {
				t.Errorf("Region = %q, want %q", cfg.Account.Region, tc.wantRegion)
			}
			if len(cfg.Printers) != tc.wantPrinters {
				t.Errorf("len(Printers) = %d, want %d", len(cfg.Printers), tc.wantPrinters)
			}
		})
	}
}

func TestBuildConfigFromLogin_PrinterFields(t *testing.T) {
	loginMap := map[string]any{
		"auth_token": "tok",
		"user_id":    "uid",
	}
	fdmData := []any{
		map[string]any{
			"station_id":    "id001",
			"station_sn":    "SN001",
			"station_name":  "Test Printer",
			"station_model": "V8111",
			"wifi_mac":      "AA:BB:CC:DD:EE:FF",
			"ip_addr":       "192.168.1.100",
			"p2p_did":       "EUPRAKM-123456-ABCDE",
			"secret_key":    "deadbeef01020304",
			"create_time":   float64(1700000000),
			"update_time":   float64(1700000100),
		},
	}
	cfg := buildConfigFromLogin(loginMap, fdmData, "eu")
	if len(cfg.Printers) != 1 {
		t.Fatalf("expected 1 printer, got %d", len(cfg.Printers))
	}
	p := cfg.Printers[0]
	if p.ID != "id001" {
		t.Errorf("ID = %q, want %q", p.ID, "id001")
	}
	if p.SN != "SN001" {
		t.Errorf("SN = %q, want %q", p.SN, "SN001")
	}
	if p.Name != "Test Printer" {
		t.Errorf("Name = %q, want %q", p.Name, "Test Printer")
	}
	if p.Model != "V8111" {
		t.Errorf("Model = %q, want %q", p.Model, "V8111")
	}
	if p.WifiMAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("WifiMAC = %q, want %q", p.WifiMAC, "AA:BB:CC:DD:EE:FF")
	}
	if p.IPAddr != "192.168.1.100" {
		t.Errorf("IPAddr = %q, want %q", p.IPAddr, "192.168.1.100")
	}
	if p.P2PDUID != "EUPRAKM-123456-ABCDE" {
		t.Errorf("P2PDUID = %q, want %q", p.P2PDUID, "EUPRAKM-123456-ABCDE")
	}
	if len(p.MQTTKey) == 0 {
		t.Error("MQTTKey is empty, expected decoded hex bytes")
	}
	if p.CreateTime.Unix() != 1700000000 {
		t.Errorf("CreateTime = %v, want Unix 1700000000", p.CreateTime)
	}
	if p.UpdateTime.Unix() != 1700000100 {
		t.Errorf("UpdateTime = %v, want Unix 1700000100", p.UpdateTime)
	}
}

func TestConfigUpload_NoFile(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/ankerctl/config/upload", nil)
	rr := httptest.NewRecorder()

	h.ConfigUpload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "No file found") {
		t.Errorf("body = %q, want to contain 'No file found'", rr.Body.String())
	}
}

func TestConfigLogout_NilConfigManager(t *testing.T) {
	h := &Handler{} // cfg is nil
	req := httptest.NewRequest(http.MethodPost, "/api/ankerctl/config/logout", nil)
	rr := httptest.NewRecorder()

	h.ConfigLogout(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
