# CLI Reference

All feature-id arguments support case-insensitive prefix matching. `4x run f001`, `4x run F001-user`, and `4x run F001` all resolve to `F001-user-authentication-w`. Ambiguous prefixes produce an error listing the matches.

---

## `4x init`

Initialize a `.4x/` workspace in the current directory.

```
4x init
```

- Auto-detects project language and build/test/lint commands, plus an initial `project.verify_command_allowlist` derived from those commands
- Generates per-role model defaults from the user config's `default_runner`: for a runner with tiers (claude, codex, …) it writes canonical `strong` / `fast` tiers (designer→`strong`, coder/reviewer/tester→`fast`, reviewer `deep_model`→`strong`); for a tier-less runner (cursor) or unknown runner it leaves `model` empty so the runner uses its own default
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
| `--subtask` | Subtask in `"id:name"` format (repeatable); id is the part before the first colon, name may contain colons (e.g. `10:00`, `group:artifact`, URLs); set description by editing the YAML afterward |
| `--rule` | Rule reference (repeatable) |
| `--depends` | Dependency feature ID (repeatable) |
| `--priority` | Priority level (0=critical, 1=high, 2=medium, 3=low) |
| `--profile` | Pipeline profile written to the feature YAML (`full`/`normal`/`quick` or custom); applied per-feature on `4x run` |
| `--repo` | Repository in scope (repeatable) |
| `--issue` | Link an existing issue in `"repo:id-or-url"` format (repeatable); repo prefix optional for single-repo features. Only used when `issue_tracker.enabled` is set (see [Concepts](concepts.md#issue-first-mr-flow)) |
| `--json` | Output as JSON |

Creates `.4x/features/F{NNN}-{slug}.yaml` with status `not-started`.
Auto-generated slug truncates at word boundary; use `--id` to override.
When `--profile` is omitted (and not `--json`), `4x new` prints a deterministic profile *suggestion* (with per-signal reasons) for the new feature; it is advisory only and does not set the feature's profile. See [Configuration → Profiles](configuration.md#profiles) for the `profile_advisor` heuristic.
Creation runs through the shared `feature.Create` path (see [Concepts](concepts.md#feature-creation)) — the dashboard's `POST /api/new` uses the same logic, so flags here map one-to-one to the dashboard's New Feature form.

When `issue_tracker.enabled` is `true` in `settings.json`, `4x new` first preflights `gh`/`glab` (installed + authenticated) for every declared repo — any failure aborts before the feature is created. It then creates a new issue per repo (or links an existing one via `--issue`), recording the result in the feature YAML's `issues` field; a per-repo failure is recorded as a warning and printed, but does not block feature creation. See [Concepts](concepts.md#issue-first-mr-flow).

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
| `--timeout` | `0` | Per-phase timeout in seconds (`0` = no limit) |
| `--dry-run` | `false` | Print role prompts without calling LLM |
| `--json` | `false` | Start run and return JSON immediately |
| `--profile` | auto | Pipeline profile (`full`/`normal`/`quick` or custom); overrides `default_profile`/priority auto-select |
| `--phase-override` | none | Temporary per-phase runner/model override for this run only, format `<phase>:<runner>:<model>` (repeatable); not written back to settings or the feature YAML |
| `--no-notify` | `false` | Disable the OS notification on run completion (overrides the `notifications` config) |
| `--all-angles` | `false` | Force deep review to run all 11 angles, ignoring the diff-based angle mapping |
| `--note` | none | One-shot free-text note injected into the prompt of the **first role** of this run only, then cleared; not persisted to the feature YAML `description` |

The `--note` flag attaches ad-hoc guidance (e.g. "focus on the retry path", "skip the perf angle") to the first role that runs this invocation (designer on a fresh run, or the resumed role on a resume). It is consumed once — later roles in the same run and the next `retry` do not see it — and lives only in `state.json`'s one-shot `runNote` field, never touching the permanent feature `description`.

When the run ends (success, failure, or interruption), 4x sends a native OS notification (`osascript` on macOS, `notify-send` on Linux, PowerShell balloon on Windows). Pass `--no-notify` to suppress it, or set `"notifications": false` in `settings.json`. Missing notification tooling is silently ignored.

With `--json`, the loop runs in a detached background process and the command returns immediately with `{featureId, runner, maxRounds, pid, logPath}`. The background process's stdout/stderr are redirected to `logPath` (`.4x/<feature-id>/run.log`), so early errors (config load, worktree setup, runner not found) are recorded there instead of being lost.

`--profile` selects which phases run and, per phase, which runner and model. Built-in profiles: `full` (designing/design-reviewing/coding/reviewing/deep-reviewing/testing/accepting), `normal` (coding/reviewing/testing/accepting), `quick` (coding/reviewing). Phases not in the profile are passed through (state advances along the legal edge without invoking the runner); `coding` is always required. When `--profile` is omitted: if stdin/stdout are interactive terminals (not `--json`/dry-run/resume) 4x prints the deterministic profile suggestion (see [Configuration → Profiles](configuration.md#profiles)) and shows a numbered profile menu with the suggested profile preselected as the cursor default; otherwise it prints the suggestion to stdout but leaves resolution unchanged — using `default_profile`, then priority-based auto-select when a `profiles` section exists (else `full`). The suggestion never auto-adopts a profile.

The per-phase runner/model is resolved by this precedence (high→low): `--phase-override <phase>:<runner>:<model>` (this-run-only temporary override) > `--runner`/manual selection > the feature YAML's `phase_overrides.<phase>` > the profile's per-phase `runner`/`model` > `roles.<role>.runner` > `default_runner` / the role's configured model. `--phase-override` only affects the phase it names (either dimension can be left empty, e.g. `reviewing:gemini:` for runner-only or `testing::opus` for model-only) and is never written back to `settings.json` or the feature YAML — the same temporary override the dashboard run dialog sends. See [Configuration → Profiles](configuration.md#profiles) for details.

The loop drives: init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → fixing → accepting → pending-review. On review failure, code gets another pass. On test failure, the loop re-enters coding. The fixing phase runs after deep-reviewing PASS to address WARNING/INFO issues from the deep review report; it only activates when the profile enables it, and is skipped even then when the deep-review report is a clean PASS (no critical or warning issues) — in that case the loop goes straight to `accepting`.

After each non-designer runner completes, guardrail checks are enforced automatically (scope, baseline, required files). A violation transitions the feature to `needs-attention` and stops the loop. Designer is exempt — it does not modify source code.

Review verdicts must start with `PASS` to pass. Blank lines between the `## Verdict` heading and the verdict text are ignored. Ambiguous output (`TODO`, `ERROR`, garbled text, missing `## Verdict` block) is treated as failure.

Phase hooks declared in `settings.json` or the feature YAML are executed automatically before and after each phase transition within the loop. See [Phase Hooks](concepts.md#phase-hooks) for configuration details.

When entering the `testing` phase (after `pre_testing` hooks, before the Tester runner is spawned), a health check verifies the environment if `health_check` is configured. Check commands run in order; on failure the recovery commands run once and the checks are retried once. If the environment still fails, the feature transitions to `needs-attention` and the loop stops. See [Health Check](concepts.md#health-check) for configuration details.

When `auto_discover_features` is enabled in `settings.json`, a final deep review **PASS** parses the `[NEW-FEATURE]` markers in `deep-review-report.md` and auto-creates feature YAMLs for the out-of-scope issues the deep reviewer flagged (deduplicated and capped). See [Configuration → Auto-Discover Features](configuration.md#auto-discover-features) and [Concepts → Auto-Discovered Features](concepts.md#auto-discovered-features) for details.

If the feature is in `blocked` or `needs-attention` phase, automatically recovers to the appropriate resume phase based on the current role.

Before running deep review, 4x selects which of the 11 review angles to run based on the diff-affected file paths. The `angle_mapping` in `roles.deep-reviewer` maps path prefixes (and `*`-prefixed suffix patterns like `*_test.go`) to angle numbers; only matched angles are dispatched. When no file matches any rule, all 11 angles run as a safety fallback. The selection is recorded in `deep-review-angles.json` in the round directory. To force all angles: pass `--all-angles` to `4x run`, or set `deep_review_all_angles: true` in the feature YAML. See [Configuration → Angle Mapping](configuration.md#angle-mapping) for the default mapping and customization.

When `4x run` starts on a feature that already has a `state.json`, it performs crash recovery: `SmartResumePhase` derives the resume start point from the on-disk artifacts of the current round (e.g. a missing/incomplete `coder-report.md` resumes at `coding`) rather than blindly trusting the recorded phase. The one exception is a phase set by a manual `4x transition` / `4x retry --to <phase>` — such a phase carries a `manualPhase` flag and is honored as-is (role derived from the phase, no artifact-based re-derivation), so a deliberate human intervention is never overwritten back to an earlier phase. The flag is consumed on the first recovery, so a genuine crash on the next round still goes through normal artifact-based recovery.

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

If the feature has codex-runner rounds, it also prints a `Codex usage` block listing each round's live subscription quota (`round N: 5h X% / 1wk Y% (N tokens)`); features with no codex rounds omit the block entirely.

---

## `4x cost`

Aggregate run cost across features from the stream logs each runner writes. Read-only — it never touches run data.

```
4x cost                       # per-role cost table across all features
4x cost --feature <id>        # per-round per-role detail for one feature
4x cost --by-round            # per-round totals + retry (round>=2) share
4x cost --feature <id> --by-round  # per-round detail for one feature
4x cost --json                # structured output (any view above)
```

| Flag | Description |
|---|---|
| `--feature <id>` | Filter to a single feature; show per-round per-role detail |
| `--by-round` | Group by round and show the retry (round>=2) share |
| `--json` | Output as JSON |

Data source is `logs/*.stream.jsonl` (one file per role invocation, carrying `total_cost_usd`); the filename encodes round and role. `events.jsonl` `run-end` events are used as a fallback for any feature that has no stream logs (older runs). Stream logs that lack a `total_cost_usd` field are skipped and reported as a `Skipped N stream log(s)` count rather than causing a failure.

The default table shows `ROLE / CALLS / TOTAL($) / AVG($) / PCT(%)` sorted by total cost, plus a `TOTAL` row. `--by-round` adds a `TYPE` column (`initial` for rounds 0–1, `retry` for round≥2) and reports the retry share in USD and percent.

The codex runner uses a ChatGPT subscription with no USD metering, so it never appears in the USD table (its `cost_usd` is 0). Instead, `4x cost --feature <id>` appends a `Codex usage` block after the USD table listing each codex round's live subscription quota (`5h X% / 1wk Y% (N tokens)`), read independently from `events.jsonl` `run-end` events so it is not masked by the stream-first USD path in mixed claude+codex features. `--json` carries the same data in a `codex_rounds` array.

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

When `--to` is omitted, the target phase is **auto-detected** from the `role` recorded in `state.json` — the role that was stuck before the feature entered `needs-attention`/`blocked` is mapped back to its working phase (e.g. `role: designer` → `designing`; `role: coder` → `coding` or `amending` depending on round). When auto-detection succeeds, it prints `auto-detected target phase from role "<role>": <phase>` before launching. If the role cannot be mapped (empty or unknown), it falls back to `accepting`. Passing `--to <phase>` explicitly overrides auto-detection.

```
4x retry F042-some-feature              # auto-detect target phase from state.json role
4x retry F042-some-feature --to amending
```

| Flag | Description |
|------|-------------|
| `--to <phase>` | Target phase to recover to (default: auto-detect from `state.json` role, falling back to `accepting`) |
| `--phase-override <phase>:<runner>:<model>` | Forwarded to the relaunched `4x run` (repeatable) — same format and semantics as `4x run`'s `--phase-override` |
| `--note <text>` | Forwarded to the relaunched `4x run` as its `--note` — a one-shot free-text note injected into the first (resumed) role only, then cleared; not persisted to the feature `description` |

The phase set by a manual `transition` / `retry --to <phase>` is respected by the subsequent `4x run` recovery: it is marked with a `manualPhase` flag so `SmartResumePhase` does not override it back to an earlier phase derived from on-disk artifacts. This means `retry --to deep-reviewing` actually resumes at `deep-reviewing` instead of being pulled back to `coding`.

The state-mutating commands (`transition`, `retry`, `force-done`, `done`) perform their phase change as a single locked read-modify-write on `state.json`, so running one against a feature that a live `4x run` is also writing cannot clobber the other's update. If the per-feature lock cannot be acquired within its timeout the command fails with a clear error instead of hanging.

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

The CLI layer never calls an LLM directly — the gate role and enrichment both run as `runner` subprocesses. Like every runner spawned by 4x, these subprocesses inherit a filtered environment — sensitive credentials are stripped per the built-in denylist and per-runner allowlist (see [Configuration → `runner_env` / `env_allowlist`](configuration.md)). Each call runs exactly **one** round; drive repeated rounds externally (cron or repeated `4x evolve`). Every round writes a summary to `.4x/evolve-report.md`.

Pipeline steps:

1. **mine** — scan `.4x/` for failure signals (escalations / stuck features / recurring FAIL patterns), dedupe, merge into `.4x/candidates.json`.
2. **gate pre** — Jaccard dedupe survivors to `.4x/gate-input.json`.
3. **gate role** — clear any stale `.4x/gate-verdicts.json` from a prior round, then spawn the `gate` LLM role to write a fresh one; if the role returns without writing, parsing fails loudly instead of silently reusing stale verdicts.
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
4x check --path <file> [feature-id]
```

| Flag | Description |
|---|---|
| `--json` | Output results as JSON |
| `--path <file>` | Fast, read-only single-file write-permission check for the current role (see below) |

Checks: required files, baseline, scope, dependencies, backlog drift. Exit 0 on pass, 1 on fail.

With `--path <file>`, `check` runs only a fast, read-only single-file write-permission check for the current role — it completely bypasses the full `guard.Check` (no required-files / baseline / docs gate) and never writes `state.json` or `events.jsonl`. It resolves the target feature from the positional argument, then `FOURX_FEATURE_ID`, then the sole active feature. The current role is read from `state.json` (falling back to the role derived from the phase when the role field is empty): only `coder` and `fixer` may write source files (anything outside `.4x/`), and for a multi-repo feature the file's top-level repo must be in the feature's `repos` (or a hub repo); writes under `.4x/` are always allowed. Exit `0` allows the write, a non-zero exit rejects it (the reason is printed to stderr). It fails open on every read/detection error — not a 4x project, no resolvable feature, missing `state.json`, or a git detection failure (not a git repo, `git` not on `PATH`, or the `git rev-parse --git-common-dir` subprocess failing) — exiting `0`. The one narrowed case: a target **outside the current worktree root** that git can prove lands inside the corresponding **main workspace root** is denied (exit 1), so a role agent running in a linked worktree cannot write the main workspace's same-named repo; a workspace-outside target that cannot be proven to live in the main workspace root still exits `0`. The authoritative fail-closed enforcement stays with the post-hoc `4x check <feature-id>`.

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

- **settings** — `settings.json` loadable, `project.name` non-empty, at least one runner defined, `default_runner` exists in the runners map, and `project.verify_command_allowlist` reports whether verify command prefix enforcement is configured.
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

**Executable AC checks**: when `test-strategy.yaml` declares `ac_checks` (binding an AC ID to one or more commands), verify runs each command, records the real exit codes into that AC's `ac_results[].checks`, and sets the AC's `passed` from those exit codes (all exit 0 = passed). Any failing `ac_check` AC makes the whole verify fail (non-zero exit). This exit-code result is authoritative — the guard recomputes `passed` from `checks` and overrides any hand-written value. When `ac_checks` is absent, behavior is unchanged.

**Command allowlist**: when `project.verify_command_allowlist` is non-empty, every AI-produced verify command (`verify_commands`, `verify_groups`, and `ac_checks`) is checked before `sh -c` execution. The command is split on shell control operators (`;`, `&`, `|`, and newline), every non-empty segment must start with an allowed prefix using a word boundary, and command/process substitution (`$(`, backtick, `<(`, `>(`) is rejected. Blocked commands are not executed and are recorded in `verify.json` with exit code `126` and error `"blocked"`. An empty allowlist preserves the older behavior. Build-gate/docs-gate commands from `settings.json` are intentionally exempt.

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

Before merging, 4x commits its own pipeline state in the main workspace — `.4x/features/*.yaml`, `.4x/learnings.json` and `.4x/learnings-context.md` — as `chore(<feature-id>): 4x pipeline state`. That commit is path-scoped, so any other uncommitted tracked change in the main workspace is left untouched and still aborts the merge. `4x merge` does the same before completing.

If the feature touched self-mod protected paths (see the `self_mod_guard` settings), the merge is blocked until you confirm with `--approve-self-mod`; the touched protected paths are printed for review:

```
4x done <feature-id> --approve-self-mod
```

When `issue_tracker.enabled` is `true` in `settings.json`, `4x done` pushes the feature branch and opens a MR/PR per repo with committed changes instead of merging locally — `done` then means "MR opened", not "merged". Each opened URL is printed as `MR opened[(repo)]: <url>` (and included as `mrUrls` in `--json` output); a repo that fails to push or open its MR keeps the feature in `pending-review` with the worktree preserved for a retry. See [Concepts](concepts.md#issue-first-mr-flow).

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->
Force a feature to done from any phase, bypassing the normal state machine. Requires `--reason` to document why.

```
4x force-done <feature-id> --reason "descoped, no longer needed"
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

The Acceptor of each feature writes a `retro-learnings.json`; the CLI harvests it into `.4x/learnings.json`. When generating each role's prompt, the CLI directly filters `.4x/learnings.json` by that role's category (with active/candidate quota buckets) and injects the result — there is no intermediate step where a Designer selects entries first. learnings are managed entirely by the CLI — runners never write `learnings.json` directly, and any learnings failure only warns without blocking state transitions.

```
4x learn add --category <cat> --content <text>  # add a learning manually (standalone sessions)
4x learn add --category ops --content "..." --json  # JSON output: {"id":"L0xx","added":true}
4x learn list                     # list active + candidate learnings (default)
4x learn list --category=testing  # filter by category
4x learn list --status=active     # filter by status (active, candidate, stale, promoted)
4x learn list --ineffective       # only show ineffective entries (used≥3 + 30d + same-category churn)
4x learn prune                    # demote inactive active → candidate; age idle candidates → stale; remove stale
4x learn prune --dry-run          # preview demoted actives and stale removals without writing
4x learn promote <id>             # mark a learning as promoted (kept but no longer injected)
4x learn remove <id>              # remove a learning entry
4x learn context                  # generate .4x/learnings-context.md snapshot
```

`learn add` checks for similar existing entries (exact, normalized, and Jaccard similarity). If a fuzzy duplicate is found, it reports the existing ID and does not write.


- Categories: `design`, `code-quality`, `testing`, `review`, `tooling`, `process`, `ops`
- Status: `active` (injectable), `candidate` (new harvest, pending cross-feature validation), `stale` (aged out, pending removal), `promoted` (upgraded to template/instructions)
- Each learning carries a `confidence` score (0–1) that is reinforced every time the entry is injected into a role prompt; prompt injection and `.4x/learnings-context.md` rank by confidence first, then recency, then ID, and truncate the lowest-scored entries once the token budget is reached. Legacy entries without a `confidence` value fall back to a deterministic score derived from `used_count` (never written back on read)
- `4x learn prune` first demotes inactive active entries back to `candidate`: an `active` learning whose last hit (by `last_used`, else `activated_at`, else `created_at`) is older than `evolution.active_demote_days` (default 90; set to 0 to disable demotion) becomes a `candidate` again, handing it back to the candidate aging path rather than deleting it. `promoted` entries are never demoted
- `4x learn prune` then ages out never-used candidates: a `candidate` with `used_count=0` created more than `evolution.candidate_max_idle_days` ago (default 30; set to 0 to disable aging) is marked `stale` so the sample pool actually converges. Aging only fires from `prune` and never touches active/promoted entries; `--dry-run` previews demoted actives and aged/stale candidates separately without removing them (a newly demoted active is never removed in the same run)
- Candidate entries are shown with `*` suffix on ID; they auto-promote to active when independently produced by a different feature or selected by a Designer
- Ineffective entries are active learnings marked with `active!` status when: used ≥ 3 times, activated > 30 days ago, and the same category keeps producing new learnings — indicating the learning isn't reducing repeat issues
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
| `role.<name>.<field>` | `4x config set role.deep-reviewer.runner codex` | Per-role `model`/`deep_model`/`runner`/`parallel_reviewers`/`angles_per_reviewer` |

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

## `4x skills`

Manage the skills bundled in this repo's `skills/` directory. Installation is **symlink-only** — 4x links `skills/<name>/` into `~/.claude/skills/<name>`, so a later `git pull` updates the skill automatically without re-installing. Run these commands from within the 4x repo (the `skills/` directory is located by walking up from the current directory).

```
4x skills list [--json]     # list available skills (name + description)
4x skills install <name>    # symlink skills/<name>/ into ~/.claude/skills/<name>
4x skills remove <name>     # remove the ~/.claude/skills/<name> symlink
```

- `list` marks installed skills with a `✓` and flags owner-only skills (e.g. `4x-autopilot`) with a WARNING.
- `install` is idempotent — re-installing an already-linked skill is a no-op. It refuses to overwrite a real directory or a symlink pointing elsewhere.
- `remove` only deletes symlinks; it never deletes files inside the repo, and refuses to delete a real (non-symlink) entry.

Installing `4x-autopilot` prints a WARNING: it is owner-only (fully automatic merge).

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

The `4567` default is read from `internal/server.DefaultPort`, the single source of truth also used by the macOS/Tauri shells; see [Port source of truth](dashboard.md#port-source-of-truth) for how the three copies stay in sync.

Without paths, loads recent projects from `~/.4x/recent-projects.json` (LRU, max 20). With paths, opens each as a project tab.

On startup `4x live` issues a per-session bearer token (written to `~/.4x/live-token`, `0600`) and guards `/api/*` and `/sse/*` endpoints with it; the launch URL carries `?token=…` so the browser can pick it up. Disable via `"dashboard_auth": false` in `~/.4x/settings.json`. See [Authentication](dashboard.md#starting-the-dashboard) for details.

---

## `4x guard-tool`

<!-- alias: 4x guardtool -->
Internal PreToolUse hook (hidden, machine use only). The `claude` runner injects this hook for **every** Claude role (matchers `Bash` and `Edit|Write|MultiEdit`); each branch self-selects by role, so injecting for all roles has no side effect. It reads the Claude Code hook JSON from stdin and always exits 0 — any parse failure, missing state, or non-matching tool is allowed, and it never fails the run.

- **`Bash` branch (reviewer git exploration):** for the `reviewer`/`deep-reviewer` roles, when the review-package.md for the round exists, the reviewer's own `git diff`/`git log`/`git show` calls are softly denied with a message pointing to review-package.md. Reads `FOURX_ROLE` / `FOURX_REVIEW_PACKAGE`; non-reviewer roles (empty `FOURX_ROLE`) pass through. Never blocks build/test/lint.
- **`Edit`/`Write`/`MultiEdit` branch (write-gate):** applies the same role×path check as [`4x check --path`](#4x-check-feature-id) to the tool's `file_path`. The current role comes from `state.json` (feature id from `FOURX_FEATURE_ID` or the unique active feature), so this branch applies to **all** roles — e.g. a `reviewer`/`designer`/`tester` editing a source file outside `.4x/`, or a `coder` writing outside the feature's repos, is denied at edit time. Fail-open: any read error allows the edit (the authoritative fail-closed check remains post-hoc `4x check`).

```
# Bash branch (reviewer git exploration)
echo '{"tool_name":"Bash","tool_input":{"command":"git diff HEAD"}}' | FOURX_ROLE=reviewer FOURX_REVIEW_PACKAGE=/path/to/review-package.md 4x guard-tool

# Write-gate branch (role×path check on file_path)
echo '{"tool_name":"Edit","tool_input":{"file_path":"cmd/4x/foo.go"}}' | 4x guard-tool
```

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

**Maintenance**

| Tool | CLI command |
|---|---|
| `4x_clean` | `clean` |
| `4x_doctor` | `doctor` |
| `4x_learn_add` | `learn add` |
| `4x_learn_list` | `learn list` |
| `4x_learn_prune` | `learn prune` |
| `4x_learn_promote` | `learn promote` |
| `4x_learn_remove` | `learn remove` |
| `4x_learn_context` | `learn context` |
| `4x_evolve` | `evolve` |

**Config & gate**

| Tool | CLI command |
|---|---|
| `4x_config_get` | `config get` |
| `4x_config_set` | `config set` |
| `4x_config_list` | `config list` |
| `4x_gate` | `gate --pre` / `gate --post` |

Orchestration and interactive commands (`init`, `sync`, `live`, `prompt`, `event`) are intentionally not exposed as MCP tools.
