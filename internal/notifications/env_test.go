package notifications

import (
	"testing"

	"github.com/django1982/ankerctl/internal/model"
)

func TestResolveAppriseEnv_OverridesEnabled(t *testing.T) {
	t.Setenv("APPRISE_ENABLED", "true")
	cfg := model.DefaultAppriseConfig()
	cfg.Enabled = false

	resolved := ResolveAppriseEnv(cfg)
	if !resolved.Enabled {
		t.Fatal("expected Enabled=true after env override")
	}
}

func TestResolveAppriseEnv_OverridesServerAndKey(t *testing.T) {
	t.Setenv("APPRISE_SERVER_URL", "https://env.example.com")
	t.Setenv("APPRISE_KEY", "env-key")
	t.Setenv("APPRISE_TAG", "env-tag")

	cfg := model.DefaultAppriseConfig()
	resolved := ResolveAppriseEnv(cfg)

	if resolved.ServerURL != "https://env.example.com" {
		t.Fatalf("ServerURL = %q", resolved.ServerURL)
	}
	if resolved.Key != "env-key" {
		t.Fatalf("Key = %q", resolved.Key)
	}
	if resolved.Tag != "env-tag" {
		t.Fatalf("Tag = %q", resolved.Tag)
	}
}

func TestResolveAppriseEnv_EventOverrides(t *testing.T) {
	t.Setenv("APPRISE_EVENT_PRINT_STARTED", "false")
	t.Setenv("APPRISE_EVENT_PRINT_PROGRESS", "false")

	cfg := model.DefaultAppriseConfig()
	resolved := ResolveAppriseEnv(cfg)

	if resolved.Events.PrintStarted {
		t.Fatal("expected PrintStarted=false after env override")
	}
	if resolved.Events.PrintProgress {
		t.Fatal("expected PrintProgress=false after env override")
	}
	// Unchanged events should keep their defaults.
	if !resolved.Events.PrintFinished {
		t.Fatal("expected PrintFinished=true (unchanged)")
	}
}

func TestResolveAppriseEnv_ProgressOverrides(t *testing.T) {
	t.Setenv("APPRISE_PROGRESS_INTERVAL", "10")
	t.Setenv("APPRISE_PROGRESS_INCLUDE_IMAGE", "true")
	t.Setenv("APPRISE_SNAPSHOT_QUALITY", "fhd")
	t.Setenv("APPRISE_SNAPSHOT_FALLBACK", "false")
	t.Setenv("APPRISE_SNAPSHOT_LIGHT", "true")
	t.Setenv("APPRISE_PROGRESS_MAX", "5")

	cfg := model.DefaultAppriseConfig()
	resolved := ResolveAppriseEnv(cfg)

	if resolved.Progress.IntervalPercent != 10 {
		t.Fatalf("IntervalPercent = %d", resolved.Progress.IntervalPercent)
	}
	if !resolved.Progress.IncludeImage {
		t.Fatal("expected IncludeImage=true")
	}
	if resolved.Progress.SnapshotQuality != "fhd" {
		t.Fatalf("SnapshotQuality = %q", resolved.Progress.SnapshotQuality)
	}
	if resolved.Progress.SnapshotFallback {
		t.Fatal("expected SnapshotFallback=false")
	}
	if !resolved.Progress.SnapshotLight {
		t.Fatal("expected SnapshotLight=true")
	}
	if resolved.Progress.MaxValue != 5 {
		t.Fatalf("MaxValue = %d", resolved.Progress.MaxValue)
	}
}

func TestResolveAppriseEnv_NoEnvUnchanged(t *testing.T) {
	cfg := model.DefaultAppriseConfig()
	cfg.Enabled = true
	cfg.ServerURL = "https://original.example.com"
	cfg.Key = "original-key"

	resolved := ResolveAppriseEnv(cfg)

	if resolved.ServerURL != "https://original.example.com" {
		t.Fatalf("ServerURL changed unexpectedly: %q", resolved.ServerURL)
	}
	if resolved.Key != "original-key" {
		t.Fatalf("Key changed unexpectedly: %q", resolved.Key)
	}
}
