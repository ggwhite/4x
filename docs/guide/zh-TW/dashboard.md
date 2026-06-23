# 4x Live 儀表板

即時監控你的 AI 開發迴圈。

## macOS Gatekeeper

4x Live app 未經 Apple Developer 簽名。macOS 會在首次啟動時封鎖。

**方法 A：移除隔離屬性（推薦）**

```bash
xattr -cr /Applications/4x\ Live.app
```

**方法 B：從系統設定允許**

1. 雙擊 app — macOS 顯示「無法打開，因為無法驗證開發者」
2. 開啟**系統設定 → 隱私權與安全性**
3. 向下捲動至**安全性**區段 — 會看到被封鎖的 app 訊息
4. 點擊**仍要打開**，輸入密碼或使用 Touch ID 確認
5. macOS 會記住你的選擇，之後不再詢問

## 啟動儀表板

```bash
# 以最近的專案啟動
4x live

# 開啟指定專案
4x live /path/to/project1 /path/to/project2

# 自訂 port
4x live -p 8080

# 自動在瀏覽器中開啟
4x live -w

# 開啟 macOS 原生 app
4x live -a
```

## 多專案支援

儀表板同時支援多個專案。不帶路徑參數時，從 `~/.4x/recent-projects.json` 載入（LRU，最多 20 個項目）。

