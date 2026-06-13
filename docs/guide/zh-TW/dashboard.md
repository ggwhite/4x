# 4x Live 儀表板

即時監控你的 AI 開發迴圈。

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

## 伺服器 API

儀表板提供 REST 和 SSE 端點：

### REST

| 端點 | 方法 | 說明 |
|---|---|---|
| `/api/tasks` | GET | 列出所有 feature |
| `/api/new` | POST | 建立新 feature |
| `/api/run` | POST | 啟動 feature 執行（產生 `4x run` 子程序） |
| `/api/stop` | POST | 停止正在執行的 feature |
| `/api/done` | POST | merge 成功後標記 feature 為完成；merge conflict/error 時維持 pending-review |
| `/api/runs` | GET | 列出活躍的執行 |
| `/api/events/{id}` | GET | 取得 feature 的事件 |
| `/api/messages/{id}` | GET | 取得 feature 的訊息 |
| `/api/logs/{id}` | GET | 列出 feature 的日誌檔 |
| `/api/logs/{id}/{file}` | GET | 取得特定日誌檔 |
| `/api/projects` | GET | 列出已註冊的專案 |
| `/api/projects` | POST | 新增專案（支援 `init: true` 以即時初始化） |
| `/api/projects` | DELETE | 移除專案 |
| `/api/browse` | GET | 資料夾選取器 |

### SSE（Server-Sent Events）

| 端點 | 說明 |
|---|---|
| `/sse/events/{id}` | 串流 feature 的事件（1 秒輪詢） |
| `/sse/logs/{id}` | 串流 feature 的最新日誌檔 |

### 多專案路由

在多專案模式下，端點前綴為 `/api/project/{project-id}/...` 和 `/sse/project/{project-id}/...`。單一專案模式使用不帶前綴的路徑以維持向後相容。

## 程序管理

儀表板管理 runner 子程序：

- 遵循專案設定中的 `max_concurrent_runs`
- 將 stdout/stderr 擷取為 run-output/run-error 事件
- 優雅關機：SIGTERM → 5 秒 → SIGKILL

## 平台

| 平台 | 狀態 |
|---|---|
| Web UI（內嵌） | 可用 |
| macOS 原生（Swift） | 規劃中 |
| Electron（Windows/Linux） | 規劃中 |
