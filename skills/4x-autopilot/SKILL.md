---
name: 4x-autopilot
description: >
  4x feature pipeline 全自動駕駛。序列執行 pick → run → done → MR → merge → pull → clean → next。
  觸發：「4x autopilot」「4x 自動跑」「自動跑 feature」「跑任務」。
  Owner 專用——全自動 merge，不停頓等確認。
  搭配 /loop 使用：`/loop 4x-autopilot` 或 `/loop 4x-autopilot ws-XXX`。
---

# 4x Autopilot

Owner 專用自動駕駛 skill。搭配 `/loop` 持續輪詢 4x 狀態，驅動 feature 完整生命週期。

## 核心原則

- **無狀態**：每次 wakeup 從 4x 側重新讀狀態，session 斷線重連即接上
- **序列執行**：一次只跑一個 feature，完成後才 pick 下一個
- **全自動**：不停頓等確認，包括 merge MR
- **專案無關**：所有專案特定值（repo 清單、GitLab 路徑）從 `.4x/settings.json` 動態讀取

## 前置：讀取專案設定

每次 wakeup 開始時，先讀 `.4x/settings.json` 取得：

```
REPOS      = Object.keys(settings.workspace.repos)   # 所有 repo 名稱（動態，不硬寫）
HUB_REPOS  = settings.hub_repos || []                 # hub repo（同步時包含，scope 檢查時排除）
USE_GITLAB = settings.issue_tracker?.enabled == true   # 是否啟用 GitLab MR 流程
```

取得 GitLab group（僅 USE_GITLAB 時需要）：

```bash
# 從第一個 repo 的 git remote 推導
GITLAB_GROUP=$(git -C <first-repo> remote get-url origin | sed -E 's|.*[:/](.+)/[^/]+\.git$|\1|')
# 例如 git@gitlab.com:server/kairos/kairos-core.git → server/kairos
```

## 參數

- 無參數：自動從 `4x status --json --pending` 按 priority + id 選下一個
- 帶 feature-id：優先跑指定 feature

## 每次 Wakeup 決策樹

每次被 `/loop` 喚醒時，依序檢查：

### 1. 讀取狀態

```bash
4x status --json --pending
```

解析 JSON，對每個 feature 取 `id`, `name`, `status`, `priority`, `active` 和對應的 `state.phase`。

### 2. 判斷當前階段

按以下優先順序判斷，**命中第一個就執行，不繼續往下**：

| 優先 | 條件 | 動作 | 下次 wakeup |
|---|---|---|---|
| 1 | 有 feature `active: true` | → 執行 RUNNING | 270s |
| 2 | 有 feature `phase: pending-review` | → 執行 FINISHING | 不排（連續執行） |
| 3 | 有 feature `phase: needs-attention` 或 `blocked`，且累計失敗 < 2 | → 執行 AUTO-FIX | 270s |
| 4 | 有 feature `phase: needs-attention` 或 `blocked`，且累計失敗 ≥ 2 | → log 跳過該 feature | 立即回到判斷 |
| 5 | 有 `status: not-started` 的 feature | → 執行 IDLE-PICK | 270s |
| 6 | 都沒有 | → log 全部完成 | 1800s |

## IDLE-PICK：前置同步 + 選擇 + 啟動

觸發條件：決策樹優先 5（有 not-started feature 可跑）。

### Step A: 前置同步（每次 pick 前必做）

對 `REPOS` 中每個 repo 依序執行（跳過 path 不存在的）：

```bash
# 跳過不存在的 repo 目錄
[ -d <repo> ] || continue

git -C <repo> fetch origin main
git -C <repo> merge --ff-only origin/main
```

然後檢查雙向同步：

```bash
git -C <repo> rev-list --count origin/main..main   # 本地領先
git -C <repo> rev-list --count main..origin/main    # 遠端領先
```

- 本地領先（第一個 > 0）→ `git -C <repo> push origin main`
- 遠端領先（第二個 > 0）→ ff-only 應該已解決；如果還有，log 警告並停止 pick
- 兩個都 0 → 同步完成

**無 git remote 的 repo**（`git -C <repo> remote get-url origin` 失敗）：跳過同步，只 log。

### Step B: 清殘留 worktree

```bash
ls .worktrees/4x/
```

如果有任何目錄：
1. 對目錄內每個 repo 子目錄：`git -C <main-repo> worktree remove <worktree-path>`
2. `rm -rf .worktrees/4x/<feature>/`
3. 對 `REPOS` 中每個存在的 repo 執行 `git -C <repo> worktree prune`

### Step C: 選擇 feature

