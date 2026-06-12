# F027 — Live Settings Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dashboard 提供 VSCode 風格的全頁設定頁，編輯 `.4x/settings.json` 的所有欄位，支援 auto-save、JSON 模式、搜尋

**Architecture:** 後端新增 `GET/PUT /api/settings` 以 raw JSON roundtrip 保留未知欄位。前端在 `index.html` 加入設定頁（左側分類導航、右側表單）、auto-save（debounce 300ms）、全頁 JSON 模式切換、欄位搜尋過濾。

**Tech Stack:** Go (net/http, encoding/json, os), vanilla JS, Tailwind CSS (CDN)

---

### Task 1: 後端 Settings API

**Files:**
- Modify: `internal/server/server.go:1-17` (imports), `internal/server/server.go:85` (route registration)
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫測試**

在 `internal/server/server_test.go` 底部加入：

```go
func TestGetSettings(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/settings", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	proj, ok := cfg["project"].(map[string]interface{})
	if !ok {
		t.Fatal("project section missing")
	}
	if proj["name"] != "server-test" {
		t.Errorf("project.name = %v, want server-test", proj["name"])
	}
}

func TestPutSettings(t *testing.T) {
	ws := setupServerWorkspace(t)
	handler := NewMux(ws, nil)

	body := `{"project":{"name":"updated"},"runners":{},"default_runner":"claude"}`
	rec := serveRequest(t, handler, http.MethodPut, "/api/settings", body)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Verify written
	cfg, err := ws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Name != "updated" {
		t.Errorf("project.name = %s, want updated", cfg.Project.Name)
	}

	// Verify backup
	bakPath := filepath.Join(ws.DotDir(), "settings.json.bak")
	if _, err := os.Stat(bakPath); err != nil {
		t.Error("settings.json.bak should exist")
	}
}

func TestPutSettings_InvalidJSON(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", `{bad json}`)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutSettings_MissingProjectName(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", `{"project":{"name":""}}`)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutSettings_PreservesUnknownFields(t *testing.T) {
	ws := setupServerWorkspace(t)
	handler := NewMux(ws, nil)

	body := `{"project":{"name":"test"},"runners":{},"default_runner":"","custom_field":"preserved"}`
	rec := serveRequest(t, handler, http.MethodPut, "/api/settings", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	// Re-read raw file and check custom_field survived
	getRec := serveRequest(t, handler, http.MethodGet, "/api/settings", "")
	var result map[string]interface{}
	json.NewDecoder(getRec.Body).Decode(&result)
	if result["custom_field"] != "preserved" {
		t.Errorf("custom_field lost, got %v", result)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run TestGetSettings -v && go test ./internal/server/ -run TestPutSettings -v`
Expected: FAIL — handler 不存在

- [ ] **Step 3: 實作 handler**

在 `internal/server/server.go` 的 import 加 `"bytes"` 和 `"io"`。

在 `readIfExists` 函式後面加：

```go
// handleGetSettings 回傳 .4x/settings.json 的原始 JSON 內容
func handleGetSettings(ws *protocol.Workspace, w http.ResponseWriter) {
	path := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "settings not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutSettings 驗證並寫入 settings.json，寫入前備份為 .bak
func handlePutSettings(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	if !json.Valid(body) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid JSON"}`)
		return
	}

	var check struct {
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
	}
	json.Unmarshal(body, &check)
	if check.Project.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"project.name is required"}`)
		return
	}

	var pretty bytes.Buffer
	json.Indent(&pretty, body, "", "  ")

	settingsPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	if existing, err := os.ReadFile(settingsPath); err == nil {
		os.WriteFile(settingsPath+".bak", existing, 0o644)
	}

	if err := os.WriteFile(settingsPath, append(pretty.Bytes(), '\n'), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}
```

在 `NewMux` 函式裡，`mux.HandleFunc("/api/done", ...)` 之前加：

```go
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetSettings(ws, w)
		case http.MethodPut:
			handlePutSettings(ws, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -v`
Expected: 全部 PASS（含新增的 5 個 settings 測試）

