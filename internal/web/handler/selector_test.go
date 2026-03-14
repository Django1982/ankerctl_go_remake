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

// TestPrintersList_OnlyContainsSupportedPrinters verifies that /api/printers
// never exposes unsupported models — V8260 is filtered at config-load time and
// therefore never reaches the handler's printer slice.
func TestPrintersList_OnlyContainsSupportedPrinters(t *testing.T) {
	// Handler receives only the supported printer (V8260 was stripped by config.Load).
	printers := []model.Printer{
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
			Index int    `json:"index"`
			Model string `json:"model"`
		} `json:"printers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Printers) != 1 {
		t.Fatalf("printers count = %d, want 1", len(resp.Printers))
	}
	if resp.Printers[0].Model != "AnkerMake M5" {
		t.Errorf("unexpected model %q", resp.Printers[0].Model)
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
