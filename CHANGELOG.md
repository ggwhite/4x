# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixes

- **learnings 三閘門互鎖解除，`ineffective` 不再是幾乎必中且不可逆的終態** — `MarkIneffective()` 改名為 `ReevaluateIneffective()` 並改為雙向重評：判定條件 3 從「最近 3 個不同 feature 的條目中有同 category」改為「相似內容（Jaccard ≥ `RecurrenceSimilarityThreshold`＝0.3）來自 ≥ `RecurrenceMinDistinctFeatures`（2）個相異 feature」，條件不再全部成立時旗標自動撤銷。consolidate 的觸發判定與輸入改用 `AllActiveEntries()`（含 ineffective），`consolidate-input.json` 每筆新增 `ineffective` 欄位，consolidate 不再被 ineffective 條目餓死；consolidate 回 0/0 的兩條 no-op 路徑各補一行 `slog.Info`，與 runner 失敗的 `slog.Warn` 可區分
- **learnings store 升到 v2，首次載入會把所有現存 `ineffective` 旗標一次性重設為 false** — 舊規則誤標的條目在 `LoadStore` 時全部撤銷，受影響的條目 ID 記在 store 層級的 `ineffective_reset_ids`，可用 `4x learn list --ineffective-reset` 查詢。重設只改記憶體，磁碟上要等下一次 store 寫入（harvest / prune / add / promote / remove / consolidate）才落地；在那之前每次載入都重跑同一份重設，冪等無害
- **harvest 新增 `(source_feature, category)` 桶上限，單一 feature 不再灌爆 store** — 同一 `(source_feature, category)` 桶最多保留 `MaxPerFeatureCategory`（3）條，超出的在 harvest 時被略過並只記入 log（`harvest skipped over-quota learnings`）。這是使用者可見的靜默行為改變：一輪吐出 8 條同 category 心得只會進 3 條。`4x learn add`（`source_feature = manual`）豁免此上限，既有的手動累積用途不受影響

### Known Limitations

- recurrence 的相似度沿用既有 `tokenize`，以空白切詞，中文內容切不出有效詞元，因此以中文為主的 store 幾乎不會再出現 `ineffective` 條目。本次改動不負責改善中文相似度品質

## [0.5.6] - 2026-07-17

### Fixes

- **手動 retry 不再覆蓋既有 designer/reviewer log** — `roleRoundIter` 迭代計數器原為 in-memory per-process，手動 `4x retry` 開新行程時歸零，使 designer/design-reviewer 在同 round（design-review FAIL 不遞增 round）重跑時從 iteration 1 重新編號、覆蓋前次 run 的 `round-0-designer.log`/`.stream.jsonl` 與 design-rounds 歸檔，真實歷史被抹除、dashboard 亦因 writer/reader 計數基準不一致而錯位。改為 RunLoop 啟動時從 `events.jsonl` 統計已發生的 phase-start 次數 seed 計數器，讓 retry 接續編號（`round-0-designer-4.log`…）不覆蓋

## [0.5.5] - 2026-07-16

### Features

- **Reviewer 一次審完再下 verdict** — design-reviewer 與 reviewer prompt 要求下 Verdict 前跑完所有檢查項度、一次列出本輪所有 blocking issue，不再抓到第一個 FAIL 就收筆，避免問題分批浮現逼出多輪不必要的往返
- **Designer 修訂輪收斂** — design-review FAIL 後，designer 改為注入前一版 task-brief/AC/test-strategy 與 review delta，要求就地 Edit 只修被點名項、未點名段落逐字保留，避免每輪拿完整素材從零重做導致 designer↔design-reviewer 迴圈不收斂

## [0.5.4] - 2026-07-13

### Features

- **AC 驗證證據 untracked/gitignore 警示** — F180：`checkACEvidence` 新增子檢查，ac_checks 命令或 inspection 證據引用的 scope 內路徑若尚未 git-tracked 或被 `.gitignore` 排除，即發出警示，防範 worktree 本地變更未同步回主工作區卻被判為假 PASS（已在 F176、kairos ws-141 各踩過一次真實案例）
- **跨 repo feature 共用根層路徑宣告機制** — F181：Feature 型別新增 `shared_paths`，讓 Designer 可宣告根層共用檔案（如 Dockerfile、docker-compose.yml）允許 Coder 改動，解決 monorepo hub 架構下根層檔案不屬於任何 repos 宣告、Coder 依角色契約主動跳過的反覆踩坑（ws-140/ws-141/ws-228）
- **tier 命名去 Claude 中心化 + init 依 runner 給合理預設** — F182：tier 解析機制本為 runner-agnostic，但預設值全寫死 Claude 命名（`opus`/`sonnet`）；改為依 `default_runner` 給合理預設，`4x init` scaffold 出的 roles 設定不再強制假設使用 Claude
- **Dashboard log 列表顯示 model 名稱** — round log list（`/api/logs/<feature>`）新增每個 round/role 使用的 model，資料源自 `events.jsonl` 既有欄位，與 messages tab 的 modelTag 一致

### Fixes

- **`.cache/` 目錄的 git 追蹤污染清除** — F183：`git rm -r --cached` 清掉加 `.gitignore` 規則前就已被追蹤的 ~2787 個 golangci-lint cache 檔案（保留本地磁碟內容），修掉 lint 每次執行改寫這些檔案導致 reviewer/acceptor 誤判 scope 違規的問題（F178/F180/F181/F182 皆曾踩過）
- **`4x done` merge 失敗時 `git reset --hard` 誤刪未 commit 修改** — F184：`internal/gitops` 的 monorepo/multirepo `Merge` 新增 preflight 檢查，主工作區有未 commit 的 tracked 變更時直接中止合併、不觸碰任何檔案，避免 merge 失敗分支的 `reset --hard` 連帶抹除與本次合併無關的既有修改（本 repo 與 kairos 專案皆曾實測遺失資料）

### Docs

- **README 補上 npx skills install 說明** — 記錄 `4x-audit`/`4x-autopilot` 兩個 skill 的安裝方式；同時將 `release` skill 標記為 internal-only，不隨 npx skills add/list 一併曝光

## [0.5.3] - 2026-07-13

### Features

- **Codex round log 可讀化 + token 統計 fallback** — F178：codex runner 的 `--json` round log（dashboard log viewer / CLI 顯示）逐行轉換成人類可讀文字（agent_message、command_execution 進行中/結果、error、turn.failed），原始 JSONL 檔案內容不變；`ParseRunStatsFromLog` 新增 codex `turn.completed.usage` 解析，作為 rollout 累計值的 fallback
- **Escalation 雙方同意拆分時提早跳出重試迴圈** — F179：design-review-report.md 新增 `Escalation Verdict` 結構化表態（agree-split/disagree/n/a），design-reviewer 主動表態同意拆分時立即轉 needs-attention（新 stopReason `escalation-confirmed`），不用再空轉到燒滿 3 輪才觸發既有的 escalation-loop 偵測

## [0.5.2] - 2026-07-11

### Features

- **Dashboard log/message 搜尋功能** — F177：log 檢視區支援 Ctrl+F 風格搜尋（`<mark>` 高亮、上下筆跳轉、multi-log 跨檔連續編號），message 時間軸支援依內容/role/label 過濾隱藏
- **`4x retry` 支援 `--phase-override`** — 恢復執行時可臨時指定 per-phase runner/model override，原樣轉發給重啟的 `4x run`，格式錯誤時在 `retry` 本身就擋下

### Fixes

- **Worktree Designer repos 欄位未同步回主工作區** — F176：修正 worktree 模式下 Designer 改的 `repos` 欄位無法回寫主工作區 YAML，導致 Designer/Design Reviewer 死循環
- **Dashboard review-fix log 缺圖示 + 同輪 tester log 檔名碰撞** — 補 `review-fix` 角色圖示；平行 review+test 與後續 sequential testing phase 重跑 tester 時改用獨立計數器避免同名 log 互相覆寫

## [0.5.1] - 2026-07-10

### Features

- **Proto/interface fan-out repo 偵測 gate** — F171：新增 gate 自動偵測變更的 proto/interface 定義檔，掃描下游 repo 是否有未同步更新的呼叫端
- **Resume 過期報告偵測** — F172：resume 時比對 report mtime 與最新程式碼變更時間，偵測「報告落後於程式碼」的過期狀態並提示重新產出
- **Dashboard codex 額度百分比** — F173：dashboard 顯示 codex CLI 的額度使用百分比

### Fixes

