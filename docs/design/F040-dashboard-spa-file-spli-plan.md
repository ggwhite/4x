# F040: Dashboard SPA File Split — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the 2100+ line single-file SPA into 6 files (1 HTML + 1 CSS + 4 JS) while maintaining zero functional regression.

**Architecture:** Extract inline `<style>` and `<script>` blocks from `index.html` into separate files (`style.css`, `core.js`, `settings.js`, `ui.js`, `init.js`). Change Go embed from single-file string to `embed.FS` with `http.FileServer`. JS files load via sequential `<script>` tags — no ES modules, no build tools.

**Tech Stack:** Go (embed, net/http), vanilla JS, CSS, Tailwind CDN

---

### Task 1: Update Go embed and routing in server.go

**Files:**
- Modify: `internal/server/server.go:28-32` (embed declarations)
- Modify: `internal/server/server.go:156-159` (`/` route handler)
- Modify: `internal/server/server.go:891-901` (locale handler to use new FS)
- Modify: `internal/server/server_test.go:142-153` (TestIndexHTML)

- [ ] **Step 1: Change embed declarations**

Replace the two separate embeds with one `embed.FS`:

```go
// Remove these:
//go:embed static/index.html
var indexHTML string

//go:embed static/locales/*.json
var localeFS embed.FS

// Replace with:
//go:embed static/*
var staticFS embed.FS
```

Add `"io/fs"` to imports.

- [ ] **Step 2: Change `/` route to use http.FileServer**

Replace the `/` handler in `NewMux`:

```go
// Remove:
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html")
    fmt.Fprint(w, indexHTML)
})

// Replace with:
sub, _ := fs.Sub(staticFS, "static")
mux.Handle("/", http.FileServer(http.FS(sub)))
```

Note: `http.FileServer` serves `index.html` for `/`, and also serves `style.css`, `core.js`, etc. API routes registered before `/` take precedence in `http.ServeMux`.

- [ ] **Step 3: Update locale handler to use staticFS**

In `handleGetLocale`, change `localeFS` references to `staticFS`:

```go
func handleGetLocale(w http.ResponseWriter, r *http.Request) {
    lang := strings.TrimPrefix(r.URL.Path, "/api/locales/")
    filename := "static/locales/" + lang + ".json"
    data, err := staticFS.ReadFile(filename)
    if err != nil {
        data, _ = staticFS.ReadFile("static/locales/en.json")
    }
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
    w.Write(data)
}
```

- [ ] **Step 4: Update TestIndexHTML**

`http.FileServer` returns `Content-Type: text/html; charset=utf-8` (with charset). Update the test:

```go
func TestIndexHTML(t *testing.T) {
    ws := setupServerWorkspace(t)
    rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/", "")

    if rec.Code != 200 {
        t.Fatalf("status = %d, want 200", rec.Code)
    }
    ct := rec.Header().Get("Content-Type")
    if !strings.HasPrefix(ct, "text/html") {
        t.Errorf("Content-Type = %s, want text/html*", ct)
    }
}
```

Add `"strings"` to test imports if not already present.

- [ ] **Step 5: Remove unused imports**

Remove `"fmt"` from `server.go` imports if no longer used (check first — other handlers may use it). Remove the old `localeFS` variable.

- [ ] **Step 6: Build and test**

Run: `go build ./cmd/4x && go vet ./... && go test ./internal/server/...`

