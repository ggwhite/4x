# Token Optimization v2 — 修正計畫

> 依據 2026-07-08 的三路分析（prompt 模板層 / orchestrator 編排層 / 711 次歷史 role 呼叫實測 ~$1,255）。
> 前作 F132 已完成：deep-review PASS 跳 fixer、review-package.md 預算 diff、per-role model 路由、UNVERIFIABLE_FROM_DIFF、acceptance-summary.md。本計畫不重複。

## 成本歸因（實測）

| 事實 | 數字 |
|---|---|
| Coder 佔總成本 | 48%（$600 / 162 次，平均 48 turns） |
| Retry 輪次（round≥2） | 27.8%（$349） |
| 每次呼叫 cache_creation | 平均 88–105K tokens（role 間不共用 cache） |
| Reviewer Bash 呼叫中探索類佔比 | 85%（無視 review-package 禁令） |

原則（Superpowers v6 驗證過的邊界）：省 token 靠**減少重複 context 搬運**，不是降級 model、不是壓縮 plan/task-brief 的資訊量（壓 plan 字數會讓測試訊號掉 62%）。

---

## Batch A — 機械性刪減（直接實作，不走 4x pipeline）

### A1. 移除 learnings 雙重注入 ⭐最高投報比

- **現狀**：`CLAUDE.md`（根目錄，約 line 156）的 `@.4x/learnings-context.md` 讓 claude runner 每次 role 呼叫都吃進 12KB 全量 learnings（54 條、不分 role）。但 `internal/prompt/prompt.go:158` 的 `LoadLearningsForRole` 已按 `roleCategoryMap`（`internal/learning/store.go:441`）篩好該 role 相關 learnings 注入模板——同內容出現兩次，role 篩選機制被繞過。
- **改法**：從根目錄 `CLAUDE.md` 移除 `@.4x/learnings-context.md` 一行。模板注入成為唯一來源。
- **注意**：互動 session 會失去自動載入的 learnings；在 CLAUDE.md 原處留一行純文字提示（如「learnings 見 .4x/learnings-context.md，需要時再讀」），不用 `@` 匯入。
- **驗證**：`4x prompt`（或等效 dump）確認 role prompt 仍含 role-filtered learnings；grep 確認 CLAUDE.md 無 `@.4x/learnings-context.md`。
- **估省**：~3k tokens × 每次 role 呼叫。

### A2. CodeMap 按 role 裁剪

- **現狀**：`internal/prompt/prompt.go:160` 對所有 role 無條件跑 `BuildCodeMap`（全庫 grep）。模板實際 render CodeMap 的只有 designer/coder/reviewer/tester；其中 tester 被模板明令「不讀 source、只驗 runtime 行為」（`templates/tester.md.tmpl:242-259`），卻拿到 symbol map（`tester.md.tmpl:45-49`）。
- **改法**：
  1. 刪除 `tester.md.tmpl:45-49` 的 Codebase Map 區塊。
  2. `prompt.go` 只對會 render 的 role（designer/coder/reviewer）呼叫 `BuildCodeMap`，其餘跳過（省 CPU/latency，非 token）。
- **驗證**：既有 prompt 測試 + 手動 dump tester prompt 確認無 CodeMap；designer/coder/reviewer prompt 不變。
- **估省**：tester 每次 ~1–1.5k tokens。

### A3. 模板 boilerplate 瘦身

- **a. Role Learnings JSON 樣板去重**：`coder:168 / deep-reviewer:292 / design-reviewer:116 / fixer:120 / reviewer:123 / tester:261` 各揹一份 ~20 行 JSON schema 教學。壓縮到 ~4 行（schema 極簡：role + category enum + content），六個模板同步改。
- **b. deep-reviewer Discovered Issues 格式去重**：`deep-reviewer.md.tmpl` 在 219-224、232-246、267-273 三處重述同一格式。保留 232-246 的獨立 MANDATORY 段，其餘只留標題引用。
- **c. tester expectedExitCode 段落壓縮**：`tester.md.tmpl:148-169`（~1.2KB，grep exit 1 冷門情境）壓成 2–3 行提示。
- **約束**：只刪重複與教學贅文，**不刪任何行為規則**（如「沒有就寫 None」的指令必須保留一份）。
- **驗證**：`make build && make test && make lint`；對照 main branch 確認既有守衛文字未誤刪（learnings 有前例：template 改動誤刪 "(if present)" 守衛）。
- **估省**：每次呼叫 ~150–400 tokens。

### Batch A 共同驗證

```bash
make build && make test && make lint
make check-docs-sync && make check-i18n
```

改 templates/ 需確認 `internal/prompt` 的 golden/snapshot 測試（若有）同步更新。

---

## Batch B — 行為變更（走 4x feature，各自獨立）

### B1. amending 輪 delta 注入（建議 profile: normal）

