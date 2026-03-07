package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseListen(t *testing.T) {
	host, port, ok := parseListen("0.0.0.0:4470")
	if !ok {
		t.Fatal("parseListen() = false, want true")
	}
	if host != "0.0.0.0" || port != 4470 {
		t.Fatalf("parseListen() = (%q, %d), want (%q, %d)", host, port, "0.0.0.0", 4470)
	}
}

func TestResolvedListenPortFromEnv(t *testing.T) {
	t.Setenv("ANKERCTL_PORT", "4488")
	t.Setenv("FLASK_PORT", "")
	if got := resolvedListenPort(""); got != 4488 {
		t.Fatalf("resolvedListenPort() = %d, want 4488", got)
	}
}

func TestResolvedListenHostPrefersCLI(t *testing.T) {
	t.Setenv("ANKERCTL_HOST", "127.0.0.1")
	if got := resolvedListenHost("0.0.0.0:4470"); got != "0.0.0.0" {
		t.Fatalf("resolvedListenHost() = %q, want 0.0.0.0", got)
	}
}

func TestRedactEmail(t *testing.T) {
	if got := redactEmail("daniel@example.com"); got != "...l@...m" {
		t.Fatalf("redactEmail() = %q", got)
	}
}

func TestRedactValue(t *testing.T) {
	if got := redactValue("SN123456789", 0, 5); got != "******56789" {
		t.Fatalf("redactValue() = %q", got)
	}
}

func TestShortRedaction(t *testing.T) {
	if got := shortRedaction("SN123456789", 4); got != "...6789" {
		t.Fatalf("shortRedaction() = %q", got)
	}
}

func TestHasAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ANKERCTL_API_KEY", "test-api-key-123456")
	if !hasAPIKey(nil) {
		t.Fatal("hasAPIKey(nil) = false, want true")
	}
}

func TestBannerAccessLinesFixedHost(t *testing.T) {
	got := bannerAccessLines("127.0.0.1", 4470)
	if len(got) != 1 || got[0] != "local:   http://127.0.0.1:4470/" {
		t.Fatalf("bannerAccessLines() = %#v", got)
	}
}

func TestBannerAccessLinesIPv4Wildcard(t *testing.T) {
	got := bannerAccessLines("0.0.0.0", 4471)
	if len(got) != 2 {
		t.Fatalf("bannerAccessLines() len = %d, want 2", len(got))
	}
	if got[0] != "local:   http://127.0.0.1:4471/" {
		t.Fatalf("bannerAccessLines()[0] = %q", got[0])
	}
	if got[1] != "exposed: all IPv4 interfaces" {
		t.Fatalf("bannerAccessLines()[1] = %q", got[1])
	}
}

func TestBannerAccessLinesIPv6Wildcard(t *testing.T) {
	got := bannerAccessLines("::", 4471)
	if len(got) != 3 {
		t.Fatalf("bannerAccessLines() len = %d, want 3", len(got))
	}
	if got[2] != "exposed: all IPv6 interfaces" {
		t.Fatalf("bannerAccessLines()[2] = %q", got[2])
	}
}

func TestEmitStartupBannerCompactOutput(t *testing.T) {
	var buf bytes.Buffer
	emitStartupBanner(&buf, startupBanner{
		ConfigDir:    "/tmp/cfg",
		DBPath:       "/tmp/cfg/ankerctl.db",
		DevMode:      true,
		PrinterIndex: 0,
		Host:         "0.0.0.0",
		Port:         4471,
		APIKeySet:    false,
	})
	out := buf.String()
	for _, want := range []string{
		"The solution for the",
		"bind: 0.0.0.0:4471",
		"local:   http://127.0.0.1:4471/",
		"exposed: all IPv4 interfaces",
		"---- server ----",
		"---- runtime log ----",
		"state: not configured",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q in output:\n%s", want, out)
		}
	}
}

func TestResolvedListenDefaults(t *testing.T) {
	t.Setenv("ANKERCTL_HOST", "")
	t.Setenv("FLASK_HOST", "")
	t.Setenv("ANKERCTL_PORT", "")
	t.Setenv("FLASK_PORT", "")
	if got := resolvedListenHost(""); got == "" {
		t.Fatal("resolvedListenHost() returned empty string")
	}
	if got := resolvedListenPort(""); got <= 0 {
		t.Fatalf("resolvedListenPort() = %d, want > 0", got)
	}
}
