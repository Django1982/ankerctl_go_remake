package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

type fakeSnapshotter struct {
	mu     sync.Mutex
	lights []bool
	err    error // if set, CaptureSnapshot returns this error
}

func (f *fakeSnapshotter) CaptureSnapshot(ctx context.Context, outputPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(outputPath, []byte("jpeg"), 0o644)
}

func (f *fakeSnapshotter) SetLight(ctx context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lights = append(f.lights, on)
	return nil
}

// newTestService creates a TimelapseService wired for testing with a fake
// ffmpeg runner that produces dummy mp4 files.
func newTestService(t *testing.T, dir string, snap *fakeSnapshotter) *TimelapseService {
	t.Helper()
	svc := NewTimelapseService(dir, snap)
	svc.runFFmpeg = func(ctx context.Context, args []string) error {
		out := args[len(args)-1]
		return os.WriteFile(out, []byte("mp4"), 0o644)
	}
	return svc
}

// enableService configures the service with sensible test defaults.
func enableService(svc *TimelapseService) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.enabled = true
	svc.interval = time.Second
	svc.maxVideos = 10
	svc.savePersist = true
	svc.lightMode = "off"
}

// countMP4s returns how many .mp4 files exist directly in dir.
func countMP4s(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			n++
		}
	}
	return n
}

// createFrames writes n dummy frame files into dir.
func createFrames(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("frame_%05d.jpg", i))
		if err := os.WriteFile(p, []byte("jpeg"), 0o644); err != nil {
			t.Fatalf("WriteFile frame %d: %v", i, err)
		}
	}
}

func TestTimelapseCaptureAndAssemble(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := NewTimelapseService(dir, snap)
	svc.runFFmpeg = func(ctx context.Context, args []string) error {
		out := args[len(args)-1]
		return os.WriteFile(out, []byte("mp4"), 0o644)
	}
	light := "auto"
	svc.Configure(model.TimelapseConfig{
		Enabled:        true,
		Interval:       1,
		MaxVideos:      10,
		SavePersistent: true,
		Light:          &light,
	}, "SN123")
	svc.mu.Lock()
	svc.enabled = true
	svc.interval = time.Second
	svc.maxVideos = 10
	svc.savePersist = true
	svc.lightMode = "auto"
	capDir := filepath.Join(dir, inProgressSubdir, "cube")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		svc.mu.Unlock()
		t.Fatalf("mkdir capture dir: %v", err)
	}
	svc.active = &captureState{
		Dir:       capDir,
		Filename:  "cube.gcode",
		FrameCtr:  0,
		StartedAt: time.Now(),
		LastShot:  time.Now().Add(-time.Second),
	}
	if err := svc.captureFrameLocked(context.Background()); err != nil {
		svc.mu.Unlock()
		t.Fatalf("capture frame 1: %v", err)
	}
	if err := svc.captureFrameLocked(context.Background()); err != nil {
		svc.mu.Unlock()
		t.Fatalf("capture frame 2: %v", err)
	}
	current := *svc.active
	svc.active = nil
	svc.finalizeCaptureLocked(context.Background(), &current, "")
	svc.mu.Unlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".mp4" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected assembled mp4 in %s", dir)
	}
}

