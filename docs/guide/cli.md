# CLI Reference

All feature-id arguments support case-insensitive prefix matching. `4x run f001`, `4x run F001-user`, and `4x run F001` all resolve to `F001-user-authentication-w`. Ambiguous prefixes produce an error listing the matches.

---

## `4x init`

Initialize a `.4x/` workspace in the current directory.

```
4x init
```

- Auto-detects project language and build/test/lint commands
- Creates `~/.4x/settings.json` with 6 default runners (claude, codex, gemini, agy, copilot, cursor)
- Deploys embedded plugin files to `.4x/plugins/`
- Adds `@import` lines to root-level files (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md, .cursorrules)
- Errors if `.4x/` already exists

### `4x init --dump-templates`

Dump the built-in role prompt templates into `.4x/templates/` so a project can override them.

```
4x init --dump-templates          # write built-in templates to .4x/templates/
4x init --dump-templates --force  # overwrite existing template files
```

- Requires `.4x/` to already exist (run `4x init` first)
- Writes every embedded `*.md.tmpl` (including `locale.tmpl`) to `.4x/templates/`
- Existing files are skipped with a warning unless `--force` is given
- At prompt time, `.4x/templates/{file}` takes precedence over the embedded template (whole-file override); `locale.tmpl` and each role template fall back independently

---

## `4x new <title>`

Create a new feature with optional metadata.

```
4x new "Feature title" [flags]
```

| Flag | Description |
|---|---|
| `--id` | Custom slug for feature ID (skips auto-truncation) |
| `--desc` | Feature description (defaults to title) |
| `--subtask` | Subtask in `"id:name"` or `"id:name:description"` format (repeatable) |
| `--rule` | Rule reference (repeatable) |
| `--depends` | Dependency feature ID (repeatable) |
| `--priority` | Priority level (0=critical, 1=high, 2=medium, 3=low) |
| `--profile` | Pipeline profile written to the feature YAML (`full`/`normal`/`quick` or custom); applied per-feature on `4x batch run` |
| `--repo` | Repository in scope (repeatable) |
| `--json` | Output as JSON |