從 `4x status --json --pending` 的結果中：
1. 過濾 `status == "not-started"` 的 feature
2. 如果使用者透過參數指定了 feature-id，優先選它
3. 否則按 `priority` ASC, `id` ASC 排序，取第一個
4. 如果 feature 有 `depends` 欄位，確認所有依賴 feature 都是 `status: done`，不是就跳過選下一個

### Step D: 啟動

```bash
4x run <feature-id> --json
```

解析 JSON 回傳的 `pid` 和 `logPath`。

印 log：`啟動 <id>: <name> (pid=<pid>)`

用 `ScheduleWakeup` 排 270s 後喚醒。

## RUNNING：輪詢執行狀態

觸發條件：決策樹優先 1（有 active feature）。

### 判斷方式

讀 `.4x/run/<feature>/state.json`，取 `active`、`phase`、`round`、`pid` 欄位。

### 分支處理

**active == true：**

先確認 process 還活著：

```bash
ps -p <pid> > /dev/null 2>&1
```

- process 活著 → 印 log：`<id> 執行中 (phase=<phase>, round=<round>/<maxRounds>)`，排 270s wakeup
- process 已死 → 印 log：`<id> process <pid> 已死但 active=true，嘗試 retry`，執行 `4x retry <id>`，排 270s wakeup

**active == false：**

根據 `phase` 決定：
- `pending-review` → 進入 FINISHING 階段（本次 wakeup 內連續執行，不排 wakeup）
- `needs-attention` 或 `blocked` → 進入 AUTO-FIX 階段
- `done` → 印 log：`<id> 已完成（非本次啟動）`，進入 IDLE-PICK

## AUTO-FIX：自動修復 + 重試

觸發條件：決策樹優先 3（needs-attention/blocked 且累計 < 2）。

### Step A: 計算失敗次數

讀 `.4x/run/<feature>/events.jsonl`，計算 `"needs-attention"` 或 `"blocked"` 的 transition 事件數量：

```bash
grep -c '"needs-attention"\|"blocked"' .4x/run/<feature>/events.jsonl
```

如果 ≥ 2：印 log `<id> 累計失敗 <count> 次，跳過`，進入 IDLE-PICK。

### Step B: 診斷卡點

讀以下檔案（按存在性）：
1. `.4x/run/<feature>/rounds/round-<N>/escalation.json` — 如果 `needed: true`，取 `reason` 和 `detail`
2. 最新的 `review-report.md` 或 `deep-review-report.md` — 找 FAIL/WARNING
3. 最新的 `coder-report.md` — 找 escalation 或 blocker 描述
4. `.4x/run/<feature>/logs/` 下最新的 `.log` 檔案 — 找 error/panic

整理成一段摘要。

### Step C: 派 executor subagent 修復

用 Agent tool 派一個 executor subagent，prompt 包含：
- 卡點摘要（Step B 產出）
- worktree 路徑：`.worktrees/4x/<feature>/`
- feature YAML 的 repos 範圍
- 指示：在 worktree 內診斷問題、修復、commit。常見問題包括：
  - worktree 缺 sibling repo symlink → 補 `ln -s`
  - init.sh/lint.sh 缺執行權限 → `chmod +x`
  - escalation.json `needed: true` 但問題已解 → 改 `needed: false`
  - go.work 引用不存在的 module → 補 symlink 或 GOWORK=off
  - 4x check scope violation 誤報 → 確認是否為已知 harness 落差

### Step D: Retry

```bash
4x retry <feature-id>
```

印 log：`<id> 自動修復第 <count+1> 次，已 retry`

排 270s wakeup。

## FINISHING：收尾全流程

觸發條件：決策樹優先 2（phase == pending-review）。

本階段的所有步驟在同一次 wakeup 內連續執行，不排中間 wakeup。

### Step 1: 4x done

```bash
4x done <feature-id>
```

檢查輸出：
- 包含 `MR opened` → 記錄 MR URL（格式：`MR opened(<repo>): <url>`）
- 包含 `Feature <id> marked as done` → 成功
- 失敗 → 重試 1 次（4x done 對已存在的 MR 冪等安全）
- 還失敗 → 印 log `<id> 4x done 失敗: <error>`，跳過進 IDLE

### Step 2: Code Review（依 profile）

讀 `.4x/run/<feature>/state.json` 的 `profile` 欄位。

判斷是否有 deep-review：
- `full` profile → 跳過 code review（4x 內建 3 個 deep-reviewer 已審過）
- 其他 profile（`lite`、`quick`、`normal`、`code-only`）→ 執行輕量 review

輕量 review 方式：
1. 對每個 scope repo，取 MR diff
2. 用 `/code-review low` 審查
3. 如果有 CONFIRMED blocker：
   a. 在 worktree 內修復（如果 worktree 已清，先 `git clone --branch <feature-branch> --single-branch` 到臨時目錄）
   b. commit + push
   c. 重新 review 確認

