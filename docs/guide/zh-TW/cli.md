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

### `4x init --dump-templates`

將內建的角色 prompt 模板傾印到 `.4x/templates/`，讓專案可以覆寫它們。

```
4x init --dump-templates          # 將內建模板寫入 .4x/templates/
4x init --dump-templates --force  # 覆寫既有的模板檔案
```

- 需要 `.4x/` 已存在（先執行 `4x init`）
- 將每個內嵌的 `*.md.tmpl`（包含 `locale.tmpl`）寫入 `.4x/templates/`
- 既有檔案會被略過並顯示警告，除非指定 `--force`
- 在 prompt 時，`.4x/templates/{file}` 優先於內嵌模板（整個檔案覆寫）；`locale.tmpl` 和每個角色模板各自獨立回退

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
| `--subtask` | 子任務，格式為 `"id:name"`（可重複）；第一個冒號前是 id，其餘整段是 name（name 可含冒號，如 `10:00`、`group:artifact`、URL）；description 由建檔後編輯 YAML 提供 |
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

## `4x cost`

從各 runner 寫出的 stream log 彙整跨 feature 的 run 成本。純唯讀，不會更動任何 run 資料。

```
4x cost                       # 所有 feature 的 per-role 成本表
4x cost --feature <id>        # 單一 feature 的 per-round per-role 明細
4x cost --by-round            # 各輪成本 + retry（round>=2）佔比
4x cost --feature <id> --by-round  # 單一 feature 的逐輪明細
4x cost --json                # 結構化輸出（上述任一視圖）
```

| Flag | 說明 |
|---|---|
| `--feature <id>` | 只看單一 feature，顯示 per-round per-role 明細 |
| `--by-round` | 依 round 彙總並顯示 retry（round>=2）佔比 |
| `--json` | 以 JSON 輸出 |

資料來源以 `logs/*.stream.jsonl` 為主（每個 role invocation 一檔，含 `total_cost_usd`），檔名編碼 round 與 role；某 feature 完全沒有 stream log（較舊的 run）時，退回 `events.jsonl` 的 `run-end` 事件為輔。缺 `total_cost_usd` 欄位的 stream log 會被跳過並回報 `Skipped N stream log(s)` 計數，而非直接失敗。

預設表格顯示 `ROLE / CALLS / TOTAL($) / AVG($) / PCT(%)`，依總成本排序，並附一列 `TOTAL`。`--by-round` 另加 `TYPE` 欄（round 0–1 為 `initial`，round≥2 為 `retry`），並以 USD 與百分比回報 retry 佔比。

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

## `4x approve <feature-id>`

核准由 enriched auto-discover 產生的 `draft` feature，將其從 `draft → not-started`，使 meta-loop 可以接手。草稿只在啟用 `enrich_discovered_features` 且 `enrich_auto_approve` 為 `false` 時才會建立。若 feature 不在 `draft` 狀態則報錯。

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

拒絕由 enriched auto-discover 產生的 `draft` feature，將其從 `draft → abandoned`，使其不進入 meta-loop。若 feature 不在 `draft` 狀態則報錯。

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

將卡在 `needs-attention` 或 `blocked` 的 feature 轉回工作階段，然後立即啟動 `4x run`。等同於 `4x transition --to <phase> <id> && 4x run <id>`。

未帶 `--to` 時，目標階段會從 `state.json` 記錄的 `role` **自動偵測**——把 feature 進入 `needs-attention`/`blocked` 前卡住的那個角色，反推回它對應的工作階段（例如 `role: designer` → `designing`；`role: coder` → `coding` 或 `amending`，視 round 而定）。自動偵測成功時，啟動前會印出 `auto-detected target phase from role "<role>": <phase>`。若角色無法對應（空值或未知），則 fallback 為 `accepting`。明確帶 `--to <phase>` 會覆蓋自動偵測。

```
4x retry F042-some-feature              # 從 state.json 的 role 自動偵測目標階段
4x retry F042-some-feature --to amending
```

| 旗標 | 說明 |
|------|------|
| `--to <phase>` | 要復原至的目標階段（預設：從 `state.json` 的 role 自動偵測，反推不出時 fallback `accepting`） |
| `--phase-override <phase>:<runner>:<model>` | 轉發給重新啟動的 `4x run`（可重複）——格式與語意同 `4x run` 的 `--phase-override` |

