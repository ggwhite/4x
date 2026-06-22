# F097: Evolution Value & Convergence Gate — Spec

## 現狀

- L2 自動發現（`autoDiscoverFeatures`，`cmd/4x/run.go`）與 F095 history miner 會持續產出 candidate feature
- L3 meta-loop（4x 跑自己）會把 candidate 撿起來實作
- **完全沒有「這個 candidate 值不值得做」的閘門**，也沒有任何 anti-hack 機制
- 風險：backlog 無限膨脹、淨做低價值 feature、重做已完成的事（目前已有 67 張 done feature）

## 目標

在 candidate 進 backlog 之前加一道閘門：用 LLM 判斷價值、用 CLI 雙層 veto 守住反 hack 紅線，並用 golden fixtures 防 gate 漂移成橡皮圖章。

## 架構

### Pipeline 位置

```
F095 miner → candidates.json
   │
   ├─[CLI PRE-veto]  跟 done+active feature 比 Jaccard，撞的先砍（省 LLM 成本）
   │
   ├─[LLM gate role] 判倖存者價值 + 強制寫 why_not_hack → gate-verdicts.json
   │
   ├─[CLI POST-veto] 不可翻硬否決：再查重、缺論述、低分、超 cap → 強制 reject
   │
   └─ 通過者 → F096 enrich → enqueue 成 feature YAML
```

- **排序**：enrich（F096）放在 gate **之後**，不浪費補強在會被拒的 candidate 上
- 建置依賴：F096、F097 都掛 F095，可平行開發；runtime 順序由 F099 driver 串成 `veto → gate → enrich`
- **reject 永遠蓋過 accept**：LLM 說 accept 但任一 POST-veto 成立 → 仍 reject

### 新 LLM role：gate

| 項 | 內容 |
|---|---|
| **Reads** | PRE-veto 倖存的 candidates、backlog 摘要（done/active feature 名稱當脈絡）、settings 門檻 |
| **Writes** | `gate-verdicts.json`——每筆 `{title, verdict, value_score, why_not_hack, reason}` |
| **Cannot** | 直接建 feature YAML（CLI 的事）、改 candidates、翻 CLI 的 POST-veto |

`gate-verdicts.json` schema：

```json
{
  "verdicts": [
    {
      "title": "candidate 標題",
      "verdict": "accept | reject",
      "value_score": 0.0,
      "why_not_hack": "為何此 candidate 真有價值、不是為了看起來有生產力而生",
      "reason": "判斷理由"
    }
  ]
}
```

### CLI 雙層 veto（反 hack 核心）

- **PRE-veto**（LLM 前，便宜）：沿用既有 `IsSimilarFeature` Jaccard，撞 done/active 的 candidate 先砍，倖存者才進 LLM
- **POST-veto**（LLM 後，不可翻）：任一條成立 → 強制 reject
  - 重複 done/active feature（二次確認，防 LLM 漏看）
  - 缺 `why_not_hack` 論述（LLM 沒寫就拒）
  - `value_score < value_floor`
  - 超過單次接受上限（`max_accept_per_run`）
  - backlog 未做數超過上限（`max_backlog_undone`）→ 停止接受

### 職責界定（避免與 F098 重疊）

core.md 反 hack 三招的「殘酷指標」原文含「測試數下降 / scope 擴大」，但那些是 feature **實作時**的 regression，屬 **F098 self-mod scope guard / runtime guardrail** 的職責。

**F097 只管 candidate 層級的否決**（重複、無論述、低分、超 cap）。實作層 regression 不在 F097。

### Golden fixtures（holdout-as-test）

- `testdata/gate-fixtures/`：一組標好 `expected: accept | reject` 的 candidate（含明顯垃圾與明顯有價值的）
- **deterministic 部分**（CLI veto 層）→ 一般單元測試，gate 放行垃圾就紅
- **LLM gate 部分**→ 因非確定性，fixtures 當 calibration smoke check（可選 / CI 非阻斷），偵測 gate 漂移成橡皮圖章
- fixtures 內容**不進** gate runtime prompt（否則等於洩題）
- 未來 F089 learnings 若餵進 gate prompt 造成漂移，此 fixtures 是回歸防線

### settings.json 介面

```json
"evolution": {
  "value_floor": 0.6,
  "max_accept_per_run": 3,
  "max_backlog_undone": 15,
  "gate_runner": "claude-code",
  "gate_model": "",
  "dedup_threshold": 0.6
}
```

- `value_floor`：低於此分數一律拒
- `max_accept_per_run`：單次 evolve 接受上限（convergence cap）
- `max_backlog_undone`：backlog 未做數超過即停止接受新 candidate
- `gate_runner` / `gate_model`：gate role 用哪個 runner / model
- `dedup_threshold`：沿用 `similarityThreshold`（預設 0.6）

## 約束（rules）

- 任一 POST-veto 條件成立 → 整筆 reject（不用加權平均）
- 被接受的 candidate 必須有 `why_not_hack` 論述
- backlog 未做數超過 `max_backlog_undone` 時停止接受新 candidate
- golden fixtures 內容 gate runtime prompt 不可讀，僅用於驗證閘門
- gate role 不可直接建 feature YAML，建檔由 CLI 在 POST-veto 後執行

## Subtasks（對 YAML 的調整）

相對原 F097 YAML，兩處調整：

1. **cruel-metric** 縮成 candidate 層級否決（PRE/POST veto），實作層 regression 移交 F098
2. **holdout** 明確化為 golden-fixtures-as-test（deterministic 阻斷 + LLM calibration 非阻斷）

| id | 內容 |
|---|---|
| `gate-role` | 新增 gate LLM role + template + `gate-verdicts.json` schema |
| `pre-veto` | LLM 前 Jaccard 去重（沿用 `IsSimilarFeature`） |
| `post-veto` | LLM 後不可翻硬否決：重複 / 無論述 / 低分 / 超 cap |
| `why-not-hack` | 強制論述欄位，缺者拒 |
| `convergence-cap` | `max_accept_per_run` + `max_backlog_undone` 邊界 |
| `golden-fixtures` | `testdata/gate-fixtures/` + deterministic 單元測試 + LLM calibration smoke check |
| `settings-schema` | settings.json `evolution` 區段 + 預設值 + doctor 驗證 |
| `test-coverage` | 各 veto 條件、verdict 格式、cap 邊界測試 |

## 依賴

- **F095**（history-miner）：提供 candidate pool
- runtime 串接由 **F099**（evolve-driver）負責
