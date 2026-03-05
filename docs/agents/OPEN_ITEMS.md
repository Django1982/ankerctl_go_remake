# Open Items Tracker (Single Source of Truth)

Purpose: track unresolved migration work in one place without duplicating details.
Detailed context stays in the linked source report/file.

## Rules

- Keep this file short and current.
- For each item, store only status/owner/target and a source link.
- Do not duplicate long explanations from reports.

## Items

| ID | Phase | Status | Owner | Target | Source of truth |
|---|---|---|---|---|---|
| OI-013-LOGIN | 13 | open | unassigned | Wire cloud login flow into `/api/ankerctl/config/login` | [`internal/web/handler/config.go:46`](/data_hdd/ankerctl_go_remake/internal/web/handler/config.go#L46), [`2026-03-05-phase10-codex.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase10-codex.md) |
| OI-013-BEDGRID-LIVE | 13 | open | unassigned | Implement live bed-level query/parsing | [`internal/web/handler/bedlevel.go:7`](/data_hdd/ankerctl_go_remake/internal/web/handler/bedlevel.go#L7), [`2026-03-05-phase10-review.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase10-review.md) |
| OI-013-BEDGRID-LAST | 13 | open | unassigned | Implement persisted last bed-level grid endpoint | [`internal/web/handler/bedlevel.go:13`](/data_hdd/ankerctl_go_remake/internal/web/handler/bedlevel.go#L13), [`2026-03-05-phase10-review.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase10-review.md) |
| OI-007-BROADCAST | 7 | done | go-migration-architect | Fixed: `syscall.SetsockoptInt` via `SyscallConn().Control()` in pppp/client | [`2026-03-05-phase6-7-review.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase6-7-review.md) |
| OI-007-PKT-TYPING | 7 | open | unassigned | Decide coverage for additional typed PPPP packet variants | [`2026-03-05-phase7-codex.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase7-codex.md) |
| OI-008-CLOSE | 7 | open | unassigned | `process()` has no `case protocol.Close` — state not set to Disconnected on clean close | [`2026-03-05-phase6-7-review.md`](/data_hdd/ankerctl_go_remake/docs/agents/reports/2026-03-05-phase6-7-review.md) |

