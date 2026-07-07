# 使用技巧與最佳實踐

## Token 用量提醒

4x 會消耗**顯著多於單一 agent** 的 token。每個 feature 至少經過 6 個角色（Designer → Coder → Reviewer → Tester → Deep-Reviewer → Acceptor），每個角色都是獨立的 LLM 呼叫。如果 Review 或 Test 失敗觸發重跑，token 成本會大幅增加。

粗估每個 feature 的 token 用量：

| 情境 | 約 LLM 呼叫次數 | 說明 |
|---|---|---|
| 一次通過（最佳情況） | 7 次 | Designer + Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| 最佳情況（未設定 deep_model） | 5 次 | Designer + Coder + Reviewer + Tester + Acceptor（跳過 Deep-Review） |
| Review 打回 1 次 | 12 次 | 多一輪 Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| 跑滿 5 rounds | ~27 次 | 每 round = Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |

**省 token 建議：**
- 簡單任務降低 `--max-rounds`（`--max-rounds 2`）
- 簡單任務全用 sonnet 等級 model（便宜 5-10 倍）
- 善用 `--dry-run` 先確認 prompt 品質，避免浪費
- Feature description 寫清楚，減少 escalation 和重跑
- 連續 3 輪無進步時 loop 會自動停，不會白燒到 max-rounds

---

## 實際工作流程（搭配 AI Agent）

這是作者日常實際使用 4x 的方式——不是直接下 CLI 命令，而是在同一對話中搭配 AI agent 完成整個流程。

### 1. 建立 Feature

請 AI agent 幫你建立 feature：

```
> 4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

### 2. Brainstorm — Spec 與 Plan

執行迴圈前，請 agent brainstorm 設計：

```
> brainstorm F001
```

Agent 使用 brainstorming skill 與你一起探索需求、取捨和邊界情況。達成共識後，產出兩份文件：

- `docs/design/F001-add-redis-cache-for-or-spec.md` — 設計 spec
- `docs/design/F001-add-redis-cache-for-or-plan.md` — 實作計畫

這些檔案遵循 `CLAUDE.md` 中 **Docs Routing** 宣告的命名慣例：`docs/design/{feature-id}-spec.md` 和 `docs/design/{feature-id}-plan.md`。

Spec 成為 Designer 的參考輸入——brainstorm 做得好，Designer 產出的 task brief 就更好，代表更少的 review 退回和重跑輪次。

### 3. 執行迴圈

```bash
4x run F001 --runner claude
```

在另一個 terminal 開 dashboard 觀察進度：

```bash
4x live -w
```

### 4. AI Code Review

迴圈完成（`pending-review`）後，請 AI agent review diff：

```
> help me review the diff on branch 4x/F001-add-redis-cache-for-or
```

Agent 讀取 `final-report.md`，diff branch 與 main，指出問題。有需要修的——手動改或請 agent 改。

### 5. Merge 與清理

滿意後，請 agent merge 並清理：

```
> merge it and clean up the worktree
```

Agent 執行：
```bash
4x done F001
```

`4x done` 自動 merge branch、移除 worktree、刪除 branch。若有 merge conflict，會提示你手動解決後再跑 `4x merge F001`。

### 6. 在 Dashboard 標記完成

開 dashboard（`4x live -w`）並在 feature 卡片上點 **Mark Done**。這刻意是人工操作——AI 迴圈永遠不會自動完成 feature。

### 為什麼這樣有效

- **先 brainstorm 再 coding** — spec 奠定整個迴圈的基礎；模糊性在前期解決，不在實作中途
- **你留在同一對話中** — 不用在 terminal 和工具間切換上下文
- **AI agent 已有完整 context** — 從 brainstorming 和執行 feature 累積的，所以 review 是有根據的
- **Mark Done 是手動的** — 你是最終把關者，不是 AI

### 4x 是什麼（和不是什麼）

4x 是一個**工作流程編排器**——它按順序執行 Designer、Coder、Reviewer 和 Tester 角色，管理它們之間的狀態機。它不取代你的判斷。

實際上，迴圈處理順利路徑很好：有清楚 spec 的直觀 feature 通常 1-2 輪就通過。但現實開發是混亂的：

- **Coder 可能誤解 spec** — Reviewer 抓到了，但下一輪的修正可能還是沒打到點。2-3 輪失敗後，直接介入或請 AI agent 修特定問題更快。
- **測試失敗可能是環境特定的** — Tester 根據 spec 寫測試，但如果專案有怪癖（自訂測試設定、不穩定的 CI、遺留限制），測試可能因 AI 無法診斷的原因失敗。你需要自己除錯。
- **邊界情況在迴圈後才浮現** — 4x 涵蓋 spec 描述的範圍。業務邏輯邊界情況、競態條件或整合問題通常在人工 review 或上線後才出現。
- **複雜重構可能需要人工指引** — 當 feature 涉及多個檔案或需要理解隱式慣例時，Coder 可能產出正確但次優的程式碼。一個快速的人工提示（「用 `pkg/util` 裡已有的 helper」）可省下多輪重試。

**正確的心態**：4x 給你一份扎實的初稿，附帶測試覆蓋和 review 回饋。把它想成一個能力不錯的 junior 開發者——精確遵循指示但有時需要指引。時間節省來自不用自己寫初始實作——而非完全把自己從流程中移除。

### 依專案自訂角色

4x 只處理狀態轉換和角色切換——它不知道你的專案該如何建置、測試或 review。這些知識在你的專案設定裡。

每個角色從專案的 `.4x/settings.json` 讀取要做什麼。給越多 context，產出越好：

```json
{
  "project": {
    "name": "my-api",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["golangci-lint run"],
    "rules": ["all exported functions must have GoDoc comments"]
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": {
      "model": "sonnet",
      "instructions": ["always use dependency injection via constructors"]
    },
    "reviewer": {
      "model": "sonnet",
      "deep_model": "opus",
      "instructions": ["check for SQL injection in all query builders"]
    },
    "tester": {
      "model": "sonnet",
      "instructions": ["use testcontainers for integration tests, not mocks"]
    }
  }
}
```

關鍵欄位：

| 欄位 | 效果 |
|---|---|
| `project.build/test/lint` | Coder 改完後執行這些；Tester 使用 `test` 驗證 |
| `project.rules` | 注入到每個角色作為硬約束 |
| `roles.*.instructions` | 角色專屬指引——該關注什麼、該避免什麼 |
| `roles.*.includes` | 額外讀取的檔案（例如 `["docs/api-conventions.md"]`） |

沒有這些，角色退回通用行為。有了它們，Designer 寫出符合你架構的 spec、Coder 遵循你的慣例、Reviewer 抓你專案特有的陷阱、Tester 寫出能在你環境中實際執行的測試。

詳見[設定](configuration.md)的完整參考。

---

## 端到端工作流程（純 CLI）

同樣的流程，但直接用 CLI 命令——適合不在 AI agent session 中的情況。

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
# 標記完成——自動 merge、移除 worktree 並刪除 branch
4x done F001
```

