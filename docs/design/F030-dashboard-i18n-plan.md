# F030: Dashboard i18n Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 為 Dashboard 加入 6 語言 i18n 支援（en, zh-TW, zh-CN, ja, ko, es），翻譯檔為 JSON + Go embed，前端用 `t(key)` 函式替換所有硬寫字串。

**Architecture:** 翻譯 JSON 放 `internal/server/static/locales/`，Go embed 嵌入 binary。Server 新增兩個 API（`/api/locales` 清單、`/api/locales/{lang}` 內容）。前端啟動時 fetch 對應 locale，用 `t(key)` + `data-i18n` attribute 替換靜態文字。Settings 面板加 Language 下拉。

**Tech Stack:** Go (embed, net/http), vanilla JS, JSON

**Spec:** `docs/design/F030-dashboard-i18n-spec.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/server/static/locales/en.json` | 英文翻譯基準檔 |
| Create | `internal/server/static/locales/zh-TW.json` | 繁體中文翻譯 |
| Create | `internal/server/static/locales/zh-CN.json` | 简体中文翻译 |
| Create | `internal/server/static/locales/ja.json` | 日本語翻訳 |
| Create | `internal/server/static/locales/ko.json` | 한국어 번역 |
| Create | `internal/server/static/locales/es.json` | Traducción al español |
| Modify | `internal/server/server.go` | 新增 locale embed + API handlers |
| Modify | `internal/server/multi.go` | 在 multi-project mux 註冊 locale routes |
| Modify | `internal/server/server_test.go` | 新增 locale API 測試 |
| Modify | `internal/server/static/index.html` | i18n runtime + 字串抽取 + Language 下拉 |
| Create | `scripts/check-i18n.sh` | CI key 完整性驗證腳本 |
| Modify | `Makefile` | 加 `check-i18n` target |

---

### Task 1: en.json 基準翻譯檔

**Files:**
- Create: `internal/server/static/locales/en.json`

先盤點 `index.html` 所有使用者可見文字，建立完整 key set。

- [ ] **Step 1: 建立 en.json**

盤點 `index.html` 裡所有使用者可見的 UI 文字（不含 feature name、log 內容等 project data），建立翻譯 key。

