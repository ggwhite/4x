**English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md)

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

**4x is a multi-role AI development framework that splits the software engineering loop into four specialized phases** — Design, Code, Review, Test — each driven by a dedicated AI agent. Like 4X strategy games (eXplore, eXpand, eXploit, eXterminate), the name reflects a system where distinct roles with distinct strengths converge to conquer complexity.

---

## Why 4x?

Single-agent coding is fast but fragile. You ask one AI to design, implement, review, and test — all in the same breath, with the same biases. It works for small tasks. It falls apart on real features.

4x splits the loop. Each role has a focused job, limited scope, and no access to the others' reasoning. The Designer doesn't write code. The Coder doesn't judge its own work. The Reviewer is adversarial by design. The Tester validates against criteria written before implementation.

The result: features that survive contact with production.

## Trade-offs

Choosing 4x means trading speed and cost for structure and correctness. Be honest about whether your project needs that trade.

### Strengths

- **Role isolation eliminates self-review bias.** The Coder never judges its own work. The Reviewer is adversarial by design. Single-agent workflows let the same model write and approve code — 4x doesn't.
- **Deterministic guardrails don't depend on AI judgment.** Scope lock, state machine, evidence requirements — these are enforced by the CLI in Go, not by prompting an LLM to "please stay in scope."
- **File-based protocol makes it LLM-agnostic.** Switch between Claude, Gemini, Codex, or mix them per role. No vendor lock-in, no SDK dependency.
- **Crash-resistant state.** Everything lives in `.4x/` files. Session dies, machine reboots — `4x run` picks up exactly where it stopped.
- **Human stays in the loop.** The `pending-review` gate ensures a human always reviews AI work before it's marked done. The AI proposes, you dispose.
- **Batch mode scales.** Dependency-aware scheduling lets you queue dozens of features overnight and review them in the morning.

### Weaknesses

- **Significantly higher token cost.** Every feature runs through 4+ separate LLM calls at minimum. A review failure doubles that. Expect 3-10x the token cost of a single-agent approach for the same task. See [Usage Tips](docs/guide/usage-tips.md) for cost estimates.
- **Slower for simple tasks.** A one-line bug fix doesn't need a Designer, Reviewer, and Tester. The overhead of the full loop is wasted on trivial changes. Use single-agent tools for quick fixes.
- **Setup cost.** `4x init`, feature YAML, settings configuration — there's ceremony before you start. Not worth it for a throwaway script.
- **Rigid loop structure.** The Design → Code → Review → Test sequence is fixed. If your workflow doesn't fit four roles, you'll fight the framework instead of using it.
- **Quality depends on prompt quality.** Vague feature descriptions produce vague specs, which produce wrong code. 4x adds structure, but garbage in still means garbage out — just with more steps.

### When to use 4x

- Features that need to be correct (payments, auth, data pipelines)
- Work that benefits from adversarial review (security-sensitive code)
- Batch processing of a feature backlog
- Teams that want audit trails of AI-generated code

### When NOT to use 4x

- Quick one-off fixes or exploratory prototyping
- Tasks where speed matters more than correctness
- Projects where token budget is tight
- Solo hacking sessions where you'd review the code yourself anyway

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
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**Layer 1 — CLI** handles everything deterministic: scope validation, state transitions, baseline snapshots, evidence collection. It never calls an LLM. Guardrails don't depend on AI judgment.

**Layer 2 — Runners** bridge the CLI protocol to your AI tool of choice. Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor — each speaks the same `.4x/` file protocol but uses native platform capabilities.

**Layer 3 — Live** is the multi-project dashboard. Watch your AI agents work in real-time, see phase transitions, stream logs. REST + SSE API.

## Quick Start

```bash
# Install
go install github.com/ggwhite/4x/cmd/4x@latest

# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` drives the Design-Code-Review-Test loop automatically. If Review finds issues, Code gets another pass. If Test fails, the loop iterates. You stay in control with `--max-rounds` and `--timeout` flags.

## The Four Roles