- **現狀**：`internal/prompt/prompt.go:167-176` 在 round>1 時把 `round-{n-1}/review-report.md` 與 `test-report.md` **全文**注入 coder prompt（`templates/coder.md.tmpl:22-31` 的 `PrevReviewReport`/`PrevTestReport`），含已 PASS checklist、格式樣板、已解決項。retry 輪佔總成本 27.8%，這是最貴路徑。
- **改法**：注入前抽取 `## Issues`（僅 FAIL/CONDITIONAL 項）+ `## Verdict` 段落 verbatim；test-report 只留 FAIL/SKIP 列。**抽取失敗（找不到 heading）時 fallback 全文**，寧可多花 token 不可漏資訊。可複用 `ParseReviewVerdict`（`internal/orchestrator/acceptance_summary.go` 已在用）。
- **AC 要點**：
  - Issues/Verdict 原文逐字保留，不改寫。
  - review PASS + test FAIL 的重試，不再注入整份已 PASS 的 review-report。
  - heading 缺失 → fallback 全文（要有測試）。
  - reviewer.md.tmpl 的 `## Issues` heading 是抽取契約，模板加註不可改名。
- **估省**：每個 amend 輪 1–3k tokens。

### B2. reviewer/tester 探索行為硬約束（建議 profile: normal）

- **現狀**：F132 的 review-package.md 機制存在，但實測 reviewer 85% 的 Bash 呼叫仍是 grep/cat/git diff/git log 自行探索——prompt 指示被無視，要用機制強制。
- **改法**（兩件事，可同 feature）：
  1. review-package.md 除 diff 外，預附「變更檔案的完整內容」（有上限，超過就列路徑），讓 reviewer 沒有自己翻檔的理由。
  2. guard/hook 層攔截 reviewer 執行 `git diff|log|show`（回覆訊息指向 review-package.md），不是硬失敗而是引導。
- **AC 要點**：攔截只針對 reviewer role；build/test 指令不受影響；review 品質不降（對照既有 feature 重跑一輪比對 verdict）。
- **估省**：reviewer 每次呼叫的探索 turns 大幅下降（目前 85% Bash 是探索）。

### B3. CONDITIONAL PASS 同輪收斂（建議 profile: normal；最大 $ 槓桿）

- **現狀**：learnings 反覆記錄「CONDITIONAL PASS 的修正項目流到下一輪才處理」，造成整輪 retry（coder $3.7 起跳/次）。retry 佔總成本 27.8%。
- **改法**：orchestrator 在 reviewing/deep-reviewing 後偵測 CONDITIONAL PASS，直接派一次輕量 coder（只給 conditional 項目清單，不給全套 context）在同輪收掉，再進下一 phase；而非把 verdict 當 PASS 放行、讓問題在 accepting 才爆開重跑。
- **AC 要點**：conditional 項目全數處理後才轉 phase；輕量 coder 的 prompt 只含 conditional 清單 + 相關檔案；狀態機轉換合法（對照 `internal/state/machine.go`）。
- **注意**：這是流程變更，需要 design review 把關狀態機影響——**必走 4x full profile 也合理**。

### B4. `4x cost` 觀測性指令（建議 profile: lite）

- **現狀**：cost 資料齊全（`.4x/run/**/*.stream.jsonl` 的 `total_cost_usd`/`usage`，`internal/runner/stream.go:25-32`；events.jsonl 的 run-end 事件，`internal/protocol/state.go:57-61`），dashboard 有 per-feature 顯示，但 CLI 沒有任何入口，跨 feature 的 per-role 歸因要手寫 script。
- **改法**：新增 `cmd/4x/cost.go`：預設彙總所有 feature 的 per-role 成本表（呼叫數/總$/平均$/佔比）、`--feature` 篩選、`--by-round` 顯示 retry 佔比、`--json` 輸出。
- **AC 要點**：對既有歷史 run 可直接跑出數字；`docs/guide/cli.md` 同步（`make check-docs` 會攔）；CLI 整合測試含 `--json`。
- **價值**：其他項目的省量驗證都靠它——**建議最先做**。

### 暫緩項（本輪不做）

- **deep-review sub-reviewer 裁 ProjectIncludes/PlanningDoc**：省量取決於 includes 大小，先用 B4 量測實際佔比再決定。
- **profile 自動選擇感知 refactor**：偏 config 而非 code（`internal/protocol/profile.go:231-243` 刻意不寫死）；先靠人工設 `feature.profile`，若 B4 數據顯示 full profile 濫用再議。
- **task-brief/report 硬性長度上限**：有品質風險（壓 plan 資訊量的失敗前例），只在 Designer/Deep-reviewer 模板加「不複述 source、只寫下游必需結論」的軟約束，可併入 A3。

---

## 執行順序

1. **Batch A**（直接實作）— 全部零風險，一次 PR。
2. **B4 `4x cost`**（lite）— 建立量測基準線。
3. **B1 delta 注入**（normal）。
4. **B2 探索硬約束**（normal）。
5. **B3 CONDITIONAL PASS 收斂**（full）— 最大槓桿但最需要把關，放最後、拿前面的省量數據佐證。

每步完成後用 `4x cost` 對比前後幾個 feature 的 per-role 成本，確認省量真實發生。