```json
{
  "app.title": "4x Live",
  "app.autoRefresh": "Auto-refresh",
  "app.selectProject": "Select a project to get started",
  "app.openProject": "Open Project...",
  "app.noArtifacts": "No artifacts yet",
  "app.confirmDone": "Mark {id} as done?",
  "nav.messages": "Messages",
  "nav.logs": "Logs",
  "sidebar.running": "Running",
  "sidebar.review": "Review",
  "sidebar.pending": "Pending",
  "sidebar.todo": "Todo",
  "sidebar.done": "Done",
  "sidebar.markDone": "Mark Done",
  "dashboard.tasks": "{count} tasks",
  "dashboard.status": "Status",
  "dashboard.total": "total",
  "dashboard.roundsDist": "Rounds Distribution",
  "dashboard.avgRounds": "avg {avg} rounds/task",
  "dashboard.recentCompletions": "Recent Completions",
  "dashboard.currentlyRunning": "Currently Running",
  "dashboard.pendingReview": "Pending Review",
  "status.inProgress": "In Progress",
  "status.review": "Review",
  "status.done": "Done",
  "status.blocked": "Blocked",
  "status.backlog": "Backlog",
  "status.notStarted": "Not Started",
  "search.placeholder": "Search features... (@project to filter)",
  "search.navigate": "navigate",
  "search.select": "select",
  "search.close": "close",
  "settings.title": "Settings",
  "settings.appearance": "Appearance",
  "settings.contentFont": "Content Font",
  "settings.codeFont": "Code Font",
  "settings.theme": "Theme",
  "settings.behavior": "Behavior",
  "settings.refresh": "Refresh",
  "settings.pollingInterval": "Polling interval",
  "settings.language": "Language",
  "settings.saved": "Saved",
  "picker.title": "Open Project",
  "picker.noRecent": "No recent projects",
  "picker.pathPlaceholder": "Enter project path...",
  "picker.browse": "Browse",
  "picker.open": "Open",
  "picker.connectionError": "Connection error",
  "picker.noSubdirs": "No subdirectories",
  "picker.is4xProject": "This is a 4x project",
  "picker.openProject": "Open Project",
  "init.title": "Not a 4x project",
  "init.message": "Initialize this directory as a new 4x project?",
  "init.cancel": "Cancel",
  "init.initialize": "Initialize",
  "projectSettings.title": "Project Settings",
  "projectSettings.form": "Form",
  "projectSettings.json": "JSON",
  "projectSettings.searchPlaceholder": "Search settings...",
  "projectSettings.cancel": "Cancel",
  "projectSettings.save": "Save",
  "projectSettings.saved": "Saved!",
  "projectSettings.project": "Project",
  "projectSettings.general": "General",
  "projectSettings.runners": "Runners",
  "projectSettings.roles": "Roles",
  "projectSettings.noProject": "No project selected",
  "projectSettings.nameRequired": "project.name is required",
  "projectSettings.saveFailed": "Save failed: {error}",
  "projectSettings.loadFailed": "Failed to load settings: {error}",
  "projectSettings.invalidJson": "Invalid JSON: {error}",
  "projectSettings.addRunner": "+ Add Runner",
  "projectSettings.runnerPlaceholder": "Runner name...",
  "projectSettings.remove": "Remove",
  "projectSettings.defaultRunner": "Default Runner",
  "projectSettings.isolation": "Isolation",
  "projectSettings.maxConcurrentRuns": "Max Concurrent Runs",
  "projectSettings.commit": "Commit",
  "projectSettings.none": "-- none --",
  "field.name": "Name",
  "field.description": "Description",
  "field.language": "Language",
  "field.build": "Build",
  "field.test": "Test",
  "field.lint": "Lint",
  "field.setup": "Setup",
  "field.docs": "Docs",
  "field.rules": "Rules",
  "field.includes": "Includes",
  "field.command": "Command",
  "field.args": "Args",
  "field.model": "Model",
  "field.deepModel": "Deep Model",
  "field.instructions": "Instructions",
  "field.hubRepos": "Hub Repos",
  "field.add": "Add...",
  "logs.noLogs": "No logs yet",
  "browse.up": ".. (up)"
}
```

- [ ] **Step 2: 驗證 JSON 格式**

```bash
python3 -c "import json; json.load(open('internal/server/static/locales/en.json'))"
```

Expected: 無輸出（合法 JSON）

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/locales/en.json
git commit -m "feat(F030): add en.json i18n base translation file"
```

---

### Task 2: 5 個語言翻譯檔

**Files:**
- Create: `internal/server/static/locales/zh-TW.json`
- Create: `internal/server/static/locales/zh-CN.json`
- Create: `internal/server/static/locales/ja.json`
- Create: `internal/server/static/locales/ko.json`
- Create: `internal/server/static/locales/es.json`

每個檔案的 key set 必須與 `en.json` 完全一致。翻譯品質要求：使用該語言的 native UI 慣用語（如繁中用「設定」不用「設置」、日文用「設定」不用片假名、韓文用原生韓語而非漢字）。

- [ ] **Step 1: 建立 zh-TW.json**

與 `en.json` 相同 key set，所有 value 翻譯為繁體中文。注意：
- `app.title` 維持 `"4x Live"`（品牌名不翻）
- 狀態名稱用繁中慣用語：Running→執行中、Review→待審查、Pending→處理中、Todo→待辦、Done→完成
- Settings 類用「設定」不用「設置」

- [ ] **Step 2: 建立 zh-CN.json**

與 `en.json` 相同 key set，所有 value 翻譯為简体中文。

- [ ] **Step 3: 建立 ja.json**

與 `en.json` 相同 key set，所有 value 翻譯為日本語。

- [ ] **Step 4: 建立 ko.json**

與 `en.json` 相同 key set，所有 value 翻譯為한국어。

- [ ] **Step 5: 建立 es.json**

與 `en.json` 相同 key set，所有 value 翻譯為 Español。

- [ ] **Step 6: 驗證所有 JSON 格式與 key 一致性**

```bash
python3 -c "
import json, sys
base = set(json.load(open('internal/server/static/locales/en.json')).keys())
for lang in ['zh-TW','zh-CN','ja','ko','es']:
    keys = set(json.load(open(f'internal/server/static/locales/{lang}.json')).keys())
    missing = base - keys
    extra = keys - base
    if missing: print(f'{lang}: MISSING {missing}'); sys.exit(1)
    if extra: print(f'{lang}: EXTRA {extra}')
    else: print(f'{lang}: OK')
