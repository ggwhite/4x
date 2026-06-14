# F060: Run Loop Hardening — Spec

## Problem
10 WARNING-level bugs found by systematic audit in the run loop and batch mode:

- W1: ConsecutiveNoProgress is checked in ShouldStop but never incremented — dead code, loops burn tokens
- W2: PhaseAbandoned transition doesn't set Active=false — abandoned features can be re-run
- W3: PTY runner bypasses exec.CommandContext, ignoring context cancellation — process hangs forever
- W4: Batch run skips dependency check, done check, and PID recording
- W5: startLiveSync stop has no drain — races with final sync
- W6: syncFeatureFromWorktree swallows all errors silently
- W7: verify.json Passed field is self-reported, never cross-validated against command exit codes
- W10: nextPhaseAfter for designing only checks task-brief, not acceptance-criteria
- W11: Scope detector uses git diff --name-only HEAD, misses untracked and staged files
- W12: Batch run infinite loop on features that repeatedly fail

## Acceptance Criteria (one per subtask)
- AC-1: ConsecutiveNoProgress incremented on amending when fail count unchanged, reset when improved
- AC-2: Transition to PhaseAbandoned sets Active=false; run loop breaks on PhaseAbandoned
- AC-3: PTY runner kills process group on context cancellation within 5 seconds
- AC-4: Batch run checks dependencies, done phase, and records PID before each feature
- AC-5: startLiveSync stopSync() waits for in-flight sync to complete
- AC-6: syncFeatureFromWorktree returns error; callers log it
- AC-7: Guard rejects verify.json where Passed=true but any command has exit code != 0
- AC-8: nextPhaseAfter for designing checks acceptance-criteria.md existence
- AC-9: Scope detector includes untracked and staged files
- AC-10: Batch run skips features that failed in current batch, prevents infinite retry
