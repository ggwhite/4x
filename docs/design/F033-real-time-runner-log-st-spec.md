# F033: Real-time Runner Log Streaming via stream-json Output

## Problem

Claude Code runner 在 `--print` 模式下使用 text output format，所有 output 在 session 結束後才一次性寫到 stdout。Dashboard log viewer 只能在階段結束後才顯示內容，無法即時觀察 runner 的工作狀態。

## Solution

改用 `--output-format stream-json --verbose` 讓每個 event 即時輸出一行 JSON。Runner 解析 stream-json 並即時寫入兩份檔案：

- **`.log`**：人類可讀摘要，dashboard log SSE 直接 tail
- **`.stream.jsonl`**：原始 stream-json，供 debug 及未來進階功能使用

## Approach: Runner 層解析（方案 A）

改動集中在 runner 層，dashboard 零改動。新增 `streamJSONProcessor` 作為 `io.Writer`，介於 `cmd.Stdout` 和 log 檔之間。

## stream-json 事件過濾規則

| type | subtype | 動作 | 寫入 .log |
|---|---|---|---|
| `system` | `init` | 忽略 | No |
| `system` | `hook_started` / `hook_response` | 忽略 | No |
| `system` | `thinking_tokens` | 忽略 | No |
| `assistant` | — | 提取 `message.content[]` 中 `type:"text"` 的 `.text` | Yes |
| `assistant` | — | 提取 `message.content[]` 中 `type:"tool_use"` 的工具名與 input 摘要 | Yes |
| `result` | `success` / `error` | 提取 `result`、`duration_ms`、`total_cost_usd` | Yes |
| `rate_limit_event` | — | 忽略 | No |
| 其他未知 type | — | 靜默跳過 | No |

所有行（含被過濾的）原封寫入 `.stream.jsonl`。

## Runner 層設計

### RunnerConfig 新增欄位

```go
type RunnerConfig struct {
    // ...existing fields...
    OutputFormat string `json:"output_format,omitempty"` // "stream-json" or ""
}
```

### SubprocessRunner.Run() 新分支

當 `r.Config.OutputFormat == "stream-json"` 時：

1. 不走 PTY 路徑（即使 `tty: true` 也忽略）
2. 建立兩個輸出檔：`round-N-role.log` + `round-N-role.stream.jsonl`
3. `cmd.Stdout` 接上 `streamJSONProcessor`
4. `cmd.Stderr` 仍寫入 `.log`

### streamJSONProcessor

實作為 `io.Writer`，內部行緩衝切行：

```
stdout bytes -> 按 \n 切行 -> 每行:
  +-- 原始寫入 .stream.jsonl
  +-- json.Unmarshal -> switch type -> 格式化寫入 .log
```

`.log` 寫入格式：

```
[assistant] 我來幫你實作這個功能...
[tool_use] Edit: internal/runner/runner.go
[tool_result] (success, 15 lines)
[result] completed in 45.2s, cost $0.12
```

### Claude runner settings.json 變更

```json
"claude": {
    "command": "claude",
    "args": [
        "--dangerously-skip-permissions",
        "-p", "{prompt}",
        "--output-format", "stream-json",
        "--verbose"
    ],
    "model": "opus",
    "output_format": "stream-json"
}
```

移除 `tty: true`。新增 `output_format: "stream-json"` 告訴 runner 走新路徑。

## Dashboard 影響

零改動。

- `handleLogSSE` 每秒 poll `.log` 新增 bytes 並推送 — 只要 `.log` 即時增長就即時顯示
- `handleLogs` 只篩 `.log` 結尾，`.stream.jsonl` 不影響 API
- 前端 log viewer 收到的仍是純文字 chunk

### 未來可選增強（不在本 feature 範圍）

- `/api/logs/{featureId}/{filename}.stream.jsonl` 端點
- 前端摺疊式 tool call 細節
- Token 用量即時統計面板

## 檔案變更範圍

| 檔案 | 改動 |
|---|---|
| `internal/protocol/types.go` | `RunnerConfig` 加 `OutputFormat` 欄位 |
| `internal/runner/runner.go` | `Run()` 加 `stream-json` 分支 |
| `internal/runner/stream.go`（新檔） | `streamJSONProcessor` 實作 |
| `internal/runner/stream_test.go`（新檔） | processor 單元測試 |
| `.4x/settings.json` | Claude runner config 更新 |

不改：`internal/server/`、其他 runner config、dashboard 前端。

## 測試策略

1. **`stream_test.go` 單元測試**：
   - 模擬 stream-json 行（assistant text、tool_use、result、system events）
   - 驗證 `.log` 只有可讀摘要
   - 驗證 `.stream.jsonl` 有全部原始行
   - 驗證未知 type 不 crash
   - 驗證不完整 JSON 行不 crash

2. **既有 `runner_test.go`**：確認 `OutputFormat: ""` 走舊路徑，行為不變

3. **手動 e2e**：用 `4x live` 跑一個小 feature，觀察 dashboard log viewer 即時顯示
