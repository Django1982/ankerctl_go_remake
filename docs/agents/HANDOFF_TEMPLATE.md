# HANDOFF Template

Use this format when delegating work between specialized agents.

```text
HANDOFF to <agent-name>
TASK: <one-sentence task>
CONTEXT: <what already exists and what to preserve>
EXPECTED OUTPUT: <files, report, tests, or decisions required>
CONSTRAINTS: <compatibility, security, performance, deadlines>
BLOCKERS: <missing inputs or dependencies>
PRIORITY: high|medium|low
```

## Example

```text
HANDOFF to go-migration-architect
TASK: Implement Phase 4 auth middleware in Go.
CONTEXT: Use docs/architecture/phase4-plan.md; keep Python auth order.
EXPECTED OUTPUT: auth.go + auth_test.go with full test matrix.
CONSTRAINTS: No new dependencies, preserve protected GET behavior.
BLOCKERS: Go toolchain currently unavailable for local test execution.
PRIORITY: high
```
