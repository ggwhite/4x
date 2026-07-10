# 4x Design — Plugin Contract

> Extracted from design.md §9

---

A 4x plugin is an executable (or library) that the CLI invokes to run a role. Plugins are responsible for all LLM interaction.

A conforming plugin must:

1. **Accept a standard invocation:**
   ```
   4x-plugin-{name} run --feature-id <id> --role <role> --workspace <path>
   ```
   The `--workspace` flag points to the root containing `.4x/`.

2. **Read context from the filesystem only.** The plugin reads `.4x/settings.json`, `.4x/features/{feature-id}.yaml`, and any existing files in `.4x/run/{feature-id}/`. It must not rely on environment variables beyond standard ones (`HOME`, `PATH`, `4X_*` prefixed vars). When spawning a runner subprocess, 4x filters the inherited environment against a built-in denylist (sensitive `*_TOKEN`/`*_KEY`/`*_SECRET`, `AWS_*`, `GITHUB_TOKEN`, etc.) but always preserves `HOME`/`PATH`, each runner's built-in credential allowlist (e.g. `ANTHROPIC_API_KEY` for `claude`, `GITHUB_TOKEN` for `copilot`), and anything the user adds via `runner_env`/`env_allowlist` in settings. Plugins still must not depend on environment variables beyond those.

3. **Write outputs to `.4x/run/{feature-id}/`.** All output files must be written atomically (write to temp, rename). Partial writes that crash mid-run must not leave corrupt state.

4. **Emit heartbeat events** at least once every 60 seconds during long operations, by calling:
   ```
   4x event heartbeat --feature-id <id>
   ```
   or by writing directly to `events.jsonl` (with correct schema).

5. **Exit with standard codes:**
   - `0` — role completed successfully; required output files are present.
   - `1` — soft failure; role could not complete but did not corrupt state. The CLI will move the feature to `needs-attention`.
   - `2` — hard error (unexpected crash, missing required tool, etc.). The CLI will halt the batch and alert.

6. **Support `--dry-run`:** When invoked with `--dry-run`, the plugin prints the prompt it would send and exits 0 without calling any LLM or writing any files.

---

## PreToolUse write-gate (Claude Code)

The judgement logic lives entirely in the `4x` CLI (LLM-agnostic); the Claude Code plugin only wires its `PreToolUse` hook to the hidden `4x guard-tool` command. Runners without a hook mechanism (gemini/codex/copilot…) are unaffected and continue to rely on the post-hoc `4x check`.

Two enforcement surfaces share the same core role×path judgement:

- **`4x check --path <file>`** — a fast, read-only single-file write-permission check for the current role. It completely bypasses the full `guard.Check` (no required-files / baseline / docs gate) and never writes `state.json` or `events.jsonl`. Exit `0` allows, non-zero rejects (reason on stderr). It **fails open** for every read/detection error — not a 4x project, no resolvable feature, missing `state.json`, or a git detection failure (not a git repo, `git` not on `PATH`, `git rev-parse --git-common-dir` subprocess failure) — exiting `0`. The one narrowed case: when the target sits **outside the current worktree root** but git can prove it lands inside the corresponding **main workspace root** (the parent of the linked worktree's common git dir), the check **denies** (exit 1, stderr mentions `main workspace` / `outside current worktree`) so a role agent cannot write the main workspace's same-named repo. A target that is merely outside the workspace and cannot be proven to live in the main workspace root still exits `0`. The authoritative fail-closed enforcement remains the post-hoc `4x check <feature-id>`.
- **`4x guard-tool`** — the `PreToolUse` hook target. It reads the Claude Code hook JSON from stdin and dispatches on `tool_name`: `Bash` keeps the existing reviewer git-exploration intercept, while `Edit` / `Write` / `MultiEdit` run the same role×path write-gate on `tool_input.file_path` and emit a `permissionDecision: "deny"` decision when the write is out of scope or not permitted for the role. It shares the `check --path` main-workspace-root judgement: a `file_path` outside the current worktree root but provably inside the main workspace root emits `deny` (command still exits 0), while all read/parse/git-detection error paths allow (exit 0).

The role×path matrix: only `coder` and `fixer` may write **source** (anything outside `.4x/`); every other role writing source is denied. For a multi-repo feature, a source file's top-level directory must be in the feature's `repos` (or a hub repo), else it is a scope violation. Writes under `.4x/` are always allowed — fine-grained artifact ownership is left to the post-hoc `4x check`.

During `4x run`, the claude runner injects this hook automatically for every claude role, so no manual configuration is needed. To enable the same gate when running `claude` manually (outside `4x run`), add a static `.claude/settings.json` pointing at the `4x` binary (exposed to the hook as `$FOURX_BIN`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "\"$FOURX_BIN\" guard-tool" }]
      },
      {
        "matcher": "Edit|Write|MultiEdit",
        "hooks": [{ "type": "command", "command": "\"$FOURX_BIN\" guard-tool" }]
      }
    ]
  }
}
```
