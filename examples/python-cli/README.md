# Python CLI — 4x Example

Demonstrates that 4x works with any language, not just Go.

This example uses a Python CLI tool (markdown-to-html converter) as the subject project.
The `.4x/` workspace is set up and ready; the AI agents generate all implementation code.

## Project Structure

```
examples/python-cli/
├── .4x/                              # created by 4x init
│   ├── settings.json
│   └── features/
│       └── markdown-converter.yaml
├── src/
│   └── md2html/
│       └── __init__.py               # placeholder — agents write the code
├── pyproject.toml
└── README.md
```

After a run completes, the agents will have created:
- `src/md2html/cli.py` — CLI entry point
- `src/md2html/converter.py` — Markdown-to-HTML conversion logic
- `tests/` — Unit tests
- `.4x/markdown-converter/` — Role artifacts (task-brief, reports, etc.)

## Setup

```sh
cd examples/python-cli

# Initialize the .4x/ workspace (already done in this example)
4x init

# The feature spec is already at .4x/features/markdown-converter.yaml
# Edit it to adjust scope or acceptance criteria before running.
```

## Run

```sh
# Run the full Designer -> Coder -> Reviewer -> Tester loop
4x run markdown-converter --runner claude
```

The runner works through the loop automatically:

1. **Designer** reads the feature yaml and writes `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml`.
2. **Coder** reads the Designer outputs, implements the CLI tool, writes `coder-report.md`.
3. **Reviewer** reads the code and Designer outputs, writes `review-report.md` with a verdict.
   - If the verdict is FAIL, the Coder amends and the Reviewer re-checks (up to `max_rounds`).
4. **Tester** runs the `verify_commands` from `test-strategy.yaml`, writes `verify.json`, `test-report.md`, and `final-report.md`.

## Watch

```sh
4x live
# Open http://localhost:4567
```

## Key Difference from todo-api

The `settings.json` uses Python tooling (`pytest`, `ruff`) instead of Go commands.
Everything else — the 4x protocol, role contracts, and workflow — is identical.
This is the point: 4x is language-agnostic.
