package util

import (
	"bytes"
	"testing"
)

func TestHex(t *testing.T) {
	src := []byte{0x00, 0x11, 0x22, 0x33}
	hexStr := "00112233"

	if Enhex(src) != hexStr {
		t.Errorf("Enhex failed: expected %s, got %s", hexStr, Enhex(src))
	}

	unhexed, err := Unhex(hexStr)
	if err != nil {
		t.Errorf("Unhex failed: %v", err)
	}
	if !bytes.Equal(unhexed, src) {
		t.Errorf("Unhex failed: expected %v, got %v", src, unhexed)
	}
}

func TestBase64(t *testing.T) {
	src := []byte("hello")
	b64Str := "aGVsbG8="

	if B64e(src) != b64Str {
		t.Errorf("B64e failed: expected %s, got %s", b64Str, B64e(src))
	}

	decoded, err := B64d(b64Str)
	if err != nil {
		t.Errorf("B64d failed: %v", err)
	}
	if !bytes.Equal(decoded, src) {
		t.Errorf("B64d failed: expected %v, got %v", src, decoded)
	}
}

func TestPrettyMAC(t *testing.T) {
	src := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	expected := "aa:bb:cc:dd:ee:ff"

	if PrettyMAC(src) != expected {
		t.Errorf("PrettyMAC byte slice failed: expected %s, got %s", expected, PrettyMAC(src))
	}

	if PrettyMAC("aabbccddeeff") != expected {
		t.Errorf("PrettyMAC string failed: expected %s, got %s", expected, PrettyMAC("aabbccddeeff"))
	}
}
