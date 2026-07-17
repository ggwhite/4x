---
name: run-history-analysis
description: >
  跨專案分析 4x pipeline 的歷史執行資料：過去遇到的問題與解法、可優化的地方、
  Opus/Sonnet 效益、Claude/Codex 交叉驗證效益。觸發：「研究 4x run 歷史」
  「分析 run history」「4x 執行歷史分析」「opus sonnet 效益分析」
  「claude codex 交叉驗證分析」。對每個目標專案各派一個 researcher agent，
  用聚合工具（4x cost、learnings-context.md、discovered-feature-gaps.md、
  events.jsonl）分析，彙整後產出帶日期檔名的報告存到 docs/reference/，
  並可選擇進一步研究優化候選項的 4x feature 可行性。
metadata:
  internal: true
---

# Run History Analysis

跨專案分析 4x pipeline 累積的執行歷史，找出問題模式、優化空間，並量化
Opus/Sonnet 與 Claude/Codex 交叉驗證的實際效益。**純研究，不修改任何原始碼**
（除非使用者在 Step 5 明確要求開新 feature）。

## 目標專案

預設分析當前 `.4x/` 所在專案；使用者若額外指名其他專案路徑（例如同時要看
Kairos），一併納入分析。每個專案獨立派一個 agent（見 Step 1），不要混在一起讀。

## Step 1: 每個專案各派一個 researcher agent

用 Agent tool，`subagent_type: researcher`，`run_in_background: false`（需要等
全部回來才能彙整）。每個 agent prompt 需明確包含：

- 目標 repo 絕對路徑
- **這是純研究任務，不要修改任何檔案**
- 先確認規模：`ls .4x/run/ | wc -l`、`ls .4x/features/ | wc -l`
- **優先用聚合工具/既有彙整資料，不要逐一讀所有 run 目錄的原始 log**（成本太高）
- 以下 4 個維度逐一分析：

### 維度 1：過去遇到什麼問題、怎麼解決

- 讀 `.4x/learnings-context.md`（若無則讀 `.4x/learnings.json`）全文
- `grep -c '"needs-attention"\|"blocked"' .4x/run/*/events.jsonl` 統計卡住比例
- 讀 `docs/reference/discovered-feature-gaps.md`（若存在），算已開票率
- 挑 2-3 個具體案例（feature id + 具體卡點 + 怎麼解決），不要只講抽象分類
- 若使用者提供已知案例作對照，要求 agent 找**新的**、還沒提過的案例，不要照抄

### 維度 2：有什麼可以優化的地方

- 從 learnings 的 process 類條目找「重複發生但架構性尚未解決」的模式
- 跑 `4x cost --by-round --json`（repo 根目錄；若無 `bin/4x` 用系統 `4x` 或
  `make build`）量化 retry（round≥2）佔總成本比例
- 找明顯偏貴/偏慢的 role/phase

### 維度 3：Opus vs Sonnet 效益

- 讀 `.4x/settings.json` 的 `roles.*.model`，確認哪些 role 用 opus/sonnet
  （fallback 邏輯見 `internal/protocol/model.go`）
- 跑 `4x cost --json`（by-role）拿 calls/totalUsd/avgUsd
- 交叉比對：貴的 opus 角色是否真的换來更低的失敗/retry率，還是純燒錢
- 點出任何 `--phase-override` 造成的臨時 model 覆蓋，避免歸因錯誤
- 若專案已有 `docs/reference/model-routing-recommendations.md` 這類既有分析，
  agent 應先讀過、只補充新發現，不要重複整理

### 維度 4：Claude/Codex 交叉驗證效益

- 搜尋是否有 run 曾用 `--phase-override <phase>:codex:...` 或指定某 role 用
  codex runner（grep events.jsonl/run.log 找 `codex`，或看 state.json 的
  `runner`/`runners` 欄位）
- 找到的話，具體看那一輪 codex 當 reviewer 時抓出的問題，跟同 feature 若曾用
  claude 審過的結果比較
- 樣本小要誠實說明，不要過度推論成通則

每個 agent 產出結構化報告（4 維度各一段，具體數字/feature id/檔案路徑佐證），
控制在 800 字內，多用條列。

## Step 2: 彙整跨專案綜合報告

等所有 agent 回來後，在主線程彙整（不要再派 agent 做這步）：

- 逐維度比較各專案的數字（表格呈現），標出**跨專案一致**的模式（例如 retry
  佔比、worktree 同步問題）——這類最有參考價值，代表是架構性而非單一專案偶發
- 標出**專案特有**的模式，不要混為一談
- 給每個維度一個明確判讀/建議，不要只是羅列數字

直接以文字回覆使用者這份綜合報告（不要用 ReportFindings 工具）。

## Step 3: 詢問是否整理成文件

比照 CLAUDE.md 的慣例，主動問使用者要不要存成文件。若確認：

- 存到分析所在專案的 `docs/reference/`
- 檔名格式：`run-history-analysis-YYYY-MM-DD.md`（日期用執行當下日期，不要用
  Step 1 內部資料的舊日期）
- 內容結構比照 Step 2 的彙整報告，開頭加范圍/資料源說明（分析了哪些 repo、
  多少 run、累計花費）
- 若專案已有相關既有分析文件（如 model-routing-recommendations.md、
  audit-noise-analysis.md），在新文件開頭用連結交叉引用，只寫新發現，不要
  整段複製既有內容

多個專案的分析，若涉及不同 repo，個別詢問要不要各自存一份（不要自作主張存
到不屬於使用者確認過的位置）。

## Step 4: 研究優化候選項的 4x feature 可行性（可選，需使用者要求才做）

Step 2 通常會浮現幾個「優化候選」（例如：驗證機制錨定 main 而非 worktree
本地檔案、跨 repo 共用檔案宣告機制）。使用者若要求「研究看看可不可行」：

- 對每個候選項各派一個 researcher agent，鎖定 **4x 自己的原始碼**（不是被分析
  的目標專案，因為優化通常要改 4x 核心才能讓所有專案受益）
- prompt 要求 agent 具體找出：現有相關程式碼的檔案+行號、技術上是否可行、
  最小改動點清單、預估風險與邊界情況（會不會誤傷正常情境）
- 這一步**只研究可行性，不建立 feature、不改程式碼**——彙整後問使用者要不要
  真的用 `4x new` 開票

## 注意事項

- 全程唯讀分析；Step 3 寫文件、Step 4 開 feature 都需要使用者明確確認才動手
- `4x cost`/`4x status --json` 等聚合指令優先於逐檔讀取，這是控制研究成本的
  關鍵手法
- 樣本量小的維度（通常是維度 4）務必誠實標注，不要為了報告好看而過度推論