Expected: all pass. The index.html still exists as a single file at this point, so the FileServer serves it correctly.

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "refactor(F040): switch to embed.FS with http.FileServer for static assets"
```

---

### Task 2: Extract style.css

**Files:**
- Create: `internal/server/static/style.css`
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: Create style.css**

Copy lines 11-133 from `index.html` (everything between `<style>` and `</style>` tags, not including the tags themselves) into a new file `internal/server/static/style.css`.

The content starts with `:root {` (CSS variables for default theme) and ends with the last animation/transition rule before `</style>`.

- [ ] **Step 2: Replace inline style with link tag**

In `index.html`, replace the entire `<style>...</style>` block (lines 10-134) with:

```html
<link rel="stylesheet" href="/style.css">
```

Place it after the `<meta>` tags but before the Tailwind script tag.

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/4x && go test ./internal/server/...`

Expected: pass (embed.FS picks up the new file automatically via `static/*` glob).

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/style.css internal/server/static/index.html
git commit -m "refactor(F040): extract inline CSS to style.css"
```

---

### Task 3: Extract core.js

**Files:**
- Create: `internal/server/static/core.js`
- Modify: `internal/server/static/index.html`

This is the foundation layer. Every other JS file depends on the globals and functions defined here.

- [ ] **Step 1: Create core.js**

Extract the following from the `<script>` block into `internal/server/static/core.js`:

1. **i18n** (lines ~400-466): `_i18nDict`, `_currentLocale`, `SUPPORTED_LOCALES`, `LOCALE_NAMES`, `t()`, `detectLocale()`, `loadLocale()`, `applyI18n()`, `switchLocale()`

2. **Global variables** (scattered near top of script): `projects`, `activeProjectId`, `openTabs`, `allTabTasks`, `current`, `lastTasks`, `lastMsgCount`, `sseSource`, `logSSE`, `settings`, `_projectSettings`, `_globalSettings`, `activeRuns`, `activeDetailTab`, `overviewCache`, `refreshTimer`

3. **Constants**: `ROLES`, `PHASE_ICON`, `PHASE_COLORS`, `SECTION_COLORS`, `RUNNER_COLORS`, `THEMES`, `S2T` (simplified-to-traditional mapping)

4. **Utility functions**: `esc()`, `escAttr()`, `fmtTokens()`, `formatDuration()`, `formatElapsed()`, `showToast()`, `apiBase()`, `sseBase()`, `normCJK()`, `fuzzyScore()`, `fuzzyMatch()`, `saveTabState()`, `loadTabState()`

Approach: Read the full `<script>` block. Identify each function/variable above by searching for its declaration. Copy them in logical order (variables → constants → utilities → i18n) into `core.js`. Do not include functions that belong to other modules (settings, ui, init).

- [ ] **Step 2: Add script tag to index.html**

At the bottom of `<body>`, before `</body>`, replace the single `<script>...</script>` block with individual script tags. For now, keep the remaining JS inline and add core.js before it:

```html
<script src="/core.js"></script>
<script>
// ... remaining JS that hasn't been extracted yet ...
</script>
</body>
```

Remove the extracted functions/variables from the inline `<script>` block.

- [ ] **Step 3: Verify build and test**

Run: `go build ./cmd/4x && go test ./internal/server/...`

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/core.js internal/server/static/index.html
git commit -m "refactor(F040): extract core.js — globals, constants, utils, i18n"
```

---

### Task 4: Extract settings.js

**Files:**
- Create: `internal/server/static/settings.js`
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: Create settings.js**

Extract the following from the remaining inline `<script>` into `internal/server/static/settings.js`:

1. **Appearance settings**: `initSettings()`, `applyTheme()`, `renderThemeGrid()`, `applyFont()`, `adjFont()`, `adjRefresh()`, `startRefreshTimer()`

2. **Project Settings**: `openProjectSettings()`, `closeProjectSettings()`, `renderProjectSettingsForm()`, `collectFormData()`, `saveProjectSettings()`, `autoSave()`, `addTag()`, `removeTag()`, `getTagItems()`, `psField()`, `psTagField()`, `filterPSFields()`, `addRunner()`, `removeRunner()`

3. **Global Settings**: `openGlobalSettings()`, `closeGlobalSettings()`, `renderGlobalSettingsForm()`, `saveGlobalSettings()`, `switchGSTab()`, `gsAddRunner()`, `gsRemoveRunner()`, `gsUpdateRunner()`, `renderGSRunners()`, `loadSupportedRunners()`, `gsSetLocale()`

All of these call functions from `core.js` (`apiBase()`, `showToast()`, `t()`, `esc()`), which is loaded first.

- [ ] **Step 2: Update script tags in index.html**

```html
<script src="/core.js"></script>
<script src="/settings.js"></script>
<script>
// ... remaining JS (ui + init) ...
</script>
</body>
```

Remove the extracted functions from the inline `<script>`.

- [ ] **Step 3: Verify build and test**

Run: `go build ./cmd/4x && go test ./internal/server/...`

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/settings.js internal/server/static/index.html
git commit -m "refactor(F040): extract settings.js — appearance, project & global settings"
```

---

### Task 5: Extract ui.js

**Files:**
- Create: `internal/server/static/ui.js`
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: Create ui.js**

Extract all remaining functions from inline `<script>` EXCEPT the `init()` function and the `init()` call. This includes:

1. **Tabs/Projects**: `renderTabs()`, `switchTab()`, `closeTab()`, `addTab()`, `loadProjects()`, `showProjectPicker()`, `renderProjectPicker()`, `confirmInit()`, `browseFiles()`, `submitBrowse()`
2. **Search**: `openSearch()`, `closeSearch()`, `renderSearchResults()`, `selectSearch()`, `onSearchKey()`, `loadAllTabTasks()`
3. **Dashboard/Sidebar**: `renderDashboard()`, `classify()`, `renderSidebar()`, `renderTaskItem()`, `phaseBadge()`, `badge()`
4. **Detail**: `loadDetail()`, `renderMsgCard()`, `loadMessages()`, `switchDetailTab()`, `loadOverview()`, `renderOverview()`
5. **SSE**: `connectSSE()`, `disconnectSSE()`, `connectLogSSE()`, `disconnectLogSSE()`
6. **Logs/Doctor**: `loadLogs()`, `viewLog()`, `showDoctor()`, `renderDoctor()`
7. **Run Management**: `getRunId()`, `openRunModal()`, `closeRunModal()`, `adjRunRounds()`, `submitRun()`, `stopRun()`, `openNewFeature()`, `closeNewFeature()`, `submitNewFeature()`
8. **Other**: `markDone()`, `showConfirm()`, `goHome()`, `load()`
9. **Keyboard handler**: the `document.addEventListener('keydown', ...)` block

- [ ] **Step 2: Update script tags in index.html**

```html
<script src="/core.js"></script>
<script src="/settings.js"></script>
<script src="/ui.js"></script>
<script>
// Only init() function and init() call remain here
</script>
</body>
```

- [ ] **Step 3: Verify build and test**

Run: `go build ./cmd/4x && go test ./internal/server/...`

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/ui.js internal/server/static/index.html
git commit -m "refactor(F040): extract ui.js — dashboard, sidebar, detail, SSE, tabs, search"
```

---

### Task 6: Extract init.js and finalize index.html

**Files:**
- Create: `internal/server/static/init.js`
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: Create init.js**

Move the remaining `init()` function and `init()` call from the inline `<script>` into `internal/server/static/init.js`:

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
  if (saved.tabs.length > 0) { for (const tab of saved.tabs) { if (projects.find(p => p.id === tab.id)) openTabs.push(tab); } activeProjectId = openTabs.find(tb => tb.id === saved.active) ? saved.active : (openTabs[0] ? openTabs[0].id : null); }
  if (openTabs.length === 0 && projects.length > 0) { projects.forEach(p => openTabs.push({ id: p.id, name: p.name })); activeProjectId = openTabs[0] ? openTabs[0].id : null; }
  saveTabState(); renderTabs();
  if (activeProjectId) load(); else renderProjectPicker();
}
init();
```

- [ ] **Step 2: Finalize index.html**

Remove the last inline `<script>` block entirely. The final `index.html` should have:

```html
<script src="/core.js"></script>
<script src="/settings.js"></script>
<script src="/ui.js"></script>
<script src="/init.js"></script>
</body>
</html>
```

Verify the file contains only: `<!DOCTYPE html>`, `<head>` with meta/link/CDN scripts/tailwind config, `<body>` with HTML markup, and 4 `<script src>` tags. No inline `<style>` or `<script>` blocks.

- [ ] **Step 3: Verify build and test**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`

