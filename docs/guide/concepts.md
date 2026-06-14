# Core Concepts

## The Four Roles

| Role | Responsibility | Inputs | Outputs | Cannot |
|---|---|---|---|---|
| **Designer** | Analyze requirements, produce spec, define acceptance criteria and test strategy | Feature description, codebase | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | Modify source code |
| **Coder** | Implement what the spec says | `task-brief.md`, prior test/review reports | Source code, `coder-report.md` | Modify acceptance criteria or test scripts |
| **Reviewer** | Catch bugs, security issues, spec violations | Diff, spec, coder report, project rules | `review-report.md` | Modify source code |
| **Tester** | Validate against acceptance criteria with evidence | Acceptance criteria, coder report, test strategy | Test scripts, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | Modify source code |

Each role is **isolated** — the Coder never sees prior review feedback during implementation. The Tester validates against criteria written by the Designer, not the Coder.

### Additional Loop Roles

Two additional roles operate later in the loop:

| Role | Phase | Responsibility |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | Adversarial review — finds the worst-case bugs across the full diff |
| **Acceptor** | `accepting` | Aggregates all round findings into `final-report.md` and `commit-plan.md` for human review |

The Acceptor uses its own dedicated model configuration (`roles.acceptor.model`) — distinct from the Designer. It reads ALL round artifacts before producing the final summary.

### Pipeline Profiles

A **pipeline profile** selects which roles run for a given feature, so simple work skips roles instead of always running the full six-role pipeline. Built-in profiles:

| Profile | Roles |
|---|---|
| `full` | designer, coder, reviewer, tester, deep-reviewer, acceptor |
| `normal` | coder, reviewer, tester, acceptor |
| `quick` | coder, reviewer |

`coder` is always required. When `profiles` are configured, the profile is auto-selected by feature priority (highest priority → `full`, then `normal`, then `quick`); `--profile` overrides the choice. A role not in the active profile is skipped — the loop transitions along the same valid state edges without invoking that runner. See [Configuration](configuration.md) for the `profiles`, `parallel_review_test`, and `coder_model` settings.

### Review: Two Phases

1. **Checklist review** (standard model) — checks against project hard rules: security, concurrency, error handling, style
2. **Adversarial review** (deep model) — "What's the worst bug hiding in this diff?" Findings rated by severity.

### Deep Review Self-Healing

When the Deep Reviewer finds blocking issues, the `deep-reviewing` phase repairs them **in place** instead of sending the work all the way back through `amending → reviewing → testing`. Since the Reviewer and Tester already passed before deep review, re-running the whole expensive chain (especially the deep model) is wasteful.

Inside the same phase the loop spawns two scoped sub-roles, repeating until the report passes or a cap is hit:

| Sub-role | Model | Reads | Writes | Scope |
|---|---|---|---|---|
| **mini-coder** | coder model | `deep-review-report.md` `## Issues` only (not `task-brief.md`) | source code, `coder-report.md` | only the issues the deep reviewer named |
| **re-verifier** | reviewer model | the prior issues + the mini-coder's diff for this iteration | `deep-reverify-{n}.md`, updates the `## Verdict` of `deep-review-report.md` | verifies old issues are fixed and the new diff introduces no bug |

The phase stays `deep-reviewing` throughout — the sub-roles are not state-machine phases. When the re-verifier confirms a clean PASS, the loop advances to `accepting`. The loop runs at most `roles.deep-reviewer.max_fix_rounds` iterations (default 2); if the mini-coder edits files outside the feature scope, or the cap is reached while still failing, the feature escalates to `needs-attention` with the FAIL report preserved.

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
| `coding` / `amending` | `escalation.json` with `spec-mismatch`, `criteria-wrong`, or `scope-change` | → `designing` |
| `reviewing` | Verdict does not start with `PASS` (must be explicit `PASS` or `CONDITIONAL PASS`) | → `amending` |
| `testing` | `verify.json` not passed or artifacts missing | → `amending` |
| `deep-reviewing` | Deep review FAILs | self-heal in place (mini-coder + re-verifier), up to `max_fix_rounds`; PASS → `accepting`, otherwise → `needs-attention` |
| any (non-designer) | Guard check finds scope violation, baseline drift, or missing required file | → `needs-attention` |