> 若有 merge conflict，`4x done` 會印出訊息請你手動解決，然後執行 `4x merge F001` 完成 merge 和清理。

### 流程總覽

```
4x new "..."                     # 建立任務
    ↓
4x run F001 --runner claude      # AI 自動跑 Design→Code→Review→Test→Deep-Review→Accept
    ↓
pending-review                   # 等你 review
    ↓
review final-report / diff       # 你看成果
    ↓
4x done F001                     # 標記完成 + 自動 merge/清理
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
| Acceptor | sonnet | 最終驗證是否符合 spec，與 reviewer 同等級 |

調整方式：

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "sonnet" }
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

1. **看 review-report.md** — 在 `.4x/run/{feature-id}/rounds/round-{N}/review-report.md`
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

Escalation 被記錄在 `.4x/run/{feature-id}/rounds/round-{N}/escalation.json`。Designer 會收到 escalation 內容重新出 spec。

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
- 開分支前，每個 repo 目前分支若設有 upstream tracking branch，會先 fetch 並 fast-forward 到該分支——本地已是最新時是 no-op，本地已與遠端分岔時也是 no-op（只印警告），不會覆蓋任何未推送的本地 commit。本地領先遠端（有未推送 commit、但沒分岔）時也會印警告——worktree 照樣從本地 HEAD 開出，但會讓你知道有 commit 還沒推送
- 每個 feature 在 `.worktrees/4x/{feature-id}/` 獨立工作
- 自動建 branch `4x/{feature-id}`
- 完成後 CLI 列印 merge 指令

```bash
# 完成後自動 merge 並清理
4x done F001
# 若有 merge conflict，手動解決後執行：4x merge F001
```

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

## 整合 gstack Browse 進行 E2E 測試

[gstack](https://github.com/garrytan/gstack) 提供一個持久化的 headless browser daemon，可以加速 4x 裡的 Playwright E2E 測試。不用每輪都冷啟動 Chromium（約 3-5 秒），daemon 讓 browser 保持運作並在各輪之間保留登入狀態。

這是**選用功能**——4x 內建的 `web` 測試 profile 不需要 gstack 也能運作。daemon 最適合以下情境：

- 你的專案需要登入（保留 session 省去每輪重新驗證）
- 你同時跑多個 feature（全部共用一個 browser 實例）
- 你想要低於 200ms 的 browser 回應時間，而非冷啟動延遲

### 安裝

1. 把 gstack 安裝為 Claude Code skill：

```bash
git clone --depth 1 https://github.com/garrytan/gstack.git ~/.claude/skills/gstack
cd ~/.claude/skills/gstack && ./setup
```

2. 啟動 browse daemon（在背景執行）：

```bash
# 在 Claude Code 中
/browse-open http://localhost:4567
```

或手動啟動：

```bash
cd ~/.claude/skills/gstack && bun run browse/src/server.ts
```

daemon 會選一個隨機 port，並把連線資訊寫入 `.gstack/browse.json`。

### 設定 4x 使用 gstack browse

在 `.4x/settings.json` 中覆寫內建的 `web` 測試 profile：

```json
{
  "test_profiles": {
    "web": {
      "include": "docs/test-profiles/gstack-web.md"
    }
  }
}
```

建立 `docs/test-profiles/gstack-web.md`：

```markdown
Web UI E2E Testing with gstack Browse:

