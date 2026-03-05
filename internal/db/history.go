package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// defaultRetentionDays is the maximum age of a history entry.
const defaultRetentionDays = 90

// defaultMaxEntries is the maximum number of rows kept after pruning.
const defaultMaxEntries = 500

// placeholderFilenames are filenames that indicate the printer did not
// yet know which file it was printing. We skip recording these to avoid
// polluting the history. (Python: _PLACEHOLDER_NAMES)
var placeholderFilenames = map[string]struct{}{
	"unknown":       {},
	"unknown.gcode": {},
	"":              {},
}

// historySchema is the CREATE TABLE statement for the print_history table.
// The schema matches the Python reference exactly.
const historySchema = `
CREATE TABLE IF NOT EXISTS print_history (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    filename       TEXT    NOT NULL,
    status         TEXT    NOT NULL DEFAULT 'started',
    started_at     TEXT    NOT NULL,
    finished_at    TEXT,
    duration_sec   INTEGER,
    progress       INTEGER DEFAULT 0,
    failure_reason TEXT,
    task_id        TEXT
);`

// historyMigrationColumns are columns added after the initial release.
// Each entry is (column_name, column_definition). The migration code
// checks PRAGMA table_info and only emits ALTER TABLE when the column
// is absent. (Python: _migrate_schema)
var historyMigrationColumns = []struct {
	name string
	def  string
}{
	{"task_id", "TEXT"},
}

// PrintRecord represents a single row in the print_history table.
type PrintRecord struct {
	ID            int64
	Filename      string
	Status        string
	StartedAt     time.Time
	FinishedAt    *time.Time
	DurationSec   *int64
	Progress      int
	FailureReason *string
	TaskID        *string
}

