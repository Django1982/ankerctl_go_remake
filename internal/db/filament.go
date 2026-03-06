package db

import (
	"database/sql"
	"fmt"
	"html"
	"log/slog"
	"strings"
)

// filamentSchema is the CREATE TABLE statement for the filaments table.
// Matches the Python reference exactly (filament.py _SCHEMA).
const filamentSchema = `
CREATE TABLE IF NOT EXISTS filaments (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    name                    TEXT NOT NULL,
    brand                   TEXT DEFAULT '',
    material                TEXT DEFAULT 'PLA',
    color                   TEXT DEFAULT '#FFFFFF',
    nozzle_temp_other_layer INTEGER DEFAULT 220,
    nozzle_temp_first_layer INTEGER DEFAULT 225,
    bed_temp_other_layer    INTEGER DEFAULT 60,
    bed_temp_first_layer    INTEGER DEFAULT 65,
    flow_rate               REAL    DEFAULT 1.0,
    filament_diameter       REAL    DEFAULT 1.75,
    pressure_advance        REAL    DEFAULT 0.0,
    max_volumetric_speed    REAL    DEFAULT 15.0,
    travel_speed            INTEGER DEFAULT 120,
    perimeter_speed         INTEGER DEFAULT 60,
    infill_speed            INTEGER DEFAULT 80,
    cooling_enabled         INTEGER DEFAULT 1,
    cooling_min_fan_speed   INTEGER DEFAULT 0,
    cooling_max_fan_speed   INTEGER DEFAULT 100,
    seam_position           TEXT    DEFAULT 'aligned',
    seam_gap                REAL    DEFAULT 0.0,
    scarf_enabled           INTEGER DEFAULT 0,
    scarf_conditional       INTEGER DEFAULT 0,
    scarf_angle_threshold   INTEGER DEFAULT 155,
    scarf_length            REAL    DEFAULT 20.0,
    scarf_steps             INTEGER DEFAULT 10,
    scarf_speed             INTEGER DEFAULT 100,
    retract_length          REAL    DEFAULT 0.8,
    retract_speed           INTEGER DEFAULT 45,
    retract_lift_z          REAL    DEFAULT 0.0,
    wipe_enabled            INTEGER DEFAULT 0,
    wipe_distance           REAL    DEFAULT 1.5,
    wipe_speed              INTEGER DEFAULT 40,
    wipe_retract_before     INTEGER DEFAULT 0,
    notes                   TEXT    DEFAULT '',
    created_at              TEXT    DEFAULT (datetime('now'))
);`

// filamentMigrationColumns are columns added after the initial release.
// The migration logic mirrors the Python _MIGRATION_COLUMNS list.
var filamentMigrationColumns = []struct {
	name string
	def  string
}{
	{"nozzle_temp_first_layer", "INTEGER DEFAULT 225"},
	{"bed_temp_other_layer", "INTEGER DEFAULT 60"},
	{"bed_temp_first_layer", "INTEGER DEFAULT 65"},
	{"flow_rate", "REAL DEFAULT 1.0"},
	{"cooling_min_fan_speed", "INTEGER DEFAULT 0"},
	{"cooling_max_fan_speed", "INTEGER DEFAULT 100"},
	{"seam_position", "TEXT DEFAULT 'aligned'"},
	{"seam_gap", "REAL DEFAULT 0.0"},
	{"scarf_enabled", "INTEGER DEFAULT 0"},
	{"scarf_conditional", "INTEGER DEFAULT 0"},
	{"scarf_angle_threshold", "INTEGER DEFAULT 155"},
	{"scarf_length", "REAL DEFAULT 20.0"},
	{"scarf_steps", "INTEGER DEFAULT 10"},
	{"scarf_speed", "INTEGER DEFAULT 100"},
	{"retract_length", "REAL DEFAULT 0.8"},
	{"retract_speed", "INTEGER DEFAULT 45"},
	{"retract_lift_z", "REAL DEFAULT 0.0"},
	{"wipe_enabled", "INTEGER DEFAULT 0"},
	{"wipe_distance", "REAL DEFAULT 1.5"},
	{"wipe_speed", "INTEGER DEFAULT 40"},
	{"wipe_retract_before", "INTEGER DEFAULT 0"},
}

