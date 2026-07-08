# 4x-audit 噪音分析與清理機制設計（2026-07-08）

> 調查問題：4x-audit skill 找出的問題被解決後，來源資料有沒有被清掉／標記？
> 結論：三類掃描來源中只有 gaps 檔有標記機制（且靠人工、會漏），review reports 與
> learnings 完全是純追加、零清理——audit 的訊噪比隨專案推進持續惡化。
> 依據：4x 與 kairos 兩專案實測（詳下表）。

## 實測數據（2026-07-08）

| 來源 | 4x 專案 | kairos 專案 | 問題 |
|---|---|---|---|
| gaps 檔 | 5 條，1 條漏標（F145 已開卻沒回標） | 39 條，11 條無標記（多為刻意延後、無人回頭決策） | 靠人工補標會漏；缺「已決定不做」狀態 |
| run reports | 119 個 done feature 從未 `4x clean`，140 份 report 含 WARNING/CONDITIONAL PASS | 92 個 done feature、606 行 WARNING、18 條 CONDITIONAL PASS、7 個 `needed:true` 的 escalation.json 留在磁碟 | **噪音最大**——audit 不分 feature 狀態全掃，已收斂的舊 WARNING 每次重報 |
| learnings | 144 條，只有 candidate/active 兩態，stale = 0 | 511 條（357KB），70% 為永不 promote 的 candidate，46% 從未被使用 | 「出現 3+ 次＝systemic issue」的樣本池只增不減，根因修掉也照算 |

另發現 skill 本身的比對 bug：SKILL.md 原寫比對 `[已開 ws-XXX]`，但 4x 專案實際
用 `[已開 FXXX]`——前綴不相容會把所有已標記項誤判為未處理。

## 根因

「已解決」的訊號散落在三個彼此不通的地方：

1. feature YAML 的 `status: done` 不會反向傳播到 gaps 檔、run reports、learnings。
2. `4x clean` 存在但純手動、逐 feature、無人在 done 時觸發。
3. `internal/learning/store.go` 有 MarkStale/Prune，但沒有任何機制自動標 stale
   （兩專案 stale 都是 0 筆，`4x learn prune` 實質清不到東西）。

## 優化方案（三塊，各歸各的位置）

**不另寫清理 skill**——audit 跑完當下就握有「哪些已解決」的判斷 context，
另開 skill 得從零重建這個判斷，成本更高還會判錯。

### 1. 改 audit skill 本身（`skills/4x-audit/SKILL.md`，已實施）

- Source 1：標記比對同時支援 `F` / `ws` 前綴，`[已直接修正]` `[不做]` `[延後]`
  也視為已處理；無標記項要對照 `.4x/features/*.yaml` 偵測「已開但漏標」。
- Source 2：只掃 status 非 done/abandoned 的 feature 的 run 目錄——
  一刀砍掉兩專案九成以上的重複噪音。
- Source 3：判定 systemic issue 前檢查根因是否已被後續 feature 修掉；
  過時 learnings 輸出給 reconcile。
- 新增 **Step 6: Reconcile**——audit 結束時經使用者確認後順手收掉已解決項：
  回標 gaps 檔、對 done feature 跑 `4x clean`、對過時 learnings 跑
  `4x learn remove`。這些命令全都已存在，缺的只是在對的時機呼叫。

### 2. 4x CLI feature（F147-learnings-lifecycle-aging，已開）

candidate 且 used_count=0 且超過閾值天數（預設 30，settings 的
`evolution.candidate_max_idle_days` 可調）自動標 stale，讓 `4x learn prune`
真的清得到東西。kairos 那 214 條從未使用的 candidate 是這個洞的直接後果。

### 3. gaps 檔標記約定（寫進 audit skill，各專案沿用）

除既有 `[已開 FXXX]` / `[已開 ws-XXX]` / `[已直接修正]` 外，新增：

- `[不做]` — 評估後決定不處理
- `[延後]` — 刻意延後，audit 仍列出但歸入獨立區塊、不算未處理缺口

## 待決策（kairos 側）

kairos gaps 檔的 11 條無標記項需人工逐條決策標記（開 feature / 不做 / 延後），
其中至少一條是真實未修的環境 bug（docker-compose admin-scheduler 缺
`--grpc-secret`，ws-159 造成的 crash-loop 缺口）。下次在 kairos 跑
`4x audit` 時會由 reconcile 步驟帶出來。