注意：`TestPutSettings` 需要 import `"os"` 和 `"path/filepath"`。若 `server_test.go` 尚未 import，在 import 區加入。

- [ ] **Step 5: 驗證編譯 + commit**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(F027): add GET/PUT /api/settings for raw JSON roundtrip"
```

---

### Task 2: 前端設定頁 — HTML 結構與核心函式

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 加齒輪 icon 到 sidebar header**

把 sidebar header 裡的 `Auto-refresh` span：

```html
      <span class="text-[10px] ml-auto" style="color:var(--text-4)">Auto-refresh</span>
```

改成：

```html
      <span class="text-[10px] ml-auto" style="color:var(--text-4)">Auto-refresh</span>
      <button onclick="event.stopPropagation();openProjectSettings()" class="ml-1 p-1 rounded hover:brightness-150 transition-colors" style="color:var(--text-3);font-size:14px" title="Project Settings (⌘,)">⚙</button>
```

- [ ] **Step 2: 加設定頁 HTML 結構**

在 `</div>` (closing `#main` div，即 `<div id="logs-panel">` 之後、`</div>` `</div>` closing main column 之前) 加入：

找到這段：

```html
    </div>
  </div>
</div>
```

即 `#main` 的結尾 `</div>` 後、main column 的 `</div>` 前，插入：

```html
    <div id="project-settings" class="hidden flex-1 flex flex-col overflow-hidden">
      <div class="border-b p-3 flex items-center gap-3" style="border-color:var(--border)">
        <button onclick="closeProjectSettings()" class="text-sm px-2 py-1 rounded hover:brightness-150" style="color:var(--text-3)">← Back</button>
        <input id="settings-search" type="text" placeholder="Search settings..." autocomplete="off"
          class="flex-1 text-sm px-3 py-1.5 rounded-lg outline-none" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)"
          oninput="renderSettingsContent()">
        <span id="settings-saved" class="text-xs transition-opacity" style="color:var(--accent);display:none">Saved</span>
        <span id="settings-error" class="text-xs" style="color:#f87171;display:none"></span>
        <label class="flex items-center gap-1.5 text-xs cursor-pointer" style="color:var(--text-3)">
          <input type="checkbox" id="json-mode-toggle" onchange="toggleJsonMode()"> JSON
        </label>
      </div>
      <div class="flex flex-1 overflow-hidden">
        <div class="w-44 border-r overflow-y-auto py-2 px-2 flex-shrink-0" style="border-color:var(--border)" id="settings-nav"></div>
        <div class="flex-1 overflow-y-auto p-6" id="settings-content"></div>
      </div>
    </div>
```

- [ ] **Step 3: 加 JS 狀態與工具函式**

在 `<script>` 區的 `let refreshTimer = null;` 後面加：

```js
let configData = null;
let activeSettingsCategory = 'project';
let settingsOpen = false;
let settingsSaveTimer = null;

function getNested(obj, path) {
  const keys = path.split('.');
  let o = obj;
  for (const k of keys) { if (!o || typeof o !== 'object') return undefined; o = o[k]; }
  return o;
}
function setNested(obj, path, value) {
  const keys = path.split('.');
  let o = obj;
  for (let i = 0; i < keys.length - 1; i++) { if (!o[keys[i]] || typeof o[keys[i]] !== 'object') o[keys[i]] = {}; o = o[keys[i]]; }
  o[keys[keys.length-1]] = value;
}
```

- [ ] **Step 4: 加 open/close 函式**

在上一步加的程式碼後面加：

```js
async function openProjectSettings() {
  if (!activeProjectId) return;
  try {
    const resp = await fetch(apiBase()+'/api/settings');
    if (!resp.ok) { alert('Failed to load settings'); return; }
    configData = await resp.json();
  } catch { alert('Failed to load settings'); return; }
  settingsOpen = true;
  document.getElementById('header').classList.add('hidden');
  document.getElementById('main').classList.add('hidden');
  document.getElementById('project-settings').classList.remove('hidden');
  document.getElementById('json-mode-toggle').checked = false;
  document.getElementById('settings-search').value = '';
  activeSettingsCategory = 'project';
  renderSettingsNav();
  renderSettingsContent();
}

function closeProjectSettings() {
  settingsOpen = false;
  document.getElementById('project-settings').classList.add('hidden');
  document.getElementById('main').classList.remove('hidden');
  if (current) document.getElementById('header').classList.remove('hidden');
  if (!current) renderDashboard(lastTasks);
}
```

