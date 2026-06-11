# F021 — Dashboard Control Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 Alpine.js 重構 4x live Dashboard 並加入控制面板功能（play/stop、run modal、new feature、log 顯示）。

**Architecture:** 新增 `GET /api/config` endpoint（Go 側），完整重寫 `internal/server/static/index.html` 為 Alpine.js 架構。Alpine.store('app') 管理全域 state，Alpine.data() 拆分元件。維持單檔 SPA。

**Tech Stack:** Alpine.js 3 (CDN), Tailwind CSS (CDN), Go net/http

---

### Task 1: GET /api/config endpoint

Run modal 需要知道可用 runners。新增 read endpoint 回傳 config 摘要。

**Files:**
- Modify: `internal/server/server.go:21-45`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫測試**

在 `server_test.go` 加：

```go
func TestGetConfig(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "config-test"},
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude", Args: []string{"-p", "{prompt}"}},
			"gemini": {Command: "gemini", Args: []string{"{prompt}"}},
		},
		Default: "claude",
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Runners           []string `json:"runners"`
		DefaultRunner     string   `json:"defaultRunner"`
		MaxConcurrentRuns int      `json:"maxConcurrentRuns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Runners) != 2 {
		t.Errorf("runners = %d, want 2", len(result.Runners))
	}
	if result.DefaultRunner != "claude" {
		t.Errorf("defaultRunner = %q, want claude", result.DefaultRunner)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run TestGetConfig -v`
Expected: FAIL — `/api/config` 回 404 或 HTML

- [ ] **Step 3: 在 NewMux 加 config handler**

在 `server.go` 的 `NewMux` 裡加：

```go
mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
	handleConfig(ws, w)
})
```

加 handler 函式：

```go
type configResponse struct {
	Runners           []string `json:"runners"`
	DefaultRunner     string   `json:"defaultRunner"`
	MaxConcurrentRuns int      `json:"maxConcurrentRuns"`
}

func handleConfig(ws *protocol.Workspace, w http.ResponseWriter) {
	cfg, err := ws.ReadConfig()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	runners := make([]string, 0, len(cfg.Runners))
	for name := range cfg.Runners {
		runners = append(runners, name)
	}
	sort.Strings(runners)

	maxRuns := cfg.MaxConcurrentRuns
	if maxRuns <= 0 {
		maxRuns = 1
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configResponse{
		Runners:           runners,
		DefaultRunner:     cfg.Default,
		MaxConcurrentRuns: maxRuns,
	})
}
```

需要在 import 加 `"sort"`。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -run TestGetConfig -v`
Expected: PASS

- [ ] **Step 5: 跑全部測試確認沒 regression**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add GET /api/config endpoint for dashboard"
```

---

### Task 2: Alpine.js 重寫 — 基礎架構與 store

用 Alpine.js 重寫 `index.html`。這是整個 plan 最大的 task。完整取代現有 vanilla JS。

**Files:**
- Modify: `internal/server/static/index.html`（完整重寫）

**背景知識：** 現有 `index.html`（515 行）使用 vanilla JS + innerHTML 渲染。有 multi-project tab、Cmd+K 搜尋、Cmd+, 設定、dashboard 統計、feature detail + messages、SSE。所有這些需要遷移到 Alpine.js 並保持功能不變。

- [ ] **Step 1: 確認現有功能**

啟動 dev server 確認現有 dashboard 可正常運作：

```bash
go build ./cmd/4x && bin/4x live --port 4580
```

在瀏覽器開 `http://localhost:4580`，確認：sidebar 列表、dashboard 統計、feature detail、Cmd+K、Cmd+,、tab 切換 都正常。然後 kill server。

- [ ] **Step 2: 寫 `<head>` — CSS + Alpine/Tailwind CDN**

保持現有的 CSS 不變（`:root` variables、theme variants、modal/stepper/theme-card 樣式、tab 樣式）。

在 `<head>` 加 Alpine.js CDN（在 Tailwind 之後）：

```html
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3/dist/cdn.min.js"></script>
```

