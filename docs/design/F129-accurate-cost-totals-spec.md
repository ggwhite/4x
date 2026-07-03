# F129 — Accurate cost totals for CLI and dashboard

## 問題

Cost 加總在兩個地方都不準確，根因不同但都源自「沒有把 `events.jsonl` 當成唯一權威來源」：

1. **CLI**：`orchestrator.Runner.totalCostUSD`（`internal/orchestrator/orchestrator.go:52`）是記憶體變數，`NewRunner()`（`orchestrator.go:56-58`）每次從 0 開始。一個 feature 若曾經中斷重啟過（crash、手動重跑），`4x run` 結束時 `printRunSummary` 印出的總價（`cmd/4x/run.go:356-357`）只反映最後一次行程內部累加的花費，漏掉更早行程已經花的錢。
2. **Dashboard**：`internal/server/feature_handlers.go` 的 `buildPhaseInfo`（369-434 行）用共用 counter 幫「同 round 同 role」的多次執行編號當 iteration，3 個平行 deep-reviewer 併發寫 log 時 iteration 常被錯亂分配到同一個 key，導致 `handleMessages`（310 行附近寫死查 `iteration=1`）拿到空值；`mini-coder`/`re-verifier`（deep-review self-heal 迴圈用的角色）更完全不在檔名對照表（296-307 行）裡，從未被納入任何聚合。實測案例：F127 的 deep-reviewing phase 實際花 $14.14（3 個平行 reviewer + synthesizer + 2 輪 self-heal），dashboard「Deep Review」那一行只顯示 $0.8185（僅 synthesizer 一項），漏了 91%。

## 目標

1. 新增一個不依賴 per-role/iteration 比對邏輯的權威加總函式，直接把 `events.jsonl` 所有 `run-end` 事件的 `cost_usd` 加起來。
2. CLI resume 一個曾中斷過的 feature 時，用這個函式 seed 回 `totalCostUSD`，讓結束時印出的總價正確反映所有歷史行程花費。
3. Dashboard「訊息」頁頂部顯示這個權威總價，讓使用者至少有一個信得過的數字可看。

## 設計

### 共用函式：`(w *Workspace) TotalCost`

新增在 `internal/protocol/workspace_state.go`，緊鄰風格相近的既有函式 `WorktreePath`（`workspace_state.go:96-126`，同樣是「掃描整個 events.jsonl」的模式）：

