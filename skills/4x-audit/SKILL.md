---
name: 4x-audit
description: >
  掃描 4x pipeline 過去產出的 artifacts，找出未處理的問題並分類排優先級。
  觸發：「4x audit」「audit 4x」「檢查過去問題」「跑 audit」「pipeline 健檢」。
  掃描三類來源：discovered-feature-gaps、escalation/review reports、learnings 反覆模式。
  產出分類報告（HTML artifact），可選擇批次建立 feature YAML 追蹤。
---

# 4x Audit

掃描 4x pipeline 累積的 artifacts，以旁觀者角度找出未處理的問題。

## 掃描範圍

三路並行（用 Agent tool 各派一個 researcher subagent）：

### Source 1: Feature Gaps

檔案：`docs/reference/discovered-feature-gaps.md`（若不存在則跳過）

找出所有「沒有 `[已開 ws-XXX]` 標記」的項目，對每筆分類：

- **priority**：P0（金流/安全）、P1（功能不完整/資料不正確）、P2（已知限制/工具改善）、P3（文件/低影響）
- **category**：金流安全 / 功能缺口 / 資料正確性 / 工具改善 / 部署設定 / 文件

### Source 2: Escalations + Review Reports

掃描 `.4x/run/*/rounds/*/` 下的：

- `escalation.json` — `needed: true` 且根因只是繞過（workaround）沒真正修的
- `review-report.md` / `deep-review-report.md` — WARNING / CONDITIONAL PASS 項目
- `test-report.md` — 標記為「已知限制」但應該修的

聚焦在：
- 跨 feature 重複出現的問題
- 從沒被修的 WARNING

### Source 3: Learnings 反覆模式

檔案：`.4x/learnings.json` + `.4x/learnings-context.md`（CLAUDE.md 內 inline 版亦可）

按主題分群，找出出現 3 次以上的同類問題，代表根本問題沒被解決。
產出：主題名、出現次數、根因、建議工具/流程改善。

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
1. 讀 `.4x/features/` 找最新 ws 編號
2. 把相關項目合併分群（按領域/repo 分，不超過 10 個 feature）
3. 建立 feature YAML（status: not-started）
4. 更新 discovered-feature-gaps.md 標記 `[已開 ws-XXX]`

## 注意事項

- 不修改任何原始碼，純讀取分析
- 不改 `.4x/run/` 下的任何 artifact
- feature YAML 只在使用者確認後才建立
- Systemic issues 通常需要在 4x CLI repo 修，不是當前專案的 feature
