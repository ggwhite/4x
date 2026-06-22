Notification: Designer action required

Time: 2026-06-19T12:43:00+08:00
Feature: F092-dashboard-settings-ui
State: needs-attention (stopped due to escalation-loop: designer escalated 3 times in round 0)

Summary:
The feature is blocked because the Designer escalated repeatedly without resolving three critical design decisions. Three TODOs have been created in the team's tracker:
- F092-bak-policy — Decide .bak policy and update task-brief/acceptance-criteria
- F092-concurrency-strategy — Define concurrency/locking strategy and add -race verify tests
- F092-patch-merge-locking — Define PATCH merge semantics and locking ownership

Requested action for Designer:
1. Pick and document the decisions above in .4x/F092-dashboard-settings-ui/task-brief.md and acceptance-criteria.md.
2. Add concrete verify_commands (include -race test) into test-strategy.yaml.
3. Mark the three TODOs done when complete so the runner can resume.

If Product/Tech Lead decision is required (e.g. whether .bak is allowed), please add a short decision note to the same task-brief and notify the team.

— Automated notice by 4x tooling