手動 `transition` / `retry --to <phase>` 設定的階段，會被後續的 `4x run` 復原流程尊重：它會標記 `manualPhase` 旗標，讓 `SmartResumePhase` 不會依磁碟上的 artifacts 把它推回較早的階段。這代表 `retry --to deep-reviewing` 真的會從 `deep-reviewing` 復原，而不會被拉回 `coding`。

狀態變更類指令（`transition`、`retry`、`force-done`、`done`）都是對 `state.json` 做單一鎖定的 read-modify-write，所以對一個正在被存活的 `4x run` 寫入的 feature 執行這些指令，不會互相覆蓋對方的更新。若在逾時內無法取得該 feature 的鎖，指令會直接報錯，而不是卡住。

若 feature 目前不在 `needs-attention` 或 `blocked` 則報錯。

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

merge 之前，4x 會先把主工作區內自己寫入的 pipeline 狀態（`.4x/features/*.yaml`、`.4x/learnings.json`、`.4x/learnings-context.md`）以 `chore(<feature-id>): 4x pipeline state` commit 掉。這個 commit 只帶指定路徑，主工作區其他未 commit 的 tracked 變更不會被收進去，也依然會中止 merge。`4x merge` 在完成合併前同樣會做這件事。

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->

從任何非終止階段強制將 feature 標記為完成。需要 `--reason` 來記錄跳過正常 pipeline 的原因。

```
4x force-done <feature-id> --reason "code reviewed and tests pass, e2e test deferred to post-merge"
```

將 feature 轉換到 `pending-review`，記錄一個帶有原因的 `force-done` 事件，然後觸發與 `4x done` 相同的 merge 流程。可從 `needs-attention`、`blocked` 或任何活躍階段執行。

Dashboard 透過 `POST /api/force-done` 加 `{id, reason}` 提供此功能。

| 旗標 | 說明 |
|---|---|
| `--reason` | 強制完成的原因（必填） |
| `--json` | 以 JSON 格式輸出結果 |

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

## `4x learn`

管理回顧學習——在 `.4x/learnings.json` 中累積的跨 feature 開發心得。

每個 feature 的 Acceptor 會寫入 `retro-learnings.json`；CLI 將其匯整至 `.4x/learnings.json`。CLI 在產每個角色的 prompt 時，會直接依該角色的 category 從 `.4x/learnings.json` 篩選（active/candidate 分桶配額）並注入——不再有 Designer 先挑選的中介步驟。learnings 完全由 CLI 管理——runner 絕不直接寫入 `learnings.json`，任何 learnings 失敗都只會警告，不阻擋狀態轉換。

```
4x learn add --category <cat> --content <text>  # 手動新增 learning（standalone session 用）
4x learn add --category ops --content "..." --json  # JSON 輸出：{"id":"L0xx","added":true}
4x learn list                     # 列出 active + candidate learnings（預設）
4x learn list --category=testing  # 依類別過濾
4x learn list --status=active     # 依狀態過濾（active、candidate、stale、promoted）
4x learn list --ineffective       # 僅顯示無效條目（used≥3 + 30天 + 同類別持續產出）
4x learn prune                    # 標記陳舊（>90 天未使用）條目並移除
4x learn prune --dry-run          # 預覽陳舊條目但不移除
4x learn promote <id>             # 標記 learning 為已升級（保留但不再注入）
4x learn remove <id>              # 移除一筆 learning 條目
```

`learn add` 會檢查是否有相似的既有條目（完全比對、正規化比對、Jaccard 相似度）。若發現模糊重複，會回報既有 ID 且不寫入。

- 類別：`design`、`code-quality`、`testing`、`review`、`tooling`、`process`、`ops`
- 狀態：`active`（可注入）、`candidate`（新 harvest，待跨 feature 驗證）、`stale`（>90 天未使用，讀取時自動標記）、`promoted`（已升級為模板/指引）
- candidate 條目 ID 後綴帶 `*` 標記；被不同 feature 獨立產出或被 Designer 選中時自動升級為 active
- 無效條目以 `active!` 狀態顯示：已注入 ≥ 3 次、激活 > 30 天、且同類別仍持續產出新 learning，表示該 learning 未能減少重複問題
- 超過 100 筆 active 條目時會顯示軟上限警告，建議執行 `4x learn prune`——不會自動刪除條目

---

## `4x mine`

