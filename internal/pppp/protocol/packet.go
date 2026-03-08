package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	// MagicNumber is the fixed PPPP message magic byte.
	MagicNumber byte = 0xF1

	drwSignature  byte   = 0xD1
	xzyhMagic            = "XZYH"
	aabbSignature uint16 = 0xBBAA // little-endian representation for wire bytes AA BB
)

// Type identifies a PPPP message type byte.
type Type uint8

const (
	TypeHello                 Type = 0x00
	TypeHelloAck              Type = 0x01
	TypeHelloTo               Type = 0x02
	TypeHelloToAck            Type = 0x03
	TypeQueryDid              Type = 0x08
	TypeQueryDidAck           Type = 0x09
	TypeDevLgn                Type = 0x10
	TypeDevLgnAck             Type = 0x11
	TypeDevLgnCRC             Type = 0x12
	TypeDevLgnAckCRC          Type = 0x13
	TypeDevLgnKey             Type = 0x14
	TypeDevLgnAckKey          Type = 0x15
	TypeDevLgnDsk             Type = 0x16
	TypeDevOnlineReq          Type = 0x18
	TypeDevOnlineReqAck       Type = 0x19
	TypeP2pReq                Type = 0x20
	TypeP2pReqAck             Type = 0x21
	TypeP2pReqDsk             Type = 0x26
	TypeLanSearch             Type = 0x30
	TypeLanNotify             Type = 0x31
	TypeLanNotifyAck          Type = 0x32
	TypePunchTo               Type = 0x40
	TypePunchPkt              Type = 0x41
	TypePunchPktEx            Type = 0x41
	TypeP2pRdy                Type = 0x42
	TypeP2pRdyEx              Type = 0x42
	TypeP2pRdyAck             Type = 0x43
	TypeRsLgn                 Type = 0x60
	TypeRsLgnAck              Type = 0x61
	TypeRsLgn1                Type = 0x62
	TypeRsLgn1Ack             Type = 0x63
	TypeListReq1              Type = 0x67
	TypeListReq               Type = 0x68
	TypeListReqAck            Type = 0x69
	TypeListReqDsk            Type = 0x6A
	TypeRlyHello              Type = 0x70
	TypeRlyHelloAck           Type = 0x71
	TypeRlyPort               Type = 0x72
	TypeRlyPortAck            Type = 0x73
	TypeRlyPortKey            Type = 0x74
	TypeRlyPortAckKey         Type = 0x75
	TypeRlyByteCount          Type = 0x78
	TypeRlyReq                Type = 0x80
	TypeRlyReqAck             Type = 0x81
	TypeRlyTo                 Type = 0x82
	TypeRlyPkt                Type = 0x83
	TypeRlyRdy                Type = 0x84
	TypeRlyToAck              Type = 0x85
	TypeRlyServerReq          Type = 0x87
	TypeRlyServerReqAck       Type = 0x87
	TypeSdevRun               Type = 0x90
	TypeSdevLgn               Type = 0x91
	TypeSdevLgnAck            Type = 0x91
	TypeSdevLgnCRC            Type = 0x92
	TypeSdevLgnAckCRC         Type = 0x92
	TypeSdevReport            Type = 0x94
	TypeConnectReport         Type = 0xA0
	TypeReportReq             Type = 0xA1
	TypeReport                Type = 0xA2
	TypeDrw                   Type = 0xD0
	TypeDrwAck                Type = 0xD1
	TypePsr                   Type = 0xD8
	TypeAlive                 Type = 0xE0
	TypeAliveAck              Type = 0xE1
	TypeClose                 Type = 0xF0
	TypeMgmDumpLoginDID       Type = 0xF4
	TypeMgmDumpLoginDIDDetail Type = 0xF5
	TypeMgmDumpLoginDID1      Type = 0xF6
	TypeMgmLogControl         Type = 0xF7
	TypeMgmRemoteManagement   Type = 0xF8
	TypeReportSessionReady    Type = 0xF9
	TypeInvalid               Type = 0xFF
)

// Result is a 32-bit PPPP result code enum.
type Result uint32