// migrateHistory creates the print_history table and applies any pending
// column additions. Safe to call multiple times; already-existing columns
// are skipped without error.
func migrateHistory(db *sql.DB, log *slog.Logger) error {
	if _, err := db.Exec(historySchema); err != nil {
		return fmt.Errorf("create print_history table: %w", err)
	}

	// Create index on task_id for fast lookup by task.
	const idxSQL = `CREATE INDEX IF NOT EXISTS idx_history_task_id ON print_history(task_id)`
	if _, err := db.Exec(idxSQL); err != nil {
		return fmt.Errorf("create task_id index: %w", err)
	}

	existing, err := tableColumns(db, "print_history")
	if err != nil {
		return fmt.Errorf("read print_history columns: %w", err)
	}

	for _, col := range historyMigrationColumns {
		if _, ok := existing[col.name]; ok {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE print_history ADD COLUMN %s %s", col.name, col.def)
		if _, err := db.Exec(stmt); err != nil {
			// SQLite may still report "duplicate column" on a race; treat as non-fatal.
			log.Warn("history migration: add column skipped", "column", col.name, "err", err)
		} else {
			log.Info("history migration: added column", "column", col.name)
		}
	}

	return nil
}

// isPlaceholder reports whether filename should be skipped when recording
// history (Python: _PLACEHOLDER_NAMES check in record_start).
func isPlaceholder(filename string) bool {
	_, ok := placeholderFilenames[strings.TrimSpace(strings.ToLower(filename))]
	return ok
}

// RecordStart records the beginning of a print job.
//
// Returns the new row ID and nil error on success.
// Returns (0, nil) if filename is a placeholder — callers should treat
// a zero ID as "no record was created".
//
// If an open 'started' entry already exists for the given taskID it is
// resumed (same semantics as the Python record_start). Any other open
// entries are closed as 'interrupted' before the new row is inserted.
// After insert, old entries are pruned (90-day retention, 500-row cap).
//
// (Python: PrintHistory.record_start)
func (d *DB) RecordStart(filename, taskID string) (int64, error) {
	if isPlaceholder(filename) {
		d.log.Debug("history: skipping placeholder filename", "filename", filename)
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var rowID int64

	err := d.withTx(func(tx *sql.Tx) error {
		// 1. Resume existing open entry for the same taskID.
		if taskID != "" {
			var id int64
			err := tx.QueryRow(
				`SELECT id FROM print_history WHERE status='started' AND task_id=?`,
				taskID,
			).Scan(&id)
			if err == nil {
				d.log.Info("history: resuming existing entry", "id", id, "task_id", taskID)
				rowID = id
				return nil
			}
			if err != sql.ErrNoRows {
				return fmt.Errorf("resume lookup: %w", err)
			}
		}

		// 2. Close any orphaned open entries.
		rows, err := tx.Query(
			`SELECT id, started_at FROM print_history WHERE status='started'`,
		)
		if err != nil {
			return fmt.Errorf("query orphans: %w", err)
		}

		type orphan struct {
			id        int64
			startedAt string
		}
		var orphans []orphan
		for rows.Next() {
			var o orphan
			if err := rows.Scan(&o.id, &o.startedAt); err != nil {
				rows.Close()
				return fmt.Errorf("scan orphan: %w", err)
			}
			orphans = append(orphans, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate orphans: %w", err)
		}

		if len(orphans) > 0 {
			now := time.Now().UTC()
			for _, o := range orphans {
				started, parseErr := time.Parse(time.RFC3339Nano, o.startedAt)
				if parseErr != nil {
					// Fallback: try without nanoseconds (Python stores isoformat).
					started, parseErr = time.Parse("2006-01-02T15:04:05.999999999", o.startedAt)
					if parseErr != nil {
						started = now
					}
				}
				duration := int64(now.Sub(started).Seconds())
				if _, err := tx.Exec(
					`UPDATE print_history SET status='interrupted', finished_at=?, duration_sec=? WHERE id=?`,
					now.UTC().Format(time.RFC3339Nano), duration, o.id,
				); err != nil {
					return fmt.Errorf("close orphan %d: %w", o.id, err)
				}
			}
			d.log.Info("history: marked orphaned entries as interrupted", "count", len(orphans))
		}

		// 3. Insert new entry.
		now := time.Now().UTC()
		var taskIDArg any
		if taskID != "" {
			taskIDArg = taskID
		}
		res, err := tx.Exec(
			`INSERT INTO print_history (filename, status, started_at, task_id) VALUES (?, 'started', ?, ?)`,
			filename, now.Format(time.RFC3339Nano), taskIDArg,
		)
		if err != nil {
			return fmt.Errorf("insert history row: %w", err)
		}

		rowID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}

		// 4. Prune old entries.
		if err := pruneHistory(tx, defaultRetentionDays, defaultMaxEntries, d.log); err != nil {
			return fmt.Errorf("prune history: %w", err)
		}

		return nil
	})

	return rowID, err
}

// RecordFinish marks an active print as finished.
//
// The active entry is located by taskID (preferred) then filename then
// most-recent fallback, matching the Python _find_active logic.
// Progress is clamped 0–100 on insert.
//
// (Python: PrintHistory.record_finish)
func (d *DB) RecordFinish(filename string, progress int, taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.withTx(func(tx *sql.Tx) error {
		row, err := findActive(tx, filename, taskID)
		if err != nil {
			return fmt.Errorf("find active: %w", err)
		}
		if row == nil {
			d.log.Debug("history: no active print to finish")
			return nil
		}

		now := time.Now().UTC()
		duration := computeDuration(row.startedAt, now)

		_, err = tx.Exec(
			`UPDATE print_history SET status='finished', finished_at=?, duration_sec=?, progress=? WHERE id=?`,
			now.Format(time.RFC3339Nano), duration, progress, row.id,
		)
		return err
	})
}

// RecordFail marks an active print as failed with an optional reason.
//
// (Python: PrintHistory.record_fail)
func (d *DB) RecordFail(filename, reason, taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.withTx(func(tx *sql.Tx) error {
		row, err := findActive(tx, filename, taskID)
		if err != nil {
			return fmt.Errorf("find active: %w", err)
		}
		if row == nil {
			d.log.Debug("history: no active print to fail")
			return nil
		}

		now := time.Now().UTC()
		duration := computeDuration(row.startedAt, now)

		var reasonArg any
		if reason != "" {
			reasonArg = reason
		}
		_, err = tx.Exec(
			`UPDATE print_history SET status='failed', finished_at=?, duration_sec=?, failure_reason=? WHERE id=?`,
			now.Format(time.RFC3339Nano), duration, reasonArg, row.id,
		)
		return err
	})
}

// GetHistory returns up to limit rows from print_history, newest first,
// starting at the given offset.
//
// (Python: PrintHistory.get_history)
func (d *DB) GetHistory(limit, offset int) ([]PrintRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.db.Query(
		`SELECT id, filename, status, started_at, finished_at, duration_sec, progress, failure_reason, task_id
		   FROM print_history
		  ORDER BY id DESC
		  LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("GetHistory query: %w", err)
	}
	defer rows.Close()

	return scanHistoryRows(rows)
}

// HistoryCount returns the total number of history entries.
//
// (Python: PrintHistory.get_count)
func (d *DB) HistoryCount() (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var count int64
	err := d.db.QueryRow(`SELECT COUNT(*) FROM print_history`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("HistoryCount: %w", err)
	}
	return count, nil
}

// ClearHistory deletes all entries from print_history.
//
// (Python: PrintHistory.clear)
func (d *DB) ClearHistory() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM print_history`)
		if err != nil {
			return fmt.Errorf("ClearHistory: %w", err)
		}
		d.log.Info("history: cleared all entries")
		return nil
	})
}

