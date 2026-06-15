# Getting Started

## Installation

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/4x
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

Requires Go 1.26+.

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### Download Binary

Pre-built binaries for macOS, Linux, and Windows (amd64 / arm64) are available on the [Releases](https://github.com/ggwhite/4x/releases) page.

### Verify

Verify with:

```bash
4x --help
```

## Initialize a Project

```bash
cd my-project
4x init
```

This creates a `.4x/` directory with:
- `settings.json` — project config, runner definitions, role model mappings
- `plugins/` — runner instruction files (CLAUDE.md, AGENTS.md, GEMINI.md, etc.)
- Root-level import files (CLAUDE.md, AGENTS.md, GEMINI.md, etc.)

4x auto-detects your project language (Go, TypeScript, JavaScript, Java, Rust, Python) and pre-fills build/test/lint commands.

If `.4x/` already exists, `init` exits with an error — use `4x sync` to refresh plugin files.

## Create a Feature

```bash
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication-w (User authentication with OAuth2)

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Features are stored in `.4x/features/{id}.yaml`. The ID format is `F{NNN}-{slug}` (slug up to 23 characters).

Use `--repo` to declare which repositories are in scope (for multi-repo projects).

```bash
4x new "Auth refactor" --id auth-refactor --desc "Refactor auth middleware" --priority 1
4x new "Batch mode" --subtask "extract:Extract logic" --depends F001
```

See `4x new --help` or [CLI Reference](cli.md) for all flags (`--id`, `--desc`, `--subtask`, `--rule`, `--depends`, `--priority`).

## Run the Loop

```bash
# Run with default runner (usually claude)
4x run F001

# Specify a runner
4x run F001 --runner claude

# Limit iterations
4x run F001 --max-rounds 3

# Set timeout in seconds (default: 3600)
4x run F001 --timeout 7200

# Use a specific pipeline profile
4x run F001 --profile quick

# Preview prompts without calling LLM
4x run F001 --dry-run
```

Feature IDs support prefix matching — `4x run F001` and `4x run f001` both work.

The loop runs: **Design → Code → Review → Test → Deep Review → Accept → Pending Review**. If Review finds issues, Code gets another pass. If Test fails, the loop iterates (up to `--max-rounds`).

## Check Status

```bash
# All features
4x status

# Single feature with details
4x status F001

# Show only non-done features
4x status --pending
```

## Complete a Feature

After the loop finishes, the feature lands in `pending-review` state — waiting for human sign-off.

```bash
# Review the outputs
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# Mark as done
4x done F001
```

## Upgrade Plugin Files

When you update the `4x` binary, re-deploy embedded plugins:

```bash
4x sync            # deploy new files
4x sync --dry-run  # preview changes only
```

## Next Steps

- [CLI Reference](cli.md) — all commands and flags
- [Core Concepts](concepts.md) — understand roles, state machine, protocol
- [Configuration](configuration.md) — customize models, runners, locale
