# 核心概念

## 四個角色

| 角色 | 職責 | 輸入 | 產出 | 不可做 |
|---|---|---|---|---|
| **Designer** | 分析需求，產出 spec，定義驗收標準和測試策略 | Feature 描述、程式碼庫 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` | 修改原始碼 |
| **Coder** | 實作 spec 所述的內容 | `task-brief.md`、先前的 test/review 報告 | 原始碼、`coder-report.md` | 修改驗收標準或測試腳本 |
| **Reviewer** | 抓 bug、安全問題、spec 違規 | Diff、spec、coder 報告、專案規則 | `review-report.md` | 修改原始碼 |
| **Tester** | 根據驗收標準用證據驗證 | 驗收標準、coder 報告、測試策略 | 測試腳本、`test-report.md`、`verify.json`、`final-report.md` | 修改原始碼 |

每個角色都是**隔離的** — Coder 在實作時永遠看不到先前的 review 回饋。Tester 根據 Designer（而非 Coder）寫的標準來驗證。

### 額外的迴圈角色

兩個額外角色在迴圈後段運作：

| 角色 | 階段 | 職責 |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | 對抗式審查——在完整 diff 中找出最糟糕的 bug |
| **Acceptor** | `accepting` | 匯總仍未解決的 issue，產出 `final-report.md` 供人類 review |

Acceptor 使用自己獨立的模型設定（`roles.acceptor.model`）——與 Designer 不同。它讀取最終輪的 review/test/deep-review 報告與各輪 escalation，找出仍未解決的 issue，而不再逐份重讀每一輪的完整報告。

### Pipeline Profiles

**Pipeline profile** 選擇一個 feature 要啟用哪些角色，讓簡單工作跳過角色，而非每次都跑完整的六角色 pipeline。內建 profile：

| Profile | 角色 |
|---|---|
| `full` | designer、coder、reviewer、tester、deep-reviewer、acceptor |
| `normal` | coder、reviewer、tester、acceptor |
| `quick` | coder、reviewer |

`coder` 是必須的。設定了 `profiles` 時，profile 會依 feature 的 priority 自動選取（最高優先→`full`，然後 `normal`，然後 `quick`）；`--profile` 可覆蓋選擇。不在啟用 profile 中的角色會被跳過——迴圈沿同樣的合法狀態邊推進但不呼叫該 runner。詳見[設定](configuration.md)中的 `profiles`、`parallel_review_test` 和 `coder_model` 設定。

### Review：兩階段

1. **清單式審查**（標準模型）— 根據專案硬規則檢查：安全性、並行性、錯誤處理、風格
2. **對抗式審查**（深度模型）— 「這個 diff 裡藏著最糟糕的 bug 是什麼？」發現按嚴重程度分級。

### Deep Review 自我修復

當 Deep Reviewer 發現阻斷性問題時，`deep-reviewing` 階段會**就地修復**，而非將工作一路送回 `amending → reviewing → testing`。因為 Reviewer 和 Tester 在 deep review 前已通過，重跑整條昂貴的鏈路（尤其是 deep model）是浪費。

在同一階段內，迴圈產生兩個範圍限定的子角色，重複直到報告通過或達到上限：

| 子角色 | 模型 | 讀取 | 寫入 | 範圍 |
|---|---|---|---|---|
| **mini-coder** | coder 模型 | `deep-review-report.md` 的 `## Issues` 部分（不讀 `task-brief.md`） | 原始碼、`coder-report.md` | 僅限 deep reviewer 指出的問題 |
| **re-verifier** | reviewer 模型 | 先前的問題 + mini-coder 本次迭代的 diff | `deep-reverify-{n}.md`，更新 `deep-review-report.md` 的 `## Verdict` | 驗證舊問題已修復且新 diff 未引入 bug |

整個過程中階段維持 `deep-reviewing`——子角色不是狀態機階段。re-verifier 確認 PASS 後，迴圈推進到 `accepting`。最多迭代 `roles.deep-reviewer.max_fix_rounds` 次（預設 2）；若 mini-coder 修改了 feature 範圍外的檔案，或達到上限但仍未通過，feature 會升級到 `needs-attention` 並保留 FAIL 報告。

### 平行 Deep Review

Deep review 涵蓋 11 個不同的審查角度（正確性、品質、慣例、歷史、回饋等）。當 `roles.deep-reviewer.parallel_reviewers` 大於 1 時，迴圈會將角度分配給多個聚焦的子審查者，而非讓一個 agent 涵蓋全部 11 個。這類似 `/code-review` 按維度拆分 review 的方式，降低每個 agent 的上下文壓力和注意力漂移。

fan-out 完全由 4x CLI 驅動——不依賴 LLM 自身的 subagent 或工具能力。`deep-reviewing` 階段維持為單一階段：