加兩組新 CSS for 控制面板：

```css
.btn-play { padding: 4px; border-radius: 6px; color: var(--accent); cursor: pointer; opacity: 0; transition: opacity .15s; }
.btn-play:hover { background: var(--bg-hover); }
*:hover > .btn-play { opacity: 1; }
.btn-stop { padding: 4px; border-radius: 6px; color: #f87171; cursor: pointer; transition: background .15s; }
.btn-stop:hover { background: rgba(248,113,113,.1); }
.log-card { font-family: ui-monospace, monospace; font-size: var(--font-code); padding: 8px 16px; white-space: pre-wrap; word-break: break-all; }
.log-stdout { background: rgba(39,39,42,.3); color: var(--text-2); border-left: 2px solid var(--border); }
.log-stderr { background: rgba(248,113,113,.05); color: #fca5a5; border-left: 2px solid #f87171; }
```

- [ ] **Step 3: 寫 Alpine.store('app') — 全域狀態**

在 `<script>` 區塊開頭用 `document.addEventListener('alpine:init', ...)` 註冊 store：

```js
document.addEventListener('alpine:init', () => {
  Alpine.store('app', {
    // ── State ──
    projects: [],
    openTabs: [],
    activeProjectId: null,
    allTabTasks: {},
    tasks: [],
    current: null,
    runs: [],
    config: { runners: [], defaultRunner: '', maxConcurrentRuns: 1 },
    messages: [],
    sseSource: null,
    refreshTimer: null,
    sseLogEntries: [],

    // ── Modal state ──
    searchOpen: false,
    settingsOpen: false,
    pickerOpen: false,
    runModalOpen: false,
    runModalFeatureId: '',
    newModalOpen: false,

    // ── Run modal form ──
    runForm: { runner: '', maxRounds: 5, extraPrompt: '' },
    // ── New feature form ──
    newForm: { name: '', description: '' },

    // ── Settings ──
    settings: { theme: 'apple-dark', fontContent: 15, fontCode: 13, refresh: 3,
      ...JSON.parse(localStorage.getItem('4x-settings') || '{}') },

    // ── Computed helpers ──
    apiBase() { return this.activeProjectId ? '/api/project/' + this.activeProjectId : ''; },
    sseBase() { return this.activeProjectId ? '/sse/project/' + this.activeProjectId : ''; },
    isRunning(featureId) { return this.runs.some(r => r.featureId === featureId); },
    runIdFor(featureId) { const r = this.runs.find(r => r.featureId === featureId); return r ? r.id : null; },
    classified() {
      const g = { running: [], pending: [], todo: [], done: [] };
      (this.tasks || []).forEach(t => {
        const a = t.active && t.phase && t.phase !== 'done';
        if (a) g.running.push(t);
        else if (t.status === 'done') g.done.push(t);
        else if (t.status === 'in-progress') g.pending.push(t);
        else g.todo.push(t);
      });
      return g;
    },

    // ── API methods ──
    async loadTasks() {
      if (!this.activeProjectId) return;
      try {
        this.tasks = await (await fetch(this.apiBase() + '/api/tasks')).json() || [];
      } catch { this.tasks = []; }
    },
    async loadRuns() {
      if (!this.activeProjectId) return;
      try {
        this.runs = await (await fetch(this.apiBase() + '/api/runs')).json() || [];
      } catch { this.runs = []; }
    },
    async loadConfig() {
      if (!this.activeProjectId) return;
      try {
        this.config = await (await fetch(this.apiBase() + '/api/config')).json();
      } catch {}
    },
    async loadMessages(featureId) {
      if (!this.activeProjectId || !featureId) return;
      try {
        this.messages = await (await fetch(this.apiBase() + '/api/messages/' + featureId)).json() || [];
      } catch { this.messages = []; }
    },
    async loadProjects() {
      try { this.projects = await (await fetch('/api/projects')).json() || []; }
      catch { this.projects = []; }
    },
    async loadAllTabTasks() {
      for (const tab of this.openTabs) {
        try {
          const tasks = await (await fetch('/api/project/' + tab.id + '/api/tasks')).json();
          this.allTabTasks[tab.id] = (tasks || []).map(t => ({ ...t, _projectId: tab.id, _projectName: tab.name }));
        } catch { this.allTabTasks[tab.id] = []; }
      }
    },

    // ── SSE ──
    connectSSE(featureId) {
      this.disconnectSSE();
      this.sseLogEntries = [];
      this.sseSource = new EventSource(this.sseBase() + '/events/' + featureId);
      this.sseSource.onmessage = (e) => {
        try {
          const evt = JSON.parse(e.data);
          if (evt.type === 'run-output' || evt.type === 'run-error') {
            this.sseLogEntries.push(evt);
          }
        } catch {}
        this.loadMessages(featureId);
      };
    },
    disconnectSSE() {
      if (this.sseSource) { this.sseSource.close(); this.sseSource = null; }
    },

    // ── Navigation ──
    selectFeature(task) {
      this.current = task.id;
      this.sseLogEntries = [];
      this.connectSSE(task.id);
      this.loadMessages(task.id);
    },
    goHome() {
      this.current = null;
      this.messages = [];
      this.sseLogEntries = [];
      this.disconnectSSE();
    },

    // ── Tabs ──
    saveTabState() { localStorage.setItem('4x-tabs', JSON.stringify({ tabs: this.openTabs, active: this.activeProjectId })); },
    loadTabState() { try { const s = JSON.parse(localStorage.getItem('4x-tabs') || '{}'); return { tabs: s.tabs || [], active: s.active || null }; } catch { return { tabs: [], active: null }; } },
    switchTab(pid) {
      this.activeProjectId = pid; this.current = null;
      this.messages = []; this.sseLogEntries = [];
      this.disconnectSSE(); this.saveTabState();
      this.loadTasks(); this.loadRuns(); this.loadConfig();
    },
    closeTab(pid) {
      this.openTabs = this.openTabs.filter(t => t.id !== pid);
      if (this.activeProjectId === pid) {
        this.activeProjectId = this.openTabs.length > 0 ? this.openTabs[0].id : null;
        this.current = null; this.disconnectSSE();
      }
      this.saveTabState();
      if (this.activeProjectId) { this.loadTasks(); this.loadRuns(); }
    },
    addTab(project) {
      if (!this.openTabs.find(t => t.id === project.id)) this.openTabs.push({ id: project.id, name: project.name });
      this.activeProjectId = project.id; this.saveTabState();
      this.loadTasks(); this.loadRuns(); this.loadConfig();
    },

    // ── Control panel actions ──
    openRunModal(featureId) {
      this.runModalFeatureId = featureId;
      this.runForm.runner = this.config.defaultRunner || '';
      this.runForm.maxRounds = 5;
      this.runForm.extraPrompt = '';
      this.runModalOpen = true;
    },
    async submitRun() {
      const body = { featureId: this.runModalFeatureId, runner: this.runForm.runner, maxRounds: this.runForm.maxRounds };
      try {
        const resp = await fetch(this.apiBase() + '/api/run', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (resp.ok) {
          this.runModalOpen = false;
          this.loadTasks(); this.loadRuns();
        } else {
          alert(await resp.text());
        }
      } catch (e) { alert('Connection error'); }
    },
    async stopRun(featureId) {
      const runId = this.runIdFor(featureId);
      if (!runId) return;
      try {
        await fetch(this.apiBase() + '/api/stop', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: runId }) });
        this.loadTasks(); this.loadRuns();
      } catch {}
    },
    async submitNewFeature() {
      if (!this.newForm.name.trim()) return;
      try {
        const resp = await fetch(this.apiBase() + '/api/new', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: this.newForm.name, description: this.newForm.description }) });
        if (resp.ok) {
          this.newModalOpen = false;
          this.newForm.name = ''; this.newForm.description = '';
          this.loadTasks();
        } else { alert(await resp.text()); }
      } catch { alert('Connection error'); }
    },

    // ── Settings ──
    saveSettings() { localStorage.setItem('4x-settings', JSON.stringify(this.settings)); },
    applyTheme(id) {
      this.settings.theme = id;
      document.documentElement.setAttribute('data-theme', id === 'apple-dark' ? '' : id);
      this.saveSettings();
    },
    applyFont() {
      document.documentElement.style.setProperty('--font-content', this.settings.fontContent + 'px');
      document.documentElement.style.setProperty('--font-code', this.settings.fontCode + 'px');
    },
    adjFont(type, delta) {
      if (type === 'content') this.settings.fontContent = Math.min(24, Math.max(12, this.settings.fontContent + delta));
      else this.settings.fontCode = Math.min(20, Math.max(10, this.settings.fontCode + delta));
      this.saveSettings(); this.applyFont();
    },
    adjRefresh(delta) {
      this.settings.refresh = Math.min(30, Math.max(1, this.settings.refresh + delta));
      this.saveSettings(); this.startRefreshTimer();
    },
    startRefreshTimer() {
      if (this.refreshTimer) clearInterval(this.refreshTimer);
      this.refreshTimer = setInterval(() => {
        if (this.activeProjectId) { this.loadTasks(); this.loadRuns(); if (this.current) this.loadMessages(this.current); }
      }, this.settings.refresh * 1000);
    },

    // ── Init ──
    async init() {
      this.applyTheme(this.settings.theme);
      this.applyFont();
      await this.loadProjects();
      const saved = this.loadTabState();
      if (saved.tabs.length > 0) {
        for (const tab of saved.tabs) { if (this.projects.find(p => p.id === tab.id)) this.openTabs.push(tab); }
        this.activeProjectId = this.openTabs.find(t => t.id === saved.active) ? saved.active : (this.openTabs[0] ? this.openTabs[0].id : null);
      }
      if (this.openTabs.length === 0 && this.projects.length > 0) {
        this.projects.forEach(p => this.openTabs.push({ id: p.id, name: p.name }));
        this.activeProjectId = this.openTabs[0] ? this.openTabs[0].id : null;
      }
      this.saveTabState();
      if (this.activeProjectId) { this.loadTasks(); this.loadRuns(); this.loadConfig(); }
      this.startRefreshTimer();
    }
  });
});
```