- [ ] **Step 5: 加 nav + content 渲染**

```js
function renderSettingsNav() {
  const cats = [{id:'project',label:'Project'},{id:'runners',label:'Runners'},{id:'roles',label:'Roles'},{id:'general',label:'General'}];
  document.getElementById('settings-nav').innerHTML = cats.map(c =>
    `<div class="px-3 py-2 rounded cursor-pointer text-sm transition-colors" style="${c.id===activeSettingsCategory?'background:var(--bg-hover);color:var(--text-1);font-weight:600':'color:var(--text-3)'}" onclick="activeSettingsCategory='${c.id}';renderSettingsNav();renderSettingsContent()">${c.label}</div>`
  ).join('') + `<div class="mt-4 border-t pt-3 px-3" style="border-color:var(--border)"><div class="py-2 rounded cursor-pointer text-xs" style="color:var(--text-4)" onclick="openSettings()">Display...</div></div>`;
}

function renderSettingsContent() {
  const el = document.getElementById('settings-content');
  if (!el || !configData) return;
  const q = (document.getElementById('settings-search')?.value || '').toLowerCase();
  if (q) {
    el.innerHTML = renderProjectSection(q) + renderRunnersSection(q) + renderRolesSection(q) + renderGeneralSection(q);
  } else {
    switch(activeSettingsCategory) {
      case 'project': el.innerHTML = renderProjectSection(''); break;
      case 'runners': el.innerHTML = renderRunnersSection(''); break;
      case 'roles': el.innerHTML = renderRolesSection(''); break;
      case 'general': el.innerHTML = renderGeneralSection(''); break;
    }
  }
}
```

- [ ] **Step 6: 更新快捷鍵**

找到 `keydown` handler 裡的：

```js
  else if ((e.metaKey||e.ctrlKey) && e.key===',') { e.preventDefault(); openSettings(); }
```

改成：

```js
  else if ((e.metaKey||e.ctrlKey) && e.key===',') { e.preventDefault(); openProjectSettings(); }
```

找到 Escape 處理：

```js
  else if (e.key==='Escape') {
    if (document.getElementById('picker-modal').classList.contains('open')) closeProjectPicker();
    else if (document.getElementById('search-modal').classList.contains('open')) closeSearch();
    else if (document.getElementById('settings-modal').classList.contains('open')) closeSettings();
  }
```

改成：

```js
  else if (e.key==='Escape') {
    if (settingsOpen) closeProjectSettings();
    else if (document.getElementById('picker-modal').classList.contains('open')) closeProjectPicker();
    else if (document.getElementById('search-modal').classList.contains('open')) closeSearch();
    else if (document.getElementById('settings-modal').classList.contains('open')) closeSettings();
  }
```

- [ ] **Step 7: 驗證編譯 + commit**

Run: `go build ./cmd/4x`
Expected: 無錯誤（embedded HTML 更新）

```bash
git add internal/server/static/index.html
git commit -m "feat(F027): settings page shell with nav, shortcuts, open/close"
```

---

### Task 3: 前端設定頁 — 表單元件與分區渲染

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 加表單元件函式**

在 `renderSettingsContent` 後面加：

