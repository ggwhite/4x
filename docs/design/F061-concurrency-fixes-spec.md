# F061: Concurrency and Atomicity Fixes — Spec

## Problem
6 WARNING-level bugs related to concurrency, atomicity, and error handling:

- W8: ensureInactive read-modify-write on state.json races with runner subprocess writing the same file — can overwrite runner's final state, regressing phase
- W9: SSE file-size guard skips data permanently when events.jsonl is truncated — `info.Size() <= lastOffset` never recovers
- W14: WriteState uses os.WriteFile (truncate+write, not atomic) — concurrent ReadState can see partial JSON
- W15: handlePostDone reads state, runs git merge (seconds), then uses stale state for transition — TOCTOU
- W16: Unresolved {model} placeholder passed as literal string to runner when ModelOverride is empty
- W17: promptFile creation failure (e.g. /tmp full) silently passes literal "{promptFile}" to runner

## Acceptance Criteria
- AC-1: ensureInactive checks UpdatedAt before overwriting state — skip if state was modified after process start
- AC-2: SSE resets lastOffset to 0 when file size < lastOffset (truncation detected)
- AC-3: WriteState uses write-to-temp + os.Rename for atomic writes
- AC-4: handlePostDone re-reads state after git merge before calling transitionDone
- AC-5: buildArgs returns error when ModelOverride is empty and {model} is in args
- AC-6: buildArgs returns error when CreateTemp fails, instead of passing placeholder