"
```

Expected: 全部 OK

- [ ] **Step 7: Commit**

```bash
git add internal/server/static/locales/
git commit -m "feat(F030): add zh-TW, zh-CN, ja, ko, es translation files"
```

---

### Task 3: Go server locale API

**Files:**
- Modify: `internal/server/server.go:1-30` (embed 宣告區) + `:28-108` (NewMux route 區)
- Modify: `internal/server/multi.go` (NewMultiMux route 區)
- Test: `internal/server/server_test.go`

- [ ] **Step 1: 寫 locale API 的 failing tests**

在 `internal/server/server_test.go` 新增：

```go
func TestGetLocales(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	var locales []string
	if err := json.NewDecoder(rec.Body).Decode(&locales); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}
	if len(locales) != len(want) {
		t.Fatalf("locales = %v, want %v", locales, want)
	}
	for i, w := range want {
		if locales[i] != w {
			t.Errorf("locales[%d] = %s, want %s", i, locales[i], w)
		}
	}
}

func TestGetLocaleEN(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/en", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %s, want immutable", cc)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if translations["app.title"] != "4x Live" {
		t.Errorf("app.title = %q, want '4x Live'", translations["app.title"])
	}
}

func TestGetLocaleZhTW(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/zh-TW", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := translations["app.title"]; !ok {
		t.Error("zh-TW missing key app.title")
	}
}

func TestGetLocaleUnknownFallback(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/nonexistent", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if translations["app.title"] != "4x Live" {
		t.Errorf("fallback should return en.json, got app.title = %q", translations["app.title"])
	}
}
```

- [ ] **Step 2: 執行測試確認 fail**

```bash
go test ./internal/server/ -run "TestGetLocale" -v
```

Expected: FAIL（route 不存在，404）

- [ ] **Step 3: 實作 locale handlers**

在 `internal/server/server.go` 頂部 embed 區加入：

```go
//go:embed static/locales/*.json
var localeFS embed.FS

var supportedLocales = []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}
```

把 `_ "embed"` 改成 `"embed"`（因為現在直接使用了 embed.FS）。

新增兩個 handler 函式：

```go
func handleGetLocales(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	json.NewEncoder(w).Encode(supportedLocales)
}

func handleGetLocale(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimPrefix(r.URL.Path, "/api/locales/")
	filename := "static/locales/" + lang + ".json"
	data, err := localeFS.ReadFile(filename)
	if err != nil {
		data, _ = localeFS.ReadFile("static/locales/en.json")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}
```

在 `NewMux` 的 route 註冊區（`mux.HandleFunc("/", ...)` 之前）加入：

```go
mux.HandleFunc("/api/locales/", func(w http.ResponseWriter, r *http.Request) {
    handleGetLocale(w, r)
})
mux.HandleFunc("/api/locales", func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/api/locales" {
        handleGetLocale(w, r)
        return
    }
    handleGetLocales(w)
})
```

注意：`/api/locales` 精確匹配回清單，`/api/locales/` 帶 suffix 回翻譯內容。Go 的 `http.ServeMux` 會自動把 `/api/locales/en` 路由到 `/api/locales/` handler。

- [ ] **Step 4: 在 multi.go 註冊 locale routes**

locale 是 global（不分 project），所以在 `NewMultiMux` 裡直接註冊（不走 per-project proxy）。在 `mux.HandleFunc("/", ...)` 之前加入：

```go
mux.HandleFunc("/api/locales/", func(w http.ResponseWriter, r *http.Request) {
    handleGetLocale(w, r)
})
mux.HandleFunc("/api/locales", func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/api/locales" {
        handleGetLocale(w, r)
        return
    }
    handleGetLocales(w)
})
```

- [ ] **Step 5: 執行測試確認 pass**

```bash
go test ./internal/server/ -run "TestGetLocale" -v
```

Expected: 4 tests PASS

- [ ] **Step 6: 全量測試**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go internal/server/multi.go internal/server/server_test.go
git commit -m "feat(F030): add locale API endpoints with Go embed"
```

