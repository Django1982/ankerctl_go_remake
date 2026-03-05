# Review Report

Use this template when Claude reviews a Codex completion report.
Save alongside the completion report:
`docs/agents/reports/<same-date>-<phase>-review.md`

---

```
REVIEW REPORT
REVIEWER: claude-code
COMPLETION REPORT: <path to completion report>
DATE: <YYYY-MM-DD>
VERDICT: approved | approved-with-fixes | needs-rework

## Findings
<!-- Each finding: severity, location, description, fix applied or required -->

### KRITISCH (blocking — must fix before merge)
- [ ] <file>:<line> — <description>

### HOCH (should fix)
- [ ] <file>:<line> — <description>

### MITTEL (fix or document)
- [ ] <file>:<line> — <description>

### NIEDRIG (nice to have)
- [ ] <file>:<line> — <description>

## Fixes Applied
<!-- Fixes that Claude applied directly -->
- <file>: <what was changed>

## Fixes Required from Codex
<!-- Fixes that Codex must implement in the next iteration -->
- <description of what to fix and why>

## Python-Compliance
<!-- Does the implementation match the Python reference? -->
- auth order: ✅/❌
- protocol constants: ✅/❌
- security headers: ✅/❌
- edge cases: ✅/❌

## Next Steps
<!-- What should happen after this review -->
- [ ] <action> — <owner: claude|codex|human>
```

---

## Compact Feedback Format (for Codex iteration)

When sending findings back to Codex for fixes, use this shorter format
as the `--task` argument to `agent-context.sh`:

```
REVIEW FEEDBACK for <phase>
VERDICT: needs-rework

FIX REQUIRED:
1. [HOCH] server.go:134 — httpServer written without mutex lock.
   Use local var hs, then s.mu.Lock(); s.httpServer = hs; s.mu.Unlock()

2. [MITTEL] security.go — Remove X-XSS-Protection, not in Python reference.

PRESERVE:
- auth order in auth.go is correct, do not change
- session HMAC implementation is correct
```
