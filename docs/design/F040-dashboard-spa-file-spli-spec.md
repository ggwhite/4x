# F040: Dashboard SPA File Split — Spec

## Problem

`internal/server/static/index.html` 是一個 2112 行的單檔 SPA，混合 HTML markup (~300 行)、inline CSS (~130 行)、inline JS (~1710 行)。搜尋跳轉困難、JS 無模組化、CSS/HTML/JS 改動互搶 diff。

## Goal

拆成 6 個檔案（1 HTML + 1 CSS + 4 JS），維持 `go:embed` 嵌入 binary 部署，零功能行為改變。

## File Structure

```
internal/server/static/
├── index.html      ~300 行  純 HTML markup + <link>/<script> 引用
├── style.css       ~130 行  CSS variables × 6 主題 + 元件樣式 + 動畫
├── core.js         ~250 行  全域狀態、常量、工具函式、i18n
├── settings.js     ~650 行  外觀設定 + Project Settings + Global Settings
├── ui.js           ~800 行  Tabs/Search/Dashboard/Sidebar/Detail/SSE/Run
├── init.js         ~20 行   Bootstrap
└── locales/                 不動
```

## HTML Structure

```html
<head>
  <link rel="stylesheet" href="/style.css">
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
  <script>tailwind.config = {...}</script>
</head>
<body>
  <!-- 純 HTML markup -->
  <script src="/core.js"></script>
  <script src="/settings.js"></script>
  <script src="/ui.js"></script>
  <script src="/init.js"></script>
</body>
```

## JS Module Split

載入順序固定，靠全域變數通訊（不用 ES modules，避免 embed 的 CORS/MIME 問題）。

### core.js (1st)

基礎層，所有其他 JS 依賴此檔。

- 全域變數宣告：`projects`, `activeProjectId`, `current`, `lastTasks`, `settings`, `sseSource`, `openTabs`, `allTabTasks`, `activeRuns`, `activeDetailTab`, `overviewCache`, `_i18nDict`, `_currentLocale` 等
- 常量：`ROLES`, `PHASE_ICON`, `PHASE_COLORS`, `SECTION_COLORS`, `RUNNER_COLORS`, `THEMES`, `SUPPORTED_LOCALES`, `LOCALE_NAMES`
- 工具函式：`esc()`, `escAttr()`, `fmtTokens()`, `formatDuration()`, `formatElapsed()`, `showToast()`, `apiBase()`, `sseBase()`, `normCJK()`, `fuzzyScore()`, `fuzzyMatch()`
- i18n：`t()`, `detectLocale()`, `loadLocale()`, `applyI18n()`, `switchLocale()`
- Tab 持久化：`saveTabState()`, `loadTabState()`

### settings.js (2nd)

設定相關 UI，自成一體。

- 外觀：`applyTheme()`, `renderThemeGrid()`, `applyFont()`, `adjFont()`, `adjRefresh()`, `startRefreshTimer()`, `initSettings()`
- Project Settings：`openProjectSettings()`, `closeProjectSettings()`, `renderProjectSettingsForm()`, `collectFormData()`, `saveProjectSettings()`, `autoSave()`, tag-list helpers (`addTag`, `removeTag`, `getTagItems`), `psField`, `psTagField`, `filterPSFields`, `addRunner`, `removeRunner`
- Global Settings：`openGlobalSettings()`, `closeGlobalSettings()`, `renderGlobalSettingsForm()`, `saveGlobalSettings()`, `switchGSTab()`, `gsAddRunner`, `gsRemoveRunner`, `gsUpdateRunner`, `renderGSRunners()`, `loadSupportedRunners()`, `gsSetLocale()`

### ui.js (3rd)

主要 UI 渲染與互動邏輯。

- Dashboard：`renderDashboard()`, `classify()`
- Sidebar：`renderSidebar()`, `renderTaskItem()`, `phaseBadge()`, `badge()`
- Detail：`loadDetail()`, `renderMsgCard()`, `loadMessages()`, `switchDetailTab()`
- Overview：`loadOverview()`, `renderOverview()`
- Tabs/Projects：`renderTabs()`, `switchTab()`, `closeTab()`, `addTab()`, `loadProjects()`, `showProjectPicker()`, `renderProjectPicker()`, `goHome()`
- Search (Cmd+K)：`openSearch()`, `closeSearch()`, `renderSearchResults()`, `selectSearch()`, `onSearchKey()`, `loadAllTabTasks()`
- SSE：`connectSSE()`, `disconnectSSE()`, `connectLogSSE()`, `disconnectLogSSE()`
- Logs：`loadLogs()`, `viewLog()`
- Doctor：`showDoctor()`, `renderDoctor()`
- Run Management：`getRunId()`, `openRunModal()`, `closeRunModal()`, `adjRunRounds()`, `submitRun()`, `stopRun()`, `openNewFeature()`, `closeNewFeature()`, `submitNewFeature()`
- Mark Done：`markDone()`, `showConfirm()`
- Data Loading：`load()`
- Keyboard Handler：`document.addEventListener('keydown', ...)`

### init.js (4th)

啟動入口，必須最後載入。

- `init()` 函式：`initSettings()` → `loadLocale()` → `applyI18n()` → `loadProjects()` → `loadTabState()` → `renderTabs()` → `load()` → `renderProjectPicker()`
- 立即呼叫 `init()`

## Go Embed Changes (server.go)

### Before

```go
//go:embed static/index.html
var indexHTML string

//go:embed static/locales/*.json
var localeFS embed.FS

mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html")
    fmt.Fprint(w, indexHTML)
})
```

### After

```go
//go:embed static/*
var staticFS embed.FS

mux.Handle("/", http.FileServer(http.FS(staticFS)))
```

- 合併 `indexHTML` 和 `localeFS` 為單一 `staticFS`
- `http.FileServer` 自動處理所有靜態資源的路由和 Content-Type
- `/api/locales/` handler 改從 `staticFS` 讀取

注意：`http.FileServer` 搭配 `embed.FS` 時路徑帶有 `static/` prefix，需要 `http.StripPrefix` 或 `fs.Sub` 去掉。使用 `fs.Sub(staticFS, "static")` 較乾淨。

## Constraints

- 不引入任何 build tool（webpack / vite / esbuild）
- 不引入 npm / node_modules
- 維持 go:embed 嵌入 binary
- 不用 ES modules（`<script type="module">`），用傳統 `<script>` 依序載入
- locales/*.json 不動
- 零功能行為改變

## Verification

- `go build ./cmd/4x && go vet ./... && go test ./...` 全部通過
- `4x live` 啟動後：
  - 所有頁面正常渲染（Dashboard/Detail/Overview/Doctor）
  - 主題切換正常（6 個主題）
  - i18n 切換正常
  - SSE 即時更新正常
  - Project Settings / Global Settings 開關與儲存正常
  - Cmd+K 搜尋正常
  - Run/Stop/New Feature modal 正常
  - macOS native wrapper 不受影響
