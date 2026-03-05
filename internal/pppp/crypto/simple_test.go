package crypto

import (
	"bytes"
	"fmt"
	"testing"
)

func TestSimpleEncryptDecryptRoundtrip(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{0x42}},
		{"two bytes", []byte{0xDE, 0xAD}},
		{"sequential bytes", func() []byte {
			b := make([]byte, 64)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
		{"all zeros", bytes.Repeat([]byte{0x00}, 32)},
		{"all 0xFF", bytes.Repeat([]byte{0xFF}, 32)},
		{"binary payload", []byte{0x00, 0x01, 0x7F, 0x80, 0xFE, 0xFF}},
		{"seed as payload", ppppSimpleSeed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encrypted := SimpleEncrypt(tc.input)
			if len(encrypted) != len(tc.input) {
				t.Errorf("SimpleEncrypt(%d bytes) length = %d, want %d",
					len(tc.input), len(encrypted), len(tc.input))
			}

			decrypted := SimpleDecrypt(encrypted)
			if len(decrypted) != len(tc.input) {
				t.Errorf("SimpleDecrypt(%d bytes) length = %d, want %d",
					len(encrypted), len(decrypted), len(tc.input))
			}

			if !bytes.Equal(decrypted, tc.input) {
				t.Errorf("roundtrip mismatch:\n  got  = %x\n  want = %x", decrypted, tc.input)
			}
		})
	}
}

func TestSimpleDecryptEncryptRoundtrip(t *testing.T) {
	// Decrypt then encrypt should also be identity (since the operations are
	// symmetric in key schedule, only differing in feedback source).
	input := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}

	decrypted := SimpleDecrypt(input)
	reEncrypted := SimpleEncrypt(decrypted)

	if !bytes.Equal(reEncrypted, input) {
		t.Errorf("Decrypt→Encrypt roundtrip failed:\n  got  = %x\n  want = %x", reEncrypted, input)
	}
}

func TestSimpleEncryptEmptyInput(t *testing.T) {
	out := SimpleEncrypt([]byte{})
	if len(out) != 0 {
		t.Errorf("SimpleEncrypt(empty) = %x, want empty", out)
	}
}

func TestSimpleDecryptEmptyInput(t *testing.T) {
	out := SimpleDecrypt([]byte{})
	if len(out) != 0 {
		t.Errorf("SimpleDecrypt(empty) = %x, want empty", out)
	}
}

func TestSimpleEncryptSingleByte(t *testing.T) {
	// For a single byte, encrypt and decrypt must use lookup(hash, 0) as the XOR mask.
	// We verify consistency between encrypt and decrypt for a single byte.
	input := []byte{0x42}
	enc := SimpleEncrypt(input)
	if len(enc) != 1 {
		t.Fatalf("SimpleEncrypt(1 byte) length = %d, want 1", len(enc))
	}
	dec := SimpleDecrypt(enc)
	if !bytes.Equal(dec, input) {
		t.Errorf("Single byte roundtrip: got 0x%02x, want 0x%02x", dec[0], input[0])
	}
}

func TestSimpleEncryptNotIdentity(t *testing.T) {
	// Verify that SimpleEncrypt actually transforms the data.
	// The first byte is XORed with lookup(hash, 0); unless that lookup returns 0,
	// the output differs from input.
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	enc := SimpleEncrypt(input)

	// At least one byte should differ (the transform is non-trivial with the given seed).
	different := false
	for i := range input {
		if enc[i] != input[i] {
			different = true
			break
		}
	}
	if !different {
		t.Error("SimpleEncrypt appears to be identity — hash/lookup may not be working")
	}
}

func TestSimpleHashDeterministic(t *testing.T) {
	h1 := simpleHash(ppppSimpleSeed)
	h2 := simpleHash(ppppSimpleSeed)
	if h1 != h2 {
		t.Error("simpleHash is not deterministic")
	}
}

