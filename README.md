```
    ___  __  __
   / _ | \ \/ /
  / __ |  \  /     Design. Code. Review. Test.
 /_/ |_|  /_/      The AI development loop.
```

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

**4x is a multi-role AI development framework that splits the software engineering loop into four specialized phases** — Design, Code, Review, Test — each driven by a dedicated AI agent. Like 4X strategy games (eXplore, eXpand, eXploit, eXterminate), the name reflects a system where distinct roles with distinct strengths converge to conquer complexity. Born from a production system that shipped 60+ features during a large-scale platform rewrite, 4x brings structure, safety, and observability to AI-assisted development.

---

## Why 4x?

Single-agent coding is fast but fragile. You ask one AI to design, implement, review, and test — all in the same breath, with the same biases. It works for small tasks. It falls apart on real features.

4x splits the loop. Each role has a focused job, limited scope, and no access to the others' reasoning. The Designer doesn't write code. The Coder doesn't judge its own work. The Reviewer is adversarial by design. The Tester validates against criteria written before implementation.

The result: features that survive contact with production.

## Architecture

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Plugins                                         |
|  Claude Code skill | Copilot ext | Cursor rules  |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  macOS native | Windows Electron | Web           |
|  Real-time monitoring of agent work              |
+--------------------------------------------------+
```

**Layer 1 — CLI** handles everything deterministic: scope validation, state transitions, baseline snapshots, evidence collection. It never calls an LLM. This means guardrails don't depend on AI judgment.

**Layer 2 — Plugins** bridge the CLI protocol to your AI tool of choice. A Claude Code skill, a Copilot extension, Cursor rules — each speaks the same `.4x/` file protocol but uses native platform capabilities.

**Layer 3 — Live** is the dashboard. Watch your AI agents work in real-time, see phase transitions, catch problems early. macOS native app, Windows Electron, or web UI.

## ⚠️ Permission Model

**4x runs AI agents in non-interactive (yolo) mode.** During `4x init`, all configured runners are set up to skip permission prompts so the Design→Code→Review→Test loop can run autonomously without human intervention:

| Runner | Flag / Mechanism |
|--------|-----------------|
| Claude Code | `--dangerously-skip-permissions` + `.claude/settings.json` allowlist |
| Codex | `codex.json` with `approval: full-auto` |
| Gemini | `-y` flag + `.gemini/settings.json` sandbox |
| Antigravity | `--dangerously-skip-permissions` + `.gemini/settings.json` sandbox |

This means agents can read, write, and execute commands within your project **without asking for confirmation**. The CLI's deterministic guardrails (scope lock, baseline snapshots, state machine) provide the safety boundary — not LLM-level permission prompts.

**Run 4x only in projects and environments where you are comfortable with autonomous AI agent execution.** Review the generated permission files (`.claude/settings.json`, `.gemini/settings.json`, `codex.json`) after `4x init` and adjust allowlists as needed.

## Quick Start

```bash
# Install
go install github.com/ggwhite/4x/cmd/4x@latest

# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created: auth-001

# Run the full loop with Claude
4x run auth-001 --runner claude

# Or watch it happen live
4x live
```

That's it. `4x run` drives the Design-Code-Review-Test loop automatically. If Review finds issues, Code gets another pass. If Test fails, the loop iterates. You stay in control with `--max-rounds` and `--pause-on` flags.

## The Four Roles

| Role | Job | Inputs | Outputs |
|---|---|---|---|
| **Designer** | Analyze requirements, produce spec + acceptance criteria + test strategy | Feature description, legacy code analysis | `.4x/design.md`, `.4x/acceptance.md` |
| **Coder** | Implement exactly what the spec says, nothing more | Design spec, scope boundary | Source code changes, `.4x/commit-plan.md` |
| **Reviewer** | Catch bugs, security issues, spec violations (checklist + adversarial) | Diff, design spec, hard rules | `.4x/review.md` with severity ratings |
| **Tester** | Validate against acceptance criteria with real evidence | Acceptance criteria, running system | `.4x/test-evidence.md`, pass/fail verdict |

Each role is **isolated**. The Coder never sees the Reviewer's prior feedback during implementation. The Tester validates against criteria written by the Designer, not the Coder. This separation prevents the blind spots that plague single-agent workflows.

## How the Loop Works

```
   +-----------+
   | Designer  |-----> Design spec + acceptance criteria
   +-----------+
        |
        v
   +-----------+
   |   Coder   |-----> Implementation + commit plan
   +-----------+
        |
        v
   +-----------+       Warnings/errors found?
   | Reviewer  |-----> YES: back to Coder (with findings)
   +-----------+       NO: proceed
        |
        v
   +-----------+       Tests fail?
   |  Tester   |-----> YES: back to Coder (with evidence)
   +-----------+       NO: done
        |
        v
     Feature
     Complete