- [ ] **Step 4: 寫 utility 函式（在 store 之外）**

這些函式不屬於 Alpine store，放在 `<script>` 最頂層：

```js
const ROLES = {
  designer:       { name: 'Designer',    color: '#a855f7', bg: 'rgba(168,85,247,.08)' },
  coder:          { name: 'Coder',       color: '#06b6d4', bg: 'rgba(6,182,212,.08)' },
  reviewer:       { name: 'Reviewer',    color: '#22c55e', bg: 'rgba(34,197,94,.08)' },
  'deep-reviewer':{ name: 'Deep Review', color: '#22c55e', bg: 'rgba(34,197,94,.08)' },
  tester:         { name: 'Tester',      color: '#f97316', bg: 'rgba(249,115,22,.08)' },
  acceptor:       { name: 'Acceptor',    color: '#eab308', bg: 'rgba(234,179,8,.08)' },
};

const THEMES = [
  { id: 'apple-dark', name: 'Apple Dark', bg: '#0f0f0f', fg: '#e5e5e5', line: '#333' },
  { id: 'midnight',   name: 'Midnight',   bg: '#0a0e1a', fg: '#c8d6e5', line: '#1e3a5f' },
  { id: 'noir',       name: 'Noir',       bg: '#000000', fg: '#a0a0a0', line: '#222' },
  { id: 'frost',      name: 'Frost',      bg: '#0f172a', fg: '#e2e8f0', line: '#334155' },
  { id: 'light',      name: 'Light',      bg: '#f5f5f5', fg: '#18181b', line: '#ddd' },
  { id: 'paper',      name: 'Paper',      bg: '#faf8f5', fg: '#1c1917', line: '#d6cfc7' },
];

const PHASE_ICON = { designing:'◆',coding:'◆',reviewing:'◆',testing:'◆',accepting:'◆',amending:'◆',done:'✓',blocked:'✕','needs-attention':'!',init:'○','not-started':'○' };

function esc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;'); }

// 繁簡中文正規化 — 搬移現有的 S2T_S, S2T_T, _cjkMap, normCJK, fuzzyMatch
// （原封不動複製現有 index.html 第 249-254 行的程式碼）
```

