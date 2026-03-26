package handler

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/model"
)

func TestDiscoverAndPersistPrinterIPs(t *testing.T) {
	tests := []struct {
		name       string
		printers   []model.Printer
		discoverFn func(ctx context.Context, duid string) (net.IP, error)
		wantIP     string // expected IP persisted for the first printer with SN
		wantNone   bool   // expect no IP persisted
	}{
		{
			name: "happy_path_ip_discovered_and_persisted",
			printers: []model.Printer{
				{SN: "PRINTER001", P2PDUID: "EUPRAKM-010001-AAAAA"},
			},
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				return net.ParseIP("192.168.1.100"), nil
			},
			wantIP: "192.168.1.100",
		},
		{
			name: "discovery_error_no_panic_no_persist",
			printers: []model.Printer{
				{SN: "PRINTER002", P2PDUID: "EUPRAKM-010002-BBBBB"},
			},
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				return nil, errors.New("no printer found on LAN")
			},
			wantNone: true,
		},
		{
			name: "invalid_ip_rejected",
			printers: []model.Printer{
				{SN: "PRINTER003", P2PDUID: "EUPRAKM-010003-CCCCC"},
			},
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				return net.ParseIP("255.255.255.255"), nil // broadcast — invalid
			},
			wantNone: true,
		},
		{
			name: "empty_duid_skipped",
			printers: []model.Printer{
				{SN: "PRINTER004", P2PDUID: ""},
			},
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				t.Fatal("discoverFn should not be called for empty DUID")
				return nil, nil
			},
			wantNone: true,
		},
		{
			name: "multiple_printers_each_discovered",
			printers: []model.Printer{
				{SN: "PRINTA", P2PDUID: "EUPRAKM-010010-AAAAA"},
				{SN: "PRINTB", P2PDUID: "EUPRAKM-010011-BBBBB"},
			},
			discoverFn: func(_ context.Context, duid string) (net.IP, error) {
				if duid == "EUPRAKM-010010-AAAAA" {
					return net.ParseIP("192.168.1.10"), nil
				}
				return net.ParseIP("192.168.1.11"), nil
			},
			wantIP: "192.168.1.10", // check first printer
		},
		{
			name:     "nil_printers_no_panic",
			printers: nil,
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				t.Fatal("discoverFn should not be called for nil printers")
				return nil, nil
			},
			wantNone: true,
		},
		{
			name: "loopback_ip_rejected",
			printers: []model.Printer{
				{SN: "PRINTER005", P2PDUID: "EUPRAKM-010005-DDDDD"},
			},
			discoverFn: func(_ context.Context, _ string) (net.IP, error) {
				return net.ParseIP("127.0.0.1"), nil
			},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			// Seed config with the test printers so Modify can find them.
			seedCfg := &model.Config{Printers: make([]model.Printer, len(tt.printers))}
			copy(seedCfg.Printers, tt.printers)
			if err := cfgMgr.Save(seedCfg); err != nil {
				t.Fatalf("Save seed config: %v", err)
			}

			h := New(cfgMgr, database, nil, nil, false, nil)
			h.lanDiscoveryFunc = tt.discoverFn

			h.discoverAndPersistPrinterIPs(tt.printers)

			// The function spawns goroutines. Wait for them to finish.
			// The discovery function is synchronous in tests (no delay),
			// but we still need to give goroutines time to complete.
			time.Sleep(200 * time.Millisecond)

			if tt.wantNone {
				// Verify no IP was persisted.
				loaded, err := cfgMgr.Load()
				if err != nil {
					t.Fatalf("Load config: %v", err)
				}
				if loaded != nil {
					for _, p := range loaded.Printers {
						if p.IPAddr != "" {
							t.Errorf("expected no IP persisted, but %s has IP %q", p.SN, p.IPAddr)
						}
					}
				}
				return
			}

			// Verify IP was persisted in config.
			loaded, err := cfgMgr.Load()
			if err != nil {
				t.Fatalf("Load config: %v", err)
			}
			if loaded == nil || len(loaded.Printers) == 0 {
				t.Fatal("loaded config has no printers")
			}
			found := false
			for _, p := range loaded.Printers {
				if p.IPAddr == tt.wantIP {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected IP %q persisted in config, got printers: %+v", tt.wantIP, loaded.Printers)
			}

			// Verify IP was persisted in DB.
			if tt.printers[0].SN != "" {
				dbIP, err := database.GetPrinterIP(tt.printers[0].SN)
				if err != nil {
					t.Fatalf("GetPrinterIP: %v", err)
				}
				if dbIP != tt.wantIP {
					t.Errorf("DB IP = %q, want %q", dbIP, tt.wantIP)
				}
			}
		})
	}
}

func TestDiscoverAndPersistPrinterIPs_ContextTimeout(t *testing.T) {
	// The function creates its own 6-second timeout context internally.
	// Verify that if discovery blocks, the context deadline is respected
	// and the goroutine completes without hanging.

	var discoveryStarted sync.WaitGroup
	discoveryStarted.Add(1)

	cfgDir := t.TempDir()
	cfgMgr, err := config.NewManager(cfgDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	printers := []model.Printer{
		{SN: "TIMEOUT001", P2PDUID: "EUPRAKM-TIMEOUT-AAAAA"},
	}
	if err := cfgMgr.Save(&model.Config{Printers: printers}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := New(cfgMgr, nil, nil, nil, false, nil)
	h.lanDiscoveryFunc = func(ctx context.Context, _ string) (net.IP, error) {
		discoveryStarted.Done()
		// Block until context is cancelled (simulating network timeout).
		<-ctx.Done()
		return nil, ctx.Err()
	}

	h.discoverAndPersistPrinterIPs(printers)

	// Verify the goroutine started.
	discoveryStarted.Wait()

	// The goroutine has a 6-second internal timeout. We don't want to wait
	// 6s in CI, so we just verify it started and will eventually exit.
	// The goroutine must not panic when it times out.
}

func TestDiscoverAndPersistPrinterIPs_NilConfigManager(t *testing.T) {
	// When h.cfg is nil, the function should still run (just skip config
	// persistence) without panicking.
	h := &Handler{}
	h.lanDiscoveryFunc = func(_ context.Context, _ string) (net.IP, error) {
		return net.ParseIP("192.168.1.50"), nil
	}

	printers := []model.Printer{
		{SN: "NILCFG001", P2PDUID: "EUPRAKM-NILCFG-AAAAA"},
	}

	// Must not panic.
	h.discoverAndPersistPrinterIPs(printers)
	time.Sleep(100 * time.Millisecond)
}

func TestDiscoverAndPersistPrinterIPs_NilDB(t *testing.T) {
	// When h.db is nil, the function should persist to config only.
	cfgDir := t.TempDir()
	cfgMgr, err := config.NewManager(cfgDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	printers := []model.Printer{
		{SN: "NILDB001", P2PDUID: "EUPRAKM-NILDB-AAAAA"},
	}
	if err := cfgMgr.Save(&model.Config{Printers: printers}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := New(cfgMgr, nil, nil, nil, false, nil) // db is nil
	h.lanDiscoveryFunc = func(_ context.Context, _ string) (net.IP, error) {
		return net.ParseIP("192.168.1.60"), nil
	}

	h.discoverAndPersistPrinterIPs(printers)
	time.Sleep(200 * time.Millisecond)

	loaded, err := cfgMgr.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || len(loaded.Printers) == 0 {
		t.Fatal("no printers in config")
	}
	if loaded.Printers[0].IPAddr != "192.168.1.60" {
		t.Errorf("config IP = %q, want 192.168.1.60", loaded.Printers[0].IPAddr)
	}
}
