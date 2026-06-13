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
| `cursor` | Cursor IDE | Rules file | Available (manual config) |

`4x init` configures claude, codex, gemini, and agy by default. Copilot and cursor require manual addition to `settings.json`.

## Plugin Files

Each runner has instruction files embedded in the `4x` binary. `4x init` deploys them to `.4x/plugins/` and adds import lines to root-level files:

| Runner | Plugin File | Root Import |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` + `workflow.js` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

Additionally, shared instruction files are deployed to `.4x/plugins/shared/` for all runners:

| File | Purpose |
|---|---|
| `shared/CREATOR.md` | Feature Creator flow — guides AI through `4x new` scaffold |

Use `4x upgrade` to re-deploy plugin files after updating the binary.

## Runner Execution Model

```
4x run F001 --runner claude
    │
	    ├── Generate prompt for current role
	    ├── Invoke runner subprocess with prompt
	    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
	    ├── Capture output to .4x/F001/logs/round-N-role.log
    ├── Check output artifacts
    └── Transition state, repeat
```

### Exit Codes

| Code | Meaning | Action |
|---|---|---|
| 0 | Success | Proceed to next phase |
| 1 | Soft failure | Feature moves to `blocked` |
| 2 | Hard error | Loop halts, requires attention |
| timeout | No response within limit | Treated as soft failure |

### Stream JSON Mode

Runners with `output_format: "stream-json"` write two files: a readable `.log` for dashboard tailing and a raw `.stream.jsonl` file for debugging. Claude Code uses this mode by default.

### PTY Mode

Runners with `tty: true` use a pseudo-terminal to capture full output including ANSI escape sequences. A stateful ANSI stripper cleans the log files. This path is skipped when `output_format` is `"stream-json"`.

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
    "tester": { "model": "sonnet" }
  }
}
```

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
