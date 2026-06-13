# F051: Multi-Mux Route Deduplication

## 現狀

`NewMultiMux`（multi.go:231-377）幾乎複製了 `NewMux` 的所有路由，只差用 `compatGetWs` 包一層。新增路由必須在兩個地方同步維護，容易漏改。

## 需求

重構為統一的路由定義，消除 `NewMultiMux` 中的路由重複。

## 設計

### WorkspaceResolver 型別

```go
type WorkspaceResolver func(r *http.Request) (*protocol.Workspace, *ProcessManager, error)
```

`NewMux` 簽名從 `(ws *Workspace, pm *ProcessManager)` 改為 `(resolver WorkspaceResolver)`。每個 handler 在處理請求時呼叫 `resolver(r)` 取得 ws/pm。

### 兩種 resolver 實作

**singleResolver** — 單一專案模式（`4x live`）：
```go
func singleResolver(ws *protocol.Workspace, pm *ProcessManager) WorkspaceResolver {
    return func(r *http.Request) (*protocol.Workspace, *ProcessManager, error) {
        return ws, pm, nil
    }
}
```

**multiResolver** — Multi 模式（`4x live --multi`）：
```go
func multiResolver(reg *ProjectRegistry) WorkspaceResolver {
    return func(r *http.Request) (*protocol.Workspace, *ProcessManager, error) {
        // 1. URL 有 /api/project/{id}/... 或 /sse/project/{id}/...
        //    → strip prefix，從 registry 查 entry，回傳 ws/pm
        // 2. URL 無 prefix → compat 邏輯：registry 剛好 1 個就用它
        //    → 0 個回傳 error "no projects loaded"
        //    → ≥2 個回傳 error "multiple projects, use /api/project/{id}"
    }
}
```

multiResolver 內部處理 prefix strip：把 `/api/project/{id}/api/tasks` 改寫為 `/api/tasks`，讓 handler 看到的 URL path 跟單一模式完全一樣。

### NewMux 改動

- 簽名改為 `func NewMux(resolver WorkspaceResolver) http.Handler`
- 每個 handler closure 改為先 `ws, pm, err := resolver(r)`，err 時回 HTTP error
- 不依賴 pm 的路由（`/api/user-config`、`/api/supported-runners`、`/api/locales`）不呼叫 resolver，維持原行為
- `pm != nil` 的條件判斷改為：run/runs/stop/new 等路由永遠註冊，handler 內部檢查 pm 是否 nil

### NewMultiMux 簡化

改後只做：
1. 建 `multiResolver(reg)`
2. 呼叫 `NewMux(multiResolver)` 取得統一 handler
3. 在外層 mux 額外註冊全域端點：`/api/projects`（GET/POST/DELETE）、`/api/browse`
4. 把所有其他請求轉發到 `NewMux` 回傳的 handler
5. 刪除所有重複的 compat 路由（約 150 行）

### Start 函式調整

```go
func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error {
    return http.ListenAndServe(addr, NewMux(singleResolver(ws, pm)))
}
```

## 約束

- 不改 API contract（URL 結構、回應格式完全不變）
- 向後相容：單一專案時無 prefix 路由仍可用
- featureID 驗證邏輯保留在各 handler 內
- 所有 `handle*` 函式簽名不變（仍接受 `ws *Workspace` 參數）
- 現有測試必須通過（multi_test.go 的 compat 和 prefix routing 測試）
