# 4x Design Specification

> Version: 0.1 — Draft
> Status: Working document; sections marked [TBD] are pending finalization.

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Protocol — The `.4x/` Directory](#protocol)
4. [State Machine](#state-machine)
5. [Four Roles](#four-roles)
6. [Escalation Mechanism](#escalation-mechanism)
7. [Guardrails — `4x check`](#guardrails)
8. [Batch Mode](#batch-mode)
9. [Plugin Contract](#plugin-contract)
10. [CLI Commands Reference](#cli-commands-reference)

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
- Enforces valid state transitions (see §4).
- Runs `4x check` guardrails before any transition.
- Invokes plugins as subprocesses or via a defined interface.
- Implements batch planning and scheduling independently of any LLM.

The CLI must be **deterministic**: given the same inputs, it produces the same output. No LLM calls, no network calls in the CLI core.

### 2.2 Plugin Layer

Plugins are responsible for everything that touches an LLM:

- Constructing role prompts from templates and context files.
- Calling the model API.
- Writing output files to the feature's `.4x/{feature-id}/` directory.
- Reporting progress via heartbeat events.

A plugin is invoked by the CLI and exits with a standard exit code (0 = success, 1 = soft failure, 2 = hard error). See §9 for the full plugin contract.

### 2.3 Dashboard Layer

The dashboard is a read-only monitoring surface. It:

- Tails `events.jsonl` for live updates.
- Renders feature status, current role, round counter, and recent events.
- Exposes a local HTTP server (default: `localhost:4567`).
- Does not write any state; it is purely observational.

---

## 3. Protocol — The `.4x/` Directory

The `.4x/` directory is the single source of truth for a 4x workspace. It lives at the root of the project (or workspace) that 4x manages.

```
.4x/
├── config.yaml                    # workspace configuration
├── features/
│   ├── rest-api-for-todo-items.yaml
│   └── user-authentication.yaml
├── rest-api-for-todo-items/       # runtime directory, one per feature
│   ├── state.json
│   ├── events.jsonl
│   ├── baseline.json
│   ├── verify.json
│   ├── task-brief.md
│   ├── acceptance-criteria.md
│   ├── test-strategy.yaml
│   ├── coder-report.md
│   ├── review-report.md
│   ├── test-report.md
│   ├── final-report.md
│   └── commit-plan.md
└── user-authentication/
    └── …
```

### 3.1 `config.yaml`

Workspace-level configuration. Created by `4x init`.

**Full example:**

```yaml
# .4x/config.yaml

version: "1"

workspace:
  name: todo-api
  description: >
    RESTful todo API with user authentication, built with Go + PostgreSQL.

# Repositories that this workspace covers.
# Each entry corresponds to a directory the CLI may scope-check.
repos:
  - id: backend
    path: ./backend
    language: go
    primary: true
  - id: frontend
    path: ./frontend
    language: typescript
  - id: infra
    path: ./infra
    language: hcl

# Default runner plugin. Can be overridden per feature or per CLI invocation.
runner:
  default: claude
  timeout: 3600          # seconds; hard kill after this

# Guardrails applied before every role transition.
guardrails:
  scope_check: true        # no edits outside declared repos
  baseline_check: true     # baseline.json must exist for Tester phase
  require_files: true      # required output files must be present
  verify_evidence: true    # verify.json must record passing results

# Batch scheduling configuration.
batch:
  max_chain_length: 4      # longest allowed dependency chain
  parallel_slots: 2        # how many features may run concurrently

# Dashboard settings.
dashboard:
  port: 4567
  auto_open: false
```

### 3.2 `features/*.yaml`

One file per feature. Created by `4x new`. This is the human-editable specification for the feature.

`.4x/features/*.yaml` is the canonical source of truth for feature identity, name, description, status, priority, repos, subtasks, rules, and dependencies. The root `feature_list.json` file is a legacy backlog mirror only; it may be useful for older tooling, but the CLI and dashboard must not treat it as authoritative feature state. Commands that display or validate feature state read from `.4x/features/*.yaml` and may warn when `feature_list.json` is present but drifts from the canonical YAML files.

**Full example:**

```yaml
# .4x/features/rest-api-for-todo-items.yaml

id: rest-api-for-todo-items
title: "REST API for todo items"
priority: high             # high | medium | low
status: todo               # todo | in-progress | done | blocked | needs-attention

# Repositories this feature touches. The scope check enforces this.
repos:
  - backend

# Optional: specific sub-paths within repos (further restricts scope).
scope:
  - backend/internal/todo/
  - backend/api/v1/

description: |
  Implement a CRUD REST API for todo items.

  Endpoints:
    GET    /v1/todos          — list todos (paginated)
    POST   /v1/todos          — create todo
    GET    /v1/todos/{id}     — get single todo
    PUT    /v1/todos/{id}     — update todo
    DELETE /v1/todos/{id}     — delete todo

  Each todo has: id (UUID), title (string), done (bool), created_at, updated_at.
  Storage: PostgreSQL table `todos`.

acceptance:
  - All five endpoints return correct HTTP status codes.
  - Pagination: default page size 20, max 100.
  - Invalid input returns 400 with structured error body.
  - Non-existent resource returns 404.
  - Integration tests pass against a real PostgreSQL instance.

dependencies:
  # Feature IDs that must reach 'done' before this feature starts.
  - user-authentication

labels:
  - api
  - database

runner: claude              # override workspace default if needed
max_rounds: 6
```

### 3.3 Runtime Directory Structure

Each feature gets a directory at `.4x/{feature-id}/`. The CLI creates it when the feature first transitions out of `init`.

```
.4x/{feature-id}/
├── state.json             # machine-readable current state
├── events.jsonl           # append-only event log
├── baseline.json          # snapshot of repo state before coding begins
├── verify.json            # structured verification results
│
│   # Designer outputs
├── task-brief.md
├── acceptance-criteria.md
├── test-strategy.yaml
│
│   # Coder outputs
├── coder-report.md
│
│   # Reviewer outputs
├── review-report.md
│
│   # Tester outputs
├── test-report.md
├── final-report.md
│
│   # Shared
└── commit-plan.md
```

### 3.4 `state.json` Schema

```jsonc
{
  "featureId":             "rest-api-for-todo-items",
  "phase":                 "coding",        // init | designing | coding | reviewing
                                            // | testing | amending | accepting
                                            // | done | blocked | needs-attention
  "role":                  "Coder",         // current active role
  "round":                 2,               // current round number (1-based)
  "maxRounds":             6,               // from features/*.yaml or config
  "active":                true,            // false when stopped or done
  "runner":                "claude",        // which plugin is running
  "label":                 "",              // optional human label for this run
  "createdAt":             "2026-06-10T08:00:00Z",
  "updatedAt":             "2026-06-10T09:15:42Z",
  "since":                 "2026-06-10T09:10:00Z",  // when current phase started
  "consecutiveNoProgress": 0,               // incremented when verify.json shows no change
  "lastFailCount":         0,               // reviewer or tester failures in latest round
  "stopReason":            ""               // populated when active=false; see escalation
}
```

Field notes:

| Field | Notes |
|---|---|
| `phase` | Matches state machine states (§4). |
| `role` | Human-readable role name; redundant with `phase` but useful for display. |
| `round` | Incremented each time the Coder starts a new amendment. |
| `consecutiveNoProgress` | Reset to 0 on any verify improvement; triggers escalation at threshold. |
| `lastFailCount` | Number of issues found by Reviewer/Tester in the most recent run. |
| `stopReason` | One of: `max-rounds`, `escalated`, `blocked`, `manual`, `done`. |

### 3.5 `events.jsonl` Schema

Append-only. One JSON object per line. Readers must handle unknown event types gracefully (forward compatibility).

**Common fields (all events):**

```jsonc
{
  "id":          "evt_01j9z...",   // unique event ID (ULID recommended)
  "type":        "run-start",      // see event types below
  "featureId":   "rest-api-for-todo-items",
  "ts":          "2026-06-10T09:10:00Z",
  "runner":      "claude",
  "round":       2
}
```

**Event types:**

| Type | When emitted | Extra fields |
|---|---|---|
| `run-start` | Plugin subprocess starts | `role`, `phase` |
| `run-end` | Plugin subprocess exits | `role`, `phase`, `exitCode`, `durationSec` |
| `phase-start` | State machine enters a new phase | `phase`, `prevPhase` |
| `phase-end` | State machine exits a phase | `phase`, `outcome` (`ok` \| `fail` \| `escalate`) |
| `transition` | State changes | `from`, `to`, `trigger` |
| `step` | Free-form progress note from plugin | `message`, `detail` (optional) |
| `verify` | Verification result recorded | `passed` (bool), `summary`, `evidence` (array) |
| `scope-check` | Scope guardrail result | `passed` (bool), `violations` (array of paths) |
| `escalation` | Escalation triggered | `reason`, `triggeredBy`, `detail` |
| `blocked` | Feature entered blocked state | `reason`, `detail` |
| `heartbeat` | Plugin liveness signal | `message` (optional) |

**Example lines:**

```jsonl
{"id":"evt_01","type":"phase-start","featureId":"rest-api-for-todo-items","ts":"2026-06-10T09:10:00Z","runner":"claude","round":1,"phase":"designing","prevPhase":"init"}
{"id":"evt_02","type":"run-start","featureId":"rest-api-for-todo-items","ts":"2026-06-10T09:10:01Z","runner":"claude","round":1,"role":"Designer","phase":"designing"}
{"id":"evt_03","type":"step","featureId":"rest-api-for-todo-items","ts":"2026-06-10T09:11:30Z","runner":"claude","round":1,"message":"Analyzing legacy system schema"}
{"id":"evt_04","type":"run-end","featureId":"rest-api-for-todo-items","ts":"2026-06-10T09:14:00Z","runner":"claude","round":1,"role":"Designer","phase":"designing","exitCode":0,"durationSec":239}
{"id":"evt_05","type":"verify","featureId":"rest-api-for-todo-items","ts":"2026-06-10T09:28:00Z","runner":"claude","round":1,"passed":true,"summary":"5/5 integration tests pass","evidence":["backend/go.test.log"]}
```

### 3.6 `baseline.json` Schema

A snapshot of repository state captured before the Coder begins. Used by the Tester to verify that changes are contained to the declared scope and that no regressions were introduced.

```jsonc
{
  "capturedAt": "2026-06-10T09:10:00Z",
  "featureId":  "rest-api-for-todo-items",
  "repos": [
    {
      "id":     "backend",
      "path":   "./backend",
      "commit": "a1b2c3d4e5f6...",
      "dirty":  false
    }
  ],
  "testResults": {
    "backend": {
      "passed": 42,
      "failed": 0,
      "skipped": 3,
      "command": "go test ./..."
    }
  }
}
```

### 3.7 `verify.json` Schema

Written by the Tester after running verification. Also written (partially) after the Reviewer's static check.

```jsonc
{
  "featureId":   "rest-api-for-todo-items",
  "phase":       "testing",
  "round":       2,
  "timestamp":   "2026-06-10T10:05:00Z",
  "passed":      true,
  "summary":     "All acceptance criteria met. 5/5 integration tests pass.",
  "evidence": [
    {
      "type":    "test-run",
      "command": "go test ./backend/internal/todo/...",
      "passed":  true,
      "output":  "ok  backend/internal/todo 0.412s"
    },
    {
      "type":    "api-smoke",
      "command": "curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/v1/todos",
      "passed":  true,
      "output":  "200"
    }
  ],
  "regressions": [],
  "noProgressRounds": 0
}
```

### 3.8 Report Formats

All reports are Markdown or YAML files written by role plugins. The CLI only checks for their existence; it does not parse their content (except `test-strategy.yaml` and `verify.json`).

#### `task-brief.md` (Designer output)

```markdown
# Task Brief — {Feature Title}

## Goal
One-paragraph summary of what is being built and why.

## Scope
- Repos: backend
- Paths: backend/internal/todo/, backend/api/v1/

## Out of Scope
- Authentication (covered by ws-002)
- Frontend changes

## Context
Background information, links to legacy analysis, relevant docs.

## Open Questions
Questions that need answers before coding begins (ideally resolved before Coder starts).
```

#### `acceptance-criteria.md` (Designer output)

```markdown
# Acceptance Criteria — {Feature Title}

Each criterion maps to a verifiable test or check.

| # | Criterion | Verification Method |
|---|---|---|
| AC-1 | GET /v1/todos returns 200 with array | Integration test |
| AC-2 | POST /v1/todos with missing title returns 400 | Integration test |
| AC-3 | Pagination: page=1&size=5 returns max 5 items | Integration test |
| AC-4 | Non-existent ID returns 404 | Integration test |
| AC-5 | All existing tests continue to pass | go test ./... |
```

#### `test-strategy.yaml` (Designer output)

```yaml
feature: rest-api-for-todo-items

levels:
  - level: unit
    scope: backend/internal/todo/
    command: go test ./backend/internal/todo/...
    required: true

  - level: integration
    scope: backend/
    command: go test -tags=integration ./backend/...
    required: true
    setup: docker compose up -d postgres

  - level: smoke
    command: ./scripts/smoke-test.sh
    required: false

baseline_command: go test ./backend/...
verify_commands:
  - go test -tags=integration ./backend/...
```

#### `coder-report.md` (Coder output)

```markdown
# Coder Report — Round {N}

## What Was Done
Summary of changes made in this round.

## Files Changed
- backend/internal/todo/handler.go — new file
- backend/internal/todo/store.go — new file
- backend/api/v1/routes.go — added todo routes
- backend/db/migrations/002_todos.sql — new migration

## Deviations from Spec
Any intentional departures from task-brief.md or acceptance-criteria.md, with rationale.

## Known Issues
Anything the Coder knows is incomplete or risky.

## Verification Run
Command and output of a quick local verify (if run).
```

#### `review-report.md` (Reviewer output)

```markdown
# Review Report — Round {N}

## Summary
Pass / Fail / Conditional Pass

## Checklist

| Item | Status | Notes |
|---|---|---|
| Scope confined to declared repos/paths | PASS | |
| Acceptance criteria addressed | PASS | |
| No hardcoded credentials or secrets | PASS | |
| Error handling complete | FAIL | Handler returns 500 on DB error without logging |
| Tests cover AC-1 through AC-5 | PASS | |

## Issues

### [HIGH] Missing error logging in handler
**File:** backend/internal/todo/handler.go:42
**Detail:** DB errors are swallowed. Should log and return structured 500.
**Required:** Yes — must fix before Tester.

## Adversarial Check
Describe one realistic misuse scenario and whether the implementation handles it.

## Verdict
FAIL — send back to Coder. See issues above.
```

#### `test-report.md` (Tester output)

```markdown
# Test Report — Round {N}

## Summary
PASS / FAIL

## Verification Results

### Unit Tests
Command: `go test ./backend/internal/todo/...`
Result: PASS (12/12)

### Integration Tests
Command: `go test -tags=integration ./backend/...`
Result: PASS (8/8)

### Smoke Tests
Command: `./scripts/smoke-test.sh`
Result: PASS

## Acceptance Criteria Coverage

| # | Criterion | Result |
|---|---|---|
| AC-1 | GET returns 200 with array | PASS |
| AC-2 | POST missing title returns 400 | PASS |
| AC-3 | Pagination works | PASS |
| AC-4 | Non-existent ID returns 404 | PASS |
| AC-5 | Existing tests pass | PASS |

## Regressions
None detected. Baseline: 42 passing, current: 54 passing.

## Verdict
PASS — ready for acceptance.
```

#### `final-report.md` (Tester output, on overall pass)

```markdown
# Final Report — {Feature Title}

## Status
DONE

## Rounds
3 rounds total. Amended once after Reviewer found missing error handling.

## What Was Built
Summary for future readers.

## Verification Evidence
- go test ./backend/...: 54 tests pass
- Integration tests: 8/8 pass
- Smoke test: pass

## Notes for Next Features
Anything that affects downstream features or shared components.
```

#### `commit-plan.md` (Tester output, on overall pass)

```markdown
# Commit Plan — {Feature Title}

Commit these changes in the following order to keep the history clean and reviewable.

## Commit 1: Add database migration
Files:
- backend/db/migrations/002_todos.sql

Message: feat(db): add todos table migration

## Commit 2: Implement todo store
Files:
- backend/internal/todo/store.go
- backend/internal/todo/store_test.go

Message: feat(todo): implement PostgreSQL-backed todo store

## Commit 3: Implement API handlers and routes
Files:
- backend/internal/todo/handler.go
- backend/internal/todo/handler_test.go
- backend/api/v1/routes.go

Message: feat(api): add CRUD endpoints for todo items

## Commit 4: Add integration tests
Files:
- backend/internal/todo/integration_test.go

Message: test(todo): add integration tests for todo API
```

---

## 4. State Machine

### States

| State | Description |
|---|---|
| `init` | Feature created, not yet started. |
| `designing` | Designer role is active. |
| `coding` | Coder role is active. |
| `reviewing` | Reviewer role is active. |
| `testing` | Tester role is active. |
| `amending` | Coder is amending based on Reviewer or Tester feedback. |
| `accepting` | Final human or automated acceptance check. |
| `done` | Feature complete. |
| `blocked` | Cannot proceed without external input. |
| `needs-attention` | Automatic escalation triggered; human review required. |

### Valid Transitions

| From | To | Trigger |
|---|---|---|
| `init` | `designing` | `4x run` or `4x transition` |
| `designing` | `coding` | Designer outputs verified present |
| `coding` | `reviewing` | Coder outputs verified present |
| `reviewing` | `testing` | Reviewer verdict: pass |
| `reviewing` | `amending` | Reviewer verdict: fail |
| `amending` | `reviewing` | Coder amendment complete |
| `testing` | `accepting` | Tester verdict: pass |
| `testing` | `amending` | Tester verdict: fail |
| `accepting` | `done` | Acceptance check passes |
| `any` | `blocked` | Escalation with `blocker` reason |
| `any` | `needs-attention` | Escalation with any other reason |
| `blocked` | `designing` | Human resolves blocker, restarts |
| `blocked` | `coding` | Human resolves blocker, restarts at coding |
| `needs-attention` | `designing` | Human intervenes, restarts |
| `needs-attention` | `coding` | Human intervenes, restarts at coding |

The CLI rejects any transition not in this table.

### Round Counter

The `round` counter increments each time the feature returns to `amending`. It is checked against `maxRounds` before any new Coder run. If `round >= maxRounds`, the CLI rejects the transition and moves to `needs-attention`.

---

## 5. Four Roles

### 5.1 Designer

**Identity:** Understands requirements, produces unambiguous specifications that a Coder can implement without further clarification.

**Inputs:**
- `features/{feature-id}.yaml` (title, description, acceptance, repos, scope)
- Legacy system analysis docs (if provided in config or feature yaml)
- Workspace `config.yaml`

**Outputs (all required before transition to coding):**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`

**Constraints:**
- Must not write any implementation code.
- Must not propose changes outside the declared `repos` and `scope`.
- Open questions in `task-brief.md` must be flagged clearly; the Designer does not invent answers.
- Acceptance criteria must each be independently verifiable; no vague criteria ("system should work correctly").

### 5.2 Coder

**Identity:** Implements exactly what the Designer specified. Does not re-interpret requirements; raises ambiguity by escalating, not by guessing.

**Inputs:**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`
- `review-report.md` (if amending)
- `test-report.md` (if amending after Tester failure)
- `baseline.json`

**Outputs (all required before transition to reviewing):**
- Implementation code in declared repos
- `coder-report.md`

**Constraints:**
- Must only write files within the declared `repos` and `scope` paths.
- Must not modify test assertions to make tests pass artificially.
- Must not delete tests.
- Must not add `try/except` or similar constructs solely to suppress errors.
- If the spec is impossible or contradictory, must write an escalation note in `coder-report.md` rather than guessing.

### 5.3 Reviewer

**Identity:** Adversarial code reviewer. Finds real bugs and violations. Also acts as a spec compliance checker.

The Reviewer runs two passes:

1. **Checklist pass:** Systematic check against a standard list (scope, error handling, security basics, test coverage, hardcoded values, logging).
2. **Adversarial pass:** Attempts to break the implementation by reasoning about misuse, edge cases, race conditions, and error paths.

**Inputs:**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`
- All changed files (via git diff or explicit list in `coder-report.md`)

**Outputs (required before transition):**
- `review-report.md` with explicit verdict: `PASS`, `CONDITIONAL PASS`, or `FAIL`

**Constraints:**
- Must categorize each issue as HIGH, MEDIUM, LOW, or INFO.
- HIGH issues always block transition to testing.
- LOW and INFO issues are recorded but do not block.
- Must not suggest changes outside the feature's declared scope.
- Must not re-design the solution; only flag problems with the current one.

**Standard Checklist:**

| Category | Items |
|---|---|
| Scope | No files outside declared repos/paths |
| Spec compliance | All acceptance criteria addressed |
| Error handling | All error paths handled; no swallowed errors |
| Security | No hardcoded secrets; input validated; appropriate auth |
| Tests | Tests cover acceptance criteria; no artificial pass tricks |
| Logging | Errors logged at appropriate level |
| Concurrency | No obvious data races (if concurrent code present) |

### 5.4 Tester

**Identity:** Verifies that the implementation actually satisfies the acceptance criteria against a real (or realistic) environment. Does not trust `coder-report.md`; runs verification independently.

**Inputs:**
- `acceptance-criteria.md`
- `test-strategy.yaml`
- `baseline.json`
- The actual repository state

**Outputs (all required before transition to accepting):**
- `verify.json`
- `test-report.md`
- `final-report.md` (on overall pass)
- `commit-plan.md` (on overall pass)

**Constraints:**
- Must run the `verify_commands` from `test-strategy.yaml`; may not substitute different commands without escalating.
- Must compare against `baseline.json` and report regressions.
- Must not modify test assertions.
- Must produce `verify.json` with evidence entries pointing to actual command output.
- `final-report.md` and `commit-plan.md` are only written when all acceptance criteria pass.

---

## 6. Escalation Mechanism

Escalation moves a feature to `blocked` or `needs-attention` and stops automatic progression. A human must intervene to resume.

### Escalation Reasons

| Reason | Description | Who Triggers |
|---|---|---|
| `spec-mismatch` | Coder finds the spec is contradictory or impossible to implement as written | Coder (via coder-report.md flag) |
| `criteria-wrong` | Tester finds an acceptance criterion cannot be verified as written | Tester |
| `blocker` | External dependency missing (service down, API unavailable, missing credential) | Any role or CLI |
| `scope-change` | The feature requires modifying repos not in its declared scope | CLI scope check |
| `max-rounds` | `round >= maxRounds` | CLI |
| `no-progress` | `consecutiveNoProgress` exceeds threshold (default: 3) | CLI |

### Escalation Flow

1. The triggering agent writes a description of the reason to the relevant report file (e.g., `coder-report.md` section "Escalation").
2. The CLI (or plugin) calls `4x event escalation --reason <reason> --detail "..."`.
3. The CLI writes the `escalation` event to `events.jsonl`.
4. The CLI updates `state.json`: `phase` → `needs-attention` or `blocked`, `active` → `false`, `stopReason` → reason.
5. The dashboard highlights the feature.
6. A human reads the report, resolves the issue, and resumes with `4x transition <feature-id> designing` (or `coding`).

---

## 7. Guardrails — `4x check`

`4x check` runs before every state transition. It is synchronous and deterministic; it never calls an LLM.

### 7.1 Scope Check

Compares files changed since baseline commit against the declared `repos` and `scope` paths in `features/{feature-id}.yaml`.

- Any changed file outside declared scope → scope check fails.
- Failure emits a `scope-check` event with `passed: false` and the list of violations.
- Transition is blocked until violations are resolved or scope is explicitly widened (requires human edit of the feature yaml).

### 7.2 Baseline Check

Verifies that `baseline.json` exists and was captured from the same commit that is the parent of current changes. Required for Tester phase.

### 7.3 Required Files Check

Before each transition, validates that all required output files for the preceding role are present and non-empty.

| Transition | Required Files |
|---|---|
| `designing` → `coding` | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` |
| `coding` → `reviewing` | `coder-report.md` |
| `reviewing` → `testing` (pass) | `review-report.md` with `PASS` verdict |
| `reviewing` → `amending` (fail) | `review-report.md` with `FAIL` or `CONDITIONAL PASS` verdict |
| `amending` → `reviewing` | `coder-report.md` updated (mtime > previous) |
| `testing` → `accepting` | `verify.json`, `test-report.md`, `final-report.md`, `commit-plan.md` |

### 7.4 Verify Evidence Check

Before `testing` → `accepting`, validates that `verify.json`:
- `passed` is `true`
- `evidence` array is non-empty
- Each evidence entry has `type`, `command`, `passed: true`

---

## 8. Batch Mode

Batch mode schedules and runs multiple features automatically, respecting inter-feature dependencies.

### 8.1 Dependency Graph

Dependencies are declared in `features/*.yaml` under the `dependencies` key (list of feature IDs). The batch planner builds a directed acyclic graph (DAG).

If cycles are detected, `4x batch plan` fails with an error listing the cycle.

### 8.2 Union-Find Grouping

Features are grouped into independent clusters using Union-Find on their dependency graph. Clusters with no shared repos or dependencies can run fully in parallel.

### 8.3 Hub and Leaf Repos

Within a cluster:

- **Hub repo:** A repo depended on by 2 or more features in the same batch. Features touching a hub repo are serialized within that cluster.
- **Leaf repo:** A repo touched by only one feature. Leaf-only features may run concurrently.

### 8.4 Bridge Detection

A **bridge** in the dependency graph is an edge whose removal disconnects the graph. Bridge features (features that are the sole dependency path between two parts of the graph) are scheduled conservatively: they must complete before any downstream cluster begins.

### 8.5 Chain Scheduling

Within a cluster, features are sorted topologically and run in chains. A chain is a maximal sequence of features where each depends on the previous.

The maximum chain length is configurable (`batch.max_chain_length` in `config.yaml`, default 4). Chains longer than the limit are split: the first segment runs, must reach `done`, then the next segment is unlocked.

This prevents long-running batch jobs from blocking the entire workspace on a single failing feature.

### 8.6 Scheduling Algorithm

```
batch plan:
  1. Build DAG from all features with status != done.
  2. Detect cycles → error if found.
  3. Union-Find to identify independent clusters.
  4. For each cluster:
     a. Topological sort.
     b. Identify hub repos.
     c. Detect bridges.
     d. Split into chains (max_chain_length).
     e. Emit a schedule: ordered list of (feature-id, can-start-after, slot).
  5. Write schedule to .4x/batch-plan.json.

batch next:
  1. Read batch-plan.json.
  2. Find features where all dependencies are done and a parallel slot is free.
  3. Start the next eligible feature.
```

### 8.7 `batch-plan.json` Format

```jsonc
{
  "generatedAt": "2026-06-10T08:00:00Z",
  "clusters": [
    {
      "id": "cluster-0",
      "features": ["user-authentication", "rest-api-for-todo-items"],
      "chains": [
        ["user-authentication", "rest-api-for-todo-items"]
      ]
    }
  ],
  "schedule": [
    {
      "featureId": "user-authentication",
      "slot": 0,
      "canStartAfter": []
    },
    {
      "featureId": "rest-api-for-todo-items",
      "slot": 0,
      "canStartAfter": ["user-authentication"]
    }
  ]
}
```

---

## 9. Plugin Contract

A 4x plugin is an executable (or library) that the CLI invokes to run a role. Plugins are responsible for all LLM interaction.

A conforming plugin must:

1. **Accept a standard invocation:**
   ```
   4x-plugin-{name} run --feature-id <id> --role <role> --workspace <path>
   ```
   The `--workspace` flag points to the root containing `.4x/`.

2. **Read context from the filesystem only.** The plugin reads `.4x/config.yaml`, `.4x/features/{feature-id}.yaml`, and any existing files in `.4x/{feature-id}/`. It must not rely on environment variables beyond standard ones (`HOME`, `PATH`, `4X_*` prefixed vars).

3. **Write outputs to `.4x/{feature-id}/`.** All output files must be written atomically (write to temp, rename). Partial writes that crash mid-run must not leave corrupt state.

4. **Emit heartbeat events** at least once every 60 seconds during long operations, by calling:
   ```
   4x event heartbeat --feature-id <id>
   ```
   or by writing directly to `events.jsonl` (with correct schema).

5. **Exit with standard codes:**
   - `0` — role completed successfully; required output files are present.
   - `1` — soft failure; role could not complete but did not corrupt state. The CLI will move the feature to `needs-attention`.
   - `2` — hard error (unexpected crash, missing required tool, etc.). The CLI will halt the batch and alert.

6. **Support `--dry-run`:** When invoked with `--dry-run`, the plugin prints the prompt it would send and exits 0 without calling any LLM or writing any files.

---

## 10. CLI Commands Reference

### `4x init`

Initialize a `.4x/` workspace in the current directory.

```
4x init [--name <name>] [--runner <runner>]
```

Creates `.4x/config.yaml` with defaults. Does not overwrite if already exists.

---

### `4x new <title>`

Create a new feature spec.

```
4x new "REST API for todo items" [--repo <repo-id>] [--priority high|medium|low]
```

Creates `.4x/features/{slugified-title}.yaml`. Opens in `$EDITOR` if `--edit` is passed.

---

### `4x status [<feature-id>]`

Show current state of one or all features.

```
4x status
4x status rest-api-for-todo-items
4x status --json
```

Output includes: phase, role, round/maxRounds, active, last event timestamp, stopReason (if any).

---

### `4x check <feature-id>`

Run all guardrails without transitioning state.

```
4x check rest-api-for-todo-items
4x check rest-api-for-todo-items --scope-only
4x check rest-api-for-todo-items --files-only
```

Exits 0 if all checks pass, 1 if any fail. Prints a summary of each check result.

---

### `4x transition <feature-id> <to-state>`

Force a state transition (human override).

```
4x transition rest-api-for-todo-items coding
4x transition rest-api-for-todo-items designing --reason "Revised spec after user feedback"
```

Validates the transition is legal per the state machine. Runs `4x check` unless `--skip-check` is passed (not recommended).

---

### `4x event <type>`

Append an event to `events.jsonl`.

```
4x event step --feature-id rest-api-for-todo-items --message "Starting DB migration"
4x event heartbeat --feature-id rest-api-for-todo-items
4x event escalation --feature-id rest-api-for-todo-items --reason blocker --detail "PostgreSQL unavailable"
```

Used by plugins and scripts to record progress without modifying state.

---

### `4x prompt <feature-id> --role <role>`

Print the prompt that would be sent to the LLM for this role, without executing it. Useful for debugging and prompt tuning.

```
4x prompt rest-api-for-todo-items --role Designer
4x prompt rest-api-for-todo-items --role Coder --format markdown
```

---

### `4x batch plan`

Analyze all features and generate a `batch-plan.json`.

```
4x batch plan
4x batch plan --dry-run    # print schedule, don't write file
4x batch plan --max-chain 3
```

---

### `4x batch next`

Start the next eligible feature according to `batch-plan.json`.

```
4x batch next
4x batch next --runner claude --slot 1
4x batch next --all    # start all currently eligible features up to parallel_slots limit
```

---

### `4x live`

Start the dashboard web server.

```
4x live
4x live --port 4567
4x live --feature rest-api-for-todo-items    # focus on one feature
```

Opens `http://localhost:4567` in the default browser unless `--no-open` is passed.
Streams `events.jsonl` updates via SSE (Server-Sent Events).