```

### What happens at each phase:

**Design** — The Designer reads your feature description, analyzes relevant existing code, and produces a spec. It defines acceptance criteria *before* any code is written. It identifies which files/modules are in scope and which are off-limits.

**Code** — The Coder receives the spec and a strict scope boundary. It implements the feature and produces a commit plan. It cannot modify files outside scope. It cannot skip acceptance criteria.

**Review** — The Reviewer gets the diff and the spec. It runs through a checklist (security, concurrency, error handling, style) and then does an adversarial pass: "What's the worst bug hiding in this diff?" Findings are rated by severity. Warnings and above go back to the Coder.

**Test** — The Tester validates every acceptance criterion with evidence. Not "I believe this works" but "here is the command output proving it works." Failed criteria send the feature back to Code with specific failure details.

## File-Based Protocol

Roles communicate through the `.4x/` directory, not shared context windows. This is what makes 4x LLM-agnostic.

```
.4x/
  feature.json          # Feature metadata + state machine
  design.md             # Designer output: spec + architecture decisions
  acceptance.md         # Testable acceptance criteria
  scope.json            # Allowed files/directories for Coder
  review.md             # Reviewer findings with severity
  test-evidence.md      # Tester output: criterion + evidence pairs
  commit-plan.md        # How to split changes into commits
  baseline/             # Snapshot of affected files before coding
  history/              # Audit trail of all phase transitions
```

Any tool that can read and write files can participate. Claude Code reads `design.md` and writes source code. Copilot reads `review.md` and applies fixes. A shell script could be a Tester. The protocol is the contract.

## Deterministic Guardrails

These don't depend on AI judgment — they're enforced by the CLI:

| Guardrail | What it does |
|---|---|
| **Scope lock** | Coder cannot modify files outside `scope.json`. CLI rejects out-of-scope changes. |
| **Baseline snapshots** | Before coding starts, affected files are snapshotted. Enables safe rollback and diff auditing. |
| **State machine** | Phases must proceed in order. No skipping Review. No jumping from Design to Test. |
| **Evidence requirement** | Tester must provide command output or screenshots, not just assertions. |
| **Severity gate** | Review findings at WARNING or above block progression. Only LOW/INFO can be skipped. |
| **Round budget** | Max iterations per feature. Prevents infinite loops of "fix one bug, introduce another." |

## Batch Mode

Real projects have dozens of features. `4x batch` runs them in dependency order:

```bash
# Run all ready features
4x batch --runner claude

# Run with concurrency (independent features in parallel)
4x batch --runner claude --parallel 3

# Dry run — show execution plan
4x batch --dry-run
```

The batch planner uses a dependency graph to determine which features can run in parallel and which must wait. Features that share files are never scheduled concurrently.

## Plugin Ecosystem

| Plugin | Status | Platform |
|---|---|---|
| Claude Code skill | Available | Claude Code CLI |
| Copilot extension | Planned | GitHub Copilot |
| Cursor rules | Planned | Cursor IDE |
| Codex runner | Planned | OpenAI Codex CLI |
| Gemini adapter | Available | Google Gemini CLI |
| Antigravity adapter | Available | Antigravity CLI |

### Writing a Plugin

Plugins implement a simple interface: read `.4x/` files, do AI work, write results back. See [`plugins/claude-code/`](plugins/claude-code/) for a reference implementation.

```
Plugin Contract:
  1. Read .4x/feature.json to know current phase
  2. Read phase-specific inputs (design.md, scope.json, etc.)
  3. Do the work (call your LLM, run tools, etc.)
  4. Write phase-specific outputs
  5. Call `4x advance` to transition state
