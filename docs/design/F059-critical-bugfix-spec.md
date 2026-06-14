# F059: Critical Bug Fixes — Spec

## Problem
Four critical bugs found by systematic audit:

### C1: PhaseToRole maps PhaseAccepting to RoleDesigner
- `internal/state/machine.go:63` groups PhaseDesigning and PhaseAccepting together
- Acceptor runs with designer's prompt template and model tier
- `cmd/4x/run.go` nextPhaseAfter also hardcodes RoleDesigner for PhaseAccepting

### C2: Signal-killed runner (exit -1) treated as success
- `internal/runner/runner.go:162` — Go's ExitCode() returns -1 for signal kills
- Run loop only checks exit 1 (soft-fail) and 2 (hard-error), -1 falls through as success
- Proceeds to next phase reading potentially incomplete artifacts

### C3: parseReviewVerdict defaults to PASS for non-FAIL
- `cmd/4x/run.go:697` — any string that doesn't start with "FAIL" is treated as PASS
- Garbled output, "TODO", "ERROR", "PENDING" all pass review

### C4: guard.Check() never called in run loop
- Only called by `4x check` CLI command
- Run loop only uses limited per-phase artifact existence checks
- Scope, baseline, and required file checks are never enforced

## Solution
Each fix is self-contained and independently testable.

## Acceptance Criteria
- AC-1: PhaseToRole(PhaseAccepting) returns RoleAcceptor
- AC-2: nextPhaseAfter(PhaseAccepting) returns RoleAcceptor
- AC-3: Runner exit code -1 is treated as hard error
- AC-4: context.Canceled produces clean "interrupted" state, not error
- AC-5: parseReviewVerdict returns Passed=false for non-"PASS" prefixed verdicts
- AC-6: guard.Check() called after each runner in the run loop
- AC-7: Guard failure transitions to needs-attention with descriptive reason
- AC-8: All existing tests pass
