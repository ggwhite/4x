# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

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
- Homebrew tap — `brew install ggwhite/tap/4x`
- Go install — `go install github.com/ggwhite/4x/cmd/4x@latest`
- GitHub Releases with checksums

### Quality

- 700 tests across 18 packages (race detector enabled)
- Atomic file writes for concurrent safety (SaveFeature, WriteState, WriteBatchReport)
- Structured logging with rotation and configurable retention

[0.1.0]: https://github.com/ggwhite/4x/releases/tag/v0.1.0
