# 4x Design — CLI Commands Reference

> Extracted from design.md §10

---

### `4x init`

Initialize a `.4x/` workspace in the current directory.

```
4x init [--name <name>] [--runner <runner>]
```

Creates `.4x/settings.json` with defaults. Does not overwrite if already exists.

---

### `4x new <title>`

Create a new feature spec.

```
4x new "REST API for todo items" [--repo <repo-id>] [--priority high|medium|low]
```

Creates `.4x/features/{slugified-title}.yaml`. Opens in `$EDITOR` if `--edit` is passed.

---

### `4x status [<feature-id>]`

Show current state of one or all features.

```
4x status
4x status rest-api-for-todo-items
4x status --json
```

Output includes: phase, role, round/maxRounds, active, last event timestamp, stopReason (if any).

---

### `4x check <feature-id>`

Run all guardrails without transitioning state.

```
4x check rest-api-for-todo-items
4x check rest-api-for-todo-items --scope-only
4x check rest-api-for-todo-items --files-only
```

Exits 0 if all checks pass, 1 if any fail. Prints a summary of each check result.

---

### `4x transition <feature-id> <to-state>`

Force a state transition (human override).

```
4x transition rest-api-for-todo-items coding
4x transition rest-api-for-todo-items designing --reason "Revised spec after user feedback"
```

Validates the transition is legal per the state machine. Runs `4x check` unless `--skip-check` is passed (not recommended).

---

### `4x event <type>`

Append an event to `events.jsonl`.

```
4x event step --feature-id rest-api-for-todo-items --message "Starting DB migration"
4x event heartbeat --feature-id rest-api-for-todo-items
4x event escalation --feature-id rest-api-for-todo-items --reason blocker --detail "PostgreSQL unavailable"
```

Used by plugins and scripts to record progress without modifying state.

---

### `4x prompt <feature-id> --role <role>`

Print the prompt that would be sent to the LLM for this role, without executing it. Useful for debugging and prompt tuning.

```
4x prompt rest-api-for-todo-items --role Designer
4x prompt rest-api-for-todo-items --role Coder --format markdown
```

---

### `4x batch plan`

Analyze all features and generate a `batch-plan.json`.

```
4x batch plan
4x batch plan --dry-run    # print schedule, don't write file
4x batch plan --max-chain 3
```

---

### `4x batch next`

Start the next eligible feature according to `batch-plan.json`.

```
4x batch next
4x batch next --runner claude --slot 1
4x batch next --all    # start all currently eligible features up to parallel_slots limit
```

---

### `4x live`

Start the dashboard web server.

```
4x live
4x live --port 4567
4x live --feature rest-api-for-todo-items    # focus on one feature
```

Opens `http://localhost:4567` in the default browser unless `--no-open` is passed.
Streams `events.jsonl` updates via SSE (Server-Sent Events).