- **Proto-fanout gate worktree 可見度落差** — F174：修正 gate 在 worktree 內看不到未 provision 的 sibling repo，改用 origin(main) workspace root 定位真實路徑，無法定位者改標「無法驗證」警告而非靜默略過
- **Worktree 路徑安全 fail-open 硬化** — F170：收斂 worktree 路徑解析在異常情境下的 fail-open 行為
- **stuckTitleText 字串前綴嗅探反模式** — F175：`stuckReason` 改用顯式 `(reason, generic bool)` 訊號取代字串前綴比對，避免 fallback 文案改字後控制流靜默失效
- **Dashboard 顯示 bug 三則** — 修正 log 檢視重複顯示、post-scaffold 排序錯誤與缺圖示問題
- **Monorepo scope gate 誤擋** — 移除 F170/F171/F172/F173 誤填的 `repos: ["."]`，解除因此觸發的誤擋
- **codex quiet 模式 log 亂碼** — codex 非-PTY 輸出路徑補套用 `ansiStripper`，避免 dashboard/SSE 顯示殘留 ANSI 方框亂碼
- **Reviewer/Tester 假驗證防範 checklist** — 補三項驗證品質 checklist：escalation self-close 需重跑驗證指令、偵測測試斷言鏡射實作/手動歸零狀態等假驗證反模式、Design Reviewer 引用需 grep/codegraph 核實
- **Worktree 腳本執行位元遺失** — `copyFileIfExists` 保留來源檔案權限，避免腳本複製後遺失執行位元

## [0.5.0] - 2026-07-10

### Features

- **Dashboard bearer-token 認證** — server 預設啟用 bearer-token 認證保護 dashboard API/SSE，可透過設定關閉；修復認證流程中 10 個 post-merge review 發現的缺陷
- **Verify 命令 allowlist 硬化** — `4x verify`/ac_checks 執行命令前可設定 `verify_command_allowlist` 限制可執行前綴，擋下重導向與 command substitution，pipe 下游唯讀過濾工具（grep/awk 等）放行但排除 tee/xargs 避免繞過
- **Role-scope PreToolUse gate** — 即時攔截 spawned role agent 對越權路徑/工具的存取（如 Designer 寫 state.json），比 round 後 `4x check` 更早擋下違規
- **Per-role runner 路由** — 支援不同 role 走不同 LLM runner（如 designer/reviewer 用 codex、coder/accepting 固定 claude），達成跨 model 對抗審查
- **Runner 子程序環境硬化** — spawn LLM runner 子程序時套用環境變數 allowlist/denylist 過濾，降低敏感變數外洩風險
- **State 檔案並發鎖** — `state.json` 讀寫改用 CAS（compare-and-swap）保護，關閉平行/多進程寫入下的復活與 lost-update 窗口
- **check-docs-sync 誤報抑制機制** — 針對已知誤報模式（含 anti-blanket guard 防範過度寬鬆 glob）抑制 `make check-docs-sync` 的雜訊
- **EGPS 執行式 AC 判定** — acceptance criteria 支援綁定實際可執行命令（ac_checks）判定通過與否，並提供假驗證 linter 防堵字串前綴糊弄
- **Worktree 工具環境隔離** — scaffold worktree 時隔離工具環境，避免跨 feature worktree 互相汙染
- **Learnings confidence 與 active 老化** — learning 條目引入 confidence 分數與閒置天數自動降級機制，`4x learn prune` 可清理過時項目
- **Designer 前提挑戰與 docs 刪除 guard** — Designer 角色新增前提假設挑戰步驟，並對刪除 `docs/` 下檔案加 guard 防誤刪
- **Profile advisor** — 依 feature 規模自動建議合適的 profile（lite/quick/normal/full），`4x new` 建立時可參考
- **安裝驗證與 screenshot 路徑硬化** — 安裝腳本加 checksum 驗證，dashboard screenshot 端點加路徑穿越防護
- **codex 額度百分比觀測** — 解析 codex CLI 的 `rate_limits` 用量（5 小時窗/週窗百分比），整合進 `4x cost`/`4x status`/events.jsonl
- **`4x retry` 自動偵測復原 phase** — 未帶 `--to` 時，改由 state.json 記錄的卡住前 role 自動推導正確復原 phase（不再一律跳 accepting）
- **Feature 規模紅線與 `--subtask` 格式同步** — `4x new`/CREATOR 流程加入規模超標警示，`--subtask`/`--rule` 改用 `StringArrayVar` 避免逗號誤切值

### Fixes

- **Designer guard 豁免收斂** — orchestrator 於 run-start 寫入的 status 轉換（not-started→in-progress）改記錄進 state.json 供 guard 比對來源，取代單純比對數值，防止 Designer 偽造同值變更逃過檢查
- **平行 review/test 路徑 CAS 補洞** — `RunReviewTestParallel` 的狀態寫入改走 CAS 護欄，關閉外部 done/abandon 期間舊快照復活 feature 的競態
- **允許清單阻擋訊息誤導修正** — allowlist 擋下的 ac_checks 命令不再誤導成「加大 --timeout 重跑」，改給出正確的設定引導
- **受保護路徑測試檔 diff-budget** — `*_test.go` 變更改套用獨立（5 倍）上限，避免完全免計而被用來夾帶未受審變更
- **多處 post-merge review 缺陷修復** — F157/F158/F159/F162/F163/F164/F165 各自的 post-merge 審查發現逐輪修復完畢（含 false-deny、denylist 涵蓋、canonical key 驗證、CAS lost-update 補洞等）
- **重複邏輯合併** — `formatPct`、`checkBuildGate`/`checkDocsGate`、tracked/untracked git 偵測等多處重複實作合併為共用 helper

## [0.4.0] - 2026-07-08

### Features

- **`4x cost` 成本觀測指令** — 彙整 run 產出的 stream log 成本資料，支援 per-role / per-round / per-feature 分群與 JSON 輸出；以 `logs/*.stream.jsonl` 為主要資料來源，`events.jsonl` 為 fallback
- **Amending 輪 delta 注入** — reviewer/tester FAIL 後的 amending 輪，prompt 只注入失敗項與 verdict delta（而非全文），減少重複 token 消耗
- **Reviewer 探索行為硬約束** — review-package.md 預附變更檔全文（100KB 共享預算），搭配 `4x guard-tool` PreToolUse hook 攔截 reviewer/deep-reviewer 的 `git diff/log/show` 探索指令，軟性引導改讀 review-package
- **CONDITIONAL PASS 同輪收斂** — reviewer 給出 CONDITIONAL PASS 時，同輪內由 mini-coder 修復殘留 issue 後重跑 reviewer，避免不必要的 amending round
- **Recovery 尊重人為 phase 介入** — `SmartResumePhase` 新增 `ManualPhase` 欄位，`4x transition` / `4x retry --to` 設定後 recovery 不再覆蓋
- **Worktree 環境完整性** — worktree scaffold 後自動裁切 `go.work` 只保留 scope repo，並支援 `settings.json` 宣告式 `post_scaffold_hooks`
- **Roles 白名單擴充** — `ConfigurableRoles()` 納入 fixer 與 mini-coder，支援 per-role model 路由；`4x doctor` 與 `roleCategoryMap` 同步更新
- **Learnings 生命週期老化** — candidate 且 `used_count=0` 超過 `candidate_max_idle_days`（預設 30 天）自動標 stale；`4x learn prune` 可清理過時 learnings
- **Check scope 誤判修正** — `hub_repos` 白名單排除 scope 檢查、tester 對 e2e repo 放行、diff 基準改用 merge-base 避免跨 branch 誤報
- **Profile-aware 角色 prompt** — 渲染時注入 profile phase 清單與角色產出物契約，解決 Tester 越界寫 final-report、Acceptor 在精簡 profile 拒收等問題
- **4x-audit reconcile 收尾** — audit skill 新增 Step 6 Reconcile（回標 gaps、4x clean、learn remove），Source 2 只掃非 done feature 降噪
- **Designer/Reviewer template 強化** — Designer 加 AC checklist 與 Premise Verification 段；Reviewer 加 Evidence Requirement 段

### Fixes

- **Pre-existing lint debt 清零** — 移除 4 個 unused function + 1 個 staticcheck（`fmt.Sprintf` 無格式化參數）
- **Tester parallel_review_test 拒跑** — tester prompt 加 parallel 模式說明，避免 state.json 顯示 reviewing 時 tester 拒絕執行
- **`conditional-pass-residual` event 登錄** — 補登到 `AllEventTypes()` 與 `event.schema.json`
- **Retry log 覆蓋** — retry 時 log 改用 append 避免覆蓋舊紀錄
- **`--timeout` 預設值** — 移除 1 小時預設限制，改為不限時

### Internal

- **Token 優化 Batch A** — 移除 learnings 雙重注入（每 role 省 ~3k tokens）、CodeMap 只給 designer/coder/reviewer、六模板 learnings JSON 壓單行
- **Model 路由文件** — 新增各 role × 各 runner 建議模型對照表

