# 使用技巧與最佳實踐

## Token 用量提醒

4x 會消耗**顯著多於單一 agent** 的 token。每個 feature 至少經過 4 個角色（Designer → Coder → Reviewer → Tester），每個角色都是獨立的 LLM 呼叫。如果 Review 或 Test 失敗觸發重跑，token 會再翻倍。

粗估每個 feature 的 token 用量：

| 情境 | 約 LLM 呼叫次數 | 說明 |
|---|---|---|
| 一次通過（最佳情況） | 5 次 | Designer + Coder + Reviewer(2 pass) + Tester |
| Review 打回 1 次 | 8 次 | 多一輪 Coder + Reviewer + Tester |
| 跑滿 5 rounds | ~20 次 | 每 round 都是 Coder + Reviewer + Tester |

**省 token 建議：**
- 簡單任務降低 `--max-rounds`（`--max-rounds 2`）
- 簡單任務全用 sonnet 等級 model（便宜 5-10 倍）
- 善用 `--dry-run` 先確認 prompt 品質，避免浪費
- Feature description 寫清楚，減少 escalation 和重跑
- 連續 3 輪無進步時 loop 會自動停，不會白燒到 max-rounds

---

## 完整工作流程

從建立任務到交付的完整流程——4x 負責 AI 開發，你負責最終 review 和合併。

### Step 1: 建立任務

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

如果需要，編輯 `.4x/features/F001-add-redis-cache-for-or.yaml` 補充 description、priority、depends、repos 等欄位。

### Step 2: 執行 loop

```bash
# 建議先 dry run 看 prompt
4x run F001 --dry-run

# 正式跑
4x run F001 --runner claude
```

可以開 dashboard 即時觀察：

```bash
4x live -w   # 另一個 terminal
```

### Step 3: Loop 完成 → pending-review

Loop 跑完後，feature 停在 `pending-review`——這是故意的。AI 做完了，但需要你來 review。

```bash
4x status F001
# Phase: pending-review
```

### Step 4: 人工 Review

檢查 AI 產出的成果：

```bash
# 看最終報告
cat .4x/F001/final-report.md

# 看 commit 計畫
cat .4x/F001/commit-plan.md

# 看 code diff
git diff                          # 非 worktree 模式
git diff main...4x/F001-add-redis  # worktree 模式
```

如果不滿意，可以：

```bash
# 手動修改後重跑 review + test
4x transition F001 --to reviewing
4x run F001

# 或完全重來
4x transition F001 --to designing
4x run F001
```

### Step 5: 合併 & 清理

**非 worktree 模式**（改動直接在 working tree）：

```bash
# 滿意後標記完成
4x done F001

# 按 commit-plan.md 提交
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Worktree 模式**（改動在獨立 branch）：

```bash
# 標記完成
4x done F001

# 合併到主分支
git merge 4x/F001-add-redis-cache-for-or

# 清理 worktree 和 branch
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### 流程總覽

```
4x new "..."                     # 建立任務
    ↓
4x run F001 --runner claude      # AI 自動跑 Design→Code→Review→Test
    ↓
pending-review                   # 等你 review
    ↓
review final-report / diff       # 你看成果
    ↓
4x done F001                     # 標記完成
    ↓
git merge + cleanup              # 合併、清 worktree/branch
```

---

## 撰寫好的 Feature 描述

Feature description 是 Designer 的唯一輸入——寫得越清楚，產出的 spec 越準。

