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

2. **Read context from the filesystem only.** The plugin reads `.4x/config.yaml`, `.4x/features/{feature-id}.yaml`, and any existing files in `.4x/{feature-id}/`. It must not rely on environment variables beyond standard ones (`HOME`, `PATH`, `4X_*` prefixed vars).

3. **Write outputs to `.4x/{feature-id}/`.** All output files must be written atomically (write to temp, rename). Partial writes that crash mid-run must not leave corrupt state.

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