掃描整個 `.4x/` 歷史記錄，尋找失敗訊號並將其彙整為 `.4x/candidates.json` 候選池。不同於自動發現（只在單次執行的 deep-review PASS 後觸發並解析 `[NEW-FEATURE]` 標記），miner 掃描**所有** feature 以取得最密集的失敗資料：escalation、卡住的 feature、以及反覆出現的 review 失敗。

miner 是純 CLI/protocol 層掃描——不呼叫 LLM，不建立 feature。它只產生候選；候選是否升級為真正的 feature 由 F097 gate 後續決定。

```
4x mine                          # 掃描並寫入 .4x/candidates.json
4x mine --dry-run                # 印出摘要但不寫入
4x mine --min-occurrences 5      # 提高失敗模式閾值（預設 3）
4x mine --output path.json       # 寫入自訂路徑
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--min-occurrences` | `3` | 反覆出現的 review 問題須達到的 distinct feature 數才成為候選 |
| `--output` | `.4x/candidates.json` | 候選池輸出路徑 |
| `--dry-run` | `false` | 只印出摘要，不寫入任何內容 |

三個掃描器餵入候選池，每個都為候選標記 `source` 以供追蹤：

- **escalation** — 讀取每一輪的 `escalation.json`（`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`）
- **stuck** — 卡在 `needs-attention` / `abandoned` / `blocked` 的 feature，從 `state.json` 或最新 escalation 的 `detail` 提取阻塞原因
- **fail-pattern** — 跨**不同** feature 反覆出現的 review / deep-review FAIL 問題（同一 feature 的多輪只算一次），以 Jaccard 相似度聚類，並受 `--min-occurrences` 閾值限制；每個叢集還會產生一筆建議加入 review checklist 的候選 learning

掃描為盡力而為——單一損壞的 feature 只記錄警告，不中止掃描。候選以三種方式去重：對照既有 feature YAML、對照前一份 `candidates.json`，以及在當前批次內部去重。

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

## `4x skills`

管理本 repo `skills/` 目錄中打包的 skill。安裝採 **僅 symlink** 方式 —— 4x 把 `skills/<name>/` 連結到 `~/.claude/skills/<name>`，因此之後 `git pull` 會自動更新 skill，不需重新安裝。請在 4x repo 內執行這些命令（`skills/` 目錄透過從當前目錄往上尋找定位）。

```
4x skills list [--json]     # 列出可用 skill（名稱 + 描述）
4x skills install <name>    # 把 skills/<name>/ 連結到 ~/.claude/skills/<name>
4x skills remove <name>     # 移除 ~/.claude/skills/<name> symlink
```

- `list` 用 `✓` 標記已安裝的 skill，並對 owner-only skill（如 `4x-autopilot`）標注 WARNING。
- `install` 是冪等的 —— 重新安裝已連結的 skill 不做任何動作。它拒絕覆蓋真實目錄或指向別處的 symlink。
- `remove` 只刪除 symlink；絕不刪除 repo 內的檔案，且拒絕刪除真實（非 symlink）項目。

安裝 `4x-autopilot` 會印出 WARNING：它是 owner-only（全自動 merge）。

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

## `4x guard-tool`

<!-- alias: 4x guardtool -->
內部 PreToolUse hook（隱藏，僅供機器呼叫）。`claude` runner 會為 `reviewer`/`deep-reviewer` 角色注入此 hook：當本輪的 review-package.md 存在時，reviewer 自跑的 `git diff`/`git log`/`git show` 會被軟性拒絕，並給出指向 review-package.md 的提示。它從 stdin 讀取 Claude Code hook JSON，從 `FOURX_ROLE` / `FOURX_REVIEW_PACKAGE` 環境變數讀取角色與 package 路徑；任何 parse 失敗或不匹配的指令一律放行（exit 0）。它不會阻斷 build/test/lint 或其他角色，也不會讓執行失敗。

```
echo '{"tool_name":"Bash","tool_input":{"command":"git diff HEAD"}}' | FOURX_ROLE=reviewer FOURX_REVIEW_PACKAGE=/path/to/review-package.md 4x guard-tool
```

---

## `4x mcp`

啟動 Model Context Protocol（MCP）伺服器。

```
4x mcp
```

啟動 4x MCP stdio 伺服器，將 4x CLI 命令以 MCP 工具的形式暴露給 LLM 客戶端（例如 Claude Code、Cursor）。