```bash
# Bad: 太模糊，Designer 會自己腦補
4x new "改善效能"

# Good: 明確目標、邊界、驗收條件
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

建議在 description 包含：
- **要做什麼**（具體功能或修改）
- **為什麼做**（業務動機或問題描述）
- **邊界**（不要動的東西、已知限制）
- **驗收標準**（可量化的成功定義）

## Feature 粒度

一個 feature 對應一個可獨立交付的變更。太大會讓 Coder 迷失、Reviewer 漏檢、Test 難驗。

| 粒度 | 適合 | 不適合 |
|---|---|---|
| 一個 API endpoint | OK | — |
| 一個 refactor（改命名、抽介面） | OK | — |
| 一個 bug fix | OK | — |
| 整個模組從零開始 | — | 拆成多個 feature + depends |
| 跨 3 個 repo 的大功能 | — | 每個 repo 一個 feature，用 depends 串 |

善用 `depends` 拆解大任務：

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## 先 Dry Run 再正式跑

第一次用新 feature 或改過 settings 後，先用 `--dry-run` 看 prompt 是否合理：

```bash
4x run F001 --dry-run
```

這會印出四個角色的完整 prompt 但不呼叫 LLM，可以確認：
- Designer 有沒有拿到足夠的 context
- 你的 project rules 有沒有正確注入
- locale 是否正確

## Model 選擇建議

| 角色 | 建議 | 理由 |
|---|---|---|
| Designer | opus 或同等級 | 需要深度理解需求、拆解架構 |
| Coder | sonnet 或同等級 | 產出量大，但不需要最強推理 |
| Reviewer (checklist) | sonnet | 規則式檢查，速度優先 |
| Reviewer (adversarial) | opus | 需要深度推理找隱藏 bug |
| Tester | sonnet | 寫測試、跑驗證，不需要最強推理 |

調整方式：

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

如果專案簡單（小 bug fix、小 refactor），全用 sonnet 也行，省成本。

## Rounds 調校

預設 5 rounds 適合多數情況。根據 feature 複雜度調整：

| 情境 | 建議 rounds |
|---|---|
| 簡單 bug fix、小改動 | 2-3 |
| 一般功能開發 | 5（預設） |
| 複雜跨模組功能 | 7-10 |

```bash
4x run F001 --max-rounds 3   # 簡單任務
4x run F001 --max-rounds 8   # 複雜任務
```

注意：loop 會在 3 輪連續無進步時自動停止（不用跑滿 max-rounds）。

## 處理 Review 失敗

Review 失敗（verdict FAIL 或 CRITICAL findings）會自動送回 Coder 修改，不需要人工介入。但如果反覆失敗：

1. **看 review-report.md** — 在 `.4x/{feature-id}/rounds/round-{N}/review-report.md`
2. **看 coder-report.md** — Coder 是否理解了問題
3. **考慮調整**：
   - feature description 太模糊 → 重寫 description，重跑 Designer
   - Reviewer 太嚴格 → 在 `roles.reviewer.instructions` 放寬特定規則
   - 真的是 hard problem → 人工介入修改，再用 `4x transition` 推進

## 處理 Escalation

Coder 或 Tester 發現 spec 跟實際不符時，會自動 escalate 回 Designer。常見情境：

- DB schema 跟 spec 描述的不同（`spec-mismatch`）
- 驗收標準不合理（`criteria-wrong`）
- 缺少外部依賴（`blocker`）

Escalation 被記錄在 `.4x/{feature-id}/rounds/round-{N}/escalation.json`。Designer 會收到 escalation 內容重新出 spec。

如果 Designer 也解不了（通常是缺 context），loop 會停在 `needs-attention`，這時需要人工介入：

```bash
# 看狀態
4x status F001

# 手動修 spec 或 codebase
vim .4x/F001/task-brief.md

# 推回 coding 繼續
4x transition F001 --to coding
```

## 恢復中斷的 Feature

4x 是 file-based 的——session 斷了、機器重開，狀態都在 `.4x/` 裡。直接重跑即可：

```bash
4x run F001 --runner claude
```

會從上次的 phase 和 round 繼續，不會重頭來。

## Worktree 隔離

如果同時跑多個 feature，或想隔離 AI 的修改，啟用 worktree：

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

效果：
- 每個 feature 在 `.worktrees/4x/{feature-id}/` 獨立工作
- 自動建 branch `4x/{feature-id}`
- 完成後提示 merge 指令

```bash
# 完成後合併
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
```

## Batch 使用時機

| 情境 | 用 `4x run` | 用 `4x batch run` |
|---|---|---|
| 做一個 feature | OK | — |
| 做多個有依賴的 feature | 要手動排序 | 自動處理依賴順序 |
| 跑一晚上消化 backlog | — | OK，搭配 `batch stop` 隨時停 |

Batch 的 commit 策略固定是 `"never"`——所有改動都在 working tree，完成後由人工 review 再 commit。

## Dashboard 使用情境

```bash
# 開著 dashboard 跑 feature，即時看 log
4x live -w &
4x run F001 --runner claude

# 從 dashboard 直接啟動 feature（不用開 terminal）
# POST /api/run 搭配 web UI

# 多專案監控
4x live /path/to/project-a /path/to/project-b -w
```

## Locale 設定

讓 AI 用你的語言回應：

```bash
4x config set locale zh-TW
```

也可以不設——會自動從 `LANG` 環境變數推斷。

## Troubleshooting

### Feature 卡在 needs-attention

代表某個 phase 缺少必要的 artifact（例如 Designer 沒產出 task-brief.md）。

```bash
4x status F001          # 看缺什麼
4x check F001           # 跑完整檢查
```

手動補檔或重跑該 phase：

```bash
4x transition F001 --to designing
4x run F001
```

### Feature 卡在 blocked

通常是 runner exit code 1（軟失敗）。看 log：

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

解決後推回：

```bash
4x transition F001 --to coding
4x run F001
```

### Dependency gate 擋住

```
blocked: F001-user-model is not done (status: coding)
```

先完成被依賴的 feature，或手動標記：

```bash
4x done F001
4x run F002
```
