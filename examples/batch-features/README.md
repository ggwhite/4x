# Batch Features -- 4x Example

Shows batch mode: schedule and run multiple features in dependency order using a DAG.

This example uses a TypeScript/Node.js task tracker as the subject project.
The focus is on the 4x batch workflow; no implementation code is included --
4x agents will generate it during the run.

## Dependency Graph

```
user-model          (no deps, runs first)
    |
    v
auth-middleware      (depends on user-model)
    |
    v
protected-routes     (depends on auth-middleware)
```

`batch plan` resolves this into a linear schedule. Features without
unmet dependencies start immediately; the rest wait until their deps reach
`done` (or `ready-for-review`).

## Project Structure

```
examples/batch-features/
├── .4x/
│   ├── settings.json
│   └── features/
│       ├── user-model.yaml
│       ├── auth-middleware.yaml
│       └── protected-routes.yaml
├── package.json
└── README.md
```

## Setup

```sh
cd examples/batch-features

# Initialize the .4x/ workspace (already present in this example)
4x init

# Create features -- already scaffolded, but this is how you would do it:
4x new "User model" --id user-model
4x new "Auth middleware" --id auth-middleware --depends user-model
4x new "Protected routes" --id protected-routes --depends auth-middleware
```

## Run Batch

```sh
# 1. Generate the DAG schedule
4x batch plan

# Prints something like:
#   Schedule (3 features):
#     1. user-model         (no deps)
#     2. auth-middleware     (after: user-model)
#     3. protected-routes   (after: auth-middleware)
#   Saved to .4x/batch-plan.json

# 2. Execute all features in order
4x batch run --runner claude

# Each feature goes through the full loop:
#   Designer -> Coder -> Reviewer -> Tester
# When user-model reaches done, auth-middleware starts automatically.
# When auth-middleware reaches done, protected-routes starts.

# 3. Monitor progress (in a second terminal)
4x live
# Open http://localhost:4567
```

## Check Status

```sh
# Per-feature status
4x status user-model
4x status auth-middleware
4x status protected-routes
```

## How Batch Scheduling Works

1. `4x batch plan` reads all features with `status: todo` (or `not-started`).
2. It builds a dependency graph from each feature's `depends` field.
3. Features are topologically sorted -- no feature starts before its
   dependencies are complete.
4. Independent features (no mutual deps) can run in parallel when
   the runner supports it.
5. The schedule is saved to `.4x/batch-plan.json`.

`4x batch run` reads the plan and executes features one by one (or in
parallel groups), advancing to the next feature only when dependencies
are satisfied.

## Stopping a Batch

```sh
# Gracefully stop after the current feature finishes
4x batch stop
```

## Review Results

After the batch completes:

```sh
cat .4x/user-model/final-report.md
cat .4x/auth-middleware/final-report.md
cat .4x/protected-routes/final-report.md
```

## Next Steps

- See `examples/todo-api/` for the single-feature workflow.
- Run `4x batch plan --help` for advanced scheduling options.
- Read `docs/guide/cli.md` for the full CLI reference.
