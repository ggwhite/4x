# F060: Run Loop Hardening — Plan

For each subtask, list: which files to change, what to change, and what test to add. Keep each item to 3-5 lines max.

## W1: ConsecutiveNoProgress
- `cmd/4x/run.go` — after transition to PhaseAmending, compare current round's fail count (from review/test report) with s.LastFailCount. If same or worse, increment ConsecutiveNoProgress; else reset to 0
- `internal/state/machine.go` ShouldStop already checks >= 3, just needs the increment
- Test: mock 3 rounds with same fail count → verify ShouldStop triggers

## W2: PhaseAbandoned Active=false
- `internal/state/machine.go` Transition — add `case to == PhaseAbandoned: s.Active = false`
- `cmd/4x/run.go` loop break condition — add PhaseAbandoned
- `cmd/4x/transition.go` — add PhaseAbandoned to deactivation list
- Test: transition to abandoned → verify Active=false

## W3: PTY context cancellation
- `internal/runner/pty_unix.go` — add goroutine watching ctx.Done(), send SIGTERM to -pgid, then SIGKILL after 5s
- Test: start PTY runner, cancel context → verify process killed within 5s

## W4: Batch run guards
- `cmd/4x/batch.go` — before runLoop: call guard.CheckDependencies, check phase != done, set s.Pid = os.Getpid()
- Test: batch with done feature → verify skipped; batch with deps → verify checked

## W5: startLiveSync drain
- `cmd/4x/run.go` — change stopSync to use sync.WaitGroup; goroutine calls wg.Done after each sync; stopSync calls close(done) then wg.Wait()
- Test: verify no concurrent sync calls after stopSync returns

## W6: syncFeatureFromWorktree errors
- `cmd/4x/run.go` — change return type to error; propagate CopyFileIfExists errors; caller logs warning
- Test: verify error is returned when copy fails

## W7: verify.json cross-validation
- `internal/guard/check.go` checkTestingToAccepting — iterate Commands, verify all ExitCode==0 when Passed=true
- Test: verify.json with Passed=true but command exit 1 → guard fails

## W8: acceptance-criteria check
- `cmd/4x/run.go` nextPhaseAfter PhaseDesigning — add os.Stat check for acceptance-criteria.md
- Test: designer without criteria → needs-attention

## W9: Scope detector
- `internal/guard/check.go` detectChangedRepos — also run `git ls-files --others --exclude-standard` for untracked
- Test: untracked file in out-of-scope repo → scope violation detected

## W10: Batch retry limit
- `cmd/4x/batch.go` — maintain `failedFeatures map[string]int`; skip if >= 2 attempts in current batch
- Test: feature fails twice → skipped on third attempt

## Order
W2 → W8 → W1 → W7 → W10 → W4 → W6 → W5 → W9 → W3 (simplest first)

## Verification
```bash
go build ./cmd/4x && go vet ./... && go test -race ./...
```