func TestTimelapseRecoverOrphan(t *testing.T) {
	dir := t.TempDir()
	inProgress := filepath.Join(dir, inProgressSubdir, "job")
	if err := os.MkdirAll(inProgress, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(inProgress, "frame_00000.jpg"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(inProgress, "frame_00001.jpg"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(inProgress, ".meta"), []byte(`{"filename":"recover.gcode"}`), 0o644)
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(inProgress, old, old)

	svc := NewTimelapseService(dir, &fakeSnapshotter{})
	svc.runFFmpeg = func(ctx context.Context, args []string) error {
		out := args[len(args)-1]
		return os.WriteFile(out, []byte("mp4"), 0o644)
	}
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	time.Sleep(200 * time.Millisecond)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	foundRecovered := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".mp4" {
			foundRecovered = true
			break
		}
	}
	if !foundRecovered {
		t.Fatalf("expected recovered mp4 in %s", dir)
	}
}

// ---------------------------------------------------------------------------
// FFmpeg / Snapshot error handling
// ---------------------------------------------------------------------------

func TestTimelapseSnapshotError_NoFrameIncrement(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{err: errors.New("camera offline")}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	capDir := filepath.Join(dir, inProgressSubdir, "fail")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		svc.mu.Unlock()
		t.Fatalf("mkdir: %v", err)
	}
	svc.active = &captureState{
		Dir:       capDir,
		Filename:  "test.gcode",
		FrameCtr:  0,
		StartedAt: time.Now(),
		LastShot:  time.Now().Add(-2 * time.Second),
	}

	err := svc.captureFrameLocked(context.Background())
	frameCtr := svc.active.FrameCtr
	svc.mu.Unlock()

	if err == nil {
		t.Fatal("expected error from captureFrameLocked, got nil")
	}
	if !strings.Contains(err.Error(), "camera offline") {
		t.Fatalf("error should wrap original: %v", err)
	}
	if frameCtr != 0 {
		t.Fatalf("frame counter should stay at 0 on error, got %d", frameCtr)
	}
}

func TestTimelapseAssembleError_NoMP4Created(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	// Override ffmpeg to fail
	svc.runFFmpeg = func(ctx context.Context, args []string) error {
		return errors.New("ffmpeg: encoder not found")
	}
	enableService(svc)

	svc.mu.Lock()
	capDir := filepath.Join(dir, inProgressSubdir, "encodefail")
	createFrames(t, capDir, 5)
	cap := &captureState{Dir: capDir, Filename: "test.gcode", FrameCtr: 5, StartedAt: time.Now()}
	svc.finalizeCaptureLocked(context.Background(), cap, "")
	svc.mu.Unlock()

	if n := countMP4s(t, dir); n != 0 {
		t.Fatalf("expected 0 mp4 files after ffmpeg error, got %d", n)
	}
	// in_progress dir should still be cleaned up
	if _, err := os.Stat(capDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture dir should be cleaned up even on ffmpeg error")
	}
}

// ---------------------------------------------------------------------------
// Resume window: >60min pause should NOT resume
// ---------------------------------------------------------------------------

// requireFFmpeg skips tests that need ffmpegAvailable() to succeed.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if ffmpegAvailable() != nil {
		t.Skip("ffmpeg not available in test environment")
	}
}

func TestTimelapseResumeWindow_Expired(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()

	// Simulate a paused capture with an expired resume deadline.
	oldDir := filepath.Join(dir, inProgressSubdir, "old_job")
	createFrames(t, oldDir, 3)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       oldDir,
			Filename:  "old.gcode",
			FrameCtr:  3,
			StartedAt: time.Now().Add(-2 * time.Hour),
			LastShot:  time.Now().Add(-2 * time.Hour),
		},
		Deadline: time.Now().Add(-1 * time.Minute), // expired 1 minute ago
	}

	// Start a new capture with a different filename - should NOT resume
	svc.startCaptureLocked(context.Background(), "new.gcode")
	active := svc.active
	svc.mu.Unlock()

	if active == nil {
		t.Fatal("expected active capture after startCaptureLocked")
	}
	if active.Filename != "new.gcode" {
		t.Fatalf("expected new capture filename 'new.gcode', got %q", active.Filename)
	}
	if active.FrameCtr != 0 {
		t.Fatalf("expected frame counter 0 for new capture, got %d", active.FrameCtr)
	}
}

