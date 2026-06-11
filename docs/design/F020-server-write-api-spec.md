# F020 — Server Write API

## 概述

為 4x live server 加入 write endpoints，讓 Dashboard（及未來 MCP server）能觸發操作。
CLI 是唯一真相源——所有 write endpoint 透過 `exec("4x", ...)` subprocess 執行。

## Endpoints

### POST /api/run

啟動 `4x run` subprocess。

```
Request:  {"featureId": "F001-xxx", "runner": "claude", "maxRounds": 5}
Response: {"id": "run-uuid", "featureId": "F001-xxx", "runner": "claude"}
```

- `runner`：選填，預設從 settings.json 的 `default_runner`
- `maxRounds`：選填，預設 5
- 超過並行上限回 409
- feature 不存在回 404

### POST /api/stop

終止執行中的 run。

```
Request:  {"id": "run-uuid"}
Response: {"ok": true}
```

- run 不存在或已結束回 404

### POST /api/new

建立新 feature。

```
Request:  {"name": "My Feature", "description": "optional desc"}
Response: {"id": "F025-my-feature", "name": "My Feature"}
```

- `name` 必填
- `description` 選填
- 內部呼叫與 CLI `4x new` 相同的 `nextFeatureNumber` + `generateID` 邏輯

### GET /api/runs

列出目前執行中的 runs。

```
Response: [{"id": "run-uuid", "featureId": "F001-xxx", "runner": "claude", "startTime": "2026-06-11T10:00:00Z"}]
```

## Process Manager

新增 `internal/server/process.go`。

### 型別

```go
type RunInfo struct {
    ID        string
    FeatureID string
    Runner    string
    Cmd       *exec.Cmd
    StartTime time.Time
}

type ProcessManager struct {
    mu          sync.Mutex
    runs        map[string]*RunInfo
    maxParallel int
    ws          *protocol.Workspace
}
```

### 行為

- **Start(featureID, runner, maxRounds)** — 檢查並行上限 → `exec.Command("4x", "run", featureID, "--runner", runner, "--max-rounds", N)` → stdout/stderr pipe 到 goroutine 寫進 feature 的 events.jsonl → 回傳 RunInfo
- **Stop(runID)** — SIGTERM → 等 5 秒 → SIGKILL → 從 map 移除
- **List()** — 回傳所有執行中的 RunInfo
- **Shutdown()** — SIGTERM 所有 subprocess → 等 5 秒 → SIGKILL，server 關閉時呼叫
- 每個 subprocess 開一個 goroutine 等結束，結束後自動從 map 移除

### 並行控制

- settings.json 新增 `max_concurrent_runs` 欄位，預設 1
- 超過上限的 Start 呼叫回傳錯誤，endpoint 轉為 409

## Server 變更

### NewMux 簽名

```go
func NewMux(ws *protocol.Workspace, pm *ProcessManager) http.Handler
```

### live.go

```go
pm := NewProcessManager(ws, maxParallel)
defer pm.Shutdown()
server.Start(ws, pm, port)
```

### Subprocess 輸出

subprocess 的 stdout/stderr 由 goroutine 逐行讀取，每行包成一筆 event 寫進對應 feature 的 events.jsonl：

```json
{"type": "run-output", "detail": "stdout line content", "round": 0, "ts": "2026-06-11T10:00:01Z"}
{"type": "run-error",  "detail": "stderr line content", "round": 0, "ts": "2026-06-11T10:00:01Z"}
```

使用既有的 `ws.AppendEvent()` 寫入。`round` 設 0 表示非 role loop 產生的 event。
現有 SSE 機制（tail events.jsonl）自動把新輸出推給前端，不需修改 SSE handler。

## Config 變更

settings.json 新增：

```json
{
  "max_concurrent_runs": 1
}
```

放在 Config struct 頂層：

```go
type Config struct {
    // ...existing fields...
    MaxConcurrentRuns int `json:"max_concurrent_runs,omitempty"`
}
```

預設值在 ProcessManager 建構時處理：若為 0 則視為 1。

## 測試

### process_test.go

- `TestProcessManager_StartAndList` — 啟動 subprocess，List 確認有一筆
- `TestProcessManager_Stop` — Stop 後確認 process 結束、從 map 移除
- `TestProcessManager_MaxParallel` — 上限 1 時第二個 Start 回錯誤
- `TestProcessManager_Shutdown` — Shutdown 終止所有 subprocess

### server_test.go 補充

- `TestPostRun` — 正常啟動回 200 + RunInfo
- `TestPostRun_Conflict` — 超過上限回 409
- `TestPostStop` — 停止回 200
- `TestPostStop_NotFound` — 不存在回 404
- `TestPostNew` — 建立 feature，確認 YAML 存在
- `TestGetRuns` — 回傳執行中清單

測試用假 command（`sleep 60`），不真的跑 `4x run`。

## 不做的事

- MCP server 整合（F023 範疇）
- Dashboard UI 控制面板（F021 範疇）
- Subprocess 輸出的即時 log streaming UI
- run 排隊機制（超過上限直接拒絕，不排隊）
