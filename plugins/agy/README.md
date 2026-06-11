# Antigravity CLI Plugin

Antigravity CLI as a 4x runner.

## Runner Config

Add to `.4x/settings.json`:

```json
{
  "runners": {
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  }
}
```

## How it works

- `AGY.md` in this directory provides 4x protocol instructions to Antigravity CLI.
- Antigravity CLI reads `AGY.md` automatically when invoked in a directory.
- The runner passes the role prompt via `{prompt}` substitution.

## Setup

1. Install Antigravity CLI: `npm install -g @google/antigravity-cli` or via Google's distribution
2. Copy `AGY.md` to your project root, or symlink this plugin's `AGY.md`
3. Add the runner config above to `.4x/settings.json`
4. Run: `4x run <feature-id> --runner agy`
