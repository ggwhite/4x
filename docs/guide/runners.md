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
| `opencode` | OpenCode CLI | Argument | Available |
| `copilot` | GitHub Copilot CLI | Argument | Available (manual config) |
| `cursor` | Cursor IDE | Argument | Available (manual config) |

`4x init` configures claude, codex, gemini, agy, and opencode by default. Copilot and cursor require manual addition to `settings.json`.

## Plugin Files

Each runner has instruction files embedded in the `4x` binary. `4x init` deploys them to `.4x/plugins/` and adds import lines to root-level files:

| Runner | Plugin File | Root Import |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| opencode | `AGENTS.md` | AGENTS.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

Additionally, shared instruction files are deployed to `.4x/plugins/shared/` for all runners:

| File | Purpose |
|---|---|
| `shared/CREATOR.md` | Feature Creator flow — guides AI through `4x new` scaffold, including scale red lines (repos > 4, subtasks > 6, or a description spanning unrelated concerns must be split into multiple features linked with `depends` at creation time) |

Use `4x sync` to re-deploy plugin files after updating the binary.

## Runner Execution Model

```
4x run F001 --runner claude
    │
	    ├── Generate prompt for current role
	    ├── Clear known stale output artifacts from the prior round
	    │     (so a runner that returns without writing fails loudly, not on stale data)
	    ├── Invoke runner subprocess with prompt
	    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
	    ├── Capture output to .4x/run/F001/logs/round-N-role.log
	    │     (parallel deep review: round-N-deep-reviewer-{i}.log + round-N-synthesizer.log)
    │     (deep review self-heal: round-N-deep-fix-{i}.log + round-N-deep-reverify-{i}.log)
    │     (reviewing conditional-pass convergence: round-N-review-fix-{i}.log + round-N-reviewer-{i+1}.log)
    ├── Check output artifacts
    └── Transition state, repeat
```

