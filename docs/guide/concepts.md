# Core Concepts

## The Four Roles

| Role | Responsibility | Inputs | Outputs | Cannot |
|---|---|---|---|---|
| **Designer** | Analyze requirements, produce spec, define acceptance criteria and test strategy | Feature description, codebase | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | Modify source code |
| **Coder** | Implement what the spec says | `task-brief.md`, prior test/review reports | Source code, `coder-report.md` | Modify acceptance criteria or test scripts |
| **Reviewer** | Catch bugs, security issues, spec violations | Diff, spec, coder report, project rules | `review-report.md` | Modify source code |
| **Tester** | Validate against acceptance criteria with evidence | Acceptance criteria, coder report, test strategy | Test scripts, `test-report.md`, `verify.json`, `final-report.md` | Modify source code |

Each role is **isolated** — the Coder never sees prior review feedback during implementation. The Tester validates against criteria written by the Designer, not the Coder.

### Additional Loop Roles

Two additional roles operate later in the loop:

| Role | Phase | Responsibility |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | Adversarial review — finds the worst-case bugs across the full diff |
| **Fixer** | `fixing` | Fixes WARNING/INFO issues from deep-review-report.md without triggering a full amending round |
| **Acceptor** | `accepting` | Aggregates remaining open issues into `final-report.md` for human review |

The Acceptor uses its own dedicated model configuration (`roles.acceptor.model`) — distinct from the Designer. It reads the final round's review/test/deep-review reports plus any escalations to surface still-open issues, rather than re-reading every round's reports in full.

### Pipeline Profiles

A **pipeline profile** is a list of **phases**, and per phase it can override the **runner** and **model** to use. Simple work skips phases instead of always running the full six-phase pipeline, and different phases can run on different runners/models. Built-in profiles:

| Profile | Phases |
|---|---|
| `full` | designing, coding, reviewing, deep-reviewing, fixing, testing, accepting |
| `normal` | coding, reviewing, testing, accepting |
| `quick` | coding, reviewing |

Profile phases may only be chosen from the selectable whitelist (`designing`, `coding`, `reviewing`, `deep-reviewing`, `fixing`, `testing`, `accepting`); `coding` is always required. A phase not in the active profile is skipped — the loop transitions along the same valid state edges without invoking that runner.

**Selection** (high→low): `--profile` wins; then the feature YAML's `profile` field (a named profile that must exist in `profiles` or the built-ins, else an error — lets `4x batch run` apply a different profile per feature); then `default_profile`; then priority-based auto-select when a `profiles` section exists (highest priority → `full`, then `normal`, then `quick`), else `full`. On an interactive terminal 4x prompts with a numbered menu (default `default_profile`) when none of the higher-priority sources resolve it.

**Per-phase runner/model precedence** (high→low): this-run-only `--phase-override <phase>:<runner>:<model>` (also sent by the dashboard run dialog; never persisted) > manual `--runner` > the feature YAML's `phase_overrides.<phase>.{runner,model}` > the profile's per-phase `runner`/`model` > `default_runner` / the role's configured model (`roles.<role>.model` → runner model → default tier). The dashboard run dialog previews the merged result via `POST /api/run/preview`, which shares the same `ResolvePipeline` resolution path the run loop uses, so the preview matches what actually executes.

The legacy `roles: []` / `coder_model` profile format is still parsed and normalized into phases on load (backward compatible). See [Configuration](configuration.md) for the `profiles`, `default_profile`, `parallel_review_test`, and phase-override settings.

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
| **sub-reviewer** (×N) | deep model (`roles.reviewer.deep_model`) | the diff + its assigned angle subset | `deep-review-partial-{i}.md` |
| **synthesizer** | synthesizer model (`roles.synthesizer.model`, defaults to `sonnet` tier) | every partial report's full content | `deep-review-report.md` |

Angles are split evenly and without overlap: with the default `parallel_reviewers: 3` the groups are `[1–4]`, `[5–8]`, `[9–11]` (correctness / quality+convention / history+feedback). Set `roles.deep-reviewer.angles_per_reviewer` to fix the group size explicitly; leave it empty for automatic `ceil(11/N)` balancing. The N sub-reviewers run in parallel, then a single synthesizer de-duplicates, arbitrates conflicts, and unifies the confidence scoring into the same `deep-review-report.md` format the self-heal loop and `parseReviewVerdict` already consume — so everything downstream is unchanged.

