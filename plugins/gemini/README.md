# Gemini CLI Plugin

Google Gemini CLI as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "gemini": {
      "command": "gemini",
      "args": ["-p", "{prompt}"]
    }
  }
}
```

## How it works

- `GEMINI.md` in this directory provides 4x protocol instructions to Gemini CLI
- Gemini CLI reads `GEMINI.md` automatically when invoked in a directory
- The runner passes the role prompt via `{prompt}` substitution

## Setup

1. Install Gemini CLI: `npm install -g @anthropic-ai/gemini-cli` or via Google's distribution
2. Copy `GEMINI.md` to your project root, or symlink this plugin's `GEMINI.md`
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner gemini`
