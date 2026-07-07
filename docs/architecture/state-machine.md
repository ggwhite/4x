# 4x Design — State Machine

> Extracted from design.md §4

---

## States

| State | Description |
|---|---|
| `init` | Feature created, not yet started. |
| `designing` | Designer role is active. |
| `design-reviewing` | Design Reviewer role is active when enabled by the active pipeline profile. |
| `coding` | Coder role is active. |
| `reviewing` | Reviewer role is active. |
| `testing` | Tester role is active. |
| `amending` | Coder is amending based on Reviewer or Tester feedback. |
| `fixing` | Fixer role is active, fixing WARNING/INFO issues from deep review. |
| `accepting` | Final human or automated acceptance check. |
| `done` | Feature complete. |
| `blocked` | Cannot proceed without external input. |
| `needs-attention` | Automatic escalation triggered; human review required. |

## Valid Transitions

| From | To | Trigger |
|---|---|---|
| `init` | `designing` | `4x run` or `4x transition` |
| `designing` | `design-reviewing` | Designer outputs verified present |
| `design-reviewing` | `coding` | Design review verdict: pass |
| `design-reviewing` | `designing` | Design review verdict: fail |
| `coding` | `reviewing` | Coder outputs verified present |
| `reviewing` | `testing` | Reviewer verdict: pass |
| `reviewing` | `amending` | Reviewer verdict: fail |
| `amending` | `reviewing` | Coder amendment complete |
| `testing` | `deep-reviewing` | Tester verdict: pass |
| `testing` | `amending` | Tester verdict: fail |
| `deep-reviewing` | `fixing` | Deep review verdict: pass |
| `deep-reviewing` | `accepting` | Deep review verdict: pass, and either fixing not in profile or the report is a clean pass (no critical/warning issues) |
| `deep-reviewing` | `amending` | Deep review self-heal exhausted |
| `fixing` | `accepting` | Fixer outputs verified present |
| `fixing` | `amending` | Fixer escalation |
| `accepting` | `done` | Acceptance check passes |
| `any` | `blocked` | Escalation with `blocker` reason |
| `any` | `needs-attention` | Escalation with any other reason |
| `blocked` | `designing` | Human resolves blocker, restarts |
| `blocked` | `design-reviewing` | Human resolves blocker, restarts at design review |
| `blocked` | `coding` | Human resolves blocker, restarts at coding |
| `blocked` | `fixing` | Human resolves blocker, restarts at fixing |
| `needs-attention` | `designing` | Human intervenes, restarts |
| `needs-attention` | `design-reviewing` | Human intervenes, restarts at design review |
| `needs-attention` | `coding` | Human intervenes, restarts at coding |
| `needs-attention` | `fixing` | Human intervenes, restarts at fixing |

The CLI rejects any transition not in this table.

## Round Counter

The `round` counter increments each time the feature returns to `amending`. It is checked against `maxRounds` before any new Coder run. If `round >= maxRounds`, the CLI rejects the transition and moves to `needs-attention`.
