package util

import "testing"

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "00:00:00"},
		{-5, "00:00:00"},
		{59, "00:00:59"},
		{60, "00:01:00"},
		{3661, "01:01:01"},
		{86399, "23:59:59"},
		{86400, "24:00:00"},
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.in); got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{-1, "0 B"},
		{500, "500 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10240, "10 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