func TestSimpleHashReversal(t *testing.T) {
	// Python's hash[::-1] reverses [h0, h1, h2, h3] to [h3, h2, h1, h0].
	// Verify this by computing the hash and checking the reversal property.
	seed := []byte{0x10, 0x20, 0x30}
	// Manual accumulation (before reversal):
	var raw [4]int
	for _, b := range seed {
		v := int(b)
		raw[0] ^= v
		raw[1] += v / 3
		raw[2] -= v
		raw[3] += v
	}
	got := simpleHash(seed)

	// After reversal: got[0] = raw[3], got[1] = raw[2], got[2] = raw[1], got[3] = raw[0].
	if got[0] != raw[3] || got[1] != raw[2] || got[2] != raw[1] || got[3] != raw[0] {
		t.Errorf("simpleHash reversal incorrect: got %v, pre-reversal was %v", got, raw)
	}
}

func TestSimpleEncryptFeedbackDifference(t *testing.T) {
	// Encrypt uses OUTPUT feedback; decrypt uses INPUT (ciphertext) feedback.
	// Verify that encrypting the same two-byte input with different first bytes
	// produces different second bytes.
	enc1 := SimpleEncrypt([]byte{0x00, 0x00})
	enc2 := SimpleEncrypt([]byte{0xFF, 0x00})

	// First bytes differ (they come from the same XOR mask but different inputs).
	if enc1[0] == enc2[0] {
		// This would only happen if lookup(hash,0) XOR 0x00 == lookup(hash,0) XOR 0xFF,
		// which is impossible.
		t.Error("first encrypted bytes should differ for different inputs")
	}

	// Second bytes should differ because the feedback is the (different) first output byte.
	if enc1[1] == enc2[1] {
		t.Error("second encrypted bytes should differ due to output feedback from different first bytes")
	}
}

func TestLookupNegativeHashValues(t *testing.T) {
	// simpleHash can produce negative values in hash[1] (h[2] -= seed[i]).
	// Verify lookup handles negative hash indices correctly.
	// Use a seed that produces negative hash[1] (after reversal, negative is at index [1]).
	// seed with large values: hash[2] -= v accumulates negative values.
	seed := bytes.Repeat([]byte{0xFF}, 20) // heavy subtraction in slot 2
	hash := simpleHash(seed)

	// hash[1] (after reversal from slot 2) should be negative.
	// Just verify lookup doesn't panic and returns a valid byte.
	for b := byte(0); b <= byte(7); b++ {
		result := lookup(hash, b)
		// Result should be a valid entry from ppppSimpleShuffle (all are valid bytes).
		_ = result // no panic = success
	}
}

func TestSimpleEncryptDeterministic(t *testing.T) {
	input := []byte{0x10, 0x20, 0x30, 0x40}
	out1 := SimpleEncrypt(input)
	out2 := SimpleEncrypt(input)
	if !bytes.Equal(out1, out2) {
		t.Error("SimpleEncrypt is not deterministic")
	}
}

func TestSimpleEncryptKnownVectors(t *testing.T) {
	// Bit-exact testvectors computed from the Python reference implementation.
	// These must match EXACTLY — any deviation indicates a porting bug.
	// python: simple_encrypt_string(bytes.fromhex("...")).hex()
	tests := []struct {
		name  string
		input string
		want  string // hex
	}{
		{"foo", "foo", "262976"},
		{"hello", "hello", "28d2aa5b5e"},
		{"000102", "\x00\x01\x02", "4074e6"},
		{"ffffffff", "\xff\xff\xff\xff", "bf6920d1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := SimpleEncrypt([]byte(tc.input))
			got := fmt.Sprintf("%x", out)
			if got != tc.want {
				t.Errorf("SimpleEncrypt(%q) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}

func TestSimpleHashKnownValues(t *testing.T) {
	// Verified against Python: simple_hash(b"SSD@cs2-network.") reversed
	// Pre-reversal: [h0, h1, h2, h3]
	// Post-reversal (what Go returns): [h3, h2, h1, h0]
	// Python result: [1431, -1431, 470, 91]  (after reversal)
	got := simpleHash(ppppSimpleSeed)
	want := [4]int{1431, -1431, 470, 91}
	if got != want {
		t.Errorf("simpleHash(PPPP_SIMPLE_SEED) = %v, want %v", got, want)
	}
}
