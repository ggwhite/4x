# F045: Phase Hooks — pre/post phase shell commands

## 現狀

Phase 轉換時沒有 hook 機制，起環境（docker compose up）或清 container 需要人工處理。

## 需求

在 phase 轉換前後自動執行 shell command，由 CLI 層負責，不經 LLM。

## 設定格式

### settings.json（全域）

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

- key 格式：`pre_{phase}` 或 `post_{phase}`
- `run`：shell command，透過 `sh -c` 執行
- `on_fail`：`"block"`（預設）或 `"warn"`

### Feature YAML（per-feature override）

Feature YAML 可加 `hooks` 欄位，格式與全域相同。Feature 的 hook 會 override 全域同名 hook（同一個 key 整組替換，不是 append）。

## 執行流程

```
pre_{target_phase} hooks（依陣列順序）
  ↓ 任一 on_fail=block 的 hook 失敗 → 中止，不轉換
state.Transition()
  ↓
post_{target_phase} hooks（依陣列順序）
  ↓ on_fail=block 的 hook 失敗 → 轉 needs-attention（不回滾）
記錄 event
```

- pre hook 失敗（block）：phase 不轉換，回報錯誤
- post hook 失敗（block）：phase 已轉換不回滾，改轉 `needs-attention`
- on_fail=warn 的 hook 失敗：記 log 繼續

## Log 記錄

### Event log（events.jsonl）

每個 hook 執行完追加一筆：

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "command": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

失敗時 `status` 為 `"fail"`，`detail` 帶 exit code。

### Hook output 檔

完整 stdout/stderr 寫入 `.4x/run/{featureId}/hook-logs/{timestamp}-{action}.log`，供 debug 用。

## 實作位置

| 動作 | 檔案 | 內容 |
|---|---|---|
| 新增 | `internal/hook/hook.go` | `HookEntry` struct、`Execute()` 函式、log 寫入 |
| 修改 | `internal/protocol/types.go` | `Config` 加 `Hooks map[string][]HookEntry`、`FeatureFile` 加 `Hooks` |
| 修改 | `internal/protocol/merge.go` | hooks 合併邏輯（feature override 全域同名 key） |
| 修改 | `cmd/4x/transition.go` | transition 前後呼叫 `hook.Execute()` |
| 修改 | `cmd/4x/run.go` | runLoop 裡 transition 前後呼叫 `hook.Execute()` |

不修改 `internal/state/machine.go`。

## 約束

- Hook 由 CLI 執行，不經 LLM
- State machine 保持純粹，不引入 side effect
- `on_fail` 預設為 `"block"`
- Hook command 透過 `sh -c` 執行，繼承 CLI 的環境變數