Expected: all 361+ tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/server/static/init.js internal/server/static/index.html
git commit -m "refactor(F040): extract init.js, finalize index.html — split complete"
```

---

### Task 7: Manual verification

**Files:** none (read-only verification)

- [ ] **Step 1: Start dashboard**

Run: `4x live` (or `go run ./cmd/4x live`)

Open the URL in browser (default `http://localhost:4580`).

- [ ] **Step 2: Verify core functionality**

Check all of the following:

- [ ] Dashboard page renders correctly with feature cards
- [ ] Sidebar lists features with correct phase badges
- [ ] Click a feature → detail view loads with messages
- [ ] Theme switching works (try all 6 themes)
- [ ] i18n switching works (try zh-TW, then back to en)
- [ ] Cmd+K search opens, finds features, navigates correctly
- [ ] Project Settings modal opens and saves
- [ ] Global Settings modal opens and saves
- [ ] SSE events update the sidebar in real-time (if a run is active)
- [ ] Run modal opens, round adjuster works
- [ ] New Feature modal opens and submits
- [ ] Overview tab loads and renders
- [ ] Doctor page loads

- [ ] **Step 3: Verify static files are served**

Open browser DevTools → Network tab. Reload the page. Confirm these files load with 200 status:

- `/` → `index.html` (text/html)
- `/style.css` (text/css)
- `/core.js` (application/javascript or text/javascript)
- `/settings.js` (application/javascript or text/javascript)
- `/ui.js` (application/javascript or text/javascript)
- `/init.js` (application/javascript or text/javascript)

- [ ] **Step 4: Verify no console errors**

Open browser DevTools → Console. There should be zero JavaScript errors.

- [ ] **Step 5: Final commit (if any fixups needed)**

If any issues were found and fixed during verification:

```bash
git add -A
git commit -m "fix(F040): fixups from manual verification"
```

---

### Task 8: Update feature status

**Files:**
- Modify: `internal/server/static/index.html` (already done by previous tasks — just verify unstaged dashboard changes from before F040 are handled)

- [ ] **Step 1: Mark feature done**

```bash
4x transition F040 --to done
```

- [ ] **Step 2: Final commit**

```bash
git add .4x/features/F040-dashboard-spa-file-spli.yaml
git commit -m "chore(F040): mark feature done"
```