const (
	ResultErrorP2PSuccessful                      Result = 0x00000000
	ResultTFCardVolumeOverflow                    Result = 0xFFFFFF7C
	ResultParamNoChange                           Result = 0xFFFFFF8C
	ResultNotFace                                 Result = 0xFFFFFF8D
	ResultDevBusy                                 Result = 0xFFFFFF8E
	ResultDevUpdateing                            Result = 0xFFFFFF8F
	ResultHubUpdateing                            Result = 0xFFFFFF90
	ResultOpenFileFail                            Result = 0xFFFFFF91
	ResultInvalidParam                            Result = 0xFFFFFF92
	ResultDevOffline                              Result = 0xFFFFFF93
	ResultWaitTimeout                             Result = 0xFFFFFF94
	ResultNvalidParamLen                          Result = 0xFFFFFF95
	ResultNotFindDev                              Result = 0xFFFFFF96
	ResultWriteFlash                              Result = 0xFFFFFF97
	ResultInvalidAccount                          Result = 0xFFFFFF98
	ResultInvalidCommand                          Result = 0xFFFFFF99
	ResultMaxHubConnectNum                        Result = 0xFFFFFF9A
	ResultHaveConnect                             Result = 0xFFFFFF9B
	ResultNullPoint                               Result = 0xFFFFFF9C
	ResultErrorP2PFailToCreateThread              Result = 0xFFFFFFEA
	ResultErrorP2PInvalidAPILicense               Result = 0xFFFFFFEB
	ResultErrorP2PSessionClosedInsufficientMemory Result = 0xFFFFFFEC
	ResultErrorP2PUserConnectBreak                Result = 0xFFFFFFED
	ResultErrorP2PUDPPortBindFailed               Result = 0xFFFFFFEE
	ResultErrorP2PMaxSession                      Result = 0xFFFFFFEF
	ResultErrorP2PUserListenBreak                 Result = 0xFFFFFFF0
	ResultErrorP2PRemoteSiteBufferFull            Result = 0xFFFFFFF1
	ResultErrorP2PSessionClosedCalled             Result = 0xFFFFFFF2
	ResultErrorP2PSessionClosedTimeout            Result = 0xFFFFFFF3
	ResultErrorP2PSessionClosedRemote             Result = 0xFFFFFFF4
	ResultErrorP2PInvalidSessionHandle            Result = 0xFFFFFFF5
	ResultErrorP2PNoRelayServerAvailable          Result = 0xFFFFFFF6
	ResultErrorP2PIDOutOfDate                     Result = 0xFFFFFFF7
	ResultErrorP2PInvalidPrefix                   Result = 0xFFFFFFF8
	ResultErrorP2PFailToResolveName               Result = 0xFFFFFFF9
	ResultErrorP2PDeviceNotOnline                 Result = 0xFFFFFFFA
	ResultErrorPPCSInvalidParameter               Result = 0xFFFFFFFB
	ResultErrorP2PInvalidID                       Result = 0xFFFFFFFC
	ResultErrorP2PTimeOut                         Result = 0xFFFFFFFD
	ResultErrorP2PAlreadyInitialized              Result = 0xFFFFFFFE
	ResultErrorP2PNotInitialized                  Result = 0xFFFFFFFF
)

// P2PCmdType is the command ID inside XZYH frames.
type P2PCmdType uint16

const (
	P2PCmdStartRealtimeMedia P2PCmdType = 0x03EB
	P2PCmdStopRealtimeMedia  P2PCmdType = 0x03EC
	P2PCmdVideoFrame         P2PCmdType = 0x0514
	P2PCmdP2pJson            P2PCmdType = 0x06A4
	P2PCmdP2pSendFile        P2PCmdType = 0x3A98
)

// P2PSubCmdType is used for API commands inside XZYH payloads.
type P2PSubCmdType uint16

const (
	P2PSubCmdStartLive        P2PSubCmdType = 0x03E8
	P2PSubCmdCloseLive        P2PSubCmdType = 0x03E9
	P2PSubCmdLightStateSwitch P2PSubCmdType = 0x03EB
	P2PSubCmdLiveModeSet      P2PSubCmdType = 0x03ED
)

// FileTransfer is the AABB frame type.
type FileTransfer uint8

