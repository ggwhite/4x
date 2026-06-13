# F053: Dashboard API response caching

## 現狀

多個 dashboard API 端點每次請求做全量磁碟 I/O：
- `handleTasks` — `ListFeatures()` parse 全部 YAML + 每個 feature `ReadState()` parse JSON
- `ProjectRegistry.List()` — 每個專案 `ReadConfig()` + `ListFeatures()` parse 全部 YAML
- `checkBacklogDrift` — `CompareBacklogMirror()` 內部 `ListFeatures()` parse 全部 YAML

多專案 + 多 feature 場景（5 專案 × 50 features）下，每個 API 請求觸發數百次 YAML/JSON parse。

## 設計

### WorkspaceReader interface

從 `Workspace` 現有讀取方法抽出 interface：

```go
type WorkspaceReader interface {
    ListFeatures() ([]Feature, error)
    LoadFeature(id string) (Feature, error)
    ReadState(featureID string) (State, error)
    ReadConfig() (Config, error)
}
```

`Workspace` 本身已滿足此 interface，不需改動。寫入方法不放進 interface。

### CachedWorkspace

```go
type CachedWorkspace struct {
    *Workspace
    mu            sync.RWMutex
    features      []Feature
    featuresMtime map[string]time.Time
    config        *Config
    configMtime   time.Time
}
```

mtime-based invalidation 邏輯：
- `ListFeatures()` — `os.ReadDir` features 目錄，比對每個 `.yaml` 的 mtime。全部相同回傳 cache；任一不同重新 parse 全部並更新 cache
- `ReadConfig()` — `os.Stat` settings.json 的 mtime，相同回傳 cache，不同重新 parse
- `LoadFeature(id)` — 單檔 mtime check
- `ReadState(id)` — 不 cache（頻繁變化、檔案小、parse 快）

### Server 端改動

- `NewMux` / `Start` 改成接收 `WorkspaceReader`
- 讀取用 handler 透過 `WorkspaceReader` 操作
- 寫入用 handler 透過內嵌的 `*Workspace` 操作
- `ProjectRegistry` entry 改持有 `WorkspaceReader`
- 建立 server 時用 `NewCachedWorkspace(ws)` 包裝

## 實作位置

| 動作 | 檔案 | 內容 |
|---|---|---|
| 新增 | `internal/protocol/reader.go` | `WorkspaceReader` interface |
| 新增 | `internal/protocol/cached.go` | `CachedWorkspace` struct、mtime cache 邏輯 |
| 新增 | `internal/protocol/cached_test.go` | cache 命中/失效測試 |
| 修改 | `internal/server/server.go` | 讀取 handler 改接 `WorkspaceReader` |
| 修改 | `internal/server/multi.go` | `ProjectRegistry` entry 改持有 `WorkspaceReader` |
| 修改 | `cmd/4x/live.go` | 建立 `CachedWorkspace` 傳給 server |

## 約束

- Cache opt-in：只有 long-running server 使用 `CachedWorkspace`，CLI 短命程序繼續用 `*Workspace`
- 不引入外部 cache library
- `ReadState` 不 cache
- mtime 比對用 `os.Stat` 只拿 metadata，不讀檔案內容
