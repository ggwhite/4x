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

### Parallel Deep Review

The deep review covers 11 distinct angles (correctness, quality, convention, history, feedback, …). When `roles.deep-reviewer.parallel_reviewers` is greater than 1, the loop fans the angles out across several focused sub-reviewers instead of asking one agent to cover all 11. This mirrors how `/code-review` splits a review by dimension, lowering each agent's context pressure and attention drift.

The fan-out is driven entirely by the 4x CLI — it does not rely on the LLM's own subagent or tool abilities. The `deep-reviewing` phase stays a single phase:

| Sub-role | Model | Reads | Writes |
|---|---|---|---|
| **sub-reviewer** (×N) | deep model | the diff + its assigned angle subset | `deep-review-partial-{i}.md` |
| **synthesizer** | deep model | every partial report's full content | `deep-review-report.md` |

Angles are split evenly and without overlap: with the default `parallel_reviewers: 3` the groups are `[1–4]`, `[5–8]`, `[9–11]` (correctness / quality+convention / history+feedback). Set `roles.deep-reviewer.angles_per_reviewer` to fix the group size explicitly; leave it empty for automatic `ceil(11/N)` balancing. The N sub-reviewers run in parallel, then a single synthesizer de-duplicates, arbitrates conflicts, and unifies the confidence scoring into the same `deep-review-report.md` format the self-heal loop and `parseReviewVerdict` already consume — so everything downstream is unchanged.

When `parallel_reviewers` is unset or `≤ 1`, the loop falls back to the original single-agent flow: one deep reviewer renders all 11 angles and writes `deep-review-report.md` directly, with no partial reports or synthesizer.

### Auto-Discovered Features

A deep reviewer often spots issues that are real but **outside the current feature's scope** — a latent bug, tech debt, a missing capability. Without a place to land, those notes get buried in the report. When `auto_discover_features` is enabled, the run loop captures them automatically.

The deep reviewer writes each out-of-scope candidate as a `[NEW-FEATURE] <title>` block (followed by a short description) in the `## Discovered Issues` section of `deep-review-report.md`. After a **final deep review PASS** (the only two return paths that reach `accepting` — first-pass PASS, and a self-heal re-verifier flipping to PASS), the loop parses those blocks and, entirely in the CLI layer (no LLM call):

- **Dedupes** each candidate against existing features and against already-kept candidates, using a Jaccard token-overlap similarity check.
- **Caps** the count at `max_discovered_features` (default `3`); the rest are recorded as capped.
- **Creates** the kept candidates as new feature YAMLs (status `not-started`, reusing the same numbering as `4x new`), appending a `feature-discovered` event per creation.
- **Summarizes** the outcome (created / skipped-as-duplicate / capped) to `.4x/{feature-id}/discovered-features.md`.

