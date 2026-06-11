# Multi-Project Live Dashboard

> 讓 `4x live` 支援多專案：啟動時選資料夾、tab 切換、跨專案搜尋。

## 動機

目前 `4x live` 只支援單一專案（從 cwd 往上找一個 `.4x/`），`4x monitor` 有多專案雛形但 UI 簡陋。使用者需要同時監控多個專案的開發進度，且希望一個指令、一個介面搞定。

## 決策

- 統一成 `4x live` 一個指令，廢棄 `4x monitor`
- Server 層用 multi-workspace mux（方案 A），一個 process、一個 port
- 前端 tab 模式，每個 tab 一個專案
- Native app（macOS Swift / 未來 Windows）是 WebView wrapper，與 web 共用同一 server

---

## 1. CLI — `4x live` 統一指令

### 用法

```
4x live                     # 無引數 → 專案選擇器（互動選資料夾）
4x live <path>              # 指定路徑 → 直接進單專案 dashboard
4x live <path1> <path2>...  # 多路徑 → 多專案 tab 模式

Flags:
  --port, -p    指定 port（預設 4567）
  --web, -w     啟動後自動開瀏覽器
  --app, -a     啟動後自動開 native app
```

### Flag 行為

| Flag | 行為 |
|------|------|
| 無 flag | 只啟動 server，印出 URL |
| `-web` | 啟動 server + `open http://localhost:{port}`（macOS）/ `start`（Windows） |
| `-app` | 啟動 server + 啟動對應平台的 native app |
| `-web -app` | 兩個都開 |

### 無引數啟動流程

CLI 無引數時不做 terminal 互動，直接啟動 server 並依 `--web` / `--app` 開啟介面。專案選擇在前端完成：

1. Server 啟動，載入 `~/.4x/recent-projects.json` 的歷史專案（驗證路徑有效後加入 workspace map）
2. 前端顯示專案選擇器（見第 3 段）
3. 使用者在前端選擇或新增專案
4. Server 更新 recent-projects.json

---

## 2. Server — Multi-Workspace Mux

### Project ID

用 workspace root 的 base name（如 `/home/user/my-app` → `my-app`），重名時加數字後綴 `my-app-2`。

### API 路由

```
# 全域
GET    /                                → 前端 SPA（嵌入 index.html）
GET    /api/projects                    → [{ id, name, path, taskCount }]
POST   /api/projects                    → 新增專案（body: { path }）
DELETE /api/projects/{id}               → 從本次 session 移除（不刪 .4x/）

# 每個專案（prefix routing）
GET    /api/project/{id}/tasks
GET    /api/project/{id}/messages/{featureId}
GET    /api/project/{id}/events/{featureId}
GET    /sse/project/{id}/events/{featureId}
```

### 實作要點

- `NewMux(ws)` 保留為內部函式，產出單一 workspace 的 handler
- 新增 `NewMultiMux(workspaces map[string]*protocol.Workspace)` 在外層組裝 prefix routing
- `POST /api/projects` 讓前端可以在 runtime 動態加專案（選資料夾後 POST 到 server），server 驗證路徑下有 `.4x/` 並同步寫入 recent-projects.json
- `DELETE /api/projects/{id}` 只從記憶體移除 workspace，不刪除 `.4x/` 目錄

### 向後相容

單一 workspace 時同時掛無 prefix（`/api/tasks`）和有 prefix（`/api/project/{id}/tasks`）兩組路由，讓舊的 native app 不用馬上改。此為過渡期設計，後續可移除。

---

## 3. 前端 — 專案選擇器 + Tab 模式

### 3a. 專案選擇器

啟動時若無專案（或按 `[+]` 新增 tab）顯示：

- **最近的專案清單**：從 `/api/projects` 拿歷史紀錄（含 recent-projects.json），點選直接開啟
- **選擇資料夾**：web 版顯示路徑輸入框 + 驗證按鈕；native app 用 `NSOpenPanel` 系統檔案選擇器
- 驗證路徑下有 `.4x/` 才允許開啟，否則顯示錯誤提示