- Use gstack browse daemon instead of launching a standalone Playwright instance
- Read connection info from .gstack/browse.json (port + auth token)
- Send commands via HTTP POST to the daemon:
  - `POST /command` with `{"command": "goto", "args": ["http://localhost:4567"]}`
  - `POST /command` with `{"command": "snapshot"}` to get the accessibility tree with @e refs
  - `POST /command` with `{"command": "click", "args": ["@e5"]}` to interact with elements
  - `POST /command` with `{"command": "screenshot"}` to capture evidence
- Include Bearer token from browse.json in all requests
- Save screenshots to the configured screenshot_dir
- Each AC item must have at least one screenshot as evidence
- Do NOT launch a separate Chromium instance — use the running daemon
- If the daemon is not running, fall back to standard Playwright (npx playwright test)
```

### 範例：搭配 gstack 的 test-strategy.yaml

```yaml
web: true
api: false
coder_only: false
profiles:
  - web
verify_commands:
  - "make build"
  - "make test"
```

當 Designer 標記 `profiles: [web]`，而你的 `test_profiles.web` 已指向 gstack 覆寫，Tester 的 prompt 裡就會自動注入 gstack 專屬的操作指引。

### 需要登入的專案

對於需要驗證的專案（例如管理後台），在跑 4x 之前先在 gstack daemon 中登入一次：

```bash
# 在 gstack daemon 中開啟登入頁
/browse-open https://your-app.example.com/login

# 手動登入，或透過 gstack fill 指令填表
# Session cookie 會在後續所有 4x 測試輪次中持續保留
```

之後 Tester 可以完全跳過登入步驟——daemon 的 browser 已持有有效的 session。

### 不使用 gstack

如果你不用 gstack，內建的 `web` profile 開箱即用：

- Tester 每輪測試啟動一個獨立的 Playwright 實例
- 建立暫時工作區、以隨機 port 啟動 `4x live`
- 跑測試、截圖、清理
- 輪次之間無持久狀態（每輪都是全新開始）

詳見[測試 Profile](concepts.md#test-profiles)了解覆寫 profile 的方法。

---

## 一次教會你的 AI Agent 4x

預設情況下，每次新的 AI 對話都會從頭重讀 4x 文件。你可以透過新增**全域指令檔**來省去這個步驟，讓 agent 在對話開始前就已經知道 4x 命令、目錄結構與角色契約。

### Claude Code

在 `~/.claude/rules/4x.md` 建立 4x 快速參考（見下方範例）。`~/.claude/rules/` 裡的檔案會自動載入每個 session。

### Gemini CLI

在 `~/.gemini/instructions/4x.md` 建立相同內容。

### Codex

將 4x 指令加入你的全域 `AGENTS.md`。

### 範例：全域規則用 4x 快速參考

將 [`docs/reference/4x-agent-rules.md`](../../reference/4x-agent-rules.md) 複製到你的 agent 全域規則目錄。其中包含：

- 所有 CLI 命令與常用旗標
- `.4x/` 目錄結構
- 角色契約（讀取 / 寫入 / 限制）
- 狀態機轉換
- 支援的 runner
