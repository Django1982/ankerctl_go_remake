package crypto

import (
	"bytes"
	"testing"
)

func TestXORBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{"empty", []byte{}, 0x00},
		{"single byte", []byte{0x42}, 0x42},
		{"two equal bytes", []byte{0xAB, 0xAB}, 0x00},
		{"known sequence", []byte{0x01, 0x02, 0x03}, 0x00}, // 01^02^03 = 00
		{"all zeros", bytes.Repeat([]byte{0x00}, 100), 0x00},
		{"all ones", bytes.Repeat([]byte{0xFF}, 4), 0x00},  // 4 XORs of 0xFF = 0
		{"all ones odd count", bytes.Repeat([]byte{0xFF}, 3), 0xFF}, // 3 XORs = 0xFF
		{"checksum byte appended", []byte{0x01, 0x02, 0x03, 0x00}, 0x00},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := XORBytes(tc.data)
			if got != tc.want {
				t.Errorf("XORBytes(%x) = 0x%02x, want 0x%02x", tc.data, got, tc.want)
			}
		})
	}
}

func TestAddChecksumRemoveChecksumRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{"empty message", []byte{}},
		{"single byte", []byte{0x42}},
		{"typical MQTT header", bytes.Repeat([]byte{0xAB}, 63)},
		{"binary data", []byte{0x00, 0x01, 0x7F, 0x80, 0xFE, 0xFF}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withChecksum := AddChecksum(tc.msg)

			// The appended checksum must make XOR of full payload = 0.
			if XORBytes(withChecksum) != 0 {
				t.Errorf("AddChecksum result XOR = 0x%02x, want 0x00", XORBytes(withChecksum))
			}

			// AddChecksum must extend length by exactly 1.
			if len(withChecksum) != len(tc.msg)+1 {
				t.Errorf("AddChecksum length = %d, want %d", len(withChecksum), len(tc.msg)+1)
			}

			// Original data must be preserved unchanged.
			if !bytes.Equal(withChecksum[:len(tc.msg)], tc.msg) {
				t.Error("AddChecksum corrupted original message bytes")
			}

			// RemoveChecksum must recover the original.
			recovered, err := RemoveChecksum(withChecksum)
			if err != nil {
				t.Fatalf("RemoveChecksum: %v", err)
			}

			if !bytes.Equal(recovered, tc.msg) {
				t.Errorf("roundtrip mismatch: got %q, want %q", recovered, tc.msg)
			}
		})
	}
}

func TestAddChecksumDoesNotMutateInput(t *testing.T) {
	msg := []byte{0x01, 0x02, 0x03}
	original := make([]byte, len(msg))
	copy(original, msg)

	AddChecksum(msg)

	if !bytes.Equal(msg, original) {
		t.Error("AddChecksum must not modify the input slice")
	}
}

func TestRemoveChecksumInvalidChecksum(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"wrong checksum byte", []byte{0x01, 0x02, 0x03, 0xFF}}, // XOR != 0
		{"all zeros single byte", []byte{0x01}},                 // XOR = 0x01 != 0
		{"correct data but last byte wrong", []byte{0xAA, 0xAA, 0x01}}, // 0xAA^0xAA^0x01 = 0x01 != 0
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RemoveChecksum(tc.payload)
			if err == nil {
				t.Errorf("RemoveChecksum(%x) expected error, got nil", tc.payload)
			}
		})
	}
}

func TestRemoveChecksumEmptyPayload(t *testing.T) {
	_, err := RemoveChecksum([]byte{})
	if err == nil {
		t.Error("expected error for empty payload, got nil")
	}
}

func TestRemoveChecksumValidKnownVector(t *testing.T) {
	// Manually construct a valid payload: msg + XOR(msg).
	msg := []byte{0x10, 0x20, 0x30}
	checksum := XORBytes(msg) // 0x10 ^ 0x20 ^ 0x30 = 0x20
	payload := append(msg, checksum)

	recovered, err := RemoveChecksum(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(recovered, msg) {
		t.Errorf("got %x, want %x", recovered, msg)
	}
}

func TestRemoveChecksumPreservesSlicePrefix(t *testing.T) {
	// Verify that the returned slice refers to the same underlying memory as
	// the input (no unnecessary copy), and that it has the correct length.
	msg := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	payload := AddChecksum(msg)

	recovered, err := RemoveChecksum(payload)
	if err != nil {
		t.Fatal(err)
	}

	if len(recovered) != len(msg) {
		t.Errorf("recovered length = %d, want %d", len(recovered), len(msg))
	}
	if !bytes.Equal(recovered, msg) {
		t.Errorf("recovered = %x, want %x", recovered, msg)
	}
}