## [0.3.15] - 2026-07-08

### Features

- **Designer 可修改 repos** — Designer 分析 codebase 後可直接修改 feature YAML 的 `repos` 欄位（僅限 repos），不需 escalate 浪費一輪；guard 層新增 `checkDesignerYAMLMod` 阻擋非 repos 欄位的修改，Design Reviewer 同步檢查變更合理性
- **`4x status --json` 補齊欄位** — 加入 `priority`、`profile`、`depends`、`pid` 欄位，讓外部工具（autopilot skill）可直接從 JSON 取得完整資訊

### Fixes

- **狀態機圖對齊** — CLAUDE.md 的 ASCII 狀態機圖補上 `fixing` 階段及所有合法轉換邊，修正過時的 `types.go` / `feature_list.json` / `progress.md` 引用
- **角色表補齊** — 全部 7 個 plugin 的角色合約表補上 Deep Reviewer 和 Acceptor
- **Architecture 樹狀圖更新** — 補上 orchestrator/doctor/feature/gitops/prompt/opencode 等子目錄
- **opencode runner 註冊** — `plugin_install.go` 新增 opencode runner 的 init 安裝邏輯
- **Template 命名修正** — `consolidate.md.tmpl` 重命名為 `consolidator.md.tmpl` 對齊 role 常量
- **Dashboard DAG 間距** — DAG 視覺化加 `mt-8` 修正與上方 section 的間距

### Internal

- **移除 batch 指令** — `4x batch`（plan/next/run/stop）由 4x-autopilot skill 完全取代；刪除 CLI 子命令、`internal/batch/` package、server batch handlers/process、protocol batch types、dashboard batch panel（含 CSS 和 6 語系 i18n keys）；server resolver 簽名從 `(ws, pm, bm, err)` 簡化為 `(ws, pm, err)`；淨刪約 6,000 行
- **Dashboard batch panel 移除** — 主畫面移除批次控制面板，圓環圖/回合分佈/最近完成直接接在 stats 下方，DAG 移至底部

## [0.3.14] - 2026-07-07

### Features

- **Learnings 獨立檔名** — 同 round 內各角色的 learnings 改為寫入 `{role}-learnings.json`（如 `coder-learnings.json`），harvester 以 glob 收割全部，不再被最後一個角色覆寫；向下相容舊的 `role-learnings.json`
- **Resume/Phase transition 對齊** — `SmartResumePhase` 加入 `ProfileConfig` 參數，deep-review PASS 後會依 profile 決定走 Fixing 或 Accepting，與 live-run 路徑行為一致
- **驗證工具強化** — `check-docs-sync` 對刪除/搬遷的檔案自動 grep docs 內舊路徑、未 commit 的變更加 WARNING 提示；`check-guide-i18n` heading 數量不符時列出雙方具體 heading 供對照
- **Coder 驗證 Guardrail** — 框架在 coder 完成後自動跑 `make check-docs-sync` + `check-i18n`，結果寫入 `docs-gate.json` artifact，reviewer 不再需要手動重跑驗證
- **Runner 層刪舊產出** — runner 執行前先清除上一輪殘留的產出檔（如 `gate-verdicts.json`），讓 runner 回 nil 卻沒寫新檔時退化為明確的 parse error，不再靜默吃 stale data
- **4x skills CLI** — 新增 `4x skills list/install/remove` 子命令，以 symlink 管理 repo `skills/` 目錄下的 skill 安裝到 `~/.claude/skills/`；owner-only skill 安裝時顯示 WARNING

### CI

- **CI Release 簽名公證** — release workflow 改用 `make dashboard-release`，匯入 Developer ID 憑證做 codesign + notarization，GitHub Release 產出的 .dmg 不再被 Gatekeeper 擋

### Internal

- **Dashboard macOS app 簽名與公證** — 本地 `make dashboard-release` 支援 Developer ID Application 簽名 + notarytool 公證 + staple，產出可直接分發的 .dmg
- **4x-autopilot / 4x-audit skill 搬進 repo** — skill 檔案從外部搬入 `skills/` 目錄，搭配 `4x skills install` 使用
- **Token 優化** — 精簡 role template 與 prompt 注入，降低每輪 token 消耗

## [0.3.13] - 2026-07-06

### Features

- **角色範圍外發現的非阻斷回報管道** — plugin 角色契約新增 Scope Gaps 章節：角色發現明確需要另開 feature、但不阻塞目前任務的範圍外問題時，append 一行到 `docs/reference/discovered-feature-gaps.md`，不自行擴大 scope 或呼叫 `4x new`，作為給人審核的候選清單。`4x init` 建立的新專案開箱即有這條規則

### Fixes

- **4x sync 偵測不到 learnings-context import 遺失** — `comparePlugins()`/`syncPlugins()` 只要 plugin 檔案本身與其 import 行都已是最新，就永遠不會偵測到 `@.4x/learnings-context.md` 這行 import 缺失，導致 `4x learn` 累積的教訓可能從未真正送進任何角色的 prompt，且沒有任何警告；現在會一併檢查這行
- **Learnings 選取偏誤，下游角色可能收不到任何過去教訓** — 過去 Designer 讀取全部 active+candidate 學習條目時 active 固定排在最前面，造成 candidate 幾乎選不上；選出的清單又依各角色自己的 category 再過濾一次，但各角色 category 幾乎沒有交集，導致 Reviewer/Tester 等角色常常收到 0 筆相關教訓。改為每個角色依自己的 category 直接篩選、active/candidate 各有保底名額並交錯呈現，同時補上 design-reviewer/fixer 兩個角色原本從未渲染過的 Past Learnings 區塊

### Docs

- **README 補上 Issue-First MR Flow、landing page 標註 cost tracking 限制** — README Key Features 表格新增 Issue-First MR Flow 一列（先前只寫在 concepts.md，README 看不到），5 語言翻譯同步；landing page Cost Visibility 區塊標註目前只有 Claude Code runner 會回報花費，其他 runner 尚未支援，6 語言 i18n 同步

## [0.3.12] - 2026-07-05

### Features

- **最近完成列表顯示花費** — dashboard「最近完成」卡片標題列顯示所有已完成 feature 的加總花費，每一列也顯示各自金額

### Fixes

- **Worktree 開啟前自動同步本地分支** — `4x run` 開 worktree 前直接用當下 checkout 的 local ref 建立分支；issue-first MR 流程下合併發生在 GitHub/GitLab 端不會更新本地，本地 main 忘記 pull 太久時新 feature 就會從過期的 base 分岔。現在會先 fetch + fast-forward 到 upstream tracking branch：本地落後就同步到最新，本地與遠端已分岔或本地領先（有未推送 commit）時印警告但不強制覆蓋任何本地 commit
- **Dashboard log 偶爾出現亂碼** — tool_use 摘要（如 Bash 指令）截斷邏輯用 byte 位移切字串，遇到中文等多位元組字元會從中間切斷，寫入 log 後在 SSE 轉發時被替換成亂碼方塊；改為在 UTF-8 字元邊界截斷
- **Messages 分頁重複顯示總花費** — Messages tab body 內有一行跟 header 重複的總花費顯示，已移除

## [0.3.11] - 2026-07-03

### Features

- **Dashboard 總花費顯示於分頁列** — 總花費徽章固定顯示在總覽／訊息／日誌／截圖分頁列右側，不論目前在哪個分頁都看得到，不用切到訊息頁才看得到

### Fixes

- **Round 產物同步遺漏子目錄** — worktree 收尾同步只複製 round 目錄下的頂層檔案，子目錄（如截圖）被靜默丟棄；加上 Designer 讀不到截圖路徑慣例只能自行編造路徑，兩者疊加導致截圖在 `4x done` 清除 worktree 後永久遺失。改為遞迴複製子目錄，Designer 直接引用 settings.json 解析後的實際路徑
- **Profile／Model Tier 下拉選單缺內建選項** — 專案 settings.json 沒自訂 `profiles` 時 Default Profile 下拉只剩 None 可選；Profile 編輯器的 Model Tier 選單原本寫死 light/standard/pro，跟系統實際 tier 名稱（opus/sonnet）對不上，選了不會生效
- **Runner 設定新增 Model Tier 編輯器** — 新增自訂模型（如 Fable 5）原本得手改 JSON；新增 key-value 編輯器可直接新增/刪除 tier。同時修正儲存表單時會把 `profiles`、`evolution`、`self_mod_guard` 等表單沒管理到的欄位靜默清空的資料遺失問題
- **平行 deep review 成本／時長歸屬錯誤** — 多個平行 sub-reviewer 共用同一個角色標籤時，事件配對邏輯讓所有 run-end 收斂到同一格互相覆蓋，導致 dashboard 只顯示 synthesizer 自己的成本、其餘 sub-reviewer 的日誌時長被誤算成離譜的數字。新增明確的事件序號解決配對問題

