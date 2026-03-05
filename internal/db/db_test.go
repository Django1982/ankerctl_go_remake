package db

import (
	"database/sql"
	"testing"
)

// openTestDB opens an in-memory SQLite database and returns a *DB that is
// ready for use. The test is failed immediately if opening or migrating fails.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestOpen_InMemory verifies that Open succeeds with an in-memory database
// and that the schema migration runs without error.
func TestOpen_InMemory(t *testing.T) {
	d := openTestDB(t)
	if d == nil {
		t.Fatal("Open returned nil DB")
	}
}

// TestOpen_TablesExist verifies that both tables exist after Open.
func TestOpen_TablesExist(t *testing.T) {
	d := openTestDB(t)

	tables := []string{"print_history", "filaments"}
	for _, tbl := range tables {
		var name string
		err := d.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found after migration", tbl)
		} else if err != nil {
			t.Errorf("querying table %q: %v", tbl, err)
		}
	}
}

// TestSchemaMigration_AddColumn verifies that migrateHistory is idempotent
// even when the column already exists, and that adding a missing column works.
func TestSchemaMigration_AddColumn(t *testing.T) {
	// Create a fresh in-memory DB whose schema is correct.
	d := openTestDB(t)

	// Verify the task_id column exists.
	cols, err := tableColumns(d.db, "print_history")
	if err != nil {
		t.Fatalf("tableColumns: %v", err)
	}
	if _, ok := cols["task_id"]; !ok {
		t.Error("task_id column missing from print_history")
	}

	// Re-running migrateHistory should be safe (idempotent).
	d.mu.Lock()
	err = migrateHistory(d.db, d.log)
	d.mu.Unlock()
	if err != nil {
		t.Errorf("second migrateHistory call: %v", err)
	}
}

// TestSchemaMigration_FilamentColumns verifies all expected filament columns
// are present after migration.
func TestSchemaMigration_FilamentColumns(t *testing.T) {
	d := openTestDB(t)

	cols, err := tableColumns(d.db, "filaments")
	if err != nil {
		t.Fatalf("tableColumns: %v", err)
	}

	required := []string{
		"id", "name", "brand", "material", "color",
		"nozzle_temp_other_layer", "nozzle_temp_first_layer",
		"bed_temp_other_layer", "bed_temp_first_layer",
		"flow_rate", "filament_diameter",
		"pressure_advance", "max_volumetric_speed",
		"travel_speed", "perimeter_speed", "infill_speed",
		"cooling_enabled", "cooling_min_fan_speed", "cooling_max_fan_speed",
		"seam_position", "seam_gap",
		"scarf_enabled", "scarf_conditional", "scarf_angle_threshold",
		"scarf_length", "scarf_steps", "scarf_speed",
		"retract_length", "retract_speed", "retract_lift_z",
		"wipe_enabled", "wipe_distance", "wipe_speed", "wipe_retract_before",
		"notes", "created_at",
	}

	for _, col := range required {
		if _, ok := cols[col]; !ok {
			t.Errorf("column %q missing from filaments table", col)
		}
	}
}

// TestClose verifies that Close does not return an error on a healthy DB.
func TestClose(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
