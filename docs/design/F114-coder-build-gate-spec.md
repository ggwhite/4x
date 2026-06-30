# F114 — Coder Build Gate

## 概述

Coder phase 結束前，`4x check` 自動執行 settings.json 的 `build` + `lint` 指令。失敗時 exit 1 讓 Coder agent 在同一 session 內自行修復；Orchestrator 層加最後防線，確保 build/lint 失敗的程式碼不會進入 Reviewer。

## 問題

目前 Coder phase 的 `4x check` 只驗 scope/baseline/required-files，不跑 build/lint。靜態就能抓到的編譯錯誤、lint warning 要等 Tester phase 的 `4x verify` 才爆，白白多跑一輪 review + test。

## 設計

### 1. `checkBuildGate()` — guard 層

在 `internal/guard/check.go` 的 `Check()` 內新增 `checkBuildGate()` 呼叫：

- 讀 `state.json` 取得當前 phase
- 只在 `PhaseCoding` 或 `PhaseAmending` 時執行，其他 phase 直接 return
- 讀 `settings.json` 的 `project.build` + `project.lint`（不含 `project.test`）
- 用 `verify.RunGroups()` 執行兩個 group（`build` 和 `lint`），組內依序、組間平行
- 結果寫入 `.4x/run/{feature}/rounds/round-{n}/build-gate.json`
- 任一指令 exit code != 0 → `r.Pass = false`，error 包含失敗指令與摘要

`build-gate.json` 復用 `protocol.VerifyEvidence` 結構：

```json
{
  "passed": false,
  "round": 1,
  "role": "coder",
  "commands": [
    {"command": "make build", "exit_code": 0, "group": "build", "duration_ms": 1234},
    {"command": "make lint",  "exit_code": 1, "group": "lint",  "duration_ms": 567, "summary": "unused variable..."}
  ]
}
```

build 和 lint 各為獨立 group 的理由：build 失敗時 lint 通常也會失敗（因為編譯不過），分開跑可以 skip lint group 省時間。但因為 `verify.RunGroups` 是平行跑所有 group 的，所以實際做法是把 build + lint 合成一個 group（build 在前），build 失敗後同 group 內的 lint 會被 skip。

修正：使用單一 group `build-gate`，commands 順序為 `build` 指令在前、`lint` 指令在後。復用 `runGroup` 的「前一個失敗就 skip 後續」語意。

### 2. Orchestrator 防線

`internal/orchestrator/phase.go` 的 `NextPhaseAfter(PhaseCoding)` 加一道檢查：

- 讀 `.4x/run/{feature}/rounds/round-{n}/build-gate.json`
- 檔案不存在 → `NeedsAttention`（Coder 沒跑 `4x check`）
- `passed: false` → `NeedsAttention`（build/lint 未修復）
- `passed: true` → 正常進 `PhaseReviewing`

不做 phase retry（不回 `PhaseCoding`）——Coder 工作量大，打回重跑代價太高。`NeedsAttention` 讓人介入。正常情況下 Coder agent 在 session 內看到 `4x check` 失敗就會自修，這道防線只是最後保險。

### 3. Coder prompt 調整

`templates/coder.md.tmpl` 加入 `4x check` 自修迴圈指示：

> `4x check` 包含 build + lint 驗證。如果 `4x check` 失敗，讀 error 輸出修復問題後重跑，直到通過才寫 coder-report.md。

不刪除原有的 `make build && make test && make lint` 指示——那是開發過程中的隨時自檢，`4x check` 是最後的 gate，兩者互補。

`settings.json` 的 `roles.coder.instructions` 對應更新。

## 不做的事

- **不跑 `test` 指令** — test 可能很重（integration/e2e），是 Tester 職責
- **不引入 Coder phase retry** — 不像 Tester 的 guard retry（刪 artifact 重跑），修復在 agent session 內完成
- **不改 `4x verify`** — Tester 流程完全不動
- **不改其他 phase 的 `4x check`** — `checkBuildGate` 靠 phase 判斷，只在 coding/amending 觸發
- **不發明新 struct** — `build-gate.json` 復用 `protocol.VerifyEvidence`

## 影響範圍

| 檔案 | 改動 |
|---|---|
| `internal/guard/check.go` | 新增 `checkBuildGate()`，`Check()` 內呼叫 |
| `internal/orchestrator/phase.go` | `PhaseCoding/PhaseAmending` case 加讀 `build-gate.json` |
| `templates/coder.md.tmpl` | 加 `4x check` 自修迴圈指示 |
| `internal/protocol/workspace.go` | 新增 `BuildGateFile` 常量 |
| `internal/guard/check_test.go` | 補 `checkBuildGate` 單元測試 |
| `internal/orchestrator/phase_test.go` | 補 orchestrator 防線測試 |