const (
	FileTransferBegin FileTransfer = 0x00
	FileTransferData  FileTransfer = 0x01
	FileTransferEnd   FileTransfer = 0x02
	FileTransferAbort FileTransfer = 0x03
	FileTransferReply FileTransfer = 0x80
)

// Message is a generic PPPP packet with raw payload.
type Message struct {
	Type    Type
	Payload []byte
}

// MarshalBinary serializes message with PPPP framing.
func (m Message) MarshalBinary() []byte {
	out := make([]byte, 4+len(m.Payload))
	out[0] = MagicNumber
	out[1] = byte(m.Type)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(m.Payload)))
	copy(out[4:], m.Payload)
	return out
}

// ParseMessage parses the PPPP message envelope.
func ParseMessage(data []byte) (Message, error) {
	if len(data) < 4 {
		return Message{}, fmt.Errorf("pppp: short message header: %d", len(data))
	}
	if data[0] != MagicNumber {
		return Message{}, fmt.Errorf("pppp: invalid magic: 0x%02x", data[0])
	}
	sz := int(binary.BigEndian.Uint16(data[2:4]))
	if len(data) < 4+sz {
		return Message{}, fmt.Errorf("pppp: payload truncated: need %d have %d", sz, len(data)-4)
	}
	payload := make([]byte, sz)
	copy(payload, data[4:4+sz])
	return Message{Type: Type(data[1]), Payload: payload}, nil
}

// Packet is a typed PPPP packet.
type Packet interface {
	PacketType() Type
	MarshalPayload() ([]byte, error)
}

// EncodePacket serializes a typed packet.
func EncodePacket(p Packet) ([]byte, error) {
	body, err := p.MarshalPayload()
	if err != nil {
		return nil, err
	}
	return Message{Type: p.PacketType(), Payload: body}.MarshalBinary(), nil
}

// DecodePacket decodes known packet types and falls back to raw Message.
func DecodePacket(data []byte) (any, error) {
	msg, err := ParseMessage(data)
	if err != nil {
		return nil, err
	}

	switch msg.Type {
	case TypeDrw:
		return ParseDrw(msg.Payload)
	case TypeDrwAck:
		return ParseDrwAck(msg.Payload)
	case TypeClose:
		return Close{}, nil
	case TypeAlive:
		return PingReq{}, nil
	case TypeAliveAck:
		return PingResp{}, nil
	case TypeHello:
		return Hello{}, nil
	case TypeLanSearch:
		return LanSearch{}, nil
	case TypeHelloAck:
		return ParseHelloAck(msg.Payload)
	case TypeP2pRdy:
		return ParseP2pRdy(msg.Payload)
	case TypeP2pRdyAck:
		return ParseP2pRdyAck(msg.Payload)
	case TypeListReqAck:
		return ParseListenResp(msg.Payload)
	case TypePunchPkt:
		return ParsePunchPkt(msg.Payload)
	default:
		return msg, nil
	}
}

// Host represents PPPP host wire format (16 bytes).
type Host struct {
	AFamily uint8
	Port    uint16
	Addr    net.IP
}

func (h Host) marshalBinary() ([]byte, error) {
	if h.AFamily == 0 {
		h.AFamily = 2
	}
	ip4 := h.Addr.To4()
	if ip4 == nil {
		return nil, errors.New("pppp: host requires IPv4")
	}
	out := make([]byte, 16)
	out[0] = 0
	out[1] = h.AFamily
	binary.LittleEndian.PutUint16(out[2:4], h.Port)
	// Python IPv4 stores reversed byte order.
	out[4] = ip4[3]
	out[5] = ip4[2]
	out[6] = ip4[1]
	out[7] = ip4[0]
	// trailing 8 bytes remain zero
	return out, nil
}

func parseHost(data []byte) (Host, []byte, error) {
	if len(data) < 16 {
		return Host{}, nil, fmt.Errorf("pppp: short host: %d", len(data))
	}
	if data[0] != 0 {
		return Host{}, nil, fmt.Errorf("pppp: invalid host pad0 0x%02x", data[0])
	}
	for i := 8; i < 16; i++ {
		if data[i] != 0 {
			return Host{}, nil, fmt.Errorf("pppp: invalid host pad1 at %d", i)
		}
	}
	h := Host{
		AFamily: data[1],
		Port:    binary.LittleEndian.Uint16(data[2:4]),
		Addr:    net.IPv4(data[7], data[6], data[5], data[4]),
	}
	return h, data[16:], nil
}

