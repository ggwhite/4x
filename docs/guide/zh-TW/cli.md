# CLI 參考

所有 feature-id 參數支援不分大小寫的前綴匹配。`4x run f001`、`4x run F001-user` 和 `4x run F001` 都會解析為 `F001-user-authentication-w`。模糊的前綴會產生列出匹配項的錯誤。

---

## `4x init`

在當前目錄初始化一個 `.4x/` workspace。

```
4x init
```

- 自動偵測專案語言和 build/test/lint 命令
- 建立 `~/.4x/settings.json`，包含 6 個預設 runner（claude、codex、gemini、agy、copilot、cursor）
- 將內嵌的 plugin 檔部署到 `.4x/plugins/`
- 在根層級檔案（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md、.cursorrules）加入 `@import` 行
- 如果 `.4x/` 已存在則報錯

---

## `4x new <title>`

建立一個新的 feature，可附帶選用的中繼資料。

```
4x new "Feature title" [flags]
```

| 旗標 | 說明 |
|---|---|
| `--id` | 自訂 feature ID slug（跳過自動截斷） |
| `--desc` | Feature 描述（預設與標題相同） |
| `--subtask` | 子任務，格式為 `"id:name"` 或 `"id:name:description"`（可重複） |
| `--rule` | 規則引用（可重複） |
| `--depends` | 依賴的 feature ID（可重複） |
| `--priority` | 優先順序（0=critical、1=high、2=medium、3=low） |
| `--repo` | 範圍內的 repository（可重複） |
| `--json` | 以 JSON 格式輸出 |