- [ ] **Step 5: 寫 HTML body — 主結構**

`<body>` 用 `x-init` 啟動 store：

```html
<body class="min-h-screen" x-data x-init="$store.app.init()">
```

**Tab bar:**

```html
<div id="tab-bar" class="flex items-center border-b overflow-hidden" style="min-height:36px;background:var(--bg-sidebar);border-color:var(--border)">
  <template x-for="tab in $store.app.openTabs" :key="tab.id">
    <div class="tab-item" :class="{ active: tab.id === $store.app.activeProjectId }"
         @click="$store.app.switchTab(tab.id)">
      <span x-text="tab.name"></span>
      <span class="tab-close" @click.stop="$store.app.closeTab(tab.id)">&times;</span>
    </div>
  </template>
  <button class="px-3 py-1.5 text-sm" style="color:var(--text-4)" @click="$store.app.pickerOpen = true">+</button>
</div>
```

**Sidebar:**

```html
<div class="w-80 border-r overflow-y-auto flex flex-col" style="border-color:var(--border)">
  <!-- Header with + button -->
  <div class="p-4 border-b flex items-center gap-2" style="border-color:var(--border)">
    <div class="w-2.5 h-2.5 rounded-full bg-emerald-500 pulse-dot cursor-pointer" @click="$store.app.goHome()"></div>
    <h1 class="text-base font-bold tracking-tight cursor-pointer" @click="$store.app.goHome()">4x Live</h1>
    <button class="ml-auto w-6 h-6 flex items-center justify-center rounded text-sm hover:bg-zinc-800/50"
            style="color:var(--text-3)" @click="$store.app.newModalOpen = true" title="New feature">+</button>
  </div>

  <!-- Feature list -->
  <div class="flex-1 overflow-y-auto p-2">
    <template x-for="[title, items] in [
      ['Running', $store.app.classified().running],
      ['Pending', $store.app.classified().pending],
      ['Todo', $store.app.classified().todo],
      ['Done', $store.app.classified().done]
    ]">
      <div x-show="items.length > 0">
        <div class="flex items-center gap-2 px-2 py-2 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">
          <span x-text="title"></span>
          <span class="ml-auto text-zinc-600" x-text="items.length"></span>
        </div>
        <template x-for="t in items" :key="t.id">
          <div class="p-3 rounded-lg cursor-pointer mb-1 transition-all duration-150 border group"
               :class="{
                 'bg-emerald-950/40 border-emerald-500/20 hover:border-emerald-500/40': t.active && t.phase && t.phase !== 'done',
                 'bg-zinc-800/80 border-zinc-700/50': t.id === $store.app.current && !(t.active && t.phase && t.phase !== 'done'),
                 'border-transparent opacity-40 hover:opacity-70': t.status === 'done' && t.id !== $store.app.current,
                 'border-transparent hover:bg-zinc-800/50': t.status !== 'done' && t.id !== $store.app.current && !(t.active && t.phase && t.phase !== 'done')
               }"
               @click="$store.app.selectFeature(t)">
            <div class="flex items-start gap-2">
              <span x-show="t.status === 'done'" class="text-emerald-500/60 text-xs">✓</span>
              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2">
                  <span class="text-[13px] font-medium truncate flex-1" x-text="t.name"></span>
                  <!-- Play/Stop button -->
                  <button x-show="$store.app.isRunning(t.id)" class="btn-stop" style="opacity:1"
                          @click.stop="$store.app.stopRun(t.id)" title="Stop">■</button>
                  <button x-show="!$store.app.isRunning(t.id)" class="btn-play"
                          @click.stop="$store.app.openRunModal(t.id)" title="Run">▶</button>
                </div>
                <div class="text-[11px] text-zinc-600 mt-0.5" x-text="t.id"></div>
                <div x-show="t.active && t.phase && t.phase !== 'done'" class="flex items-center gap-1.5 mt-1.5">
                  <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span>
                  <span class="text-[11px] text-emerald-400" x-text="t.phase"></span>
                  <span class="text-[11px] text-zinc-600" x-text="'· Round ' + (t.round || 0)"></span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </template>
  </div>
</div>
```

