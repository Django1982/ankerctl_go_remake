COMPLETION REPORT
AGENT: codex
TASK: Implement Phase 7 PPPP protocol + client (cyclic counters, packet codec, channel reliability, LAN discovery)
DATE: 2026-03-05
STATUS: partial

## Files Changed
- created: internal/pppp/protocol/cyclic.go
- created: internal/pppp/protocol/cyclic_test.go
- created: internal/pppp/protocol/packet.go
- created: internal/pppp/protocol/packet_test.go
- created: internal/pppp/protocol/channel.go
- created: internal/pppp/protocol/channel_test.go
- created: internal/pppp/client/client.go
- created: internal/pppp/client/client_test.go
- created: docs/agents/reports/2026-03-05-phase7-codex.md

## Decisions Made
- Kept message framing identical to Python (`0xF1` magic + type + big-endian payload length).
- Implemented required packet set with explicit marshal/unmarshal plus `DecodePacket` dispatch.
- Implemented `Host` exactly with little-endian port and reversed IPv4 byte order as in Python `amtypes.IPv4`.
- Added `Result` as 32-bit enum with all values from Python `pppp.py`.
- Implemented `Channel` with `max_in_flight=64`, 1024-byte chunking, 500ms retransmit default, reorder queue, and ACK progression.
- Added `Wire` abstraction for channel byte streaming with timeout-capable peek/read.
- Implemented client run loop with UDP recv + per-channel retransmit polling and basic handshake processing.
- Implemented LAN discovery helper based on `LAN_SEARCH` send and `PUNCH_PKT` matching by DUID.
- Used mock UDP connection in tests to avoid sandbox socket restrictions.

## Deviations from Spec
- `go build ./...` could not complete in this sandbox because outbound network access to `proxy.golang.org` is blocked for module downloads.
- Live UDP integration tests were replaced with mock-connection unit tests due sandbox denial on opening UDP sockets.

## Known Issues / TODO
- `OpenBroadcast` cannot explicitly set `SO_BROADCAST` via stdlib method on current Go version; if required, add socket control hook for broadcast option.
- Only the packet types requested for this phase are fully typed; unsupported message types currently decode to raw `Message`.

## Open Questions for Review
- Should additional PPPP packet types (`DEV_LGN_CRC`, `REPORT_SESSION_READY`, relay packets) be fully typed now or deferred to service integration phases?
- Is the expected naming for `ListenResp` acceptable as alias to Python `LIST_REQ_ACK` semantics?

## Test Coverage
- covered: CyclicU16 wrap comparisons and diff semantics; packet marshal/unmarshal roundtrips (DRW, DRW_ACK, HelloAck, P2P_RDY, LIST_REQ_ACK, XZYH, AABB+CRC); channel reorder/ack/window/retransmit; client LAN discovery logic with DUID match and timeout.
- not covered: real network UDP socket behavior, multi-peer LAN broadcast behavior, full PPPP handshake against a live device.