## [0.3.10] - 2026-07-03

### Features

- **Issue-first MR flow（F127）** — 新增 `.4x/settings.json` 的 `issue_tracker.enabled` 開關，開啟後 `4x new` 會依 repo 的 git remote 自動判斷 GitHub/GitLab 並建立（或用 `--issue` 連結既有）issue，`4x done` 改為 push branch + 開 MR/PR（帶 `Closes #issue`）取代本地 squash-merge，交由平台走 code review 流程
- **結構化 e2e 截圖驗證（F128）** — `test-strategy.yaml` 的 `ac_verify_map` 新增 `e2e-screenshot` 合法值，guard 在 testing→accepting 關卡機械驗證宣告此類型的 AC 確實留有截圖檔案，避免條件式的截圖需求被 tester 悄悄跳過
- **CLI 與 dashboard 總花費更準確（F129）** — 新增 `Workspace.TotalCost` 直接加總 `events.jsonl` 所有 run-end 事件成本；CLI resume 中斷過的 feature 時用它 seed 回總價；dashboard「訊息」頁新增這個權威總價顯示

### Fixes

- **平行 review/test 成本未累加** — reviewer/tester 平行執行時的 cost/tokens 只寫進 events.jsonl，未累加進行程內的總花費統計，導致開啟 `parallel_review_test` 時 CLI 結尾印出的總價低估（實測案例：印 $12.36，實際 $18.90）
- **Designer 遇到開放式問題不再卡住** — Designer 是一次性、無人可回應的 headless 執行，過去偶爾會在產出必要檔案後反問使用者選項，導致 feature 被誤標 needs-attention；新增 Escalation 區塊教它改寫 `escalation.json`

## [0.3.9] - 2026-07-02

### Fixes

- **Screenshot 探索遇到損毀 verify.json 整個掛掉** — 單一 round 的 verify.json 若混進未跳脫的 raw ANSI escape code（例如捕捉到的 subprocess 輸出未跳脫），JSON 解析失敗會讓整個 DiscoverScreenshots hard-fail，連帶讓 `4x status`/`4x check` 對整個 feature 失效。改為 best-effort：跳過該 round 的截圖，其餘 round 照常處理
- **Dashboard 日誌分頁內容裁切與 retry log 缺 icon** — 切到「日誌」分頁時外層容器缺 `min-height:0`，長 log 內容被外層 `overflow:hidden` 直接裁掉而非交由內層捲動；同一 round 內重跑第 2 次以上的角色 log（如 `round-0-designer-2.log`）因判斷正則只涵蓋 deep-* 角色，圖示與顏色消失

## [0.3.8] - 2026-07-02

### Fixes

- **Design-review loop 迭代覆寫 log/message** — design-reviewing FAIL 打回 designing 時 round 計數器不遞增，導致每次循環的 log 檔名與 task-brief/design-review-report 互相覆寫，dashboard 只能看到最後一輪。Log 檔名與訊息區改為依 (round, iteration) 分別歸檔並列出每一輪
- **Multi-repo worktree scope/guard 邊界情況修復（F126）** — hub_repo（如共用 docs）一律排除於 scope violation 判斷；build-gate 對缺失指令（如聚合腳本非每個 repo 都有）優雅降級為 skip 而非硬 fail；Tester prompt 補教 `expectedExitCode` 欄位用法；`DeferRunCleanup` 補上 `needs-attention` phase，並修正 exit-0 後收尾失敗被誤標為 process crash 的競態根因
- **build-gate 缺指令判斷在 Linux CI 失效** — 上一項的優雅降級只認 macOS `/bin/sh`（bash 相容）的 `"command not found"` 措辭，Linux 預設的 dash 印的是不含 "command" 字樣的 `"not found"`，導致同一段邏輯在 GitHub Actions（Ubuntu runner）上完全打不中。改為比對兩種 shell 共有的 `"not found"` 子字串，仍綁定 exit code 127 才生效

### Docs

- **Landing page v2 改版** — 更新過時統計數字與截圖，新增 Multi-repo & Worktree Isolation、Quality Gates、Cost Visibility 三個 section，設定範例改用 4x/Kairos 真實專案設定，6 語言（en/zh-TW/zh-CN/ja/ko/es）完整翻譯

## [0.3.7] - 2026-07-01

### Fixes

- **Multi-repo worktree scope 誤判** — multi-repo feature（如同時涉及多個獨立 git repo）的 worktree 容器目錄本身不是 git repo，`ScopeRoot` 會誤判為非 linked worktree 並靜默 fallback 回主工作區，導致 build-gate 和 symlink 檢查掃到主 repo 既有的無關問題，而非 feature 自己 worktree 內的變更。新增 `ScopeRoots`，容器目錄非 linked worktree 時改為逐一掃描子目錄找出各 sub-repo 的 linked worktree；mono-repo 行為不變

## [0.3.6] - 2026-07-01

### Features

- **Protocol package 拆分（F121）** — `internal/protocol/types.go` 拆分為 domain.go / config.go / config_resolve.go / batch.go / review.go / state.go / verify.go 等單一職責檔案，降低耦合、提升可維護性
- **Schema 單一事實來源同步機制（F122）** — 新增 `schemasync` 套件，驗證 Go struct 與 `schemas/*.schema.json`、`dashboard/web/core.js` 三邊欄位一致；`make check-schema-sync` 納入建置流程防止 drift
- **batch.go 拆分與 runnerFactory 去重（F123）** — batch 執行邏輯拆分，並抽出共用 runnerFactory 消除重複的 runner 建構程式碼
- **server 子程序啟動抽介面（F124）** — server 子程序啟動邏輯抽出介面，方便測試與未來替換實作
- **Dashboard port 單一設定來源（F125）** — dashboard port 改為單一設定來源，避免多處硬編碼不一致

### Fixes

- **Build-gate 限定 feature 自己的 worktree** — build-gate 的 lint/build 檢查範圍限定在該 feature 專屬 worktree 內，避免誤判其他 worktree 的問題
- **Dashboard role 顯示同步** — 補齊 dashboard ROLES 對照表缺少的 fixer / mini-coder / re-verifier / gate / consolidator / round-summarizer 角色顯示
- **ac_verify_map verify_type 提前驗證** — Guard 在 `Check()` 一開始就驗證 `verify_type` 合法性，避免後續流程誤用未定義型別
- **check-docs SIGPIPE** — CI 的 check-docs grep 改用 herestring，避免 SIGPIPE 導致誤判失敗
- **Acceptor message 補齊 cost/duration** — acceptor 訊息補上 model/duration/cost 欄位
- **Deep review run-end 補齊 cost/tokens** — deep review 的 run-end 事件補上 cost_usd 與 token 欄位

### Internal

- **Coder 預設 tier 改為 sonnet** — coder role 預設模型層級從 opus 切到 sonnet，降低成本

## [0.3.5] - 2026-06-30

### Fixes

- **Build-gate PATH 擴充** — GUI 啟動的 4x（如 dashboard）不繼承 shell profile，build-gate 和 hook 子程序找不到 go/node 等工具；抽出共用 `envutil.EnrichedEnv()` 補上常見路徑（含 `/usr/local/go/bin`、`$HOME/go/bin`）

## [0.3.4] - 2026-06-30

### Features

- **AC verify type 標記（F120）** — Designer 在 acceptance-criteria.md 標記每條 AC 的驗證類型（unit-test / integration / inspection / skip），Guard 依標記 enforce Tester evidence 品質；無 ac_verify_map 時維持原有行為
- **Learning effectiveness tracking（F119）** — 新增 `ActivatedAt` 欄位、`Ineffective` 標記、`MarkIneffective()` proxy 指標偵測無效 learning；CLI 支援 `learn list --ineffective`
- **learn add CLI（F116）** — 新增 `4x learn add --category --content` 指令與 MCP `4x_learn_add` tool，含 `FindSimilar()` fuzzy 重複偵測
- **Candidate learnings（F117）** — 跨 feature 重複出現的 learning 自動從 candidate 升級為 active；`learn list` 預設顯示 candidate
- **Learn context snapshot（F118）** — `4x learn context` 產生 `learnings-context.md` 摘要，harvest 後自動重新產生
- **ops category（F115）** — learning 新增 `ops` category，涵蓋環境、工具、帳號等操作知識
- **Coder build gate（F114）** — Coder 完成後 Guard 驗證 build 通過才放行
- **force-done 指令** — `4x force-done --reason` 強制跳過 pipeline 將 feature 標記 done
- **Guide i18n 同步** — 新增 `check-guide-i18n` 腳本，同步所有語系的 CLI guide 翻譯

