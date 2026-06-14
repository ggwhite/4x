# F061: Concurrency and Atomicity Fixes — Plan

## W14: Atomic WriteState (do first — fixes root cause for W8 and W15)
- `internal/protocol/workspace.go` WriteState — write to temp file in same dir, then os.Rename
- Pattern: `os.CreateTemp(dir, ".state-*.json")` → write → close → `os.Rename(tmp, path)`
- os.Rename is atomic on same filesystem (POSIX guarantee)
- Test: concurrent WriteState + ReadState → no partial JSON

## W8: ensureInactive race
- `internal/server/process.go` ensureInactive — ReadState, compare UpdatedAt with process start time; if state was updated after the tracked process started its last phase, skip the overwrite (the runner already wrote its final state)
- Test: simulate runner writing state with newer UpdatedAt → ensureInactive skips

## W9: SSE truncation recovery
- `internal/server/server.go` handleSSE — when info.Size() < lastOffset, reset lastOffset = 0, re-read from beginning
- Test: write events, establish SSE offset, truncate file, write new events → verify new events received

## W15: handlePostDone TOCTOU
- `internal/server/server.go` handlePostDone — after git merge completes, re-read state; if phase changed since first read, return 409 Conflict
- Test: mock concurrent state change during merge → verify 409 response

## W16: Unresolved {model} placeholder
- `internal/runner/runner.go` buildArgs — when ModelOverride is empty and arg contains {model}, return error instead of keeping placeholder
- Test: empty model + args with {model} → error returned

## W17: promptFile creation failure
- `internal/runner/runner.go` buildArgs — when CreateTemp fails, return error wrapping the CreateTemp error
- Test: mock CreateTemp failure → error returned

## Order
W14 → W8 → W9 → W15 → W16 → W17 (W14 first as it's the foundation)

## Verification
```bash
go build ./cmd/4x && go vet ./... && go test -race ./...
```
