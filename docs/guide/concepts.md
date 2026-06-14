# Core Concepts

## The Four Roles

| Role | Responsibility | Inputs | Outputs | Cannot |
|---|---|---|---|---|
| **Designer** | Analyze requirements, produce spec, define acceptance criteria and test strategy | Feature description, codebase | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | Modify source code |
| **Coder** | Implement what the spec says | `task-brief.md`, prior test/review reports | Source code, `coder-report.md` | Modify acceptance criteria or test scripts |
| **Reviewer** | Catch bugs, security issues, spec violations | Diff, spec, coder report, project rules | `review-report.md` | Modify source code |
| **Tester** | Validate against acceptance criteria with evidence | Acceptance criteria, coder report, test strategy | Test scripts, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | Modify source code |

Each role is **isolated** — the Coder never sees prior review feedback during implementation. The Tester validates against criteria written by the Designer, not the Coder.

### Review: Two Phases

1. **Checklist review** (standard model) — checks against project hard rules: security, concurrency, error handling, style
2. **Adversarial review** (deep model) — "What's the worst bug hiding in this diff?" Findings rated by severity.

### Escalation

The Coder or Tester can escalate back to the Designer when:

| Reason | Meaning |
|---|---|
| `spec-mismatch` | DB/API doesn't match spec |
| `criteria-wrong` | Acceptance criteria are incorrect |
| `blocker` | Missing dependency or infra issue |
| `scope-change` | Need to modify repos outside scope |

Escalation is written to `escalation.json`. The loop automatically routes back to the Designer.

---

## State Machine

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
                     ↑      ↓
                     └──────┘
```

### All Valid Transitions

| From | To |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`, `designing` |
| `reviewing` | `testing`, `amending` |
| `amending` | `reviewing`, `designing` |
| `testing` | `deep-reviewing`, `amending`, `designing` |
| `deep-reviewing` | `accepting`, `amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| any | `blocked`, `needs-attention`, `done`, `abandoned` |

### Round Counter

- Entering `coding` when round is 0 sets round to 1
- Entering `amending` increments the round
- `ShouldStop` triggers when round >= maxRounds or 3+ consecutive rounds with no progress

### Phase Decisions in the Loop

| Phase | Condition | Action |
|---|---|---|
| `designing` | `task-brief.md` missing | → `needs-attention` |
| `coding` / `amending` | `escalation.json` with `spec-mismatch` or `criteria-wrong` | → `designing` |
| `reviewing` | Verdict line starts with FAIL or has `[CRITICAL]` | → `amending` |
| `testing` | `verify.json` not passed or artifacts missing | → `amending` |

---

## File Protocol

Roles communicate through the `.4x/` directory, not shared context windows.

```
.4x/
├── settings.json                    # Project config
├── plugins/                         # Runner instruction files
├── batch-plan.json                  # Batch execution plan
├── batch-stop                       # Graceful stop signal
├── features/
│   └── {id}.yaml                    # Feature definition (canonical source)
└── {feature-id}/
    ├── state.json                   # Phase, role, round, active, runner, runners, stopReason
    ├── events.jsonl                 # Audit trail
    ├── baseline.json                # Pre-coding snapshot (HEAD, branch, dirty files)
    ├── task-brief.md                # Designer → Coder: spec + architecture
    ├── acceptance-criteria.md       # Designer → Tester: testable criteria
    ├── test-strategy.yaml           # Designer → Tester: test approach
    ├── final-report.md              # End-of-loop summary
    ├── commit-plan.md               # How to split changes into commits
    ├── logs/
    │   └── round-{N}-{role}.log     # Per-round per-role execution log
    └── rounds/round-{N}/
        ├── coder-report.md          # What the Coder did
        ├── review-report.md         # Reviewer findings + verdict
        ├── test-report.md           # Tester results
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status` mirrors `state.json` phase for quick listing. Valid values: `not-started`, `in-progress`, `ready-for-review`, `needs-attention`, `blocked`, `done`, `abandoned`. `abandoned` features are treated as completed (won't block dependencies) but display with strikethrough in the dashboard. `depends` lists feature IDs that must be done (or abandoned) before this feature can run. `repos` lists the repository names (from `workspace.repos`) that this feature touches; empty means all repos in scope.

### Workspace Config (Multi-Repo)

By default, 4x operates in monorepo mode. To work across multiple repositories, declare them in `.4x/settings.json`:

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

Each entry maps a repo name to its path (relative to the workspace root) and an optional `hub` flag. Hub repos are shared infrastructure that multiple features may touch — they are excluded from the scope clustering in `4x batch plan`.

In monorepo mode (no `workspace.repos`), all scope checks and git operations use the single repo root.

---

## Guardrails

Deterministic checks enforced by the CLI — not dependent on AI judgment.

| Guardrail | What it does |
|---|---|
| **Required files** | Verifies phase-appropriate artifacts exist (e.g., `task-brief.md` after designing) |
| **Baseline** | Captures pre-coding state (HEAD, branch, dirty files); warns if dirty files exist |
| **Scope** | In monorepo mode: compares `git diff --name-only HEAD` top-level directories against feature's declared repos. In multi-repo mode: uses `gitops.Ops.DetectChangedRepos()` across all workspace repos |
| **Dependencies** | Blocks `4x run` if depended features are not done |
| **Backlog drift** | Warns when `.4x/features/*.yaml` and external mirrors are out of sync |
| **Testing → Accepting gate** | Requires `verify.json` (passed=true), `test-report.md`, `final-report.md`, `commit-plan.md` |

Run manually with `4x check <feature-id>`.

---

## Pending Review Gate

The loop does **not** go directly to `done`. After accepting, the feature enters `pending-review` — waiting for a human to review the AI's work.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

This ensures a human always signs off before a feature is considered complete.