| 子角色 | 模型 | 讀取 | 寫入 |
|---|---|---|---|
| **sub-reviewer**（×N） | deep 模型 (`roles.reviewer.deep_model`) | diff + 分配到的角度子集 | `deep-review-partial-{i}.md` |
| **synthesizer** | synthesizer 模型 (`roles.synthesizer.model`，預設為 `sonnet` tier) | 每份 partial report 的完整內容 | `deep-review-report.md` |

角度平均分配且不重疊：預設 `parallel_reviewers: 3` 時，群組為 `[1–4]`、`[5–8]`、`[9–11]`（正確性 / 品質+慣例 / 歷史+回饋）。設定 `roles.deep-reviewer.angles_per_reviewer` 可明確固定群組大小；不設則自動 `ceil(11/N)` 均分。N 個 sub-reviewer 平行執行，然後單一 synthesizer 去重、仲裁衝突、統一信心評分，產出與自我修復迴圈和 `parseReviewVerdict` 已消費的相同 `deep-review-report.md` 格式——下游一切不變。

當 `parallel_reviewers` 未設定或 `≤ 1` 時，迴圈退回原始的單一 agent 流程：一個 deep reviewer 處理全部 11 個角度並直接寫入 `deep-review-report.md`，無 partial report 或 synthesizer。

### 選擇性 Deep Review 角度

在派發 deep review 前，4x 分析 diff 影響的檔案路徑，並選取要執行的 11 個角度中的哪幾個。`roles.deep-reviewer` 中的 `angle_mapping` 欄位將路徑前綴（例如 `internal/state/`）和後綴模式（例如 `*_test.go`）對應到角度編號。對每個變更的檔案，最長匹配的前綴勝出（前綴規則優先於後綴規則）；所有匹配角度的聯集成為選定集合。若沒有任何檔案匹配規則，則以安全 fallback 方式執行全部 11 個角度。

選擇結果記錄在輪次目錄的 `deep-review-angles.json`，包含哪些檔案匹配了哪些規則、以及每個規則貢獻了哪些角度。此 artifact 也供 crash recovery 用來判斷正確的 partial 數量。

如需強制執行全部 11 個角度，不受 mapping 影響：
- 對 `4x run` 傳入 `--all-angles`
- 在 feature YAML 中設定 `deep_review_all_angles: true`

`angle_mapping` 可在 `settings.json` 的 `roles.deep-reviewer` 下自訂；若未設定，內建預設涵蓋標準專案佈局（`internal/state/`、`internal/protocol/`、`cmd/`、`docs/`、`templates/`、`dashboard/`、`*_test.go`）。

### Deep Review 子階段與 Crash Recovery

`deep-reviewing` 階段執行數個內部步驟（sub-reviewer → synthesizer → mini-coder → re-verifier），但它們**不是**狀態機階段。為讓即時進度和 crash recovery 知道目前執行到哪個步驟，`State` 攜帶一個 `subPhase` 欄位（`internal/protocol/state.go`），只在 `phase == deep-reviewing` 時有意義：

| `subPhase` | 步驟 | 設定時機 |
|---|---|---|
| `reviewing` | sub-reviewer（或單一 agent fallback）正在掃描 diff | 進入 deep review 時 |
| `synthesizing` | synthesizer 正在合併 partial report | synthesizer 啟動時 |
| `fixing` | mini-coder 正在修復阻塞問題 | self-heal mini-coder 啟動時 |
| `reverifying` | re-verifier 正在確認修復結果 | self-heal re-verifier 啟動時 |

`WriteState` 強制執行一個不變式：任何 `phase` 不是 `deep-reviewing` 的寫入都會將 `subPhase` 清空（`omitempty` 使其完全不出現在 `state.json`）。因此離開 deep review——到 `accepting`、`amending` 或 `needs-attention`——無論走哪條退出路徑，都不會留下過時的子階段。

發生 crash 時，`smartResumePhase` 不再從頭重啟 deep review（當 `deep-review-report.md` 不完整時）。它檢查磁碟上的 artifact 並從正確步驟恢復：

- **任何 `deep-review-partial-{i}.md` 缺失或不完整** → 從 `reviewing` 恢復；平行迴圈只重新啟動 partial 缺失的 sub-reviewer（`missingDeepPartials`），並重用每個索引原本的角度群組，不重新分配。
- **所有 partial 都存在但 report 不完整** → 從 `synthesizing` 恢復；略過 sub-reviewer，只重跑 synthesizer。
- **report 完整但為 FAIL** → 行為不變：路由到 `amending` 並清空 `subPhase`。

partial 的完整性由 `deepPartialComplete` 判斷——檔案存在、非空，且包含 deep-reviewer 模板永遠輸出的 `## Statistics` 標記區段，因此寫了一半的 partial 不會被誤判為已完成。此最小重跑 recovery 避免在已完成的步驟上重花（昂貴的）deep model 費用。

### 自動發現 Feature

Deep reviewer 經常發現問題是真實的但**超出當前 feature 範圍**——潛在 bug、技術債、缺少的功能。沒有歸屬的地方，這些筆記就埋在報告裡。當啟用 `auto_discover_features` 時，執行迴圈會自動捕獲它們。

