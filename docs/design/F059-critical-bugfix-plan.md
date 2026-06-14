# F059: Critical Bug Fixes — Plan

## C1: PhaseToRole fix
1. `internal/state/machine.go:63` — split case: PhaseDesigning→RoleDesigner, PhaseAccepting→RoleAcceptor
2. `cmd/4x/run.go` nextPhaseAfter PhaseAccepting — change RoleDesigner to RoleAcceptor
3. Update tests in machine_test.go

## C2: Signal exit code handling
1. `internal/runner/runner.go` buildResult — add check: if exitCode < 0, set ExitCode to ExitHardError (2)
2. `internal/runner/runner.go` buildResult — check for context.Canceled before ExitError, return special status
3. `cmd/4x/run.go` — handle context.Canceled from r.Run() as "interrupted" (same as existing ctx.Err() path)
4. Add tests for exit -1 and context cancellation

## C3: parseReviewVerdict strictness
1. `cmd/4x/run.go` parseReviewVerdict — change logic: require "PASS" prefix for Passed=true, default to false
2. Accept "PASS", "CONDITIONAL PASS" as passing; everything else fails
3. Add tests for edge cases: empty, "TODO", "ERROR", garbled text

## C4: guard.Check() in run loop
1. After runner completes and syncFeatureFromWorktree, call guard.Check()
2. If guard fails with critical errors, transition to needs-attention with guard error detail
3. Skip guard for designer role (no source code changes expected)
4. Add test verifying guard is called and blocks on failure

## Order of implementation
C1 → C3 → C2 → C4 (increasing complexity)

## Verification
```bash
go build ./cmd/4x && go vet ./... && go test -race ./...
```
