package notifications

import (
	"fmt"
	"regexp"
)

const (
	EventPrintStarted  = "print_started"
	EventPrintFinished = "print_finished"
	EventPrintFailed   = "print_failed"
	EventPrintPaused   = "print_paused"
	EventPrintResumed  = "print_resumed"
	EventGCodeUploaded = "gcode_uploaded"
	EventPrintProgress = "print_progress"
)

var (
	placeholderPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

	eventTitles = map[string]string{
		EventPrintStarted:  "Print started",
		EventPrintFinished: "Print finished",
		EventPrintFailed:   "Print failed",
		EventPrintPaused:   "Print paused",
		EventPrintResumed:  "Print resumed",
		EventGCodeUploaded: "Upload complete",
		EventPrintProgress: "Print progress",
	}

	eventTypes = map[string]string{
		EventPrintStarted:  "info",
		EventPrintFinished: "success",
		EventPrintFailed:   "failure",
		EventPrintPaused:   "info",
		EventPrintResumed:  "info",
		EventGCodeUploaded: "success",
		EventPrintProgress: "info",
	}
)

// DefaultTemplateForEvent returns the built-in template for an event.
func DefaultTemplateForEvent(event string) string {
	switch event {
	case EventPrintStarted:
		return "Print started: {filename}"
	case EventPrintFinished:
		return "Print finished: {filename} ({duration})"
	case EventPrintFailed:
		return "Print failed: {filename} ({reason})"
	case EventPrintPaused:
		return "Print paused: {filename}"
	case EventPrintResumed:
		return "Print resumed: {filename}"
	case EventGCodeUploaded:
		return "Upload complete: {filename} ({size})"
	case EventPrintProgress:
		return "Progress: {percent}% - {filename}"
	default:
		return event
	}
}

// RenderTemplate replaces {name} placeholders with payload values.
// Missing keys are left unchanged, matching Python SafeDict behavior.
func RenderTemplate(template string, payload map[string]any) string {
	if template == "" {
		return ""
	}
	if payload == nil {
		return template
	}
	return placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		sub := placeholderPattern.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		v, ok := payload[sub[1]]
		if !ok {
			return match
		}
		return fmt.Sprintf("%v", v)
	})
}

// EventTitle returns the default title for an event.
func EventTitle(event string) string {
	if title, ok := eventTitles[event]; ok {
		return title
	}
	return "Ankerctl"
}

// EventType returns the Apprise type for an event.
func EventType(event string) string {
	if typ, ok := eventTypes[event]; ok {
		return typ
	}
	return "info"
}
