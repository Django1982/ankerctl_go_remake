package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestPacketMarshalBinary_KnownPythonVector(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	pkt := Packet{
		M3:         mqttMagicM3,
		M4:         mqttMagicM4,
		M5:         mqttMagicM5M5,
		M6:         mqttMagicM6,
		M7:         mqttMagicM7,
		PacketType: MqttPktSingle,
		PacketNum:  7,
		Time:       1700000000,
		DeviceGUID: "123e4567-e89b-12d3-a456-426614174000",
		Padding:    bytes.Repeat([]byte{0}, 11),
		Data:       []byte(`{"commandType":1000,"value":1}`),
	}

	got, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	want := mustHex(t, "4d4161000501020546c0070000f1536531323365343536372d653839622d313264332d613435362d343236363134313734303030000000000000000000000000ee083eb0dbecc9f0016443bbdbbf0cac3f4ba0933d56ec4c41001983e985b3c906")
	if !bytes.Equal(got, want) {
		t.Fatalf("marshal mismatch\n got: %x\nwant: %x", got, want)
	}
}

func TestPacketUnmarshalPacket_RoundTripM5(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	in := NewPacket("123e4567-e89b-12d3-a456-426614174000", []byte(`{"commandType":1000}`))
	in.PacketNum = 42
	in.Time = 1700000000

	encoded, err := in.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	out, err := UnmarshalPacket(encoded, key)
	if err != nil {
		t.Fatalf("UnmarshalPacket: %v", err)
	}

	if out.M5 != mqttMagicM5M5 {
		t.Fatalf("M5 = %d, want %d", out.M5, mqttMagicM5M5)
	}
	if out.PacketNum != in.PacketNum {
		t.Fatalf("PacketNum = %d, want %d", out.PacketNum, in.PacketNum)
	}
	if out.Time != in.Time {
		t.Fatalf("Time = %d, want %d", out.Time, in.Time)
	}
	if out.DeviceGUID != in.DeviceGUID {
		t.Fatalf("DeviceGUID = %q, want %q", out.DeviceGUID, in.DeviceGUID)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("Data mismatch: got %q want %q", out.Data, in.Data)
	}
}

func TestPacketUnmarshalPacket_RoundTripM5C(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	in := Packet{
		M3:         mqttMagicM3,
		M4:         mqttMagicM4,
		M5:         mqttMagicM5M5C,
		M6:         mqttMagicM6,
		M7:         mqttMagicM7,
		PacketType: MqttPktSingle,
		PacketNum:  1,
		Padding:    bytes.Repeat([]byte{0xAB}, 12),
		Data:       []byte(`{"commandType":1000}`),
	}

	encoded, err := in.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	out, err := UnmarshalPacket(encoded, key)
	if err != nil {
		t.Fatalf("UnmarshalPacket: %v", err)
	}
	if out.M5 != mqttMagicM5M5C {
		t.Fatalf("M5 = %d, want %d", out.M5, mqttMagicM5M5C)
	}
	if len(out.Padding) != 12 {
		t.Fatalf("Padding len = %d, want 12", len(out.Padding))
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("Data mismatch: got %q want %q", out.Data, in.Data)
	}
}

// TestPacketUnmarshalPacket_KnownPythonVectorM5C verifies that packets generated
// by the Python reference implementation (libflagship/mqtt.py _parse_m5c path)
// are decoded identically by Go.  The hex payloads were produced by running:
//
//	mqtt_aes_encrypt(data, key) → assemble 24-byte header → mqtt_checksum_add
//
// with key=00112233445566778899aabbccddeeff and the data shown in each sub-test.
// This is a cross-implementation test: Python generates, Go must decode.
// The existing round-trip test (RoundTripM5C) only proves Go↔Go consistency; it
// cannot catch a symmetric bug present in both MarshalBinary and UnmarshalPacket.
func TestPacketUnmarshalPacket_KnownPythonVectorM5C(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")

	tests := []struct {
		name      string
		packetHex string
		wantData  string
		wantPN    uint16
	}{
		{
			name:      "ct=1000 value=1 pktNum=1",
			packetHex: "4d4139000501010546c00100000000000000000000000000ee083eb0dbecc9f0016443bbdbbf0cac3f4ba0933d56ec4c41001983e985b3c9cc",
			wantData:  `{"commandType":1000,"value":1}`,
			wantPN:    1,
		},
		{
			name:      "ct=1001 progress pktNum=2",
			packetHex: "4d4159000501010546c00200000000000000000000000000ee083eb0dbecc9f0016443bbdbbf0cac2fee35963a47c1acde91d92833e976902483908cb33d0615108bb1f2a0585b514c9ef330802a5e23dc7631599af0a025f9",
			wantData:  `{"commandType":1001,"progress":5000,"filename":"test.gcode"}`,
			wantPN:    2,
		},
		{
			name:      "ct=1003 temperature pktNum=3",
			packetHex: "4d4149000501010546c00300000000000000000000000000ee083eb0dbecc9f0016443bbdbbf0cace5d7cbb2f3645d046f1164cb84a00fbaba8380796338162304f1c49014babe6045",
			wantData:  `{"commandType":1003,"temperature":19500}`,
			wantPN:    3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := mustHex(t, tc.packetHex)

			pkt, err := UnmarshalPacket(payload, key)
			if err != nil {
				t.Fatalf("UnmarshalPacket: %v", err)
			}
			if pkt.M5 != mqttMagicM5M5C {
				t.Fatalf("M5 = %d, want %d (M5C)", pkt.M5, mqttMagicM5M5C)
			}
			if pkt.PacketNum != tc.wantPN {
				t.Fatalf("PacketNum = %d, want %d", pkt.PacketNum, tc.wantPN)
			}
			if len(pkt.Padding) != 12 {
				t.Fatalf("Padding len = %d, want 12", len(pkt.Padding))
			}
			if !bytes.Equal(pkt.Data, []byte(tc.wantData)) {
				t.Fatalf("Data mismatch\n got:  %q\nwant: %q", pkt.Data, tc.wantData)
			}
		})
	}
}

func TestPacketUnmarshalPacket_BadChecksum(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	pkt := NewPacket("123e4567-e89b-12d3-a456-426614174000", []byte(`{"x":1}`))
	encoded, err := pkt.MarshalBinary(key)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	encoded[len(encoded)-1] ^= 0xFF

	if _, err := UnmarshalPacket(encoded, key); err == nil {
		t.Fatal("expected checksum error, got nil")
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("DecodeString(%q): %v", s, err)
	}
	return b
}
