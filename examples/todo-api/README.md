# Todo API — 4x Example

Shows the basic 4x workflow: `init` → `new` → `run` → `review`.

This example uses a minimal Go + PostgreSQL todo API as the subject project.
The focus is on the 4x workflow itself; the API code is intentionally simple.

## Project Structure

```
examples/todo-api/
├── .4x/                          # created by 4x init
│   ├── settings.json
│   ├── features/
│   │   └── rest-api-for-todo-items.yaml
│   └── rest-api-for-todo-items/  # created when run starts
│       ├── state.json
│       ├── events.jsonl
│       ├── task-brief.md
│       ├── acceptance-criteria.md
│       ├── test-strategy.yaml
│       ├── coder-report.md
│       ├── review-report.md
│       ├── test-report.md
│       ├── final-report.md
│       └── commit-plan.md
├── backend/                      # the Go project 4x will work on
│   ├── go.mod
│   ├── main.go
│   └── internal/
└── README.md
```

## Setup

```sh
cd examples/todo-api

# Initialize the .4x/ workspace
4x init

# Create a new feature spec (opens .4x/features/rest-api-for-todo-items.yaml)
4x new "REST API for todo items" --repo backend

# Edit the spec to add description, acceptance criteria, and scope
# (a minimal version is already present after 4x new)
```

The generated `.4x/features/rest-api-for-todo-items.yaml` will look like:

```yaml
id: rest-api-for-todo-items
title: "REST API for todo items"
priority: medium
status: todo
repos:
  - backend
description: |
  Implement a CRUD REST API for todo items.
  Endpoints: GET/POST /v1/todos, GET/PUT/DELETE /v1/todos/{id}
acceptance:
  - All five endpoints return correct HTTP status codes.
  - Invalid input returns 400 with a structured error body.
  - Non-existent resource returns 404.
  - Integration tests pass against a real PostgreSQL instance.
max_rounds: 5
```

Edit this file to add as much or as little detail as you want before running.

## Run

```sh
# Run the full Designer → Coder → Reviewer → Tester loop
4x run rest-api-for-todo-items --runner claude
```

The runner works through the loop automatically:

1. **Designer** reads the feature yaml and writes `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml`.
2. **Coder** reads the Designer outputs, implements the API, writes `coder-report.md`.
3. **Reviewer** reads the code and Designer outputs, writes `review-report.md` with a verdict.
   - If the verdict is FAIL, the Coder amends and the Reviewer re-checks (up to `max_rounds`).
4. **Tester** runs the `verify_commands` from `test-strategy.yaml`, writes `verify.json`, `test-report.md`, `final-report.md`, and `commit-plan.md`.

## Watch

Open a second terminal and start the dashboard while the run is in progress:

```sh
4x live
# Open http://localhost:4567
```

The dashboard shows:
- Current phase and role
- Round counter
- Live event stream (tail of `events.jsonl`)
- Pass/fail status per acceptance criterion as the Tester reports results

## Check Status

```sh
# Summary of current state
4x status rest-api-for-todo-items

# Run guardrails manually (scope check, required files, verify evidence)
4x check rest-api-for-todo-items
```

## Review the Results

After the run completes:

```sh
# Overall summary
cat .4x/rest-api-for-todo-items/final-report.md

# Ordered commit plan
cat .4x/rest-api-for-todo-items/commit-plan.md

# Full test evidence
cat .4x/rest-api-for-todo-items/verify.json
```

Follow the `commit-plan.md` to commit changes in the recommended order:

```sh
# Example — adapt to the actual files listed in commit-plan.md
git add backend/db/migrations/
git commit -m "feat(db): add todos table migration"

git add backend/internal/todo/
git commit -m "feat(todo): implement PostgreSQL-backed todo store"

git add backend/api/
git commit -m "feat(api): add CRUD endpoints for todo items"
```

## Restarting After a Failure

If a run stops due to escalation or error:

```sh
# See why it stopped
4x status rest-api-for-todo-items

# Read the relevant report for details
cat .4x/rest-api-for-todo-items/coder-report.md   # if stopped at coding
cat .4x/rest-api-for-todo-items/review-report.md  # if stopped at reviewing

# Resolve the issue, then resume from the appropriate state
4x transition rest-api-for-todo-items coding --reason "Fixed spec ambiguity"
4x run rest-api-for-todo-items --runner claude
```

## Running Without an LLM (Dry Run)

To see what prompts would be sent without calling any model:

```sh
4x prompt rest-api-for-todo-items --role Designer
4x prompt rest-api-for-todo-items --role Coder
```

## Next Steps

- Read `docs/design.md` for the full protocol specification.
- See `examples/` for more complex examples (multi-repo, batch mode, custom plugins).
- Run `4x batch plan` to schedule multiple features automatically.