// Duid represents printer identity tuple.
type Duid struct {
	Prefix string
	Serial uint32
	Check  string
}

func ParseDuidString(v string) (Duid, error) {
	var d Duid
	n, err := fmt.Sscanf(v, "%7s-%d-%5s", &d.Prefix, &d.Serial, &d.Check)
	if err != nil || n != 3 {
		return Duid{}, fmt.Errorf("pppp: invalid duid string %q", v)
	}
	return d, nil
}

func (d Duid) String() string {
	return fmt.Sprintf("%s-%06d-%s", d.Prefix, d.Serial, d.Check)
}

func (d Duid) marshalBinary() []byte {
	out := make([]byte, 20)
	copy(out[0:8], []byte(d.Prefix))
	binary.BigEndian.PutUint32(out[8:12], d.Serial)
	copy(out[12:18], []byte(d.Check))
	return out
}

func parseDuid(data []byte) (Duid, []byte, error) {
	if len(data) < 20 {
		return Duid{}, nil, fmt.Errorf("pppp: short duid: %d", len(data))
	}
	prefix := string(trimZero(data[0:8]))
	check := string(trimZero(data[12:18]))
	d := Duid{Prefix: prefix, Serial: binary.BigEndian.Uint32(data[8:12]), Check: check}
	if data[18] != 0 || data[19] != 0 {
		return Duid{}, nil, errors.New("pppp: invalid duid padding")
	}
	return d, data[20:], nil
}

func trimZero(in []byte) []byte {
	for i, b := range in {
		if b == 0 {
			return in[:i]
		}
	}
	return in
}

// Drw is reliable data transport packet.
type Drw struct {
	Chan  uint8
	Index uint16
	Data  []byte
}

func (d Drw) PacketType() Type { return TypeDrw }

func (d Drw) MarshalPayload() ([]byte, error) {
	out := make([]byte, 4+len(d.Data))
	out[0] = drwSignature
	out[1] = d.Chan
	binary.BigEndian.PutUint16(out[2:4], d.Index)
	copy(out[4:], d.Data)
	return out, nil
}

func ParseDrw(payload []byte) (Drw, error) {
	if len(payload) < 4 {
		return Drw{}, fmt.Errorf("pppp: short drw payload: %d", len(payload))
	}
	if payload[0] != drwSignature {
		return Drw{}, fmt.Errorf("pppp: invalid drw signature 0x%02x", payload[0])
	}
	res := Drw{Chan: payload[1], Index: binary.BigEndian.Uint16(payload[2:4])}
	res.Data = append([]byte(nil), payload[4:]...)
	return res, nil
}

// DrwAck acknowledges DRW packet indices.
type DrwAck struct {
	Chan uint8
	Acks []uint16
}

func (d DrwAck) PacketType() Type { return TypeDrwAck }

func (d DrwAck) MarshalPayload() ([]byte, error) {
	out := make([]byte, 4+2*len(d.Acks))
	out[0] = drwSignature
	out[1] = d.Chan
	binary.BigEndian.PutUint16(out[2:4], uint16(len(d.Acks)))
	for i, ack := range d.Acks {
		binary.BigEndian.PutUint16(out[4+i*2:], ack)
	}
	return out, nil
}

func ParseDrwAck(payload []byte) (DrwAck, error) {
	if len(payload) < 4 {
		return DrwAck{}, fmt.Errorf("pppp: short drw_ack payload: %d", len(payload))
	}
	if payload[0] != drwSignature {
		return DrwAck{}, fmt.Errorf("pppp: invalid drw_ack signature 0x%02x", payload[0])
	}
	count := int(binary.BigEndian.Uint16(payload[2:4]))
	if len(payload) != 4+count*2 {
		return DrwAck{}, fmt.Errorf("pppp: drw_ack malformed count=%d len=%d", count, len(payload))
	}
	res := DrwAck{Chan: payload[1], Acks: make([]uint16, count)}
	for i := 0; i < count; i++ {
		res.Acks[i] = binary.BigEndian.Uint16(payload[4+i*2:])
	}
	return res, nil
}