---

### Task 4: 前端 i18n runtime

**Files:**
- Modify: `internal/server/static/index.html:279-300` (JS 全域變數區) + `:1194-1202` (init 函式區)

這一步只加入 i18n 基礎設施（`t()` 函式、locale 偵測、fetch 翻譯），還不改動既有 HTML/JS 字串。

- [ ] **Step 1: 在 JS 全域變數區加入 i18n runtime**

在 `index.html` 的 `<script>` 區塊最上方（`let projects = [];` 之前）加入：

```javascript
let _i18nDict = {};
let _currentLocale = 'en';
const SUPPORTED_LOCALES = [];
const LOCALE_NAMES = {
  en: 'English', 'zh-TW': '繁體中文', 'zh-CN': '简体中文',
  ja: '日本語', ko: '한국어', es: 'Español'
};

function t(key) {
  return _i18nDict[key] || key;
}

function detectLocale() {
  const saved = localStorage.getItem('4x-locale');
  if (saved && LOCALE_NAMES[saved]) return saved;
  const nav = navigator.language || 'en';
  if (LOCALE_NAMES[nav]) return nav;
  const prefix = nav.split('-')[0];
  const match = Object.keys(LOCALE_NAMES).find(k => k.startsWith(prefix));
  return match || 'en';
}

async function loadLocale(lang) {
  try {
    const resp = await fetch('/api/locales/' + lang);
    if (resp.ok) {
      _i18nDict = await resp.json();
      _currentLocale = lang;
    }
  } catch {}
}

function applyI18n() {
  document.querySelectorAll('[data-i18n]').forEach(el => {
    el.textContent = t(el.dataset.i18n);
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    el.placeholder = t(el.dataset.i18nPlaceholder);
  });
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    el.title = t(el.dataset.i18nTitle);
  });
}

async function switchLocale(lang) {
  localStorage.setItem('4x-locale', lang);
  await loadLocale(lang);
  applyI18n();
}
```

- [ ] **Step 2: 修改 init 函式載入 locale**

找到 `index.html` 底部的 `init()` 函式（約 line 1194），改為：

```javascript
async function init() {
  initSettings();
  try {
    const resp = await fetch('/api/locales');
    if (resp.ok) {
      const list = await resp.json();
      SUPPORTED_LOCALES.length = 0;
      list.forEach(l => SUPPORTED_LOCALES.push(l));
    }
  } catch {}
  await loadLocale(detectLocale());
  applyI18n();
  await loadProjects();
  const saved = loadTabState();
  if (saved.tabs.length > 0) { for (const tab of saved.tabs) { if (projects.find(p => p.id === tab.id)) openTabs.push(tab); } activeProjectId = openTabs.find(t => t.id === saved.active) ? saved.active : (openTabs[0] ? openTabs[0].id : null); }
  if (openTabs.length === 0 && projects.length > 0) { projects.forEach(p => openTabs.push({ id: p.id, name: p.name })); activeProjectId = openTabs[0] ? openTabs[0].id : null; }
  saveTabState(); renderTabs();
  if (activeProjectId) load(); else renderProjectPicker();
}
```

- [ ] **Step 3: build + 手動驗證**

```bash
go build ./cmd/4x && go vet ./...
```

