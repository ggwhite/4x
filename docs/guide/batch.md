# Batch Mode

Run multiple features in dependency-aware order.

## Workflow

```bash
# 1. Generate execution plan
4x batch plan

# 2. Check what's next
4x batch next

# 3. Run all eligible features
4x batch run --runner claude

# 4. Gracefully stop (finishes current feature)
4x batch stop
```

## Planning

`4x batch plan` analyzes all non-done features and generates `.4x/batch-plan.json`:

1. **Dependency DAG** — builds a directed graph from feature `depends` fields
2. **Cycle detection** — errors if circular dependencies exist
3. **Union-Find clustering** — groups features that share non-hub repositories (hub repos defined via `hub_repos` config or `workspace.repos[*].hub: true` are excluded from clustering)
4. **Topological sort** — orders features within each cluster. When several features become eligible at the same time (no remaining unmet dependencies), they are ordered by `priority` (lower number = higher priority; features without a priority sort last). Ties on priority fall back to feature ID for a stable, deterministic order.
5. **Chain scheduling** — splits long dependency chains (max length configurable with `--max-chain`)

```bash
# Preview the plan
4x batch plan --dry-run

# Limit chain length
4x batch plan --max-chain 3
```

Example output:

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## Running

`4x batch run` executes features sequentially in dependency order:

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- Uses commit strategy `"never"` (you commit manually after review)
- Checks for `.4x/batch-stop` file between features
- Skips features whose dependencies are not done
- Reports progress after each feature

## Stopping

```bash
4x batch stop
```

Creates a `.4x/batch-stop` signal file. The batch finishes the current feature, then exits gracefully.

## Merge Conflicts

When auto-merge hits a conflict, the batch pauses and writes `.4x/batch-conflict.json` recording the feature, the conflicting repo (multi-repo mode), and the affected files. The worktree is preserved so you can resolve the conflict. The signal file lets the [dashboard](dashboard.md) surface the conflict and offer a **Continue Batch** action — under the hood it clears the signal file and restarts `4x batch run`. From the CLI, resolve the files, run `4x merge <id>`, then re-run `4x batch run` to continue. The conflict file is cleared automatically at the start of every batch run.

## Checking Progress

```bash
# See which feature is next (prints feature ID)
4x batch next

# JSON output with subtask frontier info
4x batch next --json

# Overview of all features
4x status
```

With `--json`, the output includes subtask dependency frontier — the set of subtasks whose dependencies are all completed and are ready to work on:

```json
{
  "featureId": "F044-subtask-frontier",
  "slot": 0,
  "subtaskFrontier": ["parse-depends", "build-dag"]
}
```

Returns `null` when no eligible features remain.
