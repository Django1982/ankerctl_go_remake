package main

import "testing"

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
	if got := redactEmail("daniel@example.com"); got != "d*****@e**********" {
		t.Fatalf("redactEmail() = %q", got)
	}
}

func TestRedactValue(t *testing.T) {
	if got := redactValue("SN123456789", 0, 5); got != "******56789" {
		t.Fatalf("redactValue() = %q", got)
	}
}

func TestHasAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ANKERCTL_API_KEY", "test-api-key-123456")
	if !hasAPIKey(nil) {
		t.Fatal("hasAPIKey(nil) = false, want true")
	}
}

func TestBannerURLsFixedHost(t *testing.T) {
	got := bannerURLs("127.0.0.1", 4470)
	if len(got) != 1 || got[0] != "http://127.0.0.1:4470/" {
		t.Fatalf("bannerURLs() = %#v", got)
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
