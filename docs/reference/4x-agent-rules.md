# 4x — AI Agent Quick Reference

> Copy this file to your AI agent's global rules directory so it knows how to operate 4x in every project without re-reading docs.
>
> - **Claude Code**: `~/.claude/rules/4x.md`
> - **Gemini CLI**: `~/.gemini/instructions/4x.md`
> - **Codex**: append to global `AGENTS.md`

---

When a project has a `.4x/` directory, it is managed by 4x — a multi-role AI development loop. The CLI manages state transitions; AI agents execute each role's task.

## CLI Commands

```bash
4x init                          # Initialize .4x/ directory
4x new "feature name"            # Create new feature (--desc, --subtask, --rule, --depends, --priority)
4x run F001 --runner claude      # Run full loop for a feature
4x run F001 --from coding        # Resume from a specific phase
4x status                        # List all features and their status
4x status F001                   # Show detailed status for one feature
4x check F001                    # Guardrail check
4x verify F001                   # Run verify commands from test-strategy.yaml
4x transition F001 --to coding   # Manual phase transition
4x done F001                     # Mark feature as done
4x clean                         # Remove runtime artifacts for completed features
4x batch plan.json               # Execute multiple features in sequence
4x live                          # Start dashboard server (port 4567)
4x sync                          # Re-deploy plugin files after binary update
```

## Directory Structure

```
.4x/
├── settings.json                # Project config (runners, roles, rules)
├── features/                    # Feature definitions ({id}.yaml)
├── plugins/                     # Runner instruction files
├── learnings.json               # Retro learnings store
└── run/                         # Runtime artifacts
    └── {feature-id}/
        ├── state.json           # Current phase, role, round
        ├── task-brief.md        # Designer output
        ├── acceptance-criteria.md
        ├── test-strategy.yaml
        ├── rounds/round-{N}/
        │   ├── coder-report.md
        │   ├── review-report.md
        │   └── verify.json
        └── logs/
```

## Role Contracts

| Role | Reads | Writes | Cannot |
|---|---|---|---|
| Designer | feature YAML, codebase | task-brief.md, acceptance-criteria.md, test-strategy.yaml | modify source code |
| Design Reviewer | task-brief, AC, test-strategy | design-review-report.md (PASS/FAIL) | modify source code |
| Coder | task-brief, review/test reports | source code, coder-report.md | modify AC or tests |
| Reviewer | diff, task-brief, coder-report | review-report.md (PASS/FAIL) | modify source code |
| Tester | AC, test-strategy | verify.json, test-report.md, final-report.md | modify source code or test assertions |

## State Machine

```
init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
```

FAIL triggers amending → back to coding for another round.

## Supported Runners

claude, codex, gemini, agy, opencode, copilot, cursor. Configured in `settings.json` under the `runners` key.

## When Operating as a Role

1. Read `.4x/settings.json` for project config
2. Read `.4x/features/{id}.yaml` for feature spec
3. Read `.4x/run/{id}/state.json` to know current role and round
4. Read artifacts from prior roles in `.4x/run/{id}/`
5. After your work, run `4x check {id}` for guardrail validation
