# Migration Status

This page tracks the progress of the Python-to-Go migration for ankerctl.

**Started:** 2026-03-03
**Target:** 1:1 feature parity with the Python original
**Current progress:** 100% — v0.9.24 released

---

## Phase Overview

| # | Phase | Status | Notes |
|---|-------|--------|-------|
| 1 | Project Scaffold | ✅ DONE | Go module, directory structure, placeholder files |
| 2 | Config Parser + Data Models | ✅ DONE | JSON config, custom UnmarshalJSON for `__type__` |
| 3 | Crypto Layer | ✅ DONE | AES-256-CBC, ECDH, XOR checksums, PPPP curse/decurse |
| 4 | Middleware + HTTP Server | ✅ DONE | chi/v5, full security middleware stack |
| 5 | SQLite DB Layer | ✅ DONE | Print history + filament profiles, parameterized queries |
| 6 | MQTT Protocol + Client | ✅ DONE | 39 message types, encrypted communication |
| 7 | PPPP Protocol + Client | ✅ DONE | UDP P2P, 8 channels, DRW pipelining |
| 8 | Service Framework | ✅ DONE | Lifecycle, ServiceManager, ref-counting |
| 9 | Web Services | ✅ DONE | All 8 services implemented |
| 10 | HTTP API Handlers | ✅ DONE | 40+ REST endpoints |
| 11 | WebSocket Handlers | ✅ DONE | 5 WebSocket streams |
| 12 | Notifications + GCode Utils | ✅ DONE | Apprise client, GCode patching |
| 13 | Anker Cloud HTTP API | ✅ DONE | Login, device query, region detection |
| 14 | Frontend + Templates | ✅ DONE | Jinja2-to-Go template conversion, `//go:embed` |
| 15 | CLI Commands | ✅ DONE | cobra CLI: config, mqtt, pppp, http, webserver |
| 16 | Docker + CI | ✅ DONE | Multi-arch build, health check, CI pipeline |

---

## Recent Activity (March 2026)

Key commits from the last development cycle:

- `fix(web): remove dead []byte case in Video tap callback` (N-004)
- `fix(service): extract typed VideoQueue interfaces` (H-004)
- `fix(pppp): replace sync.Cond with channel-based Wire signaling` (M-003)
- `fix(docker): upgrade builder image to golang:1.25-alpine`
- `fix(web): redact email in config show output` (N-001)
- `test(service): expand bed leveling test coverage`
- `test(ws): add pppp-state probe state machine tests`
- `test(pppp): add upload integration test`
- `feat(web): add graceful shutdown API endpoint`
- `fix(handler): reject PrintersSwitch to unsupported device models`
- `fix(middleware): block WebSocket paths for unsupported devices`

---

## Closed Items

All known gaps are resolved as of v0.9.24.

### Recently Closed

| ID | Description | Resolution |
|----|-------------|------------|
| N-001 | Redact email PII in config output | Fixed |
| N-004 | Remove dead `[]byte` case in Video tap | Fixed |
| H-004 | Extract typed VideoQueue interfaces | Fixed |
| M-003 | Replace sync.Cond with channel-based Wire signaling | Fixed |
| H-002 | PPPP state probe/retry logic | Already implemented (confirmed in QA) |

### Test Coverage Added

| Area | Description |
|------|-------------|
| PPPP upload integration | End-to-end file transfer test |
| `discoverAndPersistPrinterIPs` | Background goroutine test |
| pppp-state probe | State machine tests (8 scenarios) |
| Bed leveling | Comprehensive test coverage (Phase 3 + 4 merge) |

---

## QA Reports

Detailed QA reports are stored in `docs/agents/reports/`:

| Date | Report | Focus |
|------|--------|-------|
| 2026-03-04 | Phase 4 Review | Middleware stack |
| 2026-03-05 | Phase 6-12 Reviews | Protocol, services, handlers, WebSocket |
| 2026-03-05 | Security Consistency Audit | Cross-cutting security review |
| 2026-03-09 | QA Review | Full-project quality assessment |
| 2026-03-10 | QA Follow-up | Bug fixes and open items |
| 2026-03-23 | Graceful Shutdown Review | Shutdown API endpoint review |
| 2026-03-25 | Final Gaps Plan | Remaining items to 100% parity |

---

## Risk Register

| Risk | Severity | Mitigation |
|------|----------|------------|
| PPPP UDP asymmetry complex to port | High | Incremental testing, channel-based Wire signaling |
| ECDH login password encryption | Medium | Go stdlib crypto/ecdh; verified curve params |
| ffmpeg subprocess dependency | Low | Same approach as Python (exec.Command) |
| WebSocket streaming semantics | Medium | gorilla/websocket; notify pattern with channels |
| Service lifecycle (ref-counting) | High | Carefully designed ServiceManager with sync patterns |
| Python `__type__` JSON polymorphism | Medium | Custom UnmarshalJSON in Go |