---

## File Protocol

Roles communicate through the `.4x/` directory, not shared context windows.

```
.4x/
├── settings.json                    # Project config
├── plugins/                         # Runner instruction files
├── batch-plan.json                  # Batch execution plan
├── batch-stop                       # Graceful stop signal
├── batch-conflict.json              # Batch auto-merge conflict signal (paused)
├── features/
│   └── {id}.yaml                    # Feature definition (canonical source)
└── {feature-id}/
    ├── state.json                   # Phase, role, round, active, runner, runners, stopReason, profile
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

### Batch Signal Files

Two top-level signal files coordinate a running batch with external observers (the CLI and the dashboard):

- **`batch-stop`** — an empty marker file. `4x batch run` polls for it between features and stops gracefully once it exists (see [Batch Mode](batch.md)).
- **`batch-conflict.json`** — written when batch auto-merge hits a merge conflict and pauses. It carries enough detail for the dashboard to render the conflict without re-running git:

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  `conflictRepo` is empty in monorepo mode. The file is cleared at the start of each batch run and when the user continues a paused batch.

### Atomic State Writes

`state.json` is read and written by multiple actors concurrently — the run loop, the dashboard server, and background reconcilers. To avoid a reader ever seeing a truncated or half-written file, `WriteState` never writes in place. It marshals the state, writes it to a temp file (`.state-*.json`) **in the same directory** (guaranteeing the same filesystem so the rename is atomic), then `os.Rename`s it over `state.json`. A reader therefore always sees either the complete old file or the complete new file — never a partial one. On any failure the temp file is removed so no `.state-*.json` debris accumulates. No file lock is used; correctness comes from the atomic rename plus `UpdatedAt` comparison.

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
hooks: {}    # optional phase hooks (same format as settings.json)
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

## Phase Hooks

Phase hooks let you run shell commands automatically before or after a phase transition — useful for spinning up Docker containers, seeding test databases, or cleaning up after testing. Hooks are executed by the CLI, not by any AI role.

### Configuration

Hooks are declared in `settings.json` under the `hooks` key. The key format is `pre_{phase}` or `post_{phase}`:

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

Each entry is a `HookEntry` with two fields:

| Field | Type | Description |
|---|---|---|
| `run` | string | Shell command executed via `sh -c` |
| `on_fail` | string | `"block"` (default) or `"warn"` (case-insensitive) |

Feature YAML files can also declare a `hooks` field with the same format. When a feature defines hooks for the same key as the global config, the feature's definition **replaces** the global one entirely (no merging within a key).

### Execution Order

```
pre_{target_phase} hooks (in array order)
  ↓ any on_fail=block hook fails → transition to needs-attention, abort
state.Transition()
  ↓
record transition event
  ↓
post_{target_phase} hooks (in array order)
  ↓ on_fail=block hook fails → transition to needs-attention (no rollback)
```

### Failure Behavior

| `on_fail` | Hook fails | Effect |
|---|---|---|
| `block` (default) | pre hook | Feature moved to `needs-attention`; phase transition aborted |
| `block` (default) | post hook | Phase already changed; feature moved to `needs-attention` |
| `warn` | either | Result logged; execution continues |

### Logging

Each hook execution appends a `type: "hook"` event to `events.jsonl`:

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "command": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

Full stdout/stderr output is written to `.4x/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`.

### Hook Merging (`MergeHooks`)

Global and feature hooks are merged by `MergeHooks`: all global keys are copied, then feature keys override same-named global keys entirely. Keys that appear only in global are preserved. Both nil returns nil.

---

## Pending Review Gate

The loop does **not** go directly to `done`. After accepting, the feature enters `pending-review` — waiting for a human to review the AI's work.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

This ensures a human always signs off before a feature is considered complete.
