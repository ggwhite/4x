# Configuration

## Project Config (`.4x/settings.json`)

Created by `4x init`. Contains project metadata, runner definitions, and role model mappings.

You can also edit this file visually from the **4x Live dashboard** — click the gear icon (⚙) next to the "4x Live" title, or press `Cmd+Shift+,`. The editor supports both a form view and a raw JSON view, validates required fields, and backs up the previous settings to `settings.json.bak` before writing.

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "docs_check": ["make check-docs-sync"],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
	    "claude": {
	      "command": "claude",
	      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
	      "model": "opus",
	      "output_format": "stream-json"
	    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default_runner": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project Section

| Field | Description |
|---|---|
| `name` | Project name (auto-detected from directory) |
| `language` | Detected language |
| `build` | Build commands |
| `test` | Test commands |
| `lint` | Lint commands |
| `docs_check` | Docs/i18n sync verification commands (e.g., `make check-docs-sync`). Run by the framework's docs-gate during `coding`/`amending`; results written to `docs-gate.json` (non-blocking). Empty → docs-gate skipped |
| `verify_command_allowlist` | Allowed command-prefix list for AI-produced verify commands (empty = enforcement off). Each `verify_commands`/`verify_groups`/`ac_checks` command is split on shell operators (`; & |` and newline); every segment must start with a listed prefix (word-boundary), and command/process substitution (`$(`, backtick, `<(`, `>(`) is rejected outright. Not quote-aware (metacharacters inside quotes are conservatively treated as delimiters). Blocked commands are recorded as `ExitCode 126` / `Error "blocked"` and not executed. build-gate/docs-gate commands from settings.json are exempt |
| `setup` | Setup commands (e.g., `docker-compose up -d`) |
| `description` | Project description (optional) |
| `docs` | Documentation file paths for Designer reference |
| `rules` | Project-specific rules injected into role prompts |
| `includes` | Files to include in role prompts |
| `runner_env` | Environment-variable filtering for spawned runner subprocesses. `denylist` (list of glob patterns appended to the built-in defaults) and `allowlist` (patterns that override the denylist — use it to un-filter a built-in default pattern). See the environment filtering notes below. |

### Runner Config

| Field | Description |
|---|---|
| `command` | Executable name |
| `args` | Arguments. `{prompt}` and `{promptFile}` are replaced at runtime. `{model}` is replaced with the role's model. |
| `model` | Default model for this runner |
| `tiers` | Map of tier names to runner-specific model names (e.g. `{"opus": "claude-opus-4-5-20250514"}`). Lookup order: role model → tiers translation → fallback to original name. |
| `output_format` | Set to `"stream-json"` for runners whose stdout should be parsed into readable `.log` plus raw `.stream.jsonl` files. |
| `tty` | Use PTY for capturing output. Ignored when `output_format` is `"stream-json"`. |
| `stdin` | Send prompt via stdin instead of argument (used by Codex) |
| `quiet` | Suppress runner stdout in terminal; output is still captured in log files |
| `transient_retries` | Max automatic retries when the runner exits non-zero with a transient error in stderr (socket closed, connection reset, rate limit, 5xx, etc.). Omit for the default (3); set `0` to disable; set a positive number for a custom limit. |
| `env_allowlist` | Additional environment-variable glob patterns to allow through the filter for this runner only (unioned with the built-in per-runner credential allowlist). Project value fully replaces the user value. |

If `{model}` is not present in `args`, the runner auto-appends `--model <model>`.

Before spawning a runner subprocess, 4x filters the inherited environment so sensitive credentials are not exposed to the autonomous AI subprocess. Filtering rules:

- **Built-in denylist** (always applied): `*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`, `*_CREDENTIALS`, `*_SECRETS`, `AWS_*`, `SECRET_*`, `GITHUB_TOKEN`, `GH_TOKEN`. Add more via `runner_env.denylist`.
- **Per-runner built-in allowlist** (keyed by runner **name**, e.g. `claude` → `ANTHROPIC_API_KEY`/`ANTHROPIC_*`/`CLAUDE_*`, `codex` → `OPENAI_API_KEY`/`OPENAI_*`/`CODEX_*`, `copilot` → `GITHUB_TOKEN`/`GH_TOKEN`/`COPILOT_*`). The name is the key in `runners`; if it doesn't match a known runner, the command basename (minus `.exe`/`.cmd`/`.bat`) is tried as a fallback, so an absolute-path or wrapper `command` still resolves. Extend per runner via `env_allowlist`.
- **Allowlist overrides denylist**: a variable matching both is kept. This is how `ANTHROPIC_API_KEY` (which matches `*_KEY`) survives for `claude`, and how you remove a built-in denylist pattern — add it to `runner_env.allowlist`.
- **Always kept**: `PATH`, `HOME` (plus `USERPROFILE`/`SYSTEMROOT` on Windows), 4x's own injected variables (`FOURX_BIN`, `FOURX_ROLE`, …), and F154 worktree variables are never filtered.
- Patterns are `*` globs matched **case-insensitively**. Variables that are filtered out are surfaced via `slog.Debug` (key names only, never values).

#### Transient retry

When a runner subprocess exits non-zero **and** its captured stderr matches a known transient error pattern (`socket closed`, `connection reset`, `connection refused`, `rate limit` / `429`, `5xx` server errors, `i/o timeout`, etc.), 4x automatically re-runs the same command with exponential backoff (default base 1s, capped at 30s), up to `transient_retries` times. A retry that eventually succeeds returns exit 0 transparently, so a long-running feature run is not interrupted by a network hiccup.

Non-transient failures (compilation errors, assertion failures, panics), exit 0, context cancel/timeout, and command-launch failures are never retried, and the final attempt's exit code semantics (0/1/2) are unchanged.

### Role Config

| Field | Description |
|---|---|
| `model` | Model name for this role |
| `deep_model` | Model for adversarial review pass (reviewer only). **Required for the `deep-reviewing` phase to execute** — if unset, the phase is skipped and the run transitions directly from `testing` to `accepting`. |
| `runner` | Runner this role runs on for its entire pipeline (must exist in `runners`; empty = inherit `default_runner`). Use it for cross-model adversarial review — e.g. `roles.deep-reviewer.runner: codex` puts the Deep Reviewer on a different model family than the Coder. Precedence: `phase_overrides.{phase}.runner` > profile per-phase runner > `roles.{role}.runner` > `default_runner`. |
| `max_fix_rounds` | Max self-heal iterations in the `deep-reviewing` phase (`deep-reviewer` only; default 2). Each iteration runs a scoped mini-coder + re-verifier; exceeding the cap escalates to `needs-attention`. |
| `instructions` | Additional instructions injected into the role prompt |
| `includes` | Files to include in the role prompt |
| `screenshot_dir` | Directory path for tester screenshots |
| `parallel_reviewers` | Number of parallel sub-reviewers for deep review (deep-reviewer only; <=1 falls back to single-agent mode) |
| `angles_per_reviewer` | Review angles per sub-reviewer (deep-reviewer only; 0 means auto-distribute evenly) |
| `angle_mapping` | Map of path patterns → angle numbers for selective deep review (deep-reviewer only). Keys are path prefixes; keys starting with `*` are suffix patterns (e.g. `*_test.go`). When unset, a built-in default mapping is used. See [Concepts → Selective Deep Review Angles](concepts.md#selective-deep-review-angles). |

### Other Config Fields

| Field | Description |
|---|---|
| `hub_repos` | Shared repositories (excluded from repo-based scope clustering) |
| `isolation` | Set to `"worktree"` to run features in git worktrees |
| `max_concurrent_runs` | Max concurrent runs via the dashboard server |
| `commit` | Commit strategy: `"per-round"` (default), `"on-done"`, or `"never"` |
| `profiles` | Named pipeline profiles (role subsets); see [Profiles](#profiles) |
| `parallel_review_test` | Run reviewer and tester concurrently during the reviewing phase (default `false`) |
| `auto_discover_features` | Auto-create features from `[NEW-FEATURE]` markers in the deep review report (default `false`); see [Auto-Discover Features](#auto-discover-features) |
| `workspace` | Multi-repo workspace configuration (repo name → path mapping) |
| `hooks` | Lifecycle hooks (keyed by hook point, e.g. post-run) |
| `health_check` | Global pre-test environment check commands (can be overridden per-feature in test-strategy.yaml) |
| `test_profiles` | Custom or overridden test profile definitions (keyed by profile name) |
| `max_discovered_features` | Max features auto-created per run; unset or `<= 0` applies the default (`3`) |
| `notifications` | Enable OS notifications on run completion (default `true` when unset). Set to `false` to disable; the project value overrides the user-level setting. `4x run --no-notify` overrides both |
| `workspace` (go.work) | In multi-repo mode, `go.work` is automatically pruned when scaffolding a worktree; see below |
| `worktree.post_scaffold` | Commands run in the worktree root right after scaffolding; see below |

**Multi-repo `go.work` pruning.** In multi-repo mode a feature only checks out the repos it declares, but the workspace root `go.work` still lists every module via `use ./…`. Copied verbatim, those `use` entries would point at directories that were never checked out, so `go build/vet/test` fail across the board. When setting up a worktree 4x therefore prunes `go.work`, keeping only the modules actually present (`<worktree>/<repo>/go.mod` exists). If nothing remains, `go.work` (and `go.work.sum`) is omitted entirely so each module builds from its standalone `go.mod`. Monorepo worktrees are untouched (whole-repo checkout already contains every module). Workspaces without a `go.work` are unaffected.

**`worktree.post_scaffold`.** A string array of shell commands run in order in the worktree root immediately after it is scaffolded (mono and multi both). Use it for project-specific environment init that 4x does not build in. A failing command only logs a warning and the remaining commands still run — scaffolding never aborts. Combined stdout/stderr is appended to `<.4x>/run/<feature-id>/logs/post-scaffold.log`, and each command emits a `hook` / `post-scaffold` event. Only new worktrees run the hook; an existing worktree (idempotent re-scaffold) skips it.

```json
{
  "worktree": {
    "post_scaffold": [
      "chmod +x scripts/*.sh",
      "ln -sf ../shared/config.yaml config.yaml",
      "npm install --prefix web"
    ]
  }
}
```

**Worktree tool environment isolation.** When a runner (or a `test-strategy.yaml` verify command) executes inside a linked worktree, 4x injects worktree-local tool settings so results don't leak across worktrees: `GOLANGCI_LINT_CACHE` points at `<worktree>/.cache/golangci-lint` (golangci-lint's cache stores absolute paths, so a shared cache produces false lint errors referencing sibling worktrees; `GOCACHE` stays shared because Go's build cache is content-addressed and path-independent). Additionally, if the worktree contains an executable `bin/4x` (i.e. you are developing 4x itself and the feature branch built its own binary), `FOURX_BIN` is pinned to that worktree-local binary and its directory is prepended to `PATH` — guard-tool hooks and manual testing then exercise the freshly built branch binary instead of the main workspace's stale one; the pinned path is logged (`worktree env: FOURX_BIN pinned…`). Note that code-intelligence indexes built in the main workspace (e.g. a `codegraph` MCP index) do not cover commits made inside a worktree and return main-workspace absolute paths — point such tools at the main workspace explicitly and treat their view of worktree-only changes as stale.

### Auto-Discover Features

When `auto_discover_features` is `true`, the run loop parses the final deep review report (`deep-review-report.md`) after it **passes** and turns each `[NEW-FEATURE]` marker into a new feature YAML — capturing out-of-scope issues the deep reviewer spotted instead of letting them get buried.

- **Trigger point**: only fires when the final deep review passes (the first-pass PASS, or a PASS after self-heal). Intermediate rounds, reviewer/tester failures, and deep-review FAIL/needs-attention paths never reach it.
- **Dedup**: each candidate is compared (token-overlap similarity) against every existing feature's name + description, and against candidates already kept in the same batch. Similar candidates are skipped.
- **Cap**: at most `max_discovered_features` (default `3`) features are created per run; the rest are recorded as capped.
- **Output**: a `discovered-features.md` summary is written under `.4x/<feature-id>/` listing created / skipped-as-duplicate / capped candidates, and a `feature-discovered` event is appended per created feature.

All of this happens in the CLI layer (plain text parse + file writes, no LLM call) and never blocks the transition to `accepting` — any error is logged best-effort.

### Profiles

A profile selects which phases run for a feature, so simple features can skip the full pipeline. Phases not listed are passed through — the state advances along the legal edge without invoking the runner, checking artifacts, or running guards. `coding` is the only required phase; a profile missing it is a configuration error. The optional `design-reviewing` phase runs only when included, and its `design-review-report.md` must PASS before coding starts.

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

Each phase entry supports optional `runner` and `model` overrides:

| Field | Description |
|---|---|
| `phase` | Phase name (must be a selectable phase: designing, design-reviewing, coding, reviewing, testing, deep-reviewing, accepting) |
| `runner` | Optional runner override for this phase |
| `model` | Optional model tier override for this phase |

**Selection precedence:**

1. `4x run --profile <name>` — explicit override (looked up in `profiles`, then the built-in defaults).
2. Otherwise, if a `profiles` section exists, auto-select by the feature's `priority`: `null`/`0`/`1` → `full`, `2` → `normal`, `≥3` → `quick`.
3. If no `profiles` section exists, every feature runs `full` (priority-based auto-select is disabled — backward compatible).

The three built-in profiles (`full`/`normal`/`quick`) are always available as fallbacks even without a `profiles` section. The active profile name is recorded in the feature state and shown on the dashboard card.

When `parallel_review_test` is `true` and the active profile enables both `reviewer` and `tester`, the two read-only roles run concurrently in the same worktree during the reviewing phase; both passing advances to deep review, otherwise the loop re-enters coding.

## User Config (`~/.4x/settings.json`)

Global user preferences and runner defaults. Cross-project settings managed via `4x config` or the dashboard's **Global Settings** editor (⚙G button in sidebar).

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### User Config Fields

| Field | Description |
|---|---|
| `locale` | Language for role prompt instructions |
| `theme` | Dashboard theme (`dark`/`light`) |
| `default_runner` | Default runner name (overridden by project) |
| `runners` | Runner definitions (command, args, tty, etc.) |
| `roles` | Role model defaults |
| `logLevel` | Minimum log level (debug/info/warn/error; default "info"; overridden by FOURX_LOG_LEVEL env var) |
| `logRetainDays` | Days to retain log files in ~/.4x/logs/ (default 7) |
| `dashboard_auth` | Enable bearer-token auth for the `4x live` dashboard. Default `true` (enabled when unset); set `false` to disable. |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args` is an array field — edit `~/.4x/settings.json` directly to set it.

### Locale

Sets the language for role prompt instructions. Supported values:

| Value | Language |
|---|---|
| `en` | English (default) |
| `zh-TW` | Traditional Chinese |
| `zh-CN` | Simplified Chinese |
| `ja` | Japanese |
| `ko` | Korean |
| `es` | Spanish |
| `fr` | French |
| `de` | German |
| `pt` | Portuguese |
| `ru` | Russian |
| `vi` | Vietnamese |

The locale is also inferred from the `LANG` environment variable if not explicitly set.

## Settings Merge

When `4x run` or `4x prompt` executes, user-level and project-level settings are deep-merged:

- **Priority:** project > user > defaults
- **Runner merge:** per-field — project's non-zero fields override user's. `args` replaces entirely (not appended). `tiers` merges at key level.
- **Role merge:** per-field — same as runner.
- **Project-only fields**: all fields except `default_runner`, `runners`, and `roles` are project-only and never overridden by user config.

The dashboard's project settings editor shows the **raw** project config, not the merged result. Use the **Merged** tab in project settings to see the final effective settings after merge.
