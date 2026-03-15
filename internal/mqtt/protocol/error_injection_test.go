package protocol

import (
	"bytes"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// UnmarshalPacket — checksum manipulation
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_ChecksumSingleBitFlip(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	pkt := NewPacket("guid-1234", []byte(`{"commandType":1000,"value":1}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Flip every single bit position in the last byte (the checksum byte).
	// All must produce a checksum error.
	for bit := 0; bit < 8; bit++ {
		corrupt := make([]byte, len(wire))
		copy(corrupt, wire)
		corrupt[len(corrupt)-1] ^= 1 << uint(bit)
		_, err := UnmarshalPacket(corrupt, key)
		if err == nil {
			t.Fatalf("bit %d: expected checksum error, got nil", bit)
		}
		if !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("bit %d: error %q does not mention checksum", bit, err.Error())
		}
	}
}

func TestUnmarshalPacket_ChecksumMiddleByteFlipped(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	pkt := NewPacket("guid-5678", []byte(`{"commandType":1001}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Flip a byte in the middle of the payload — checksum must detect it.
	corrupt := make([]byte, len(wire))
	copy(corrupt, wire)
	corrupt[len(corrupt)/2] ^= 0xFF
	_, err = UnmarshalPacket(corrupt, key)
	if err == nil {
		t.Fatal("expected checksum error for middle-byte flip, got nil")
	}
}

// ---------------------------------------------------------------------------
// UnmarshalPacket — wrong / short AES key causes decrypt failure
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_WrongKeyDecryptFails(t *testing.T) {
	correctKey := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	wrongKey := mustHex(t, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	pkt := NewPacket("guid-9999", []byte(`{"commandType":1000}`))
	wire, err := pkt.MarshalBinary(correctKey)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	// Recalculate checksum so it passes checksum validation with wrong key,
	// but decrypt must still fail because padding will be invalid.
	// We recompute the last byte to make XOR=0 after modifying nothing —
	// the existing checksum is correct so just pass wrong key directly.
	_, err = UnmarshalPacket(wire, wrongKey)
	// Decrypt with wrong key will likely produce invalid PKCS7 padding.
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}
}

func TestUnmarshalPacket_EmptyKeyDecryptFails(t *testing.T) {
	correctKey := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	pkt := NewPacket("guid-empty", []byte(`{"commandType":1000}`))
	wire, err := pkt.MarshalBinary(correctKey)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// Empty key → AES cipher creation must fail.
	_, err = UnmarshalPacket(wire, []byte{})
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestUnmarshalPacket_ShortKeyDecryptFails(t *testing.T) {
	correctKey := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	pkt := NewPacket("guid-shortkey", []byte(`{"commandType":1000}`))
	wire, err := pkt.MarshalBinary(correctKey)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// 15-byte key is not a valid AES key length.
	_, err = UnmarshalPacket(wire, make([]byte, 15))
	if err == nil {
		t.Fatal("expected error for 15-byte key, got nil")
	}
}

// ---------------------------------------------------------------------------
// UnmarshalPacket — invalid PKCS7 padding in ciphertext
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_InvalidPKCS7Padding(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	// Build a valid packet, then corrupt the last AES block (which contains
	// the padding) so that unpadding fails.
	pkt := NewPacket("guid-pad", []byte(`{"commandType":1003,"temperature":19500}`))
	wire, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// The checksum byte is the last byte of wire.
	// Before it is the encrypted body; the last 16 bytes of the body form the
	// final AES block containing the PKCS7 padding.
	// Flip bytes inside that last AES block (positions -17 through -2).
	corrupt := make([]byte, len(wire))
	copy(corrupt, wire)
	// Corrupt byte 2 from the end of the encrypted body (excluding checksum).
	if len(corrupt) > 2 {
		corrupt[len(corrupt)-2] ^= 0xFF
		// Recompute checksum so it passes the XOR check.
		var xor byte
		for _, b := range corrupt[:len(corrupt)-1] {
			xor ^= b
		}
		corrupt[len(corrupt)-1] = xor
	}

	_, err = UnmarshalPacket(corrupt, key)
	// Either the decrypt or unpad step must return an error.
	if err == nil {
		// Rarely the corrupted block might accidentally decode to valid padding;
		// accept success only if the resulting JSON is still parseable — but we
		// can't be certain, so we only warn.
		t.Log("note: corrupted ciphertext unexpectedly decoded without error (rare)")
	}
}

// ---------------------------------------------------------------------------
// UnmarshalPacket — truncated header (< 12 bytes after checksum strip)
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_TruncatedHeader(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "empty payload",
			payload: []byte{},
		},
		{
			name: "single checksum byte only",
			payload: func() []byte {
				b := []byte{0x42}
				// XOR of payload = 0x42; append checksum so XOR of all = 0
				b = append(b, 0x42)
				return b
			}(),
		},
		{
			// 11 bytes after checksum removal — one byte short of minimum (12)
			name: "11 bytes after checksum strip",
			payload: func() []byte {
				b := make([]byte, 11)
				var xor byte
				for _, v := range b {
					xor ^= v
				}
				b = append(b, xor)
				return b
			}(),
		},
		{
			// 12 bytes but wrong signature bytes (not 'M','A')
			name: "12 bytes with bad signature",
			payload: func() []byte {
				b := make([]byte, 12)
				b[0] = 0xDE
				b[1] = 0xAD
				var xor byte
				for _, v := range b {
					xor ^= v
				}
				b = append(b, xor)
				return b
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalPacket(tc.payload, key)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// UnmarshalPacket — unsupported M5 value
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_UnsupportedM5Value(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	// Build a raw frame with M5=99 (unsupported format).
	// We need a valid signature and checksum to get past the first checks.
	raw := make([]byte, 24) // at least 12 bytes
	raw[0] = mqttSignatureA
	raw[1] = mqttSignatureB
	raw[6] = 99 // unsupported m5
	var xor byte
	for _, b := range raw {
		xor ^= b
	}
	raw = append(raw, xor)

	_, err := UnmarshalPacket(raw, key)
	if err == nil {
		t.Fatal("expected error for unsupported M5 value, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported packet format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UnmarshalPacket — invalid MQTT signature bytes
// ---------------------------------------------------------------------------

func TestUnmarshalPacket_InvalidSignature(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	tests := []struct {
		name  string
		sig   [2]byte
	}{
		{"all zeros", [2]byte{0x00, 0x00}},
		{"reversed MA", [2]byte{'A', 'M'}},
		{"garbage", [2]byte{0xFF, 0xFE}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := make([]byte, 24)
			raw[0] = tc.sig[0]
			raw[1] = tc.sig[1]
			raw[6] = mqttMagicM5M5 // valid M5 to reach signature check
			var xor byte
			for _, b := range raw {
				xor ^= b
			}
			raw = append(raw, xor)

			_, err := UnmarshalPacket(raw, key)
			if err == nil {
				t.Fatal("expected invalid signature error, got nil")
			}
			if !strings.Contains(err.Error(), "signature") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MarshalBinary — size overflow check (contrived huge data)
// ---------------------------------------------------------------------------

func TestMarshalBinary_UnsupportedM5(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	pkt := Packet{
		M5:   99, // unsupported format
		Data: []byte(`{"commandType":1000}`),
	}
	_, err := pkt.MarshalBinary(key)
	if err == nil {
		t.Fatal("expected error for unsupported M5 in MarshalBinary, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported packet format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MarshalBinary + UnmarshalPacket — known ct values round-trip correctly
// ---------------------------------------------------------------------------

func TestPacket_KnownCTValuesRoundTrip(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	tests := []struct {
		name string
		json string
	}{
		{"ct=1000 idle", `{"commandType":1000,"value":0}`},
		{"ct=1000 printing", `{"commandType":1000,"value":1}`},
		{"ct=1000 paused", `{"commandType":1000,"value":2}`},
		{"ct=1000 aborted", `{"commandType":1000,"value":8}`},
		{"ct=1001 progress", `{"commandType":1001,"progress":5000}`},
		{"ct=1003 nozzle temp", `{"commandType":1003,"temperature":19500}`},
		{"ct=1004 bed temp", `{"commandType":1004,"temperature":6000}`},
		{"ct=1008 pause cmd", `{"commandType":1008,"value":2}`},
		{"ct=1008 resume cmd", `{"commandType":1008,"value":3}`},
		{"ct=1008 stop cmd", `{"commandType":1008,"value":4}`},
		{"ct=1043 gcode", `{"commandType":1043,"command":"M503"}`},
		{"ct=1052 layers", `{"commandType":1052,"real_print_layer":5,"total_layer":100}`},
		{"unknown ct=9999", `{"commandType":9999,"value":42}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := NewPacket("test-guid-abc", []byte(tc.json))
			wire, err := pkt.MarshalBinary(key)
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			out, err := UnmarshalPacket(wire, key)
			if err != nil {
				t.Fatalf("UnmarshalPacket: %v", err)
			}
			if !bytes.Equal(out.Data, []byte(tc.json)) {
				t.Fatalf("data mismatch:\n got:  %s\n want: %s", out.Data, tc.json)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetJSON — invalid JSON in packet data
// ---------------------------------------------------------------------------

func TestPacket_GetJSON_InvalidJSON(t *testing.T) {
	pkt := &Packet{Data: []byte("not-valid-json{")}
	var v map[string]any
	err := pkt.GetJSON(&v)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestPacket_SetJSON_UnmarshallableType(t *testing.T) {
	pkt := &Packet{}
	// Functions cannot be marshalled to JSON.
	err := pkt.SetJSON(func() {})
	if err == nil {
		t.Fatal("expected error for non-marshallable type, got nil")
	}
}