```

No SDK required. No runtime dependency. Just files.

## 4x Live Dashboard

> Screenshot placeholder — coming soon

Real-time monitoring of your AI development loop:

- **Phase timeline** — See each role activate, work, and hand off
- **Diff viewer** — Watch code changes appear as the Coder works
- **Review feed** — Findings stream in with severity highlighting
- **Test matrix** — Acceptance criteria light up green/red as Tester validates
- **Batch overview** — Track multiple features across the dependency graph

Available as a macOS native app (Swift), Windows desktop app (Electron), or web UI.

```bash
# Start the dashboard
4x live

# Or connect to a remote session
4x live --connect ws://ci-server:4200
```

## 4x vs Single-Agent Coding

| Scenario | Single Agent | 4x |
|---|---|---|
| Agent implements feature it misunderstands | Ships wrong feature. Discovers in QA. | Designer spec is reviewed before coding starts. |
| Security vulnerability in implementation | Agent might catch it. Might not. | Reviewer runs adversarial security pass on every diff. |
| Agent "tests" by asserting it works | "I verified this works correctly." | Tester must provide command output or it doesn't count. |
| Scope creep — agent refactors unrelated code | Common. Hard to detect in large diffs. | Scope lock rejects changes outside declared boundary. |
| Subtle bug introduced while fixing review feedback | Fix one thing, break another. No one checks. | New code goes through full Review + Test again. |
| Agent hallucinates test results | Claims tests pass without running them. | Evidence requirement: show the terminal output. |
| Feature partially done, session dies | No record of what was completed. | File-based state survives any crash. Pick up exactly where it stopped. |
| 10 features need to ship this sprint | Sequential. Slow. Context window exhaustion. | Batch mode with dependency-aware parallelism. |

## Project Structure

```
4x/
  cmd/4x/              # CLI entry point
  internal/
    guard/              # Scope lock, baseline, evidence validation
    protocol/           # .4x/ file format, state machine
    runner/             # Plugin runner interface
    batch/              # Dependency graph, parallel scheduler
    server/             # SSE + REST server for Live dashboard
    state/              # Feature state management
  plugins/
    claude-code/        # Reference plugin: Claude Code skill
    gemini/             # Gemini CLI runner
    codex/              # Codex CLI runner
    agy/                # Antigravity CLI runner
    copilot/            # Copilot CLI runner
    cursor/             # Cursor rules
    embed.go            # go:embed plugin files into binary
  dashboard/
    macos/              # Swift native app
    electron/           # Windows/Linux desktop
    web/                # Browser-based UI
  schemas/              # JSON schemas for .4x/ files
  templates/            # Default role prompt templates
  docs/                 # Documentation
  examples/
    todo-api/           # Example project with 4x configured
```

## Contributing

Contributions welcome. The best way to start:

1. **Try it** on a real project and file issues for rough edges
2. **Write a plugin** for your favorite AI tool
3. **Improve guardrails** — propose new deterministic checks

```bash
# Clone and build
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x

# Run tests
go test ./...
```

Please read [CONTRIBUTING.md](docs/CONTRIBUTING.md) before submitting a PR.

## FAQ

**Q: Does 4x call any LLM APIs directly?**
No. The CLI is pure Go with zero LLM dependencies. Plugins handle all AI interaction using their native platform capabilities.

**Q: Can I use different LLMs for different roles?**
Yes. That's the point of the file-based protocol. Use Claude for Design, Copilot for Code, Gemini for Review — each reads the same `.4x/` files.

**Q: What if I only want Review + Test, not the full loop?**
Use `4x run --start-from review` to skip Design and Code phases. Bring your own implementation, let 4x review and test it.

**Q: How is this different from Devin / SWE-agent / OpenHands?**
Those are autonomous agents that do everything in one shot. 4x is a *framework* that structures multi-role collaboration with deterministic guardrails. It's closer to a CI pipeline for AI than a single autonomous agent.

## Origin Story

4x was born inside a production system called DCT (Designer-Coder-Tester) that shipped 60+ features for a large-scale platform rewrite. The patterns that survived — role isolation, file-based protocol, deterministic scope checking, evidence-based testing — became 4x. The parts that didn't survive — LLM-specific hacks, shared context assumptions, trust-based guardrails — were deliberately left out.

## License

[MIT](LICENSE)

---

<p align="center">
  <strong>Stop hoping your AI writes correct code. Start verifying it.</strong>
</p>