Expected: 編譯通過。此時 `t(key)` 會回傳 key 本身（直到 step 5 替換字串後才會生效），UI 看起來跟之前一模一樣。

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F030): add i18n runtime (t function, locale detection, fetch)"
```

---

### Task 5: HTML 靜態字串抽取

**Files:**
- Modify: `internal/server/static/index.html` (HTML 區 line 1-278)

把 HTML 中所有硬寫的使用者可見文字改成 `data-i18n` attribute 或對應的 `data-i18n-placeholder` / `data-i18n-title`。

- [ ] **Step 1: 替換 HTML 中的靜態文字**

逐一替換以下位置（不改動 JS 產生的動態文字，那是 Task 6）：

| 原始文字 | 位置（約 line） | 改為 |
|---------|---------------|------|
| `<html lang="en">` | 1 | 不動（HTML lang 可在 JS 裡動態設） |
| `+` (add tab btn title="Add project") | 115 | `data-i18n-title="picker.title"` |
| `4x Live` (sidebar h1) | 124 | `<h1 ... data-i18n="app.title">4x Live</h1>` |
| `Auto-refresh` | 125 | `<span ... data-i18n="app.autoRefresh">Auto-refresh</span>` |
| `Search features... (@project to filter)` | 163 | `data-i18n-placeholder="search.placeholder"` |
| `navigate` | 169 | `<span data-i18n="search.navigate">navigate</span>` |
| `select` | 170 | `<span data-i18n="search.select">select</span>` |
| `close` | 171 | `<span data-i18n="search.close">close</span>` |
| `Settings` (modal title) | 180 | `<span ... data-i18n="settings.title">Settings</span>` |
| `Appearance` | 184 | `<div ... data-i18n="settings.appearance">Appearance</div>` |
| `Content Font` | 187 | `<span ... data-i18n="settings.contentFont">Content Font</span>` |
| `Code Font` | 191 | `<span ... data-i18n="settings.codeFont">Code Font</span>` |
| `Theme` | 197 | `<div ... data-i18n="settings.theme">Theme</div>` |
| `Behavior` | 201 | `<div ... data-i18n="settings.behavior">Behavior</div>` |
| `Refresh` | 203 | `<span ... data-i18n="settings.refresh">Refresh</span>` |
| `Polling interval` | 203 | `<div ... data-i18n="settings.pollingInterval">Polling interval</div>` |
| `Open Project` (picker title) | 215 | `<span ... data-i18n="picker.title">Open Project</span>` |
| `Enter project path...` | 224 | `data-i18n-placeholder="picker.pathPlaceholder"` |
| `Browse` | 227 | `<button ... data-i18n="picker.browse">Browse</button>` |
| `Open` | 228 | `<button ... data-i18n="picker.open">Open</button>` |
| `Project Settings` (modal title) | 239 | `<span ... data-i18n="projectSettings.title">Project Settings</span>` |
| `Saved` (autosave indicator) | 240 | `<span ... data-i18n="settings.saved">Saved</span>` |
| `Form` | 242 | `<button ... data-i18n="projectSettings.form">Form</button>` |
| `JSON` | 243 | 不翻（技術名詞，全語言通用） |
| `Search settings...` | 248 | `data-i18n-placeholder="projectSettings.searchPlaceholder"` |
| `Cancel` (project settings) | 258 | `<button ... data-i18n="projectSettings.cancel">Cancel</button>` |
| `Save` (project settings) | 259 | `<button ... data-i18n="projectSettings.save">Save</button>` |
| `Not a 4x project` | 268 | `<div ... data-i18n="init.title">Not a 4x project</div>` |
| `Initialize this directory as a new 4x project?` | 270 | `<div ... data-i18n="init.message">Initialize this directory...</div>` |
| `Cancel` (init modal) | 273 | `<button ... data-i18n="init.cancel">Cancel</button>` |
| `Initialize` (init modal) | 274 | `<button ... data-i18n="init.initialize">Initialize</button>` |
| `Messages` (detail tab) | 143 | `<button ... data-i18n="nav.messages">Messages</button>` |
| `Logs` (detail tab) | 144 | `<button ... data-i18n="nav.logs">Logs</button>` |

保留原始英文文字作為 fallback（i18n 載入前的顯示）。

- [ ] **Step 2: build 驗證**

```bash
go build ./cmd/4x && go vet ./...
```

Expected: 編譯通過

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F030): extract static HTML strings to data-i18n attributes"
```