// filamentTextFields are the text columns that must be HTML-escaped on
// insert and update to prevent stored XSS. (Python: _TEXT_FIELDS)
var filamentTextFields = map[string]struct{}{
	"name":          {},
	"brand":         {},
	"notes":         {},
	"material":      {},
	"color":         {},
	"seam_position": {},
}

// filamentWritableFields is the ordered list of fields accepted on
// create/update. The ID, created_at are managed by the DB.
// (Python: _FIELDS)
var filamentWritableFields = []string{
	"name", "brand", "material", "color",
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
	"notes",
}

// FilamentProfile mirrors a single row in the filaments table.
// All pointer fields are nullable in the DB (though in practice most have
// NOT NULL / DEFAULT constraints). Fields matching filamentTextFields
// are sanitized before storage.
type FilamentProfile struct {
	ID                   int64
	Name                 string
	Brand                string
	Material             string
	Color                string
	NozzleTempOtherLayer int
	NozzleTempFirstLayer int
	BedTempOtherLayer    int
	BedTempFirstLayer    int
	FlowRate             float64
	FilamentDiameter     float64
	PressureAdvance      float64
	MaxVolumetricSpeed   float64
	TravelSpeed          int
	PerimeterSpeed       int
	InfillSpeed          int
	CoolingEnabled       bool
	CoolingMinFanSpeed   int
	CoolingMaxFanSpeed   int
	SeamPosition         string
	SeamGap              float64
	ScarfEnabled         bool
	ScarfConditional     bool
	ScarfAngleThreshold  int
	ScarfLength          float64
	ScarfSteps           int
	ScarfSpeed           int
	RetractLength        float64
	RetractSpeed         int
	RetractLiftZ         float64
	WipeEnabled          bool
	WipeDistance         float64
	WipeSpeed            int
	WipeRetractBefore    bool
	Notes                string
	CreatedAt            string
}