// Close terminates PPPP connection.
type Close struct{}

func (Close) PacketType() Type                 { return TypeClose }
func (Close) MarshalPayload() ([]byte, error)  { return nil, nil }
func ParseClose(payload []byte) (Close, error) { return Close{}, emptyPayload(payload) }

// PingReq corresponds to ALIVE.
type PingReq struct{}

func (PingReq) PacketType() Type                   { return TypeAlive }
func (PingReq) MarshalPayload() ([]byte, error)    { return nil, nil }
func ParsePingReq(payload []byte) (PingReq, error) { return PingReq{}, emptyPayload(payload) }

// PingResp corresponds to ALIVE_ACK.
type PingResp struct{}

func (PingResp) PacketType() Type                    { return TypeAliveAck }
func (PingResp) MarshalPayload() ([]byte, error)     { return nil, nil }
func ParsePingResp(payload []byte) (PingResp, error) { return PingResp{}, emptyPayload(payload) }

// Hello is an empty HELLO packet.
type Hello struct{}

func (Hello) PacketType() Type                 { return TypeHello }
func (Hello) MarshalPayload() ([]byte, error)  { return nil, nil }
func ParseHello(payload []byte) (Hello, error) { return Hello{}, emptyPayload(payload) }

// HelloAck responds with host info.
type HelloAck struct {
	Host Host
}

func (h HelloAck) PacketType() Type { return TypeHelloAck }

func (h HelloAck) MarshalPayload() ([]byte, error) {
	return h.Host.marshalBinary()
}

func ParseHelloAck(payload []byte) (HelloAck, error) {
	h, rest, err := parseHost(payload)
	if err != nil {
		return HelloAck{}, err
	}
	if len(rest) != 0 {
		return HelloAck{}, errors.New("pppp: trailing bytes in hello_ack")
	}
	return HelloAck{Host: h}, nil
}

// LanSearch is LAN discovery broadcast packet.
type LanSearch struct{}

func (LanSearch) PacketType() Type                     { return TypeLanSearch }
func (LanSearch) MarshalPayload() ([]byte, error)      { return nil, nil }
func ParseLanSearch(payload []byte) (LanSearch, error) { return LanSearch{}, emptyPayload(payload) }

// P2pRdy announces peer-ready state.
type P2pRdy struct {
	DUID Duid
}

func (p P2pRdy) PacketType() Type { return TypeP2pRdy }

func (p P2pRdy) MarshalPayload() ([]byte, error) {
	return p.DUID.marshalBinary(), nil
}

func ParseP2pRdy(payload []byte) (P2pRdy, error) {
	d, rest, err := parseDuid(payload)
	if err != nil {
		return P2pRdy{}, err
	}
	if len(rest) != 0 {
		return P2pRdy{}, errors.New("pppp: trailing bytes in p2p_rdy")
	}
	return P2pRdy{DUID: d}, nil
}

// PunchPkt carries DUID for LAN discovery response.
type PunchPkt struct {
	DUID Duid
}

func (p PunchPkt) PacketType() Type { return TypePunchPkt }

func (p PunchPkt) MarshalPayload() ([]byte, error) {
	return p.DUID.marshalBinary(), nil
}

func ParsePunchPkt(payload []byte) (PunchPkt, error) {
	d, _, err := parseDuid(payload)
	if err != nil {
		return PunchPkt{}, err
	}
	// Trailing bytes are ignored (Python parity: PktPunchPkt.parse returns
	// remaining bytes without error; some firmware versions append extra data).
	return PunchPkt{DUID: d}, nil
}

// P2pRdyAck acknowledges P2P ready with host metadata.
type P2pRdyAck struct {
	DUID Duid
	Host Host
	Pad  [8]byte
}

func (p P2pRdyAck) PacketType() Type { return TypeP2pRdyAck }

func (p P2pRdyAck) MarshalPayload() ([]byte, error) {
	hostBytes, err := p.Host.marshalBinary()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 44)
	out = append(out, p.DUID.marshalBinary()...)
	out = append(out, hostBytes...)
	out = append(out, p.Pad[:]...)
	return out, nil
}

