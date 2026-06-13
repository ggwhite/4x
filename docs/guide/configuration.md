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
  "default": "claude",
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
| `setup` | Setup commands (e.g., `docker-compose up -d`) |
| `docs` | Documentation file paths for Designer reference |
| `rules` | Project-specific rules injected into role prompts |

### Runner Config

| Field | Description |
|---|---|
| `command` | Executable name |
| `args` | Arguments. `{prompt}` and `{promptFile}` are replaced at runtime. `{model}` is replaced with the role's model. |
| `model` | Default model for this runner |
| `model_map` | Map of role model names to runner-specific names (e.g. `{"opus": "claude-opus-4-5-20250514"}`). Lookup order: role model → model_map translation → fallback to original name. |
| `output_format` | Set to `"stream-json"` for runners whose stdout should be parsed into readable `.log` plus raw `.stream.jsonl` files. |
| `tty` | Use PTY for capturing output. Ignored when `output_format` is `"stream-json"`. |
| `stdin` | Send prompt via stdin instead of argument (used by Codex) |
| `quiet` | Suppress runner stdout in terminal; output is still captured in log files |

If `{model}` is not present in `args`, the runner auto-appends `--model <model>`.

### Role Config

| Field | Description |
|---|---|
| `model` | Model name for this role |
| `deep_model` | Model for adversarial review pass (reviewer only) |
| `instructions` | Additional instructions injected into the role prompt |
| `includes` | Files to include in the role prompt |

### Other Config Fields

| Field | Description |
|---|---|
| `hub_repos` | Shared repositories (for batch DAG grouping) |
| `isolation` | Set to `"worktree"` to run features in git worktrees |
| `max_concurrent_runs` | Max concurrent runs via the dashboard server |
| `commit` | Commit strategy: `"per-round"` (default), `"on-done"`, or `"never"` |

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
- **Project-only fields** (`project`, `isolation`, `commit`, `max_concurrent_runs`, `hub_repos`, `rules`, `model_tiers`): always taken from project config, never overridden by user config.

The dashboard's project settings editor shows the **raw** project config, not the merged result. Use the **Merged** tab in project settings to see the final effective settings after merge.
