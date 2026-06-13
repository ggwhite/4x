# F032: Dashboard Feature Overview Tab

## 目標

在 live dashboard 的 feature detail 新增 Overview tab（放在第一個位置），讓使用者一眼看到 feature 的完整資訊：description、YAML 欄位（repos/subtasks/rules/depends）、以及對應的 spec 和 plan 設計文件。

## 動機

目前 feature detail 只有 Messages 和 Logs 兩個 tab，無法看到 feature 的描述、設計規格、實作計畫等完整資訊。使用者必須手動去翻 YAML 和 docs/design/ 目錄才能了解 feature 在做什麼。

## API

### `GET /api/overview/{featureId}`

回傳該 feature 的完整資訊。

**Response:**

```json
{
  "id": "F028-pending-review-gate",
  "name": "F028: Pending Review Gate",
  "description": "在 accepting 和 done 之間加入 pending-review 階段...",
  "status": "in-progress",
  "priority": 1,
  "repos": {"state": "internal/state/"},
  "subtasks": [
    {"id": "add-phase", "name": "新增 pending-review phase", "status": "done", "description": "..."}
  ],
  "rules": ["測試必須全過"],
  "depends": ["F001-state-tests"],
  "spec": "# Spec 完整 markdown 內容...",
  "plan": "# Plan 完整 markdown 內容...",
  "specSource": "docs/design/F028-pending-review-gate-spec.md",
  "planSource": "docs/design/F028-pending-review-gate-plan.md"
}
```

### Spec/Plan 解析順序

1. Feature YAML 有 `spec` / `plan` 欄位 → 讀取指定路徑（相對於專案根目錄）
2. 沒有 → fallback 到 `docs/design/{featureId}-spec.md` 和 `docs/design/{featureId}-plan.md`
3. 檔案不存在 → `spec` / `plan` 回傳空字串，`specSource` / `planSource` 也空

### Multi-project 路由

`GET /api/project/{projectId}/api/overview/{featureId}`

與現有 messages/events/logs 一致，由 multi.go prefix routing 處理。

## Feature YAML Schema 變更

Feature struct 新增兩個 optional 欄位：

```go
Spec string `yaml:"spec,omitempty" json:"spec,omitempty"`
Plan string `yaml:"plan,omitempty" json:"plan,omitempty"`
```

這裡的值是檔案路徑（相對於專案根），不是內容。

## 前端

### Tab 配置

- Tab 順序：**Overview** → Messages → Logs
- 點進 feature 預設顯示 Overview tab（取代原本的 Messages）

### Overview 頁面結構

```
┌─ Description ──────────────────────────────┐
│ （直接顯示 markdown 渲染結果）               │
└────────────────────────────────────────────┘

┌─ Feature Details ──────────────────────────┐
│ Priority: 1                                │
│ Repos: state → internal/state/             │
│ Depends: F001-state-tests                  │
│ Rules:                                     │
│   • 測試必須全過                             │
│   • 不改 CLI 介面                           │
└────────────────────────────────────────────┘

┌─ Subtasks ─────────────────────────────────┐
│ ✓ add-phase — 新增 pending-review phase    │
│ ○ add-button — Dashboard 加 Mark Done 按鈕 │
└────────────────────────────────────────────┘

┌─ Spec ─────────────────────────────────────┐
│ 📄 docs/design/F028-...-spec.md           │
│ ▶ （可展開，完整 markdown 渲染）            │
└────────────────────────────────────────────┘

┌─ Plan ─────────────────────────────────────┐
│ 📄 docs/design/F028-...-plan.md           │
│ ▶ （可展開，完整 markdown 渲染）            │
└────────────────────────────────────────────┘
```

### 互動規則

- **Description**：直接渲染（通常很短）
- **Feature Details**：repos / depends / rules / priority 直接顯示；欄位為空則隱藏
- **Subtasks**：帶狀態圖示（✓ done / ○ 其他），有 description 則顯示
- **Spec / Plan**：預設收合（可能 50KB+），點擊展開後用 marked.js 渲染
- 無 spec / plan 檔案時，該區塊不顯示

### 快取

前端對 overview 回應做 in-memory 快取（key = featureId），切 tab 不重複 fetch。重新點擊 sidebar 同一 feature 也用快取，除非手動 refresh（Cmd+R 或 pull-to-refresh）。

## 異動清單

| 層 | 檔案 | 變更 |
|---|---|---|
| Protocol | `internal/protocol/types.go` | Feature struct 加 Spec/Plan 欄位 |
| Server | `internal/server/server.go` | 新增 handleOverview + 路由 |
| Server | `internal/server/server_test.go` | 測試 overview endpoint |
| Multi | `internal/server/multi.go` | 加 prefix 路由 |
| Multi | `internal/server/multi_test.go` | 測試 multi overview |
| Frontend | `internal/server/static/index.html` | Overview tab + 渲染邏輯 |
| Docs | `docs/guide/dashboard.md` | 更新 API 文件 |
