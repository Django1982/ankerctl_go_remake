package protocol

import (
	"net"
	"testing"
)

func mustEncodePacket(t *testing.T, p Packet) []byte {
	t.Helper()
	out, err := EncodePacket(p)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	return out
}

func TestMessageRoundTripDrw(t *testing.T) {
	orig := Drw{Chan: 3, Index: 42, Data: []byte("hello")}
	wire := mustEncodePacket(t, orig)
	decodedAny, err := DecodePacket(wire)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	decoded, ok := decodedAny.(Drw)
	if !ok {
		t.Fatalf("expected Drw, got %T", decodedAny)
	}
	if decoded.Chan != orig.Chan || decoded.Index != orig.Index || string(decoded.Data) != string(orig.Data) {
		t.Fatalf("decoded packet mismatch: %#v vs %#v", decoded, orig)
	}
}

func TestMessageRoundTripDrwAck(t *testing.T) {
	orig := DrwAck{Chan: 1, Acks: []uint16{1, 2, 65535}}
	wire := mustEncodePacket(t, orig)
	decodedAny, err := DecodePacket(wire)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	decoded, ok := decodedAny.(DrwAck)
	if !ok {
		t.Fatalf("expected DrwAck, got %T", decodedAny)
	}
	if len(decoded.Acks) != len(orig.Acks) {
		t.Fatalf("ack length mismatch: %d != %d", len(decoded.Acks), len(orig.Acks))
	}
	for i := range orig.Acks {
		if decoded.Acks[i] != orig.Acks[i] {
			t.Fatalf("ack[%d] mismatch: %d != %d", i, decoded.Acks[i], orig.Acks[i])
		}
	}
}

func TestHostRoundTrip(t *testing.T) {
	h := Host{AFamily: 2, Port: 32108, Addr: net.ParseIP("192.168.1.50")}
	payload, err := (HelloAck{Host: h}).MarshalPayload()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	parsed, err := ParseHelloAck(payload)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Host.Port != h.Port || !parsed.Host.Addr.Equal(h.Addr.To4()) {
		t.Fatalf("host mismatch: %#v vs %#v", parsed.Host, h)
	}
}

func TestDuidRoundTrip(t *testing.T) {
	d := Duid{Prefix: "ABCDEF", Serial: 123456, Check: "QWERT"}
	pkt := P2pRdy{DUID: d}
	wire := mustEncodePacket(t, pkt)
	decodedAny, err := DecodePacket(wire)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	decoded := decodedAny.(P2pRdy)
	if decoded.DUID.Serial != d.Serial || decoded.DUID.Prefix != d.Prefix || decoded.DUID.Check != d.Check {
		t.Fatalf("duid mismatch: %#v vs %#v", decoded.DUID, d)
	}
}

func TestListenRespRoundTrip(t *testing.T) {
	orig := ListenResp{Relays: []Host{
		{AFamily: 2, Port: 32100, Addr: net.ParseIP("10.0.0.1")},
		{AFamily: 2, Port: 32101, Addr: net.ParseIP("10.0.0.2")},
	}}
	payload, err := orig.MarshalPayload()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	parsed, err := ParseListenResp(payload)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(parsed.Relays) != 2 {
		t.Fatalf("expected 2 relays, got %d", len(parsed.Relays))
	}
	if !parsed.Relays[0].Addr.Equal(net.ParseIP("10.0.0.1").To4()) {
		t.Fatalf("unexpected first relay: %v", parsed.Relays[0].Addr)
	}
}

func TestXzyhRoundTrip(t *testing.T) {
	orig := Xzyh{Cmd: 0x06A4, Unk0: 1, Unk1: 2, Chan: 1, SignCode: 3, Unk3: 4, DevType: 5, Data: []byte("payload")}
	wire, err := orig.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	parsed, err := ParseXzyh(wire)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Cmd != orig.Cmd || string(parsed.Data) != string(orig.Data) || parsed.Chan != orig.Chan {
		t.Fatalf("xzyh mismatch: %#v vs %#v", parsed, orig)
	}
}

func TestAabbWithCRCRoundTrip(t *testing.T) {
	orig := Aabb{FrameType: FileTransferData, SN: 7, Pos: 1024}
	blob := []byte("abcdef")
	wire, err := orig.PackWithCRC(blob)
	if err != nil {
		t.Fatalf("pack failed: %v", err)
	}
	parsedHdr, parsedData, err := ParseAabbWithCRC(wire)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsedHdr.FrameType != orig.FrameType || parsedHdr.SN != orig.SN || parsedHdr.Pos != orig.Pos {
		t.Fatalf("aabb header mismatch: %#v vs %#v", parsedHdr, orig)
	}
	if string(parsedData) != string(blob) {
		t.Fatalf("aabb data mismatch: %q != %q", parsedData, blob)
	}
}

func TestDecodeUnknownMessageFallback(t *testing.T) {
	msg := Message{Type: TypeInvalid, Payload: []byte{1, 2, 3}}
	decoded, err := DecodePacket(msg.MarshalBinary())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := decoded.(Message); !ok {
		t.Fatalf("expected raw Message fallback, got %T", decoded)
	}
}