func TestTimelapseResumeWindow_WithinWindow(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()

	resumeDir := filepath.Join(dir, inProgressSubdir, "paused_job")
	createFrames(t, resumeDir, 5)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       resumeDir,
			Filename:  "same.gcode",
			FrameCtr:  5,
			StartedAt: time.Now().Add(-10 * time.Minute),
			LastShot:  time.Now().Add(-10 * time.Minute),
		},
		Deadline: time.Now().Add(50 * time.Minute), // still valid
	}

	// Same filename should resume
	svc.startCaptureLocked(context.Background(), "same.gcode")
	active := svc.active
	resume := svc.resume
	svc.mu.Unlock()

	if active == nil {
		t.Fatal("expected active capture after resume")
	}
	if active.FrameCtr != 5 {
		t.Fatalf("expected resumed frame counter 5, got %d", active.FrameCtr)
	}
	if resume != nil {
		t.Fatal("resume state should be cleared after successful resume")
	}
}

func TestTimelapseResumeWindow_DifferentFilename(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()

	resumeDir := filepath.Join(dir, inProgressSubdir, "other_job")
	createFrames(t, resumeDir, 4)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       resumeDir,
			Filename:  "other.gcode",
			FrameCtr:  4,
			StartedAt: time.Now().Add(-5 * time.Minute),
			LastShot:  time.Now().Add(-5 * time.Minute),
		},
		Deadline: time.Now().Add(55 * time.Minute),
	}

	// Different filename — old resume dir should be cleaned up, new capture starts
	svc.startCaptureLocked(context.Background(), "different.gcode")
	active := svc.active
	resume := svc.resume
	svc.mu.Unlock()

	if active == nil {
		t.Fatal("expected active capture")
	}
	if active.FrameCtr != 0 {
		t.Fatalf("expected fresh capture with 0 frames, got %d", active.FrameCtr)
	}
	if resume != nil {
		t.Fatal("stale resume should be cleared")
	}
	// Old resume dir should be cleaned up
	if _, err := os.Stat(resumeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old resume dir should have been cleaned up")
	}
}

// TestTimelapseResumeWindow_Expired_NoFFmpeg tests the resume expiry logic
// without requiring ffmpeg by using tick() which checks the deadline.
func TestTimelapseResumeWindow_Expired_NoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	oldDir := filepath.Join(dir, inProgressSubdir, "expired_resume")
	createFrames(t, oldDir, 4)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       oldDir,
			Filename:  "expired.gcode",
			FrameCtr:  4,
			StartedAt: time.Now().Add(-70 * time.Minute),
			LastShot:  time.Now().Add(-70 * time.Minute),
		},
		Deadline: time.Now().Add(-1 * time.Minute), // expired
	}
	svc.mu.Unlock()

	// tick() checks resume deadline and finalizes if expired
	svc.tick(context.Background())

	svc.mu.Lock()
	resume := svc.resume
	svc.mu.Unlock()

	if resume != nil {
		t.Fatal("expired resume should be finalized by tick")
	}
	// With 4 frames + savePersist=true, an mp4 should exist
	if n := countMP4s(t, dir); n != 1 {
		t.Fatalf("expected 1 mp4 from expired resume, got %d", n)
	}
}

