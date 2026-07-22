# Issue tracker: 4x feature system (this repo's own tracker)

Issues/tickets for this repo are tracked as 4x feature YAML files under
`.4x/features/{feature-id}.yaml`, managed via the `4x` CLI — not GitHub Issues.
(`.4x/settings.json`'s `issue_tracker.enabled` can additionally link a feature to a
GitHub/GitLab issue when configured — check settings before assuming plain local-only.)

## Conventions

- **Create**: `4x new "<name>" --desc "..." [--subtask "id:name"]... [--rule "..."]...
  [--depends F0xx]... [--priority N] [--id <slug>]`. Follow CLAUDE.md's Feature
  Description 撰寫規則 (現狀/需求/約束/subtasks) for the `--desc` body.
- **Read**: `cat .4x/features/{feature-id}.yaml`, or `4x status {feature-id}` for
  current pipeline state (phase/round, not just the YAML's static `status`).
- **List**: `4x status` (all features) or `ls .4x/features/`.
- **Update**: edit the YAML directly, respecting the role write-boundaries in
  CLAUDE.md (e.g. only Designer touches `repos`; humans/orchestrator own top-level
  metadata like `priority`/`rules`/`depends`).
- **"Labels" / triage state**: the `status` field — one of `draft`, `not-started`,
  `in-progress`, `ready-for-review`, `blocked`, `needs-attention`, `done`, `abandoned`.
- **Dependencies / blocking**: the feature's own `depends: [F0xx, ...]` array — no
  separate dependency graph API.
- **Close**: features reach `done`/`abandoned` through the pipeline (`4x done`,
  `4x force-done`, or the Acceptor role) — not by hand-editing `status`.
- **Comment / discussion**: no first-class comment thread. Use
  `docs/reference/discovered-feature-gaps.md` for non-blocking notes spotted mid-task,
  or the feature's `.4x/run/{feature-id}/` artifacts for pipeline-produced findings.

## When a skill says "publish to the issue tracker"

Run `4x new` with a `--desc` that follows the 現狀/需求/約束/subtasks structure.

## When a skill says "fetch the relevant ticket"

Read `.4x/features/{feature-id}.yaml` directly, or run `4x status {feature-id}`.