// defaultFilaments are the four built-in profiles seeded when the table is
// empty. Values match the Python _DEFAULTS list exactly.
var defaultFilaments = []map[string]any{
	{
		"name": "Generic PLA", "brand": "Generic", "material": "PLA", "color": "#FFFFFF",
		"nozzle_temp_other_layer": 220, "nozzle_temp_first_layer": 225,
		"bed_temp_other_layer": 60, "bed_temp_first_layer": 65,
		"flow_rate": 1.0, "filament_diameter": 1.75,
		"pressure_advance": 0.04, "max_volumetric_speed": 15.0,
		"travel_speed": 150, "perimeter_speed": 60, "infill_speed": 80,
		"cooling_enabled": 1, "cooling_min_fan_speed": 20, "cooling_max_fan_speed": 100,
		"seam_position": "aligned", "seam_gap": 0.0,
		"scarf_enabled": 0, "scarf_conditional": 0, "scarf_angle_threshold": 155,
		"scarf_length": 20.0, "scarf_steps": 10, "scarf_speed": 100,
		"retract_length": 0.6, "retract_speed": 45, "retract_lift_z": 0.0,
		"wipe_enabled": 0, "wipe_distance": 1.5, "wipe_speed": 40, "wipe_retract_before": 0,
		"notes": "",
	},
	{
		"name": "Generic PETG", "brand": "Generic", "material": "PETG", "color": "#FFFFFF",
		"nozzle_temp_other_layer": 240, "nozzle_temp_first_layer": 245,
		"bed_temp_other_layer": 75, "bed_temp_first_layer": 80,
		"flow_rate": 1.0, "filament_diameter": 1.75,
		"pressure_advance": 0.07, "max_volumetric_speed": 10.0,
		"travel_speed": 130, "perimeter_speed": 50, "infill_speed": 70,
		"cooling_enabled": 1, "cooling_min_fan_speed": 30, "cooling_max_fan_speed": 80,
		"seam_position": "aligned", "seam_gap": 0.0,
		"scarf_enabled": 0, "scarf_conditional": 0, "scarf_angle_threshold": 155,
		"scarf_length": 20.0, "scarf_steps": 10, "scarf_speed": 100,
		"retract_length": 0.8, "retract_speed": 45, "retract_lift_z": 0.2,
		"wipe_enabled": 0, "wipe_distance": 1.5, "wipe_speed": 40, "wipe_retract_before": 0,
		"notes": "",
	},
	{
		"name": "Generic ABS", "brand": "Generic", "material": "ABS", "color": "#FFFFFF",
		"nozzle_temp_other_layer": 245, "nozzle_temp_first_layer": 250,
		"bed_temp_other_layer": 90, "bed_temp_first_layer": 95,
		"flow_rate": 1.0, "filament_diameter": 1.75,
		"pressure_advance": 0.05, "max_volumetric_speed": 12.0,
		"travel_speed": 150, "perimeter_speed": 60, "infill_speed": 80,
		"cooling_enabled": 0, "cooling_min_fan_speed": 0, "cooling_max_fan_speed": 15,
		"seam_position": "aligned", "seam_gap": 0.0,
		"scarf_enabled": 0, "scarf_conditional": 0, "scarf_angle_threshold": 155,
		"scarf_length": 20.0, "scarf_steps": 10, "scarf_speed": 100,
		"retract_length": 0.8, "retract_speed": 45, "retract_lift_z": 0.2,
		"wipe_enabled": 0, "wipe_distance": 1.5, "wipe_speed": 40, "wipe_retract_before": 0,
		"notes": "",
	},
	{
		"name": "Generic TPU", "brand": "Generic", "material": "TPU", "color": "#FFFFFF",
		"nozzle_temp_other_layer": 230, "nozzle_temp_first_layer": 235,
		"bed_temp_other_layer": 40, "bed_temp_first_layer": 45,
		"flow_rate": 1.0, "filament_diameter": 1.75,
		"pressure_advance": 0.1, "max_volumetric_speed": 5.0,
		"travel_speed": 100, "perimeter_speed": 30, "infill_speed": 40,
		"cooling_enabled": 1, "cooling_min_fan_speed": 30, "cooling_max_fan_speed": 60,
		"seam_position": "aligned", "seam_gap": 0.0,
		"scarf_enabled": 0, "scarf_conditional": 0, "scarf_angle_threshold": 155,
		"scarf_length": 20.0, "scarf_steps": 10, "scarf_speed": 100,
		"retract_length": 2.0, "retract_speed": 25, "retract_lift_z": 0.2,
		"wipe_enabled": 0, "wipe_distance": 1.5, "wipe_speed": 40, "wipe_retract_before": 0,
		"notes": "",
	},
}

// migrateFilaments creates the filaments table, applies column migrations,
// and seeds defaults if the table is empty.
func migrateFilaments(db *sql.DB, log *slog.Logger) error {
	if _, err := db.Exec(filamentSchema); err != nil {
		return fmt.Errorf("create filaments table: %w", err)
	}

	// Handle legacy column renames (SQLite 3.25+). Only attempted when the
	// old column actually exists — avoids spurious log output on fresh DBs.
	existingAtCreate, err := tableColumns(db, "filaments")
	if err != nil {
		return fmt.Errorf("read filaments columns (pre-rename): %w", err)
	}
	legacyRenames := [][2]string{
		{"nozzle_temp", "nozzle_temp_other_layer"},
		{"bed_temp", "bed_temp_other_layer"},
	}
	for _, pair := range legacyRenames {
		if _, ok := existingAtCreate[pair[0]]; !ok {
			continue // old column not present, nothing to rename
		}
		stmt := fmt.Sprintf("ALTER TABLE filaments RENAME COLUMN %s TO %s", pair[0], pair[1])
		if _, err := db.Exec(stmt); err != nil {
			log.Warn("filament migration: rename failed", "old", pair[0], "new", pair[1], "err", err)
		} else {
			log.Info("filament migration: renamed column", "old", pair[0], "new", pair[1])
		}
	}

	existing, err := tableColumns(db, "filaments")
	if err != nil {
		return fmt.Errorf("read filaments columns: %w", err)
	}

	for _, col := range filamentMigrationColumns {
		if _, ok := existing[col.name]; ok {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE filaments ADD COLUMN %s %s", col.name, col.def)
		if _, err := db.Exec(stmt); err != nil {
			log.Warn("filament migration: add column skipped", "column", col.name, "err", err)
		} else {
			log.Info("filament migration: added column", "column", col.name)
		}
	}

	// Seed defaults if the table is empty.
	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM filaments").Scan(&count); err != nil {
		return fmt.Errorf("count filaments: %w", err)
	}
	if count == 0 {
		if err := seedDefaultFilaments(db, log); err != nil {
			return fmt.Errorf("seed defaults: %w", err)
		}
	}

	return nil
}

