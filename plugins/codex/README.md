# Codex Plugin

OpenAI Codex CLI as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "codex": {
      "command": "codex",
      "args": ["exec", "-C", ".", "-s", "workspace-write", "{prompt}"]
    }
  }
}
```

## How it works

- `AGENTS.md` in this directory provides 4x protocol instructions to Codex
- Codex reads `AGENTS.md` automatically when invoked in a directory
- The runner passes the role prompt via `{prompt}` substitution
- Codex operates in `workspace-write` sandbox (file I/O allowed, no network)

## Setup

1. Install Codex CLI: `npm install -g @openai/codex`
2. Copy `AGENTS.md` to your project root, or symlink this plugin's `AGENTS.md`
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner codex`