Before the runner subprocess is spawned, 4x filters the inherited environment variables so sensitive credentials (`*_TOKEN`/`*_KEY`/`*_SECRET`, `AWS_*`, `GITHUB_TOKEN`, `DATABASE_URL`, …, plus a case-insensitive substring match on `PASSWORD`/`SECRET`/`TOKEN`/`CREDENTIAL`/`PASSWD`/`PWD` that also catches names like `PGPASSWORD`/`MYSQL_PWD`) are not exposed to the autonomous AI subprocess. `PATH`/`HOME`, 4x's own injected variables, `GITHUB_TOKEN`/`GH_TOKEN` (allowed for every runner, since any role may shell out to `git`/`gh`), and each runner's built-in credential allowlist (e.g. `ANTHROPIC_API_KEY` plus Bedrock/Vertex/Azure auth for `claude`/`agy`, `OPENAI_API_KEY` for `codex`, `GITHUB_TOKEN` for `copilot`, extra provider keys for `opencode`, `CURSOR_*` for `cursor`) are always preserved so the runner starts normally. Tune the filter with the `runner_env` and per-runner `env_allowlist` settings — see [Configuration → Runner environment filtering](configuration.md#runner-config). Runner log files are created with `0600` permissions (enforced on every open, including pre-existing files from before this hardening) so other users on the same machine cannot read them.

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

Runners with `output_format: "stream-json"` write two files: a readable `.log` for dashboard tailing and a raw `.stream.jsonl` file for debugging. Both are opened in append mode, so retrying the same round continues the existing log instead of overwriting the earlier attempt. Claude Code uses this mode by default. Tool-use summaries in the `.log` (e.g. Bash commands) are truncated at a bounded length, cut at a UTF-8 rune boundary so multi-byte characters are never split mid-character.

### Non-PTY Process Group Handling

Non-PTY runners (stream-json mode, stdin mode, plain argument mode) use an independent process group (`Setpgid` on Unix). When the run context is cancelled, the process group is sent `SIGKILL` immediately — there is no SIGTERM grace period. On Windows, the default `exec.CommandContext` behavior applies.

### PTY Mode

Runners with `tty: true` (and not using `output_format: "stream-json"`) use a pseudo-terminal to capture full output including ANSI escape sequences. A stateful ANSI stripper cleans the log files. The PTY path uses `exec.Command` with a dedicated context watcher for graceful shutdown, while non-PTY runners use `exec.CommandContext` with process-group-level cancellation (see above).

The PTY child runs in its own session/process group. When the run context is cancelled (e.g. timeout or Ctrl+C), the whole process group is sent `SIGTERM`, escalating to `SIGKILL` after 5 seconds if it has not exited — so no orphaned child outlives the run.

### Stdin Mode

Runners with `stdin: true` (Codex) receive the prompt via standard input instead of command-line arguments.

### PreToolUse Guard (claude only)

For **every** claude role, the `claude` runner injects a Claude Code PreToolUse hook via a temporary `--settings` file that calls `4x guard-tool`, with two matchers — `Bash` and `Edit|Write|MultiEdit` — both pointing at `"$FOURX_BIN" guard-tool`. The hook covers two intercepts, both claude-specific, never affecting other runners and never failing the run (all error paths allow):

- **Reviewer git-exploration guard** — for the `reviewer` / `deep-reviewer` roles the runner also injects `FOURX_ROLE` / `FOURX_REVIEW_PACKAGE`. When the round's `review-package.md` exists, the reviewer's own `git diff` / `git log` / `git show` calls are softly denied with a message pointing to that pre-computed package (which also embeds the full contents of each changed file within a size budget). If the package is absent, the reviewer falls back to running git itself.
- **Role-scope write-gate** — on `Edit` / `Write` / `MultiEdit`, `guard-tool` runs the same role×path check as `4x check --path` against the current role (read from `state.json`): only `coder` / `fixer` may write source (outside `.4x/`), and a multi-repo feature's source writes must stay within the feature's `repos`. Out-of-scope or role-forbidden source writes are denied at edit time instead of waiting for the post-hoc `4x check`. Writes under `.4x/` are always allowed, and any read error fails open. See [plugin-contract](../reference/plugin-contract.md) for the full matrix. Non-reviewer roles get the same injected hook, but the Bash branch is a no-op for them because `FOURX_ROLE` is unset.

## Using Different Models per Role

Configure in `.4x/settings.json`:

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "design-reviewer": { "model": "sonnet" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "fixer": { "model": "sonnet" },
    "deep-reviewer": { "parallel_reviewers": 3 }
  }
}
```

The `model` and `deep_model` values are abstract tier names (e.g. `"opus"`, `"sonnet"`), not literal model IDs. Each runner has a `tiers` mapping in its config that translates tier names to runner-specific model IDs (e.g. claude maps `"opus"` to `"opus"`, codex maps `"opus"` to `"gpt-5.5"`). See [Configuration](configuration.md) for the full resolution chain.

> **Note:** `deep_model` is configured on the **reviewer** role, not the deep-reviewer role. If `roles.reviewer.deep_model` is not set, the `deep-reviewing` phase is **skipped entirely** — the run transitions directly from `testing` to `accepting`. This is by design: deep review is opt-in.

The Deep Reviewer uses `roles.reviewer.deep_model`. When `parallel_reviewers > 1`, the deep review fans the 11 angles out across that many parallel sub-reviewers (each running `deep_model`) plus one synthesizer that merges their partial reports; `1` keeps the single-agent flow. The synthesizer only merges text — it does not re-read source — so it resolves its own model via `roles.synthesizer.model` (defaulting to the `sonnet` tier) instead of the more expensive `deep_model`. Optional `angles_per_reviewer` sets a fixed number of angles per sub-reviewer; when omitted, angles are distributed evenly (`ceil(11/N)` per reviewer). See [Concepts → Parallel Deep Review](concepts.md).

You can also mix runners — use Claude for Design, Gemini for Code, etc. — by running each phase manually with different `--runner` flags and `4x transition` between phases.

**Cross-model adversarial review.** A reviewer that shares the coder's model family tends to share its blind spots — it rubber-stamps the same class of mistakes it would have made itself. To break that symmetry, pin the review roles to a **different** runner (hence a different model family) than the coder. Set `roles.{role}.runner` to route a role's entire pipeline through another runner:

```json
{
  "roles": {
    "coder": { "runner": "claude" },
    "reviewer": { "runner": "codex" },
    "deep-reviewer": { "runner": "codex" }
  }
}
```

With `roles.deep-reviewer.runner: codex`, the Deep Reviewer runs on `codex` while the Coder stays on `claude`, so the adversarial pass reasons from a genuinely independent model. The runner name must exist in `runners`, otherwise the run fails fast (and `4x doctor` flags it up front). Precedence for the effective runner is (high→low): a feature's `phase_overrides.{phase}.runner` > the profile's per-phase runner > `roles.{role}.runner` > `default_runner`. Leaving `roles.{role}.runner` unset keeps the existing behavior unchanged.

## Writing a Plugin

Plugins follow a simple contract — read `.4x/` files, do AI work, write results back:

1. Read `.4x/features/{id}.yaml` to know the feature
2. Read `state.json` to know the current phase
3. Read phase-specific inputs (task-brief.md, scope, etc.)
4. Do the work (call your LLM, run tools)
5. Write phase-specific outputs (design-review-report.md, coder-report.md, review-report.md, etc.)
6. Exit with appropriate code (0 = success, 1 = soft fail, 2 = hard error)

No SDK required. No runtime dependency. Just files.

### Escalation & Scope Gaps

Plugin instruction files also define two distinct reporting channels for roles:

- **Escalation** — if a role hits a blocker inside its current task (impossible spec, contradictory criteria), it writes `.4x/run/{feature-id}/rounds/round-{n}/escalation.json` (`{"needed": true, "reason": "...", "detail": "..."}`). Valid reasons: `spec-mismatch`, `criteria-wrong`, `blocker`, `scope-change`.
- **Scope Gaps** — if a role notices an issue clearly outside the feature's scope that does *not* block its current task (e.g. something that deserves its own feature later), it appends a line to `docs/reference/discovered-feature-gaps.md` instead of expanding scope or self-triggering `4x new`. This is a non-blocking, human-reviewed channel — it does not affect state, exit code, or guard checks.