---

### Task 6: JS 動態字串替換

**Files:**
- Modify: `internal/server/static/index.html` (JS 區 line 279-1205)

替換 JS 裡用字串字面值產生的使用者可見文字。每個替換都用 `t('key')` 包裹。

- [ ] **Step 1: 替換 JS 動態字串**

以下是主要需替換的位置（按函式分組）：

**badge() 函式（約 line 962-968）：**
- `'In Progress'` → `t('status.inProgress')`
- `'Review'` → `t('status.review')`
- `'Done'` → `t('status.done')`
- `'Blocked'` → `t('status.blocked')`
- `'Backlog'` → `t('status.backlog')`

**renderSearchResults() 函式（約 line 470-488）：**
- `'In Progress'` → `t('status.inProgress')`
- `'Review'` → `t('status.review')`
- `'Done'` → `t('status.done')`
- `'Blocked'` → `t('status.blocked')`
- `'Not Started'` → `t('status.notStarted')`

**renderDashboard() 函式（約 line 995-1023）：**
- 各 bucket label（`'Running'`、`'Review'`、`'Pending'`、`'Todo'`、`'Done'`）→ 用 `t('sidebar.running')` 等
- `'Status'` → `t('dashboard.status')`
- `'total'` → `t('dashboard.total')`
- `'Rounds Distribution'` → `t('dashboard.roundsDist')`
- `'avg ... rounds/task'` → 用 `t('dashboard.avgRounds').replace('{avg}', avg.toFixed(1))`
- `'Recent Completions'` → `t('dashboard.recentCompletions')`
- `'Currently Running'` → `t('dashboard.currentlyRunning')`
- `'Pending Review'` → `t('dashboard.pendingReview')`
- `'{count} tasks'` → `t('dashboard.tasks').replace('{count}', total)`
- `'Select a project to get started'` → `t('app.selectProject')`
- `'Open Project...'` → `t('app.openProject')`

**load() 函式內 rg() 的 sidebar group labels（約 line 1053-1057）：**
- `'Running'` → `t('sidebar.running')`
- `'Review'` → `t('sidebar.review')`
- `'Pending'` → `t('sidebar.pending')`
- `'Todo'` → `t('sidebar.todo')`
- `'Done'` → `t('sidebar.done')`
- `'Mark Done'` → `t('sidebar.markDone')`

**markDone() 函式（約 line 970）：**
- `'Mark '+fid+' as done?'` → `t('app.confirmDone').replace('{id}', fid)`

**loadMessages() 函式（約 line 1129）：**
- `'No artifacts yet'` → `t('app.noArtifacts')`

**renderRecentList()（約 line 382）：**
- `'No recent projects'` → `t('picker.noRecent')`

**browseTo()（約 line 445）：**
- `'No subdirectories'` → `t('picker.noSubdirs')`
- `'Connection error'` → `t('picker.connectionError')`
- `'This is a 4x project'` → `t('picker.is4xProject')`
- `'Open Project'` → `t('picker.openProject')`

**addProjectFromInput()（約 line 400）：**
- `'Connection error'` → `t('picker.connectionError')`

**renderProjectSettingsForm()（約 line 675-770）：**
- Section labels: `'Project'` → `t('projectSettings.project')`、`'General'` → `t('projectSettings.general')`、`'Runners'` → `t('projectSettings.runners')`、`'Roles'` → `t('projectSettings.roles')`
- Field labels: `'Name'` → `t('field.name')` 等
- `'-- none --'` → `t('projectSettings.none')`
- `'+ Add Runner'` → `t('projectSettings.addRunner')`
- `'Runner name...'` → `t('projectSettings.runnerPlaceholder')`
- `'Remove'` → `t('projectSettings.remove')`
- `'Default Runner'` → `t('projectSettings.defaultRunner')`

