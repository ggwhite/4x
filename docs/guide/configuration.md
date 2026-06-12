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
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus",
      "tty": true
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
| `tty` | Use PTY for capturing output (needed for CLI tools with ANSI output like Claude Code) |
| `stdin` | Send prompt via stdin instead of argument (used by Codex) |

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

Global user preferences. Managed via `4x config`.

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

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