| Role | Job | Outputs |
|---|---|---|
| **Designer** | Analyze requirements, produce spec + acceptance criteria | `task-brief.md`, `acceptance-criteria.md` |
| **Coder** | Implement exactly what the spec says | Source code, `coder-report.md` |
| **Reviewer** | Catch bugs and spec violations (checklist + adversarial) | `review-report.md` with verdict |
| **Tester** | Validate against acceptance criteria with evidence | `test-report.md`, `verify.json` |

Each role is **isolated**. The Coder never sees the Reviewer's prior feedback. The Tester validates against criteria written by the Designer, not the Coder. This separation prevents the blind spots that plague single-agent workflows.

## How the Loop Works

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **Review failure** (verdict FAIL or CRITICAL findings) sends code back for amending
- **Test failure** (verify not passed) sends code back for amending
- **Escalation** (spec mismatch, criteria wrong) routes back to Designer
- **Pending review** gate ensures a human always reviews before marking done
- **Round budget** (default 5) prevents infinite loops

## Deterministic Guardrails

Enforced by the CLI, not AI judgment:

| Guardrail | What it does |
|---|---|
| **Scope check** | Changed files must be within declared repos |
| **Baseline snapshot** | Pre-coding state captured for safe rollback |
| **State machine** | Phases must proceed in legal order |
| **Evidence requirement** | Tester must provide verify.json with command output |
| **Testing gate** | verify.json + test-report + final-report + commit-plan required |
| **Dependency gate** | Features with unmet dependencies cannot start |

## Batch Mode

```bash
4x batch plan            # generate dependency-aware execution plan
4x batch run --runner claude  # run all eligible features in order
4x batch stop            # graceful shutdown after current feature
```

## MCP Server

Start the Model Context Protocol (MCP) server:

```bash
4x mcp
```

## Permission Model

**4x runs AI agents in non-interactive mode.** During `4x init`, runners are configured with flags that skip permission prompts (`--dangerously-skip-permissions`, `-y`, `approval: full-auto`) so the loop runs autonomously.

The CLI's deterministic guardrails (scope lock, baseline snapshots, state machine) provide the safety boundary.

**Run 4x only in projects where you are comfortable with autonomous AI agent execution.**

## Documentation

| Document | Description |
|---|---|
| **[User Guide](docs/guide/)** | Complete usage documentation |
| [Getting Started](docs/guide/getting-started.md) | Installation and first run |
| [CLI Reference](docs/guide/cli.md) | All commands and flags |
| [Core Concepts](docs/guide/concepts.md) | Roles, state machine, protocol, guardrails |
| [Configuration](docs/guide/configuration.md) | Settings, models, locale, runners |
| [Runners & Plugins](docs/guide/runners.md) | Supported runners and plugin contract |
| [Dashboard](docs/guide/dashboard.md) | 4x Live multi-project dashboard |
| [Batch Mode](docs/guide/batch.md) | Dependency-aware batch execution |

## Project Structure

```
4x/
  cmd/4x/              CLI entry point (Cobra)
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    batch/              Dependency DAG, batch scheduler
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
  plugins/
    claude-code/        Claude Code skill + workflow
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions + workflow
    cursor/             Cursor rules
    embed.go            go:embed plugin files into binary
  dashboard/
    macos/              Swift native app (planned)
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

## FAQ

**Q: Does 4x call any LLM APIs directly?**
No. The CLI is pure Go with zero LLM dependencies. Runners handle all AI interaction using their native platform capabilities.

**Q: Can I use different LLMs for different roles?**
Yes. Configure per-role models in `.4x/settings.json`. Use Claude for Design, Gemini for Code — each reads the same `.4x/` files.

**Q: How is this different from Devin / SWE-agent / OpenHands?**
Those are autonomous agents that do everything in one shot. 4x is a *framework* that structures multi-role collaboration with deterministic guardrails. It's closer to a CI pipeline for AI than a single autonomous agent.

## Origin Story

4x was born inside a production system called DCT (Designer-Coder-Tester) that shipped 60+ features for a large-scale platform rewrite. The patterns that survived — role isolation, file-based protocol, deterministic scope checking, evidence-based testing — became 4x. The parts that didn't survive — LLM-specific hacks, shared context assumptions, trust-based guardrails — were deliberately left out.

## Contributing

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## License

[MIT](LICENSE)

---

<p align="center">
  <strong>Stop hoping your AI writes correct code. Start verifying it.</strong>
</p>
