package crypto

import (
	"bytes"
	"fmt"
	"testing"
)

func TestCurseOutputLength(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{0x00}},
		{"8 bytes", bytes.Repeat([]byte{0x42}, 8)},
		{"100 bytes", bytes.Repeat([]byte{0xAB}, 100)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Curse(tc.input)
			want := len(tc.input) + 4
			if len(out) != want {
				t.Errorf("Curse(%d bytes) length = %d, want %d", len(tc.input), len(out), want)
			}
		})
	}
}

func TestCurseDecurseRoundtrip(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{0x42}},
		{"binary data", []byte{0x00, 0x01, 0x7F, 0x80, 0xFE, 0xFF}},
		{"100 bytes sequential", func() []byte {
			b := make([]byte, 100)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
		{"all zeros", bytes.Repeat([]byte{0x00}, 32)},
		{"all 0xFF", bytes.Repeat([]byte{0xFF}, 32)},
		{"PPPP seed as payload", []byte(ppppSeed)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cursed := Curse(tc.input)
			recovered, err := Decurse(cursed)
			if err != nil {
				t.Fatalf("Decurse after Curse: %v", err)
			}
			if !bytes.Equal(recovered, tc.input) {
				t.Errorf("roundtrip mismatch:\n  got  = %x\n  want = %x", recovered, tc.input)
			}
		})
	}
}

func TestDecurseTrailerValidation(t *testing.T) {
	// Produce a valid cursed blob, then corrupt the last 4 bytes.
	input := []byte("test data for curse")
	cursed := Curse(input)

	// Corrupt each trailer byte individually.
	for i := 1; i <= 4; i++ {
		corrupted := make([]byte, len(cursed))
		copy(corrupted, cursed)
		corrupted[len(corrupted)-i] ^= 0xFF // flip all bits

		_, err := Decurse(corrupted)
		if err == nil {
			t.Errorf("Decurse with corrupted trailer byte -%d: expected error, got nil", i)
		}
	}
}

func TestDecurseTooShort(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0x01}},
		{"two bytes", []byte{0x01, 0x02}},
		{"three bytes", []byte{0x01, 0x02, 0x03}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decurse(tc.input)
			if err == nil {
				t.Errorf("Decurse(%d bytes): expected error for too-short input, got nil", len(tc.input))
			}
		})
	}
}

func TestCurseNotIdentity(t *testing.T) {
	// Curse must not be the identity function (i.e., it must actually transform
	// the data).
	input := []byte{0x01, 0x02, 0x03, 0x04}
	cursed := Curse(input)

	// The first 4 bytes of cursed must differ from input (with overwhelming
	// probability for any non-trivial key schedule).
	if bytes.Equal(cursed[:len(input)], input) {
		t.Error("Curse appears to be identity — key schedule may not be working")
	}
}

func TestDecursePureTrailer(t *testing.T) {
	// Curse of empty input should produce exactly 4 bytes, all of which
	// Decurse must decode as 0x43 (the trailer constant).
	cursedEmpty := Curse([]byte{})
	if len(cursedEmpty) != 4 {
		t.Fatalf("Curse(empty) length = %d, want 4", len(cursedEmpty))
	}

	// Decurse of those 4 bytes should succeed and return empty.
	recovered, err := Decurse(cursedEmpty)
	if err != nil {
		t.Fatalf("Decurse of cursed-empty: %v", err)
	}
	if len(recovered) != 0 {
		t.Errorf("Decurse of cursed-empty: got %x, want empty", recovered)
	}
}

func TestInternalCurseDecurseConsistency(t *testing.T) {
	// The internal curse and decurse functions must be exact inverses using the
	// same key and shuffle.
	input := []byte("hello, pppp world!")
	cursedOut := curse(input, ppppSeed, ppppShuffle)
	decursedOut := decurse(cursedOut, ppppSeed, ppppShuffle)

	// decurse output includes the 4 trailer bytes — strip them for comparison.
	if len(decursedOut) < len(input)+4 {
		t.Fatalf("decurse output too short: %d < %d", len(decursedOut), len(input)+4)
	}
	payload := decursedOut[:len(decursedOut)-4]
	if !bytes.Equal(payload, input) {
		t.Errorf("internal roundtrip failed:\n  got  = %x\n  want = %x", payload, input)
	}

	// The last 4 bytes should all be 0x43.
	trailer := decursedOut[len(decursedOut)-4:]
	for i, b := range trailer {
		if b != 0x43 {
			t.Errorf("trailer byte %d = 0x%02x, want 0x43", i, b)
		}
	}
}

func TestCurseDeterministic(t *testing.T) {
	// Curse is deterministic — same input always gives same output.
	input := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	out1 := Curse(input)
	out2 := Curse(input)
	if !bytes.Equal(out1, out2) {
		t.Error("Curse is not deterministic")
	}
}

func TestCurseKnownVectors(t *testing.T) {
	// Bit-exact testvectors computed from the Python reference implementation.
	// These must match EXACTLY — any deviation indicates a porting bug.
	tests := []struct {
		name  string
		input string
		want  string // hex
	}{
		{"empty", "", "8f8386df"},
		{"00", "\x00", "cc7cf18b19"},
		{"ff", "\xff", "335f5dbb43"},
		{"hello", "hello", "a4d34c24295b3b3f19"},
		{"EUPRAKM", "EUPRAKM", "898922902bedb19bb5afd1"},
		{"8 zeros", "\x00\x00\x00\x00\x00\x00\x00\x00", "cc3f42fe30cc24bca36f0717"},
		{"8 0xFF", "\xff\xff\xff\xff\xff\xff\xff\xff", "33e39df357098d05cd75cbb2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Curse([]byte(tc.input))
			got := fmt.Sprintf("%x", out)
			if got != tc.want {
				t.Errorf("Curse(%q) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}