// seedDefaultFilaments inserts the four built-in filament profiles.
func seedDefaultFilaments(db *sql.DB, log *slog.Logger) error {
	for _, profile := range defaultFilaments {
		cols := make([]string, 0, len(filamentWritableFields))
		vals := make([]any, 0, len(filamentWritableFields))
		for _, f := range filamentWritableFields {
			if v, ok := profile[f]; ok {
				cols = append(cols, f)
				vals = append(vals, v)
			}
		}
		placeholders := strings.Repeat("?,", len(cols))
		placeholders = placeholders[:len(placeholders)-1]
		stmt := fmt.Sprintf(
			"INSERT INTO filaments (%s) VALUES (%s)",
			strings.Join(cols, ", "),
			placeholders,
		)
		if _, err := db.Exec(stmt, vals...); err != nil {
			return fmt.Errorf("seed profile %q: %w", profile["name"], err)
		}
	}
	log.Info("filament store: seeded default profiles", "count", len(defaultFilaments))
	return nil
}

// sanitizeFilamentData returns a new map containing only the allowed
// writable fields, with text fields HTML-escaped to prevent stored XSS.
// (Python: the sanitize + field-filter logic inside create/update)
func sanitizeFilamentData(data map[string]any) map[string]any {
	allowed := make(map[string]struct{}, len(filamentWritableFields))
	for _, f := range filamentWritableFields {
		allowed[f] = struct{}{}
	}

	safe := make(map[string]any, len(data))
	for k, v := range data {
		if _, ok := allowed[k]; !ok {
			continue
		}
		if _, isText := filamentTextFields[k]; isText {
			if s, ok := v.(string); ok {
				v = html.EscapeString(s)
			}
		}
		safe[k] = v
	}
	return safe
}

// CreateFilament inserts a new filament profile and returns it.
//
// The "name" field is required. Text fields are HTML-escaped. Only fields
// listed in filamentWritableFields are accepted; unknown keys are ignored.
//
// (Python: FilamentStore.create)
func (d *DB) CreateFilament(data map[string]any) (*FilamentProfile, error) {
	safe := sanitizeFilamentData(data)
	if name, _ := safe["name"].(string); strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("CreateFilament: name is required")
	}

	cols := make([]string, 0, len(safe))
	vals := make([]any, 0, len(safe))
	// Preserve field order for determinism.
	for _, f := range filamentWritableFields {
		if v, ok := safe[f]; ok {
			cols = append(cols, f)
			vals = append(vals, v)
		}
	}
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = placeholders[:len(placeholders)-1]
	stmt := fmt.Sprintf(
		"INSERT INTO filaments (%s) VALUES (%s)",
		strings.Join(cols, ", "),
		placeholders,
	)

	d.mu.Lock()
	defer d.mu.Unlock()

	var newID int64
	err := d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(stmt, vals...)
		if err != nil {
			return fmt.Errorf("insert filament: %w", err)
		}
		newID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return nil, err
	}

	return d.getFilamentLocked(newID)
}

// GetFilament returns the filament profile with the given ID, or nil if
// not found.
//
// (Python: FilamentStore.get)
func (d *DB) GetFilament(id int64) (*FilamentProfile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getFilamentLocked(id)
}