```go
// TotalCost 加總 events.jsonl 中所有 run-end 事件的 cost_usd，作為該 feature
// 跨行程（含中斷重啟）的權威總花費，不依賴 per-role/iteration 比對。
// events.jsonl 不存在時回傳 (0, nil)（新 feature 尚無歷史）。
func (w *Workspace) TotalCost(featureID string) (float64, error) {
	eventsPath := filepath.Join(w.FeatureDir(featureID), EventsFile)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var total float64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type    string  `json:"type"`
			CostUSD float64 `json:"cost_usd"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "run-end" {
			total += ev.CostUSD
		}
	}
	return total, nil
}
```

`protocol.CachedWorkspace`（`internal/protocol/cached.go:31-32`）內嵌 `*Workspace`，方法自動 promote，CLI（`*protocol.Workspace`）與 dashboard（`*protocol.CachedWorkspace`）都能直接呼叫 `ws.TotalCost(featureID)`，不用寫兩份加總邏輯。

### CLI：`orchestrator.NewRunner` 建構時 seed

`internal/orchestrator/orchestrator.go:56-58`：

```go
func NewRunner(cfg Config) *Runner {
	seedCost, _ := cfg.Ws.TotalCost(cfg.Feature.ID)
	return &Runner{Config: cfg, totalCostUSD: seedCost}
}
```

新 feature（`events.jsonl` 不存在）`TotalCost` 回傳 `(0, nil)`，等同於現在的行為，不用額外判斷「是不是 resume」——一律 seed，全新 run 是無害的 no-op，中斷重啟的 run 會正確帶回歷史花費。錯誤忽略（`_`）：讀檔失敗時 seed 為 0，跟現有零值行為一致，不讓一個統計功能的錯誤擋下整個 run。

### Dashboard：`handleMessages` 回應結構改動

`internal/server/feature_handlers.go` 的 `handleMessages`（237 行起）目前在 353 行 `json.NewEncoder(w).Encode(messages)` 直接輸出**裸陣列**。改成：

```go
totalCost, _ := ws.TotalCost(featureID)
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(struct {
	Messages     []messageInfo `json:"messages"`
	TotalCostUSD float64       `json:"totalCostUSD"`
}{Messages: messages, TotalCostUSD: totalCost})
```

**這是一個 API 回應形狀的破壞性改動**（陣列 → 物件），`/api/messages/:id` 目前唯一的消費端是同一個 repo 的 `dashboard/web/ui.js`，這次一併改，不影響外部使用者（4x 沒有對外公開這支 API）。

### 前端：`dashboard/web/ui.js` 顯示總價

`loadMessages`（`ui.js:1071-1099`）目前：

```js
const msgs = await (await fetch(apiBase()+'/api/messages/'+id)).json();
const list = msgs || [];
```

改成：

```js
const data = await (await fetch(apiBase()+'/api/messages/'+id)).json();
const list = data.messages || [];
const totalCostUSD = data.totalCostUSD || 0;
```

在 `list.length === 0` 判斷之前、`el.innerHTML` 清空重繪的既有邏輯之外，新增一個總價區塊（沿用現有 `renderedMsgKeys` 增量更新的風格，總價區塊用固定 key 如 `__total__` 比對是否變動，避免每次 poll 都整個重繪）：

```js
const totalKey = '__total__';
const totalHash = totalCostUSD.toFixed(4);
if (renderedMsgKeys.get(totalKey) !== totalHash) {
	let totalEl = el.querySelector('[data-msg-key="__total__"]');
	if (!totalEl) {
		totalEl = document.createElement('div');
		totalEl.dataset.msgKey = totalKey;
		totalEl.className = 'msg-total-cost text-sm text-zinc-400 mb-2';
		el.prepend(totalEl);
	}
	totalEl.textContent = t('app.totalCost') + '：$' + totalCostUSD.toFixed(4);
	renderedMsgKeys.set(totalKey, totalHash);
}
```

新增 i18n key `app.totalCost`（各語系檔案同步加入，`make check-i18n` 驗證），文案如中文「總花費」、英文「Total cost」。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/protocol/workspace_state.go` | 新增 `(w *Workspace) TotalCost(featureID string) (float64, error)` |
| `internal/orchestrator/orchestrator.go` | `NewRunner` 建構時 seed `totalCostUSD` |
| `internal/server/feature_handlers.go` | `handleMessages` 回應結構改成 `{messages, totalCostUSD}` 物件 |
| `dashboard/web/ui.js` | `loadMessages` 適配新回應結構，新增總價顯示區塊 |
| `dashboard/web/locales/{en,es,ja,ko,zh-CN,zh-TW}.json` | 新增 `app.totalCost` key（六個語系檔案都要同步，以 `en.json` 為基準） |

## 不做的事

- 不修正 `buildPhaseInfo` 既有的 iteration key 併發 bug（`internal/server/feature_handlers.go:369-434`），「Deep Review」那一行單獨顯示的數字暫時維持現狀不準確，範圍與風險留待後續另開 feature 處理
- 不新增第二份持續寫回的累計欄位（不做 state.json 累加計數器），維持 `events.jsonl` 為單一權威來源，每次即時重新加總（events.jsonl 檔案通常不大，重新掃描成本可忽略）
- 不動 `dashboard/macos`（Swift）——確認過那只是 `WKWebView` wrapper（`dashboard/macos/Sources/main.swift:398-403`），實際 UI 都在 `dashboard/web/`
- 不處理其他 API 端點（如 `handleOverview`）是否也該顯示總價，僅限「訊息」頁
