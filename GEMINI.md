# 4x — Multi-Role AI Development Loop (Gemini CLI Developer Guide)

This guide is for developers using the Google Gemini CLI to build and maintain the 4x codebase.

## Quick Start

```bash
make build          # Compile CLI to bin/4x
make test           # Run standard tests (go test ./...)
make lint           # Run linter (go vet ./...)
```

## Architecture

Three-layer architecture (No LLM calls are allowed in the CLI layer):

```
cmd/4x/             CLI subcommands (Cobra) - one file per command
internal/
  protocol/          .4x/ file structure, Workspace IO, types
  state/             State machine (phase transitions)
  guard/             Guardrail checking (scope, baseline, required files)
  batch/             Batch DAG scheduling with dependency planning
  server/            WebSocket & SSE server for Live dashboard
plugins/
  claude-code/       Claude Code skill and workflow.js
  gemini/            Gemini CLI instructions (GEMINI.md) and runner readme
  codex/             Codex CLI instructions (AGENTS.md) and runner readme
  copilot/           Copilot CLI instructions (AGENTS.md) and workflow.js
  cursor/            Cursor rules (.cursorrules) and runner readme
dashboard/
  macos/             Swift native application
  electron/          Windows/Linux desktop application
  web/               Browser-based dashboard UI
schemas/             JSON Schema definitions for state/event/feature
templates/           Role prompt templates (.md.tmpl)
docs/                Authoritative design specifications
```

## State Machine

```
init → designing → coding → reviewing → testing → accepting → done
                     ↑          ↓           ↓
                     └── amending ←─────────┘
any → blocked / needs-attention
```

Valid phase transitions are defined in `internal/state/machine.go`.

## Protocol (.4x/ directory)

Roles communicate asynchronously via files in the `.4x/` directory.
Schemas are defined in `internal/protocol/types.go`, constants in `internal/protocol/workspace.go`.

## Development Rules

- **Language & Style**: Go 1.26+, follow standard `gofmt` and `go vet` rules.
- **CLI Commands**: CLI entry points are in `cmd/4x/`. Each command must reside in its own file (e.g., `cmd/4x/init.go`).
- **No LLM in Core**: The CLI must remain deterministic. No LLM calls or network requests are allowed in `cmd/` or `internal/` (except `internal/server/`).
- **Specs First**: Design specs are authoritative (located in `docs/`). When implementation and spec conflict, match the spec.
- **Testing**: Use standard `testing` package. Test files must reside in the same directory as the files they test.

## Verification

Before claiming work is done or submitting a pull request, run the following verification pipeline:

```bash
make build && make lint && make test
```

All three checks must pass.