### Fixes

- **Guard backward compat** — 無 ac_verify_map 時只做基礎檢查（passed + evidence 非空），不強制 execution pattern，不破壞既有 feature
- **Guard YAML error 處理** — test-strategy.yaml 解析失敗改為 warning 繼續檢查，不再 early return 跳過所有 AC 驗證
- **executionPattern 正則** — PASS/FAIL 加 word boundary 避免 BYPASS/COMPASS 等誤匹配
- **Guard ReadTestStrategy 單次讀取** — checkACEvidence 和 checkManualChecks 共用同一次讀取結果
- **ActiveEntries 過濾 Ineffective** — 標記為 ineffective 的 learning 不再注入 role prompt
- **hasCategoryContinuation** — 只計 active/candidate entries、跳過空 SourceFeature、要求 entries 比 target 更新
- **HarvestLearnings context 重新產生** — ineffective 標記變更時也觸發 learnings-context.md 更新
- **learn list --ineffective 可組合** — --ineffective 和 --status 現在可同時使用
- **force-done 修正** — 直接 transition 到 Done（universal target）修正從大多數 phase 失敗的問題；加入 self-mod guard 檢查；PendingReview 分支也記錄 reason

## [0.3.3] - 2026-06-30

### Features

- **manual_checks 強制機制** — Designer 可在 `test-strategy.yaml` 定義 `manual_checks`（id/description/steps），Guard 驗證 Tester 必須實際執行並在 `verify.json` 提供 `manual_check_results` 與 evidence，讀 code 判定不再算數
- **Guard retry 自癒** — Tester guard 失敗若為 retryable（manual_checks 沒執行、AC evidence 為空），自動帶 `guard-feedback.json` 重跑一次，不再直接進 needs-attention 等人介入
- **Tester 反模式提示** — Tester template 新增「你不是 Reviewer」區塊，明確列出 Reviewer 已做的事、定義合格 evidence 必須來自實際執行

### Fixes

- **Guard retry 防循環** — 三層防線避免無限重試：同 round 最多 1 次、跨 round 累計上限 2 次（`MaxGuardRetries`）、加上既有 maxRounds / ConsecutiveNoProgress

## [0.3.2] - 2026-06-29

### Features

- **Fixer role（F113）** — 在 deep-reviewing 與 accepting 之間新增 fixing phase，Fixer 角色自動修補 deep-reviewer 指出的可改問題，減少人工介入
- **4x retry 指令** — 新增 `4x retry` 指令，讓 `needs-attention` / `blocked` 狀態的 feature 能直接重跑，無需手動重置
- **Verify fallback（F112）** — `4x verify` 在無 `test-strategy.yaml` 時改用 `settings.json` 的指令做 fallback，避免驗證空跑
- **Golangci-lint 整合** — `make lint` 新增 gofmt 檢查與 golangci-lint（nolintlint / exhaustive / bodyclose / govulncheck），並補齊既有 lint 問題

### Fixes

- **並行 role cost 顯示** — Dashboard 並行跑的 Reviewer / Tester 現在正確顯示金額與執行時間（`run-end` 事件補上 `cost_usd` 和 `duration_ms`）
- **Acceptor needs-attention** — 修正 Tester `ac_results.passed` 與 Acceptor needs-attention 未被 CLI 驗證的問題
- **CheckVerify 一致性** — `CheckVerify` 加入 `ac_results` 一致性檢查，避免謊報自動落 needs-attention
- **Acceptor 模板守衛** — acceptor 模板對 review/test-report 加 `if present` 守衛，避免缺檔時報錯

## [0.3.1] - 2026-06-28

### Features

- **Coder 讀取驗收標準** — Coder prompt 新增 `== Acceptance Criteria & Test Strategy ==` 區塊，明確要求在寫程式前讀取 `acceptance-criteria.md` 與 `test-strategy.yaml`，避免跳過驗收標準
- **訊息 tab token 用量** — 訊息 tab 顯示每個 role 的 token 用量與金額，方便追蹤成本
- **Deep-review 自癒路徑** — deep-review FAIL 時也觸發 auto-discover，確保自癒機制在所有耗盡路徑都有效
- **Auto-summarize amend rounds** — 進入 accepting 前自動摘要過去的 amend rounds，減少 context 長度

### Fixes

- **舊格式 feature ID 相容** — numRe 相容無 slug 的舊格式 feature ID（如 ws-094）
- **Deep-reviewer Discovered Issues** — 強制寫出 `## Discovered Issues` section，避免欄位缺漏
- **Reviewer symbol 驗證** — reviewer 驗證 symbol 時改用 `codegraph_explore`，禁止 grep 避免誤判

### Internal

- **Codegraph 工具規則** — 加入 codegraph/graphify 工具規則與 PreToolUse hook
- **Opus runner tier** — 移除 [1m] context suffix，避免 runner tier 辨識問題

## [0.3.0] - 2026-06-27

### Features

- **AC evidence mapping** — `verify.json` 新增 `ac_results` 欄位，每個 acceptance criterion 必須有對應 evidence；`4x check` 阻擋缺 evidence 的通過
- **Selective deep review** — Deep review 根據 diff 影響的檔案路徑自動選擇相關角度（而非每次跑全部 11 個），減少小 diff 的 token 浪費。支援 feature YAML 或 CLI flag 強制全角度
- **Auto-consolidate learnings** — Feature 完成後若 active learnings 超過 30 條，自動呼叫 AI 判斷語意重複並合併/移除，避免 prompt bloat
- **Cost per phase** — Dashboard 顯示每個 phase 的 token 消耗與耗時

### Refactoring

- **Extract orchestrator** — `cmd/4x/run.go` 的 run loop 下沉至 `internal/orchestrator/`，拆為 orchestrator / phase / deep_review / parallel / artifact / resume / worktree / hook 八個檔案
- **Extract prompt package** — Prompt 組裝邏輯從 `cmd/4x/` 下沉至 `internal/prompt/`，900+ 行搬遷
- **Split workspace** — `internal/protocol/workspace.go` 拆為 workspace / workspace_state / workspace_config / workspace_feature / workspace_batch / workspace_screenshot 六個檔案
- **Internal tests** — 補齊 `internal/` 各 package 的測試覆蓋

### Fixes

- **Error handling** — 修正多處 `_ = err` 靜默吞錯的問題，改為 log 或回傳
- **Feature profile priority** — Dashboard run dialog 正確優先使用 feature YAML 的 profile 而非 default_profile
- **Phase order** — 修正 testing 與 deep-reviewing 的順序

### Internal

- **Profile config** — 重新設計 profile 系統，pin claude opus 至 4.6[1m]，review/test/accept 改用 sonnet 降低成本
- **Learnings consolidation** — 手動合併 6 條語意重複的 learnings（44 → 38）

## [0.2.6] - 2026-06-26

### Token Optimization

本版聚焦降低 4x 每次 run 的 token 消耗。full profile 單輪約 ~500K tokens，
優化後 lite + codebase map 可降至 ~210K（-58%）。

| 情境 | Token 估算 | vs 原始 |
|---|---|---|
| full（原始） | ~500K | 100% |
| full + codebase map | ~430K | 86% |
| **lite + codebase map** | **~210K** | **42%** |
| quick | ~140K | 28% |

### Features

- **Built-in lite profile** — `--profile lite`（designing → coding → testing），跳過所有 review 層，token 約 full 的 40%。適合中等風險功能
- **Token usage 顯示** — 每個 phase 結束顯示 token 數與耗時，run 結束顯示總計。Event 新增 `tokens_used` / `duration_ms` 欄位供 dashboard 使用
- **Amending baseline diff** — Amending 時 coder prompt 自動帶入從 baseline 到目前的 git diff（上限 200 行），不用重新探索上輪改了什麼
- **Codebase map 注入** — 自動掃描 Go 專案的 exported symbol，產出每 package 一行的精簡索引（~20 行），注入 coder/designer/reviewer/tester prompt。Agent 不用花 token 探索目錄結構
- **Configurable feature ID** — 支援自訂 feature ID 前綴與位數

### Refactoring

- **runContext 重構** — 從 run.go 抽出 `runContext` struct 收納 8-10 個重複傳遞的參數，主迴圈從 475 行縮減為 ~30 行的 method call 序列
- **server.go 路由簡化** — 引入 `wsRoute`/`pmRoute`/`bmRoute` helper，380 行重複的 handler 註冊收斂為簡潔的 route 宣告
- **prompt.go 去重** — CLI command 改為呼叫 `generatePrompt` 而非重複建構 promptData
- **batch/evolve/deep-review** — 抽出 helper 函式降低巢狀與重複