**Main area (detail header + content):**

```html
<div class="flex-1 flex flex-col overflow-hidden">
  <!-- Detail header (shown when a feature is selected) -->
  <div x-show="$store.app.current" class="border-b p-4" style="border-color:var(--border)">
    <template x-if="$store.app.current">
      <div>
        <div class="flex items-center gap-3">
          <span class="text-sm" style="color:var(--text-4)" x-text="$store.app.current"></span>
          <span class="font-bold" x-text="($store.app.tasks.find(t => t.id === $store.app.current) || {}).name"></span>
          <div class="ml-auto flex items-center gap-2">
            <!-- Play/Stop in header -->
            <button x-show="$store.app.isRunning($store.app.current)" class="btn-stop"
                    @click="$store.app.stopRun($store.app.current)" title="Stop" style="opacity:1">■ Stop</button>
            <button x-show="!$store.app.isRunning($store.app.current)"
                    class="px-3 py-1 rounded-lg text-xs font-semibold border transition-colors"
                    style="color:var(--accent);border-color:var(--accent)"
                    @click="$store.app.openRunModal($store.app.current)">▶ Run</button>
          </div>
        </div>
      </div>
    </template>
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-y-auto p-6">
    <!-- Dashboard (no feature selected) -->
    <div x-show="!$store.app.current && $store.app.activeProjectId">
      <!-- 遷移現有 renderDashboard 的 HTML，用 Alpine 的 x-text/x-for 取代 innerHTML -->
      <!-- 沿用現有的 summary cards、donut chart、rounds distribution、recent completions、running details -->
      <!-- 結構與樣式完全保持原樣，只把 ${...} 替換為 x-text/x-bind -->
    </div>

    <!-- No project selected -->
    <div x-show="!$store.app.activeProjectId" class="flex items-center justify-center min-h-[60vh] flex-col gap-4">
      <div class="text-2xl font-bold">4x Live</div>
      <div class="text-sm" style="color:var(--text-3)">Select a project to get started</div>
      <button @click="$store.app.pickerOpen = true" class="mt-2 px-6 py-2.5 rounded-lg text-sm font-semibold cursor-pointer"
              style="background:var(--accent);color:#000">Open Project...</button>
    </div>

    <!-- Messages (feature selected) -->
    <div x-show="$store.app.current" class="space-y-4">
      <!-- Role artifacts -->
      <template x-for="m in $store.app.messages" :key="m.file + '-' + m.round">
        <div class="fade-in rounded-lg overflow-hidden">
          <div class="flex items-center gap-2 px-4 py-2"
               :style="'background:' + (ROLES[m.role] || {bg:'rgba(100,100,100,.05)'}).bg">
            <span class="w-1.5 h-1.5 rounded-full" :style="'background:' + (ROLES[m.role] || {color:'#666'}).color"></span>
            <span class="text-xs font-semibold" :style="'color:' + (ROLES[m.role] || {color:'#666'}).color"
                  x-text="(ROLES[m.role] || {name:m.role}).name"></span>
            <span class="text-xs text-zinc-600" x-text="m.label + (m.round ? ' · Round ' + m.round : '')"></span>
          </div>
          <div class="px-4 py-3 text-[13px] text-zinc-300 leading-relaxed whitespace-pre-wrap border-l-2"
               :style="'border-color:' + (ROLES[m.role] || {color:'#666'}).color + '20'"
               x-text="m.content.slice(0, 3000)"></div>
        </div>
      </template>

      <!-- SSE log entries (run-output / run-error) -->
      <template x-for="(entry, idx) in $store.app.sseLogEntries" :key="'log-' + idx">
        <div class="fade-in rounded-lg overflow-hidden log-card"
             :class="entry.type === 'run-error' ? 'log-stderr' : 'log-stdout'"
             x-text="entry.detail"></div>
      </template>

      <!-- Empty state -->
      <div x-show="$store.app.messages.length === 0 && $store.app.sseLogEntries.length === 0"
           class="text-zinc-600 text-sm mt-8 text-center">No artifacts yet</div>
    </div>
  </div>
</div>
```

