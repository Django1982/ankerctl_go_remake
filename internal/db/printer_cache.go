package db

import (
	"database/sql"
	"fmt"
	"time"
)

// printerCacheSchema holds the last-known LAN IP per printer serial number.
// This survives logout/login cycles so that PPPP can reconnect without a
// full LAN broadcast scan when the printer is already known.
const printerCacheSchema = `
CREATE TABLE IF NOT EXISTS printer_cache (
    sn         TEXT PRIMARY KEY,
    ip_addr    TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);`

// migratePrinterCache creates the printer_cache table if it does not exist.
// It is idempotent and safe to call multiple times.
func migratePrinterCache(db *sql.DB) error {
	if _, err := db.Exec(printerCacheSchema); err != nil {
		return fmt.Errorf("create printer_cache table: %w", err)
	}
	return nil
}

// SetPrinterIP upserts the last-known IP address for a printer identified
// by its serial number. updated_at is stored as a Unix timestamp (seconds).
func (d *DB) SetPrinterIP(sn, ip string) error {
	if sn == "" {
		return fmt.Errorf("SetPrinterIP: sn must not be empty")
	}
	if ip == "" {
		return fmt.Errorf("SetPrinterIP: ip must not be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`INSERT INTO printer_cache (sn, ip_addr, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(sn) DO UPDATE SET ip_addr=excluded.ip_addr, updated_at=excluded.updated_at`,
		sn, ip, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("SetPrinterIP: %w", err)
	}
	return nil
}

// GetPrinterIP returns the last-known IP address for a printer serial number.
// It returns ("", nil) when no cached entry exists (sql.ErrNoRows is swallowed).
func (d *DB) GetPrinterIP(sn string) (string, error) {
	if sn == "" {
		return "", fmt.Errorf("GetPrinterIP: sn must not be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var ip string
	err := d.db.QueryRow(
		`SELECT ip_addr FROM printer_cache WHERE sn = ?`, sn,
	).Scan(&ip)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetPrinterIP: %w", err)
	}
	return ip, nil
}
