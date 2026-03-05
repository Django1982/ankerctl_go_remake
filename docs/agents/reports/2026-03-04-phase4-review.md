REVIEW REPORT
REVIEWER: claude-code
COMPLETION REPORT: (Codex, 2026-03-04, Phase 4 HTTP Middleware)
DATE: 2026-03-04
VERDICT: approved-with-fixes

## Findings

### HOCH (behoben)
- [x] server.go:134 — httpServer ohne Mutex zwischen Start/Shutdown — Race Condition unter -race
      FIX: lokale Variable hs, dann s.mu.Lock(); s.httpServer = hs; s.mu.Unlock()

### MITTEL (behoben)
- [x] server.go:222 — PRINTER_INDEX: os.LookupEnv und envInt separat aufgerufen →
      printerIndexLocked=true auch bei ungültigem Wert
      FIX: printerIndexLocked direkt aus bool-Rückgabe von envInt()
- [x] security.go — X-XSS-Protection nicht in Python-Referenz (deprecated 2019)
      FIX: Header entfernt, Test prüft explizit Abwesenheit
- [ ] routes.go — /video braucht eigene Inline-Auth (kein Redirect!) in Phase 6
      FIX: TODO(phase-6)-Kommentar ergänzt — Codex muss das in Phase 6 beachten

### NIEDRIG (dokumentiert, kein Fix)
- [ ] ratelimit.go — Einträge nur on-request bereinigt, kein Background-Ticker
      Akzeptabel für Heimnetzwerk/localhost-Nutzungsprofil

## Fixes Applied (by Claude)
- server.go: httpServer mutex fix + PRINTER_INDEX fix
- middleware/security.go: X-XSS-Protection entfernt
- middleware/security_test.go: Test für Abwesenheit des Headers
- routes.go: TODO(phase-6) Kommentar für /video inline auth

## Python-Compliance
- auth order (8 steps): ✅
- protectedGETPaths (8 entries incl. /api/printers, /api/history): ✅
- /api/debug/* prefix match: ✅
- setup path exemption: ✅
- security headers (4 headers): ✅ (nach Fix)
- session cookies (HttpOnly, SameSite=Strict): ✅
- middleware order: ✅

## Next Steps
- [ ] Phase 5 (SQLite DB Layer) — owner: codex via agent-context.sh
- [ ] Phase 6: /video inline auth implementieren — owner: codex (TODO-Marker vorhanden)
