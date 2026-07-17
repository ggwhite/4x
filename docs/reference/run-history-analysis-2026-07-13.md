# 4x + Kairos Run 歷史分析（2026-07-13）

> 範圍：`/Users/white/github/4x`（164 runs / 158 features / $2,119 累計）與
> `/Users/white/Tyche/Kairos`（170 runs / 234 features / $4,311 累計）兩專案的
> `.4x/run/*` 執行歷史、`4x cost` 聚合、`.4x/learnings-context.md`、
> `docs/reference/discovered-feature-gaps.md`。
>
> Opus/Sonnet 各角色分級與模型建議已有專文，見
> [`model-routing-recommendations.md`](./model-routing-recommendations.md)，本文不重複，
> 只記錄本次歷史資料額外挖出、該文尚未涵蓋的發現。gaps 檔噪音問題已有專文，見
> [`audit-noise-analysis.md`](./audit-noise-analysis.md)。

## 摘要

| | 4x | Kairos |
|---|---|---|
| 卡住比例（needs-attention/blocked） | 15%（25/164） | 32%（36/111 有事件的 feature） |
| gap 已開票率 | 55%（11/20） | 62%（52/84） |
| retry（round≥2）佔總成本 | 26.2% | 25.9% |
| 最貴 role | coder（39.8%） | coder（37.5%） |

两個完全不同的專案，retry 稅收斂到幾乎一樣的比例（~26%）——這暗示這是現行 pipeline 設計下
相對穩定的「重跑稅」，比起微調個別 role 的模型，**降低 round-1 一次通過率**才是最大槓桿。

## 1. 跨專案共通的架構性問題：worktree 證據沒有對 main 驗證

兩個專案都反覆出現同一種失敗模式：Tester/Acceptor 在 worktree 內對**本地未追蹤或
`.gitignore` 排除的檔案**跑驗證，得出「PASS」，但這些內容從未真正進到 main 分支。

- **4x F176**：designer 在 worktree 改的 `repos` 欄位只存在 worktree 本地副本，
  沒有機制同步回主工作區 `.4x/features/{id}.yaml`，導致 design-reviewer 每輪都查到
  舊值，死循環 3 輪才被人工發現根因。
- **Kairos ws-219**：同款 `repos` 欄位未同步問題。
- **Kairos ws-141-bullrob**：round-6/7 的 AC-13「PASS」是 Tester 對
  `.gitignore` 排除的 worktree 本地檔案 grep 出來的假證據，實際上這些變更從未提交，
  且該功能的根層 Docker 改動已被人工 revert 卻沒補回，最終才在更後面的檢查中判
  needs-attention。

**根因**：目前 AC 驗證（`ac_checks`）與部分 Tester grep 驗證沒有區分「這份證據來自
git-tracked 且已進 main 的檔案」還是「只存在於 worktree 工作樹的暫存內容」。

## 2. Kairos 特有但可能該有通用 4x 機制的缺口：根層跨 repo wiring 沒有歸屬

Kairos 的多 repo feature（例如新遊戲上線）常需要改動根層共用檔案
（`Dockerfile`、`docker-compose.yml`、`dev.sh`），但這些檔案不屬於任何單一宣告的
`repos` scope，Coder 依「只改 declared repos」規則直接跳過。

- **ws-140-goldenflower**：根層 wiring 缺漏
- **ws-141-bullrob**：Coder 把新遊戲接入根層共用 `Dockerfile` 的單一 builder stage，
  builder 編譯失敗直接波及並行跑的 ws-143-luckywheel 測試環境
- **ws-228-triangles**：同款問題第三次出現

同一個坑咬過三次，屬於「Designer 沒有正式管道宣告『這個 feature 需要動根層共用檔案』」
的設計缺口，而不是 Coder 的錯。

## 3. Opus vs Sonnet：一個具體的反例

`model-routing-recommendations.md` 的核心論點（判斷密集角色不降級）在本次資料裡繼續成立，
但本次額外發現一個值得放進反面清單的具體反例：

- Kairos 的 **Sonnet Tester/Acceptor** 在 ws-140（5 輪測不出需求缺口）、
  ws-141（照單全收假證據）兩案例中，「省下的單價」以更晚、更貴的 retry/人工介入形式
  賠了回去——這跟原則 4「打回重做是最大成本來源」是同一件事，只是具體發生在
  T4 機械執行角色的「證據真偽核對」這個子任務上，而不是 T1/T2 的產出品質上。

**與第 1 點的關聯**：這兩個案例的根因都是「證據驗證沒有錨定 main」，不是單純的模型能力
問題——升級模型可能只是把同一個架構缺口的發生機率往下壓，治標不治本。優先修第 1 點的
機制性問題，可能比把 acceptor/tester 升級到更貴的模型更划算。

## 4. Claude/Codex 交叉驗證：有牙齒，但樣本仍小

| Feature | 角色 | 結果 |
|---|---|---|
| Kairos ws-177 | codex reviewer | 抓到 Claude Coder 引入的 typed-nil interface panic regression（`Room.identity` 改 interface 後，db1 不可用時 nil 檢查失效，`Resolve` 對 nil receiver panic）——功能測試全綠，金流路徑有真實 crash 風險 |
| 4x ws-8 | codex reviewer | 抓到 scope 違規（多檔案超出 task brief 範圍），claude 自己沒抓到 |
| 4x F162/F168 | codex acceptor | 讀 137.7K review-package 後 5 秒內靜默 exit-1，兩次重試皆同——大 context 下不穩定 |

**判讀**：codex 當「窄範圍對抗式 reviewer」有實測抓到 claude 自己審不出的問題（尤其
typed-nil 這類同廠模型容易共享盲點的模式）；但不適合接大 context 的最終簽核角色。
這個結論支撐了目前 4x 專案自己已經在做的事（reviewing phase 固定指定 codex）。

樣本量仍小（<15 個 feature 曾用 codex，且多數非「同 feature 同輪 claude vs codex 並列審
同一 diff」的乾淨對照組），不足以量化「codex 比 claude 多抓 X%」，僅可視為方向性證據。

## 待評估的優化候選（详见「可行性研究」章節，尚未開票）

1. 證據驗證錨定 main：AC 驗證/Tester grep 改成明確要求對 git-tracked 且已進 main（或至少
   已 commit 到 feature 分支）的內容驗證，worktree 本地未追蹤檔案不算數
2. 多 repo feature 的「根層共用檔案」宣告機制（Kairos 需求驅動，但屬通用缺口）
3. codex 作為 reviewer 角色的標準配置選項（Kairos 目前只是偶爾用）
