# Multi-Runner — 4x Example

Demonstrates assigning different AI runners to different phases of the same feature,
so you can leverage each model's strengths or manage cost by using cheaper models
for simpler roles.

## How It Works

4x resolves the runner for each phase using a priority chain:

1. `--runner` flag (manual, applies to all phases)
2. Feature-level `phase_overrides` in the feature YAML
3. Profile-level `PhaseSpec.Runner` in `settings.json`
4. `default_runner` in `settings.json`

This example uses approach 3 (profile-level) to map each phase to a specific runner.

## Configuration

In `.4x/settings.json`, define your runners and a profile with per-phase overrides:

```json
{
  "runners": {
    "claude": { "command": "claude", "..." : "..." },
    "gemini": { "command": "gemini", "..." : "..." },
    "codex":  { "command": "codex",  "..." : "..." }
  },
  "default_runner": "claude",
  "profiles": {
    "multi": {
      "phases": [
        { "phase": "designing",       "runner": "claude", "model": "opus" },
        { "phase": "design-reviewing", "runner": "claude", "model": "opus" },
        { "phase": "coding",          "runner": "gemini" },
        { "phase": "reviewing",       "runner": "claude", "model": "sonnet" },
        { "phase": "testing",         "runner": "codex" },
        { "phase": "deep-reviewing",  "runner": "claude", "model": "opus" },
        { "phase": "accepting",       "runner": "claude", "model": "sonnet" }
      ]
    }
  },
  "default_profile": "multi"
}
```

## Run

```sh
cd examples/multi-runner

# Run the full loop — each phase uses the runner defined in the "multi" profile
4x run config-parser

# Or specify the profile explicitly
4x run config-parser --profile multi

# Override all phases to a single runner (highest priority)
4x run config-parser --runner claude
```

## Per-Feature Override

You can also override runners at the feature level in the YAML, which takes
priority over the profile but yields to `--runner`:

```yaml
# .4x/features/config-parser.yaml
phase_overrides:
  coding:
    runner: gemini
  testing:
    runner: codex
    model: sonnet
```

## Why Mix Runners?

- **Design and review** benefit from stronger reasoning models (e.g., Claude Opus).
- **Coding** may work well with models that generate clean code quickly (e.g., Gemini).
- **Testing** can use cost-effective models that follow structured verify commands (e.g., Codex).
- Some teams mix runners to avoid vendor lock-in or to compare model output quality.

## Project Structure

```
examples/multi-runner/
├── .4x/
│   ├── settings.json              # runners + profile with per-phase mapping
│   └── features/
│       └── config-parser.yaml     # feature spec
├── go.mod                         # minimal Go module
└── README.md
```

## Next Steps

- Run `4x doctor config-parser` to preview which runner/model each phase resolves to.
- See `docs/reference/plugin-contract.md` for details on runner configuration.
- See `examples/todo-api/` for a single-runner walkthrough.