- [ ] **Step 6: 寫 modals — Run Modal**

```html
<!-- Run Modal -->
<div class="modal-backdrop" :class="{ open: $store.app.runModalOpen }"
     @click.self="$store.app.runModalOpen = false">
  <div class="modal-panel fade-in" style="width:480px">
    <div style="padding:20px 24px 16px;border-bottom:1px solid var(--border)">
      <div style="font-size:16px;font-weight:700">Run Feature</div>
      <div class="text-xs mt-1" style="color:var(--text-3)" x-text="$store.app.runModalFeatureId"></div>
    </div>
    <div style="padding:20px 24px">
      <div style="display:flex;flex-direction:column;gap:16px">
        <!-- Runner -->
        <div>
          <label class="text-xs font-semibold block mb-2" style="color:var(--text-3)">Runner</label>
          <select x-model="$store.app.runForm.runner"
                  style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
            <template x-for="r in $store.app.config.runners" :key="r">
              <option :value="r" x-text="r"></option>
            </template>
          </select>
        </div>
        <!-- Max Rounds -->
        <div>
          <label class="text-xs font-semibold block mb-2" style="color:var(--text-3)">Max Rounds</label>
          <input type="number" x-model.number="$store.app.runForm.maxRounds" min="1" max="20"
                 style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
        </div>
        <!-- Extra Prompt -->
        <div>
          <label class="text-xs font-semibold block mb-2" style="color:var(--text-3)">Extra Prompt</label>
          <textarea x-model="$store.app.runForm.extraPrompt" rows="3" placeholder="Optional..."
                    style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;resize:vertical"></textarea>
        </div>
      </div>
    </div>
    <div style="padding:12px 24px 20px;display:flex;justify-content:flex-end;gap:8px">
      <button @click="$store.app.runModalOpen = false"
              style="padding:8px 16px;background:none;border:1px solid var(--border);border-radius:8px;color:var(--text-2);font-size:13px;cursor:pointer">Cancel</button>
      <button @click="$store.app.submitRun()"
              style="padding:8px 16px;background:var(--accent);color:#000;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer">Run ▶</button>
    </div>
  </div>
</div>
```