### Fixes

- **Worktree scope** — worktree 模式跳過非 feature repo 避免 false scope violations
- **Dashboard sort** — Design-reviewer log 排序修正

## [0.2.5] - 2026-06-25

### Features

- **Plan 精簡注入** — Coder prompt 只注入 plan 的架構段落與 Task 標題，去掉詳細 step checkbox（每 feature 省 ~30KB）；Designer 跳過時自動給全文
- **Artifact 內嵌** — Task-brief 和 amending 的 review/test feedback 直接嵌入 coder prompt，省掉 2-3 次 Read tool call
- **CONDITIONAL PASS 容忍 warning** — Review verdict 為 CONDITIONAL PASS 且無 critical issue 時不再觸發 amending，減少不必要的重跑

### Fixes

- **Convention file 重複注入** — `project.includes` 明確列出的檔案（如 CLAUDE.md）在 runner 自動讀取時仍被注入，現在正確跳過
- **空行壓縮** — Prompt 最終輸出統一壓縮連續空行，避免 template 條件區塊產生的多餘空行浪費 token

### Internal

- **Coder template 強化** — 新增 Edit over Write、不重複 Read 的約束規則
- **Coder instructions 前置檢查** — 加入 docs-sync 和 error handling 提醒，減少 reviewer FAIL 觸發的 amending

## [0.2.4] - 2026-06-24

### Features

- **MCP server full coverage** — 補齊所有 CLI 子指令的 MCP tool 對應（approve、reject、gate、evolve、mine、clean、config 等），dashboard 與 MCP client 可操作完整功能

### Fixes

- **Dashboard done 不 commit learnings** — Dashboard 按 Done 按鈕走的 API 路徑遺漏了 `commitLearnings` 呼叫，learnings.json 變更永遠不會被 commit
- **Batch plan 排入 abandoned feature** — CLI `4x batch plan` 只排除 done 狀態，abandoned feature 仍被排入計畫（server 端已正確排除）
- **CLI status 顯示 stale active** — `4x status` 未呼叫 `ReconcileActive`，已死的 runner 仍顯示為 active
- **Transition 讀不到 user config** — `4x transition` 用 `ReadConfig` 只讀專案設定，user config 的 hooks 不會生效，改為 `LoadMergedConfig`
- **Batch failure 原因未顯示** — Dashboard batch 失敗時未顯示具體錯誤原因
- **Homebrew formula 命名** — Ruby class 不能以數字開頭，formula 改名為 `fourx`
- **Verify.json 訊息混淆** — 區分 verify.json missing 與 verify failed 的 escalation 訊息

### Internal

- **Dedupe CLI vs server** — 抽取 `Workspace.WriteBatchStop()`、刪除 `transitionDone` dead code，統一 CLI 與 server 共用邏輯

## [0.2.3] - 2026-06-24

### Features

- **Symlink guardrail warning** — `4x check` 偵測到 scope 內含 symlink 時發出警告，Coder plugin 新增禁止 `git add .` 規則避免意外加入非預期檔案
- **Batch stop toast** — Dashboard 發送 batch stop signal 後顯示 toast 通知

### Fixes

- **Runner PATH precedence** — 修正 `4x live` 啟動的 agent 可能吃到系統安裝的舊版 `4x` 的問題，exe 目錄改為 prepend 到 PATH 最前面

### Internal

- **Server/run module split** — 拆分 run.go 和 server.go 為更聚焦的模組
- **Dashboard constants dedup** — 合併重複的 dashboard 常量，移除無用的 settings 程式碼

## [0.2.2] - 2026-06-23

### Features

- **Profile-aware templates** — 跳過 Designer 的 profile（normal/quick）不再讓 Coder 找不到 task-brief，template 自動引導 role 從 feature YAML 讀取需求與驗收標準
- **Coder learnings selection** — 跳過 Designer 時由 round 1 Coder 承接 learnings 選擇責任，從全部 active learnings 中挑出相關的寫入 selected-learnings.json，後續 role 正常消費
- **Auto-commit learnings.json** — feature merge 成功後自動 commit learnings.json（若有變更），batch 與手動 `4x done` 兩條路徑皆適用，避免累積的 learnings 遺失
- **Batch replan API** — Dashboard 新增「重建計畫」按鈕，新增 feature 或調整 profile 後可重新產生 batch-plan.json，無需重啟 batch
- **`.4x/.gitignore` on init** — `4x init` 自動產生 `.gitignore`，排除 runtime artifacts 避免誤 commit
- **`deep_model` fallback** — deep_model 未設定時自動 fallback 到 opus tier，不再報錯

### Fixes

- **Screenshot path** — 修正 working directory 重構後 screenshot_dir 路徑未更新的問題
- **Dashboard profile tag** — feature 卡片的 profile tag 改為同時讀取 state 與 feature YAML，未跑過的 feature 也能正確顯示

### Docs

- **Global agent rules** — 新增持久化 4x 知識的 global agent rules 設定範例

## [0.2.1] - 2026-06-23

### Features

- **OpenCode runner** — 新增 OpenCode CLI 作為 supported runner，支援 75+ LLM provider，一個 runner 即可切換不同家的 model

### Internal

- **Feature working directory restructure** — 將 feature runtime artifacts 從 `.4x/{id}/` 移至 `.4x/run/{id}/`，分離 config 與執行期檔案，保持 `.4x/` 目錄整潔

### Docs

- **macOS Gatekeeper & Windows SmartScreen** — 新增安裝後解除系統安全封鎖的操作說明

## [0.2.0] - 2026-06-22

### Features

- **Self-evolution pipeline (`4x evolve`)** — 從歷史 run 挖掘失敗訊號，自動產出候選 feature，經 value gate 篩選後排入 backlog 並跑完整 loop；支援 dry-run 預覽、pre/post veto、convergence 偵測
- **Evolution value gate (`4x gate`)** — 候選 feature 須通過 LLM 價值評估（value score 閾值 + 反 hack 論述），防止低品質或自我膨脹的 feature 進入 backlog
- **History miner (`4x mine`)** — 掃描 escalation、stuck feature、跨 feature 反覆出現的 review FAIL pattern，彙整為候選池（candidates.json）
- **Self-modification scope guard** — 自動偵測 coding phase 觸及受保護路徑（state machine、guard、runner 等核心地基），要求人工 `--approve-self-mod` 才能完成 merge
- **Discovered feature enrichment** — auto-discover 產出的候選 feature 可經 LLM enrichment 補齊 subtasks、repos、rules、priority，支援 auto-approve 或 draft + `4x approve/reject` 人工審核
- **Template & retro learnings** — Acceptor 產出的 learnings 自動 harvest 到 learnings.json，透過 prompt 注入後續 feature 的各 role，支援 stale 標記、prune、promote 生命週期
- **Project-level template overrides** — 支援覆寫 role prompt 模板，per-project 客製化 designer/coder/reviewer/tester 行為

### Fixes

- **Event runner attribution** — self-mod-detected 和 guard-fail event 現在正確記錄 per-phase runner，而非 default runner
- **Enrichment cancellation** — auto-discover enrichment 改用 propagated context，Ctrl+C 可正確中斷
- **Done exit code** — `4x done` 被 self-mod guard 擋住時回傳 error（exit 1），不再靜默 exit 0
- **Gate config error handling** — `4x gate` 不再吞掉 settings.json 載入錯誤
- **Learning role mapping** — 補上 designer 和 design-reviewer 的 category mapping，修正這兩個 role 無法收到 learnings 注入的問題
- **Candidate pool atomic write** — CandidatePool.Save 改為 atomic temp+rename，避免 dashboard SSE 並發讀到截斷 JSON
- **Feature schema ID pattern** — JSON Schema 的 ID pattern 改接受大寫 F 開頭，補上 draft status
- **Worktree auto-commit skip** — 修正 worktree 模式下 feature YAML status 被意外 auto-commit 的問題
- **Enricher response parsing** — parseResponse 取最後一個 marker block，避免 prompt echo 干擾解析
- **Dashboard settings form** — 統一 project settings UI，修正表單樣式
- **Dashboard pipeline phases** — 正確 color-code pipeline phase 並修正 canonical 排序

### Docs

- **CONTRIBUTING.md** — 新增社群貢獻指南，涵蓋 bug report、docs、examples、plugin、core 五種路徑
- **Three new examples** — python-cli（跨語言）、batch-features（批次排程）、multi-runner（runner 混搭）
- **State machine diagram** — 更新 CLAUDE.md 圖示，補上 design-reviewing 和 pending-review phase
- **Profile docs** — 更新 configuration.md 的 profile 範例為 phases 格式，同步 6 語系
- **Evolve & gate docs** — cli.md、concepts.md、dashboard.md 新增 evolve/gate 章節，同步 5 語系

