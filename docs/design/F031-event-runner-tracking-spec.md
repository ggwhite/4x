# F031 — Event Runner/Model Tracking

## 問題

同一個 feature 可能跨 runner 執行（例如第一次跑 Codex 中斷，第二次用 Claude 接續），但目前：

- **Event** (`events.jsonl`) 不記錄 runner 和 model，無法追溯每個動作是由哪個 LLM 執行
- **State** (`state.json`) 只記當前 `runner`，每次 run 開始就覆蓋，丟失歷史

結果是無法得知一個 feature 總共用過哪些 LLM。

## 目標

1. 每個 event 記錄產生它的 runner 和 model
2. State 維護累積的 runners 清單，供快速查詢
3. Dashboard feature 列表顯示 runner tag，詳情 timeline 顯示每個 event 的 runner/model

## 設計

### Data Model

**Event struct** — 新增兩個 `omitempty` 欄位：

```go
type Event struct {
    Timestamp string `json:"ts"`
    Phase     Phase  `json:"phase"`
    Type      string `json:"type"`
    Role      Role   `json:"role,omitempty"`
    Round     int    `json:"round,omitempty"`
    Action    string `json:"action,omitempty"`
    Command   string `json:"cmd,omitempty"`
    Status    string `json:"status,omitempty"`
    Detail    string `json:"detail,omitempty"`
    Runner    string `json:"runner,omitempty"`
    Model     string `json:"model,omitempty"`
}
```

**State struct** — 新增累積清單：

```go
type State struct {
    // ...existing fields...
    Runners []string `json:"runners,omitempty"`
}
```

`Runners` 是去重的 runner 名稱清單（如 `["codex", "claude"]`），只 append 不刪除。

### 向後相容

- Event 新欄位為 `omitempty`：舊 events.jsonl 中不含這些欄位的行照常 unmarshal
- State 新欄位為 `omitempty`：舊 state.json 沒有 `runners` 時 unmarshal 為 nil
- Dashboard 對 `runners` 為空時不顯示 tag，不影響既有 feature

### 寫入邏輯

#### cmd/4x/run.go

1. **Run 啟動時**（`runCmd` 中寫入初始 state 之前）：若 `runnerName` 不在 `s.Runners` 中，append 並寫回 state
2. **所有 `AppendEvent` 呼叫**：帶上 `Runner: s.Runner`
3. **`runLoop` 中的 phase-start / run-end**：同時帶 `Model: resolveModel(...)`（此時 model 已 resolve）
4. **run-start**（在 `runLoop` 之前）：帶 `Runner`，Model 可為空（尚未進入 phase）
5. **escalation**：帶 `Runner`，Model 可為空

#### workflow.js

workflow.js 透過 shell echo 寫 events.jsonl。args 已包含 runner 資訊（從 plugin CLAUDE.md 傳入），model 從 `args.models` 解析：

```javascript
echo '{"type":"phase-start","phase":"...","role":"...","round":1,"runner":"claude","model":"opus"}' >> events.jsonl
```

### Dashboard

#### Feature 列表 sidebar

在 feature 名稱下方（`renderDashboard` 和 `load` 中的 feature item 渲染），從 task 的 `runners` 欄位聚合，顯示彩色 tag：

```html
<span class="runner-tag">claude</span>
<span class="runner-tag">codex</span>
```

每個 runner 配固定顏色（可用 hash 或預設色盤）。

#### Feature 詳情 header

目前 `h-meta` 顯示 `⬡ {runner}`（單一值，來自 `state.runner`）。改為遍歷 `state.runners` 顯示所有用過的 runner。

#### Messages timeline

Message card（`renderMsgCard`）目前顯示 role 名稱。events 帶了 runner/model 後，SSE 推送的 event 可在 card 中補充顯示 runner 資訊。但 messages 來自 protocol artifacts、不是 events，所以這部分是 **nice-to-have**，不在核心 scope 內。

### API

無需新增 endpoint：

- `/api/tasks` 返回的 task 物件已包含 state 欄位 → `runners` 自動出現
- `/api/events/{id}` 返回 raw JSONL → 新欄位直接可見

### 測試

- `internal/protocol/` 型別序列化測試：確認新欄位正確 marshal/unmarshal
- `cmd/4x/` 測試：確認 event 寫入時帶 runner/model、state.Runners 累積邏輯
- 向後相容測試：舊格式的 events.jsonl 和 state.json 仍可正常 parse

## 不做的事

- 不追蹤每個 role 各自用了哪個 model 的聚合統計（可從 events 衍生，不需額外欄位）
- 不改 `/api/tasks` 的 response 結構（新欄位直接加在既有物件上）
- Messages timeline 的 runner 標註列為 nice-to-have，不在本次 scope
