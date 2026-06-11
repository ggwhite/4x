# 4x Progress

## Current Status

| Area | Done | In Progress | Todo |
|---|---|---|---|
| CLI | init, new, status, check, transition, event, prompt, batch, live, run, dependency gate | — | backlog sync guard |
| Plugin | runner interface, claude-code (SKILL.md + workflow.js), codex (AGENTS.md), gemini (GEMINI.md), agy (AGY.md), copilot (AGENTS.md), cursor (.cursorrules) | — | — |
| Dashboard | web UI (SSE + REST API) | — | macOS native (Swift), Electron |
| Tests | state, protocol, guard, batch, server, runner, cmd integration = **passing via `go test ./...`** | — | broaden e2e with live LLM only when explicitly needed |

## Active Feature

None

## Last Session (2026-06-11)

### WS-1: State machine tests (56 tests)
- Table-driven tests for CanTransition, Transition, PhaseToRole, ShouldStop
- **Bug fixed**: `Transition()` round increment checked `s.Phase` after overwrite — amending→coding incorrectly incremented round

### WS-2: Protocol tests (11 tests)
- Workspace.Find walk-up, Init, Config roundtrip, Feature CRUD, State roundtrip, Event append

### WS-3: Guardrail tests (11 tests)
- checkRequiredFiles, checkBaseline, checkScope, CaptureBaseline with git fixture
- **Bug fixed**: `checkRequiredFiles` used string `>=` comparison on Phase — `"init" > "coding"` lexicographically, causing false requirement of design outputs in init phase

### WS-4: Batch planner (10 tests)
- Complete rewrite: dependency DAG, cycle detection, Union-Find clustering, topological sort, chain scheduling (max_chain_length), batch-plan.json output
- CLI `batch plan --dry-run` and `batch next` using plan

### WS-5: Live server (6 tests)
- Added SSE endpoint `/sse/events/{featureId}` with file polling
- Refactored `NewMux()` for testability
- httptest tests for tasks, events, messages, SSE, index

### WS-6: Plugin runner (8 tests)
- `Runner` interface + `SubprocessRunner` implementation
- Exit code handling (0=success, 1=soft fail, 2=hard error), context timeout
- `NewRunner` factory from config

### WS-7: E2e validation
- Set up examples/todo-api with .4x/ workspace
- Shell-based e2e test exercising full Design→Code→Review→Test→Accept→Done flow
- Model config passthrough in workflow.js (hardcoded → `args.models`)

### WS-8: Backlog source-of-truth sync
- `.4x/features/*.yaml` 為 canonical source of truth，`feature_list.json` 為 legacy mirror
- `CompareBacklogMirror()` drift detection（missing/extra/mismatch）
- `4x status` 和 `4x check` 顯示 drift warnings
- `testing→accepting` transition gate：要求 verify.json / test-report.md / final-report.md / commit-plan.md
- Tester prompt 更新：要求產出 verify.json 及 final/commit 文件

## Next Steps

1. Add dependency gating before `4x run`
2. Build real e2e test with Claude Code plugin (requires live LLM)
3. macOS native dashboard (Swift)
4. Additional plugins (Copilot, Cursor)
