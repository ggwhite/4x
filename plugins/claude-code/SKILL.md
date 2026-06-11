---
name: 4x
description: >
  Multi-role AI development loop — Designer, Coder, Reviewer, Tester.
  Trigger: user says "4x run <feature>", "4x <feature>", "run <feature>".
  Batch: "4x batch", "4x auto".
  Status: "4x status".
  This is a rigid process skill — follow steps exactly.
---

# 4x — Designer-Coder-Reviewer-Tester Loop

Four specialized AI roles collaborate through file-based protocol to implement features.
Each role operates in its own context window; communication happens via `.4x/` directory artifacts.

## Prerequisites

- `4x` CLI must be installed: `go install github.com/ggwhite/4x/cmd/4x@latest`
- Project must be initialized: `4x init`
- Feature must exist: `4x new "feature name"` or manually created in `.4x/features/`

## Flow

### Step 1: Parse Input

Extract from user message:
- `featureId`: feature identifier
- `maxRounds`: max loop iterations (default 5)
- `only`: run single phase (design/code/review/test)
- `resume`: resume from specific phase
- `batch`: batch mode flag

### Step 2: Load Feature

```bash
4x status <featureId>
```

Confirm feature exists and status is not `done`.

### Step 3: Load Model Config

Read `.4x/config.yaml` and extract the `roles` section. Map to workflow model names:

```yaml
# .4x/config.yaml example
roles:
  designer:
    model: opus           # task-brief, acceptance-criteria, amend
  coder:
    model: sonnet         # implementation, fix review/test issues
  reviewer:
    model: sonnet         # checklist review
    deep_model: opus      # adversarial deep review
  tester:
    model: sonnet         # run tests, report evidence
  acceptor:
    model: opus           # final-report, commit-plan
```

Build the `models` object from config:

| Config key | models field | Default |
|---|---|---|
| `roles.designer.model` | `designer` | `opus` |
| `roles.coder.model` | `coder` | `sonnet` |
| `roles.reviewer.model` | `reviewer` | `sonnet` |
| `roles.reviewer.deep_model` | `deep_reviewer` | `opus` |
| `roles.tester.model` | `tester` | `sonnet` |
| `roles.acceptor.model` | `acceptor` | `opus` |

If no `roles` section exists, all defaults apply.

### Step 4: Initialize State

```bash
4x transition <featureId> --to init
4x event <featureId> --type run-start --role designer
```

### Step 5: Launch Workflow

Use the Workflow tool to orchestrate the four roles. Pass models from Step 3 and project profile.

```
Workflow({
  scriptPath: "<path-to-this-plugin>/workflow.js",
  args: {
    featureId: "<featureId>",
    maxRounds: 5,
    dotDir: ".4x",
    only: null,
    resume: null,
    models: {
      designer: "opus",
      coder: "sonnet",
      reviewer: "sonnet",
      deep_reviewer: "opus",
      tester: "sonnet",
      acceptor: "opus"
    },
    project: {
      setup: ["docker compose up -d"],
      build: ["make build"],
      test: ["make test"],
      lint: ["make lint"],
      docs: ["docs/architecture.md"],
      rules: ["All APIs must have integration tests"]
    }
  }
})
```

### Step 6: Report Results

Read `.4x/<featureId>/final-report.md` and `.4x/<featureId>/commit-plan.md`.
Display to user:
- Final status (ready-for-review / needs-attention / blocked)
- Summary of what was done
- Commit plan for human confirmation

## Roles

### Designer (Opus)
- Reads: feature YAML, existing codebase, external references
- Writes: task-brief.md, acceptance-criteria.md, test-strategy.yaml
- Cannot: modify source code

### Coder (Sonnet)
- Reads: task-brief.md, test-report from previous round
- Writes: source code changes, coder-report.md
- Must: run verify commands after changes
- Cannot: modify acceptance criteria or test scripts

### Reviewer (Sonnet checklist + Opus adversarial)
- Reads: diff, task-brief, coder-report, project rules
- Writes: review-report.md
- Phase 1: Checklist review against project hard rules
- Phase 2: Adversarial review for business logic, security, concurrency, performance
- Cannot: modify source code

### Tester (Sonnet)
- Reads: acceptance-criteria, coder-report, test-strategy
- Writes: test scripts, test-report.md, screenshots
- Must: write test scripts first, then run, then report
- Cannot: modify source code

## Guardrails

After each round, the plugin calls:
```bash
4x check <featureId>
```

This verifies:
- Changed files are within allowed scope
- No baseline dirty files were mixed in
- Required protocol files exist
- Verify commands were executed

## Escalation

Coder or Tester can escalate to Designer when:
- `spec-mismatch`: DB/API doesn't match spec
- `criteria-wrong`: acceptance criteria are incorrect
- `blocker`: missing dependency or infra issue
- `scope-change`: need to modify repos outside scope
