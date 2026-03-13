package util

import (
	"net"
	"testing"
)

func TestIsValidPrinterIP(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{"nil", nil, false},
		{"unspecified", net.IPv4zero, false},
		{"loopback", net.ParseIP("127.0.0.1"), false},
		{"loopback-other", net.ParseIP("127.0.1.1"), false},
		{"broadcast", net.IPv4bcast, false},
		{"link-local", net.ParseIP("169.254.1.1"), false},
		{"valid-private", net.ParseIP("192.168.1.141"), true},
		{"valid-10", net.ParseIP("10.0.0.5"), true},
		{"valid-172", net.ParseIP("172.16.0.1"), true},
		{"valid-public", net.ParseIP("8.8.8.8"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPrinterIP(tt.ip); got != tt.want {
				t.Errorf("IsValidPrinterIP(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIsValidPrinterIPString(t *testing.T) {
	tests := []struct {
		name  string
		ipStr string
		want  bool
	}{
		{"empty", "", false},
		{"broadcast", "255.255.255.255", false},
		{"loopback", "127.0.0.1", false},
		{"unspecified", "0.0.0.0", false},
		{"valid", "192.168.1.141", true},
		{"link-local", "169.254.0.1", false},
		{"garbage", "notanip", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPrinterIPString(tt.ipStr); got != tt.want {
				t.Errorf("IsValidPrinterIPString(%q) = %v, want %v", tt.ipStr, got, tt.want)
			}
		})
	}
}
