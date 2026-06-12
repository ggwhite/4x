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

Create a new feature.

```
4x new "Feature title" [--repo <repo>...]
```

| Flag | Description |
|---|---|
| `--repo` | Repository in scope (repeatable for multi-repo features) |

Creates `.4x/features/F{NNN}-{slug}.yaml` with status `not-started`.

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

The loop drives: init → designing → coding → reviewing → testing → accepting → pending-review. On review failure, code gets another pass. On test failure, the loop re-enters coding.

Automatically checks dependency gate — blocks if depended features are not done.

If `isolation: "worktree"` is set in config, runs in a git worktree under `.worktrees/4x/<feature-id>/`.

---

## `4x status [feature-id]`

Show feature status.

```
4x status              # all features, grouped by state
4x status <feature-id> # single feature details with subtasks
4x status --pending    # filter pending-review features
```

Groups: Running, Review, Pending, Todo, Done (done shows max 5). Includes backlog drift warnings.

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

## `4x transition <feature-id>`

Force a state transition.

```
4x transition <feature-id> --to <phase> [--role <role>]
```

| Flag | Description |
|---|---|
| `--to` | Target phase (required) |
| `--role` | Role performing the transition |

Validates the transition is legal per the state machine. Auto-initializes state if it doesn't exist. The `testing → accepting` transition runs additional gates (verify.json, test-report.md, final-report.md, commit-plan.md must exist and verify must pass).

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

---

## `4x done <feature-id>`

Mark a pending-review feature as done.

```
4x done <feature-id>
```

Only works when feature is in `pending-review` phase. Errors on any other phase.

---

## `4x config`

Manage user-level configuration (`~/.4x/settings.json`).

```
4x config list          # show all user config
4x config get <key>     # get a value
4x config set <key> <value>  # set a value
```

Currently supported key: `locale`.

---

## `4x upgrade`

Re-deploy embedded plugin files to an existing project.

```
4x upgrade [--dry-run]
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
4x batch next
```

### `4x batch run`

Run eligible features sequentially in dependency order.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| Flag | Default | Description |
|---|---|---|
| `--runner` | config default | Runner plugin name |
| `--max-rounds` | `5` | Max rounds per feature |
| `--timeout` | `3600` | Per-phase timeout in seconds |

Polls for `.4x/batch-stop` file between features for graceful shutdown.

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