**openProjectSettings()（約 line 553）：**
- `'No project selected'` → `t('projectSettings.noProject')`
- `'Failed to load settings: '` → `t('projectSettings.loadFailed').replace('{error}', ...)`

**saveProjectSettings()（約 line 901-939）：**
- `'project.name is required'` → `t('projectSettings.nameRequired')`
- `'Save failed: '` → `t('projectSettings.saveFailed').replace('{error}', ...)`
- `'Saved!'` → `t('projectSettings.saved')`
- `'Invalid JSON: '` → `t('projectSettings.invalidJson').replace('{error}', ...)`

**loadLogs()（約 line 1154）：**
- `'No logs yet'` → `t('logs.noLogs')`

**renderTagList()（約 line 614）：**
- `'Add...'` placeholder → `t('field.add')`

**browseTo() 裡的 ".. (up)"（約 line 437）：**
- `'.. (up)'` → `t('browse.up')`

- [ ] **Step 2: build 驗證**

```bash
go build ./cmd/4x && go vet ./...
```

Expected: 編譯通過

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F030): replace JS dynamic strings with t() calls"
```

---

### Task 7: Settings 面板 Language 下拉

**Files:**
- Modify: `internal/server/static/index.html` (Settings modal HTML + JS)

- [ ] **Step 1: 在 Settings modal 加 Language section**

在 Settings modal 裡（`index.html` 約 line 196 的 Theme section 之後，Behavior section 之前），插入 Language section：

```html
<div class="settings-section">
  <div class="settings-label" data-i18n="settings.language">Language</div>
  <div>
    <select id="locale-select" onchange="switchLocale(this.value)"
      style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;cursor:pointer">
    </select>
  </div>
</div>
```

- [ ] **Step 2: 修改 openSettings 函式填充下拉選項**

找到 `openSettings()` 函式（約 line 502），在 `renderThemeGrid();` 之後加入：

```javascript
const sel = document.getElementById('locale-select');
sel.innerHTML = SUPPORTED_LOCALES.map(l =>
  `<option value="${l}"${l === _currentLocale ? ' selected' : ''}>${LOCALE_NAMES[l] || l}</option>`
).join('');
```

- [ ] **Step 3: 修改 switchLocale 觸發 applyI18n 後重新 render 當前畫面**

確認 `switchLocale` 函式（Task 4 已加入）最後呼叫 `applyI18n()`。追加：如果當前有 dashboard 顯示中就重新 render，sidebar 也重新 render：

```javascript
async function switchLocale(lang) {
  localStorage.setItem('4x-locale', lang);
  await loadLocale(lang);
  applyI18n();
  document.documentElement.lang = lang;
  if (!current && activeProjectId) renderDashboard(lastTasks);
  if (activeProjectId) load();
}
```

- [ ] **Step 4: build 驗證**

```bash
go build ./cmd/4x && go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F030): add Language dropdown to Settings panel"
```

---

### Task 8: CI key 完整性驗證

**Files:**
- Create: `scripts/check-i18n.sh`
- Modify: `Makefile`

- [ ] **Step 1: 建立 check-i18n.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

LOCALES_DIR="internal/server/static/locales"
BASE="$LOCALES_DIR/en.json"

if [ ! -f "$BASE" ]; then
  echo "ERROR: base file $BASE not found"
  exit 1
fi

base_keys=$(python3 -c "import json; print('\n'.join(sorted(json.load(open('$BASE')).keys())))")
exit_code=0

for f in "$LOCALES_DIR"/*.json; do
  lang=$(basename "$f" .json)
  [ "$lang" = "en" ] && continue

  # Validate JSON
  if ! python3 -c "import json; json.load(open('$f'))" 2>/dev/null; then
    echo "ERROR: $f is not valid JSON"
    exit_code=1
    continue
  fi

  file_keys=$(python3 -c "import json; print('\n'.join(sorted(json.load(open('$f')).keys())))")

  missing=$(comm -23 <(echo "$base_keys") <(echo "$file_keys"))
  extra=$(comm -13 <(echo "$base_keys") <(echo "$file_keys"))

  if [ -n "$missing" ]; then
    echo "ERROR: $lang.json missing keys:"
    echo "$missing" | sed 's/^/  /'
    exit_code=1
  fi
  if [ -n "$extra" ]; then
    echo "WARNING: $lang.json extra keys:"
    echo "$extra" | sed 's/^/  /'
  fi
done

if [ $exit_code -eq 0 ]; then
  echo "OK: all locale files have matching keys"
fi
exit $exit_code
```