### Internal

- **Miner I/O dedup** — 三個 scanner 共用一份 ListFeatures 結果，消除 4+ 次重複 YAML I/O
- **AtomicWriteFile export** — 統一 3 處 atomic write pattern 為共用 helper
- **Settings PATCH error handling** — 4 處 json.Marshal error 改為回傳 clientErr
- **Evolve SkipAutoCommit** — 修正 worktree closure 後 flag 沒還原的 mutation leak
- **Review role model tier** — 升級 reviewer/deep-reviewer 預設 model 至 opus tier

## [0.1.17] - 2026-06-18

### Features

- **Runner transient retry** — Runner 子程序遇到暫態 API 錯誤（socket closed、connection reset、rate limit、5xx）時自動 backoff 重試，預設 3 次，避免網路抖動中斷整個 batch run
- **Runner robustness** — 強化 runner 錯誤處理與邊界條件防禦
- **Server & dashboard reliability** — SSE server 與 dashboard 穩定性改進
- **Feature ID & cache correctness** — Feature ID 解析與快取正確性修正
- **State & concurrency safety** — 狀態機與併發操作的安全性強化
- **Scope guard bypass fixes** — 修正 scope guard 可被繞過的邊界案例
- **Doctor accuracy** — `4x doctor` 診斷準確度提升

### Fixes

- **Worktree feature YAML sync** — `syncFeatureToWorktree` 漏同步 feature YAML，導致 tester 在 worktree 裡無法執行 `4x verify`，造成 parallel review/test 無限 amending 迴圈
- **Squash merge conflict cleanup** — `git merge --squash` 衝突時 `merge --abort` 因無 MERGE_HEAD 靜默失敗，殘留 staged/unmerged 檔案；改用 `reset --hard` 確保 index 乾淨
- **Run loop resilience** — 修正 exit code 不一致、state write 錯誤靜默丟棄、worktree 清理提示、WorktreePath 掃描不完整、parallel no-progress tracking 繞過等多項問題
- **App notification icon** — 註冊 app bundle 至 LaunchServices，修正通知圖示顯示
- **Dashboard menu bar icons** — 加寬 menu bar 圖示提升可讀性
- **Dashboard screenshots flickering** — 修正截圖 tab polling 刷新時的閃爍

### Internal

- **Security hardening** — CI actions 釘 SHA、安裝檔 checksum 驗證、檔案權限收緊
- **Feature creator docs** — 同步 CREATOR.md 與最新 feature YAML schema

## [0.1.16] - 2026-06-18

### Features

- **Screenshots tab** — 新增截圖分頁，依 round 分組顯示 tester 截圖，支援縮圖 grid 佈局與點擊放大 lightbox

## [0.1.15] - 2026-06-18

### Features

- **Menu bar icons** — 精簡狀態列圖示，閒置/停止時顯示「4x」，執行中加上播放箭頭

### Fixes

- **Detail tab preservation** — 在訊息或日誌 tab 時，polling 刷新和 Cmd+R 不再跳回總覽

## [0.1.14] - 2026-06-18

### Features

- **Deep-review subPhase tracking** — deep-review 階段支援 sub-reviewer 進度追蹤，Dashboard 可即時顯示各 sub-reviewer 的狀態
- **Token optimization for run pipeline** — run pipeline 加入 token 用量最佳化，減少不必要的 context 傳遞
- **Runner crash recovery** — runner 異常中斷後可自動偵測並恢復，不再需要手動清理殘留狀態

### Fixes

- **Short ID prefix matching** — `LoadFeature` 支援用 feature ID 前綴匹配，不必輸入完整 ID
- **Dashboard page refresh** — 切換 feature 後重新整理頁面時保留當前檢視，不再跳回總覽
- **Dashboard dependency prefix match** — 依賴狀態的前綴比對修正，卡片點擊事件不再誤觸
- **Merge empty commit** — 合併時正確處理「nothing added to commit」情境，不再報錯
- **Multi-repo worktree scope** — worktree scope 檢查限縮至 feature 宣告的 repos，不再掃到無關 repo
- **Notification icon** — 通知改走 native app 路徑，確保顯示正確 icon

## [0.1.13] - 2026-06-17

### Features

- **Worktree-aware scope check** — `4x check` 在 git worktree 內執行時自動偵測 worktree 根目錄，scope 檢查只掃 worktree 的 uncommitted changes，不再誤報 main workspace 的改動
- **Design doc search dirs** — 支援設定自訂設計文件搜尋目錄，不再限於預設的 `docs/design/`

### Fixes

- **App icon notification** — 4x Live 執行中時系統通知改用 app icon 而非預設圖示
- **Dependency badge colors** — Dashboard Overview detail 面板的依賴狀態徽章加上顏色標示
- **Multi-repo worktree cleanup** — 清理 worktree 時偵測並處理孤立 worktree，避免殘留

### Docs

- **SVG logo** — README banner 從 ASCII art 換成 SVG logo

### Internal

- **Copilot & Cursor plugins** — 新增 copilot AGENTS.md 和 cursor .cursorrules 的 plugin import

## [0.1.12] - 2026-06-17

### Features

- **Loose feature validation** — `ListFeatures` 改用寬鬆驗證，feature YAML 有格式問題（如 subtask status 不合法）時仍會列出並附帶 `warnings`，不再靜默跳過

### Fixes

- **Multi-repo merge** — 合併時跳過沒有 feature branch 的 repo，避免誤報錯誤
- **Dependency graph** — 依賴圖按連通分量分離佈局，避免無關 feature 擠成一團
- **macOS notification guard** — 缺少 bundle ID 時安全跳過 UNUserNotificationCenter 初始化
- **Folder picker** — 原生資料夾選取器在非 4x 專案時不再靜默失敗
- **Notification toggle** — 通知開關的強調色修正套用到正確的元素上

### CI

- **Node.js 24** — CI actions 升級至 Node.js 24，加速桌面打包

## [0.1.11] - 2026-06-17

### Features

- **StopMessage** — runner 結束時提供詳細停止原因（完成/錯誤/中斷/逾時），dashboard detail 面板即時刷新顯示
- **Struct validation** — Feature 和 Config 新增結構驗證，載入時自動檢查必填欄位與格式
- **Template resume** — 所有 role template 支援增量寫入與中斷續寫
- **macOS menus** — 新增 Edit/Help 選單、DevTools 切換、通知偏好設定

### Fixes

- **No-op merge** — merge --squash 無淨變更時不再誤報 merge 失敗
- **Logs tab scrollbar** — 修正 logs 頁籤出現多餘外層捲軸

### Docs

- **Landing page** — 新增 GitHub Pages 多語系首頁，含 dashboard 截圖、Roles & Instructions 分頁、terminal demo GIF
- **Runner icons** — 改用官方 Claude 與 Antigravity SVG

## [0.1.10] - 2026-06-16

### Features

- **System notifications** — cross-platform notifications when a run completes, fails, or is interrupted; supports web (Browser Notification API), macOS native (UNUserNotificationCenter), Tauri (tauri-plugin-notification), and CLI (osascript/notify-send/PowerShell); three-layer toggle: `--no-notify` flag > project settings > global user config
- **Popover i18n** — menu bar popover now loads translations from the server, supporting all 6 languages; stat labels, project list, and status text are fully localized
- **Popover quit button** — power icon in popover header to exit the app without opening the main window
- **Auto-commit feature YAML** — feature status changes (done, blocked, in-progress, etc.) are automatically committed when the YAML is git-tracked
- **Coder commit policy** — coder and mini-coder templates now instruct the AI to commit after each meaningful change instead of batching to end-of-phase, protecting progress on session interruption
- **Auto-sync plugins** — `4x run` automatically syncs plugin files before each run

### Fixes

- **Runner FOURX_BIN** — export `FOURX_BIN` env var so coder shell scripts can locate the 4x binary

### Internal

- **Hardened runtime** — macOS app codesign now uses hardened runtime with entitlements
- **CI workflow merge** — release and desktop workflows merged into one; goreleaser and build-binaries run in parallel, eliminating the release polling loop; Tauri CLI cached; redundant goreleaser test hooks removed

## [0.1.9] - 2026-06-16

### Fixes

- **Template Rules injection** — tester, acceptor, and re-verifier templates now receive Project.Rules and Feature.Rules; previously these three roles silently ignored project/feature rules that other roles (designer, coder, reviewer) already had

## [0.1.8] - 2026-06-16

### Fixes

