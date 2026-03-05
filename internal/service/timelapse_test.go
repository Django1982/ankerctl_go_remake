package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

type fakeSnapshotter struct {
	lights []bool
}

func (f *fakeSnapshotter) CaptureSnapshot(ctx context.Context, outputPath string) error {
	return os.WriteFile(outputPath, []byte("jpeg"), 0o644)
}

func (f *fakeSnapshotter) SetLight(ctx context.Context, on bool) error {
	f.lights = append(f.lights, on)
	return nil
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