```js
function sField(label, key, html) {
  return `<div class="settings-field mb-5" data-key="${key}"><div class="text-xs font-semibold mb-1" style="color:var(--text-2)">${label}</div><div class="text-[10px] mb-2" style="color:var(--text-4)">${key}</div>${html}</div>`;
}
function sInput(path, value, ph) {
  return `<input type="text" value="${escAttr(value||'')}" placeholder="${ph||''}" class="w-full text-sm px-3 py-2 rounded-lg outline-none" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)" oninput="setNested(configData,'${path}',this.value);scheduleProjectSettingsSave()">`;
}
function sTextarea(path, value, rows) {
  return `<textarea rows="${rows||3}" class="w-full text-sm px-3 py-2 rounded-lg outline-none resize-y" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)" oninput="setNested(configData,'${path}',this.value);scheduleProjectSettingsSave()">${esc(value||'')}</textarea>`;
}
function sSelect(path, value, opts) {
  return `<select class="text-sm px-3 py-2 rounded-lg outline-none" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)" onchange="setNested(configData,'${path}',this.value);scheduleProjectSettingsSave()">${opts.map(o=>`<option value="${escAttr(o.v)}"${o.v===(value||'')?'selected':''}>${esc(o.l)}</option>`).join('')}</select>`;
}
function sCheckbox(path, checked, label) {
  return `<label class="flex items-center gap-2 text-sm cursor-pointer" style="color:var(--text-2)"><input type="checkbox" ${checked?'checked':''} onchange="setNested(configData,'${path}',this.checked);scheduleProjectSettingsSave()"> ${label}</label>`;
}
function sNumber(path, value) {
  return `<input type="number" value="${value||0}" min="0" class="text-sm px-3 py-2 rounded-lg outline-none w-24" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)" oninput="setNested(configData,'${path}',parseInt(this.value)||0);scheduleProjectSettingsSave()">`;
}
function sTagList(path) {
  const items = getNested(configData, path) || [];
  return `<div class="flex flex-wrap gap-1.5">${items.map((item,i)=>`<span class="inline-flex items-center gap-1 px-2 py-1 text-xs rounded" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-2)">${esc(item)}<span class="cursor-pointer hover:text-red-400 ml-0.5" onclick="sTagRm('${path}',${i})">×</span></span>`).join('')}<input type="text" placeholder="Add..." class="text-xs px-2 py-1 rounded outline-none" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1);width:100px" onkeydown="if(event.key==='Enter'){event.preventDefault();sTagAdd('${path}',this.value);this.value=''}"></div>`;
}
function sTagAdd(path, v) { if(!v.trim())return; const items=getNested(configData,path)||[]; items.push(v.trim()); setNested(configData,path,items); renderSettingsContent(); scheduleProjectSettingsSave(); }
function sTagRm(path, i) { const items=getNested(configData,path)||[]; items.splice(i,1); setNested(configData,path,items); renderSettingsContent(); scheduleProjectSettingsSave(); }
function sItemList(path) {
  const items = getNested(configData, path) || [];
  return `<div class="space-y-2">${items.map((item,i)=>`<div class="flex gap-2 items-start"><textarea rows="2" class="flex-1 text-xs px-3 py-2 rounded-lg outline-none resize-y" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1)" oninput="sItemUpd('${path}',${i},this.value)">${esc(item)}</textarea><button class="text-xs px-2 py-1 rounded hover:text-red-400 flex-shrink-0" style="color:var(--text-4)" onclick="sItemRm('${path}',${i})">×</button></div>`).join('')}<button class="text-xs px-3 py-1.5 rounded" style="border:1px dashed var(--border);color:var(--text-3)" onclick="sItemAdd('${path}')">+ Add</button></div>`;
}
function sItemAdd(path) { const items=getNested(configData,path)||[]; items.push(''); setNested(configData,path,items); renderSettingsContent(); }
function sItemRm(path, i) { const items=getNested(configData,path)||[]; items.splice(i,1); setNested(configData,path,items); renderSettingsContent(); scheduleProjectSettingsSave(); }
function sItemUpd(path, i, v) { const items=getNested(configData,path)||[]; items[i]=v; scheduleProjectSettingsSave(); }
```

- [ ] **Step 2: 加 Project 和 General 分區渲染**