- **Runner self-PATH** — 4x now adds its own binary directory to child process PATH, so runners can call `4x verify` without manual PATH configuration (critical for GUI app / Windows installs)
- **verify.json missing error** — guard check now reports "4x not in PATH" hint instead of generic read error when verify.json is absent, preventing misdiagnosis as "no progress"
- **Windows NSIS upgrade** — installer now kills running 4x processes before writing files, and sidecar is properly killed on app exit
- **Windows ProcessAlive** — process liveness check always returned false on Windows, breaking active runner detection
- **Update check resilience** — update check no longer fails when GitHub API is unreachable; removed omitempty from version response and made macOS client tolerate missing optional fields

### Features

- **Smart resume** — `4x run` now skips already-completed steps (design/code/review/test) when restarting a feature, resuming from where it left off

## [0.1.7] - 2026-06-16

### Fixes

- **macOS port conflict** — app now detects if port 4567 is occupied and finds an available port before launching the embedded server, preventing connection to a stale dev instance (showing wrong version/data)
- **Windows quoted paths** — strip surrounding quotes from path input so `"D:\HelloWorld"` is handled correctly instead of being treated as a relative path
- **Windows bare drive letter** — `C:` now resolves to `C:\` (drive root) instead of CWD on that drive, fixing browse navigation to drive root
- **Windows duplicate tray icon** — remove declarative tray icon from Tauri config that duplicated the programmatic one, leaving a ghost icon with no event handlers
- **Release notes** — goreleaser now correctly publishes CHANGELOG content as release body

## [0.1.6] - 2026-06-16

### Fixes

- **Remove home directory restriction** — browse and project add APIs no longer block paths outside home directory; Windows users can open projects on D:\, E:\ etc. and paths with unicode characters (e.g. Chinese usernames)
- **Windows drive listing** — browsing root now lists available drive letters (C:\, D:\, etc.)
- **Windows tray icon** — single click no longer flashes window (filter to mouse-up only)
- **Single instance guard** — macOS and Windows/Linux apps prevent duplicate launches

### Features

- **Native folder picker** — Browse button opens system folder picker on macOS (NSOpenPanel) and Windows/Linux (Tauri dialog plugin) instead of built-in browser
- **Tauri dialog plugin** — added for Windows/Linux native file dialogs

## [0.1.5] - 2026-06-16

### Fixes

- **Scope guard false positive** — `.4x/` protocol directory was incorrectly flagged as a scope violation during runs, since it's always modified (state, logs) but is not a source repo
- **Error visibility** — runner errors and stop reasons now display as a banner in the feature detail header, not just in the sidebar
- **Advanced options toggle** — new feature modal's advanced section was always visible due to `hidden` attr conflicting with inline `display:flex`; switched to proper display toggle
- **Spec hint removed** — removed the brainstorming hint from new feature modal

## [0.1.4] - 2026-06-16

### Fixes

- **Runner command resolution** — fix runners (claude, codex, etc.) not found when launched from GUI app. `exec.Command` uses the current process PATH, but GUI apps have minimal PATH; now resolves command path against enriched PATH before exec
- **CI release notes** — desktop workflow now waits for goreleaser to create the release before uploading assets, preventing empty release notes

## [0.1.3] - 2026-06-16

### Fixes

- **Version display** — fix `vv0.1.2` double-v prefix and false "update available" prompt when already on latest version
- **Runner PATH enrichment** — GUI app launches now enrich PATH with common tool locations (homebrew, cargo, local/bin, snap, nvm, fnm) on macOS, Windows, and Linux, so runners like `claude` are found without full-path config

### CI

- **Faster builds** — use `cargo-binstall` for tauri-cli instead of compiling from source (~10s vs ~8min)
- **Node.js 22** — upgrade all GitHub Actions to Node.js 22 versions, fixing deprecation warnings

## [0.1.2] - 2026-06-16

### macOS App

- **About window** — custom About panel with app icon, version, description, and GitHub link
- **Menu bar popover redesign** — accurate per-project stats, multi-project layout with highlight tasks, dynamic height, SVG action icons
- **Icon resolution fix** — walk up directory tree to find Resources in both dev and release builds
- **DMG drag-to-install** — Applications symlink + Finder window layout for standard macOS install experience
- **Ad-hoc codesign** — sign binaries individually to prevent Gatekeeper "damaged app" error
- **Full resource bundling** — copy all menu bar icons into app bundle, not just AppIcon

### Server

- **Port auto-fallback** — when default port 4567 is occupied, automatically pick a free port instead of crashing; support `--port=0` for OS-assigned port

### Packaging

- **CI-compatible DMG** — AppleScript graceful fallback for headless environments, explicit DMG sizing, better compression

## [0.1.1] - 2026-06-16

### Packaging

- **DMG build fix** — copy all Resources into app bundle, sign binaries individually instead of deprecated `--deep`, CI headless compatibility

## [0.1.0] - 2026-06-15

First public release.

### Core

- **Multi-role AI development loop** — Designer, Coder, Reviewer, Tester, Deep Reviewer, Acceptor with isolated context windows
- **Deterministic guardrails** — scope lock, baseline snapshots, state machine, evidence requirements (enforced by CLI, not AI)
- **File-based protocol** (`.4x/` directory) — crash-resistant, LLM-agnostic inter-role communication
- **State machine** — `init → designing → coding → reviewing → testing → deep-reviewing → accepting → done` with `blocked` / `needs-attention` escape states and `amending` re-entry
- **Pending-review gate** — human always reviews before marking done
- **Phase hooks** — pre/post shell commands on phase transitions
- **Health check** — auto-verify environment before testing phase
- **Adaptive pipeline** — profile-based role selection by feature complexity

### CLI

- `4x init` — initialize project with runner configuration
- `4x new` — create features with subtask dependencies, priority, and rules
- `4x run` — execute the full Design-Code-Review-Test loop with deep review
- `4x status` — view feature status with detail levels
- `4x done` — mark features complete with auto-merge from worktree
- `4x merge` — multi-repo squash merge with rollback
- `4x batch plan/run/next/stop` — dependency-aware DAG scheduling with auto-merge and reports
- `4x live` — real-time SSE multi-project dashboard
- `4x config` — manage settings
- `4x check` — run guardrail checks
- `4x doctor` — universal settings and workspace health check
- `4x clean` — remove workspace artifacts for completed features
- `4x transition` / `4x event` — manual state and event management
- `4x prompt` — generate role prompts for manual use
- `4x subtask` — manage subtask status with validation
- `4x verify` — run verification commands and check evidence
- `4x sync` — re-deploy plugin files after binary update
- `4x mcp` — Model Context Protocol server

### Run Loop

- Automatic phase transitions with configurable max rounds and stop conditions
- Deep review with parallel fan-out (N sub-reviewers + synthesizer)
- Deep review self-healing loop (fix → re-verify cycle)
- Per-round auto-commit with worktree isolation support
- Review verdict parsing with severity counting

### Runners

- 6 runners: Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor — plugins embedded in binary
- Per-role model configuration with abstract tier resolution (`opus` / `sonnet` / `haiku`)
- PTY mode with graceful SIGTERM → SIGKILL shutdown sequence
- Non-PTY process group isolation (no orphaned subprocesses on cancellation)
- Stream JSON mode, stdin mode, argument mode
- Placeholder resolution with fail-loud semantics

### Batch Mode

- Dependency DAG scheduling with cycle detection and topological sort
- Chain scheduling with configurable max chain length
- Auto-merge on feature completion
- Batch report generation on stop/crash
- Orphan process cleanup
- In-progress status tracked as failure to prevent infinite retry loops

### Dashboard (4x Live)

- Real-time event streaming via SSE
- Multi-project management with LRU recent projects and file browser
- Feature overview with design doc resolution
- Log viewer with stream-json support
- Batch control and dependency DAG visualization
- Settings editor with inline config editing
- Control panel — run, stop, new feature, mark done
- API response caching
- Path traversal protection with symlink resolution
- i18n — English, 繁體中文, 简体中文, 日本語, 한국어, Español

### Desktop App

- Tauri-based cross-platform app (macOS, Linux, Windows)
- System tray, app menu, close-to-hide
- Frost theme with menu bar popover

### Documentation

- Full user guide (8 documents) in 6 languages
- Architecture docs (overview, protocol, state machine, cross-platform packaging)
- Design specs and plans for all features
- Plugin contract reference

### Distribution

- Cross-platform binaries — macOS, Linux, Windows (amd64 + arm64)
- Homebrew tap — `brew install ggwhite/tap/fourx`
- Go install — `go install github.com/ggwhite/4x/cmd/4x@latest`
- GitHub Releases with checksums

### Quality

- 700 tests across 18 packages (race detector enabled)
- Atomic file writes for concurrent safety (SaveFeature, WriteState, WriteBatchReport)
- Structured logging with rotation and configurable retention

[0.1.0]: https://github.com/ggwhite/4x/releases/tag/v0.1.0
