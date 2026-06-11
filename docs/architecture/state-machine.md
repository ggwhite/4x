# 4x Design — State Machine

> Extracted from design.md §4

---

## States

| State | Description |
|---|---|
| `init` | Feature created, not yet started. |
| `designing` | Designer role is active. |
| `coding` | Coder role is active. |
| `reviewing` | Reviewer role is active. |
| `testing` | Tester role is active. |
| `amending` | Coder is amending based on Reviewer or Tester feedback. |
| `accepting` | Final human or automated acceptance check. |
| `done` | Feature complete. |
| `blocked` | Cannot proceed without external input. |
| `needs-attention` | Automatic escalation triggered; human review required. |

## Valid Transitions

| From | To | Trigger |
|---|---|---|
| `init` | `designing` | `4x run` or `4x transition` |
| `designing` | `coding` | Designer outputs verified present |
| `coding` | `reviewing` | Coder outputs verified present |
| `reviewing` | `testing` | Reviewer verdict: pass |
| `reviewing` | `amending` | Reviewer verdict: fail |
| `amending` | `reviewing` | Coder amendment complete |
| `testing` | `accepting` | Tester verdict: pass |
| `testing` | `amending` | Tester verdict: fail |
| `accepting` | `done` | Acceptance check passes |
| `any` | `blocked` | Escalation with `blocker` reason |
| `any` | `needs-attention` | Escalation with any other reason |
| `blocked` | `designing` | Human resolves blocker, restarts |
| `blocked` | `coding` | Human resolves blocker, restarts at coding |
| `needs-attention` | `designing` | Human intervenes, restarts |
| `needs-attention` | `coding` | Human intervenes, restarts at coding |

The CLI rejects any transition not in this table.

## Round Counter

The `round` counter increments each time the feature returns to `amending`. It is checked against `maxRounds` before any new Coder run. If `round >= maxRounds`, the CLI rejects the transition and moves to `needs-attention`.
