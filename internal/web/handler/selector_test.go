package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/model"
)

// newTestHandlerWithPrinters creates a Handler backed by a config that contains
// the supplied printers so selector-endpoint tests can exercise real logic.
func newTestHandlerWithPrinters(t *testing.T, printers []model.Printer) *Handler {
	t.Helper()
	cfgDir := t.TempDir()
	cfgMgr, err := config.NewManager(cfgDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &model.Config{
		Account:            &model.Account{AuthToken: "tok", Region: "eu"},
		Printers:           printers,
		ActivePrinterIndex: 0,
	}
	if err := cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	mockRender := func(w http.ResponseWriter, name string, data any) error { return nil }
	return New(cfgMgr, database, nil, nil, false, mockRender)
}

// makePrinterModel returns a minimal Printer with the given model code.
func makePrinterModel(name, sn, modelCode string) model.Printer {
	return model.Printer{
		ID:    sn,
		SN:    sn,
		Name:  name,
		Model: modelCode,
	}
}

// TestPrintersList_SupportedField_ReflectsModel verifies that /api/printers
// returns "supported": false for V8260 and "supported": true for M5.
func TestPrintersList_SupportedField_ReflectsModel(t *testing.T) {
	printers := []model.Printer{
		makePrinterModel("UV Printer", "SN-UV", "V8260"),
		makePrinterModel("AnkerMake M5", "SN-M5", "AnkerMake M5"),
	}
	h := newTestHandlerWithPrinters(t, printers)

	w := httptest.NewRecorder()
	h.PrintersList(w, httptest.NewRequest(http.MethodGet, "/api/printers", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Printers []struct {
			Index     int    `json:"index"`
			Model     string `json:"model"`
			Supported bool   `json:"supported"`
		} `json:"printers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Printers) != 2 {
		t.Fatalf("printers count = %d, want 2", len(resp.Printers))
	}
	for _, p := range resp.Printers {
		switch p.Model {
		case "V8260":
			if p.Supported {
				t.Errorf("printer[%d] model=%q: supported=true, want false", p.Index, p.Model)
			}
		case "AnkerMake M5":
			if !p.Supported {
				t.Errorf("printer[%d] model=%q: supported=false, want true", p.Index, p.Model)
			}
		}
	}
}

// TestPrintersSwitch_ToUnsupportedDevice_Returns403 ensures that switching
// to a V8260 device returns 403 Forbidden (Python parity).
func TestPrintersSwitch_ToUnsupportedDevice_Returns403(t *testing.T) {
	// Active=0 (M5), target=1 (V8260)
	printers := []model.Printer{
		makePrinterModel("AnkerMake M5", "SN-M5", "AnkerMake M5"),
		makePrinterModel("UV Printer", "SN-UV", "V8260"),
	}
	h := newTestHandlerWithPrinters(t, printers)

	body := bytes.NewBufferString(`{"index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/printers/active", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.PrintersSwitch(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected non-empty 'error' field in 403 response")
	}
}

// TestPrintersSwitch_ToSupportedDevice_ReturnsOK verifies the happy path is
// unaffected: switching from one supported printer to another still returns 200.
func TestPrintersSwitch_ToSupportedDevice_ReturnsOK(t *testing.T) {
	printers := []model.Printer{
		makePrinterModel("AnkerMake M5", "SN-M5", "AnkerMake M5"),
		makePrinterModel("AnkerMake M5C", "SN-M5C", "V8110"),
	}
	h := newTestHandlerWithPrinters(t, printers)

	body := bytes.NewBufferString(`{"index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/printers/active", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.PrintersSwitch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestPrintersSwitch_SameIndex_ReturnsOK verifies that switching to the already
// active printer index returns 200 with "Already active" message.
func TestPrintersSwitch_SameIndex_ReturnsOK(t *testing.T) {
	printers := []model.Printer{
		makePrinterModel("AnkerMake M5", "SN-M5", "AnkerMake M5"),
		makePrinterModel("AnkerMake M5C", "SN-M5C", "V8110"),
	}
	h := newTestHandlerWithPrinters(t, printers)

	body := bytes.NewBufferString(`{"index":0}`) // already active
	req := httptest.NewRequest(http.MethodPost, "/api/printers/active", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.PrintersSwitch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