```js
function renderProjectSection(q) {
  const p = configData.project || {};
  const fields = [
    {l:'Name',k:'project.name',h:sInput('project.name',p.name,'Project name')},
    {l:'Description',k:'project.description',h:sTextarea('project.description',p.description,2)},
    {l:'Language',k:'project.language',h:sInput('project.language',p.language,'e.g. go')},
    {l:'Setup',k:'project.setup',h:sTagList('project.setup')},
    {l:'Build',k:'project.build',h:sTagList('project.build')},
    {l:'Test',k:'project.test',h:sTagList('project.test')},
    {l:'Lint',k:'project.lint',h:sTagList('project.lint')},
    {l:'Docs',k:'project.docs',h:sTagList('project.docs')},
    {l:'Rules',k:'project.rules',h:sItemList('project.rules')},
    {l:'Includes',k:'project.includes',h:sTagList('project.includes')},
  ];
  let html = '<h2 class="text-base font-bold mb-4" style="color:var(--text-1)">Project</h2>';
  for (const f of fields) { if (q && !f.l.toLowerCase().includes(q) && !f.k.includes(q)) continue; html += sField(f.l, f.k, f.h); }
  return html;
}

function renderGeneralSection(q) {
  const rKeys = Object.keys(configData.runners || {});
  const rOpts = [{v:'',l:'(none)'},...rKeys.map(k=>({v:k,l:k}))];
  const iOpts = [{v:'',l:'(none)'},{v:'worktree',l:'worktree'},{v:'branch',l:'branch'}];
  const fields = [
    {l:'Default Runner',k:'default_runner',h:sSelect('default_runner',configData.default_runner,rOpts)},
    {l:'Isolation',k:'isolation',h:sSelect('isolation',configData.isolation,iOpts)},
    {l:'Max Concurrent Runs',k:'max_concurrent_runs',h:sNumber('max_concurrent_runs',configData.max_concurrent_runs)},
    {l:'Commit',k:'commit',h:sInput('commit',configData.commit,'e.g. squash')},
    {l:'Rules',k:'rules',h:sItemList('rules')},
    {l:'Hub Repos',k:'hub_repos',h:sTagList('hub_repos')},
  ];
  let html = '<h2 class="text-base font-bold mb-4" style="color:var(--text-1)">General</h2>';
  for (const f of fields) { if (q && !f.l.toLowerCase().includes(q) && !f.k.includes(q)) continue; html += sField(f.l, f.k, f.h); }
  return html;
}
```

- [ ] **Step 3: 加 Runners 分區渲染**

```js
function renderRunnersSection(q) {
  const runners = configData.runners || {};
  let html = '<h2 class="text-base font-bold mb-4" style="color:var(--text-1)">Runners</h2>';
  for (const [key, runner] of Object.entries(runners)) {
    if (q && !key.toLowerCase().includes(q) && !'runner'.includes(q) && !'command'.includes(q)) continue;
    html += `<div class="mb-4 rounded-lg overflow-hidden" style="border:1px solid var(--border)">
      <div class="flex items-center px-4 py-2.5 cursor-pointer" style="background:var(--bg-hover)" onclick="this.nextElementSibling.classList.toggle('hidden');this.querySelector('.sc').classList.toggle('rotate-90')">
        <span class="sc text-xs transition-transform rotate-90" style="color:var(--text-4)">▶</span>
        <span class="ml-2 text-sm font-semibold" style="color:var(--text-1)">${esc(key)}</span>
        <button class="ml-auto text-xs px-2 py-1 rounded hover:text-red-400 transition-colors" style="color:var(--text-4)" onclick="event.stopPropagation();sRunnerRm('${escAttr(key)}')">Delete</button>
      </div>
      <div class="p-4 space-y-3">
        ${sField('Command','runners.'+key+'.command',sInput('runners.'+key+'.command',runner.command,'e.g. claude'))}
        ${sField('Args','runners.'+key+'.args',sTagList('runners.'+key+'.args'))}
        ${sField('Model','runners.'+key+'.model',sInput('runners.'+key+'.model',runner.model,'e.g. opus'))}
        <div class="flex gap-6">${sCheckbox('runners.'+key+'.stdin',runner.stdin,'stdin')} ${sCheckbox('runners.'+key+'.tty',runner.tty,'tty')}</div>
      </div></div>`;
  }
  html += `<button class="text-xs px-3 py-1.5 rounded transition-colors" style="border:1px dashed var(--border);color:var(--text-3)" onclick="sRunnerAdd()">+ Add Runner</button>`;
  return html;
}
function sRunnerAdd() { const n=prompt('Runner name:'); if(!n||!n.trim())return; if(!configData.runners)configData.runners={}; if(configData.runners[n.trim()]){alert('Already exists');return;} configData.runners[n.trim()]={command:'',args:[]}; renderSettingsContent(); scheduleProjectSettingsSave(); }
function sRunnerRm(key) { if(!confirm('Remove runner "'+key+'"?'))return; delete configData.runners[key]; renderSettingsContent(); scheduleProjectSettingsSave(); }
```

