package db

import (
	"testing"
)

func TestPrinterCache_SetAndGet(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	// Non-existent SN returns empty string without error.
	ip, err := d.GetPrinterIP("UNKNOWN")
	if err != nil {
		t.Fatalf("GetPrinterIP missing: %v", err)
	}
	if ip != "" {
		t.Errorf("expected empty for missing SN, got %q", ip)
	}

	// Set then get.
	if err := d.SetPrinterIP("SN001", "192.168.1.100"); err != nil {
		t.Fatalf("SetPrinterIP: %v", err)
	}
	ip, err = d.GetPrinterIP("SN001")
	if err != nil {
		t.Fatalf("GetPrinterIP after set: %v", err)
	}
	if ip != "192.168.1.100" {
		t.Errorf("got %q, want %q", ip, "192.168.1.100")
	}
}

func TestPrinterCache_Upsert(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if err := d.SetPrinterIP("SN001", "10.0.0.1"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := d.SetPrinterIP("SN001", "10.0.0.2"); err != nil {
		t.Fatalf("second set (upsert): %v", err)
	}

	ip, err := d.GetPrinterIP("SN001")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if ip != "10.0.0.2" {
		t.Errorf("expected updated IP 10.0.0.2, got %q", ip)
	}
}

func TestPrinterCache_MultipleSNs(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	pairs := map[string]string{
		"SN-A": "192.168.1.1",
		"SN-B": "192.168.1.2",
		"SN-C": "192.168.1.3",
	}
	for sn, ip := range pairs {
		if err := d.SetPrinterIP(sn, ip); err != nil {
			t.Fatalf("set %s: %v", sn, err)
		}
	}
	for sn, wantIP := range pairs {
		got, err := d.GetPrinterIP(sn)
		if err != nil {
			t.Fatalf("get %s: %v", sn, err)
		}
		if got != wantIP {
			t.Errorf("SN %s: got %q, want %q", sn, got, wantIP)
		}
	}
}

func TestPrinterCache_EmptySNErrors(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if err := d.SetPrinterIP("", "1.2.3.4"); err == nil {
		t.Error("expected error for empty SN on Set")
	}
	if _, err := d.GetPrinterIP(""); err == nil {
		t.Error("expected error for empty SN on Get")
	}
}

func TestPrinterCache_EmptyIPErrors(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if err := d.SetPrinterIP("SN001", ""); err == nil {
		t.Error("expected error for empty IP on Set")
	}
}