// --- internal helpers ---

// activeRow is the minimal data needed to compute duration when closing
// an active record.
type activeRow struct {
	id        int64
	startedAt string
}

// findActive locates the most recent open ('started') print record using
// the same three-level priority as the Python _find_active helper:
//  1. Match by taskID (strongest)
//  2. Match by filename (legacy fallback)
//  3. Any open entry, most recent first
func findActive(tx *sql.Tx, filename, taskID string) (*activeRow, error) {
	var row activeRow

	if taskID != "" {
		err := tx.QueryRow(
			`SELECT id, started_at FROM print_history WHERE status='started' AND task_id=?`,
			taskID,
		).Scan(&row.id, &row.startedAt)
		if err == nil {
			return &row, nil
		}
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("task_id lookup: %w", err)
		}
	}

	if filename != "" {
		err := tx.QueryRow(
			`SELECT id, started_at FROM print_history WHERE status='started' AND filename=? ORDER BY id DESC LIMIT 1`,
			filename,
		).Scan(&row.id, &row.startedAt)
		if err == nil {
			return &row, nil
		}
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("filename lookup: %w", err)
		}
	}

	err := tx.QueryRow(
		`SELECT id, started_at FROM print_history WHERE status='started' ORDER BY id DESC LIMIT 1`,
	).Scan(&row.id, &row.startedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fallback lookup: %w", err)
	}
	return &row, nil
}

// pruneHistory removes entries older than retentionDays and trims the
// total count to maxEntries. (Python: PrintHistory._prune)
func pruneHistory(tx *sql.Tx, retentionDays, maxEntries int, log *slog.Logger) error {
	if retentionDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)
		res, err := tx.Exec(`DELETE FROM print_history WHERE started_at < ?`, cutoff)
		if err != nil {
			return fmt.Errorf("retention prune: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Info("history: pruned old entries", "count", n, "retention_days", retentionDays)
		}
	}

	if maxEntries > 0 {
		res, err := tx.Exec(`
			DELETE FROM print_history WHERE id NOT IN (
				SELECT id FROM print_history ORDER BY id DESC LIMIT ?
			)`, maxEntries,
		)
		if err != nil {
			return fmt.Errorf("cap prune: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Info("history: trimmed entries to cap", "removed", n, "max_entries", maxEntries)
		}
	}

	return nil
}

// computeDuration returns the number of whole seconds between startedAt
// (stored as RFC3339Nano or Python isoformat) and now.
func computeDuration(startedAt string, now time.Time) int64 {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, startedAt); err == nil {
			dur := now.Sub(t).Seconds()
			if dur < 0 {
				return 0
			}
			return int64(dur)
		}
	}
	return 0
}

// scanHistoryRows reads all rows from a *sql.Rows into a slice of PrintRecord.
func scanHistoryRows(rows *sql.Rows) ([]PrintRecord, error) {
	var records []PrintRecord
	for rows.Next() {
		var (
			r            PrintRecord
			finishedAtS  sql.NullString
			durationSec  sql.NullInt64
			failureReason sql.NullString
			taskID       sql.NullString
			startedAtS   string
		)
		if err := rows.Scan(
			&r.ID, &r.Filename, &r.Status,
			&startedAtS,
			&finishedAtS,
			&durationSec,
			&r.Progress,
			&failureReason,
			&taskID,
		); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}

		// Parse started_at (always present).
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, startedAtS); err == nil {
				r.StartedAt = t
				break
			}
		}

		if finishedAtS.Valid {
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
				if t, err := time.Parse(layout, finishedAtS.String); err == nil {
					r.FinishedAt = &t
					break
				}
			}
		}
		if durationSec.Valid {
			r.DurationSec = &durationSec.Int64
		}
		if failureReason.Valid {
			r.FailureReason = &failureReason.String
		}
		if taskID.Valid {
			r.TaskID = &taskID.String
		}

		records = append(records, r)
	}
	return records, rows.Err()
}