建立 `.4x/features/F{NNN}-{slug}.yaml`，狀態為 `not-started`。
自動產生的 slug 會在 word boundary 截斷；用 `--id` 可覆蓋。
建立流程走統一的 `feature.Create` 路徑（見[核心概念](concepts.md#feature-creation)）— 儀表板的 `POST /api/new` 也走同一邏輯，因此此處的旗標與儀表板的「新 Feature」表單一一對應。

範例：
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

---

## `4x run <feature-id>`

執行一個 feature 的 Design-Code-Review-Test 迴圈。

```
4x run <feature-id> [flags]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--runner` | 設定預設值 | Runner plugin 名稱 |
| `--max-rounds` | `5` | 最大迴圈迭代次數 |
| `--timeout` | `3600` | 每階段逾時秒數 |
| `--dry-run` | `false` | 印出角色 prompt 但不呼叫 LLM |
| `--json` | `false` | 啟動執行並立即以 JSON 格式回傳 |
| `--profile` | auto | Pipeline profile（`full`/`normal`/`quick` 或自訂）；覆蓋依優先順序自動選取的結果 |

`--profile` 選擇要啟用哪些角色。內建 profile：`full`（全部 6 個角色）、`normal`（coder/reviewer/tester/acceptor）、`quick`（coder/reviewer）。未在 profile 中的角色會直接跳過（狀態沿合法邊界推進但不呼叫 runner）。省略時，若 `settings.json` 有 `profiles` 區段，會依 feature 的 priority 自動選取（否則為 `full`）。詳見[設定 → Profiles](configuration.md#profiles)。

迴圈驅動：init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review。Review 失敗時，code 會再跑一輪。Test 失敗時，迴圈重新進入 coding。

每個非 Designer 角色完成後，會自動執行 guardrail 檢查（scope、baseline、必要檔案）。違規時 feature 轉為 `needs-attention` 並停止迴圈。Designer 豁免——它不修改原始碼。

Review 判定必須以 `PASS` 開頭才算通過。`## Verdict` 標題與判定文字之間的空行會被忽略。模糊輸出（`TODO`、`ERROR`、亂碼、缺少 `## Verdict` 區塊）視為失敗。

`settings.json` 或 feature YAML 中宣告的 phase hook 會在迴圈內每次階段轉換前後自動執行。詳見[Phase Hooks](concepts.md#phase-hooks)。

進入 `testing` 階段時（`pre_testing` hook 之後、Tester runner 啟動之前），若有設定 `health_check`，會自動驗證環境健康狀態。檢查命令依序執行；失敗時執行 recovery 命令一次再重試。若環境仍然不健康，feature 轉為 `needs-attention` 並停止迴圈。詳見[Health Check](concepts.md#health-check)。

當 `settings.json` 啟用 `auto_discover_features` 時，deep review 最終 **PASS** 後會解析 `deep-review-report.md` 中的 `[NEW-FEATURE]` 標記，自動為 deep reviewer 發現的超出範圍問題建立 feature YAML（去重並設上限）。詳見[設定 → Auto-Discover Features](configuration.md#auto-discover-features) 及[核心概念 → 自動發現 Feature](concepts.md#auto-discovered-features)。

如果 feature 處於 `blocked` 或 `needs-attention` 階段，會根據當前角色自動恢復到適當的恢復階段。

自動檢查依賴閘門 — 如果被依賴的 feature 未完成則阻擋。

如果設定中設了 `isolation: "worktree"`，會在 `.worktrees/4x/<feature-id>/` 的 git worktree 中執行。多 repo 模式下（workspace.repos 有設定），每個 repo 各有自己的 worktree，位於 `.worktrees/4x/<feature-id>/<repo-name>/`，workspace 層級的檔案（go.work、Makefile 等）會複製到旁邊。Coder prompt 包含 `== Workspace Repos ==` 區段；worktree 模式下每個項目顯示 repo 名稱作為相對路徑（例如 `core → core/`），讓 Coder 在正確的目錄邊界內操作。

---

## `4x status [feature-id]`

顯示 feature 狀態。

```
4x status              # 所有 feature，按狀態分組
4x status <feature-id> # 單一 feature 詳情含子任務
4x status --pending    # 隱藏 done/abandoned 的 feature
4x status --json       # 以 JSON 格式輸出
```

| 旗標 | 說明 |
|---|---|
| `--pending` | 隱藏 done/abandoned 的 feature |
| `--json` | 以 JSON 格式輸出 |

分組：Running、Review、Pending、Todo、Done（done 最多顯示 5 個）。包含 backlog drift 警告。

查看單一 feature 詳情（`4x status <feature-id>`）時，若有截圖存在，也會印出：

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x subtask <feature-id> <subtask-id>`

更新 feature 內某個子任務的狀態。

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| 旗標 | 說明 |
|---|---|
| `--status` | 新狀態：`done`、`in-progress`、`blocked`、`not-started`、`ready-for-review`（必填） |

範例：
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x gate`

對挖掘出的候選 feature 套用 F097 evolve **value gate** 否決層。純 CLI 確定性否決——不呼叫 LLM。`gate` LLM 角色在兩階段之間執行（由 evolve driver 編排），產出 `gate-verdicts.json`。

必須指定 `--pre` 或 `--post` 其中之一：

- `--pre` — PRE-否決：讀取 `.4x/candidates.json`，丟棄與既有 feature 或批次內重複的 Jaccard 相似候選，將存活者寫入 `.4x/gate-input.json`。
- `--post` — POST-否決：讀取 `.4x/gate-input.json` + `.4x/gate-verdicts.json`，套用不可覆寫的硬否決（non-accept / 缺少 `why_not_hack` / 低於 `value_floor` / 與既有重複 / 超過 `max_accept_per_run` / 超過 `max_backlog_undone`），將通過的候選（含 `value_score`/`why_not_hack`）寫入 `.4x/accepted-candidates.json`。

閾值來自 `settings.json` 的 `evolution` 區段（`value_floor`、`max_accept_per_run`、`max_backlog_undone`、`dedup_threshold`）。

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

執行一輪持續自我改進 pipeline，將既有的進化零件串成可重複執行的閉迴路：

**mine → gate (pre → gate LLM 角色 → post) → enrich → enqueue → (可選) auto-run meta-loop → learnings 回饋下一輪。**

CLI 層絕不直接呼叫 LLM——gate 角色與 enrichment 都以 `runner` 子程序執行。每次呼叫恰好執行**一輪**；透過外部驅動（cron 或重複呼叫 `4x evolve`）進行多輪。每輪結果寫入 `.4x/evolve-report.md`。

Pipeline 步驟：

1. **mine** — 掃描 `.4x/` 尋找失敗訊號（escalation / 卡住的 feature / 反覆 FAIL 模式），去重後合併至 `.4x/candidates.json`。
2. **gate pre** — Jaccard 去重存活者至 `.4x/gate-input.json`。
3. **gate role** — 啟動 `gate` LLM 角色寫入 `.4x/gate-verdicts.json`。
4. **gate post** — 套用不可覆寫的否決 + 收斂上限，寫入 `.4x/accepted-candidates.json`。
5. **enrich + enqueue** — 將每個通過的候選具現化為 `not-started` feature YAML（enrichment 失敗時降級為從候選文字建立的基本 feature，標記 `enriched=false`）。
6. **auto-run**（可選）— 為每個排入的 feature 執行 meta-loop，受 F098 self-mod scope guard 保護。

反空轉：當某輪未接受任何候選時，`.4x/evolve-state.json` 遞增 `consecutiveNoAccept`；達到 `evolution.max_idle_rounds`（預設 3；設 `<= 0` 停用）後，下次呼叫提早中止並標記報告為 `Halted`，以 exit 0 結束。使用 `--force` 可覆寫。

```
4x evolve                        # 執行一輪，feature 維持 not-started
4x evolve --dry-run              # 唯讀：印出 mine/dedupe 摘要，不寫入任何檔案
4x evolve --auto-run             # 同時為排入的 feature 執行 meta-loop
4x evolve --force                # 繞過反空轉中止
```

| Flag | 說明 |
|---|---|
| `--auto-run` | 為每個排入的 feature 執行 meta-loop（F098 self-mod guard 始終強制） |
| `--dry-run` | 唯讀分析：印出 mined/deduped 數量，不寫入檔案、不啟動 runner、不建立 feature |
| `--min-occurrences` | 失敗模式成為候選的 distinct-feature 閾值（預設 3） |
| `--force` | 覆寫反空轉中止，即使連續空轉輪也執行 |
| `--runner` | gate / enrich / auto-run 使用的 runner plugin（預設 `evolution.gate_runner` 或專案預設） |
| `--timeout` | LLM 子程序逾時秒數（預設 3600） |
| `--max-rounds` | `--auto-run` 時每個 feature 的最大輪數（預設 5） |

Dashboard 透過 `GET /api/evolve-report` 呈現最新報告。

---

## `4x check <feature-id>`

執行 guardrail 檢查但不轉換狀態。

```
4x check <feature-id> [--json]
```

| 旗標 | 說明 |
|---|---|
| `--json` | 以 JSON 格式輸出結果 |

檢查項目：必要檔案、基線、範圍、依賴、backlog drift。通過時 exit 0，失敗時 exit 1。

---

## `4x transition <feature-id>`

強制狀態轉換。

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| 旗標 | 說明 |
|---|---|
| `--to` | 目標階段（必填） |
| `--role` | 執行轉換的角色 |
| `--json` | 以 JSON 格式輸出 |

驗證轉換是否合乎狀態機規則。如果狀態不存在則自動初始化。`testing → accepting` 轉換會執行額外的閘門（verify.json、test-report.md、final-report.md 必須存在且驗證必須通過）。

若 `settings.json` 或 feature YAML 宣告了 `hooks`，`pre_{phase}` hook 會在轉換前執行，`post_{phase}` hook 在轉換後執行。`block` 模式的 pre-hook 失敗會中止轉換；`block` 模式的 post-hook 失敗會將 feature 移至 `needs-attention`。詳見 [Phase Hooks](concepts.md#phase-hooks)。

---

## `4x event <feature-id>`

在 `events.jsonl` 中追加一個事件。

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| 旗標 | 說明 |
|---|---|
| `--type` | 事件類型（必填） |
| `--role` | 觸發事件的角色 |
| `--round` | 輪次編號 |
| `--action` | 動作名稱 |
| `--detail` | 額外的說明文字 |

---

## `4x prompt <feature-id>`

印出一個 feature 的角色 prompt。

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| 旗標 | 說明 |
|---|---|
| `--role` | 目標角色（省略時從當前狀態推斷） |
| `--round` | 輪次編號 |

支援語系注入（來自使用者設定或 `LANG` 環境變數）、規劃文件自動引入、以及專案/角色 include。spec/plan 文件透過共用解析器（`protocol.ResolveDesignDoc`）定位 — 先看 feature YAML 的 `spec`/`plan` 欄位，再找 `docs/design/{id}-{type}.md`，最後嘗試去掉 `FNNN-` 前綴的 `docs/design/{slug}-{type}.md` fallback — 因此 prompt 看到的文件與儀表板 overview 一致。詳見[設計文件解析](concepts.md#design-doc-resolution)。

若是 `tester` 角色，feature 的 `test-strategy.yaml` 中列出的 `profiles` 會被解析（透過 `loadProfiles`）並以 `== Test Profile: {name} ==` 區塊注入 prompt。每個 profile 的內容來源為 `settings.json` 的 `test_profiles[name]`（`content` 或 `include`），若無則使用內建的 `templates/profiles/{name}.md`。詳見[Test Profiles](concepts.md#test-profiles)。

---

## `4x done <feature-id>`

將一個 pending-review 的 feature 標記為完成。若 feature 有 worktree（`.worktrees/4x/<id>`），會自動 merge branch 回主分支並清理 worktree 與 branch。

```
4x done <feature-id>
```

僅在 feature 處於 `pending-review` 階段時有效。其他階段會報錯。

如果 merge 發生 conflict 或錯誤，feature 會維持 `pending-review`，worktree 會保留，並印出後續處理指引。解完 conflict 後，用 `4x merge <id>` 完成。

---

## `4x merge <feature-id>`

完成 `4x done` 發現 conflict 後的 merge。

```
4x merge <feature-id>
```

僅在 feature 處於 `pending-review` 或 `done` 階段，且 `.worktrees/4x/<id>` 存在時有效。會在 worktree commit 已解決的 conflict、merge 回主分支，然後清理 worktree 與 branch。若 feature 仍是 `pending-review`，merge 成功後才標記為 `done`。

多 repo 模式下，已解決的 conflict 會依 repo 各自 commit（每個 repo 在 `.worktrees/4x/<id>/<repo-name>/` 下獨立 stage 和 commit），接著所有 repo 一次性 all-or-nothing merge。若 conflict 再次出現，會顯示 `repo: <name>` 指出衝突的 repo。

---

## `4x clean [feature-id]`

移除已完成 feature 的 workspace artifact（`logs/`、`rounds/`、報告、`state.json`、`events.jsonl`），釋放磁碟空間。Feature 定義檔（`.4x/features/*.yaml`）和 feature 狀態永遠保留。

```
4x clean              # 列出可清理的 feature 及大小，確認後清理
4x clean --dry-run    # 只列出，不刪除
4x clean --force      # 跳過確認提示
4x clean <feature-id> # 清理單一 feature（仍須為 done/abandoned）
```

只有狀態為 `done` 或 `abandoned` 且有既有 workspace 目錄的 feature 才合格。活躍中（執行中）的 feature 永遠不會被清理，`blocked` / `needs-attention` 的 feature 也會保留以便除錯。清理不是狀態機轉換——它不會改變 feature 生命週期。

---

## `4x doctor`

對合併後的設定（`.4x/settings.json` + `~/.4x/settings.json`）和 workspace 完整性執行一次性的唯讀健康檢查，在你開始執行前使用。它不會呼叫 LLM，也不需要安裝任何 runner。

```
4x doctor [--json]
```

| 旗標 | 說明 |
|---|---|
| `--json` | 以 JSON 格式輸出完整報告（供 CI 使用） |

檢查分為以下幾個區段：

- **settings** — `settings.json` 可載入、`project.name` 非空、至少定義一個 runner、`default_runner` 存在於 runners 對應中。
- **runners** — 每個 runner 的 `command` 可在 `PATH` 上找到（找不到 → WARN 而非 FAIL，因為 runner 可能在遠端機器上）。
- **roles** — 解析每個角色（designer/coder/reviewer/tester/acceptor）透過預設 runner 實際使用的模型，以及 reviewer 的 `deep_model`。
- **workspace** — 孤立的 worktree（feature 已 done/abandoned 但 `.worktrees/4x/<id>` 仍存在）、懸掛的 worktree（目錄存在但無對應 feature）、過時的狀態（`active=true` 但 process 已消失）、以及格式錯誤的 feature YAML。

每行前綴為 `✅`（PASS）、`⚠️`（WARN）或 `❌`（FAIL），最後附摘要計數。

Exit code：沒有 FAIL 時為 `0`（WARN 不影響 exit code），有任何檢查失敗時為 `1`。`doctor` 嚴格唯讀——它不會改寫 `state.json`、清理 worktree、或修改設定。

```bash
# CI gate：任何 FAIL 檢查都讓 build 失敗
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

執行 feature 的 `test-strategy.yaml` 中的驗證命令，並將結果寫入 `rounds/round-{N}/verify.json`。

命令可透過 `verify_groups` 組織成群組：群組間平行執行，群組內循序執行。若群組中某個命令失敗，該群組剩餘命令會跳過，但其他群組繼續執行。若只定義了 `verify_commands`，則退回單一循序的 `default` 群組。同時宣告兩者會報錯。

平行執行完全由 CLI 處理——不涉及 LLM。Tester 角色呼叫此命令而非自己執行驗證命令；人類也可以獨立使用它來除錯。

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| 旗標 | 說明 |
|---|---|
| `--round` | 輪次編號（預設：state.json 中的當前輪次） |
| `--timeout` | 所有群組的整體逾時（預設：5m） |
| `--json` | 以 JSON 格式輸出完整的 verify.json |

所有非跳過的命令都通過時 exit 0，任一命令失敗時 exit 1。

---

## `4x config`

管理使用者層級的設定（`~/.4x/settings.json`）。

```
4x config list          # 顯示所有使用者設定
4x config get <key>     # 取得一個值
4x config set <key> <value>  # 設定一個值
```

Key 使用 dotted path。支援的格式：

| Key | 範例 | 說明 |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | UI / prompt 語系 |
| `theme` | `4x config set theme dark` | 儀表板主題 |
| `default_runner` | `4x config set default_runner claude` | 預設 runner plugin |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | 各 runner 的 `command`/`model`/`tty`/`stdin`/`quiet` |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | 各角色的 `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` |

`role.deep-reviewer.parallel_reviewers` 控制 deep review 展開的平行子審查者數量（`1` = 單一 agent 退化模式）；`role.deep-reviewer.angles_per_reviewer` 固定每組的審查角度數量（不設則自動均分）。詳見[核心概念 → 平行 Deep Review](concepts.md)。

---

## `4x sync`

將內嵌的 plugin 檔重新部署到既有專案。

```
4x sync [--dry-run]
```

| 旗標 | 說明 |
|---|---|
| `--dry-run` | 只報告差異，不寫入檔案 |

每個檔案會報告為 created、updated 或 current。

---

## `4x batch`

多個 feature 的批次操作。

### `4x batch plan`

產生依賴感知的執行計畫。

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--dry-run` | `false` | 印出排程但不寫入檔案 |
| `--max-chain` | `4` | 每個 cluster 的最大鏈長度 |

寫入 `.4x/batch-plan.json`。

### `4x batch next`

顯示下一個可執行的 feature（根據計畫和當前狀態）。

```
4x batch next [--json]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--json` | `false` | 以 JSON 格式輸出，含子任務前沿（subtaskFrontier） |

不帶 `--json` 時，以純文字印出 feature ID（向後相容）。帶 `--json` 時，輸出包含 `subtaskFrontier` 的 JSON 物件——即所有依賴都已完成的子任務。沒有合格 feature 時，JSON 模式回傳 `null`。

### `4x batch run`

按依賴順序循序執行合格的 feature。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--runner` | 設定預設值 | Runner plugin 名稱 |
| `--max-rounds` | `5` | 每個 feature 的最大輪次 |
| `--timeout` | `3600` | 每階段逾時秒數 |
| `--no-auto-merge` | `false` | 完成的 feature 停在 `pending-review` 而非自動 merge 回 main |

在 feature 之間檢查 `.4x/batch-stop` 檔案以實現優雅停機。

當批次執行結束時——無論是正常完成、被停止、收到 signal（`SIGTERM`/`SIGINT`）、或 crash——都會寫入 `.4x/batch-report.json`，摘要本次執行（outcome、completed/failed/remaining 計數、runner、耗時、每個 feature 的最終狀態）。詳見 [Batch Mode → Run Report](batch.md#run-report)。

預設行為下，feature 完成（到達 `pending-review`）後，批次會自動將其 worktree branch merge 回 main，讓下一個 feature 從更新後的 main 分支——實現無人值守的持續批次。若 merge 遇到 conflict，批次會優雅暫停，將 feature 留在 `pending-review`，worktree 保持完整，並寫入 `.4x/batch-conflict.json` 信號檔（feature、衝突 repo、檔案），供[儀表板](dashboard.md)顯示衝突；解完 conflict 後執行 `4x merge <id>`，再重新執行 `4x batch run` 繼續。衝突信號在每次執行開始時清除。非 conflict 的 merge 錯誤會印出警告，批次繼續處理下一個 feature。傳入 `--no-auto-merge` 可恢復舊行為（feature 停在 `pending-review` 等待人工 review）。

若設定中啟用了 `isolation: "worktree"`，每個 feature 在自己的獨立 worktree 中執行。多 repo 模式下，每個 feature 取得一個複合 worktree（`.worktrees/4x/<feature-id>/`），含每個 repo 的子目錄，commit 在每輪進行（不延後到完成時）。Hub repo（來自 `hub_repos` 設定或 `workspace.repos[*].hub: true`）會排除在共用 repo 的群組化之外，以允許平行執行。

### `4x batch stop`

通知正在執行的批次在當前 feature 完成後停止。

```
4x batch stop
```

建立一個 `.4x/batch-stop` 信號檔。

---

## `4x live [path...]`

啟動 4x Live 儀表板伺服器。

```
4x live [path...] [flags]
```

| 旗標 | 短旗標 | 預設值 | 說明 |
|---|---|---|---|
| `--port` | `-p` | `4567` | 伺服器 port |
| `--web` | `-w` | `false` | 在瀏覽器中開啟 |
| `--app` | `-a` | `false` | 開啟 macOS 原生 app |

不帶路徑時，從 `~/.4x/recent-projects.json` 載入最近的專案（LRU，最多 20 個）。帶路徑時，每個路徑作為一個專案分頁開啟。

---

## `4x mcp`

啟動 Model Context Protocol（MCP）伺服器。

```
4x mcp
```

啟動 4x MCP stdio 伺服器，將 4x CLI 命令以 MCP 工具的形式暴露給 LLM 客戶端（例如 Claude Code、Cursor）。
