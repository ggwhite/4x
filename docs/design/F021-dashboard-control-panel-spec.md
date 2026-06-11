# F021 — Dashboard Control Panel

## 概述

把 4x live Dashboard 從純監控升級為控制面板，讓使用者能從 UI 啟動/停止 run、建立新 feature。
依賴 F020 Server Write API 提供的 endpoints。

用 Alpine.js（CDN）重構現有 vanilla JS，改為聲明式元件架構，維持單檔 SPA。

## Alpine.js 引入

```html
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3/dist/cdn.min.js"></script>
```

用 `Alpine.store('app')` 管理全域 state，`Alpine.data()` 拆分元件。所有 JS 仍在 `<script>` 裡，維持單檔。

## 元件拆分

| 元件 | 職責 |
|---|---|
| `app` | 全域 state（tasks、current、settings、runs）、polling、SSE |
| `sidebar` | feature 列表、分組、play/stop 按鈕、+ 新增按鈕 |
| `detail` | header + messages 區、play/stop 按鈕、log 顯示 |
| `dashboard` | 首頁統計（donut、rounds、recent completions） |
| `runModal` | runner/maxRounds/extraPrompt 表單 |
| `newModal` | name/description 表單 |
| `searchModal` | Cmd+K 搜尋（從 vanilla JS 遷移） |
| `settingsModal` | Cmd+, 設定（從 vanilla JS 遷移） |

所有元件透過 `Alpine.store('app')` 共享 state。

## 新增 Endpoint: GET /api/config

Run modal 需要知道可用的 runners 和預設值。新增一個 read endpoint：

```
GET /api/config
Response: {
  "runners": ["claude", "gemini", "agy"],
  "defaultRunner": "claude",
  "maxConcurrentRuns": 1
}
```

實作在 `internal/server/server.go`，讀取 workspace 的 settings.json。

## UI 功能

### Play/Stop 按鈕

**Sidebar 卡片：**
- 非執行中 → hover 顯示 ▶ play 按鈕（卡片右上角）
- 執行中 → 顯示 ■ stop 按鈕（取代 play）
- 點 play → 開 run modal
- 點 stop → 直接呼叫 `POST /api/stop`（有 graceful shutdown，不需額外確認）

**Detail header：**
- 同樣邏輯，按鈕放在 badge 旁邊

**狀態判斷：** 跟 `GET /api/tasks` 同頻率 polling（使用者設定的 refresh interval，預設 3 秒），同時拉 `GET /api/runs`，比對 featureId 判斷哪些 feature 正在跑。

### Run Modal

按 play → `$store.app.openRunModal(featureId)` → 顯示 modal：

- **Runner 下拉** — 從 `GET /api/config` 的 runners 清單填入，預選 defaultRunner
- **Max Rounds** — 數字輸入，預設 5
- **Extra Prompt** — textarea，選填
- **Cancel / Run ▶** 按鈕

所有欄位有預設值，直接按 Run 就能啟動。

確認後呼叫 `POST /api/run`：
- 成功 → 關閉 modal、refresh task list、按鈕切換為 stop
- 409（並行上限）→ 顯示 error toast
- 404（feature 不存在）→ 顯示 error toast

### New Feature Modal

按 sidebar 頂部 + 按鈕 → 顯示 modal：

- **Name** — 文字輸入，必填
- **Description** — textarea，選填
- **Cancel / Create** 按鈕

確認後呼叫 `POST /api/new`：
- 成功 → 關閉 modal、refresh task list、新 feature 出現在 sidebar

### Log 顯示

SSE 推送的 `run-output` / `run-error` event 直接 append 到 messages 區，跟 role artifact 混在一起：

- `run-output` → 灰底 monospace card，像 terminal 輸出
- `run-error` → 紅色左邊框 monospace card

不經 `/api/messages/` endpoint，直接從 SSE event 即時渲染。

## 既有功能遷移

以下功能從 vanilla JS 遷移到 Alpine.js，行為不變：

- Cmd+K 搜尋（fuzzy match、繁簡中文正規化）
- Cmd+, 設定（theme、font size、refresh interval）
- Dashboard 首頁統計（donut、rounds distribution、recent completions）
- Sidebar 分組（running/pending/done）
- Feature detail（header + messages）
- SSE 連線管理
- localStorage 設定持久化

## 樣式

- 沿用現有 CSS variables dark theme 系統
- 新 modal 風格跟 search/settings modal 一致（`.modal-backdrop` + `.modal-panel`）
- 按鈕用 `var(--accent)` 色系

## 測試

### Go 端

- `TestGetConfig` — 確認 `GET /api/config` 回傳 runners 清單和 defaultRunner

### 手動驗證

- Play → run modal → 預設值正確 → Run → API 呼叫成功
- Stop → API 呼叫成功 → 按鈕切回 play
- + → new modal → 填名稱 → Create → sidebar 出現新 feature
- SSE log → 執行中 messages 區有 run-output card
- 既有功能 → Cmd+K、Cmd+,、theme、dashboard 統計都正常

## 不做的事

- build toolchain（webpack、vite 等）
- 拆成多檔（維持單檔 SPA）
- run 排隊 UI（F020 不支援排隊）
- feature 編輯/刪除 UI