- [ ] **Step 7: 寫 modals — New Feature Modal**

```html
<!-- New Feature Modal -->
<div class="modal-backdrop" :class="{ open: $store.app.newModalOpen }"
     @click.self="$store.app.newModalOpen = false">
  <div class="modal-panel fade-in" style="width:480px">
    <div style="padding:20px 24px 16px;border-bottom:1px solid var(--border)">
      <span style="font-size:16px;font-weight:700">New Feature</span>
    </div>
    <div style="padding:20px 24px">
      <div style="display:flex;flex-direction:column;gap:16px">
        <div>
          <label class="text-xs font-semibold block mb-2" style="color:var(--text-3)">Name *</label>
          <input type="text" x-model="$store.app.newForm.name" placeholder="Feature name..."
                 style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none"
                 @keydown.enter="$store.app.submitNewFeature()">
        </div>
        <div>
          <label class="text-xs font-semibold block mb-2" style="color:var(--text-3)">Description</label>
          <textarea x-model="$store.app.newForm.description" rows="3" placeholder="Optional..."
                    style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;resize:vertical"></textarea>
        </div>
      </div>
    </div>
    <div style="padding:12px 24px 20px;display:flex;justify-content:flex-end;gap:8px">
      <button @click="$store.app.newModalOpen = false"
              style="padding:8px 16px;background:none;border:1px solid var(--border);border-radius:8px;color:var(--text-2);font-size:13px;cursor:pointer">Cancel</button>
      <button @click="$store.app.submitNewFeature()"
              style="padding:8px 16px;background:var(--accent);color:#000;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer">Create</button>
    </div>
  </div>
</div>
```