// TestTimelapseResumeWindow_Active_NoFFmpeg tests that a resume within the
// window is NOT finalized by tick, without requiring ffmpeg.
func TestTimelapseResumeWindow_Active_NoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	resumeDir := filepath.Join(dir, inProgressSubdir, "active_resume")
	createFrames(t, resumeDir, 3)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       resumeDir,
			Filename:  "active.gcode",
			FrameCtr:  3,
			StartedAt: time.Now().Add(-10 * time.Minute),
			LastShot:  time.Now().Add(-10 * time.Minute),
		},
		Deadline: time.Now().Add(50 * time.Minute), // still valid
	}
	svc.mu.Unlock()

	// tick() should NOT finalize because deadline has not passed
	svc.tick(context.Background())

	svc.mu.Lock()
	resume := svc.resume
	svc.mu.Unlock()

	if resume == nil {
		t.Fatal("resume within window should NOT be finalized by tick")
	}
	if n := countMP4s(t, dir); n != 0 {
		t.Fatalf("expected 0 mp4 (resume still active), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Tick: resume deadline expiry triggers finalize
// ---------------------------------------------------------------------------

func TestTimelapseTick_ResumeDeadlineExpiry(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	resumeDir := filepath.Join(dir, inProgressSubdir, "tick_expire")
	createFrames(t, resumeDir, 3)
	svc.resume = &resumeState{
		captureState: captureState{
			Dir:       resumeDir,
			Filename:  "tick.gcode",
			FrameCtr:  3,
			StartedAt: time.Now().Add(-70 * time.Minute),
		},
		Deadline: time.Now().Add(-1 * time.Second), // already expired
	}
	svc.mu.Unlock()

	// Tick should finalize the expired resume
	svc.tick(context.Background())

	svc.mu.Lock()
	resume := svc.resume
	svc.mu.Unlock()

	if resume != nil {
		t.Fatal("resume should be nil after tick finalizes expired resume")
	}
	// With 3 frames and savePersist=true, an mp4 should have been assembled
	if n := countMP4s(t, dir); n != 1 {
		t.Fatalf("expected 1 mp4 from finalized resume, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Orphan recovery: >24h ignored, <=24h recovered
// ---------------------------------------------------------------------------

func TestTimelapseOrphanRecovery_OlderThan24h_Ignored(t *testing.T) {
	dir := t.TempDir()
	inProg := filepath.Join(dir, inProgressSubdir)

	// Create orphan older than 24h
	oldDir := filepath.Join(inProg, "ancient_job")
	createFrames(t, oldDir, 5)
	_ = os.WriteFile(filepath.Join(oldDir, ".meta"), []byte(`{"filename":"ancient.gcode"}`), 0o644)
	ancient := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(oldDir, ancient, ancient)

	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	if err := svc.recoverOrphans(context.Background()); err != nil {
		t.Fatalf("recoverOrphans: %v", err)
	}

	// No mp4 should be created — orphan too old
	if n := countMP4s(t, dir); n != 0 {
		t.Fatalf("expected 0 mp4 for >24h orphan, got %d", n)
	}
	// Dir should have been removed
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ancient orphan dir should be removed")
	}
}

func TestTimelapseOrphanRecovery_Within24h_Recovered(t *testing.T) {
	dir := t.TempDir()
	inProg := filepath.Join(dir, inProgressSubdir)

	// Create orphan younger than 24h but older than resume window
	recentDir := filepath.Join(inProg, "recent_job")
	createFrames(t, recentDir, 4)
	_ = os.WriteFile(filepath.Join(recentDir, ".meta"), []byte(`{"filename":"recent.gcode"}`), 0o644)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(recentDir, twoHoursAgo, twoHoursAgo)

	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	svc.mu.Lock()
	svc.savePersist = true
	svc.mu.Unlock()

	if err := svc.recoverOrphans(context.Background()); err != nil {
		t.Fatalf("recoverOrphans: %v", err)
	}

	// Should produce a _recovered mp4
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_recovered.mp4") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected _recovered.mp4 for orphan within 24h")
	}
}

func TestTimelapseOrphanRecovery_WithinResumeWindow_NotAssembled(t *testing.T) {
	dir := t.TempDir()
	inProg := filepath.Join(dir, inProgressSubdir)

	// Create orphan younger than resume window (60min)
	freshDir := filepath.Join(inProg, "fresh_job")
	createFrames(t, freshDir, 3)
	_ = os.WriteFile(filepath.Join(freshDir, ".meta"), []byte(`{"filename":"fresh.gcode"}`), 0o644)
	fiveMinAgo := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(freshDir, fiveMinAgo, fiveMinAgo)

	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	svc.mu.Lock()
	svc.savePersist = true
	svc.mu.Unlock()

	if err := svc.recoverOrphans(context.Background()); err != nil {
		t.Fatalf("recoverOrphans: %v", err)
	}

	// Should NOT produce mp4 — should be placed into resume state
	if n := countMP4s(t, dir); n != 0 {
		t.Fatalf("expected 0 mp4 for within-resume-window orphan, got %d", n)
	}

	svc.mu.Lock()
	resume := svc.resume
	svc.mu.Unlock()

	if resume == nil {
		t.Fatal("orphan within resume window should be placed into resume state")
	}
	if resume.Filename != "fresh.gcode" {
		t.Fatalf("resume filename should be 'fresh.gcode', got %q", resume.Filename)
	}
	if time.Until(resume.Deadline) < 50*time.Minute {
		t.Fatalf("resume deadline should be ~55min from now, got %v", time.Until(resume.Deadline))
	}
}

// ---------------------------------------------------------------------------
// Max videos pruning
// ---------------------------------------------------------------------------

func TestTimelapsePruneVideos_DeletesOldest(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)

	svc.mu.Lock()
	svc.maxVideos = 3
	svc.mu.Unlock()

	// Create 5 fake mp4 files with staggered mod times
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("video_%d.mp4", i)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("mp4"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		mt := time.Now().Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, mt, mt)
	}

	svc.mu.Lock()
	svc.pruneVideosLocked()
	svc.mu.Unlock()

	remaining := countMP4s(t, dir)
	if remaining != 3 {
		t.Fatalf("expected 3 videos after pruning, got %d", remaining)
	}

	// The two oldest (video_0.mp4, video_1.mp4) should be gone
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("video_%d.mp4", i)
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s should have been pruned", name)
		}
	}
	// The three newest should remain
	for i := 2; i < 5; i++ {
		name := fmt.Sprintf("video_%d.mp4", i)
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s should still exist: %v", name, err)
		}
	}
}

