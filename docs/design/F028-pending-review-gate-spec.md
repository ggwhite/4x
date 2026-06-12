# F028 — Pending Review Gate

## 問題

Feature 跑完 acceptor 後直接變 `done`，使用者沒有機會 review 產出物和 commit plan 就結束了。兩條路徑行為不一致：

- **Go runner** (`4x run`)：`accepting → done` 無條件轉換
- **Plugin** (workflow.js)：繞過狀態機直接寫 `state.json`，設 `ready-for-review` 但這不是合法 phase

Dashboard 也沒有 "等待 review" 的專屬區域，完成的 feature 混在 Pending 或直接到 Done。

## 目標

1. Feature 完成 AI loop 後停在 `pending-review` phase，等待 user 確認
2. User 透過 CLI (`4x done`) 或 Dashboard 按鈕手動推進到 `done`
3. Dashboard 有獨立的 Review 區域顯示等待確認的 feature
4. 統一 Go runner 和 Plugin 兩條路徑的行為

## 設計

### 狀態機

新增 `pending-review` phase，插入 `accepting` 和 `done` 之間：

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     └── amending ←─────────┘
any → blocked / needs-attention
```

轉換規則：

| From | To | 觸發者 |
|---|---|---|
| `accepting` | `pending-review` | AI loop 自動 |
| `pending-review` | `done` | User 手動（CLI / Dashboard） |

`pending-review` 同樣可以被轉到 `blocked` / `needs-attention`（universal target 規則不變）。

### Phase 與 Feature Status 映射

`syncFeatureStatus` 新增：

| Phase | Feature Status |
|---|---|
| `pending-review` | `ready-for-review` |

`feature.schema.json` 已有 `ready-for-review` enum，不需變更。

### Go Runner 變更

`cmd/4x/run.go`：

- `nextPhaseAfter` 的 `case PhaseAccepting` 回傳 `PhasePendingReview`（取代 `PhaseDone`）
- Loop break 條件加 `PhasePendingReview`（與 `done` / `blocked` / `needs-attention` 並列）
- 結束 switch 加 `PhasePendingReview` case：`active=false`、`stopReason="pending-review"`

### Plugin Workflow 統一

`plugins/claude-code/workflow.js` acceptor 結束後：

- 移除直接寫 `state.json` 的 `echo` 指令
- 改為在 acceptor prompt 結尾加 `4x transition <featureId> --to pending-review`
- 移除 `finalStatus` 的三元判斷——不管是測試全過或連續無進展 break，acceptor 跑完後一律進 `pending-review`，由 user 決定下一步

### CLI：`4x done`

新增 `cmd/4x/done.go`：

```
4x done <featureId>
```

行為：
1. 讀取 state，檢查 phase 是否為 `pending-review`
2. 執行 `Transition(s, PhaseDone, "")`
3. `syncFeatureStatus` 更新 feature YAML 為 `done`
4. 寫入 event `{type: "transition", phase: "done"}`
5. 印出完成訊息

若 phase 不是 `pending-review`，報錯退出。

### Server API：`POST /api/done/:id`

`internal/server/server.go` 新增 handler：

- Route：`POST /api/done/{id}`
- 邏輯同 CLI：檢查 phase → transition → sync → event
- 回傳 `200 {"status": "done"}` 或 `400 {"error": "..."}`

### Dashboard 變更

`internal/server/static/index.html`：

**classify 函式**新增 `review` 分組：

```js
function classify(tasks) {
  const g = { running: [], review: [], pending: [], todo: [], done: [] };
  (tasks||[]).forEach(t => {
    const a = t.active && t.phase && t.phase !== 'done';
    if (a) g.running.push(t);
    else if (t.status === 'ready-for-review') g.review.push(t);
    else if (t.status === 'done') g.done.push(t);
    else if (t.status === 'in-progress') g.pending.push(t);
    else g.todo.push(t);
  });
  return g;
}
```

**統計卡**：從 4 格改為 5 格（Running / Review / Pending / Todo / Done），Review 用 `amber-500` 色。

**圓餅圖**：加 review 色塊（amber）。

**Sidebar 分組**：Running → Review → Pending → Todo → Done。

**Review 區域**：每個 feature 卡片帶 "Mark Done" 按鈕，點擊呼叫 `POST /api/done/:id`，成功後 reload。

### Schema 更新

`schemas/state.schema.json` phase enum 加 `"pending-review"`。

### Guard

`internal/guard/check.go` 的 phase 白名單加 `pending-review`（如有需要）。

### PhaseToRole

`pending-review` 不對應任何 role（沒有 AI 在跑），`PhaseToRole` 回傳空字串即可（走 default case）。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/protocol/types.go` | 新增 `PhasePendingReview` 常量 |
| `internal/state/machine.go` | 轉換表加 `pending-review`；`PhaseToRole` 不需改（default 已回傳空） |
| `cmd/4x/run.go` | `nextPhaseAfter`、loop break、結束 switch |
| `cmd/4x/transition.go` | `syncFeatureStatus` 加 `pending-review` case |
| `cmd/4x/done.go` | 新增 `4x done` subcommand |
| `plugins/claude-code/workflow.js` | acceptor 結束改用 `4x transition` |
| `internal/server/server.go` | 新增 `POST /api/done/:id` handler |
| `internal/server/static/index.html` | classify、統計卡、圓餅圖、sidebar、review 區域 |
| `schemas/state.schema.json` | phase enum |

## 不做的事

- 不改 `feature.schema.json`（已有 `ready-for-review`）
- 不加 acceptor template（沿用現有 inline prompt）
- 不加自動 commit 功能（commit plan 仍由 user 自行決定）
