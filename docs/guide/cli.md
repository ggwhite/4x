# CLI Reference

All feature-id arguments support case-insensitive prefix matching. `4x run f001`, `4x run F001-user`, and `4x run F001` all resolve to `F001-user-authentication-w`. Ambiguous prefixes produce an error listing the matches.

---

## `4x init`

Initialize a `.4x/` workspace in the current directory.

```
4x init
```

- Auto-detects project language and build/test/lint commands
- Creates `.4x/settings.json` with 4 default runners (claude, codex, gemini, agy)
- Deploys embedded plugin files to `.4x/plugins/`
- Adds `@import` lines to root-level files (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md)
- Errors if `.4x/` already exists

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
| `--priority` | Priority level (1=high, 2=medium, 3=low) |
| `--repo` | Repository in scope (repeatable) |
| `--json` | Output as JSON |

Creates `.4x/features/F{NNN}-{slug}.yaml` with status `not-started`.
Auto-generated slug truncates at word boundary; use `--id` to override.

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
| `--profile` | auto | Pipeline profile (`full`/`normal`/`quick` or custom); overrides priority-based auto-select |

`--profile` selects which roles run. Built-in profiles: `full` (all 6 roles), `normal` (coder/reviewer/tester/acceptor), `quick` (coder/reviewer). Roles not in the profile are passed through (state advances along the legal edge without invoking the runner). When omitted, the profile is auto-selected by the feature's priority if a `profiles` section exists in `settings.json` (otherwise `full`). `--profile` is mutually exclusive with `--only`. See [Configuration → Profiles](configuration.md#profiles) for details.

The loop drives: init → designing → coding → reviewing → testing → accepting → pending-review. On review failure, code gets another pass. On test failure, the loop re-enters coding.

After each non-designer runner completes, guardrail checks are enforced automatically (scope, baseline, required files). A violation transitions the feature to `needs-attention` and stops the loop. Designer is exempt — it does not modify source code.

Review verdicts must start with `PASS` to pass. Blank lines between the `## Verdict` heading and the verdict text are ignored. Ambiguous output (`TODO`, `ERROR`, garbled text, missing `## Verdict` block) is treated as failure.

Phase hooks declared in `settings.json` or the feature YAML are executed automatically before and after each phase transition within the loop. See [Phase Hooks](concepts.md#phase-hooks) for configuration details.

When entering the `testing` phase (after `pre_testing` hooks, before the Tester runner is spawned), a health check verifies the environment if `health_check` is configured. Check commands run in order; on failure the recovery commands run once and the checks are retried once. If the environment still fails, the feature transitions to `needs-attention` and the loop stops. See [Health Check](concepts.md#health-check) for configuration details.

When `auto_discover_features` is enabled in `settings.json`, a final deep review **PASS** parses the `[NEW-FEATURE]` markers in `deep-review-report.md` and auto-creates feature YAMLs for the out-of-scope issues the deep reviewer flagged (deduplicated and capped). See [Configuration → Auto-Discover Features](configuration.md#auto-discover-features) and [Concepts → Auto-Discovered Features](concepts.md#auto-discovered-features) for details.

If the feature is in `blocked` or `needs-attention` phase, automatically recovers to the appropriate resume phase based on the current role.

Automatically checks dependency gate — blocks if depended features are not done.

If `isolation: "worktree"` is set in config, runs in a git worktree under `.worktrees/4x/<feature-id>/`. In multi-repo mode (workspace.repos configured), each repo gets its own worktree under `.worktrees/4x/<feature-id>/<repo-name>/`, and workspace-level files (go.work, Makefile, etc.) are copied alongside. Coder prompts include a `== Workspace Repos ==` section; in worktree mode, each entry shows the repo name as a relative path (e.g. `core → core/`) so the coder operates within the correct directory boundaries.

---

## `4x status [feature-id]`

Show feature status.

```
4x status              # all features, grouped by state
4x status <feature-id> # single feature details with subtasks
4x status --pending    # filter pending-review features
4x status --json       # output as JSON
```

| Flag | Description |
|---|---|
| `--pending` | Filter pending-review features |
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
| `--status` | New status: `done`, `in-progress`, `blocked`, `not-started` (required) |

Example:
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

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

Validates the transition is legal per the state machine. Auto-initializes state if it doesn't exist. The `testing → accepting` transition runs additional gates (verify.json, test-report.md, final-report.md, commit-plan.md must exist and verify must pass).

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

Supports locale injection (from user config or `LANG` env), planning doc auto-inclusion (`docs/design/{id}-spec.md` and `{id}-plan.md`), and project/role includes.

For the `tester` role, any `profiles` listed in the feature's `test-strategy.yaml` are resolved (via `loadProfiles`) and injected into the prompt as `== Test Profile: {name} ==` blocks. Each profile's content comes from `settings.json` `test_profiles[name]` (`content` or `include`) when present, otherwise the built-in `templates/profiles/{name}.md`. See [Test Profiles](concepts.md#test-profiles).

---

## `4x done <feature-id>`

Mark a pending-review feature as done. If the feature has a worktree (`.worktrees/4x/<id>`), automatically merges the branch back to main and removes the worktree and branch.

```
4x done <feature-id>
```

Only works when feature is in `pending-review` phase. Errors on any other phase.

If a merge conflict or merge error occurs, the feature remains in `pending-review`, the worktree is preserved, and guidance is printed. In multi-repo mode, the conflicting repo name is printed as `repo: <name>`. Use `4x merge <id>` to complete after resolving conflicts.

---

## `4x merge <feature-id>`

Complete a merge after resolving conflicts from `4x done`.

```
4x merge <feature-id>
```

Only works when feature is in `pending-review` or `done` phase and a worktree exists at `.worktrees/4x/<id>`. Commits resolved conflicts in the worktree, merges to main, then removes the worktree and branch. If the feature is still in `pending-review`, it is marked `done` after the merge succeeds.

In multi-repo mode, resolved conflicts are committed per repo (each repo under `.worktrees/4x/<id>/<repo-name>/` is staged and committed independently), then all repos are merged all-or-nothing. The conflicting repo name is shown as `repo: <name>` if a conflict recurs.

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
4x mcp [flags]
```

| Flag | Description |
|---|---|
| `--version` | Show MCP server version info |

Starts the 4x MCP stdio server to expose 4x CLI commands as MCP tools to LLM clients (e.g., Claude Code, Cursor).