Deep reviewer 將每個超出範圍的候選寫為 `deep-review-report.md` 的 `## Discovered Issues` 區段中的 `[NEW-FEATURE] <title>` 區塊（附簡短描述）。在**最終 deep review PASS** 後（僅有兩條到達 `accepting` 的回傳路徑——首次 PASS、以及自我修復的 re-verifier 翻轉為 PASS），迴圈解析這些區塊，完全在 CLI 層（無 LLM 呼叫）：

- **去重** — 每個候選與既有 feature 以及已保留的候選進行 Jaccard token-overlap 相似度比較。
- **設上限** — 數量上限為 `max_discovered_features`（預設 `3`）；其餘記錄為已設上限。
- **建立** — 將保留的候選建為新的 feature YAML（狀態 `not-started`，沿用 `4x new` 的編號），每次建立附加一個 `feature-discovered` 事件。
- **摘要** — 將結果（created / skipped-as-duplicate / capped）寫入 `.4x/run/{feature-id}/discovered-features.md`。

此步驟為 best-effort：任何錯誤都會記錄但不會阻擋轉換到 `accepting`。它只在最終 deep review PASS 時執行——中間輪次和 FAIL/`needs-attention` 路徑永遠不會到達它。詳見[設定 → Auto-Discover Features](configuration.md#auto-discover-features)。

### 歷史 Miner 與候選池

自動發現 Feature 只在**最終 deep review PASS** 時觸發，且只解析該單次執行 `deep-review-report.md` 的 `[NEW-FEATURE]` 區塊。最豐富的訊號——**失敗**——從未被收集：`escalation.json`、卡在 `needs-attention`/`abandoned`/`blocked` 的 feature，或跨多個 feature 反覆出現的相同 reviewer FAIL 問題。

`4x mine` 命令填補了這個缺口。它掃描**整個** `.4x/` 目錄的歷史失敗訊號，並將其彙整為 `.4x/candidates.json` 候選池。這是純 CLI/protocol 層命令——**不呼叫 LLM**，只是機械式掃描加上與自動發現 Feature 相同的 Jaccard token-overlap 去重。三個掃描器餵入候選池，每個都為每筆候選標記 `Source` 和 `Origin` 追蹤字串：

| Source | 訊號 | Origin 格式 |
|---|---|---|
| `escalation` | 每一輪中 `needed: true` 的 `escalation.json`，依 `reason` 分類（spec-mismatch / criteria-wrong / blocker / scope-change） | `<featureID> round-<n> <reason>` |
| `stuck` | `state.json` 的 phase 為 `needs-attention`、`abandoned` 或 `blocked` 的 feature；阻塞原因取自 `stopReason`/`stopMessage`，若無則 fallback 到最新輪次的 escalation `detail` | `<featureID> <phase>` |
| `fail-pattern` | 跨**不同** feature 反覆出現的 reviewer/deep-reviewer FAIL 問題標題（同一 feature 的多輪只算一次），以 Jaccard 相似度聚類，並受 `--min-occurrences`（預設 `3`）限制 | `N features: <ids>` |

反覆出現的 fail-pattern 還會產生一筆 `CandidateLearning`（類別 `review`），建議將該問題提升為 review checklist 或模板。

輸出的 `CandidatePool`（`candidates.json`）包含 `Version`、`GeneratedAt`、`Candidate` 列表和 `CandidateLearning` 列表。寫入前以三種方式去重：對照既有 feature YAML、對照前一份 `candidates.json`、以及在當前批次內部去重。旗標：`--min-occurrences`（fail-pattern 閾值）、`--output`（預設 `.4x/candidates.json`）、`--dry-run`（只印摘要不寫入）。

整個命令為盡力而為——單一損壞的 feature 只記錄並跳過，絕不中止掃描。`4x mine` **只產生候選池；它從不建立 feature**。候選是否升級為真正的 feature 交由獨立的 gate（F097）決定。這使它與自動發現 Feature 互補而非取代：一個在成功時收集範圍內的筆記，另一個收集整個歷史的失敗訊號。

### Evolve Driver

`4x evolve` 將 mine、F097 value gate 與 enrichment 串成一條可重複執行的閉迴路：**mine → gate (pre → gate LLM 角色 → post) → enrich → enqueue → (可選) auto-run → learnings 回饋下一輪**。CLI 層維持不碰 LLM——gate 角色與 enrichment 都以 `runner.Runner` 子程序執行，絕非內嵌 API 呼叫。

Pipeline 順序為 **mine → gate → enrich → enqueue**（而非 mine → enrich → gate）：gate 消費的是未加工的 `Candidate`，因此 enrichment——將候選具現化為完整 `feature.Feature`——只在 gate 存活者上執行，絕不浪費 LLM 成本在被否決的候選上。通過的候選排入為 `not-started` feature YAML（通過 value gate **即為**核准；沒有第二道 draft→not-started 步驟）。若 enrichment 失敗或被捨棄，候選仍以其描述文字建立的基本 feature 排入，標記 `enriched=false`——gate 已為其價值背書。

每次呼叫恰好執行**一輪**；重複輪次由外部驅動（cron 或重複呼叫）。每輪寫入 `.4x/evolve-report.md`（Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted），由 dashboard 透過 `GET /api/evolve-report` 呈現。

**反空轉中止**防止迴路在毫無產出時無限運轉。`.4x/evolve-state.json` 跨呼叫持久化 `consecutiveNoAccept`；未接受任何候選的一輪遞增它，接受任何候選的一輪重置為零。達到 `evolution.max_idle_rounds` 時，下次呼叫在 mining 前中止，標記報告為 `Halted`，以 exit 0 結束。此設定區分**未設定**（`nil` → 預設 `3`）與明確設為 `<= 0`（停用中止——永遠執行）；`--force` 可覆寫單次中止。

使用 `--auto-run` 時，每個排入 feature 的 meta-loop 立即執行，始終在 F098 self-mod scope guard 下運作：觸及 `self_mod_guard.protected_paths` 且未獲核准的 feature 不會被自動完成，而是在報告中標記 `SelfModBlocked`（以 `4x done --approve-self-mod` 解除）。`--dry-run` 為唯讀——印出 mine/dedupe 摘要、不寫入任何內容、不啟動 runner、不建立 feature（且在有 `--auto-run` 時以警告忽略）。

### Escalation

Coder 或 Tester 可以在以下情況時 escalate：

| 原因 | 意義 | 路由到 |
|---|---|---|
| `spec-mismatch` | DB/API 與 spec 不符 | Designer |
| `criteria-wrong` | 驗收標準不正確 | Designer |
| `blocker` | 缺少依賴或基礎設施問題 | `needs-attention`（人工介入） |
| `scope-change` | 需要修改範圍外的 repo | Designer |

Escalation 會寫入 `escalation.json`。迴圈會自動將 `spec-mismatch`、`criteria-wrong` 和 `scope-change` 路由回 Designer。`blocker` escalation 則進入 `needs-attention` 等待人工介入。

---

## 狀態機

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
                     ↑      ↓
                     └──────┘
```

### 所有合法轉換

| 從 | 到 |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`、`designing` |
| `reviewing` | `testing`、`amending` |
| `amending` | `reviewing`、`designing` |
| `testing` | `deep-reviewing`、`amending`、`designing` |
| `deep-reviewing` | `accepting`、`amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`、`coding`、`testing` |
| `needs-attention` | `designing`、`coding`、`testing` |
| any | `blocked`、`needs-attention`、`done`、`abandoned` |

### 輪次計數器

- 在 round 為 0 時進入 `coding` 會將 round 設為 1
- 進入 `amending` 會遞增 round
- 當 round >= maxRounds 或連續 3 輪以上無進展時，`ShouldStop` 會觸發

### 迴圈中的階段決策

| 階段 | 條件 | 動作 |
|---|---|---|
| `designing` | `task-brief.md` 或 `acceptance-criteria.md` 遺失 | → `needs-attention` |
| `coding` / `amending` | `escalation.json` 含 `spec-mismatch`、`criteria-wrong` 或 `scope-change` | → `designing` |
| `reviewing` | Review 未通過（需要明確的 `PASS` 或 `CONDITIONAL PASS` verdict 且報告中零 `[CRITICAL]`/`[WARNING]` 問題） | → `amending` |
| `testing` | `verify.json` 未通過或缺少 artifact | → `amending` |
| `deep-reviewing` | Deep review FAIL | 就地自我修復（mini-coder + re-verifier），最多 `max_fix_rounds` 次；PASS → `accepting`，否則 → `needs-attention` |
| any（非 designer） | Guard 檢查發現 scope 違規、baseline drift 或缺少必要檔案 | → `needs-attention` |

---

## 檔案協定

角色透過 `.4x/` 目錄通訊，而非共享的上下文視窗。

```
.4x/
├── settings.json                    # 專案設定
├── plugins/                         # Runner 指令檔
├── batch-plan.json                  # 批次執行計畫
├── batch-stop                       # 優雅停止信號
├── batch-pid                        # 執行中批次子程序的 PID（伺服器孤立程序認領用）
├── batch-conflict.json              # 批次自動 merge conflict 信號（暫停狀態）
├── batch-report.json                # 最近一次批次執行報告（統計 + 每 feature 結果）
├── features/
│   └── {id}.yaml                    # Feature 定義（正式來源）
└── run/                            # 執行期產物（每個 feature 的工作目錄）
    └── {feature-id}/
        ├── state.json                   # 階段、角色、輪次、是否活躍、runner、runners、停止原因、profile
        ├── events.jsonl                 # 審計軌跡
        ├── baseline.json                # 編碼前快照（HEAD、branch、dirty 檔案）
        ├── task-brief.md                # Designer → Coder：spec + 架構
        ├── acceptance-criteria.md       # Designer → Tester：可測試的標準
        ├── test-strategy.yaml           # Designer → Tester：測試方法
        ├── final-report.md              # 迴圈結束摘要
        ├── logs/
        │   ├── round-{N}-{role}.log              # 每輪每角色的執行日誌
        │   ├── round-{N}-deep-reviewer-{i}.log   # 每個平行 sub-reviewer 的日誌（fan-out 時）
        │   └── round-{N}-synthesizer.log         # synthesizer 合併 partial report 的日誌
        └── rounds/round-{N}/
            ├── coder-report.md            # Coder 做了什麼
            ├── review-report.md           # Reviewer 的發現 + 裁決
            ├── test-report.md             # Tester 的結果
            ├── deep-review-partial-{i}.md # 單個平行 sub-reviewer 的發現（fan-out 時）
            ├── deep-review-report.md      # 合併後的 deep review（synthesizer 輸出或單一 agent）
            ├── verify.json                # {passed, round, role, commands[]}
            └── escalation.json            # {needed, reason, detail}
```

### 批次信號檔

兩個頂層信號檔協調執行中的批次與外部觀察者（CLI 和儀表板）：

- **`batch-stop`** — 空的標記檔。`4x batch run` 在 feature 之間輪詢它，存在時優雅停止（見 [Batch Mode](batch.md)）。
- **`batch-conflict.json`** — 當批次自動 merge 碰到 merge conflict 並暫停時寫入。攜帶足夠資訊讓儀表板渲染衝突而無需重跑 git：

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  monorepo 模式下 `conflictRepo` 為空。此檔案在每次批次執行開始時和使用者繼續暫停的批次時被清除。

- **`batch-report.json`** — 批次執行結束時寫入（正常完成、停止、中斷或 crash）。不同於上述兩個信號檔，它在執行之間持續存在，作為儀表板在無批次活躍時顯示的「最近批次報告」。記錄 `outcome`、總計數（`total` / `completed` / `failed` / `remaining`）、runner、總耗時、以及每 feature 明細（最終狀態、輪次、停止原因）；`crashed` outcome 還攜帶 `panicMessage`。以原子方式寫入（暫存檔 + rename），儀表板不會讀到寫了一半的報告。

### 原子狀態寫入

`state.json` 由多個角色並行讀寫——執行迴圈、儀表板伺服器和背景 reconciler。為避免讀取者看到截斷或寫了一半的檔案，`WriteState` 不會原地寫入。它 marshal 狀態後，寫到同目錄的暫存檔（`.state-*.json`，保證在同一檔案系統使 rename 為原子操作），然後 `os.Rename` 覆蓋 `state.json`。讀取者因此永遠看到完整的舊檔案或完整的新檔案——不會看到部分寫入。寫入失敗時暫存檔會被移除，不會累積 `.state-*.json` 殘留。不使用檔案鎖；正確性來自原子 rename 加 `UpdatedAt` 比較。

### Worktree 路徑復原

當 feature 在 worktree 隔離環境中執行時，迴圈在啟動時印出 `worktree: <path>`，並以 `run-output` 事件記錄至 `events.jsonl`。`Workspace.WorktreePath` 在後續（例如截圖探索）透過掃描審計軌跡來復原該路徑，而非重新執行 git。

掃描會讀取**整個** `events.jsonl`，並從**最後一筆**匹配的 `run-output` 事件取得路徑。這對於重跑很重要：每次 `4x run` 都會附加一個新的 `worktree: …` 事件，因此檔案會在 feature 的生命週期中累積多筆。只讀前幾行要麼在事件累積後找不到路徑，要麼回傳一個已移除的舊 worktree。取最後一筆匹配永遠回傳最近一次執行的 worktree。

### Workspace 讀取快取（儀表板伺服器）

CLI 是短命程序：每個命令讀取它需要的 `.4x/` 檔案一次後退出，因此總是使用普通的 `*protocol.Workspace`。儀表板伺服器（`4x live`）相反——它是長期執行的，每個 API 請求都重新讀取相同檔案。在多專案×多 feature 的 workspace 中（例如 5 個專案×50 個 feature），單一請求可觸發數百次 YAML/JSON 解析。

為避免此問題，伺服器將每個 workspace 包裝在 `*protocol.CachedWorkspace`（`internal/protocol/cached.go`）中，這是一個基於 mtime 的記憶體快取，覆蓋 `WorkspaceReader` 介面（`internal/protocol/reader.go`）宣告的唯讀操作：

- **`ReadConfig`** — 快取 `settings.json`；`os.Stat` 比對檔案 mtime，僅在變更時重新解析。
- **`ListFeatures`** — 快取完整的 feature 清單；`os.ReadDir` 比對 `.yaml` 檔案集合和每個檔案的 mtime，僅在檔案新增、移除或修改時重新解析。回傳副本供呼叫者自由修改。使用寬鬆驗證：格式有問題的 feature（如 subtask status 不合法）仍會列出並附帶 `Warnings`，而非靜默跳過。
- **`LoadFeature`** — 依 id 快取每個 feature，以 YAML 的 mtime 為 key。使用嚴格驗證——任何格式問題都會回傳 error。
- **`ReadState`** — 刻意**不快取**（變更頻繁、檔案小、解析快）；直接透過內嵌的 `*Workspace`。

失效是隱式的：寫入方法（`SaveFeature`、`WriteState` 等）不需要通知快取，因為下次讀取會偵測到新的 mtime。快取為 opt-in——僅伺服器建構 `CachedWorkspace`；CLI 繼續使用 `*Workspace`，行為相同。

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # 數字：0-1 = full profile、2 = normal、3+ = quick（省略為 nil/unset）
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # 選用的設計 spec 明確路徑（覆蓋 docs/design/ 查找）
plan: ""     # 選用的實作計畫明確路徑
hooks: {}    # 選用的 phase hooks（格式與 settings.json 相同）
```

`status` 反映 `state.json` 的階段以便快速列表。合法值：`not-started`、`in-progress`、`ready-for-review`、`needs-attention`、`blocked`、`done`、`abandoned`。`abandoned` 的 feature 視為已完成（不會阻擋依賴），但在儀表板以刪除線顯示。`depends` 列出必須先 done（或 abandoned）的 feature ID。`repos` 列出此 feature 涉及的 repository 名稱（來自 `workspace.repos`）；空表示所有 repo 都在範圍內。

#### 設計文件解析

儀表板 overview 和 `4x prompt` 的規劃文件注入透過一個共用解析器（`protocol.ResolveDesignDoc`）定位 feature 的 spec/plan，因此兩者永遠看到同一份文件。每種文件類型（`spec`/`plan`）的解析順序：

1. feature YAML 的 `spec`/`plan` 欄位，非空時作為路徑讀取（相對路徑以 workspace 根目錄為基準）。
2. `docs/design/{feature.ID}-{type}.md`。
3. `docs/design/{slug}-{type}.md`，其中 `slug` 去掉 ID 的 `FNNN-` 前綴（僅在與 ID 不同時嘗試）。

第一個存在的檔案勝出；都沒有則視為文件不存在。

### Feature 建立

`Feature`/`Subtask`/`Status` 型別和建立邏輯位於獨立的 `internal/feature` package（ID 產生、backlog drift、截圖輔助也搬到那裡）。`protocol.Workspace` 和 `protocol.CachedWorkspace` 滿足 `feature.Store` 介面，而 `feature` 不 import `protocol`（單向依賴，透過 `Store` 解耦）。CLI（`4x new`）和儀表板（`POST /api/new`）都透過單一的 `feature.Create(store, opts)` 入口點建立 feature，因此編號、ID 截斷和預設欄位的行為無論從哪個入口都相同。

### Workspace 設定（多 Repo）

預設情況下，4x 以 monorepo 模式運作。若要跨多個 repository 工作，在 `.4x/settings.json` 中宣告：

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

每個項目將 repo 名稱映射到路徑（相對於 workspace 根目錄）和選用的 `hub` 旗標。Hub repo 是多個 feature 可能觸及的共用基礎設施——它們在 `4x batch plan` 的範圍群組化中被排除。

monorepo 模式下（無 `workspace.repos`），所有範圍檢查和 git 操作使用單一 repo 根目錄。

---

## Guardrail

由 CLI 強制執行的確定性檢查 — 不依賴 AI 判斷。

| Guardrail | 功能 |
|---|---|
| **必要檔案** | 驗證階段對應的 artifact 是否存在（例如 designing 後的 `task-brief.md`） |
| **基線** | 擷取編碼前的狀態（HEAD、branch、dirty 檔案）；如有 dirty 檔案則警告 |
| **範圍** | monorepo 模式：比對 `git diff --name-only HEAD` 的頂層目錄與 feature 宣告的 repo。多 repo 模式：使用 `gitops.Ops.DetectChangedRepos()` 跨所有 workspace repo 檢查 |
| **依賴** | 如果被依賴的 feature 未完成，則阻擋 `4x run` |
| **Backlog drift** | 當 `.4x/features/*.yaml` 與外部映射不同步時警告 |
| **Build gate** | 在 coding/amending 階段：執行 `settings.json` 的 build + lint 命令，寫入 `build-gate.json`。失敗會阻擋此輪；Coder agent 應修復並重跑 `4x check` |
| **Testing → Accepting 閘門** | 需要 `verify.json`（passed=true）、`test-report.md`、`final-report.md`。若 `test-strategy.yaml` 定義了 `manual_checks`，每項都必須在 `manual_check_results` 中有對應的非空 evidence 條目 |
| **Self-mod guard** | 疊加在 Scope 之上（不取代它）：標記受保護路徑（預設 `internal/state/`、`internal/guard/`、`internal/protocol/`）的檔案級變更，當每輪受保護的 diff 超過預算時阻擋，要求在 accepting 前附帶測試，並在手動核准前阻擋自動 merge |

可用 `4x check <feature-id>` 手動執行。

### Self-mod guard

當 4x 在自身上執行（meta-loop）時，對其核心基礎（狀態機 / guardrail / protocol）的變更比一般 feature 工作風險更高——那裡的 regression 會破壞整個多角色迴圈。self-mod guard 在 repo 層級的 Scope guard 之上新增一個額外層，在 `settings.json` 的 `self_mod_guard` 下設定：

```json
"self_mod_guard": {
  "protected_paths": ["internal/state/", "internal/guard/", "internal/protocol/"],
  "max_diff_lines": 200,
  "require_tests": true
}
```

- `protected_paths` — 路徑前綴許可清單（相對於 scope 根目錄）；這些路徑下的變更會被標記。未設定時預設為三條架構紅線。
- `max_diff_lines` — 每輪受保護 diff 的預算；超過則 guard 失敗，feature 降至 `needs-attention`。預設 `200`。
- `require_tests` — 為 `true`（預設）時，受保護的 `.go` 變更必須在 feature 離開 `testing` 前附帶受保護的 `_test.go` 變更。

觸碰行為在 coding 後 guard 檢查時偵測一次並持久化到 `state.json`（`selfModTouched` / `selfModPaths`）。觸碰受保護路徑永遠不會自動 merge：`4x done` / `4x merge` 會阻擋，直到你以 `--approve-self-mod` 重跑，這會在 state 中記錄 `selfModApproved`。

---

## Phase Hooks

Phase hooks 讓你在階段轉換前後自動執行 shell 命令——適合啟動 Docker 容器、初始化測試資料庫、或在測試後清理。Hooks 由 CLI 執行，不由任何 AI 角色執行。

### 設定

Hooks 宣告在 `settings.json` 的 `hooks` key 下。Key 格式為 `pre_{phase}` 或 `post_{phase}`：

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

每個項目是一個 `HookEntry`，有兩個欄位：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `run` | string | 透過 `sh -c` 執行的 shell 命令 |
| `on_fail` | string | `"block"`（預設）或 `"warn"`（不分大小寫） |

Feature YAML 也可以宣告同格式的 `hooks` 欄位。當 feature 與全域設定為同一 key 定義了 hooks 時，feature 的定義會**完全取代**全域的（同一 key 內不合併）。

### 執行順序

```
pre_{target_phase} hooks（依陣列順序）
  ↓ 任何 on_fail=block 的 hook 失敗 → 轉為 needs-attention，中止
state.Transition()
  ↓
記錄轉換事件
  ↓
post_{target_phase} hooks（依陣列順序）
  ↓ on_fail=block 的 hook 失敗 → 轉為 needs-attention（不回滾）
```

### 失敗行為

| `on_fail` | Hook 失敗位置 | 效果 |
|---|---|---|
| `block`（預設） | pre hook | Feature 移至 `needs-attention`；階段轉換中止 |
| `block`（預設） | post hook | 階段已變更；feature 移至 `needs-attention` |
| `warn` | 任一 | 結果記錄；繼續執行 |

### 日誌

每次 hook 執行會在 `events.jsonl` 追加一個 `type: "hook"` 事件：

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

完整的 stdout/stderr 輸出寫入 `.4x/run/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`。

### Hook 合併（`MergeHooks`）

全域和 feature 的 hooks 由 `MergeHooks` 合併：所有全域 key 複製過來，然後 feature 的 key 完全覆蓋同名的全域 key。僅存在於全域的 key 保留。兩者皆為 nil 時回傳 nil。

---

## Health Check

Tester 角色啟動前，CLI 可自動驗證環境健康——確認建置通過、服務正常運行、端點有回應。在此處抓到損壞的環境，可省下一整個浪費的測試週期。Health check 由 CLI 執行，不由任何 AI 角色執行，且僅在進入 `testing` 階段時執行，在 `pre_testing` hooks 之後、Tester runner 啟動之前。

### 設定

Health check 有三個欄位（`internal/protocol/verify.go` 中的 `HealthCheck`）：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `commands` | `[]string` | 依序執行的檢查命令；任何失敗會停止執行 |
| `recovery` | `[]string` | 選用。檢查失敗時依序執行，用於修復環境 |
| `timeout` | `int` | 每命令逾時秒數；`<= 0` 時套用預設值 `30` |

可在 `settings.json` 中全域宣告（JSON，無 yaml tag）：

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

也可在 `test-strategy.yaml` 中依 feature 宣告（透過 `Workspace.ReadTestStrategy` 讀取）：

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**合併：** `ResolveHealthCheck` 做整組覆蓋，不做欄位級合併。若 `test-strategy.yaml` 定義了 `health_check`，它完全取代全域的；否則使用全域設定。兩者都未設定時，跳過 health check，Tester 直接啟動。

### 執行流程

```
進入 testing 階段（pre_testing hooks 已執行）
  ↓
依序執行 commands（每個有各自的 timeout）
  ├─ 全部通過 → 啟動 Tester
  └─ 任一失敗 →
      ├─ 無 recovery → 升級到 needs-attention
      └─ 有 recovery → 依序執行 recovery 命令
          ├─ recovery 失敗 → 升級到 needs-attention
          └─ recovery 通過 → 重新執行所有 commands 一次
              ├─ 通過 → 啟動 Tester
              └─ 仍失敗 → 升級到 needs-attention
```

Recovery 最多觸發一次——沒有多次重試或退避機制。

### 失敗行為

最終失敗時，執行記錄一個 `type: "health-check-failed"` 事件（角色 `tester`，含失敗命令和錯誤於 `detail`），將 feature 轉為 `needs-attention`，設定 `StopReason` 為 `health-check-failed`，並停止迴圈。每個命令透過 `sh -c` 在各自的逾時下執行；逾時視為失敗，其輸出寫入 stderr 以便除錯。

---

## Test Profiles

**Test profile** 是一組可重用的測試方法論，由 Designer 標記在 feature 上，讓 Tester 的 prompt 自動注入對應的指引——而非在 `settings.json` 的 `roles.tester.instructions` 中手動維護一個所有 feature 共用的巨型清單。

> 不要與 **[pipeline profiles](#pipeline-profiles)**（`Config.Profiles`）搞混，後者選擇*哪些角色執行*。Test profiles（`Config.TestProfiles`）僅向 Tester prompt 注入*測試方法論內容*。

### 宣告 profile

Designer 在 `test-strategy.yaml` 中列出 profiles（`internal/protocol/verify.go` 的 `TestStrategy.Profiles`）：

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` 為 `omitempty`——沒有它的 `test-strategy.yaml` 行為與以前完全相同（不注入）。

### 手動檢查

針對需要超出 build/test/lint 範圍的執行期驗證的 AC 項目，Designer 可在 `test-strategy.yaml` 中新增 `manual_checks`（`internal/protocol/verify.go` 的 `TestStrategy.ManualChecks`）：

```yaml
manual_checks:
  - id: mc-1
    ac_ref: AC-3
    description: "驗證 routing 正確分流"
    steps:
      - "啟動 server: go run ./cmd/gate --port 8080"
      - "curl http://localhost:8080/health → 確認 200"
  - id: mc-2
    ac_ref: AC-5
    description: "驗證 graceful shutdown"
    steps:
      - "啟動 server 並送 SIGTERM"
      - "確認 exit code 為 0"
```

Tester 必須執行每個步驟，並在 `verify.json` 的 `manual_check_results` 下記錄實際輸出作為證據（`VerifyEvidence.ManualCheckResults`）。若任何手動檢查無結果或證據為空，guard 會阻擋 `testing → accepting`。若失敗可重試，tester 會透過 `guard-feedback.json` 注入 guard 錯誤獲得一次自動重試；第二次失敗則升級至 `needs-attention`。

### 內建 profiles

四個 profile 內嵌在 binary 中（`templates/profiles/*.md`，透過 `templates.ProfilesFS` 暴露）：

| Profile | 方法論 |
|---|---|
| `unit` | Go `go test`、`t.TempDir()` 隔離、table-driven、錯誤案例、每個 AC 一份 verify.json |
| `web` | 針對 `4x live` 儀表板的 Playwright 測試；headless、獨立 workspace + 隨機 port、截圖作為證據、不干擾使用者正在執行的伺服器 |
| `api` | HTTP 端點測試——狀態碼、回應 body、邊界案例、認證 |
| `e2e` | 端到端多服務流程、DB 狀態和跨服務一致性 |

### 在 settings.json 中覆蓋

專案可透過 `Config.TestProfiles`（`test_profiles`）取代或擴展任何 profile，以 profile 名稱為 key（`TestProfileOverride`）：

```json
{
  "test_profiles": {
    "web": { "content": "用 Cypress 而非 Playwright 測試..." },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` — 行內替換文字
- `include` — 路徑（相對於 workspace 根目錄），讀取該檔案的內容

**解析順序**（每個 profile 名稱）：`test_profiles[name].content` → `test_profiles[name].include` → 內建 `profiles/{name}.md`。覆蓋是整體替換，不做欄位級合併。未知名稱（無覆蓋、無內建）會印出 stderr 警告並跳過。

Tester prompt 將每個解析出的 profile 渲染為 `== Test Profile: {name} ==` 區塊。載入由 `loadProfiles` / `resolveProfileContent`（`cmd/4x/prompt.go`）實作。

---

## Pending Review 閘門

迴圈**不會**直接進入 `done`。接受後，feature 進入 `pending-review` — 等待人類審查 AI 的工作。

```
... → accepting → pending-review → （人類審查）→ 4x done F001
```

這確保在 feature 被視為完成之前，人類一定會簽核。