Creates `.4x/features/F{NNN}-{slug}.yaml` with status `not-started`.
Auto-generated slug truncates at word boundary; use `--id` to override.
Creation runs through the shared `feature.Create` path (see [Concepts](concepts.md#feature-creation)) — the dashboard's `POST /api/new` uses the same logic, so flags here map one-to-one to the dashboard's New Feature form.

Examples:
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

---

## `4x run <feature-id>`

Run the Design-Code-Review-Test loop for a feature.

```
4x run <feature-id> [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--runner` | config default | Runner plugin name |
| `--max-rounds` | `5` | Maximum loop iterations |
| `--timeout` | `3600` | Per-phase timeout in seconds |
| `--dry-run` | `false` | Print role prompts without calling LLM |
| `--json` | `false` | Start run and return JSON immediately |
| `--profile` | auto | Pipeline profile (`full`/`normal`/`quick` or custom); overrides `default_profile`/priority auto-select |
| `--phase-override` | none | Temporary per-phase runner/model override for this run only, format `<phase>:<runner>:<model>` (repeatable); not written back to settings or the feature YAML |
| `--no-notify` | `false` | Disable the OS notification on run completion (overrides the `notifications` config) |
| `--all-angles` | `false` | Force deep review to run all 11 angles, ignoring the diff-based angle mapping |

When the run ends (success, failure, or interruption), 4x sends a native OS notification (`osascript` on macOS, `notify-send` on Linux, PowerShell balloon on Windows). Pass `--no-notify` to suppress it, or set `"notifications": false` in `settings.json`. Missing notification tooling is silently ignored.

With `--json`, the loop runs in a detached background process and the command returns immediately with `{featureId, runner, maxRounds, pid, logPath}`. The background process's stdout/stderr are redirected to `logPath` (`.4x/<feature-id>/run.log`), so early errors (config load, worktree setup, runner not found) are recorded there instead of being lost.

`--profile` selects which phases run and, per phase, which runner and model. Built-in profiles: `full` (designing/design-reviewing/coding/reviewing/deep-reviewing/testing/accepting), `normal` (coding/reviewing/testing/accepting), `quick` (coding/reviewing). Phases not in the profile are passed through (state advances along the legal edge without invoking the runner); `coding` is always required. When `--profile` is omitted: if stdin/stdout are interactive terminals (not `--json`/dry-run/resume) 4x shows a numbered profile menu defaulting to `default_profile`; otherwise it uses `default_profile`, then priority-based auto-select when a `profiles` section exists (else `full`).

The per-phase runner/model is resolved by this precedence (high→low): `--phase-override <phase>:<runner>:<model>` (this-run-only temporary override) > `--runner`/manual selection > the feature YAML's `phase_overrides.<phase>` > the profile's per-phase `runner`/`model` > `default_runner` / the role's configured model. `--phase-override` only affects the phase it names (either dimension can be left empty, e.g. `reviewing:gemini:` for runner-only or `testing::opus` for model-only) and is never written back to `settings.json` or the feature YAML — the same temporary override the dashboard run dialog sends. See [Configuration → Profiles](configuration.md#profiles) for details.

The loop drives: init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review. On review failure, code gets another pass. On test failure, the loop re-enters coding.

After each non-designer runner completes, guardrail checks are enforced automatically (scope, baseline, required files). A violation transitions the feature to `needs-attention` and stops the loop. Designer is exempt — it does not modify source code.

Review verdicts must start with `PASS` to pass. Blank lines between the `## Verdict` heading and the verdict text are ignored. Ambiguous output (`TODO`, `ERROR`, garbled text, missing `## Verdict` block) is treated as failure.

Phase hooks declared in `settings.json` or the feature YAML are executed automatically before and after each phase transition within the loop. See [Phase Hooks](concepts.md#phase-hooks) for configuration details.

When entering the `testing` phase (after `pre_testing` hooks, before the Tester runner is spawned), a health check verifies the environment if `health_check` is configured. Check commands run in order; on failure the recovery commands run once and the checks are retried once. If the environment still fails, the feature transitions to `needs-attention` and the loop stops. See [Health Check](concepts.md#health-check) for configuration details.

When `auto_discover_features` is enabled in `settings.json`, a final deep review **PASS** parses the `[NEW-FEATURE]` markers in `deep-review-report.md` and auto-creates feature YAMLs for the out-of-scope issues the deep reviewer flagged (deduplicated and capped). See [Configuration → Auto-Discover Features](configuration.md#auto-discover-features) and [Concepts → Auto-Discovered Features](concepts.md#auto-discovered-features) for details.

If the feature is in `blocked` or `needs-attention` phase, automatically recovers to the appropriate resume phase based on the current role.

Before running deep review, 4x selects which of the 11 review angles to run based on the diff-affected file paths. The `angle_mapping` in `roles.deep-reviewer` maps path prefixes (and `*`-prefixed suffix patterns like `*_test.go`) to angle numbers; only matched angles are dispatched. When no file matches any rule, all 11 angles run as a safety fallback. The selection is recorded in `deep-review-angles.json` in the round directory. To force all angles: pass `--all-angles` to `4x run`, or set `deep_review_all_angles: true` in the feature YAML. See [Configuration → Angle Mapping](configuration.md#angle-mapping) for the default mapping and customization.

If a run crashes mid `deep-reviewing`, recovery is incremental rather than restarting the whole (expensive) deep review. `SmartResumePhase` (in `internal/orchestrator`) inspects the on-disk artifacts and restores the matching `subPhase`: any missing/incomplete `deep-review-partial-{i}.md` resumes at `reviewing` and only the missing sub-reviewers are re-spawned (`MissingDeepPartials`), reusing each index's original angle group; all partials present but an incomplete report resumes at `synthesizing` (sub-reviewers skipped, only the synthesizer re-runs); a complete-but-FAILed report routes to `amending` as before. A partial counts as complete only when it carries the `## Statistics` sentinel section (`DeepPartialComplete`). See [Concepts → Deep Review SubPhase & Crash Recovery](concepts.md#deep-review-subphase--crash-recovery).

Automatically checks dependency gate — blocks if depended features are not done.

If `isolation: "worktree"` is set in config, runs in a git worktree under `.worktrees/4x/<feature-id>/`. In multi-repo mode (workspace.repos configured), each repo gets its own worktree under `.worktrees/4x/<feature-id>/<repo-name>/`, and workspace-level files (go.work, Makefile, etc.) are copied alongside. Coder prompts include a `== Workspace Repos ==` section; in worktree mode, each entry shows the repo name as a relative path (e.g. `core → core/`) so the coder operates within the correct directory boundaries.

When a worktree run ends in `done`/`pending-review`, the output prints the branch and merge command. When it ends in any other state (`needs-attention`, `blocked`, interruption, or error), the worktree is preserved and the output prints its path plus a cleanup command (`git worktree remove … && git branch -d …`) so the orphan worktree stays visible. 4x never auto-deletes the worktree in these cases, to avoid discarding unsaved work.

---

## `4x status [feature-id]`

Show feature status.

```
4x status              # all features, grouped by state
4x status <feature-id> # single feature details with subtasks
4x status --pending    # hide done/abandoned features
4x status --json       # output as JSON
```

| Flag | Description |
|---|---|
| `--pending` | Hide done/abandoned features |
| `--json` | Output as JSON |

Groups: Running, Review, Pending, Todo, Done (done shows max 5). Includes backlog drift warnings.

For single feature detail (`4x status <feature-id>`), if screenshots exist, it also prints:

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x subtask <feature-id> <subtask-id>`

Update the status of a subtask within a feature.

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| Flag | Description |
|---|---|
| `--status` | New status: `done`, `in-progress`, `blocked`, `not-started`, `ready-for-review` (required) |

Example:
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x approve <feature-id>`

Approve a `draft` feature produced by enriched auto-discover, transitioning it `draft → not-started` so the meta-loop will pick it up. Drafts are only created when `enrich_discovered_features` is enabled and `enrich_auto_approve` is `false`. Errors if the feature is not in `draft` status.

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

Reject a `draft` feature produced by enriched auto-discover, transitioning it `draft → abandoned` so it stays out of the meta-loop. Errors if the feature is not in `draft` status.

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

Recover a feature stuck in `needs-attention` or `blocked` by transitioning it back to a working phase, then immediately launching `4x run`. Equivalent to `4x transition --to <phase> <id> && 4x run <id>`.

Default target phase is `accepting` (re-run the Acceptor after the human fixes issues). Use `--to` to target a different phase.

```
4x retry F042-some-feature
4x retry F042-some-feature --to amending
```

| Flag | Description |
|------|-------------|
| `--to <phase>` | Target phase to recover to (default: `accepting`) |

Errors if the feature is not currently in `needs-attention` or `blocked`.

---

## `4x gate`

Apply the F097 evolve **value gate** veto layers to mined candidate features. Pure CLI deterministic veto — it does not call an LLM. The `gate` LLM role runs between the two phases (orchestrated by the evolve driver) to produce `gate-verdicts.json`.

Exactly one of `--pre` / `--post` must be given:

- `--pre` — PRE-veto: read `.4x/candidates.json`, drop candidates that are Jaccard-similar to existing features (and intra-batch duplicates), write survivors to `.4x/gate-input.json`.
- `--post` — POST-veto: read `.4x/gate-input.json` + `.4x/gate-verdicts.json`, apply the non-overridable hard veto (non-accept / missing `why_not_hack` / below `value_floor` / duplicates existing / over `max_accept_per_run` / over `max_backlog_undone`), write accepted candidates (with `value_score`/`why_not_hack`) to `.4x/accepted-candidates.json`.

Thresholds come from the `evolution` section of `settings.json` (`value_floor`, `max_accept_per_run`, `max_backlog_undone`, `dedup_threshold`).

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

Run one round of the continuous self-improvement pipeline, stitching the existing evolution parts into a repeatable closed loop:

**mine → gate (pre → gate LLM role → post) → enrich → enqueue → (optional) auto-run meta-loop → learnings feed the next round.**

The CLI layer never calls an LLM directly — the gate role and enrichment both run as `runner` subprocesses. Each call runs exactly **one** round; drive repeated rounds externally (cron or repeated `4x evolve`). Every round writes a summary to `.4x/evolve-report.md`.

Pipeline steps:

1. **mine** — scan `.4x/` for failure signals (escalations / stuck features / recurring FAIL patterns), dedupe, merge into `.4x/candidates.json`.
2. **gate pre** — Jaccard dedupe survivors to `.4x/gate-input.json`.
3. **gate role** — spawn the `gate` LLM role to write `.4x/gate-verdicts.json`.
4. **gate post** — apply the non-overridable veto + convergence caps, write `.4x/accepted-candidates.json`.
5. **enrich + enqueue** — materialize each accepted candidate into a `not-started` feature YAML (on enrich failure it falls back to a bare feature built from the candidate text, marked `enriched=false`).
6. **auto-run** (optional) — run the meta-loop for each enqueued feature, protected by the F098 self-mod scope guard.

Anti-spin: when a round accepts nothing, `.4x/evolve-state.json` increments `consecutiveNoAccept`; once it reaches `evolution.max_idle_rounds` (default 3; set `<= 0` to disable) the next call halts early, marks the report `Halted`, and exits 0. Use `--force` to override.

```
4x evolve                        # run one round, leave features at not-started
4x evolve --dry-run              # read-only: print mine/dedupe summary, write nothing
4x evolve --auto-run             # also run the meta-loop for enqueued features
4x evolve --force                # bypass the anti-spin halt
```

| Flag | Description |
|---|---|
| `--auto-run` | Run the meta-loop for each enqueued feature (F098 self-mod guard always enforced) |
| `--dry-run` | Read-only analysis: print mined/deduped counts, write no files, spawn no runner, create no feature |
| `--min-occurrences` | Distinct-feature threshold for a fail-pattern to become a candidate (default 3) |
| `--force` | Override the anti-spin halt and run even after consecutive idle rounds |
| `--runner` | Runner plugin for gate / enrich / auto-run (default `evolution.gate_runner` or project default) |
| `--timeout` | LLM subprocess timeout in seconds (default 3600) |
| `--max-rounds` | Max rounds per feature when `--auto-run` is set (default 5) |

The dashboard surfaces the latest report via `GET /api/evolve-report`.

---

## `4x check <feature-id>`

Run guardrail checks without transitioning state.

```
4x check <feature-id> [--json]
```

| Flag | Description |
|---|---|
| `--json` | Output results as JSON |

Checks: required files, baseline, scope, dependencies, backlog drift. Exit 0 on pass, 1 on fail.

---

## `4x doctor`

Run a one-shot, read-only health check on the merged settings (`.4x/settings.json` + `~/.4x/settings.json`) and workspace integrity, before you start a run. It never calls an LLM and does not require any runner to be installed.

```
4x doctor [--json]
```

| Flag | Description |
|---|---|
| `--json` | Output the full report as JSON (for CI) |

Checks are grouped into sections:

- **settings** — `settings.json` loadable, `project.name` non-empty, at least one runner defined, `default_runner` exists in the runners map.
- **runners** — each runner's `command` is resolvable on `PATH` (missing → WARN, not FAIL, since a runner may live on a remote machine).
- **roles** — resolves the actual model each role (designer/coder/reviewer/tester/acceptor) will use via the default runner, plus the reviewer's `deep_model`.
- **workspace** — orphaned worktrees (feature done/abandoned but `.worktrees/4x/<id>` remains), dangling worktrees (directory with no matching feature), stale state (`active=true` but the process is gone), and malformed feature YAML.

Each line is prefixed with `✅` (PASS), `⚠️` (WARN), or `❌` (FAIL), followed by a summary count.

Exit code: `0` when there is no FAIL (WARN does not affect the exit code), `1` when any check fails. `doctor` is strictly read-only — it never rewrites `state.json`, cleans worktrees, or modifies settings.

```bash
# CI gate: fail the build on any FAIL check
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

Run the verify commands from the feature's `test-strategy.yaml` and write the results to `rounds/round-{N}/verify.json`.

Commands can be organised into groups via `verify_groups`: groups run in parallel, while commands within a group run sequentially. If a command in a group fails, the remaining commands in that group are skipped, but other groups keep running. When only `verify_commands` is defined, it falls back to a single sequential `default` group. Declaring both is an error.

**Fallback**: when `test-strategy.yaml` does not exist (e.g. Designer was skipped by the profile), verify automatically falls back to the project's `build`/`test`/`lint` commands from `settings.json`, grouped under a single `"fallback"` group. The fallback path also auto-generates `ac_results` entries (one per non-skipped command), so the `testing → accepting` guard passes without manual intervention.

Parallel execution is handled entirely by the CLI — no LLM involved. The Tester role calls this command instead of running verify commands itself; humans can also run it standalone for debugging.

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| Flag | Description |
|---|---|
| `--round` | Round number (default: current round from state.json) |
| `--timeout` | Overall timeout for all groups (default: 5m) |
| `--json` | Output the full verify.json as JSON |

Exit 0 when every non-skipped command passes, 1 when any command fails.

---

## `4x transition <feature-id>`

Force a state transition.

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| Flag | Description |
|---|---|
| `--to` | Target phase (required) |
| `--role` | Role performing the transition |
| `--json` | Output as JSON |

Validates the transition is legal per the state machine. Auto-initializes state if it doesn't exist. The `testing → accepting` transition runs additional gates (verify.json, test-report.md, final-report.md must exist and verify must pass).

If `settings.json` or the feature YAML declares `hooks`, the `pre_{phase}` hooks run before the transition and `post_{phase}` hooks run after. A `block` pre-hook failure aborts the transition; a `block` post-hook failure moves the feature to `needs-attention`. See [Phase Hooks](concepts.md#phase-hooks) for the full configuration format.

---

## `4x event <feature-id>`

Append an event to `events.jsonl`.

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| Flag | Description |
|---|---|
| `--type` | Event type (required) |
| `--role` | Role that triggered the event |
| `--round` | Round number |
| `--action` | Action name |
| `--detail` | Additional detail text |

---

## `4x prompt <feature-id>`

Print the role prompt for a feature.

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| Flag | Description |
|---|---|
| `--role` | Target role (inferred from current state if omitted) |
| `--round` | Round number |

Supports locale injection (from user config or `LANG` env), planning doc auto-inclusion, and project/role includes. The spec/plan docs are located via the shared resolver (`protocol.ResolveDesignDoc`) — the feature YAML `spec`/`plan` field first, then `docs/design/{id}-{type}.md`, then the `FNNN-`-stripped `docs/design/{slug}-{type}.md` fallback — so the prompt sees the same documents as the dashboard overview. See [Design Doc Resolution](concepts.md#design-doc-resolution).

For the `tester` role, any `profiles` listed in the feature's `test-strategy.yaml` are resolved (via `loadProfiles`) and injected into the prompt as `== Test Profile: {name} ==` blocks. Each profile's content comes from `settings.json` `test_profiles[name]` (`content` or `include`) when present, otherwise the built-in `templates/profiles/{name}.md`. See [Test Profiles](concepts.md#test-profiles).

---

## `4x done <feature-id>`

Mark a pending-review feature as done. If the feature has a worktree (`.worktrees/4x/<id>`), automatically merges the branch back to main and removes the worktree and branch.

```
4x done <feature-id>
```

Only works when feature is in `pending-review` phase. Errors on any other phase.

If a merge conflict or merge error occurs, the feature remains in `pending-review`, the worktree is preserved, and guidance is printed. In multi-repo mode, the conflicting repo name is printed as `repo: <name>`. Use `4x merge <id>` to complete after resolving conflicts.

If the feature touched self-mod protected paths (see the `self_mod_guard` settings), the merge is blocked until you confirm with `--approve-self-mod`; the touched protected paths are printed for review:

```
4x done <feature-id> --approve-self-mod
```

---

## `4x merge <feature-id>`

Complete a merge after resolving conflicts from `4x done`.

```
4x merge <feature-id>
```

Only works when feature is in `pending-review` or `done` phase and a worktree exists at `.worktrees/4x/<id>`. Commits resolved conflicts in the worktree, merges to main, then removes the worktree and branch. If the feature is still in `pending-review`, it is marked `done` after the merge succeeds.

In multi-repo mode, resolved conflicts are committed per repo (each repo under `.worktrees/4x/<id>/<repo-name>/` is staged and committed independently), then all repos are merged all-or-nothing. The conflicting repo name is shown as `repo: <name>` if a conflict recurs.

Like `4x done`, this command blocks merging a feature that touched self-mod protected paths until confirmed with `--approve-self-mod`.

---

## `4x clean [feature-id]`

Remove workspace artifacts (`logs/`, `rounds/`, reports, `state.json`, `events.jsonl`) for completed features, freeing disk space. Feature definitions (`.4x/features/*.yaml`) and feature status are always preserved.

```
4x clean              # list cleanable features + sizes, confirm, then clean
4x clean --dry-run    # list only, delete nothing
4x clean --force      # skip confirmation prompt
4x clean <feature-id> # clean a single feature (still must be done/abandoned)
```

Only features in `done` or `abandoned` status with an existing workspace directory are eligible. Active (running) features are never cleaned, and `blocked` / `needs-attention` features are kept so their debug artifacts remain available. Cleaning is not a state-machine transition — it does not change feature lifecycle.

---

## `4x learn`

Manage retro learnings — development lessons accumulated across features in `.4x/learnings.json`.

The Acceptor of each feature writes a `retro-learnings.json`; the CLI harvests it into `.4x/learnings.json`. On the next feature, the Designer picks relevant entries into `selected-learnings.json`, and the CLI injects them (filtered by category) into each role's prompt. learnings are managed entirely by the CLI — runners never write `learnings.json` directly, and any learnings failure only warns without blocking state transitions.

```
4x learn list                     # list all learnings (id/category/status/used/content)
4x learn list --category=testing  # filter by category
4x learn prune                    # mark stale (>90 days unused) entries and remove them
4x learn prune --dry-run          # preview stale entries without removing
4x learn promote <id>             # mark a learning as promoted (kept but no longer injected)
4x learn remove <id>              # remove a learning entry
```

- Categories: `design`, `code-quality`, `testing`, `review`, `tooling`, `process`
- Status: `active` (injectable), `stale` (>90 days unused, auto-marked on read), `promoted` (upgraded to template/instructions)
- A soft cap of 100 active entries triggers a warning suggesting `4x learn prune` — entries are never auto-deleted

---

## `4x mine`

Scan the entire `.4x/` history for failure signals and aggregate them into a candidate pool at `.4x/candidates.json`. Unlike auto-discovery (which only fires on a single run's deep-review PASS and parses `[NEW-FEATURE]` markers), the miner sweeps **all** features for the densest failure data: escalations, stuck features, and recurring review failures.

The miner is a pure CLI/protocol-layer scan — it never calls an LLM and never creates features. It only produces candidates; whether a candidate is promoted to a real feature is decided later by the F097 gate.

```
4x mine                          # scan and write .4x/candidates.json
4x mine --dry-run                # print summary without writing
4x mine --min-occurrences 5      # raise the fail-pattern threshold (default 3)
4x mine --output path.json       # write to a custom path
```

| Flag | Default | Description |
|---|---|---|
| `--min-occurrences` | `3` | Distinct-feature count a recurring review issue must reach to become a candidate |
| `--output` | `.4x/candidates.json` | Candidate pool output path |
| `--dry-run` | `false` | Print the summary only, write nothing |

Three scanners feed the pool, each tagging candidates with a `source` for traceability:

- **escalation** — reads every round's `escalation.json` (`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`)
- **stuck** — features stuck in `needs-attention` / `abandoned` / `blocked`, with the blocking reason extracted from `state.json` or the latest escalation
- **fail-pattern** — review / deep-review FAIL issues that recur across `>= --min-occurrences` distinct features (clustered by Jaccard similarity); each cluster also emits a candidate learning suggesting a review checklist

Scanning is best-effort: a single corrupt feature only logs a warning and never aborts the rest. Candidates are deduplicated (Jaccard) against existing features, the previous `candidates.json`, and each other.

---

## `4x config`

Manage user-level configuration (`~/.4x/settings.json`).

```
4x config list          # show all user config
4x config get <key>     # get a value
4x config set <key> <value>  # set a value
```

Keys are dotted paths. Supported forms:

| Key | Example | Description |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | UI / prompt locale |
| `theme` | `4x config set theme dark` | Dashboard theme |
| `default_runner` | `4x config set default_runner claude` | Default runner plugin |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | Per-runner `command`/`model`/`tty`/`stdin`/`quiet` |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | Per-role `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` |

`role.deep-reviewer.parallel_reviewers` controls how many parallel sub-reviewers the deep review fans out to (`1` = single-agent fallback); `role.deep-reviewer.angles_per_reviewer` fixes each group's angle count (leave unset for automatic balancing). See [Concepts → Parallel Deep Review](concepts.md).

---

## `4x sync`

Sync embedded plugin files to the project.

```
4x sync [--dry-run]
```

| Flag | Description |
|---|---|
| `--dry-run` | Report differences without writing files |

Reports each file as created, updated, or current.

---

## `4x batch`

Batch operations for multiple features.

### `4x batch plan`

Generate a dependency-aware execution plan.

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| Flag | Default | Description |
|---|---|---|
| `--dry-run` | `false` | Print schedule without writing file |
| `--max-chain` | `4` | Maximum chain length per cluster |

Writes `.4x/batch-plan.json`.

### `4x batch next`

Show the next eligible feature to run (based on the plan and current status).

```
4x batch next [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output in JSON format with subtask frontier |

Without `--json`, prints the feature ID as plain text (backward compatible). With `--json`, outputs a JSON object including `subtaskFrontier` — the subtasks whose dependencies are all completed. Returns `null` in JSON mode when no eligible features remain.

### `4x batch run`

Run eligible features sequentially in dependency order.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| Flag | Default | Description |
|---|---|---|
| `--runner` | config default | Runner plugin name |
| `--max-rounds` | `5` | Max rounds per feature |
| `--timeout` | `3600` | Per-phase timeout in seconds |
| `--no-auto-merge` | `false` | Leave each completed feature at `pending-review` instead of auto-merging back to main |

Polls for `.4x/batch-stop` file between features for graceful shutdown.

When the run ends — whether it finishes normally, is stopped, is interrupted (`SIGTERM`/`SIGINT`), or crashes — it writes a `.4x/batch-report.json` summarizing the run (`outcome`, completed/failed/remaining counts, runner, duration, and each feature's final status). See [Batch Mode → Run Report](batch.md#run-report).

By default, after a feature completes (reaches `pending-review`), the batch automatically merges its worktree branch back to main so the next feature branches from the updated main — enabling unattended continuous batches. On a merge conflict the batch pauses gracefully, leaving the feature at `pending-review` and the worktree intact, and writes a `.4x/batch-conflict.json` signal file (feature, conflicting repo, files) so the [dashboard](dashboard.md) can display the conflict; resolve the conflict, run `4x merge <id>`, then re-run `4x batch run` to continue. The conflict signal is cleared at the start of each run. Non-conflict merge errors print a warning and the batch continues with the next feature. Pass `--no-auto-merge` to restore the old behavior (features stop at `pending-review` for manual review).

If `isolation: "worktree"` is set in config, each feature runs in its own isolated worktree. In multi-repo mode, each feature gets a composite worktree (`.worktrees/4x/<feature-id>/`) with per-repo sub-directories, and commits are made per round (not deferred to completion). Hub repos (from `hub_repos` config or `workspace.repos[*].hub: true`) are excluded from shared-repo clustering to allow parallel execution.

### `4x batch stop`

Signal the running batch to stop after the current feature completes.

```
4x batch stop
```

Creates a `.4x/batch-stop` signal file.

---

## `4x live [path...]`

Start the 4x Live dashboard server.

```
4x live [path...] [flags]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--port` | `-p` | `4567` | Server port |
| `--web` | `-w` | `false` | Open in browser |
| `--app` | `-a` | `false` | Open macOS native app |

Without paths, loads recent projects from `~/.4x/recent-projects.json` (LRU, max 20). With paths, opens each as a project tab.

---

## `4x mcp`

Start the Model Context Protocol (MCP) server.

```
4x mcp
```

Starts the 4x MCP stdio server to expose 4x CLI commands as MCP tools to LLM clients (e.g., Claude Code, Cursor).

Each tool is a thin wrapper that invokes the matching `4x` subcommand with `--json` and returns the parsed result. The following tools are exposed:

**Core loop**

| Tool | CLI command |
|---|---|
| `4x_status` | `status [feature-id]` |
| `4x_new` | `new` |
| `4x_run` | `run` |
| `4x_stop` | `stop` |
| `4x_check` | `check` |
| `4x_transition` | `transition` |

**Lifecycle**

| Tool | CLI command |
|---|---|
| `4x_done` | `done` |
| `4x_verify` | `verify` |
| `4x_approve` | `approve` |
| `4x_reject` | `reject` |
| `4x_subtask` | `subtask` |
| `4x_merge` | `merge` |
| `4x_mine` | `mine` |

**Batch & maintenance**

| Tool | CLI command |
|---|---|
| `4x_batch_next` | `batch next` |
| `4x_batch_stop` | `batch stop` |
| `4x_clean` | `clean` |
| `4x_doctor` | `doctor` |
| `4x_learn_list` | `learn list` |
| `4x_learn_prune` | `learn prune` |
| `4x_learn_promote` | `learn promote` |
| `4x_learn_remove` | `learn remove` |
| `4x_evolve` | `evolve` |

**Config & gate**

| Tool | CLI command |
|---|---|
| `4x_config_get` | `config get` |
| `4x_config_set` | `config set` |
| `4x_config_list` | `config list` |
| `4x_gate` | `gate --pre` / `gate --post` |

Orchestration and interactive commands (`init`, `sync`, `live`, `prompt`, `event`, `batch run`, `batch plan`) are intentionally not exposed as MCP tools.
