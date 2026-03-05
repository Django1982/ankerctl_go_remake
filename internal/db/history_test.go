package db

import (
	"fmt"
	"testing"
	"time"
)

// TestRecordStart_Basic verifies that a valid filename is recorded and
// returns a non-zero ID.
func TestRecordStart_Basic(t *testing.T) {
	d := openTestDB(t)

	id, err := d.RecordStart("cube.gcode", "task-001")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero row ID")
	}
}

// TestRecordStart_PlaceholderFilter verifies that placeholder filenames are
// skipped and return id=0 without error.
func TestRecordStart_PlaceholderFilter(t *testing.T) {
	d := openTestDB(t)

	cases := []string{"unknown", "unknown.gcode", "", "  "}
	for _, name := range cases {
		id, err := d.RecordStart(name, "")
		if err != nil {
			t.Errorf("RecordStart(%q): unexpected error: %v", name, err)
		}
		if id != 0 {
			t.Errorf("RecordStart(%q): expected id=0 (placeholder), got %d", name, id)
		}
	}
}

// TestRecordStart_Resume verifies that a second call with the same task_id
// returns the original row ID (resume, not a new entry).
func TestRecordStart_Resume(t *testing.T) {
	d := openTestDB(t)

	id1, err := d.RecordStart("cube.gcode", "task-abc")
	if err != nil {
		t.Fatalf("first RecordStart: %v", err)
	}

	id2, err := d.RecordStart("cube.gcode", "task-abc")
	if err != nil {
		t.Fatalf("second RecordStart (resume): %v", err)
	}

	if id1 != id2 {
		t.Errorf("expected same row ID on resume: got %d and %d", id1, id2)
	}

	count, err := d.HistoryCount()
	if err != nil {
		t.Fatalf("HistoryCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 history row, got %d", count)
	}
}

// TestRecordStart_OrphanClose verifies that starting a new print with a
// different task_id marks the previous open entry as 'interrupted'.
func TestRecordStart_OrphanClose(t *testing.T) {
	d := openTestDB(t)

	_, err := d.RecordStart("first.gcode", "task-1")
	if err != nil {
		t.Fatalf("first RecordStart: %v", err)
	}

	_, err = d.RecordStart("second.gcode", "task-2")
	if err != nil {
		t.Fatalf("second RecordStart: %v", err)
	}

	rows, err := d.GetHistory(10, 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(rows))
	}

	// Newest entry (id DESC) should be 'started'.
	// The older one should be 'interrupted'.
	statuses := make(map[string]int)
	for _, r := range rows {
		statuses[r.Status]++
	}
	if statuses["started"] != 1 {
		t.Errorf("expected 1 'started', got %d", statuses["started"])
	}
	if statuses["interrupted"] != 1 {
		t.Errorf("expected 1 'interrupted', got %d", statuses["interrupted"])
	}
}

// TestRecordFinish verifies status, duration, and progress are set.
func TestRecordFinish(t *testing.T) {
	d := openTestDB(t)

	_, err := d.RecordStart("cube.gcode", "task-fin")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // ensure non-zero duration

	if err := d.RecordFinish("cube.gcode", 100, "task-fin"); err != nil {
		t.Fatalf("RecordFinish: %v", err)
	}

	rows, err := d.GetHistory(1, 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no history rows returned")
	}

	r := rows[0]
	if r.Status != "finished" {
		t.Errorf("expected status='finished', got %q", r.Status)
	}
	if r.Progress != 100 {
		t.Errorf("expected progress=100, got %d", r.Progress)
	}
	if r.DurationSec == nil {
		t.Error("expected non-nil duration_sec")
	}
	if r.FinishedAt == nil {
		t.Error("expected non-nil finished_at")
	}
}

// TestRecordFail verifies failure_reason and status are set.
func TestRecordFail(t *testing.T) {
	d := openTestDB(t)

	_, err := d.RecordStart("fail_job.gcode", "task-fail")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}

	if err := d.RecordFail("fail_job.gcode", "nozzle clog", "task-fail"); err != nil {
		t.Fatalf("RecordFail: %v", err)
	}

	rows, err := d.GetHistory(1, 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no history rows returned")
	}

	r := rows[0]
	if r.Status != "failed" {
		t.Errorf("expected status='failed', got %q", r.Status)
	}
	if r.FailureReason == nil || *r.FailureReason != "nozzle clog" {
		t.Errorf("expected failure_reason='nozzle clog', got %v", r.FailureReason)
	}
}

