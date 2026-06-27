# Coder Report — Round 2

## What Was Done

修復 Round 1 review 指出的兩大問題：gofmt 格式違規與測試未搬遷至 orchestrator package。
同時完成 task-brief Step 5（精簡 newRunCmd / 搬遷測試）。

### 1. gofmt 格式修正
修正 7 個檔案的 gofmt 違規（struct literal 對齊、函式呼叫空格、trailing newline）。

### 2. 精簡 run.go — 消除 state/guard import
- 新增 orchestrator wrapper：`CheckDependencies`、`RecoverState`、`PhaseToRole`
- 搬遷 `parsePhaseOverrides` → `orchestrator.ParsePhaseOverrides`
- 搬遷 `worktreeExitHints` → `orchestrator.WorktreeExitHints`
- 搬遷 `startBackgroundRun` → `orchestrator.StartBackgroundRun`
- 搬遷 `DeferRunCleanup` 到 orchestrator（原為 RunE 內 defer 區塊）
- 拆分 `newRunCmd()` 為 `resolveRunParams` / `launchBackgroundJSON` / `setupWorktree` / `initOrResumeState` / `resolveProfileFlag` / `handlePostLoop`
- 搬遷 profile UI 到新檔 `profile.go`（不匹配 `run*.go` 前綴）
- run.go 不再 import `internal/state`、`internal/guard`、`internal/prompt`

### 3. 測試搬遷至 internal/orchestrator/
- `run_context_test.go` → `orchestrator_test.go`（ParseRunStatsFromLog, FormatTokens）
- `run_resume_test.go` + `run_subphase_test.go` → `resume_test.go`（SmartResumePhase 等 18 tests）
- `run_f089_test.go` → `worktree_test.go`（SyncFeature{To,From}Worktree）
- `run_f096_test.go` → `deep_review_test.go`（AutoDiscoverFeatures 4 tests）
- 新增 `cmd/4x/test_helpers_test.go` 放 cmd 層測試共用 helper

### 4. 文件同步
更新 `docs/guide/cli.md` 中引用的內部函式名（smartResumePhase → SmartResumePhase 等）。

## Files Changed

### 新增
- `cmd/4x/profile.go` — profile 互動選單 UI（從 run.go 搬出）
- `cmd/4x/test_helpers_test.go` — cmd 層測試共用 helper
- `internal/orchestrator/orchestrator_test.go` — ParseRunStatsFromLog / FormatTokens 測試
- `internal/orchestrator/resume_test.go` — SmartResumePhase / CleanStaleArtifact / DeepResumeSubPhase 等 18 tests
- `internal/orchestrator/worktree_test.go` — SyncFeature{To,From}Worktree 測試
- `internal/orchestrator/deep_review_test.go` — AutoDiscoverFeatures 4 tests

### 修改
- `internal/orchestrator/orchestrator.go` — 新增 CheckDependencies / RecoverState / PhaseToRole / ParsePhaseOverrides / WorktreeExitHints / DeferRunCleanup / StartBackgroundRun；修正 gofmt
- `cmd/4x/run.go` — 精簡 newRunCmd()，消除 state/guard import，拆出 helper 函式
- `cmd/4x/phase_override_test.go` — 改用 orchestrator.ParsePhaseOverrides
- `cmd/4x/run_f086_test.go` — 改用 orchestrator.WorktreeExitHints / StartBackgroundRun
- `cmd/4x/batch_test.go` — gofmt 修正
- `cmd/4x/done.go` — gofmt 修正
- `cmd/4x/transition.go` — gofmt 修正
- `docs/guide/cli.md` — 更新內部函式名反映 orchestrator 重構

### 刪除
- `cmd/4x/run_context_test.go` — 搬遷至 internal/orchestrator/
- `cmd/4x/run_resume_test.go` — 搬遷至 internal/orchestrator/
- `cmd/4x/run_subphase_test.go` — 搬遷至 internal/orchestrator/
- `cmd/4x/run_f089_test.go` — 搬遷至 internal/orchestrator/
- `cmd/4x/run_f096_test.go` — 搬遷至 internal/orchestrator/

## Verification

- `go build ./cmd/4x`: 成功
- `go vet ./...`: 無 warning
- `go test -race -count=1 ./...`: 1243 tests passed（23 packages）
- `gofmt -l cmd/4x/ internal/orchestrator/`: 無違規
- `make check-docs-sync`: OK: no doc updates needed
- `make check-i18n`: OK: all locale files have matching keys
- `cmd/4x/run*.go` 非測試行數: 399（run.go 363 + run_phase.go 36）≤ 400
- `newRunCmd()` 行數: 97 ≤ 150
- `run.go` 不 import `internal/state`、`internal/guard`、`internal/prompt`
- `internal/orchestrator/` 有 4 個 `_test.go` 檔案
