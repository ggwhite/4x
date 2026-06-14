# F073: Structured Logging — Spec

## 現狀

- 整個 codebase 零 structured logging
- 35 處 `fmt.Fprintf(os.Stderr, ...)` 散落 15 個檔案，只有 warning 等級
- Server 無 request logging、無 panic 以外的 error 追蹤
- 出問題完全無跡可循

## 目標

為 4x CLI/server 加入完整的 structured logging，讓問題可追蹤、可診斷。

## 架構

### 新 package: `internal/logging`

| 檔案 | 職責 |
|---|---|
| `logger.go` | `Init()` 初始化全域 slog、level 解析、context helpers |
| `rotate.go` | 按日期輪轉的 file writer（`~/.4x/logs/4x-YYYY-MM-DD.log`） |
| `middleware.go` | HTTP request logging middleware |

### 輸出

- **File**: `~/.4x/logs/4x-YYYY-MM-DD.log`，JSON 格式（機器可解析）
- **Stderr**: text 格式，僅 warn 以上（保留現有使用者可見行為）
- 雙輸出用 `io.MultiWriter` 或 slog handler chain

### Log Level

| Level | 用途 |
|---|---|
| `debug` | 詳細追蹤：每個 state transition 的參數、config 載入細節 |
| `info` | 正常操作：server 啟動、feature run 開始/結束、batch 進度 |
| `warn` | 非致命問題：config 讀取失敗用 default、hook 失敗 |
| `error` | 需要關注：panic recover、process crash、merge 失敗 |

### 設定

`~/.4x/config.json`（user-level config）加欄位：

```json
{
  "logLevel": "info",
  "logRetainDays": 7
}
```

也支援環境變數 `FOURX_LOG_LEVEL` 覆蓋（方便 debug）。

### 輪轉與清理

- 每次 `Init()` 時檢查 `~/.4x/logs/` 下超過 `logRetainDays` 天的 `.log` 檔，自動刪除
- 不用外部 library，純 `os.ReadDir` + 日期比較

## 遷移規則

### fmt.Fprintf(os.Stderr) → slog

| 原始 pattern | 轉換為 |
|---|---|
| `fmt.Fprintf(os.Stderr, "warning: %s\n", msg)` | `slog.Warn(msg, "key", val)` |
| `fmt.Fprintf(os.Stderr, "[4x] panic in %s: %v\n", ...)` | `slog.Error("panic recovered", "method", m, "path", p, "error", rv)` |
| `fmt.Fprintf(os.Stderr, "  auto-commit failed: %v\n", err)` | `slog.Error("auto-commit failed", "error", err)` |

### 新增 log 點

**Server（middleware）：**
- 每個 request: method, path, status, duration, content-length
- SSE connect/disconnect
- Panic recovery（已有，改用 slog）

**CLI — run.go：**
- Feature run 開始/結束（info）
- 每個 phase transition（info）
- Plugin 呼叫開始/結束 + duration（info）
- Auto-commit 成功/失敗（info/error）
- Hook 執行（debug）
- Config 載入（debug）

**CLI — batch.go：**
- Batch plan/run/stop（info）
- 每個 feature 開始/完成（info）
- Auto-merge 成功/失敗（info/error）

**CLI — live.go：**
- Server 啟動 + port + pid（info）
- Project 載入（info）
- Shutdown（info）

**CLI — transition.go / done.go / merge.go：**
- State transition（info）
- Merge 操作（info）

**internal/server/：**
- Multi-project add/remove（info）
- Config read/write（debug）

## 約束

- 不引入外部 log library
- 不改 CLI stdout/stderr 的使用者可見輸出格式
- 不做 remote log shipping
- Log 檔不含敏感資訊（不 log prompt 內容、API key）
- `Init()` 失敗（如目錄建立失敗）不 block 程式啟動，fallback 到 stderr only

## 測試

- `logging` package 的 unit test：level 解析、輪轉邏輯、middleware status capture
- 不需要 E2E 測試 log 檔案內容
