# 4x Design — Guardrails (`4x check`)

> Extracted from design.md §7

---

`4x check` runs before every state transition. It is synchronous and deterministic; it never calls an LLM.

## 7.1 Scope Check

Compares files changed since baseline commit against the declared `repos` and `scope` paths in `features/{feature-id}.yaml`.

- Any changed file outside declared scope → scope check fails.
- Failure emits a `scope-check` event with `passed: false` and the list of violations.
- Transition is blocked until violations are resolved or scope is explicitly widened (requires human edit of the feature yaml).

## 7.2 Baseline Check

Verifies that `baseline.json` exists and was captured from the same commit that is the parent of current changes. Required for Tester phase.

## 7.3 Required Files Check

Before each transition, validates that all required output files for the preceding role are present and non-empty.

| Transition | Required Files |
|---|---|
| `designing` → `coding` | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` |
| `coding` → `reviewing` | `coder-report.md` |
| `reviewing` → `testing` (pass) | `review-report.md` with `PASS` verdict |
| `reviewing` → `amending` (fail) | `review-report.md` with `FAIL` or `CONDITIONAL PASS` verdict |
| `amending` → `reviewing` | `coder-report.md` updated (mtime > previous) |
| `testing` → `accepting` | `verify.json`, `test-report.md`, `final-report.md`, `commit-plan.md` |

## 7.4 Verify Evidence Check

Before `testing` → `accepting`, validates that `verify.json`:
- `passed` is `true`
- `evidence` array is non-empty
- Each evidence entry has `type`, `command`, `passed: true`