### 3b. Tab 模式 Dashboard

- 頂部 tab bar：每個 tab 顯示專案名稱，active tab 有高亮
- 切換 tab → 前端切 API prefix，sidebar + main 區重新 load 該專案資料
- SSE 連線策略：只有 active tab 且有展開的 feature 才建 SSE 連線，切走時斷開
- `[+]` 按鈕 → 彈出專案選擇器
- Tab 可關閉：hover 顯示 `×`（只從 UI 移除，不影響 server）
- Tab 順序 + 開啟狀態存 localStorage，重新整理後恢復

### 3c. Cmd+K 搜尋 — 跨專案

- 預設搜尋所有已開啟專案的 features
- 結果按專案分組，每筆前帶標籤：`[my-app] CLI integration tests...`
- 選中結果 → 自動切換到對應專案的 tab 並展開該 feature
- 限縮範圍：輸入 `@my-app query` 只搜該專案

---

## 4. Native App

### macOS（Swift，`dashboard/macos/`）

- 啟動時 poll `/api/projects` 等 server ready，再載入 web UI
- 「選擇資料夾」觸發 `NSOpenPanel`，選完後 POST `/api/projects` 通知 server
- 標題列顯示當前 active tab 的專案名稱
- 視窗大小記憶（`UserDefaults`）

### Windows

本次只在 spec 標記為 placeholder，不實作。預期結構與 macOS 對稱（WebView2 wrapper + 系統檔案選擇器）。

### 共用 Server

Native app 和 web 連同一個 server，打同一組 API，狀態自然同步。

---

## 5. 資料持久化 — `~/.4x/`

### 目錄結構

```
~/.4x/
  recent-projects.json
```

### `recent-projects.json` 格式

```json
{
  "projects": [
    { "path": "/Users/white/github/my-app", "lastOpened": "2026-06-11T16:00:00+08:00" },
    { "path": "/Users/white/work/backend-api", "lastOpened": "2026-06-10T09:30:00+08:00" }
  ]
}
```

- LRU 順序，最近開的排前面
- 上限 20 筆，超過淘汰最舊
- 每次開啟專案時更新 `lastOpened` 並重排序
- 路徑不存在或 `.4x/` 已刪除的項目，讀取時靜默跳過（不主動清除，下次開時自然淘汰）

### 讀寫時機

| 事件 | 動作 |
|------|------|
| CLI 啟動（無引數）| 讀取，顯示選擇器 |
| CLI 啟動（有引數）| 讀取 + 把引數路徑寫入 |
| 前端 POST `/api/projects` | server 寫入 |
| 前端關閉 tab | 不動（只是 UI 移除，不從歷史刪）|

---

## 6. 遷移策略

### 移除

- 刪除 `cmd/4x/monitor.go`
- 移除 `monitor` 的 Cobra command 註冊

### 遷移

- `live.go` 吸收 monitor 的多 workspace mux 邏輯
- `server.go` 的 `NewMux(ws)` 保留為內部函式，`NewMultiMux` 在外層呼叫它組裝 prefix routing

### 不動的東西

- `internal/protocol/` — Workspace 型別不改，multi-project 在 server 層處理
- 現有 dashboard 功能（主題、auto-refresh、Cmd+, 設定）— 全部保留，包進 tab 裡
- `internal/state/`、`internal/guard/`、`internal/batch/` — 不動

---

## 影響範圍

| 檔案 | 變動 |
|------|------|
| `cmd/4x/live.go` | 重寫：多引數、--web/--app flag、互動選擇器 |
| `cmd/4x/monitor.go` | 刪除 |
| `internal/server/server.go` | 新增 `NewMultiMux`、project CRUD handler |
| `internal/server/static/index.html` | tab bar、專案選擇器、跨專案搜尋 |
| `dashboard/macos/Sources/main.swift` | NSOpenPanel、標題同步、等 server ready |
| 新增 `~/.4x/recent-projects.json` | 專案歷史持久化 |
