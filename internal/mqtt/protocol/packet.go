package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	internalcrypto "github.com/django1982/ankerctl/internal/crypto"
)

// Packet represents one encrypted MQTT frame exchanged with Anker printers.
//
// Wire format:
// - Header (M5: 64 bytes, M5C: 24 bytes)
// - AES-256-CBC encrypted JSON body
// - XOR checksum byte
//
// Size includes header + encrypted body + checksum byte.
type Packet struct {
	Size       uint16
	M3         byte
	M4         byte
	M5         byte
	M6         byte
	M7         byte
	PacketType MqttPktType
	PacketNum  uint16
	Time       uint32
	DeviceGUID string
	Padding    []byte
	Data       []byte
}

// NewPacket builds a default M5 packet (m5=2) for command/query payloads.
func NewPacket(guid string, data []byte) Packet {
	return Packet{
		M3:         mqttMagicM3,
		M4:         mqttMagicM4,
		M5:         mqttMagicM5M5,
		M6:         mqttMagicM6,
		M7:         mqttMagicM7,
		PacketType: MqttPktSingle,
		PacketNum:  0,
		Time:       0,
		DeviceGUID: guid,
		Padding:    bytes.Repeat([]byte{0x00}, 11),
		Data:       data,
	}
}

// MarshalBinary encodes a packet into its MQTT wire representation.
func (p Packet) MarshalBinary(key []byte) ([]byte, error) {
	headerLen, err := packetHeaderLen(p.M5)
	if err != nil {
		return nil, err
	}

	encrypted, err := internalcrypto.MQTTEncrypt(p.Data, key)
	if err != nil {
		return nil, fmt.Errorf("mqtt marshal: encrypt data: %w", err)
	}

	size := headerLen + len(encrypted) + 1
	if size > 0xFFFF {
		return nil, fmt.Errorf("mqtt marshal: packet too large (%d bytes)", size)
	}

	buf := make([]byte, headerLen+len(encrypted))
	buf[0] = mqttSignatureA
	buf[1] = mqttSignatureB
	binary.LittleEndian.PutUint16(buf[2:4], uint16(size))
	buf[4] = p.M3
	buf[5] = p.M4
	buf[6] = p.M5
	buf[7] = p.M6
	buf[8] = p.M7
	buf[9] = byte(p.PacketType)
	binary.LittleEndian.PutUint16(buf[10:12], p.PacketNum)

	if p.M5 == mqttMagicM5M5 {
		binary.LittleEndian.PutUint32(buf[12:16], p.Time)
		copy(buf[16:53], packCString37(p.DeviceGUID))
		copy(buf[53:64], ensurePadding(p.Padding, 11))
	} else {
		copy(buf[12:24], ensurePadding(p.Padding, 12))
	}

	copy(buf[headerLen:], encrypted)
	return internalcrypto.AddChecksum(buf), nil
}

// UnmarshalPacket decodes a packet from its MQTT wire representation.
func UnmarshalPacket(payload []byte, key []byte) (*Packet, error) {
	msg, err := internalcrypto.RemoveChecksum(payload)
	if err != nil {
		return nil, fmt.Errorf("mqtt unmarshal: remove checksum: %w", err)
	}
	if len(msg) < 12 {
		return nil, fmt.Errorf("mqtt unmarshal: payload too short (%d)", len(msg))
	}
	if msg[0] != mqttSignatureA || msg[1] != mqttSignatureB {
		return nil, fmt.Errorf("mqtt unmarshal: invalid signature %q", msg[:2])
	}

	size := binary.LittleEndian.Uint16(msg[2:4])
	m5 := msg[6]
	headerLen, err := packetHeaderLen(m5)
	if err != nil {
		return nil, err
	}
	if len(msg) < headerLen {
		return nil, fmt.Errorf("mqtt unmarshal: short header (%d < %d)", len(msg), headerLen)
	}
	if m5 == mqttMagicM5M5 && int(size) != len(msg)+1 {
		return nil, fmt.Errorf("mqtt unmarshal: size mismatch (header=%d actual=%d)", size, len(msg)+1)
	}

	plain, err := internalcrypto.MQTTDecrypt(msg[headerLen:], key)
	if err != nil {
		return nil, fmt.Errorf("mqtt unmarshal: decrypt data: %w", err)
	}

	pkt := &Packet{
		Size:       size,
		M3:         msg[4],
		M4:         msg[5],
		M5:         m5,
		M6:         msg[7],
		M7:         msg[8],
		PacketType: MqttPktType(msg[9]),
		PacketNum:  binary.LittleEndian.Uint16(msg[10:12]),
		Data:       plain,
	}

	if m5 == mqttMagicM5M5 {
		pkt.Time = binary.LittleEndian.Uint32(msg[12:16])
		pkt.DeviceGUID = decodeCString(msg[16:53])
		pkt.Padding = append([]byte(nil), msg[53:64]...)
	} else {
		pkt.Padding = append([]byte(nil), msg[12:24]...)
	}

	return pkt, nil
}

// GetJSON decodes packet data as JSON.
func (p Packet) GetJSON(v any) error {
	if err := json.Unmarshal(p.Data, v); err != nil {
		return fmt.Errorf("mqtt packet json decode: %w", err)
	}
	return nil
}

// SetJSON encodes value as JSON and stores it as packet data.
func (p *Packet) SetJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mqtt packet json encode: %w", err)
	}
	p.Data = data
	return nil
}

func packetHeaderLen(m5 byte) (int, error) {
	switch m5 {
	case mqttMagicM5M5:
		return HeaderLenM5, nil
	case mqttMagicM5M5C:
		return HeaderLenM5C, nil
	default:
		return 0, fmt.Errorf("mqtt: unsupported packet format m5=%d", m5)
	}
}

func packCString37(s string) []byte {
	trimmed := s
	if len(trimmed) > 36 {
		trimmed = trimmed[:36]
	}
	out := make([]byte, 37)
	copy(out, []byte(trimmed))
	return out
}

func decodeCString(b []byte) string {
	idx := bytes.IndexByte(b, 0)
	if idx < 0 {
		idx = len(b)
	}
	return strings.TrimRight(string(b[:idx]), "\x00")
}

func ensurePadding(in []byte, want int) []byte {
	out := make([]byte, want)
	copy(out, in)
	return out
}
