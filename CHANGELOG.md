# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-06-15

First public release.

### Core

- **Multi-role AI development loop** — Designer, Coder, Reviewer, Tester, Deep Reviewer, Acceptor with isolated context windows
- **Deterministic guardrails** — scope lock, baseline snapshots, state machine, evidence requirements (enforced by CLI, not AI)
- **File-based protocol** (`.4x/` directory) — crash-resistant, LLM-agnostic inter-role communication
- **State machine** — `init → designing → coding → reviewing → testing → deep-reviewing → accepting → done` with `blocked` / `needs-attention` escape states and `amending` re-entry
- **Pending-review gate** — human always reviews before marking done
- **Phase hooks** — pre/post shell commands on phase transitions
- **Health check** — auto-verify environment before testing phase
- **Adaptive pipeline** — profile-based role selection by feature complexity

### CLI

- `4x init` — initialize project with runner configuration
- `4x new` — create features with subtask dependencies, priority, and rules
- `4x run` — execute the full Design-Code-Review-Test loop with deep review
- `4x status` — view feature status with detail levels
- `4x done` — mark features complete with auto-merge from worktree
- `4x merge` — multi-repo squash merge with rollback
- `4x batch plan/run/next/stop` — dependency-aware DAG scheduling with auto-merge and reports
- `4x live` — real-time SSE multi-project dashboard
- `4x config` — manage settings
- `4x check` — run guardrail checks
- `4x doctor` — universal settings and workspace health check
- `4x clean` — remove workspace artifacts for completed features
- `4x transition` / `4x event` — manual state and event management
- `4x prompt` — generate role prompts for manual use
- `4x subtask` — manage subtask status with validation
- `4x verify` — run verification commands and check evidence
- `4x sync` — re-deploy plugin files after binary update
- `4x mcp` — Model Context Protocol server

### Run Loop

- Automatic phase transitions with configurable max rounds and stop conditions
- Deep review with parallel fan-out (N sub-reviewers + synthesizer)
- Deep review self-healing loop (fix → re-verify cycle)
- Per-round auto-commit with worktree isolation support
- Review verdict parsing with severity counting

### Runners

- 6 runners: Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor — plugins embedded in binary
- Per-role model configuration with abstract tier resolution (`opus` / `sonnet` / `haiku`)
- PTY mode with graceful SIGTERM → SIGKILL shutdown sequence
- Non-PTY process group isolation (no orphaned subprocesses on cancellation)
- Stream JSON mode, stdin mode, argument mode
- Placeholder resolution with fail-loud semantics

### Batch Mode

- Dependency DAG scheduling with cycle detection and topological sort
- Chain scheduling with configurable max chain length
- Auto-merge on feature completion
- Batch report generation on stop/crash
- Orphan process cleanup
- In-progress status tracked as failure to prevent infinite retry loops

### Dashboard (4x Live)

- Real-time event streaming via SSE
- Multi-project management with LRU recent projects and file browser
- Feature overview with design doc resolution
- Log viewer with stream-json support
- Batch control and dependency DAG visualization
- Settings editor with inline config editing
- Control panel — run, stop, new feature, mark done
- API response caching
- Path traversal protection with symlink resolution
- i18n — English, 繁體中文, 简体中文, 日本語, 한국어, Español

### Desktop App

- Tauri-based cross-platform app (macOS, Linux, Windows)
- System tray, app menu, close-to-hide
- Frost theme with menu bar popover

### Documentation

- Full user guide (8 documents) in 6 languages
- Architecture docs (overview, protocol, state machine, cross-platform packaging)
- Design specs and plans for all features
- Plugin contract reference

### Distribution

- Cross-platform binaries — macOS, Linux, Windows (amd64 + arm64)
- Homebrew tap — `brew install ggwhite/tap/4x`
- Go install — `go install github.com/ggwhite/4x/cmd/4x@latest`
- GitHub Releases with checksums

### Quality

- 700 tests across 18 packages (race detector enabled)
- Atomic file writes for concurrent safety (SaveFeature, WriteState, WriteBatchReport)
- Structured logging with rotation and configurable retention

[0.1.0]: https://github.com/ggwhite/4x/releases/tag/v0.1.0
