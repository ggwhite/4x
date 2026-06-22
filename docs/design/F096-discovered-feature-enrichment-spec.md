{% raw %}
# F096 — Discovered Feature Enrichment

## 概述

auto-discover 產出的 feature 只有 Title + Description + not-started，太薄。F096 在 candidate 進 backlog 前加一步 LLM enrichment，補齊 subtasks、repos/scope、rules、priority，讓後續 Designer 有足夠資訊產出高品質 task-brief。

## 設計決策

| 決策 | 選擇 | 理由 |
|------|------|------|
| 執行時機 | 發現當下立即補強 | 避免薄 feature 汙染 backlog |
| 補強引擎 | LLM role | 推斷品質高，能從脈絡理解需求意圖 |
| 失敗處理 | 丟棄不存 | subtasks < 2 代表描述品質不足，不值得進 backlog |
| 脈絡深度 | 重量級 | feature 列表 + 目錄樹 + keyword grep 程式碼片段 |
| 自動程度 | 可設定 | `enrichAutoApprove: true`（預設）全自動；`false` 存為 `draft` 等人 approve |
| 實作方式 | 獨立 `internal/enrich` package | 可複用於 auto-discover 與 F095 miner 兩個輸入源 |

## Enrichment 契約

### 輸入

`protocol.DiscoveredFeature{Title, Description}` — 來自 auto-discover（deep-review-report.md 解析）或 F095 miner（candidates.json）。

### 輸出

完整 `feature.Feature` struct：

| 欄位 | 來源 | 必填 |
|------|------|------|
| `ID` | `feat.GenerateFeatureID` 產生 | ✅ |
| `Name` | `F{NNN}: {Title}` | ✅ |
| `Description` | LLM 可潤飾原始描述，不得捏造 | ✅ |
| `Subtasks` | LLM 推斷，≥ 2 個可獨立驗證 | ✅ |
| `Repos` | LLM 從脈絡推斷影響路徑 | ✅ |
| `Rules` | LLM 從描述萃取約束 | 選填 |
| `Priority` | LLM 推斷 1-5（1 最高） | ✅ |
| `Status` | 依 `enrichAutoApprove` → `not-started` 或 `draft` | ✅ |

### 丟棄條件

- LLM 回傳非法 JSON → 丟棄，reason: `invalid response format`
- JSON 合法但 subtasks < 2 → 丟棄，reason: `insufficient subtasks`
- Runner 執行失敗（超時等）→ 回傳 error，由呼叫端決定處理

## Package 結構

### `internal/enrich`

```go
type EnrichResult struct {
    Feature   feature.Feature
    Discarded bool
    Reason    string
}

type Enricher struct {
    ws          *protocol.Workspace
    runner      runner.Runner
    autoApprove bool
}

func New(ws *protocol.Workspace, r runner.Runner, autoApprove bool) *Enricher
func (e *Enricher) Enrich(ctx context.Context, candidate protocol.DiscoveredFeature) (*EnrichResult, error)
```

### 內部職責

| 函式 | 職責 |
|------|------|
| `collectContext()` | 收集 feature 列表 + 目錄樹 + keyword grep 結果（從 Title 拆 token，過濾 stop words，取前 5 關鍵字各 grep 10 行，合計上限 ~50 行） |
| `buildPrompt()` | 組合 candidate + 脈絡成 LLM prompt |
| `parseResponse()` | 解析 LLM 回傳的 JSON 成 Feature |
| `validate()` | 驗證 ≥ 2 subtasks、必填欄位非空 |

### Prompt Template

`templates/enrich.md.tmpl`，用 `go:embed` 嵌入：

```
你是 Feature Shaper。根據以下 candidate 描述和專案脈絡，產出完整 feature 規格。

## Candidate
標題：{{.Title}}
描述：{{.Description}}

## 專案脈絡
### 現有 Feature 列表
{{.FeatureList}}

### 目錄結構
{{.DirTree}}

### 相關程式碼片段
{{.CodeSnippets}}

## 輸出要求
以 JSON 格式回傳：
- subtasks: 至少 2 個可獨立驗證的子任務，每個有 id/name/description
- repos: 推斷的影響路徑（對應專案目錄結構中的真實路徑）
- rules: 從描述萃取的約束條件
- priority: 1-5（1 最高）
- description: 可基於原始描述潤飾補充，但不得捏造需求

不確定的欄位不要硬填，寧可少寫。
```

