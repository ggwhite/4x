# Codex Plugin

OpenAI Codex CLI as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    }
  }
}
```

## How it works

- `AGENTS.md` in this directory provides 4x protocol instructions to Codex
- Codex reads `AGENTS.md` automatically when invoked in a directory
- The runner pipes the role prompt to Codex via stdin (`"stdin": true`)
- Codex operates in its default sandbox (configure with `-s` flag in args if needed)

## Setup

1. Install Codex CLI: `npm install -g @openai/codex`
2. Copy `AGENTS.md` to your project root, or symlink this plugin's `AGENTS.md`
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner codex`
