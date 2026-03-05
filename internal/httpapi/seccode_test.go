package httpapi

import (
	"testing"
)

func TestGenCheckCodeV1_Table(t *testing.T) {
	tests := []struct {
		name     string
		baseCode string
		seed     string
		want     string // Uppercase hex (16 chars, str[0x10:0x20])
	}{
		{
			"Vector 1",
			"SN12345678",
			"0112345678",
			"1901778E9E3D31729F30A10BF36D94B1",
		},
		{
			"Vector 2 - Short",
			"ABC",
			"XYZ",
			"C210CB9ED86698A08268516768861C4B",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GenCheckCodeV1(tc.baseCode, tc.seed)
			if got != tc.want {
				t.Errorf("GenCheckCodeV1(%q, %q) = %q, want %q", tc.baseCode, tc.seed, got, tc.want)
			}
		})
	}
}

func TestCalcCheckCode_Table(t *testing.T) {
	tests := []struct {
		sn   string
		mac  string
		want string
	}{
		{"ABCDEF1234", "aabbccddeeff", "999cb80154ed63cef3e07f4543cd3a52"},
		{"AB", "aabb", ""}, // Too short
	}

	for _, tc := range tests {
		t.Run(tc.sn, func(t *testing.T) {
			got := CalcCheckCode(tc.sn, tc.mac)
			if got != tc.want {
				t.Errorf("CalcCheckCode(%q, %q) = %q, want %q", tc.sn, tc.mac, got, tc.want)
			}
		})
	}
}

func TestGenBaseCode_Table(t *testing.T) {
	tests := []struct {
		sn   string
		mac  string
		want string
	}{
		{"ABCDEF1234", "aabbccddeeff", "EF123458"},
	}

	for _, tc := range tests {
		t.Run(tc.sn, func(t *testing.T) {
			got := GenBaseCode(tc.sn, tc.mac)
			if got != tc.want {
				t.Errorf("GenBaseCode(%q, %q) = %q, want %q", tc.sn, tc.mac, got, tc.want)
			}
		})
	}
}