### LLM 回傳格式

```json
{
  "subtasks": [
    {"id": "slug-id", "name": "子任務名稱", "description": "說明"},
    {"id": "slug-id-2", "name": "第二個子任務", "description": "說明"}
  ],
  "repos": ["internal/protocol", "cmd/4x"],
  "rules": ["約束條件"],
  "priority": 3,
  "description": "潤飾後的完整描述"
}
```

## 接入點

### 接入點 1：`autoDiscoverFeatures`（run.go）

現有流程 parse → dedup → cap → SaveFeature 改為：

```
parse → dedup → cap → enrich → SaveFeature（成功）/ 丟棄（失敗）
```

```
for each candidate:
    if !settings.EnrichDiscoveredFeatures:
        SaveFeature(thin feature)  // 向後相容舊路徑
    else:
        result := enricher.Enrich(ctx, candidate)
        if result.Discarded:
            記入報告 "enrichment failed: {reason}"
        else:
            SaveFeature(result.Feature)
```

### 接入點 2：F095 miner / F099 evolve-driver（未來）

F095 產出 `candidates.json`，F099 evolve-driver 讀取後呼叫同一個 `enricher.Enrich()`。介面已備好，實作時直接接。

## 設定

`settings.json` / `protocol.Settings` 新增：

| 欄位 | 型別 | 預設 | 說明 |
|------|------|------|------|
| `enrichDiscoveredFeatures` | `bool` | `true` | 是否對 auto-discover 產出跑 enrichment |
| `enrichAutoApprove` | `bool` | `true` | `true` → `not-started`；`false` → `draft` 等人 approve |

## `draft` 狀態

### 狀態機變更

新增轉換：

```
draft → not-started   （approve）
draft → done          （reject）
```

`draft` 不會進入 `designing` 以後的流程。meta-loop 選 feature 時只撈 `not-started`，自動跳過 `draft`。

### CLI 指令

```bash
4x approve <feature-id>   # draft → not-started
4x reject <feature-id>    # draft → done
```

### 向後相容

- `enrichDiscoveredFeatures: false` → 完全走舊路徑，薄 feature 照存
- 舊的已存在薄 feature 不受影響，`4x run` 照跑
- 現有狀態機的其他轉換不變

## 測試策略

### 單元測試（`internal/enrich/`）

| 測試 | 驗證 |
|------|------|
| `TestValidate_MinSubtasks` | subtasks < 2 → Discarded=true |
| `TestValidate_Pass` | subtasks ≥ 2 且必填欄位齊全 → Discarded=false |
| `TestParseResponse_ValidJSON` | 合法 JSON → 正確解析成 Feature |
| `TestParseResponse_InvalidJSON` | 非法 JSON → Discarded |
| `TestParseResponse_MissingFields` | JSON 合法但缺必填 → Discarded |
| `TestCollectContext` | 脈絡包含 feature 列表、目錄樹、code snippets |
| `TestBuildPrompt` | template render 包含 candidate 資訊和脈絡 |

### 整合測試（`cmd/4x/`）

| 測試 | 驗證 |
|------|------|
| `TestAutoDiscover_WithEnrich` | 開啟 enrichment 時 feature 有完整欄位 |
| `TestAutoDiscover_EnrichDisabled` | 關閉時走舊路徑存薄 feature |
| `TestAutoDiscover_EnrichFail_Discarded` | enrichment 失敗時 feature 不存入 |
| `TestAutoDiscover_DraftMode` | `enrichAutoApprove=false` 時狀態為 `draft` |

LLM 呼叫用 mock runner 回傳固定 JSON，不實際呼叫 LLM。
{% endraw %}