func ParseP2pRdyAck(payload []byte) (P2pRdyAck, error) {
	d, rest, err := parseDuid(payload)
	if err != nil {
		return P2pRdyAck{}, err
	}
	h, rest, err := parseHost(rest)
	if err != nil {
		return P2pRdyAck{}, err
	}
	if len(rest) != 8 {
		return P2pRdyAck{}, fmt.Errorf("pppp: invalid p2p_rdy_ack padding len=%d", len(rest))
	}
	var pad [8]byte
	copy(pad[:], rest)
	return P2pRdyAck{DUID: d, Host: h, Pad: pad}, nil
}

// ListenResp (Python LIST_REQ_ACK) contains relay list.
type ListenResp struct {
	Relays []Host
}

func (l ListenResp) PacketType() Type { return TypeListReqAck }

func (l ListenResp) MarshalPayload() ([]byte, error) {
	if len(l.Relays) > 255 {
		return nil, errors.New("pppp: too many relay hosts")
	}
	out := make([]byte, 4)
	out[0] = byte(len(l.Relays))
	for _, h := range l.Relays {
		hb, err := h.marshalBinary()
		if err != nil {
			return nil, err
		}
		out = append(out, hb...)
	}
	return out, nil
}

func ParseListenResp(payload []byte) (ListenResp, error) {
	if len(payload) < 4 {
		return ListenResp{}, fmt.Errorf("pppp: short listen_resp payload: %d", len(payload))
	}
	n := int(payload[0])
	if payload[1] != 0 || payload[2] != 0 || payload[3] != 0 {
		return ListenResp{}, errors.New("pppp: invalid listen_resp padding")
	}
	rest := payload[4:]
	relays := make([]Host, 0, n)
	for i := 0; i < n; i++ {
		h, next, err := parseHost(rest)
		if err != nil {
			return ListenResp{}, err
		}
		relays = append(relays, h)
		rest = next
	}
	if len(rest) != 0 {
		return ListenResp{}, errors.New("pppp: trailing bytes in listen_resp")
	}
	return ListenResp{Relays: relays}, nil
}

// Xzyh is the 16-byte command frame.
type Xzyh struct {
	Cmd      P2PCmdType
	Len      uint32
	Unk0     uint8
	Unk1     uint8
	Chan     uint8
	SignCode uint8
	Unk3     uint8
	DevType  uint8
	Data     []byte
}

// VideoFrame is the 64-byte XZYH frame for video data.
type VideoFrame struct {
	Xzyh
	Timestamp uint32
	Index     uint32
	Width     uint16
	Height    uint16
	Format    uint8
}

func (x Xzyh) MarshalBinary() ([]byte, error) {
	if x.Len == 0 {
		x.Len = uint32(len(x.Data))
	}
	if int(x.Len) != len(x.Data) {
		return nil, fmt.Errorf("pppp: xzyh len mismatch header=%d data=%d", x.Len, len(x.Data))
	}
	out := make([]byte, 16+len(x.Data))
	copy(out[0:4], []byte(xzyhMagic))
	binary.LittleEndian.PutUint16(out[4:6], uint16(x.Cmd))
	binary.LittleEndian.PutUint32(out[6:10], x.Len)
	out[10] = x.Unk0
	out[11] = x.Unk1
	out[12] = x.Chan
	out[13] = x.SignCode
	out[14] = x.Unk3
	out[15] = x.DevType
	copy(out[16:], x.Data)
	return out, nil
}

func ParseXzyh(data []byte) (Xzyh, error) {
	if len(data) < 16 {
		return Xzyh{}, fmt.Errorf("pppp: short xzyh frame: %d", len(data))
	}
	if string(data[0:4]) != xzyhMagic {
		return Xzyh{}, fmt.Errorf("pppp: invalid xzyh magic %q", data[:4])
	}
	sz := int(binary.LittleEndian.Uint32(data[6:10]))
	if len(data) < 16+sz {
		return Xzyh{}, fmt.Errorf("pppp: truncated xzyh data: need %d have %d", sz, len(data)-16)
	}
	x := Xzyh{
		Cmd:      P2PCmdType(binary.LittleEndian.Uint16(data[4:6])),
		Len:      uint32(sz),
		Unk0:     data[10],
		Unk1:     data[11],
		Chan:     data[12],
		SignCode: data[13],
		Unk3:     data[14],
		DevType:  data[15],
		Data:     append([]byte(nil), data[16:16+sz]...),
	}
	return x, nil
}

