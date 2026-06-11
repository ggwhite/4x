# 4x Design — Escalation Mechanism

> Extracted from design.md §6

---

Escalation moves a feature to `blocked` or `needs-attention` and stops automatic progression. A human must intervene to resume.

## Escalation Reasons

| Reason | Description | Who Triggers |
|---|---|---|
| `spec-mismatch` | Coder finds the spec is contradictory or impossible to implement as written | Coder (via coder-report.md flag) |
| `criteria-wrong` | Tester finds an acceptance criterion cannot be verified as written | Tester |
| `blocker` | External dependency missing (service down, API unavailable, missing credential) | Any role or CLI |
| `scope-change` | The feature requires modifying repos not in its declared scope | CLI scope check |
| `max-rounds` | `round >= maxRounds` | CLI |
| `no-progress` | `consecutiveNoProgress` exceeds threshold (default: 3) | CLI |

## Escalation Flow

1. The triggering agent writes a description of the reason to the relevant report file (e.g., `coder-report.md` section "Escalation").
2. The CLI (or plugin) calls `4x event escalation --reason <reason> --detail "..."`.
3. The CLI writes the `escalation` event to `events.jsonl`.
4. The CLI updates `state.json`: `phase` → `needs-attention` or `blocked`, `active` → `false`, `stopReason` → reason.
5. The dashboard highlights the feature.
6. A human reads the report, resolves the issue, and resumes with `4x transition <feature-id> designing` (or `coding`).
