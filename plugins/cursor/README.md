# Cursor Plugin

Cursor IDE background agent as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "cursor": {
      "command": "cursor",
      "args": ["--background", "--prompt-file", "{promptFile}"]
    }
  }
}
```

> **Note:** Uses `{promptFile}` because Cursor's CLI interface passes prompts via file.

## Status: Planned

Cursor's background agent mode is under active development. This plugin will be functional once Cursor exposes a stable CLI interface for headless agent execution.

Current blockers:
- Cursor is primarily an IDE — no stable headless CLI mode yet
- Background agent API may require Cursor Pro subscription
- File write behavior and sandbox model are not yet documented

## How it works (planned)

- `.cursorrules` in the project root provides 4x protocol instructions
- Cursor reads `.cursorrules` automatically
- The runner invokes Cursor's background agent with the role prompt

## Setup (once available)

1. Install Cursor and ensure `cursor` CLI is in PATH
2. Copy `.cursorrules` to your project root
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner cursor`
