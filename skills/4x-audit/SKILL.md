---
name: 4x-audit
description: >
  掃描 4x pipeline 過去產出的 artifacts，找出未處理的問題並分類排優先級。
  觸發：「4x audit」「audit 4x」「檢查過去問題」「跑 audit」「pipeline 健檢」。
  掃描三類來源：discovered-feature-gaps、escalation/review reports、learnings 反覆模式。
  產出分類報告（HTML artifact），可選擇批次建立 feature YAML 追蹤，
  並以 reconcile 收尾步驟清理已解決項（回標 gaps、4x clean、learn remove）。
---

# 4x Audit

掃描 4x pipeline 累積的 artifacts，以旁觀者角度找出未處理的問題。

## 掃描範圍

三路並行（用 Agent tool 各派一個 researcher subagent）：

### Source 1: Feature Gaps

檔案：`docs/reference/discovered-feature-gaps.md`（若不存在則跳過）

以下標記一律視為「已處理」（feature ID 前綴依專案而異，`F` 和 `ws` 都要吃）：

- `[已開 FXXX]` / `[已開 ws-XXX]` — 已開 feature 追蹤
- `[已直接修正]` — 已直接修掉
- `[不做]` — 評估後決定不處理
- `[延後]` — 刻意延後；仍列出但歸入報告的獨立「延後」區塊，不算未處理缺口

對無標記項目，先對照 `.4x/features/*.yaml` 的 description 內容——若已有 feature
實質覆蓋該項（常見：audit 或人工開了 feature 但忘了回標），歸入 reconcile 清單
（Step 6 補標記），不要當成未處理缺口報出來。

剩下真正未處理的項目，對每筆分類：

- **priority**：P0（金流/安全）、P1（功能不完整/資料不正確）、P2（已知限制/工具改善）、P3（文件/低影響）
- **category**：金流安全 / 功能缺口 / 資料正確性 / 工具改善 / 部署設定 / 文件

### Source 2: Escalations + Review Reports

**先讀 `.4x/features/*.yaml` 的 status，只掃 status 非 done / abandoned 的
feature 的 run 目錄**。done feature 的舊 report 是歷史紀錄不是待辦——功能已
合入，其中的 WARNING 多半早已收斂，全掃會把上線功能的舊 warning 每次重報一遍
（實測：kairos 92 個 done feature 累積 606 行 WARNING、4x repo 140 份含
WARNING 的舊 report，全是噪音）。

對入選 feature 掃描 `.4x/run/{feature-id}/rounds/*/` 下的：

- `escalation.json` — `needed: true` 且根因只是繞過（workaround）沒真正修的
- `review-report.md` / `deep-review-report.md` — WARNING / CONDITIONAL PASS 項目
- `test-report.md` — 標記為「已知限制」但應該修的

聚焦在：
- 跨 feature 重複出現的問題
- 從沒被修的 WARNING

同時回報「done/abandoned 但 run 目錄還在」的 feature 清單（`4x clean --dry-run`
可直接取得），交給 Step 6 詢問是否清理。

### Source 3: Learnings 反覆模式

檔案：`.4x/learnings.json` + `.4x/learnings-context.md`（CLAUDE.md 內 inline 版亦可）

按主題分群（排除 status=stale 的條目），找出出現 3 次以上的同類問題。
注意樣本池只增不減：判定為 systemic issue 前，先對照 `.4x/features/*.yaml`
檢查該根因是否已被後續 feature 修掉——已修的不算 systemic issue。

產出兩份：
- systemic issues：主題名、出現次數、根因、建議工具/流程改善
- 過時 learnings 的 id 清單（根因已修、或一次性環境備忘且 used_count=0）——
  交給 Step 6 詢問是否清理

## 執行步驟

### Step 1: 前置檢查

確認在有 `.4x/` 目錄的專案根目錄。沒有就提示使用者切到正確目錄。

### Step 2: 派 3 個 researcher subagent 並行掃描

每個 agent 回傳 JSON array（結構化資料）。
prompt 中明確指定：
- 掃描的檔案路徑（用絕對路徑，基於 working directory）
- 回傳格式（JSON array）
- 分類標準

### Step 3: 整合報告

等三路全部回來後：

1. 合併去重（同一問題可能出現在 gaps 和 review 兩處）
2. 按 priority 排序
3. 計算統計：total / P0 / P1 / P2 / P3
4. 分為三區塊：
   - **Systemic Issues**（learnings 反覆模式）— 修一個根因消除多條 learnings
   - **Unactioned Feature Gaps**（discovered-feature-gaps 未標已開）
   - **Unresolved Review Findings**（review/escalation 中放過的）

### Step 4: 產出 HTML Artifact

用 Artifact tool 產出互動式報告：
- 頂部統計列（total / P0~P3）
- 三區塊卡片式呈現
- 按 priority 可篩選
- 支援 light/dark theme

### Step 5: 詢問後續動作

報告產出後問使用者：

> 要批次建 feature YAML 追蹤嗎？我會把相關項目合併成幾個 feature，不會開零散的。

使用者確認後：
1. 讀 `.4x/features/` 找最新編號（前綴依專案，`F` 或 `ws`）
2. 把相關項目合併分群（按領域/repo 分，不超過 10 個 feature）
3. 建立 feature YAML（status: not-started）
4. 更新 discovered-feature-gaps.md 標記 `[已開 FXXX]`（或 `[已開 ws-XXX]`）

### Step 6: Reconcile（清理已解決項，逐類確認後執行）

audit 當下已握有「哪些項目其實已解決」的判斷，順手收掉，別留給下次 audit
重報。彙整三類清理清單，逐類向使用者確認後執行：

1. **gaps 檔漏標**（Source 1 找到的「已有 feature 覆蓋但無標記」項）
   → 補上 `[已開 FXXX]`；使用者當場決定不做/延後的項補 `[不做]` / `[延後]`
2. **done/abandoned 的 run 目錄**（Source 2 回報的清單）
   → `4x clean <feature-id>`。提醒使用者：會刪 rounds/reports/state，
   dashboard 看不到該 feature 歷史；不確定就先跳過
3. **過時 learnings**（Source 3 回報的 id 清單）
   → `4x learn remove <id>`；若 CLI 支援老化（`4x learn prune` 含
   candidate 老化）優先用 prune

每類都可以整類跳過。全部處理完在報告尾部附 reconcile 摘要
（補標 N 筆 / clean N 個 / 移除 N 條 learnings）。

## 注意事項

- 不修改任何原始碼，純讀取分析；唯一的寫入動作在 Step 5（建 feature）與
  Step 6（reconcile），且都需使用者確認
- Step 6 以外不改 `.4x/run/` 下的任何 artifact
- feature YAML 只在使用者確認後才建立
- Systemic issues 通常需要在 4x CLI repo 修，不是當前專案的 feature