- [ ] **Step 2: 設定執行權限並測試**

```bash
chmod +x scripts/check-i18n.sh
bash scripts/check-i18n.sh
```

Expected: `OK: all locale files have matching keys`

- [ ] **Step 3: 加入 Makefile**

在 `Makefile` 的 `.PHONY` 行加 `check-i18n`，並在檔案最後新增：

```makefile
check-i18n:
	@bash scripts/check-i18n.sh
```

- [ ] **Step 4: 驗證 make target**

```bash
make check-i18n
```

Expected: OK

- [ ] **Step 5: Commit**

```bash
git add scripts/check-i18n.sh Makefile
git commit -m "feat(F030): add i18n key completeness check script"
```

---

### Task 9: 全量驗證 + Feature YAML 更新

**Files:**
- Modify: `.4x/features/F030-dashboard-i18n.yaml`

- [ ] **Step 1: 全量 build + test + lint**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

Expected: 全部 PASS

- [ ] **Step 2: i18n key 驗證**

```bash
make check-i18n
```

Expected: OK

- [ ] **Step 3: 更新 feature YAML**

把 `F030-dashboard-i18n.yaml` 的：
- `status` 改為 `in-progress`
- `rules` 的 spec/plan 路徑修正為實際路徑

```yaml
id: F030-dashboard-i18n
name: 'F030: Dashboard i18n multi-language support'
description: |
  為 Dashboard（Web UI + macOS Swift wrapper）加入 6 語言 i18n 支援：
  en, zh-TW, zh-CN, ja, ko, es。
  Locale 跟隨 localStorage 設定，翻譯檔獨立 JSON + Go embed。
  Settings 面板加 Language 下拉選單。Swift 層不動。
status: in-progress
rules:
  - "spec: docs/design/F030-dashboard-i18n-spec.md"
  - "plan: docs/design/F030-dashboard-i18n-plan.md"
```

- [ ] **Step 4: Commit**

```bash
git add .4x/features/F030-dashboard-i18n.yaml
git commit -m "chore(F030): update feature YAML with correct spec/plan paths"
```

---

### Task 10: 手動驗證清單

啟動 dashboard 並逐項驗證 i18n 功能。

- [ ] **Step 1: 啟動 dashboard**

```bash
bin/4x live --port 4580
```

- [ ] **Step 2: 驗證項目**

在瀏覽器開啟 `http://localhost:4580`，逐項確認：

1. 預設語言正確偵測（根據瀏覽器語言）
2. Settings → Language 下拉出現 6 個選項
3. 切換到 zh-TW → 所有 UI 文字變繁體中文
4. 切換到 ja → 所有 UI 文字變日文
5. 切換到 en → 回到英文
6. 重新整理頁面 → 語言選擇保留
7. Feature name、log 內容等 project data 沒有被翻譯
8. Sidebar group labels（Running/Review/Pending/Todo/Done）正確翻譯
9. Dashboard 統計區的標題和數字正確
10. Search modal 的 placeholder 正確翻譯
11. Project Settings modal 的所有 label 正確翻譯
12. Init confirm modal 的文字正確翻譯

- [ ] **Step 3: 修正問題（如有）**

遇到未翻譯的字串 → 補進 `en.json` 和其他 5 個 JSON + 加上 `data-i18n` 或 `t()` 呼叫。

- [ ] **Step 4: 最終 commit（如有修正）**

```bash
git add -A
git commit -m "fix(F030): patch missed i18n strings found during manual testing"
```
