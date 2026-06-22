# Contributing to 4x

Thanks for your interest in contributing! This guide covers the main ways you can help.

## Quick Reference

```bash
make build          # Build CLI to bin/4x
make test           # go test -race ./...
make lint           # go vet ./...
make check-docs-sync  # Check if docs need updating
make check-i18n     # Check i18n key sync
```

All PRs must pass `make test` and `make lint`.

## Ways to Contribute

### Bug Reports & Feature Requests

Open an [issue](https://github.com/ggwhite/4x/issues) using the appropriate template. Include your `4x version` output and the runner you're using.

### Documentation & Translations

Docs live in `docs/guide/`. Translations live in `docs/translate/` (README) and `docs/guide/{lang}/` (guide pages). Currently supported: en, zh-TW, zh-CN, ja, ko, es.

To add a new language or fix a translation, just edit the file and submit a PR.

### Examples

Examples live in `examples/`. Each example is a self-contained project with a `.4x/` workspace. We especially welcome examples in different languages (Python, TypeScript, Java, Rust) and different scenarios (batch mode, multi-runner).

A good example includes:
- A small but realistic project (not just hello-world)
- A few feature YAMLs showing different complexity levels
- A README explaining what the example demonstrates

### New Runner Plugins

This is the most impactful contribution. A plugin teaches a new AI coding agent to speak the 4x protocol.

#### What's in a plugin

Each plugin lives in `plugins/{name}/` and contains:

| File | Purpose |
|---|---|
| `CLAUDE.md` / `AGENTS.md` / `GEMINI.md` / etc. | Instruction file the AI agent reads |
| `README.md` | Setup instructions and runner config |

The instruction file teaches the agent the 4x role contract: what each role reads, writes, and cannot do. See any existing plugin (e.g. `plugins/codex/`) for the pattern.

#### Two ways to contribute a plugin

**Option A: Full PR (instruction file + Go integration)**

1. Create `plugins/{name}/` with instruction file + README
2. Add default runner config in `internal/protocol/types.go` (`DefaultRunners`)
3. Add embed directive in `plugins/embed.go`
4. Add deploy logic in `internal/protocol/workspace.go` (`DeployPlugins`)
5. Submit PR

**Option B: Instruction file only**

If you don't want to touch Go code, submit a PR with just `plugins/{name}/` containing the instruction file, README, and a suggested runner config JSON. The maintainer will handle the Go integration.

#### Runner config format

A runner config tells 4x how to invoke the CLI tool:

```json
{
  "command": "your-cli",
  "args": ["run", "--prompt", "{prompt}"],
  "stdin": false,
  "tty": false,
  "quiet": false
}
```

- `{prompt}` — replaced with the role prompt inline
- `{promptFile}` — replaced with a temp file path containing the prompt
- `{model}` — replaced with the resolved model name
- `stdin: true` — pipe the prompt to stdin instead of using args
- `tty: true` — allocate a pseudo-terminal (for CLIs that require it)
- `quiet: true` — suppress stdout (capture to log file only)

See [Plugin Contract](docs/reference/plugin-contract.md) for the full spec.

### Core CLI Changes

The CLI is written in Go 1.26+. Architecture:

```
cmd/4x/          CLI entry (Cobra), one file per subcommand
internal/
  protocol/      .4x/ file format, workspace I/O, types
  state/         State machine (phase transitions)
  guard/         Guardrail checks (scope/baseline/required files)
  batch/         Batch DAG scheduling
  runner/        Runner interface and subprocess execution
  server/        SSE + REST server (dashboard)
```

Before submitting a core change:
1. Read the relevant code in `internal/` and `cmd/4x/`
2. Check `docs/` for design specs that may constrain the change
3. Write tests — the project uses Go's standard `testing` package
4. Run `make test && make lint`
5. Run `make check-docs-sync` to see if docs need updating

## Development Setup

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
make build
bin/4x --version
```

## Pull Request Process

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Ensure `make test` and `make lint` pass
4. Fill out the PR template (summary, test plan, checklist)
5. Submit — a maintainer will review

## Code Style

- Follow `gofmt` and `go vet`
- One file per CLI subcommand in `cmd/4x/`
- Internal packages in `internal/`, not exported
- No LLM calls in the CLI layer — all AI interaction belongs in plugins
- Minimal comments — good naming over comments

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
