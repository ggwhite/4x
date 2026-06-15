# Runners & Plugins

## What is a Runner?

A runner is a bridge between the 4x CLI and an AI tool. The CLI generates role prompts and manages state; the runner sends prompts to the AI and captures output.

Runners are configured in `.4x/settings.json` under the `runners` key. The CLI invokes runners as subprocesses.

## Built-in Runners

| Runner | AI Tool | Mode | Status |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | Available |
| `codex` | OpenAI Codex CLI | Stdin | Available |
| `gemini` | Google Gemini CLI | Argument | Available |
| `agy` | Antigravity CLI | Argument | Available |
| `copilot` | GitHub Copilot CLI | Argument | Available (manual config) |
| `cursor` | Cursor IDE | Argument | Available (manual config) |

`4x init` configures claude, codex, gemini, and agy by default. Copilot and cursor require manual addition to `settings.json`.

## Plugin Files

Each runner has instruction files embedded in the `4x` binary. `4x init` deploys them to `.4x/plugins/` and adds import lines to root-level files:

| Runner | Plugin File | Root Import |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

Additionally, shared instruction files are deployed to `.4x/plugins/shared/` for all runners:

| File | Purpose |
|---|---|
| `shared/CREATOR.md` | Feature Creator flow — guides AI through `4x new` scaffold |

Use `4x sync` to re-deploy plugin files after updating the binary.

## Runner Execution Model

```
4x run F001 --runner claude
    │
	    ├── Generate prompt for current role
	    ├── Invoke runner subprocess with prompt
	    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
	    ├── Capture output to .4x/F001/logs/round-N-role.log
	    │     (parallel deep review: round-N-deep-reviewer-{i}.log + round-N-synthesizer.log)
    │     (deep review self-heal: round-N-deep-fix-{i}.log + round-N-deep-reverify-{i}.log)
    ├── Check output artifacts
    └── Transition state, repeat
```

### Exit Codes

| Code | Meaning | Action |
|---|---|---|
| 0 | Success | Proceed to next phase |
| 1 | Soft failure | Feature moves to `blocked` |
| 2 | Hard error | Loop halts, requires attention |
| negative (e.g. -1) | Signal kill (SIGKILL/SIGTERM) | Treated as hard error (same as exit 2) |
| timeout | No response within limit | Treated as soft failure |

When the run loop is interrupted (e.g. Ctrl+C), context cancellation is handled as a clean interrupt — the feature is not left in `needs-attention`. The in-progress phase is treated as incomplete, and the next `4x run` resumes from that phase.

### Placeholder Resolution

Runner `args` may contain placeholders that the CLI substitutes before invoking the subprocess:

| Placeholder | Replaced with |
|---|---|
| `{prompt}` | The role prompt text, inline as an argument |
| `{promptFile}` | Path to a temp file containing the prompt |
| `{model}` | The resolved model override for this role |

Placeholder resolution **fails loudly** rather than passing a literal placeholder through to the AI CLI:

- `{model}` present but no model override resolved → the runner errors with `model not resolved for runner <name>` instead of sending `--model {model}` (which the CLI would reject with an opaque error).
- `{promptFile}` but the temp file cannot be created or written (e.g. `/tmp` full) → the runner returns the wrapped underlying error (`runner <name>: create prompt temp file: ...`) and removes any partially-created temp file, instead of sending the literal string `{promptFile}`.

Any temp file created during resolution is always cleaned up, even when a later step fails.

### Stream JSON Mode

Runners with `output_format: "stream-json"` write two files: a readable `.log` for dashboard tailing and a raw `.stream.jsonl` file for debugging. Claude Code uses this mode by default.

### Non-PTY Process Group Handling

Non-PTY runners (stream-json mode, stdin mode, plain argument mode) use an independent process group (`Setpgid` on Unix). When the run context is cancelled, the process group is sent `SIGKILL` immediately — there is no SIGTERM grace period. On Windows, the default `exec.CommandContext` behavior applies.

### PTY Mode

Runners with `tty: true` (and not using `output_format: "stream-json"`) use a pseudo-terminal to capture full output including ANSI escape sequences. A stateful ANSI stripper cleans the log files. The PTY path uses `exec.Command` with a dedicated context watcher for graceful shutdown, while non-PTY runners use `exec.CommandContext` with process-group-level cancellation (see above).

The PTY child runs in its own session/process group. When the run context is cancelled (e.g. timeout or Ctrl+C), the whole process group is sent `SIGTERM`, escalating to `SIGKILL` after 5 seconds if it has not exited — so no orphaned child outlives the run.

### Stdin Mode

Runners with `stdin: true` (Codex) receive the prompt via standard input instead of command-line arguments.

## Using Different Models per Role

Configure in `.4x/settings.json`:

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "deep-reviewer": { "deep_model": "opus", "parallel_reviewers": 3 }
  }
}
```

The `model` and `deep_model` values are abstract tier names (e.g. `"opus"`, `"sonnet"`), not literal model IDs. Each runner has a `tiers` mapping in its config that translates tier names to runner-specific model IDs (e.g. claude maps `"opus"` to `"opus"`, codex maps `"opus"` to `"gpt-5.5"`). See [Configuration](configuration.md) for the full resolution chain.

The Deep Reviewer uses `deep_model`. When `parallel_reviewers > 1`, the deep review fans the 11 angles out across that many parallel sub-reviewers (each running `deep_model`) plus one synthesizer that merges their partial reports; `1` keeps the single-agent flow. Optional `angles_per_reviewer` sets a fixed number of angles per sub-reviewer; when omitted, angles are distributed evenly (`ceil(11/N)` per reviewer). See [Concepts → Parallel Deep Review](concepts.md).

You can also mix runners — use Claude for Design, Gemini for Code, etc. — by running each phase manually with different `--runner` flags and `4x transition` between phases.

## Writing a Plugin

Plugins follow a simple contract — read `.4x/` files, do AI work, write results back:

1. Read `.4x/features/{id}.yaml` to know the feature
2. Read `state.json` to know the current phase
3. Read phase-specific inputs (task-brief.md, scope, etc.)
4. Do the work (call your LLM, run tools)
5. Write phase-specific outputs (coder-report.md, review-report.md, etc.)
6. Exit with appropriate code (0 = success, 1 = soft fail, 2 = hard error)

No SDK required. No runtime dependency. Just files.
