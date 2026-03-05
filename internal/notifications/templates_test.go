package notifications

import "testing"

func TestDefaultTemplateForEvent(t *testing.T) {
	if got := DefaultTemplateForEvent(EventPrintStarted); got != "Print started: {filename}" {
		t.Fatalf("started template = %q", got)
	}
	if got := DefaultTemplateForEvent(EventPrintFinished); got != "Print finished: {filename} ({duration})" {
		t.Fatalf("finished template = %q", got)
	}
	if got := DefaultTemplateForEvent(EventPrintFailed); got != "Print failed: {filename} ({reason})" {
		t.Fatalf("failed template = %q", got)
	}
	if got := DefaultTemplateForEvent(EventPrintPaused); got != "Print paused: {filename}" {
		t.Fatalf("paused template = %q", got)
	}
	if got := DefaultTemplateForEvent(EventPrintResumed); got != "Print resumed: {filename}" {
		t.Fatalf("resumed template = %q", got)
	}
}

func TestRenderTemplate_ReplacesKnownKeys(t *testing.T) {
	got := RenderTemplate("Print started: {filename}", map[string]any{"filename": "part.gcode"})
	if got != "Print started: part.gcode" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestRenderTemplate_KeepsUnknownKeys(t *testing.T) {
	got := RenderTemplate("Print failed: {filename} ({reason})", map[string]any{"filename": "part.gcode"})
	if got != "Print failed: part.gcode ({reason})" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestEventTitleAndType(t *testing.T) {
	if got := EventTitle(EventPrintFinished); got != "Print finished" {
		t.Fatalf("title = %q", got)
	}
	if got := EventType(EventPrintFailed); got != "failure" {
		t.Fatalf("type = %q", got)
	}
	if got := EventTitle("unknown"); got != "Ankerctl" {
		t.Fatalf("fallback title = %q", got)
	}
	if got := EventType("unknown"); got != "info" {
		t.Fatalf("fallback type = %q", got)
	}
}