The step is best-effort: any error is logged and never blocks the transition to `accepting`. It runs only on the final deep review PASS — intermediate rounds and FAIL/`needs-attention` paths never reach it. See [Configuration → Auto-Discover Features](configuration.md#auto-discover-features) for the settings.

### Escalation

The Coder or Tester can escalate when:

| Reason | Meaning | Routes to |
|---|---|---|
| `spec-mismatch` | DB/API doesn't match spec | Designer |
| `criteria-wrong` | Acceptance criteria are incorrect | Designer |
| `blocker` | Missing dependency or infra issue | `needs-attention` (human intervention) |
| `scope-change` | Need to modify repos outside scope | Designer |

Escalation is written to `escalation.json`. The loop automatically routes `spec-mismatch`, `criteria-wrong`, and `scope-change` back to the Designer. A `blocker` escalation goes to `needs-attention` for human intervention.

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
| `designing` | `task-brief.md` or `acceptance-criteria.md` missing | → `needs-attention` |
| `coding` / `amending` | `escalation.json` with `spec-mismatch`, `criteria-wrong`, or `scope-change` | → `designing` |
| `reviewing` | Review not passed (requires explicit `PASS` or `CONDITIONAL PASS` verdict AND zero `[CRITICAL]`/`[WARNING]` issues in the report) | → `amending` |
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
├── batch-pid                        # PID of running batch subprocess (server orphan adoption)
├── batch-conflict.json              # Batch auto-merge conflict signal (paused)
├── batch-report.json                # Last batch run report (stats + per-feature outcome)
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
    │   ├── round-{N}-{role}.log              # Per-round per-role execution log
    │   ├── round-{N}-deep-reviewer-{i}.log   # Per parallel sub-reviewer (when fanned out)
    │   └── round-{N}-synthesizer.log         # Synthesizer merging the partial reports
    └── rounds/round-{N}/
        ├── coder-report.md            # What the Coder did
        ├── review-report.md           # Reviewer findings + verdict
        ├── test-report.md             # Tester results
        ├── deep-review-partial-{i}.md # One parallel sub-reviewer's findings (when fanned out)
        ├── deep-review-report.md      # Merged deep review (synthesizer output, or single-agent)
        ├── verify.json                # {passed, round, role, commands[]}
        └── escalation.json            # {needed, reason, detail}
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

- **`batch-report.json`** — written when a batch run ends (normally, stopped, interrupted, or crashed). Unlike the two signal files above it persists between runs as the "last batch report" the dashboard shows when no batch is active. It records the `outcome`, overall counts (`total` / `completed` / `failed` / `remaining`), the runner, total duration, and a per-feature breakdown (final status, rounds, stop reason); a `crashed` outcome also carries `panicMessage`. Written atomically (temp file + rename) so the dashboard never reads a half-written report.

### Atomic State Writes

`state.json` is read and written by multiple actors concurrently — the run loop, the dashboard server, and background reconcilers. To avoid a reader ever seeing a truncated or half-written file, `WriteState` never writes in place. It marshals the state, writes it to a temp file (`.state-*.json`) **in the same directory** (guaranteeing the same filesystem so the rename is atomic), then `os.Rename`s it over `state.json`. A reader therefore always sees either the complete old file or the complete new file — never a partial one. On any failure the temp file is removed so no `.state-*.json` debris accumulates. No file lock is used; correctness comes from the atomic rename plus `UpdatedAt` comparison.

### Workspace Read Cache (Dashboard Server)

The CLI is a short-lived process: each command reads the `.4x/` files it needs once and exits, so it always uses a plain `*protocol.Workspace`. The dashboard server (`4x live`) is the opposite — it is long-running and every API request re-reads the same files. In a multi-project × multi-feature workspace (e.g. 5 projects × 50 features) a single request can trigger hundreds of YAML/JSON parses.

To avoid that, the server wraps each workspace in a `*protocol.CachedWorkspace` (`internal/protocol/cached.go`), an mtime-based in-memory cache over the read-only operations declared by the `WorkspaceReader` interface (`internal/protocol/reader.go`):

- **`ReadConfig`** — caches `settings.json`; `os.Stat` compares the file mtime, re-parsing only when it changes.
- **`ListFeatures`** — caches the full feature list; `os.ReadDir` compares the `.yaml` file set and each file's mtime, re-parsing only when a file is added, removed, or modified. Returns a copy so callers can mutate freely.
- **`LoadFeature`** — caches each feature by id, keyed on the YAML's mtime.
- **`ReadState`** — intentionally **not** cached (changes frequently, small file, fast parse); it falls through to the embedded `*Workspace`.

Invalidation is implicit: write methods (`SaveFeature`, `WriteState`, …) need not notify the cache because the next read detects the new mtime. The cache is opt-in — only the server constructs a `CachedWorkspace`; the CLI keeps using `*Workspace` with identical behaviour. Because Go embedding has no virtual dispatch, internal `*Workspace` method calls (e.g. `CompareBacklogMirror` calling `w.ListFeatures()`) still run the uncached original; this is acceptable since those paths are not server hot-paths.

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # numeric: 0-1 = full profile, 2 = normal, 3+ = quick (omit for nil/unset)
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # optional explicit path to the design spec (overrides docs/design/ lookup)
plan: ""     # optional explicit path to the implementation plan
hooks: {}    # optional phase hooks (same format as settings.json)
```

`status` mirrors `state.json` phase for quick listing. Valid values: `not-started`, `in-progress`, `ready-for-review`, `needs-attention`, `blocked`, `done`, `abandoned`. `abandoned` features are treated as completed (won't block dependencies) but display with strikethrough in the dashboard. `depends` lists feature IDs that must be done (or abandoned) before this feature can run. `repos` lists the repository names (from `workspace.repos`) that this feature touches; empty means all repos in scope.

#### Design Doc Resolution

The dashboard overview and the `4x prompt` planning-doc injection locate a feature's spec/plan through one shared resolver (`protocol.ResolveDesignDoc`), so both always see the same document. Resolution order per doc type (`spec`/`plan`):

1. The feature YAML `spec`/`plan` field, read as a path (relative paths resolve against the workspace root) when non-empty.
2. `docs/design/{feature.ID}-{type}.md`.
3. `docs/design/{slug}-{type}.md`, where `slug` strips the `FNNN-` prefix from the ID (only attempted when it differs from the ID).

The first existing file wins; if none match, the doc is treated as absent.

### Feature Creation

The `Feature`/`Subtask`/`Status` types and the creation logic live in the standalone `internal/feature` package (ID generation, backlog drift, screenshot helpers moved there too). `protocol.Workspace` and `protocol.CachedWorkspace` satisfy the `feature.Store` interface, and `feature` does not import `protocol` (one-way dependency, decoupled through `Store`). Both the CLI (`4x new`) and the dashboard (`POST /api/new`) create features through the single `feature.Create(store, opts)` entry point, so numbering, ID truncation, and default fields behave identically regardless of entry point.

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
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

Full stdout/stderr output is written to `.4x/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`.

### Hook Merging (`MergeHooks`)

Global and feature hooks are merged by `MergeHooks`: all global keys are copied, then feature keys override same-named global keys entirely. Keys that appear only in global are preserved. Both nil returns nil.

---

## Health Check

Before the Tester role starts, the CLI can automatically verify the environment is healthy — that the build passes, services are up, endpoints respond. A broken environment caught here saves a whole wasted test cycle. Health checks are executed by the CLI, not by any AI role, and run only when entering the `testing` phase, after `pre_testing` hooks and before the Tester runner is spawned.

### Configuration

A health check has three fields (`HealthCheck` in `internal/protocol/types.go`):

| Field | Type | Description |
|---|---|---|
| `commands` | `[]string` | Check commands run in order; any failure stops the run |
| `recovery` | `[]string` | Optional. Run in order when a check fails, to repair the environment |
| `timeout` | `int` | Per-command timeout in seconds; `<= 0` applies the default `30` |

It can be declared globally in `settings.json` (JSON, no yaml tag):

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

…or per-feature in `test-strategy.yaml` (read via `Workspace.ReadTestStrategy`):

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**Merging:** `ResolveHealthCheck` does whole-group override, not field-level merge. If `test-strategy.yaml` defines `health_check`, it replaces the global one entirely; otherwise the global config is used. When neither is set, the health check is skipped and the Tester starts immediately.

### Execution Flow

```
testing phase entered (pre_testing hooks already ran)
  ↓
run commands in order (each with its own timeout)
  ├─ all pass → start Tester
  └─ any fails →
      ├─ no recovery → escalate to needs-attention
      └─ has recovery → run recovery commands in order
          ├─ recovery fails → escalate to needs-attention
          └─ recovery passes → re-run all commands once
              ├─ pass → start Tester
              └─ still fails → escalate to needs-attention
```

Recovery is triggered at most once — there is no multi-retry or back-off loop.

### Failure Behavior

On final failure the run records a `type: "health-check-failed"` event (role `tester`, with the failing command and error in `detail`), transitions the feature to `needs-attention`, sets `StopReason` to `health-check-failed`, and stops the loop. Each command runs via `sh -c` under a per-command timeout; a timeout counts as a failure and its output is written to stderr for debugging.

---

## Test Profiles

A **test profile** is a reusable block of test methodology that the Designer tags on a feature so the Tester's prompt is auto-injected with the matching guidance — instead of hand-maintaining one giant `roles.tester.instructions` list in `settings.json` that every feature shares regardless of type.

> Not to be confused with **[pipeline profiles](#pipeline-profiles)** (`Config.Profiles`), which select *which roles run*. Test profiles (`Config.TestProfiles`) inject *test methodology content* into the Tester prompt only.

### Declaring profiles

The Designer lists profiles in `test-strategy.yaml` (`TestStrategy.Profiles` in `internal/protocol/types.go`):

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` is `omitempty` — a `test-strategy.yaml` without it behaves exactly as before (no injection).

### Built-in profiles

Four profiles ship embedded in the binary (`templates/profiles/*.md`, exposed via `templates.ProfilesFS`):

| Profile | Methodology |
|---|---|
| `unit` | Go `go test`, `t.TempDir()` isolation, table-driven, error cases, verify.json per AC |
| `web` | Playwright against `4x live` dashboard; headless, isolated workspace + random port, screenshots as evidence, no interference with the user's running server |
| `api` | HTTP endpoint testing — status codes, response body, edge cases, auth |
| `e2e` | End-to-end multi-service flows, DB state and cross-service consistency |

### Overriding in settings.json

A project can replace or extend any profile via `Config.TestProfiles` (`test_profiles`), keyed by profile name (`TestProfileOverride`):

```json
{
  "test_profiles": {
    "web": { "content": "用 Cypress 而非 Playwright 測試…" },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` — inline replacement text
- `include` — path (relative to workspace root) to a file whose contents are used

**Resolution order** (per profile name): `test_profiles[name].content` → `test_profiles[name].include` → built-in `profiles/{name}.md`. Override is a whole replacement, not a field-level merge. An unknown name (no override, no built-in) prints a warning to stderr and is skipped.

The Tester prompt renders each resolved profile as a `== Test Profile: {name} ==` block. Loading is implemented in `loadProfiles` / `resolveProfileContent` (`cmd/4x/prompt.go`).

---

## Pending Review Gate

The loop does **not** go directly to `done`. After accepting, the feature enters `pending-review` — waiting for a human to review the AI's work.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

This ensures a human always signs off before a feature is considered complete.
