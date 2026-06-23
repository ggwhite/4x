# 4x Design — Overview & Architecture

> Extracted from design.md §1–§2

---

## 1. Overview

**4x** is a multi-role AI development loop framework. It structures AI-assisted software development around four distinct roles — **Designer**, **Coder**, **Reviewer**, and **Tester** — that run in sequence, producing verifiable artifacts at each step before handing off to the next role.

### Core Properties

| Property | Description |
|---|---|
| LLM-agnostic | No dependency on any specific model or provider. Plugins handle model I/O. |
| File-based protocol | All state, artifacts, and events are plain files under `.4x/`. Any tool can read or write them. |
| Deterministic guardrails | The CLI enforces invariants (scope, baseline, required files) independently of any LLM. |
| Observable | A structured event log (`events.jsonl`) supports live dashboards and post-hoc analysis. |
| Resumable | Any session can be stopped and resumed; state survives across processes and machines. |

### The Loop

```
  ┌─────────────┐
  │   Designer  │  produces: task-brief.md, acceptance-criteria.md, test-strategy.yaml
  └──────┬──────┘
         │
  ┌──────▼──────┐
  │    Coder    │  produces: implementation, coder-report.md
  └──────┬──────┘
         │
  ┌──────▼──────┐
  │   Reviewer  │  produces: review-report.md  ──► amend if issues found
  └──────┬──────┘
         │
  ┌──────▼──────┐
  │    Tester   │  produces: test-report.md, verify.json, final-report.md
  └──────┬──────┘
         │
      done / escalate
```

Each role reads its predecessor's outputs, does its work, and writes its own outputs. The CLI validates that required files are present before allowing a transition.

---

## 2. Architecture

4x has three layers:

```
┌───────────────────────────────────────────────────────────────┐
│  Dashboard (monitoring)                                       │
│  Web UI, live event stream, feature status overview           │
└───────────────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────────────┐
│  Plugins (per-platform orchestration)                         │
│  claude-plugin, openai-plugin, codex-plugin, …               │
│  Responsible for: prompt construction, model I/O, retries     │
└───────────────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────────────┐
│  CLI (Go, deterministic guardrails)                           │
│  State machine, file protocol, scope check, batch planner     │
└───────────────────────────────────────────────────────────────┘
```

### 2.1 CLI Layer (Go)

The CLI is the authoritative keeper of state. It:

- Reads and writes `state.json` and `events.jsonl` atomically.
- Enforces valid state transitions (see [state-machine.md](state-machine.md)).
- Runs `4x check` guardrails before any transition.
- Invokes plugins as subprocesses or via a defined interface.
- Implements batch planning and scheduling independently of any LLM.

The CLI must be **deterministic**: given the same inputs, it produces the same output. No LLM calls, no network calls in the CLI core.

### 2.2 Plugin Layer

Plugins are responsible for everything that touches an LLM:

- Constructing role prompts from templates and context files.
- Calling the model API.
- Writing output files to the feature's `.4x/run/{feature-id}/` directory.
- Reporting progress via heartbeat events.

A plugin is invoked by the CLI and exits with a standard exit code (0 = success, 1 = soft failure, 2 = hard error). See [plugin-contract.md](../reference/plugin-contract.md) for the full contract.

### 2.3 Dashboard Layer

The dashboard is a read-only monitoring surface. It:

- Tails `events.jsonl` for live updates.
- Renders feature status, current role, round counter, and recent events.
- Exposes a local HTTP server (default: `localhost:4567`).
- Does not write any state; it is purely observational.
