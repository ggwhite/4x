# 4x — Agent Instructions

## Build & Verify

```bash
make build          # compile CLI to bin/4x
make test           # go test ./...
make lint           # go vet ./...
```

All three must pass before claiming work is done.

## Architecture

- `cmd/4x/` — CLI commands (Cobra). One file per subcommand.
- `internal/protocol/` — File protocol types and workspace I/O.
- `internal/state/` — State machine transitions.
- `internal/guard/` — Guardrail checks (scope, baseline, required files).
- `internal/batch/` — Batch scheduling with dependency DAG.
- `internal/server/` — WebSocket server for dashboard.
- `plugins/` — Runner plugins (file-based protocol, no SDK).
- `docs/design.md` — Authoritative design spec. Implementation must match.

## Rules

- CLI must be deterministic: no LLM calls, no network calls in core.
- Go 1.26+, follow gofmt and go vet.
- Tests use standard `testing` package, same directory as source.
- Do not modify files outside declared feature scope during a 4x run.

## Key Files

| File | Purpose |
|---|---|
| `internal/protocol/types.go` | All data types (Phase, Role, State, Event, Config, Feature) |
| `internal/protocol/workspace.go` | .4x/ directory operations |
| `internal/state/machine.go` | Valid phase transitions |
| `internal/guard/check.go` | Guardrail enforcement |
| `docs/design.md` | Full design specification |

## Status

See `progress.md` for current progress and `feature_list.json` for the feature backlog.
