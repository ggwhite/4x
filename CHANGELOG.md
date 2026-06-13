# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-06-13

First public release.

### Core

- **Four-role AI development loop** — Designer, Coder, Reviewer, Tester with isolated context windows
- **Deterministic guardrails** — scope lock, baseline snapshots, state machine, evidence requirements (enforced by CLI, not AI)
- **File-based protocol** (`.4x/` directory) — crash-resistant, LLM-agnostic inter-role communication
- **State machine** — `init → designing → coding → reviewing → testing → accepting → done` with `blocked` / `needs-attention` escape states
- **Pending-review gate** — human always reviews before marking done

### CLI

- `4x init` — initialize project with runner configuration
- `4x new` — create features with YAML spec
- `4x run` — execute the full Design-Code-Review-Test loop
- `4x status` — view feature status with detail levels
- `4x done` — mark features complete with auto-merge from worktree
- `4x merge` — resolve worktree merge conflicts
- `4x batch plan/run/stop` — dependency-aware DAG scheduling
- `4x live` — real-time SSE dashboard with REST API
- `4x config` — manage settings
- `4x check` — run guardrail checks
- `4x transition` / `4x event` — manual state and event management
- `4x prompt` — generate role prompts for manual use
- `4x mcp` — Model Context Protocol server
- `4x upgrade` — upgrade `.4x/` directory to latest schema

### Runners

- Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor plugins embedded in binary
- Per-role model configuration with tier-based resolution (`opus` / `sonnet` / `haiku`)
- PTY mode, stream-json output format, quiet mode, stdin prompt injection

### Dashboard (4x Live)

- Real-time event streaming via SSE
- Multi-project management with LRU recent projects
- Feature overview with phase timeline
- Log viewer with stream-json support
- Settings editor
- Control panel — run, stop, new feature, mark done
- i18n support — English, 繁體中文, 简体中文, 日本語, 한국어, Español

### Distribution

- Cross-platform binaries — macOS, Linux, Windows (amd64 + arm64)
- Homebrew tap — `brew install ggwhite/tap/4x`
- Go install — `go install github.com/ggwhite/4x/cmd/4x@latest`
- GitHub Releases with checksums

[0.1.0]: https://github.com/ggwhite/4x/releases/tag/v0.1.0