When `parallel_reviewers` is unset or `≤ 1`, the loop falls back to the original single-agent flow: one deep reviewer renders all 11 angles and writes `deep-review-report.md` directly, with no partial reports or synthesizer.

### Selective Deep Review Angles

Before dispatching the deep review, 4x analyzes the diff-affected file paths and selects which of the 11 angles to run. The `angle_mapping` field in `roles.deep-reviewer` maps path prefixes (e.g. `internal/state/`) and suffix patterns (e.g. `*_test.go`) to angle numbers. For each changed file the longest matching prefix wins (prefix rules take precedence over suffix rules); the union of all matched angles becomes the selected set. When no file matches any rule, all 11 angles run as a safety fallback.

The selection is recorded in `deep-review-angles.json` in the round directory, including which files matched which rules and which angles each contributed. This artifact is also used by crash recovery to determine the correct partial count.

To force all 11 angles regardless of the mapping:
- Pass `--all-angles` to `4x run`
- Set `deep_review_all_angles: true` in the feature YAML

The `angle_mapping` can be customized in `settings.json` under `roles.deep-reviewer`; when not set, a built-in default covers the standard project layout (`internal/state/`, `internal/protocol/`, `cmd/`, `docs/`, `templates/`, `dashboard/`, `*_test.go`).

### Deep Review SubPhase & Crash Recovery

The `deep-reviewing` phase runs several internal steps (sub-reviewer → synthesizer → mini-coder → re-verifier), but they are **not** state-machine phases. To make the live progress and crash recovery aware of *which* step is running, `State` carries a `subPhase` field (`internal/protocol/state.go`) that is only meaningful while `phase == deep-reviewing`:

| `subPhase` | Step | Set when |
|---|---|---|
| `reviewing` | sub-reviewers (or single-agent fallback) are scanning the diff | entering deep review |
| `synthesizing` | the synthesizer is merging the partial reports | synthesizer spawned |
| `fixing` | the mini-coder is repairing blocking issues | self-heal mini-coder spawned |
| `reverifying` | the re-verifier is confirming the fix | self-heal re-verifier spawned |

`WriteState` enforces a single invariant: any write whose `phase` is not `deep-reviewing` clears `subPhase` to the empty string (`omitempty` keeps it out of `state.json` entirely). So leaving deep review — to `accepting`, `amending`, or `needs-attention` — never leaves a stale sub-phase behind, regardless of which exit path is taken.

On crash recovery, `smartResumePhase` no longer restarts deep review from scratch when `deep-review-report.md` is incomplete. It inspects the on-disk artifacts and resumes from the right step:

- **Any `deep-review-partial-{i}.md` missing or incomplete** → resume at `reviewing`; the parallel loop only re-spawns the sub-reviewers whose partials are missing (`missingDeepPartials`), reusing each index's original angle group so nothing is re-assigned.
- **All partials present but the report incomplete** → resume at `synthesizing`; the sub-reviewers are skipped and only the synthesizer re-runs.
- **Report complete but FAILed** → unchanged behavior: route to `amending` with `subPhase` cleared.

A partial is judged complete by `deepPartialComplete` — the file exists, is non-empty, and contains the `## Statistics` sentinel section the deep-reviewer template always emits, so a half-written partial is never mistaken for a finished one. This minimal-rerun recovery avoids re-spending the (expensive) deep model on steps that already completed before the crash.

### Auto-Discovered Features

A deep reviewer often spots issues that are real but **outside the current feature's scope** — a latent bug, tech debt, a missing capability. Without a place to land, those notes get buried in the report. When `auto_discover_features` is enabled, the run loop captures them automatically.

The deep reviewer writes each out-of-scope candidate as a `[NEW-FEATURE] <title>` block (followed by a short description) in the `## Discovered Issues` section of `deep-review-report.md`. After a **final deep review PASS** (the only two return paths that reach `accepting` — first-pass PASS, and a self-heal re-verifier flipping to PASS), the loop parses those blocks and, entirely in the CLI layer (no LLM call):