func TestTimelapsePruneVideos_ZeroMaxVideos_NoPrune(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)

	svc.mu.Lock()
	svc.maxVideos = 0
	svc.mu.Unlock()

	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("v%d.mp4", i))
		_ = os.WriteFile(p, []byte("mp4"), 0o644)
	}

	svc.mu.Lock()
	svc.pruneVideosLocked()
	svc.mu.Unlock()

	if n := countMP4s(t, dir); n != 5 {
		t.Fatalf("maxVideos=0 should not prune, expected 5 got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation: WorkerRun exits cleanly
// ---------------------------------------------------------------------------

func TestTimelapseContextCancellation(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	waitForState(t, svc, StateRunning, 2*time.Second)

	// Cancel context — service should stop cleanly
	cancel()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("service did not stop within 3s after context cancellation")
		default:
			if svc.State() == StateStopped {
				return // success
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------------------
// Empty capturesDir: default path is resolved
// ---------------------------------------------------------------------------

func TestTimelapseEmptyCapturesDir_UsesDefault(t *testing.T) {
	snap := &fakeSnapshotter{}
	svc := NewTimelapseService("", snap)

	if svc.capturesDir == "" {
		t.Fatal("capturesDir should not remain empty after construction")
	}
	if !strings.Contains(svc.capturesDir, "ankerctl") {
		t.Fatalf("default capturesDir should contain 'ankerctl', got %q", svc.capturesDir)
	}
	if !strings.HasSuffix(svc.capturesDir, "captures") {
		t.Fatalf("default capturesDir should end with 'captures', got %q", svc.capturesDir)
	}
}

// ---------------------------------------------------------------------------
// finishCaptureLocked: non-final creates resume state
// ---------------------------------------------------------------------------

func TestTimelapseFinishCapture_NonFinal_CreatesResume(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	capDir := filepath.Join(dir, inProgressSubdir, "pause_test")
	createFrames(t, capDir, 3)
	svc.active = &captureState{
		Dir:       capDir,
		Filename:  "pause.gcode",
		FrameCtr:  3,
		StartedAt: time.Now(),
		LastShot:  time.Now(),
	}

	svc.finishCaptureLocked(context.Background(), false)
	active := svc.active
	resume := svc.resume
	svc.mu.Unlock()

	if active != nil {
		t.Fatal("active should be nil after non-final finish")
	}
	if resume == nil {
		t.Fatal("resume should be set after non-final finish")
	}
	if resume.Filename != "pause.gcode" {
		t.Fatalf("resume filename should be 'pause.gcode', got %q", resume.Filename)
	}
	// Deadline should be approximately resumeWindow from now
	remaining := time.Until(resume.Deadline)
	if remaining < 59*time.Minute || remaining > 61*time.Minute {
		t.Fatalf("resume deadline should be ~60min from now, got %v", remaining)
	}
}

// ---------------------------------------------------------------------------
// failCaptureLocked: produces _partial suffix
// ---------------------------------------------------------------------------

func TestTimelapseFailCapture_ProducesPartialMP4(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	capDir := filepath.Join(dir, inProgressSubdir, "fail_test")
	createFrames(t, capDir, 4)
	svc.active = &captureState{
		Dir:       capDir,
		Filename:  "fail.gcode",
		FrameCtr:  4,
		StartedAt: time.Now(),
		LastShot:  time.Now(),
	}

	svc.failCaptureLocked(context.Background())
	active := svc.active
	resume := svc.resume
	svc.mu.Unlock()

	if active != nil {
		t.Fatal("active should be nil after fail")
	}
	if resume != nil {
		t.Fatal("resume should be nil after fail (no retry)")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "_partial.mp4") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected _partial.mp4 after failCaptureLocked")
	}
}

// ---------------------------------------------------------------------------
// startCaptureLocked: rejects "unknown" filenames
// ---------------------------------------------------------------------------

func TestTimelapseStartCapture_RejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	tests := []struct {
		name     string
		filename string
	}{
		{"empty", ""},
		{"unknown", "unknown"},
		{"unknown_gcode", "unknown.gcode"},
		{"unknown_upper", "UNKNOWN"},
		{"unknown_gcode_upper", "UNKNOWN.GCODE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc.mu.Lock()
			svc.active = nil
			svc.startCaptureLocked(context.Background(), tc.filename)
			active := svc.active
			svc.mu.Unlock()

			if active != nil {
				t.Fatalf("filename %q should be rejected, but active was set", tc.filename)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Finalize with <2 frames produces no MP4
// ---------------------------------------------------------------------------

func TestTimelapseFinalize_TooFewFrames_NoMP4(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	svc.mu.Lock()
	capDir := filepath.Join(dir, inProgressSubdir, "short")
	createFrames(t, capDir, 1)
	cap := &captureState{Dir: capDir, Filename: "short.gcode", FrameCtr: 1, StartedAt: time.Now()}
	svc.finalizeCaptureLocked(context.Background(), cap, "")
	svc.mu.Unlock()

	if n := countMP4s(t, dir); n != 0 {
		t.Fatalf("expected 0 mp4 with only 1 frame, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Notify dispatches print_state events
// ---------------------------------------------------------------------------

func TestTimelapseNotify_PrintStateRouting(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapshotter{}
	svc := newTestService(t, dir, snap)
	enableService(svc)

	// state=1 (printing) should send start command
	svc.Notify(map[string]any{
		"event":    "print_state",
		"state":    1,
		"filename": "notify.gcode",
	})

	// Drain command channel
	select {
	case raw := <-svc.cmdCh:
		cmd, ok := raw.(timelapseStartCmd)
		if !ok {
			t.Fatalf("expected timelapseStartCmd, got %T", raw)
		}
		if cmd.Filename != "notify.gcode" {
			t.Fatalf("expected filename 'notify.gcode', got %q", cmd.Filename)
		}
	case <-time.After(time.Second):
		t.Fatal("expected start command on cmdCh")
	}

	// state=8 (aborted) should send fail command
	svc.Notify(map[string]any{
		"event": "print_state",
		"state": float64(8), // JSON numbers arrive as float64
	})

	select {
	case raw := <-svc.cmdCh:
		if _, ok := raw.(timelapseFailCmd); !ok {
			t.Fatalf("expected timelapseFailCmd, got %T", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("expected fail command on cmdCh")
	}

	// Non-print_state events should be ignored
	svc.Notify(map[string]any{"event": "temperature", "temp": 200})
	select {
	case raw := <-svc.cmdCh:
		t.Fatalf("unexpected command for non-print event: %T", raw)
	case <-time.After(100 * time.Millisecond):
		// expected — no command
	}
}