// getFilamentLocked queries a single profile by ID. Caller must hold d.mu.
func (d *DB) getFilamentLocked(id int64) (*FilamentProfile, error) {
	row := d.db.QueryRow(`SELECT `+filamentSelectCols+` FROM filaments WHERE id = ?`, id)
	return scanFilamentRow(row)
}

// ListFilaments returns all filament profiles ordered by ID ascending.
//
// (Python: FilamentStore.list_all)
func (d *DB) ListFilaments() ([]FilamentProfile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.db.Query(`SELECT ` + filamentSelectCols + ` FROM filaments ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("ListFilaments query: %w", err)
	}
	defer rows.Close()

	var profiles []FilamentProfile
	for rows.Next() {
		p, err := scanFilamentRowCols(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, *p)
	}
	return profiles, rows.Err()
}

// UpdateFilament updates the fields in data for the profile with the given
// ID. Only fields listed in filamentWritableFields are accepted. Text
// fields are HTML-escaped. Returns the updated profile, or nil if not found.
//
// (Python: FilamentStore.update)
func (d *DB) UpdateFilament(id int64, data map[string]any) (*FilamentProfile, error) {
	safe := sanitizeFilamentData(data)
	if len(safe) == 0 {
		// Nothing to update — return the current profile.
		return d.GetFilament(id)
	}

	assignments := make([]string, 0, len(safe))
	vals := make([]any, 0, len(safe)+1)
	for _, f := range filamentWritableFields {
		if v, ok := safe[f]; ok {
			assignments = append(assignments, f+" = ?")
			vals = append(vals, v)
		}
	}
	vals = append(vals, id)

	stmt := fmt.Sprintf("UPDATE filaments SET %s WHERE id = ?", strings.Join(assignments, ", "))

	d.mu.Lock()
	defer d.mu.Unlock()

	err := d.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(stmt, vals...)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("UpdateFilament: %w", err)
	}

	return d.getFilamentLocked(id)
}

// DeleteFilament removes the profile with the given ID. Returns true if a
// row was deleted, false if no such profile existed.
//
// (Python: FilamentStore.delete)
func (d *DB) DeleteFilament(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var deleted bool
	err := d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM filaments WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = n > 0
		return nil
	})
	return deleted, err
}

// DuplicateFilament copies the profile with the given ID, appending
// " (copy)" to the name. Returns the new profile, or nil if the source
// profile does not exist.
//
// (Python: FilamentStore.duplicate)
func (d *DB) DuplicateFilament(id int64) (*FilamentProfile, error) {
	original, err := d.GetFilament(id)
	if err != nil {
		return nil, fmt.Errorf("DuplicateFilament: get original: %w", err)
	}
	if original == nil {
		return nil, nil
	}

	data := filamentProfileToMap(original)
	data["name"] = original.Name + " (copy)"
	delete(data, "id")
	delete(data, "created_at")

	return d.CreateFilament(data)
}

// filamentSelectCols is the column list used in SELECT statements. Must
// match the order expected by scanFilamentRow.
const filamentSelectCols = `id, name, brand, material, color,
    nozzle_temp_other_layer, nozzle_temp_first_layer,
    bed_temp_other_layer, bed_temp_first_layer,
    flow_rate, filament_diameter,
    pressure_advance, max_volumetric_speed,
    travel_speed, perimeter_speed, infill_speed,
    cooling_enabled, cooling_min_fan_speed, cooling_max_fan_speed,
    seam_position, seam_gap,
    scarf_enabled, scarf_conditional, scarf_angle_threshold,
    scarf_length, scarf_steps, scarf_speed,
    retract_length, retract_speed, retract_lift_z,
    wipe_enabled, wipe_distance, wipe_speed, wipe_retract_before,
    notes, created_at`

// rowScanner abstracts *sql.Row and *sql.Rows so the same scan logic works
// for both QueryRow and Query iteration.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanFilamentRow scans a single *sql.Row into a FilamentProfile.
// Returns nil, nil when the row was not found (sql.ErrNoRows).
func scanFilamentRow(row *sql.Row) (*FilamentProfile, error) {
	p, err := scanFilamentRowCols(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// scanFilamentRowCols scans any rowScanner into a FilamentProfile.
func scanFilamentRowCols(s rowScanner) (*FilamentProfile, error) {
	var p FilamentProfile
	var (
		coolingEnabled    int
		scarfEnabled      int
		scarfConditional  int
		wipeEnabled       int
		wipeRetractBefore int
	)
	err := s.Scan(
		&p.ID, &p.Name, &p.Brand, &p.Material, &p.Color,
		&p.NozzleTempOtherLayer, &p.NozzleTempFirstLayer,
		&p.BedTempOtherLayer, &p.BedTempFirstLayer,
		&p.FlowRate, &p.FilamentDiameter,
		&p.PressureAdvance, &p.MaxVolumetricSpeed,
		&p.TravelSpeed, &p.PerimeterSpeed, &p.InfillSpeed,
		&coolingEnabled, &p.CoolingMinFanSpeed, &p.CoolingMaxFanSpeed,
		&p.SeamPosition, &p.SeamGap,
		&scarfEnabled, &scarfConditional, &p.ScarfAngleThreshold,
		&p.ScarfLength, &p.ScarfSteps, &p.ScarfSpeed,
		&p.RetractLength, &p.RetractSpeed, &p.RetractLiftZ,
		&wipeEnabled, &p.WipeDistance, &p.WipeSpeed, &wipeRetractBefore,
		&p.Notes, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.CoolingEnabled = coolingEnabled != 0
	p.ScarfEnabled = scarfEnabled != 0
	p.ScarfConditional = scarfConditional != 0
	p.WipeEnabled = wipeEnabled != 0
	p.WipeRetractBefore = wipeRetractBefore != 0
	return &p, nil
}

// filamentProfileToMap converts a FilamentProfile back to map[string]any
// for use with DuplicateFilament.
func filamentProfileToMap(p *FilamentProfile) map[string]any {
	boolToInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return map[string]any{
		"id":                       p.ID,
		"name":                     p.Name,
		"brand":                    p.Brand,
		"material":                 p.Material,
		"color":                    p.Color,
		"nozzle_temp_other_layer":  p.NozzleTempOtherLayer,
		"nozzle_temp_first_layer":  p.NozzleTempFirstLayer,
		"bed_temp_other_layer":     p.BedTempOtherLayer,
		"bed_temp_first_layer":     p.BedTempFirstLayer,
		"flow_rate":                p.FlowRate,
		"filament_diameter":        p.FilamentDiameter,
		"pressure_advance":         p.PressureAdvance,
		"max_volumetric_speed":     p.MaxVolumetricSpeed,
		"travel_speed":             p.TravelSpeed,
		"perimeter_speed":          p.PerimeterSpeed,
		"infill_speed":             p.InfillSpeed,
		"cooling_enabled":          boolToInt(p.CoolingEnabled),
		"cooling_min_fan_speed":    p.CoolingMinFanSpeed,
		"cooling_max_fan_speed":    p.CoolingMaxFanSpeed,
		"seam_position":            p.SeamPosition,
		"seam_gap":                 p.SeamGap,
		"scarf_enabled":            boolToInt(p.ScarfEnabled),
		"scarf_conditional":        boolToInt(p.ScarfConditional),
		"scarf_angle_threshold":    p.ScarfAngleThreshold,
		"scarf_length":             p.ScarfLength,
		"scarf_steps":              p.ScarfSteps,
		"scarf_speed":              p.ScarfSpeed,
		"retract_length":           p.RetractLength,
		"retract_speed":            p.RetractSpeed,
		"retract_lift_z":           p.RetractLiftZ,
		"wipe_enabled":             boolToInt(p.WipeEnabled),
		"wipe_distance":            p.WipeDistance,
		"wipe_speed":               p.WipeSpeed,
		"wipe_retract_before":      boolToInt(p.WipeRetractBefore),
		"notes":                    p.Notes,
		"created_at":               p.CreatedAt,
	}
}
