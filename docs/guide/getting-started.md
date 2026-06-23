# Getting Started

## Installation

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
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

### macOS Gatekeeper

The CLI binary and the 4x Live dashboard app are not signed with an Apple Developer certificate. macOS Gatekeeper will block them on first launch. Two ways to fix this:

**Option A: Remove quarantine attribute (recommended)**

```bash
# For the CLI binary
xattr -cr /usr/local/bin/4x

# For the dashboard app
xattr -cr /Applications/4x\ Live.app
```

**Option B: Allow via System Settings**

1. Double-click the app — macOS shows "cannot be opened because the developer cannot be verified"
2. Open **System Settings → Privacy & Security**
3. Scroll down to the **Security** section — you'll see a message about the blocked app
4. Click **Open Anyway**
5. Enter your password or use Touch ID to confirm
6. The app will launch; macOS remembers your choice for future launches

### Windows SmartScreen

The binary is not signed with a code signing certificate. Chrome and Edge may block the download, and Windows SmartScreen may block execution.

**Download blocked by browser:**

1. Chrome: click the download warning → **Keep** → **Keep anyway**
2. Edge: click `...` on the download bar → **Keep** → **Show more** → **Keep anyway**

**Execution blocked by SmartScreen:**

1. Double-click the exe — Windows shows "Windows protected your PC"
2. Click **More info**
3. Click **Run anyway**

Alternatively, unblock via PowerShell:

```powershell
Unblock-File -Path .\4x.exe
```

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

## Version Control

`4x init` creates a `.4x/.gitignore` that excludes runtime artifacts. Commit the rest:

| Path | Track | Why |
|---|---|---|
| `.4x/settings.json` | **Yes** | Project config — shared across the team |
| `.4x/features/*.yaml` | **Yes** | Feature definitions |
| `.4x/learnings.json` | **Yes** | Cross-feature retro knowledge base |
| `.4x/candidates.json` | **Yes** | Auto-discovered feature candidate pool |
| `.4x/plugins/` | **Yes** | Runner instruction files |
| `.4x/run/` | **No** | Runtime artifacts (state, logs, reports) — auto-excluded by `.gitignore` |

For existing projects initialized before this feature, add the gitignore manually:

```bash
printf 'run/\ngate-input.json\ngate-verdicts.json\nevolve-state.json\n' > .4x/.gitignore
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