- **Dedupes** each candidate against existing features and against already-kept candidates, using a Jaccard token-overlap similarity check.
- **Caps** the count at `max_discovered_features` (default `3`); the rest are recorded as capped.
- **Creates** the kept candidates as new feature YAMLs (status `not-started`, reusing the same numbering as `4x new`), appending a `feature-discovered` event per creation.
- **Summarizes** the outcome (created / skipped-as-duplicate / capped / enrichment-failed) to `.4x/run/{feature-id}/discovered-features.md`.

The step is best-effort: any error is logged and never blocks the transition to `accepting`. It runs only on the final deep review PASS — intermediate rounds and FAIL/`needs-attention` paths never reach it. See [Configuration → Auto-Discover Features](configuration.md#auto-discover-features) for the settings.

#### Enrichment

A raw candidate carries only a title and a one-line description — too thin for a Designer to produce a high-quality task-brief. When `enrich_discovered_features` is enabled, each kept candidate is first passed through an **LLM enrichment** step before it lands in the backlog. Enrichment is delegated through the existing `runner.Runner` interface (the CLI layer never calls an LLM directly), reusing the deep-review runner with the cheaper reviewer model, and:

- Gathers heavyweight context — the existing feature list, the project directory tree, and code snippets grepped by keywords extracted from the candidate title.
- Asks the runner to return a structured JSON block (`[ENRICHMENT-RESULT] … [/ENRICHMENT-RESULT]`) with inferred `subtasks` (≥ 2 independently verifiable), `repos`, `rules`, `priority`, and a polished `description`.
- **Discards** any candidate whose response is malformed JSON or yields fewer than 2 subtasks (recorded under *Enrichment Failed* in the summary) — a thin candidate is not worth polluting the backlog.

The resulting feature's status depends on `enrich_auto_approve`: `true` (fully automatic) saves it as `not-started`; `false` saves it as `draft`, holding it out of the meta-loop until a human runs `4x approve` (→ `not-started`) or `4x reject` (→ `abandoned`). When `enrich_discovered_features` is disabled (or no runner is available), the loop falls back to the original thin-feature path for full backward compatibility.

### History Miner & Candidate Pool

Auto-Discovered Features only fires on a **final deep review PASS**, and only parses the `[NEW-FEATURE]` blocks of that single round's `deep-review-report.md`. The richest signal — the *failures* — never gets harvested: an `escalation.json`, a feature stuck in `needs-attention`/`abandoned`/`blocked`, or the same reviewer FAIL issue recurring across many features.

The `4x mine` command closes that gap. It scans the **entire** `.4x/` directory for historical failure signals and aggregates them into a candidate pool at `.4x/candidates.json`. It is a pure CLI/protocol-layer command — **no LLM call**, just mechanical scanning plus the same Jaccard token-overlap dedup used by Auto-Discovered Features. Three scanners feed the pool, each tagging every candidate with a `Source` and an `Origin` traceability string:

| Source | Signal | Origin format |
|---|---|---|
| `escalation` | each round's `escalation.json` with `needed: true`, classified by `reason` (spec-mismatch / criteria-wrong / blocker / scope-change) | `<featureID> round-<n> <reason>` |
| `stuck` | features whose `state.json` phase is `needs-attention`, `abandoned`, or `blocked`; blocker reason taken from `stopReason`/`stopMessage`, falling back to the latest round's escalation `detail` | `<featureID> <phase>` |
| `fail-pattern` | reviewer/deep-reviewer FAIL issue titles that recur across **distinct** features (same feature's multiple rounds count once), clustered by Jaccard similarity and gated by `--min-occurrences` (default `3`) | `N features: <ids>` |

A recurring fail-pattern also emits a `CandidateLearning` (category `review`) suggesting the issue be promoted into a review checklist or template.

The output `CandidatePool` (`candidates.json`) holds `Version`, `GeneratedAt`, a list of `Candidate`s, and a list of `CandidateLearning`s. Before writing, candidates are deduped three ways: against existing feature YAMLs, against the previous `candidates.json`, and within the current batch. Flags: `--min-occurrences` (fail-pattern threshold), `--output` (default `.4x/candidates.json`), and `--dry-run` (print the summary without writing).

The whole command is best-effort — a single corrupt feature is logged and skipped, never aborting the scan. Crucially, `4x mine` **only produces the candidate pool; it never creates features**. Whether a candidate is promoted into a real feature is left to a separate gate (F097). This makes it complementary to — not a replacement for — Auto-Discovered Features: one harvests in-scope notes on success, the other harvests failure signals across all of history.

### Evolve Driver

`4x evolve` stitches mine, the F097 value gate, and enrichment into one repeatable closed loop: **mine → gate (pre → gate LLM role → post) → enrich → enqueue → (optional) auto-run → learnings feed the next round**. The CLI layer stays LLM-free — the gate role and enrichment both run as `runner.Runner` subprocesses, never an inline API call.

The pipeline order is **mine → gate → enrich → enqueue** (not mine → enrich → gate): the gate consumes bare `Candidate`s, so enrichment — which materializes a full `feature.Feature` — only runs on gate survivors, never wasting LLM cost on candidates that get vetoed. Accepted candidates are enqueued as `not-started` feature YAMLs (passing the value gate **is** the approval; there is no second draft→not-started step). If enrichment fails or is discarded, the candidate is still enqueued as a bare feature built from its description, marked `enriched=false` — the gate already vouched for its value.

Each call runs exactly **one** round; repeated rounds are driven externally (cron or repeated invocation). Every round writes `.4x/evolve-report.md` (Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted), surfaced by the dashboard via `GET /api/evolve-report`.

**Anti-spin halt** prevents the loop spinning forever with nothing to show. `.4x/evolve-state.json` persists `consecutiveNoAccept` across calls; a round that accepts nothing increments it, a round that accepts anything resets it to zero. Once it reaches `evolution.max_idle_rounds` the next call halts before mining, marks the report `Halted`, and exits 0. The setting distinguishes **unset** (`nil` → default `3`) from an explicit `<= 0` (disables the halt — always run); `--force` overrides a halt for one call.

With `--auto-run`, each enqueued feature's meta-loop runs immediately, always under the F098 self-mod scope guard: a feature that touches `self_mod_guard.protected_paths` without approval is not auto-completed and is flagged `SelfModBlocked` in the report (resolve with `4x done --approve-self-mod`). `--dry-run` is read-only — it prints the mine/dedupe summary and writes nothing, spawns no runner, and creates no feature (and ignores `--auto-run` with a warning).

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
init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → fixing → accepting → pending-review → done
                              ↓          ↑          ↓           ↓            ↓
                              └──────────┴── amending ←─────────┴────────────┘
                                         ↑      ↓
                                         └──────┘
```

### All Valid Transitions

| From | To |
|---|---|
| `init` | `designing` |
| `designing` | `design-reviewing` |
| `design-reviewing` | `coding`, `designing` |
| `coding` | `reviewing`, `designing` |
| `reviewing` | `testing`, `amending` |
| `amending` | `reviewing`, `designing` |
| `testing` | `deep-reviewing`, `amending`, `designing` |
| `deep-reviewing` | `fixing`, `accepting`, `amending` |
| `fixing` | `accepting`, `amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `design-reviewing`, `coding`, `fixing`, `testing` |
| `needs-attention` | `designing`, `design-reviewing`, `coding`, `fixing`, `testing` |
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
| `coding` / `amending` | `build-gate.json` missing or `passed=false` | → `needs-attention` (orchestrator safety net; normally the Coder agent self-fixes via `4x check` loop) |
| `reviewing` | Review not passed (requires explicit `PASS` or `CONDITIONAL PASS` verdict AND zero `[CRITICAL]`/`[WARNING]` issues in the report) | → `amending` |
| `testing` | `verify.json` not passed or artifacts missing | → `amending` |
| `testing` | Guard gate retryable errors only (e.g., missing `manual_check_results` or AC evidence) | Auto-retry tester once with `guard-feedback.json`; second failure → `needs-attention` |
| `deep-reviewing` | Deep review FAILs | self-heal in place (mini-coder + re-verifier), up to `max_fix_rounds`; PASS → `fixing`, otherwise → `needs-attention` |
| `fixing` | `escalation.json` with `blocker` | → `needs-attention`; `fixer-report.md` present → `accepting` |
| any (non-designer) | Guard check finds scope violation, baseline drift, or missing required file | → `needs-attention` (except retryable testing errors, which auto-retry once) |

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
├── learnings.json                  # Retro learnings store (accumulated across features)
├── learnings-context.md            # Auto-generated active learnings snapshot (@ included by CLAUDE.md)
├── templates/                      # Optional project-level prompt template overrides (4x init --dump-templates)
└── run/                            # Runtime artifacts (per-feature working directories)
    └── {feature-id}/
        ├── state.json                   # Phase, role, round, active, runner, runners, stopReason, profile, baseCommit
        ├── events.jsonl                 # Audit trail
        ├── baseline.json                # Pre-coding snapshot (HEAD, branch, dirty files)
        ├── task-brief.md                # Designer → Coder: spec + architecture
        ├── retro-learnings.json         # Acceptor → CLI: harvested into learnings.json
        ├── acceptance-criteria.md       # Designer → Tester: testable criteria
        ├── test-strategy.yaml           # Designer → Tester: test approach
        ├── final-report.md              # End-of-loop summary
        ├── logs/
        │   ├── round-{N}-{role}.log              # Per-round per-role execution log
        │   ├── round-{N}-deep-reviewer-{i}.log   # Per parallel sub-reviewer (when fanned out)
        │   └── round-{N}-synthesizer.log         # Synthesizer merging the partial reports
        └── rounds/round-{N}/
            ├── coder-report.md            # What the Coder did
            ├── review-package.md          # Orchestrator-budgeted diff (commits/stat/full diff) for reviewer/deep-reviewer, replacing a self-run git diff
            ├── review-report.md           # Reviewer findings + verdict
            ├── test-report.md             # Tester results
            ├── deep-review-partial-{i}.md # One parallel sub-reviewer's findings (when fanned out)
            ├── deep-review-report.md      # Merged deep review (synthesizer output, or single-agent)
            ├── acceptance-summary.md      # Orchestrator-generated digest of verify/review/deep-review reports for the Acceptor
            ├── verify.json                # {passed, round, role, commands[], ac_results[], manual_check_results[]}
            ├── build-gate.json            # Build+lint gate results (written by 4x check in coding/amending phase)
            ├── guard-feedback.json        # Guard retry errors (written on retryable guard failure)
            └── escalation.json            # {needed, reason, detail}
```

`review-package.md` and `acceptance-summary.md` are best-effort budgeting artifacts the orchestrator writes to cut down on the reviewer/deep-reviewer/acceptor re-deriving context from scratch. On first entering `coding` (round 1), the loop captures the current HEAD as `state.json`'s `baseCommit` (mono-repo only; multi-repo relies on each repo's `baseline.json` instead). On every `coding`/`amending` → `reviewing` transition, it writes `review-package.md` with the commits/file-stat/full diff since `baseCommit`. Before entering `accepting`, it regenerates `acceptance-summary.md` from the round's `verify.json`, `review-report.md`, and `deep-review-report.md`. Both are generated on a best-effort basis — if there's nothing to summarize yet (e.g. no `baseCommit`) the file is simply not written, and the corresponding templates (`reviewer.md.tmpl`, `deep-reviewer.md.tmpl`, `acceptor.md.tmpl`) fall back to reading the original sources themselves.

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

### Worktree Path Recovery

When a feature runs in worktree isolation, the loop prints `worktree: <path>` on startup, which is recorded in `events.jsonl` as a `run-output` event. `Workspace.WorktreePath` recovers that path later (e.g. for screenshot discovery) by scanning the audit trail rather than re-running git.

The scan reads the **entire** `events.jsonl` and returns the path from the **last** matching `run-output` event. This matters for re-runs: each `4x run` appends a fresh `worktree: …` event, so the file accumulates entries over the feature's lifetime. Reading only the first few lines would either miss the path once enough events pile up, or return a stale worktree that has since been removed. Taking the last match always yields the most recent run's worktree.

### Authoritative Cost Totals

`Workspace.TotalCost` follows the same "scan the entire audit trail" pattern as `WorktreePath` above, but sums instead of taking the last match: it reads every `run-end` event in `events.jsonl` and adds up each one's `cost_usd`. This is the single source of truth for a feature's total spend, used by both the CLI (`4x run`'s summary, seeded into `orchestrator.NewRunner` so a resumed run's total includes prior process's cost) and the dashboard (`/api/messages/{id}`'s `totalCostUSD` field). A missing `events.jsonl` (new feature, no runs yet) returns `(0, nil)`; a malformed line is skipped rather than aborting the whole scan, since one bad line shouldn't hide the cost of every other run.

### Screenshot Discovery Resilience

`Workspace.DiscoverScreenshots` reads every round's `verify.json` to collect Tester-recorded screenshot evidence. A single round's `verify.json` can end up malformed — e.g. a captured subprocess's raw ANSI escape codes leaking into the file — which would otherwise fail JSON parsing. Rather than propagating that as a hard error and taking down the whole discovery call (and with it `4x status`/`4x check` for the entire feature), a parse failure is treated as best-effort: that round contributes no verify-sourced screenshots, but its round number is still tracked and every other round's evidence — plus the directory scan fallback — is processed normally.

### Workspace Read Cache (Dashboard Server)

The CLI is a short-lived process: each command reads the `.4x/` files it needs once and exits, so it always uses a plain `*protocol.Workspace`. The dashboard server (`4x live`) is the opposite — it is long-running and every API request re-reads the same files. In a multi-project × multi-feature workspace (e.g. 5 projects × 50 features) a single request can trigger hundreds of YAML/JSON parses.

To avoid that, the server wraps each workspace in a `*protocol.CachedWorkspace` (`internal/protocol/cached.go`), an mtime-based in-memory cache over the read-only operations declared by the `WorkspaceReader` interface (`internal/protocol/reader.go`):

- **`ReadConfig`** — caches `settings.json`; `os.Stat` compares the file mtime, re-parsing only when it changes.
- **`ListFeatures`** — caches the full feature list; `os.ReadDir` compares the `.yaml` file set and each file's mtime, re-parsing only when a file is added, removed, or modified. Returns a copy so callers can mutate freely. Uses loose validation: features with format issues (e.g. unrecognized subtask status) are still included with `Warnings` populated, rather than being silently skipped.
- **`LoadFeature`** — caches each feature by id, keyed on the YAML's mtime. Uses strict validation — returns an error for any format issue.
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
deep_review_all_angles: false  # force deep review to run all 11 angles (ignores angle_mapping)
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
| **Build gate** | In coding/amending phase: runs `settings.json` build + lint commands, writes `build-gate.json`. Failure blocks the round; the Coder agent should fix and re-run `4x check` |
| **Testing → Accepting gate** | Requires `verify.json` (passed=true), `test-report.md`, `final-report.md`. If `test-strategy.yaml` defines `manual_checks`, each must have a corresponding `manual_check_results` entry with non-empty evidence. If `test-strategy.yaml` defines `ac_verify_map`, execution-type ACs (unit-test / integration) must have command output in evidence; inspection ACs need non-empty evidence; skip ACs are not checked. ACs not listed in the map default to execution. ACs declared `e2e-screenshot` must have at least one screenshot file that actually exists on disk for the round (checked in both the custom screenshot dir and the default `.4x/` dir), on top of the usual passed+evidence checks — this only verifies a screenshot was captured, not that its contents match the AC; ACs that just need an API call verified should stay on `execution` instead of a dedicated api-only type |
| **Self-mod guard** | Layered on top of Scope (does not replace it): flags file-level changes to protected paths (default `internal/state/`, `internal/guard/`, `internal/protocol/`), blocks the round when the per-round protected diff exceeds the budget, requires accompanying tests before accepting, and blocks auto-merge until manually approved |

Run manually with `4x check <feature-id>`.

### Self-mod guard

When 4x runs on itself (meta-loop), changes to its own core foundation (state machine / guardrails / protocol) are riskier than ordinary feature work — a regression there breaks the whole multi-role loop. The self-mod guard adds an extra layer on top of the repo-level Scope guard, configured under `self_mod_guard` in `settings.json`:

```json
"self_mod_guard": {
  "protected_paths": ["internal/state/", "internal/guard/", "internal/protocol/"],
  "max_diff_lines": 200,
  "require_tests": true
}
```

- `protected_paths` — path-prefix allowlist (relative to scope root); changes under these are flagged. Defaults to the three architecture red lines when unset.
- `max_diff_lines` — per-round protected diff budget; exceeding it fails the guard and drops the feature to `needs-attention`. Defaults to `200`.
- `require_tests` — when `true` (default), protected `.go` changes must ship with protected `_test.go` changes before the feature can leave `testing`.

A touch is detected once during the post-coding guard check and persisted to `state.json` (`selfModTouched` / `selfModPaths`). Touching protected paths never auto-merges: `4x done` / `4x merge` block until you re-run with `--approve-self-mod`, which records `selfModApproved` in state.

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

Full stdout/stderr output is written to `.4x/run/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`.

### Hook Merging (`MergeHooks`)

Global and feature hooks are merged by `MergeHooks`: all global keys are copied, then feature keys override same-named global keys entirely. Keys that appear only in global are preserved. Both nil returns nil.

---

## Health Check

Before the Tester role starts, the CLI can automatically verify the environment is healthy — that the build passes, services are up, endpoints respond. A broken environment caught here saves a whole wasted test cycle. Health checks are executed by the CLI, not by any AI role, and run only when entering the `testing` phase, after `pre_testing` hooks and before the Tester runner is spawned.

### Configuration

A health check has three fields (`HealthCheck` in `internal/protocol/verify.go`):

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

The Designer lists profiles in `test-strategy.yaml` (`TestStrategy.Profiles` in `internal/protocol/verify.go`):

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` is `omitempty` — a `test-strategy.yaml` without it behaves exactly as before (no injection).

### Manual checks

For AC items that need runtime verification beyond build/test/lint, the Designer can add `manual_checks` to `test-strategy.yaml` (`TestStrategy.ManualChecks` in `internal/protocol/verify.go`):

```yaml
manual_checks:
  - id: mc-1
    ac_ref: AC-3
    description: "驗證 routing 正確分流"
    steps:
      - "啟動 server: go run ./cmd/gate --port 8080"
      - "curl http://localhost:8080/health → 確認 200"
  - id: mc-2
    ac_ref: AC-5
    description: "驗證 graceful shutdown"
    steps:
      - "啟動 server 並送 SIGTERM"
      - "確認 exit code 為 0"
```

The Tester must execute each step and record actual output as evidence in `verify.json` under `manual_check_results` (`VerifyEvidence.ManualCheckResults`). The guard blocks `testing → accepting` if any manual check has no result or empty evidence. If the failure is retryable, the tester gets one automatic retry with guard errors injected via `guard-feedback.json`; a second failure escalates to `needs-attention`.

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

## Issue-First MR Flow

Setting `"issue_tracker": {"enabled": true}` in `settings.json` (default `false`, project-level only) switches `4x new`/`4x done` to hand code review off to GitHub/GitLab instead of merging locally. The platform (GitHub vs. GitLab, including self-hosted) is auto-detected per repo from its `origin` remote hostname — no per-repo platform setting needed. `gh`/`glab` must be installed and authenticated.

- `4x new` preflights `gh`/`glab` for every declared repo before creating the feature — any failure aborts creation. It then creates a new issue per repo (or links an existing one via `--issue "repo:id-or-url"`), recording `{repo, id, url}` in the feature YAML's `issues` field. A per-repo creation/link failure is recorded as a warning (`warnings` field, also printed) and does not block feature creation — partial success is fine.
- `4x done` pushes the feature branch and opens a MR/PR (body includes `Closes #<issue-id>` when an issue exists for that repo) for every repo with committed changes, instead of the local squash-merge. `done` then means "MR opened, awaiting platform review", not "merged" — nothing polls or waits for the actual merge. Opened URLs print as `MR opened[(repo)]: <url>` and appear as `mrUrls` in `--json` output. If a repo fails to push or open its MR, the feature stays `pending-review` with the worktree preserved so a retry can pick up where it left off (opening a MR for a branch that already has one is idempotent — it returns the existing MR's URL).
- This flow only triggers from an explicit `4x new` — the dashboard's "New Feature" form and auto-discovered/evolved features never create issues.

When `issue_tracker.enabled` is `false` (the default), none of the above applies: `4x new`/`4x done` behave exactly as documented elsewhere in this page.

## Pending Review Gate

The loop does **not** go directly to `done`. After accepting, the feature enters `pending-review` — waiting for a human to review the AI's work.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

This ensures a human always signs off before a feature is considered complete.