### Step 3: Merge（依 USE_GITLAB 分流）

**USE_GITLAB == true（有 GitLab MR）：**

對每個 scope repo 的 MR：

```bash
glab mr merge <mr-number> --repo <GITLAB_GROUP>/<repo> --yes
```

取得 MR number 的方式：從 Step 1 的 `4x done` 輸出解析，或：
```bash
glab mr list --source-branch 4x/<feature-id> --repo <GITLAB_GROUP>/<repo>
```

**衝突處理：**

如果 merge 失敗且輸出含 "conflict" 或 "cannot be merged"：

1. checkout feature branch：
   ```bash
   # 如果 worktree 還在
   cd .worktrees/4x/<feature>/<repo>
   # 如果 worktree 已清
   git -C <repo> fetch origin 4x/<feature-id>
   git -C <repo> checkout 4x/<feature-id>
   ```
2. merge main 進來：
   ```bash
   git -C <repo> fetch origin main
   git -C <repo> merge origin/main
   ```
3. 解衝突：
   - `go.sum`、generated files → `git checkout --theirs`
   - 程式碼衝突 → 派 executor subagent 讀上下文解決
4. commit + push：
   ```bash
   git -C <repo> add -A
   git -C <repo> commit -m "resolve merge conflict with main"
   git -C <repo> push origin 4x/<feature-id>
   ```
5. 重試 merge
6. 還失敗 → 印 log `<id> MR 合併失敗，需手動處理`
7. 如果是在主 repo checkout 的 branch，切回 main：
   ```bash
   git -C <repo> checkout main
   ```

**USE_GITLAB == false（本地合併，無 MR）：**

`4x done` 已在本地完成合併（merge worktree 分支到 main）。
此時只需對有 git remote 的 scope repo 推送：

```bash
git -C <repo> push origin main 2>/dev/null || true
```

推送失敗（無 remote 或無權限）只 log 警告，不阻塞。

### Step 4: Pull

對所有 scope repo（從 feature YAML 的 `repos` 欄位取）：

```bash
git -C <repo> pull origin main 2>/dev/null || true
```

無 remote 的 repo 跳過。

### Step 5: 確認同步

對所有有 remote 的 scope repo：

```bash
git -C <repo> rev-list --count origin/main..main  # 應為 0
git -C <repo> rev-list --count main..origin/main   # 應為 0
```

任何非 0 → log 警告但不阻塞。
無 remote 的 repo → 跳過檢查。

### Step 6: 清 worktree

```bash
ls .worktrees/4x/<feature>/ 2>/dev/null
```

如果目錄存在：
1. 對目錄內每個 repo 子目錄：`git -C <main-repo> worktree remove .worktrees/4x/<feature>/<repo>`
2. `rm -rf .worktrees/4x/<feature>/`
3. 對 `REPOS` 中每個存在的 repo：`git -C <repo> worktree prune`

### Step 7: 完成

印 log：`<id>: <name> 完成`（USE_GITLAB 時附 MR URLs）

進入 IDLE-PICK。

## ScheduleWakeup 規則

每次 wakeup 結束時，根據結果排下一次：

| 結束狀態 | delaySeconds | reason |
|---|---|---|
| 已啟動 4x run，等待執行 | 270 | "monitoring <id> (phase=<phase>)" |
| AUTO-FIX 後 retry | 270 | "retry <id>, waiting for 4x run" |
| FINISHING 完成，有下一個 feature | 不排（本次 wakeup 內直接 IDLE-PICK） | — |
| FINISHING 完成，無下一個 feature | 1800 | "all features done, long sleep" |
| 無 feature 可跑 | 1800 | "no pending features" |

**重要**：FINISHING 的所有步驟（done → review → merge → pull → clean）在同一次 wakeup 內連續執行。完成後如果還有待做 feature，直接在本次 wakeup 內執行 IDLE-PICK + 啟動下一個，然後才排 270s wakeup。這樣避免不必要的 wakeup 間隔。

## 使用者插手

使用者隨時可以在 session 中打字。常見情境：

- **「先做 ws-XXX」**：skill 在下次 IDLE-PICK 時優先選擇該 feature
- **「暫停」**：skill 不再排 ScheduleWakeup，loop 自然停止
- **手動 `4x run ws-XXX`**：skill 下次 wakeup 偵測到 active feature，自動 monitor
- **加文件/改設定**：不影響 skill，4x run 的 subagent 會在下次讀取時看到

## 斷線恢復

session 斷線後重連，使用者只需重新執行：

```
/loop 4x-autopilot
```

skill 會重新讀 4x 狀態，自動接上：
- 有 active feature → RUNNING
- 有 pending-review → FINISHING
- 有 needs-attention → AUTO-FIX
- 都沒有 → IDLE-PICK
