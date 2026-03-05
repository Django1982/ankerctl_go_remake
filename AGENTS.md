# Repository Guidelines

## Project Structure & Module Organization
`ankerctl` is a Go 1.22 monorepo. Keep entrypoints in `cmd/ankerctl/` and implementation in `internal/`.

- `cmd/ankerctl/main.go`: CLI/webserver bootstrap.
- `internal/`: core packages (`config`, `model`, `crypto`, `mqtt`, `pppp`, `httpapi`, `service`, `web`, `notifications`, `gcode`, `util`, `logging`).
- `static/`: frontend assets served by the web layer.
- `docs/`: architecture, protocol notes, and development docs.

Follow the layering documented in `CLAUDE.md`; do not introduce upward imports across internal package layers.

## Build, Test, and Development Commands
Use these from repository root:

- `go build -o ankerctl ./cmd/ankerctl/`: build local binary.
- `go test ./...`: run all unit tests.
- `go test ./internal/config/...`: run package-scoped tests.
- `go test -run TestConfigLoad ./internal/config/`: run a single test.
- `go test -v -cover ./internal/model/...`: verbose run with coverage.
- `go vet ./...`: static checks for common Go issues.

## Coding Style & Naming Conventions
- Format all Go code with `gofmt` before commit.
- Use standard Go naming: exported `PascalCase`, unexported `camelCase`, package names short/lowercase.
- Keep files focused by feature (`manager.go`, `manager_test.go`).
- Prefer explicit error wrapping (`fmt.Errorf("...: %w", err)`) and avoid panics in production paths.
- Preserve protocol constants and crypto behavior exactly where documented.

## Testing Guidelines
- Write table-driven tests for protocol/model/config behavior.
- Co-locate tests with code using `*_test.go`.
- Name tests clearly (`TestXxx`, `TestXxx_Condition`).
- For new logic, include positive, negative, and edge-case coverage.

## Commit & Pull Request Guidelines
Git history follows a strict phase-based and task-based structure:

- **Branching (MANDATORY):** Never work on `main`. Create a feature branch: `git checkout -b <branch-name>`.
- **Atomic Commits:** One logical change per commit.
- **Commit subjects:** imperative, concise, <= 72 chars (example: `web: enforce API key on debug routes`).
- **Merging:** Merge into `main` only after full test verification (`go test ./...`).
- **Hook enforcement:** Run `sh scripts/install-hooks.sh` once after cloning. Installs a `pre-commit` hook that hard-blocks direct commits to `main`.

## Security & Configuration Notes
- Never log secrets (`auth_token`, `mqtt_key`, `api_key`).
- Keep config handling compatible with existing defaults and permissions (config dir `0700`).
- Validate user input for file paths and web/API parameters.