專案分頁列尾端有兩個操作：**新增專案**（資料夾加號圖示）和**全域設定**（齒輪圖示）。側邊欄標題列帶有當前專案的**專案設定**齒輪，旁邊還有一個 **Clean** 按鈕（垃圾桶圖示）。點擊 Clean 會開啟確認對話框，警告清理後的 feature 會失去儀表板中的詳細日誌、報告和輪次歷史（feature 定義和狀態保留）；確認後呼叫 [`POST /api/clean`](#post-apiclean) 清理整個專案，並以 toast 顯示結果。

## Feature 卡片

每張 feature 卡片顯示 priority、依賴、停止原因（若 feature 異常中止）的標籤，以及——當非預設的 [pipeline profile](concepts.md#pipeline-profiles) 啟用時——一個 **profile 標籤**（例如 `quick`、`normal`）。高優先 feature（P0/P1）有強調邊框。已完成的依賴顯示綠色勾號。`profile`、`stopReason` 和 `stopMessage` 欄位包含在 `/api/tasks` JSON 中。`stopReason` 是短分類碼（如 `runner-error`、`guard-fail`、`no-progress`），用於顏色標記；`stopMessage` 是顯示在分類標籤下方的人可讀詳細說明。

## 新 Feature 表單

**新 Feature** 對話框是漸進式表單。基本區域永遠顯示 **Name**（必填）、**Description**（選填，預設與名稱相同）和 **Priority** 選擇器（P0–P3 或無）。**Advanced** 展開切換顯示 **Custom ID**（留空自動產生）、**Depends**（逗號分隔的 feature ID）、**Rules**（逗號分隔）和動態的 **Subtasks** 清單（新增/移除 id + name 列）。提交時 `POST` 至 [`/api/new`](#rest)；CLI `4x new` 和儀表板現在共用同一建立路徑（`feature.Create`，見[核心概念](concepts.md#feature-creation)），因此兩者遵循相同的旗標/欄位和 ID 產生邏輯。

## 依賴 DAG

Overview 以 inline SVG 渲染所有 feature 的依賴圖——不載入外部圖表函式庫（d3、mermaid、chart.js）。Feature 依依賴深度分層排列；邊從每個 feature 連到它依賴的 feature。節點顏色依階段狀態：綠色 = done、藍色 = 執行中（活躍的 run 或 coding/reviewing/testing 等進行中階段）、灰色 = todo、紅色 = blocked / needs-attention。點擊節點開啟該 feature 的詳情，與點擊 feature 卡片的路徑相同。圖形在每次輪詢週期從快取的 `/api/tasks` 資料重建，因此顏色隨 feature 推進即時更新。

## 批次面板

Overview 還包含一個批次控制面板，背後使用[批次控制 API](#batch-control)。它顯示 **Start / Stop / Continue Batch** 按鈕（Start 在啟動前會先確認）、執行中指示器、排程佇列（每個 feature 的進度顯示為完成勾號、執行中標記或等待位置），以及——當 merge conflict 暫停批次時——一張衝突卡片，列出 feature、repo 和衝突檔案以及 Continue Batch 操作。面板從 `GET /api/batch/status` 在與儀表板其餘部分相同的輪詢迴圈中重新整理。

## 伺服器 API

儀表板提供 REST 和 SSE 端點：

讀取密集的端點（`/api/tasks`、`/api/overview`、`/api/projects`、`/api/settings` 等）透過 `*protocol.CachedWorkspace` 而非普通的 `*protocol.Workspace` 服務。因為伺服器是長期執行的，這個基於 mtime 的記憶體快取避免了每次請求都重新解析所有 feature YAML 和 `settings.json`——見[Workspace 讀取快取](concepts.md#workspace-read-cache-dashboard-server)。快取失效是自動的：寫入（透過儀表板或 runner）改變檔案 mtime，下次讀取會透明地重新解析。

### REST

| 端點 | 方法 | 說明 |
|---|---|---|
| `/api/tasks` | GET | 列出所有 feature（feature YAML 有格式問題時包含 `warnings` 陣列） |
| `/api/new` | POST | 建立新 feature（接受 `name`、`description`，以及選填的 `customId`、`priority`、`depends`、`rules`、`subtasks`） |
| `/api/run` | POST | 啟動 feature 執行（產生 `4x run` 子程序） |
| `/api/stop` | POST | 停止正在執行的 feature |
| `/api/done` | POST | 標記 feature 為完成；若有 worktree 則自動 merge（多 repo：all-or-nothing） |
| `/api/clean` | POST | 移除專案中所有可清理（done/abandoned）feature 的 workspace artifact |
| `/api/runs` | GET | 列出活躍的執行 |
| `/api/batch/start` | POST | 啟動批次執行（`4x batch run` 子程序）；若有未解決的衝突則回傳 409 |
| `/api/batch/stop` | POST | 優雅停止批次（寫入 `.4x/batch-stop`） |
| `/api/batch/continue` | POST | 清除衝突信號並重啟批次（解完 worktree 中的衝突後使用） |
| `/api/batch/status` | GET | 批次執行狀態、排程佇列、當前 feature 和衝突信號 |
| `/api/events/{id}` | GET | 取得 feature 的事件 |
| `/api/overview/{id}` | GET | 取得 feature overview（YAML 欄位 + spec/plan 內容，透過共用的 `protocol.ResolveDesignDoc` 解析——見[設計文件解析](concepts.md#design-doc-resolution)） |
| `/api/messages/{id}` | GET | 取得 feature 的訊息 |
| `/api/evolve-report` | GET | 最新 `4x evolve` 輪次摘要（`.4x/evolve-report.md`）；`{content, exists}`，不存在時 `exists:false` |
| `/api/features/{id}/screenshots` | GET | 取得依輪次分組的截圖 |
| `/api/features/{id}/screenshots/{filename}` | GET | 提供單張截圖 |
| `/api/logs/{id}` | GET | 列出 feature 的日誌檔 |
| `/api/logs/{id}/{file}` | GET | 取得特定日誌檔 |
| `/api/projects` | GET | 列出已註冊的專案 |
| `/api/projects` | POST | 新增專案（支援 `init: true` 以即時初始化） |
| `/api/projects/{id}` | DELETE | 移除專案 |
| `/api/browse` | GET | 資料夾選取器 |
| `/api/settings` | GET | 取得專案設定（`.4x/settings.json`） |
| `/api/settings` | PUT | 更新專案設定（驗證、備份、寫入） |
| `/api/user-config` | GET | 取得使用者設定（`~/.4x/settings.json`） |
| `/api/user-config` | PUT | 更新使用者設定（備份至 `.bak` 後寫入） |
| `/api/merged-config` | GET | 專案+使用者合併後的有效設定（唯讀） |
| `/api/locales` | GET | 回傳支援的 locale 清單 |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON |
| `/api/supported-runners` | GET | 列出支援的 runner 名稱 |

#### `POST /api/done` 回應

正常情況回傳 HTTP 200。`status` 欄位僅在狀態轉換成功後為 `"done"`。若發生 merge conflict 或 merge error，`status` 維持 `"pending-review"`。額外欄位指示 merge 結果：

| 欄位 | 型別 | 意義 |
|---|---|---|
| `merged` | bool | `true` 表示 branch 已 merge 且 worktree 已清理 |
| `merged` | bool | `false` 表示不存在 worktree（僅狀態轉換） |
| `merge_conflict` | bool | `true` 表示 merge 有衝突；worktree 保留 |
| `merge_error` | string | Merge 錯誤訊息；feature 維持 pending-review |
| `conflicts` | string[] | 衝突檔案清單（僅在 `merge_conflict: true` 時出現） |

衝突後，在 worktree 中解決檔案再執行 `4x merge <id>` 完成。

若 feature 的階段在 merge 期間被改變（runner 或背景 reconciler 在 merge 執行期間更新了 `state.json`），端點回傳 **HTTP 409 Conflict**，帶有 `{"status":"<currentPhase>","error":"state changed during merge"}`，且不執行 done 轉換——這防止用過時的 merge 前快照覆蓋較新的狀態。

#### `POST /api/clean`

移除專案中**每個**可清理 feature 的 `.4x/run/{feature-id}/` workspace artifact（logs、`rounds/`、報告、`state.json`、`events.jsonl`）——與 `4x clean` 清理的集合相同：`done`/`abandoned`、非活躍、有既有 workspace 目錄。Feature 定義檔（`.4x/features/*.yaml`）保留，因此清理後的 feature 仍顯示在清單中並帶有最終狀態。

非 `POST` 請求回傳 **HTTP 405**。每個 feature 獨立清理；某個失敗（例如競態使其變為活躍）會跳過但不中止其餘。handler 永遠回傳 HTTP 200：

| 欄位 | 型別 | 意義 |
|---|---|---|
| `cleaned` | int | 已移除 artifact 的 feature 數量 |
| `freed` | int64 | 釋放的總位元組數 |
| `freed_human` | string | `freed` 的人類可讀格式（例如 `38M`） |
| `features` | string[] | 已清理的 feature ID（無可清理時為 `[]`） |

無可清理時回應為 `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`。

#### 批次控制

儀表板可從頭到尾驅動批次執行，無需回到終端。專用的 `BatchManager`（與每 feature 的 `ProcessManager` 分開）管理一個專案的單一 `4x batch run` 子程序——同時只能有一個批次執行。

- **Start**（`POST /api/batch/start`）— UI 先確認以避免意外啟動，然後開始執行。若 `.4x/batch-conflict.json` 仍存在，端點回傳 **HTTP 409**，必須先解決或繼續過時的衝突。請求 body 可帶 `{runner, maxRounds}`；省略的欄位退回合併後的專案/使用者設定。
- **Stop**（`POST /api/batch/stop`）— 寫入 `.4x/batch-stop` 以優雅停止（批次完成當前 feature 後退出）。**不會** kill 子程序。
- **Continue**（`POST /api/batch/continue`）— 清除 `.4x/batch-conflict.json`，然後重啟批次。在 worktree 中解完衝突後使用。
- **Status**（`GET /api/batch/status`）— 回傳執行旗標、排程佇列、當前 feature、衝突信號（或 `null`），以及 `lastReport`（解析的 `.4x/batch-report.json`，無報告時省略）：

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  佇列由 `batch.PlanBatch` 建立，因此遵循與 CLI 相同的依賴和優先序排序。每個項目的 `state` 為 `done`（feature done / ready-for-review）、`running`（活躍的 run 且非 done）、`error`（blocked / needs-attention）或 `waiting`；`position` 為未完成項目的編號（排除 `done` 和 `error`）。

  `lastReport` 攜帶最近一次批次執行的報告（`outcome`、計數、runner、耗時和每 feature 明細——見 [Batch Mode](batch.md#run-report)）。無批次執行時，面板將其渲染為「最近批次報告」摘要卡片，可展開查看每 feature 詳情；`crashed` outcome 還會顯示 `panicMessage`。

### 截圖分頁

Feature 詳情頁在有截圖時包含**截圖**分頁。截圖依輪次分組，顯示為縮圖，可在 lightbox 中開啟，支援左右導航和 ESC 關閉。

### SSE（Server-Sent Events）

| 端點 | 說明 |
|---|---|
| `/sse/events/{id}` | 串流 feature 的事件（1 秒輪詢） |
| `/sse/logs/{id}` | 串流 feature 的活躍日誌檔（一或多個） |

事件串流追蹤 `events.jsonl` 的位元組偏移量，僅傳送新增的行。若檔案被**截斷或輪替**——例如 `4x transition --to init` 重設 feature 並從頭改寫 `events.jsonl`——新檔案大小降至追蹤偏移量以下。串流偵測到這個情況（`size < lastOffset`），將偏移量重設為 0，從頭重讀整個檔案，讓客戶端恢復而非永久停滯。大小等於偏移量仍表示「無新內容」，會被跳過。

日誌串流（`/sse/logs/{id}`）同樣追蹤位元組偏移量並僅傳送新增內容。為避免每次輪詢產生垃圾，它重複使用每個連線分配一次的固定 32KB 讀取緩衝區。每次 tick 從偏移量迴圈讀到 EOF；大於 32KB 的差量拆分為多個 SSE 訊息，每個攜帶相同的 `{"file": "...", "content": "..."}` payload。客戶端依到達順序附加內容，因此拆分是透明的。當 `size <= lastOffset`（無新內容）時，tick 被跳過而不開啟檔案。

當多個角色同時寫入日誌——平行 deep-review sub-reviewer、或同時的 reviewer + tester——串流會 tail **所有**當前活躍的日誌，而非僅最近修改的一個。不帶 `?file=` 查詢參數時，它追蹤每個 mtime 落在近期視窗內的日誌（各有自己的偏移量），每個訊息的 `file` 欄位讓客戶端將內容路由到對應的面板。傳入 `?file=<name>` 可將串流固定在單一日誌上。

### 多專案路由

在多專案模式下，端點前綴為 `/api/project/{project-id}/...` 和 `/sse/project/{project-id}/...`。單一專案模式使用不帶前綴的路徑以維持向後相容。

## 共用 Web 前端

儀表板 UI（HTML/CSS/JS + locale JSON）的單一真相源位於 `dashboard/web/`，透過 `dashboard/web/embed.go`（`web.Assets embed.FS`）嵌入 `4x` binary。Go 伺服器（`internal/server/server.go`、`internal/server/multi.go`）直接從 `web.Assets` 提供靜態資源和 locale 檔案，因此所有平台殼層使用同一前端——Go 提供的 web UI、macOS WKWebView 和 Tauri webview。沒有需要保持同步的各平台 UI 副本。

## 鍵盤快速鍵

| 快速鍵 | 動作 |
|---|---|
| `Cmd+K` | 搜尋 |
| `Cmd+,` | 專案設定（在專案中）/ 全域設定（在首頁） |
| `Cmd+Shift+,` | 全域設定 |
| `Esc` | 關閉當前對話框 |

## 程序管理

儀表板管理 runner 子程序：

- 遵循專案設定中的 `max_concurrent_runs`
- 將 stdout/stderr 擷取為 run-output/run-error 事件
- 優雅關機：SIGTERM → 5 秒 → SIGKILL

當 runner 子程序退出時，伺服器標記 feature 為非活躍（`Active=false`、`StopReason=process-exit`）。此操作防範競態：runner 可能在退出前剛寫入自己的最終 `state.json`（例如 `pending-review`）。伺服器記錄程序退出時間，在覆寫前重新讀取狀態——若 `state.json` 在退出時間**之後或同時**被更新（`UpdatedAt >= endTime`），則保留 runner 的最終狀態並跳過非活躍寫入。這防止伺服器用過時的記憶體快照回退剛寫入的階段或覆蓋其 `StopReason`。

## 平台

| 平台 | 殼層 | 打包 |
|---|---|---|
| Web UI（內嵌） | Go 伺服器提供 `web.Assets` | `4x live` |
| macOS 原生 | Swift WKWebView，自動啟動內建的 `4x live` 伺服器 | universal `.dmg`（`make package-macos`） |
| Windows | Tauri v2 webview，`4x` sidecar | `.msi`（`dashboard/tauri`） |
| Linux | Tauri v2 webview，`4x` sidecar | `.AppImage`（`dashboard/tauri`） |

所有桌面殼層透過 `http://localhost:<port>` 載入相同的 `dashboard/web/` 前端，由內嵌的 `4x` 伺服器提供。CI 矩陣在 `.github/workflows/desktop.yml` 中交叉編譯各平台的 `4x` binary，產出 `.dmg` / `.msi` / `.AppImage` artifact。