// TestClearHistory verifies that all rows are removed.
func TestClearHistory(t *testing.T) {
	d := openTestDB(t)

	for i := range 5 {
		_, err := d.RecordStart(fmt.Sprintf("job%d.gcode", i), "")
		if err != nil {
			t.Fatalf("RecordStart %d: %v", i, err)
		}
	}

	if err := d.ClearHistory(); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}

	count, err := d.HistoryCount()
	if err != nil {
		t.Fatalf("HistoryCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after clear, got %d", count)
	}
}

// TestGetHistory_Pagination verifies limit and offset.
func TestGetHistory_Pagination(t *testing.T) {
	d := openTestDB(t)

	// Insert 10 jobs (each new one closes the previous as 'interrupted').
	for i := range 10 {
		_, err := d.RecordStart(fmt.Sprintf("job%d.gcode", i), fmt.Sprintf("t%d", i))
		if err != nil {
			t.Fatalf("RecordStart %d: %v", i, err)
		}
	}

	page1, err := d.GetHistory(5, 0)
	if err != nil {
		t.Fatalf("GetHistory page 1: %v", err)
	}
	if len(page1) != 5 {
		t.Errorf("expected 5 rows, got %d", len(page1))
	}

	page2, err := d.GetHistory(5, 5)
	if err != nil {
		t.Fatalf("GetHistory page 2: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("expected 5 rows on page 2, got %d", len(page2))
	}

	// IDs should be descending across pages without overlap.
	if page1[len(page1)-1].ID <= page2[0].ID {
		t.Error("pages overlap or are not in descending ID order")
	}
}

// TestRetention_90Days verifies that the 90-day prune fires during insert.
func TestRetention_90Days(t *testing.T) {
	d := openTestDB(t)

	// Manually insert a row that is 91 days old.
	oldTime := time.Now().UTC().AddDate(0, 0, -91).Format(time.RFC3339Nano)
	_, err := d.db.Exec(
		`INSERT INTO print_history (filename, status, started_at) VALUES ('old.gcode', 'finished', ?)`,
		oldTime,
	)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}

	// Trigger prune by inserting a new record.
	_, err = d.RecordStart("new.gcode", "")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}

	rows, err := d.GetHistory(100, 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	for _, r := range rows {
		if r.Filename == "old.gcode" {
			t.Error("old.gcode should have been pruned by 90-day retention")
		}
	}
}

// TestRetention_500Cap verifies the 500-row max-entries cap.
func TestRetention_500Cap(t *testing.T) {
	d := openTestDB(t)

	// Insert 502 finished rows directly (bypassing RecordStart to avoid
	// orphan-close churn and to keep the test fast).
	ts := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	for i := range 502 {
		_, err := d.db.Exec(
			`INSERT INTO print_history (filename, status, started_at) VALUES (?, 'finished', ?)`,
			fmt.Sprintf("job%d.gcode", i), ts,
		)
		if err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	// Trigger pruning via RecordStart.
	_, err := d.RecordStart("trigger.gcode", "")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}

	count, err := d.HistoryCount()
	if err != nil {
		t.Fatalf("HistoryCount: %v", err)
	}
	// After pruning to 500 and inserting 1 new row the max should be <=500.
	if count > 500 {
		t.Errorf("expected <=500 rows after cap prune, got %d", count)
	}
}

// TestHistoryCount_Empty verifies count on a fresh DB.
func TestHistoryCount_Empty(t *testing.T) {
	d := openTestDB(t)

	count, err := d.HistoryCount()
	if err != nil {
		t.Fatalf("HistoryCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

// TestRecordFinish_NoActiveEntry verifies that RecordFinish is a no-op
// when there is no open entry (no error).
func TestRecordFinish_NoActiveEntry(t *testing.T) {
	d := openTestDB(t)

	if err := d.RecordFinish("cube.gcode", 100, "no-such-task"); err != nil {
		t.Errorf("RecordFinish with no active entry: %v", err)
	}
}

// TestRecordFail_NoActiveEntry verifies that RecordFail is a no-op
// when there is no open entry (no error).
func TestRecordFail_NoActiveEntry(t *testing.T) {
	d := openTestDB(t)

	if err := d.RecordFail("cube.gcode", "reason", "no-such-task"); err != nil {
		t.Errorf("RecordFail with no active entry: %v", err)
	}
}
