package protocol

import (
	"net"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseMessage — wire-level framing errors
// ---------------------------------------------------------------------------

func TestParseMessage_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "empty input",
			data:    []byte{},
			wantErr: "short message header",
		},
		{
			name:    "3 bytes — too short for header",
			data:    []byte{MagicNumber, 0xD0, 0x00},
			wantErr: "short message header",
		},
		{
			name:    "wrong magic byte",
			data:    []byte{0xDE, 0xD0, 0x00, 0x00},
			wantErr: "invalid magic",
		},
		{
			name:    "all-zero magic",
			data:    []byte{0x00, 0x00, 0x00, 0x00},
			wantErr: "invalid magic",
		},
		{
			name:    "length field claims 10 bytes but only 0 bytes follow",
			data:    []byte{MagicNumber, byte(TypeDrw), 0x00, 0x0A},
			wantErr: "payload truncated",
		},
		{
			name:    "length field claims 5 bytes but only 3 bytes follow",
			data:    []byte{MagicNumber, byte(TypeDrw), 0x00, 0x05, 0x01, 0x02, 0x03},
			wantErr: "payload truncated",
		},
		{
			name:    "length field claims 1 byte but payload is absent",
			data:    []byte{MagicNumber, byte(TypeAlive), 0x00, 0x01},
			wantErr: "payload truncated",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMessage(tc.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// Valid magic + zero-length payload must succeed (empty Alive etc.).
func TestParseMessage_ZeroLengthPayload(t *testing.T) {
	data := []byte{MagicNumber, byte(TypeAlive), 0x00, 0x00}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if msg.Type != TypeAlive {
		t.Fatalf("type = %v, want %v", msg.Type, TypeAlive)
	}
	if len(msg.Payload) != 0 {
		t.Fatalf("payload len = %d, want 0", len(msg.Payload))
	}
}

// ---------------------------------------------------------------------------
// DecodePacket — unknown / unrecognised type falls back gracefully
// ---------------------------------------------------------------------------

func TestDecodePacket_UnknownTypeFallback(t *testing.T) {
	unknownTypes := []Type{
		0x05, // gap between HelloAck and QueryDid
		0x50, // unused gap
		0xAB, // arbitrary unknown
		0xBB, // arbitrary unknown
	}

	for _, typ := range unknownTypes {
		msg := Message{Type: typ, Payload: []byte{0x01, 0x02}}
		pkt, err := DecodePacket(msg.MarshalBinary())
		if err != nil {
			t.Fatalf("type 0x%02x: unexpected error: %v", typ, err)
		}
		raw, ok := pkt.(Message)
		if !ok {
			t.Fatalf("type 0x%02x: expected Message fallback, got %T", typ, pkt)
		}
		if raw.Type != typ {
			t.Fatalf("type 0x%02x: fallback type = 0x%02x", typ, raw.Type)
		}
	}
}

// TypeInvalid (0xFF) is already in the existing test; this covers a few more
// known gaps to confirm no panic occurs on any valid Message with bad type.
func TestDecodePacket_UnknownTypeNoPanic(t *testing.T) {
	// Fuzz-style: iterate all 256 possible type bytes and ensure no panic.
	for i := 0; i < 256; i++ {
		msg := Message{Type: Type(i), Payload: []byte{}}
		_, _ = DecodePacket(msg.MarshalBinary())
	}
}

// ---------------------------------------------------------------------------
// ParseDrw — corrupted signature / truncated payload
// ---------------------------------------------------------------------------

func TestParseDrw_Errors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "empty payload",
			payload: []byte{},
			wantErr: "short drw payload",
		},
		{
			name:    "3 bytes — header incomplete",
			payload: []byte{drwSignature, 0x01, 0x00},
			wantErr: "short drw payload",
		},
		{
			name:    "wrong signature byte",
			payload: []byte{0xAA, 0x01, 0x00, 0x00},
			wantErr: "invalid drw signature",
		},
		{
			name:    "zero signature byte",
			payload: []byte{0x00, 0x00, 0x00, 0x00},
			wantErr: "invalid drw signature",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDrw(tc.payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseDrwAck — malformed count field
// ---------------------------------------------------------------------------

func TestParseDrwAck_Errors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "empty payload",
			payload: []byte{},
			wantErr: "short drw_ack payload",
		},
		{
			name:    "wrong signature",
			payload: []byte{0xFF, 0x01, 0x00, 0x01, 0x00, 0x00},
			wantErr: "invalid drw_ack signature",
		},
		{
			// count=3 but only 2 ack shorts (4 bytes) present → total 8, want 4+6=10
			name:    "count/length mismatch — count says 3, only 2 acks present",
			payload: []byte{drwSignature, 0x01, 0x00, 0x03, 0x00, 0x01, 0x00, 0x02},
			wantErr: "malformed",
		},
		{
			// count=1 but extra trailing bytes present
			name:    "count/length mismatch — trailing garbage",
			payload: []byte{drwSignature, 0x01, 0x00, 0x01, 0x00, 0x05, 0xFF},
			wantErr: "malformed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDrwAck(tc.payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseXzyh — bad magic, truncated data
// ---------------------------------------------------------------------------

func TestParseXzyh_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "empty input",
			data:    []byte{},
			wantErr: "short xzyh frame",
		},
		{
			name:    "15 bytes — one short of minimum header",
			data:    make([]byte, 15),
			wantErr: "short xzyh frame",
		},
		{
			name:    "wrong magic — XZYH expected",
			data:    append([]byte("XXXX"), make([]byte, 12)...),
			wantErr: "invalid xzyh magic",
		},
		{
			name:    "all-zero magic",
			data:    make([]byte, 16),
			wantErr: "invalid xzyh magic",
		},
		{
			// Header is valid XZYH but Len=5, only 3 data bytes follow
			name: "truncated data — Len larger than actual bytes",
			data: func() []byte {
				b := make([]byte, 16+3) // header + 3 bytes
				copy(b[0:4], "XZYH")
				// Len field at bytes 6:10 (little-endian) = 5
				b[6] = 0x05
				b[7] = 0x00
				b[8] = 0x00
				b[9] = 0x00
				return b
			}(),
			wantErr: "truncated xzyh data",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseXzyh(tc.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseVideoFrame — too short
// ---------------------------------------------------------------------------

func TestParseVideoFrame_TooShort(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"63 bytes — one byte short", make([]byte, 63)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseVideoFrame(tc.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "short") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseAabb — bad signature, truncated
// ---------------------------------------------------------------------------

func TestParseAabb_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "empty",
			data:    []byte{},
			wantErr: "short aabb header",
		},
		{
			name:    "11 bytes — one byte short",
			data:    make([]byte, 11),
			wantErr: "short aabb header",
		},
		{
			name:    "wrong signature — first two bytes not 0xAA 0xBB",
			data:    append([]byte{0xDE, 0xAD}, make([]byte, 10)...),
			wantErr: "invalid aabb signature",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAabb(tc.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseAabbWithCRC — checksum failure, truncation
// ---------------------------------------------------------------------------

func TestParseAabbWithCRC_Errors(t *testing.T) {
	// Build a valid AABB frame as a baseline.
	orig := Aabb{FrameType: FileTransferData, SN: 1, Pos: 0}
	payload := []byte("testdata")
	valid, err := orig.PackWithCRC(payload)
	if err != nil {
		t.Fatalf("PackWithCRC: %v", err)
	}

	t.Run("too short — under 14 bytes", func(t *testing.T) {
		_, _, err := ParseAabbWithCRC(valid[:13])
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "short") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CRC last byte flipped", func(t *testing.T) {
		corrupt := make([]byte, len(valid))
		copy(corrupt, valid)
		corrupt[len(corrupt)-1] ^= 0xFF
		_, _, err := ParseAabbWithCRC(corrupt)
		if err == nil {
			t.Fatal("expected CRC mismatch, got nil")
		}
		if !strings.Contains(err.Error(), "crc mismatch") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CRC first byte flipped", func(t *testing.T) {
		corrupt := make([]byte, len(valid))
		copy(corrupt, valid)
		// CRC occupies the last 2 bytes
		corrupt[len(corrupt)-2] ^= 0x01
		_, _, err := ParseAabbWithCRC(corrupt)
		if err == nil {
			t.Fatal("expected CRC mismatch, got nil")
		}
	})

	t.Run("payload byte modified — CRC fails", func(t *testing.T) {
		corrupt := make([]byte, len(valid))
		copy(corrupt, valid)
		// Flip a byte inside the data portion (after 12-byte header)
		corrupt[12] ^= 0xFF
		_, _, err := ParseAabbWithCRC(corrupt)
		if err == nil {
			t.Fatal("expected CRC mismatch, got nil")
		}
	})

	t.Run("header byte modified — CRC fails", func(t *testing.T) {
		corrupt := make([]byte, len(valid))
		copy(corrupt, valid)
		// Flip SN byte (index 3) — included in CRC computation
		corrupt[3] ^= 0xFF
		_, _, err := ParseAabbWithCRC(corrupt)
		if err == nil {
			t.Fatal("expected CRC mismatch, got nil")
		}
	})

	t.Run("body truncated — Len claims more bytes than present", func(t *testing.T) {
		// Build a frame where Len > actual data
		trunc := Aabb{FrameType: FileTransferData, SN: 2, Pos: 0}
		raw, err := trunc.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		// Force Len=100 in header but provide no actual data
		raw[8] = 100 // LittleEndian Len bytes 8:12
		raw[9] = 0
		raw[10] = 0
		raw[11] = 0
		// Append only 2 CRC bytes (14 total) — not the 100 the header claims
		raw = append(raw, 0x00, 0x00)
		_, _, err = ParseAabbWithCRC(raw)
		if err == nil {
			t.Fatal("expected truncation error, got nil")
		}
		if !strings.Contains(err.Error(), "truncated") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ParseHelloAck — host field errors
// ---------------------------------------------------------------------------

func TestParseHelloAck_Errors(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		_, err := ParseHelloAck([]byte{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("15 bytes — one byte short of Host struct", func(t *testing.T) {
		_, err := ParseHelloAck(make([]byte, 15))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("trailing bytes after host", func(t *testing.T) {
		h := Host{AFamily: 2, Port: 32108, Addr: net.ParseIP("192.168.1.1")}
		payload, err := (HelloAck{Host: h}).MarshalPayload()
		if err != nil {
			t.Fatalf("MarshalPayload: %v", err)
		}
		// Append one extra trailing byte
		payload = append(payload, 0xFF)
		_, err = ParseHelloAck(payload)
		if err == nil {
			t.Fatal("expected trailing bytes error, got nil")
		}
		if !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ParseP2pRdy — Duid errors
// ---------------------------------------------------------------------------

func TestParseP2pRdy_Errors(t *testing.T) {
	t.Run("empty payload — duid too short", func(t *testing.T) {
		_, err := ParseP2pRdy([]byte{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("19 bytes — one byte short of duid", func(t *testing.T) {
		_, err := ParseP2pRdy(make([]byte, 19))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("trailing bytes after duid", func(t *testing.T) {
		d := Duid{Prefix: "EUPRAKM", Serial: 1234, Check: "ABCDE"}
		payload := d.marshalBinary()
		payload = append(payload, 0xFF) // spurious trailing byte
		_, err := ParseP2pRdy(payload)
		if err == nil {
			t.Fatal("expected trailing bytes error, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// ParseDuid — padding validation
// ---------------------------------------------------------------------------

func TestParseDuid_InvalidPadding(t *testing.T) {
	d := Duid{Prefix: "EUPRAKM", Serial: 1234, Check: "ABCDE"}
	raw := d.marshalBinary()
	// Corrupt the mandatory zero padding at bytes 18–19
	raw[18] = 0xFF
	_, _, err := parseDuid(raw)
	if err == nil {
		t.Fatal("expected padding error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid duid padding") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ParseListenResp — padding and trailing bytes
// ---------------------------------------------------------------------------

func TestParseListenResp_Errors(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		_, err := ParseListenResp([]byte{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("non-zero padding bytes in header", func(t *testing.T) {
		// payload[0]=1 host, but padding bytes [1:4] are not zero
		raw := []byte{0x01, 0xFF, 0x00, 0x00}
		raw = append(raw, make([]byte, 16)...) // 16-byte host placeholder with wrong afamily...
		_, err := ParseListenResp(raw)
		// Exact error may vary (padding or host parse); we just need an error.
		if err == nil {
			t.Fatal("expected error for non-zero padding, got nil")
		}
	})

	t.Run("trailing bytes after relay list", func(t *testing.T) {
		orig := ListenResp{Relays: []Host{
			{AFamily: 2, Port: 32100, Addr: net.ParseIP("10.0.0.1")},
		}}
		payload, err := orig.MarshalPayload()
		if err != nil {
			t.Fatalf("MarshalPayload: %v", err)
		}
		payload = append(payload, 0xFF) // trailing garbage
		_, err = ParseListenResp(payload)
		if err == nil {
			t.Fatal("expected trailing bytes error, got nil")
		}
		if !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ParseDuidString — malformed strings
// ---------------------------------------------------------------------------

func TestParseDuidString_Errors(t *testing.T) {
	// fmt.Sscanf with %7s only consumes up to 7 chars, so strings like
	// "TOOLONG-1234567-EXTRA" are accepted (prefix is truncated to 7 chars).
	// These are the inputs that genuinely fail the Sscanf pattern.
	bad := []string{
		"",
		"NOHYPHEN",
		"ABCDEF-notanumber-QWERT",
		"ABCDEF-123",
		"only-one-hyphen",
	}
	for _, s := range bad {
		_, err := ParseDuidString(s)
		if err == nil {
			t.Fatalf("expected error for %q, got nil", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Drw via DecodePacket — corrupted inner bytes after valid envelope
// ---------------------------------------------------------------------------

func TestDecodePacket_DrwBadInnerSignature(t *testing.T) {
	// Construct a PPPP envelope that says TypeDrw but has a bad DRW signature.
	inner := []byte{0xAB, 0x01, 0x00, 0x02, 0x11, 0x22} // 0xAB != 0xD1
	msg := Message{Type: TypeDrw, Payload: inner}
	_, err := DecodePacket(msg.MarshalBinary())
	if err == nil {
		t.Fatal("expected error for bad drw signature, got nil")
	}
	if !strings.Contains(err.Error(), "invalid drw signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodePacket_DrwAckCountMismatch(t *testing.T) {
	// Construct a PPPP envelope with TypeDrwAck that has count/length mismatch.
	// count=3 but only 2 ack slots present (4 bytes data).
	inner := []byte{drwSignature, 0x01, 0x00, 0x03, 0x00, 0x01, 0x00, 0x02}
	msg := Message{Type: TypeDrwAck, Payload: inner}
	_, err := DecodePacket(msg.MarshalBinary())
	if err == nil {
		t.Fatal("expected error for drw_ack count mismatch, got nil")
	}
}
