# F052: Feature Creation Logic Unification — Spec

## 現狀

建立 feature 的邏輯在兩處獨立實作且行為不一致：

- **CLI (`cmd/4x/new.go`)**：支援 `--subtask`、`--rule`、`--depends`、`--priority`、`--id`、`--repo`，ID 截斷邏輯完整
- **Server (`internal/server/server.go` `handlePostNew`)**：只接受 `name` + `description`，不支援 subtasks/rules/depends/priority/custom ID

額外差異：CLI 不呼叫 `InitFeatureDir()`，Server 有呼叫。未來新增 feature 欄位時要改兩處。

## 目標

1. 抽出 `internal/feature/` package，統一建立邏輯為 `feature.Create(store, opts)`
2. CLI 和 Server 都呼叫同一個函式
3. Dashboard 表單擴充為漸進式，支援 priority、depends、subtasks 等欄位
4. 把所有 feature 相關型別與函式從 `protocol` 搬到 `feature` package

## 設計

### 1. `internal/feature/` Package 結構

```
internal/feature/
├── types.go        # Feature、Subtask 型別定義
├── store.go        # Store interface 定義
├── create.go       # Create(store, opts) 統一建立邏輯
├── id.go           # GenerateFeatureID、GenerateFeatureIDFromSlug、NextNumber
├── backlog.go      # CompareBacklogMirror 及相關 drift 函式
├── screenshot.go   # DiscoverScreenshots 及相關
├── create_test.go
├── id_test.go
```

### 2. Store Interface (`store.go`)

```go
type Store interface {
    DotDir() string
    FeatureDir(featureID string) string
    RoundDir(featureID string, round int) string
    SaveFeature(f Feature) error
    LoadFeature(id string) (Feature, error)
    ListFeatures() ([]Feature, error)
    InitFeatureDir(featureID string) error
    ResolveFeatureID(prefix string) (string, error)
}
```

`protocol.Workspace` 隱式實作此 interface。`feature` package 不 import `protocol`。

### 3. CreateOpts 與 Create 函式 (`create.go`)

```go
type CreateOpts struct {
    Name        string
    Description string            // 預設 = Name
    CustomID    string            // 空 = 自動產生 + 截斷
    Subtasks    []Subtask
    Rules       []string
    Depends     []string
    Priority    *int
    Repos       map[string]string
}
```

`Create(store Store, opts CreateOpts) (Feature, error)` 流程：

1. `NextNumber(store)` 取號
2. 依 `CustomID` 是否為空選擇 `GenerateFeatureID` 或 `GenerateFeatureIDFromSlug`
3. 組 `Feature` struct（`Description` 為空時預設為 `Name`）
4. `store.SaveFeature(f)`
5. `store.InitFeatureDir(f.ID)`
6. 回傳 `Feature`

### 4. 型別搬遷

從 `protocol` 搬到 `feature`：

| 搬出內容 | 來源 | 目標 |
|---|---|---|
| `Feature`、`Subtask`、`Status` 型別及常量 | `protocol/types.go` | `feature/types.go` |
| `GenerateFeatureID`、`GenerateFeatureIDFromSlug` | `protocol/feature.go` | `feature/id.go` |
| `NextFeatureNumber` | `protocol/feature.go` | `feature/id.go` |
| `BacklogMirror`、`BacklogDriftKind`、`BacklogDrift` 型別 | `protocol/types.go` | `feature/types.go` |
| `CompareBacklogMirror`、drift 函式 | `protocol/workspace.go` | `feature/backlog.go` |
| `Screenshot`、`DefaultScreenshotDir` | `protocol/types.go` | `feature/types.go` |
| `DiscoverScreenshots` 等 | `protocol/workspace.go` | `feature/screenshot.go` |

留在 `protocol`：

- `Workspace` struct 及其 method（`SaveFeature`、`LoadFeature` 等——它是 `Store` 的實作者）
- `State`、`Event` 等狀態相關型別，`PhaseToStatus` 留在 protocol（回傳 `feature.Status`）
- `Find()`、`ReadState()`、`WriteState()`、`AppendEvent()`
- 常量（`FeaturesDir`、`RoundsDir`、`StateFile` 等）

`protocol` 會 import `feature`（因為 `Workspace.SaveFeature` 參數型別變為 `feature.Feature`），反向不依賴。

### 5. 呼叫端遷移

**CLI (`cmd/4x/new.go`)**：

現有的 ~40 行建立邏輯縮成：

```go
f, err := feature.Create(ws, feature.CreateOpts{
    Name:     name,
    Description: desc,
    CustomID: customID,
    Subtasks: subtasks,
    Rules:    rules,
    Depends:  depends,
    Priority: priority,
    Repos:    repos,
})
```

CLI flag 介面不變。

**Server (`internal/server/server.go` `handlePostNew`)**：

request body 擴充：

```go
type newRequest struct {
    Name        string            `json:"name"`
    Description string            `json:"description"`
    CustomID    string            `json:"customId,omitempty"`
    Subtasks    []feature.Subtask `json:"subtasks,omitempty"`
    Rules       []string          `json:"rules,omitempty"`
    Depends     []string          `json:"depends,omitempty"`
    Priority    *int              `json:"priority,omitempty"`
    Repos       map[string]string `json:"repos,omitempty"`
}
```

handler 改成呼叫 `feature.Create(ws, ...)`。向下相容——只傳 `name` 的舊 request 照常運作。

**其他呼叫端**：所有 import `protocol.Feature` / `protocol.Subtask` 的地方改成 `feature.Feature` / `feature.Subtask`。

### 6. Package 依賴方向

```
cmd/4x/          ──→ feature, protocol, state
internal/server/ ──→ feature, protocol
internal/feature/──→ (不 import protocol，透過 Store interface 解耦)
internal/state/  ──→ feature, protocol
internal/guard/  ──→ feature, protocol
internal/batch/  ──→ feature, protocol
protocol         ──→ feature (型別依賴)
```

無 circular dependency。

### 7. Dashboard 表單擴充

漸進式表單設計：

**基本區（直接可見）：**
- Name（必填）
- Description（選填，預設 = name）
- Priority（下拉選單：P0 Critical / P1 High / P2 Medium / P3 Low，預設不選）

**進階區（「Advanced」展開按鈕，預設收合）：**
- Custom ID（text input，placeholder「留空自動產生」）
- Depends（text input，逗號分隔 feature ID）
- Rules（text input，逗號分隔）
- Subtasks（動態列表，每行 id + name + description，可新增/移除行）

不做的事：
- Repos 欄位不放進表單（使用頻率低）
- Depends autocomplete 不做

### 8. 測試策略

**`internal/feature/` 測試：**

| 測試檔 | 涵蓋範圍 |
|---|---|
| `id_test.go` | 從 `protocol/feature_test.go` 搬過來，現有 test case 不變 |
| `create_test.go` | 用 mock Store 測試 `Create()` 完整流程 |
| `backlog_test.go` | 從現有 backlog drift 測試搬過來 |

Mock Store：in-memory 實作 `Store` interface，不碰檔案系統。

呼叫端：改 import path 後跑既有測試確認不壞。Server 測試擴充帶 subtasks/priority/depends 的 request body。

Dashboard 前端不寫自動化測試。

## 約束

- 不改 CLI flag 介面
- POST `/api/new` 的 `name` + `description` 基本格式保持向下相容
- `feature` package 不 import `protocol`（透過 Store interface 解耦）
- `protocol` 不留 `Feature` / `Subtask` type alias——一次到位
- `protocol/feature.go` 搬完後刪除（內容全部移至 `feature/id.go`）