- [ ] **Step 4: 加 Roles 分區渲染**

```js
function renderRolesSection(q) {
  const roles = configData.roles || {};
  let html = '<h2 class="text-base font-bold mb-4" style="color:var(--text-1)">Roles</h2>';
  for (const [key, role] of Object.entries(roles)) {
    if (q && !key.toLowerCase().includes(q) && !'role'.includes(q) && !'model'.includes(q)) continue;
    html += `<div class="mb-4 rounded-lg overflow-hidden" style="border:1px solid var(--border)">
      <div class="flex items-center px-4 py-2.5 cursor-pointer" style="background:var(--bg-hover)" onclick="this.nextElementSibling.classList.toggle('hidden');this.querySelector('.sc').classList.toggle('rotate-90')">
        <span class="sc text-xs transition-transform rotate-90" style="color:var(--text-4)">▶</span>
        <span class="ml-2 text-sm font-semibold" style="color:var(--text-1)">${esc(key)}</span>
        <button class="ml-auto text-xs px-2 py-1 rounded hover:text-red-400 transition-colors" style="color:var(--text-4)" onclick="event.stopPropagation();sRoleRm('${escAttr(key)}')">Delete</button>
      </div>
      <div class="p-4 space-y-3">
        ${sField('Model','roles.'+key+'.model',sInput('roles.'+key+'.model',role.model,'e.g. opus'))}
        ${sField('Deep Model','roles.'+key+'.deep_model',sInput('roles.'+key+'.deep_model',role.deep_model,'e.g. opus'))}
        ${sField('Instructions','roles.'+key+'.instructions',sItemList('roles.'+key+'.instructions'))}
        ${sField('Includes','roles.'+key+'.includes',sTagList('roles.'+key+'.includes'))}
      </div></div>`;
  }
  html += `<button class="text-xs px-3 py-1.5 rounded transition-colors" style="border:1px dashed var(--border);color:var(--text-3)" onclick="sRoleAdd()">+ Add Role</button>`;
  return html;
}
function sRoleAdd() { const n=prompt('Role name:'); if(!n||!n.trim())return; if(!configData.roles)configData.roles={}; if(configData.roles[n.trim()]){alert('Already exists');return;} configData.roles[n.trim()]={model:'',instructions:[]}; renderSettingsContent(); scheduleProjectSettingsSave(); }
function sRoleRm(key) { if(!confirm('Remove role "'+key+'"?'))return; delete configData.roles[key]; renderSettingsContent(); scheduleProjectSettingsSave(); }
```

- [ ] **Step 5: 驗證編譯 + commit**

Run: `go build ./cmd/4x`
Expected: 無錯誤

```bash
git add internal/server/static/index.html
git commit -m "feat(F027): settings form sections — project, runners, roles, general"
```

---

### Task 4: 前端設定頁 — Auto-save、JSON 模式、搜尋

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 加 auto-save 函式**

在 `sRoleRm` 後面加：