// ParseVideoFrame parses a 64-byte XZYH video frame.
func ParseVideoFrame(data []byte) (VideoFrame, error) {
	if len(data) < 64 {
		return VideoFrame{}, fmt.Errorf("pppp: short video frame: %d", len(data))
	}
	base, err := ParseXzyh(data)
	if err != nil {
		return VideoFrame{}, err
	}
	vf := VideoFrame{
		Xzyh:      base,
		Timestamp: binary.LittleEndian.Uint32(data[16:20]),
		Index:     binary.LittleEndian.Uint32(data[20:24]),
		Width:     binary.LittleEndian.Uint16(data[24:26]),
		Height:    binary.LittleEndian.Uint16(data[26:28]),
		Format:    data[28],
	}
	// Keep full XZYH payload in Data to match Python behaviour.
	// Python ws/video forwards msg.data directly from XZYH (pkt[16:]).
	return vf, nil
}

// Aabb is file-transfer frame header and CRC wrapper.
type Aabb struct {
	FrameType FileTransfer
	SN        uint8
	Pos       uint32
	Len       uint32
}

func (a Aabb) MarshalBinary() ([]byte, error) {
	out := make([]byte, 12)
	binary.LittleEndian.PutUint16(out[0:2], aabbSignature)
	out[2] = byte(a.FrameType)
	out[3] = a.SN
	binary.LittleEndian.PutUint32(out[4:8], a.Pos)
	binary.LittleEndian.PutUint32(out[8:12], a.Len)
	return out, nil
}

func ParseAabb(data []byte) (Aabb, error) {
	if len(data) < 12 {
		return Aabb{}, fmt.Errorf("pppp: short aabb header: %d", len(data))
	}
	if binary.LittleEndian.Uint16(data[0:2]) != aabbSignature {
		return Aabb{}, fmt.Errorf("pppp: invalid aabb signature: %x", data[0:2])
	}
	return Aabb{
		FrameType: FileTransfer(data[2]),
		SN:        data[3],
		Pos:       binary.LittleEndian.Uint32(data[4:8]),
		Len:       binary.LittleEndian.Uint32(data[8:12]),
	}, nil
}

func (a Aabb) PackWithCRC(data []byte) ([]byte, error) {
	a.Len = uint32(len(data))
	head, err := a.MarshalBinary()
	if err != nil {
		return nil, err
	}
	crc := ppcsCRC16(append(head[2:], data...))
	out := make([]byte, 0, len(head)+len(data)+2)
	out = append(out, head...)
	out = append(out, data...)
	out = append(out, crc...)
	return out, nil
}

func ParseAabbWithCRC(raw []byte) (Aabb, []byte, error) {
	if len(raw) < 14 {
		return Aabb{}, nil, fmt.Errorf("pppp: short aabb frame: %d", len(raw))
	}
	a, err := ParseAabb(raw[:12])
	if err != nil {
		return Aabb{}, nil, err
	}
	need := 12 + int(a.Len) + 2
	if len(raw) < need {
		return Aabb{}, nil, fmt.Errorf("pppp: truncated aabb body: need %d have %d", need, len(raw))
	}
	payload := append([]byte(nil), raw[12:12+int(a.Len)]...)
	crcWire := raw[12+int(a.Len) : need]
	crcCalc := ppcsCRC16(append(raw[2:12], payload...))
	if crcWire[0] != crcCalc[0] || crcWire[1] != crcCalc[1] {
		return Aabb{}, nil, fmt.Errorf("pppp: aabb crc mismatch: got %02x%02x want %02x%02x", crcWire[0], crcWire[1], crcCalc[0], crcCalc[1])
	}
	return a, payload, nil
}

func emptyPayload(payload []byte) error {
	if len(payload) != 0 {
		return fmt.Errorf("pppp: expected empty payload, got %d", len(payload))
	}
	return nil
}

func ppcsCRC16(data []byte) []byte {
	crc := uint16(0x0000)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	out := make([]byte, 2)
	binary.LittleEndian.PutUint16(out, crc)
	return out
}