- [ ] **Step 8: 遷移 Search Modal**

將現有 search modal 改為 Alpine：

```html
<!-- Search Modal (Cmd+K) -->
<div class="modal-backdrop" :class="{ open: $store.app.searchOpen }"
     @click.self="$store.app.searchOpen = false"
     x-data="searchModal()" @keydown.escape.window="$store.app.searchOpen = false">
  <!-- 結構跟現有一樣，但用 x-model, x-for, @input, @keydown 取代 oninput/onkeydown -->
  <!-- fuzzyMatch 和 normCJK 保持不變 -->
</div>
```

search modal 用 `Alpine.data('searchModal', ...)` 封裝自己的 local state（`query`、`selectedIdx`、`filtered`），保持跟現有相同的 fuzzy match、繁簡正規化、跨 tab 搜尋、`@project` scope 過濾。

- [ ] **Step 9: 遷移 Settings Modal + Project Picker Modal**

同理，將現有的 settings modal 和 project picker modal 改為 Alpine `x-show`/`x-model`。邏輯保持不變。

- [ ] **Step 10: 加全域快捷鍵**

```html
<div x-data @keydown.window="
  if (($event.metaKey || $event.ctrlKey) && $event.key === 'k') { $event.preventDefault(); $store.app.searchOpen = true; }
  else if (($event.metaKey || $event.ctrlKey) && $event.key === ',') { $event.preventDefault(); $store.app.settingsOpen = true; }
  else if ($event.key === 'Escape') {
    if ($store.app.searchOpen) $store.app.searchOpen = false;
    else if ($store.app.settingsOpen) $store.app.settingsOpen = false;
    else if ($store.app.pickerOpen) $store.app.pickerOpen = false;
    else if ($store.app.runModalOpen) $store.app.runModalOpen = false;
    else if ($store.app.newModalOpen) $store.app.newModalOpen = false;
  }
"></div>
```

- [ ] **Step 11: 組裝完整 index.html 並確認 build**

將上述所有段落組裝成完整的 `internal/server/static/index.html`。確認：

Run: `go build ./cmd/4x && go vet ./...`
Expected: 成功（`go:embed` 會嵌入新的 index.html）

- [ ] **Step 12: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat: rewrite dashboard with Alpine.js + control panel"
```

---

### Task 3: 手動驗證

- [ ] **Step 1: 啟動 dev server**

```bash
bin/4x live --port 4580
```

- [ ] **Step 2: 驗證既有功能**

在瀏覽器開 `http://localhost:4580`，逐項確認：

- Sidebar 顯示 feature 列表，分 Running/Pending/Todo/Done 四組
- 點擊 feature → 進 detail view，顯示 messages
- Dashboard 首頁 → summary cards、donut chart、rounds distribution
- Cmd+K → 搜尋 modal，fuzzy match、繁簡正規化可用
- Cmd+, → 設定 modal，theme 切換、font 調整、refresh 調整
- Tab bar → 切換專案、關閉 tab
- SSE → 執行中 feature 的 messages 會即時更新

- [ ] **Step 3: 驗證新功能**

- Sidebar feature 卡片 hover → 顯示 ▶ play 按鈕
- 點 ▶ → 開 run modal，runner 下拉有值、max rounds 預設 5
- 直接按 Run → 呼叫 API（看 network tab 確認）
- Detail header → 也有 Run/Stop 按鈕
- Sidebar 頂部 + 按鈕 → 開 new feature modal
- 填 name 按 Create → sidebar 出現新 feature
- （如果 F020 已實作）執行中 feature 的 messages 區有 log card

- [ ] **Step 4: 確認 Go 測試全過**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: 最終 commit（如有修正）**

確認 `git status` 乾淨。
