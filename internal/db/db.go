// Package db provides thread-safe SQLite-backed persistence for ankerctl.
//
// It wraps a single *sql.DB (opened with the modernc.org/sqlite driver)
// and exposes high-level CRUD helpers for the two persistent stores:
//   - PrintHistory  (print_history table)
//   - FilamentStore (filaments table)
//
// All exported methods serialise access through a single sync.Mutex so
// that callers never need to think about connection-level thread safety.
//
// Usage:
//
//	d, err := db.Open("/path/to/ankerctl.db")
//	if err != nil { ... }
//	defer d.Close()
//
// Python sources: web/service/history.py, web/service/filament.py
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	// Pure-Go SQLite driver — no CGo, works on linux/amd64, linux/arm64, linux/arm/v7.
	_ "modernc.org/sqlite"
)

// DB is a thread-safe wrapper around a SQLite database.
// The zero value is not usable; call Open to obtain an instance.
type DB struct {
	db  *sql.DB
	mu  sync.Mutex
	log *slog.Logger
}

// Open opens (or creates) a SQLite database at path and runs all schema
// migrations. Pass ":memory:" for an in-memory database (useful in tests).
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db.Open: open sqlite %q: %w", path, err)
	}

	// SQLite does not support concurrent writers; a single connection is
	// safest here. The outer Mutex prevents concurrent queries anyway.
	sqlDB.SetMaxOpenConns(1)

	d := &DB{
		db:  sqlDB,
		log: slog.With("component", "db"),
	}

	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db.Open: migrate: %w", err)
	}

	return d, nil
}

// Close releases the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// migrate applies all schema creation and incremental ALTER TABLE migrations
// for every table managed by this package. It is idempotent: running it
// against an already-current schema is always safe.
func (d *DB) migrate() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Enable WAL mode for better read concurrency (even with one writer).
	if _, err := d.db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return fmt.Errorf("set WAL mode: %w", err)
	}

	// Enforce FK constraints (SQLite disables them by default).
	if _, err := d.db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := migrateHistory(d.db, d.log); err != nil {
		return fmt.Errorf("history migration: %w", err)
	}

	if err := migrateFilaments(d.db, d.log); err != nil {
		return fmt.Errorf("filament migration: %w", err)
	}

	if err := migratePrinterCache(d.db); err != nil {
		return fmt.Errorf("printer cache migration: %w", err)
	}

	return nil
}

// withTx executes fn inside a database transaction. It commits on success
// and rolls back on any error returned by fn.
// Callers must hold d.mu before calling withTx.
func (d *DB) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// tableColumns returns the set of column names for the given table by
// querying PRAGMA table_info. It is used during migrations to detect
// whether a column already exists before issuing ALTER TABLE ADD COLUMN.
// Callers must NOT hold d.mu (or must pass a *sql.Tx instead of d.db).
func tableColumns(q interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, table string) (map[string]struct{}, error) {
	rows, err := q.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("PRAGMA table_info(%s): %w", table, err)
	}
	defer rows.Close()

	cols := make(map[string]struct{})
	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scan PRAGMA row: %w", err)
		}
		cols[name] = struct{}{}
	}
	return cols, rows.Err()
}
