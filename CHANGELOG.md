# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

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
