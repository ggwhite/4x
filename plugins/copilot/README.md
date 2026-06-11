# Copilot Plugin

GitHub Copilot Coding Agent as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "copilot": {
      "command": "copilot-cli",
      "args": ["-p", "{promptFile}"]
    }
  }
}
```

> **Note:** Uses `{promptFile}` instead of `{prompt}` because Copilot prompts can exceed shell argument limits. The runner writes the prompt to a temp file and passes its path.

## Status: Experimental

GitHub Copilot's CLI agent capabilities are evolving. Current limitations:

- No `workspace-write` equivalent sandbox — file writes need explicit approval
- Session management differs from Claude/Codex/Gemini patterns
- The instruction file format may change as Copilot CLI matures

## How it works

- `AGENTS.md` in this directory provides 4x protocol instructions
- Copilot reads `AGENTS.md` when invoked in a project directory
- The runner passes the role prompt via a temp file (`{promptFile}` substitution)

## Setup

1. Install GitHub Copilot CLI (requires GitHub Copilot license)
2. Copy `AGENTS.md` to your project root
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner copilot`
