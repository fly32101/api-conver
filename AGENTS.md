# Repository Guidelines

## Project Structure & Module Organization
This is a Go service following a DDD-style layout.
- `cmd/server/main.go` is the entrypoint.
- `internal/config/` loads YAML config.
- `internal/domain/` holds models and protocol conversion logic.
- `internal/application/usecase/` orchestrates use cases.
- `internal/infrastructure/` contains upstream HTTP client and config repository.
- `internal/interface/` defines HTTP handlers and Gin routes.
Configuration files live at `config.yaml` and `.env` (examples in `config.yaml.example` and `.env.example`).

## Build, Test, and Development Commands
- `go run ./cmd/server` starts the API locally.
- `go build -o server ./cmd/server` builds a binary for the current platform.
- `go test ./...` runs all Go tests (no dedicated test suite is present yet).

## Coding Style & Naming Conventions
- Use standard Go formatting; run `gofmt` on all Go files.
- Follow Go naming: `CamelCase` for exported symbols, `camelCase` for local variables.
- Keep package names short and lowercase, matching directory names.

## Testing Guidelines
No testing framework is configured beyond the Go toolchain. If you add tests:
- Place them next to the code under `internal/` using `*_test.go`.
- Name tests with `TestXxx` and table-test style where useful.
- Run `go test ./...` before opening a PR.

## Commit & Pull Request Guidelines
Recent commit history uses short, informal messages (e.g., `init`, `new`), so no strict convention is enforced. Prefer concise, imperative summaries like `add config loader` and include details in the body when needed.
For PRs, include:
- A clear description of behavior changes and any new routes.
- Config changes (e.g., `config.yaml` keys) and migration notes.
- Example requests (`curl`) when API behavior changes.

## Security & Configuration Tips
- Prefer `config.yaml` with aliases for upstreams; environment variables are a fallback.
- Avoid committing real API keys; use `.env.example` for documentation.
