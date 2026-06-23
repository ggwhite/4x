# 4x Design — Protocol (`.4x/` Directory)

> Extracted from design.md §3

---

The `.4x/` directory is the single source of truth for a 4x workspace. It lives at the root of the project (or workspace) that 4x manages.

```
.4x/
├── settings.json                    # workspace configuration
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

## 3.1 `settings.json`

Workspace-level configuration. Created by `4x init`.

**Full example:**

```json
{
  "project": {
    "name": "todo-api",
    "description": "RESTful todo API with user authentication, built with Go + PostgreSQL.",
    "language": "go",
    "build": ["make build"],
    "test": ["make test"],
    "lint": ["make lint"],
    "includes": ["CLAUDE.md"]
  },
  "default_runner": "claude",
  "runners": {
    "claude": { "command": "claude", "model": "opus" }
  },
  "roles": {
    "designer": { "model": "opus", "instructions": ["設計時考慮冪等性"] },
    "coder": { "model": "sonnet", "includes": ["docs/coding-standards.md"] },
    "reviewer": { "model": "sonnet", "deep_model": "opus", "instructions": ["重點檢查 SQL injection"] },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "opus" }
  }
}
```

## 3.2 `features/*.yaml`

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

## 3.3 Runtime Directory Structure

Each feature gets a directory at `.4x/run/{feature-id}/`. The CLI creates it when the feature first transitions out of `init`.

```
.4x/run/{feature-id}/
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

## 3.4 `state.json` Schema

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
| `phase` | Matches state machine states (see [state-machine.md](state-machine.md)). |
| `role` | Human-readable role name; redundant with `phase` but useful for display. |
| `round` | Incremented each time the Coder starts a new amendment. |
| `consecutiveNoProgress` | Reset to 0 on any verify improvement; triggers escalation at threshold. |
| `lastFailCount` | Number of issues found by Reviewer/Tester in the most recent run. |
| `stopReason` | One of: `max-rounds`, `escalated`, `blocked`, `manual`, `done`. |

## 3.5 `events.jsonl` Schema

Append-only. One JSON object per line. Readers must handle unknown event types gracefully (forward compatibility).

**Common fields (all events):**

```jsonc
{
  "id":          "evt_01j9z...",   // unique event ID (ULID recommended)
  "type":        "run-start",      // see event types below
  "featureId":   "rest-api-for-todo-items",
  "ts":          "2026-06-10T09:10:00Z",
  "runner":      "claude",
  "round":       2,
  "notify":      "success"         // optional: notification hint (success|error|warning); empty = no notification
}
```

The optional `notify` field is a hint for clients (web/desktop dashboards) to surface an OS notification. It is set on terminal `run-end` (success), `guard-fail` (error), and `escalation` (warning) events; absent on all other events. Adding it is backward-compatible (`omitempty`).

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

## 3.6 `baseline.json` Schema

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

## 3.7 `verify.json` Schema

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

## 3.8 Report Formats

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