```js
function scheduleProjectSettingsSave() {
  if (settingsSaveTimer) clearTimeout(settingsSaveTimer);
  settingsSaveTimer = setTimeout(saveProjectSettings, 300);
}
async function saveProjectSettings() {
  if (!configData) return;
  const indicator = document.getElementById('settings-saved');
  const errorEl = document.getElementById('settings-error');
  try {
    const resp = await fetch(apiBase()+'/api/settings', { method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(configData) });
    if (!resp.ok) {
      const data = await resp.json().catch(()=>({error:'Save failed'}));
      if (errorEl) { errorEl.textContent = data.error; errorEl.style.display = ''; }
      return;
    }
    if (errorEl) errorEl.style.display = 'none';
    if (indicator) { indicator.style.display = ''; indicator.style.opacity = '1'; setTimeout(() => { indicator.style.opacity = '0'; setTimeout(() => indicator.style.display = 'none', 300); }, 1500); }
  } catch { if (errorEl) { errorEl.textContent = 'Connection error'; errorEl.style.display = ''; } }
}
```

- [ ] **Step 2: 加 JSON 模式**

```js
function toggleJsonMode() {
  const on = document.getElementById('json-mode-toggle').checked;
  const nav = document.getElementById('settings-nav');
  const content = document.getElementById('settings-content');
  const search = document.getElementById('settings-search');
  if (on) {
    nav.classList.add('hidden');
    search.disabled = true; search.style.opacity = '.4';
    content.innerHTML = `<div class="flex items-center gap-3 mb-3"><h2 class="text-base font-bold" style="color:var(--text-1)">JSON</h2><div id="json-error" class="text-xs" style="color:#f87171;display:none"></div></div><textarea id="json-editor" class="w-full text-xs font-mono px-4 py-3 rounded-lg outline-none resize-y" style="background:var(--bg-input);border:1px solid var(--border);color:var(--text-1);min-height:70vh" onblur="saveJsonEditor()">${esc(JSON.stringify(configData,null,2))}</textarea>`;
  } else {
    nav.classList.remove('hidden');
    search.disabled = false; search.style.opacity = '1';
    fetch(apiBase()+'/api/settings').then(r=>r.json()).then(data=>{ configData=data; renderSettingsContent(); }).catch(()=>renderSettingsContent());
  }
}
function saveJsonEditor() {
  const ta = document.getElementById('json-editor');
  const err = document.getElementById('json-error');
  try {
    configData = JSON.parse(ta.value);
    ta.style.borderColor = 'var(--border)';
    if (err) err.style.display = 'none';
    scheduleProjectSettingsSave();
  } catch(e) {
    ta.style.borderColor = '#f87171';
    if (err) { err.textContent = e.message; err.style.display = ''; }
  }
}
```

- [ ] **Step 3: 驗證編譯 + commit**

Run: `go build ./cmd/4x`
Expected: 無錯誤

```bash
git add internal/server/static/index.html
git commit -m "feat(F027): auto-save, JSON mode, search filtering"
```

---

### Task 5: 更新文件與全量驗證

**Files:**
- Modify: `docs/guide/dashboard.md`

- [ ] **Step 1: 更新 dashboard 文件**

在 `docs/guide/dashboard.md` 加入 Settings 段落，說明：

- `GET /api/settings` — 讀取 `.4x/settings.json`
- `PUT /api/settings` — 寫入（自動備份 `.bak`、驗證 `project.name`）
- 前端入口：齒輪 icon 或 `Cmd+,`
- JSON 模式切換

- [ ] **Step 2: 全部 Go 測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 3: CLI help 驗證**

Run: `go run ./cmd/4x --help`
Expected: 沒有新增 subcommand（F027 只改 server 端）

- [ ] **Step 4: 手動驗證**

1. `go run ./cmd/4x live` 啟動 dashboard
2. 開瀏覽器 `http://localhost:4580`
3. 按 `Cmd+,` 或點齒輪 icon 開啟設定頁
4. 驗證 Project、Runners、Roles、General 各分區顯示正確
5. 修改 `project.description` → 確認 "Saved" 指示出現
6. 確認 `.4x/settings.json` 和 `.4x/settings.json.bak` 更新
7. 切換 JSON 模式 → 修改 → blur → 確認儲存
8. 搜尋 "model" → 確認跨分類過濾
9. 新增/刪除 runner → 確認正常
10. 按 Escape 關閉設定頁 → 回到 dashboard

- [ ] **Step 5: Commit docs**

```bash
git add docs/guide/dashboard.md
git commit -m "docs(F027): add settings editor section to dashboard guide"
```
