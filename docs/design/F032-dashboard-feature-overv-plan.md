# F032: Dashboard Feature Overview Tab — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 dashboard feature detail 新增 Overview tab（第一個 tab、預設顯示），展示 feature 完整資訊含 YAML 全欄位 + spec/plan 文件。

**Architecture:** 後端新增 `GET /api/overview/{featureId}` 回傳 feature YAML 全部欄位 + spec/plan markdown 內容。前端加 Overview tab 放在第一位，切換 feature 預設顯示 Overview。Spec/plan 來源：YAML `spec`/`plan` 欄位優先，fallback 到 `docs/design/{featureId}-spec.md`。

**Tech Stack:** Go (net/http, encoding/json, gopkg.in/yaml.v3), vanilla JS + Tailwind CSS + marked.js

---

## File Map

| Action | File | Responsibility |
|---|---|---|
| Modify | `internal/protocol/types.go:44-54` | Feature struct 加 Spec/Plan 欄位 |
| Modify | `internal/server/server.go` | 新增 overviewInfo struct + handleOverview + 路由 |
| Modify | `internal/server/server_test.go` | 測試 overview endpoint |
| Modify | `internal/server/multi.go` | 向後相容 /api/overview/ 路由 |
| Modify | `internal/server/multi_test.go` | 測試 multi overview prefix routing |
| Modify | `internal/server/static/index.html` | Overview tab UI + 渲染邏輯 |
| Modify | `docs/guide/dashboard.md` | 更新 API 文件 |

---

### Task 1: Feature struct 加 Spec/Plan 欄位

**Files:**
- Modify: `internal/protocol/types.go:44-54`

- [ ] **Step 1: 加 Spec/Plan 欄位到 Feature struct**

```go
// Feature 是 features/*.yaml 的結構
type Feature struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Status      string            `yaml:"status" json:"status"`
	Priority    int               `yaml:"priority,omitempty" json:"priority,omitempty"`
	Repos       map[string]string `yaml:"repos,omitempty" json:"repos,omitempty"`
	Subtasks    []Subtask         `yaml:"subtasks,omitempty" json:"subtasks,omitempty"`
	Rules       []string          `yaml:"rules,omitempty" json:"rules,omitempty"`
	Depends     []string          `yaml:"depends,omitempty" json:"depends,omitempty"`
	Spec        string            `yaml:"spec,omitempty" json:"-"`
	Plan        string            `yaml:"plan,omitempty" json:"-"`
}
```

注意：`json:"-"` 因為 Spec/Plan 在 Feature struct 裡是檔案路徑，不直接序列化到 API（overview endpoint 用專屬 struct）。

- [ ] **Step 2: 驗證編譯通過**

Run: `go build ./...`
Expected: 成功，無錯誤

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F032): add Spec/Plan path fields to Feature struct"
```

---

### Task 2: 後端 handleOverview endpoint

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: 新增 overviewInfo struct**

在 `server.go` 的 `taskInfo` struct 下方（約 line 131 後）新增：

```go
type overviewInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Priority    int               `json:"priority,omitempty"`
	Repos       map[string]string `json:"repos,omitempty"`
	Subtasks    []protocol.Subtask `json:"subtasks,omitempty"`
	Rules       []string          `json:"rules,omitempty"`
	Depends     []string          `json:"depends,omitempty"`
	Spec        string            `json:"spec"`
	Plan        string            `json:"plan"`
	SpecSource  string            `json:"specSource"`
	PlanSource  string            `json:"planSource"`
}
```

- [ ] **Step 2: 新增 resolveDoc helper**

在 `readIfExists` 下方新增：

```go
// resolveDoc 依優先序讀取設計文件：YAML 指定路徑 > docs/design/ 慣例路徑。
// 回傳 (內容, 來源路徑)。
func resolveDoc(root, yamlPath, featureID, suffix string) (string, string) {
	if yamlPath != "" {
		abs := yamlPath
		if !filepath.IsAbs(yamlPath) {
			abs = filepath.Join(root, yamlPath)
		}
		content := readIfExists(abs)
		if content != "" {
			return content, yamlPath
		}
	}
	conventionPath := filepath.Join("docs", "design", featureID+"-"+suffix+".md")
	content := readIfExists(filepath.Join(root, conventionPath))
	if content != "" {
		return content, conventionPath
	}
	return "", ""
}
```

- [ ] **Step 3: 新增 handleOverview 函式**

```go
func handleOverview(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	spec, specSrc := resolveDoc(ws.Root, f.Spec, f.ID, "spec")
	plan, planSrc := resolveDoc(ws.Root, f.Plan, f.ID, "plan")

	info := overviewInfo{
		ID:          f.ID,
		Name:        f.Name,
		Description: f.Description,
		Status:      f.Status,
		Priority:    f.Priority,
		Repos:       f.Repos,
		Subtasks:    f.Subtasks,
		Rules:       f.Rules,
		Depends:     f.Depends,
		Spec:        spec,
		Plan:        plan,
		SpecSource:  specSrc,
		PlanSource:  planSrc,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}
```

- [ ] **Step 4: 在 NewMux 註冊路由**

在 `mux.HandleFunc("/api/events/", ...)` 上方加：

```go
mux.HandleFunc("/api/overview/", func(w http.ResponseWriter, r *http.Request) {
	featureID := strings.TrimPrefix(r.URL.Path, "/api/overview/")
	handleOverview(ws, featureID, w)
})
```

- [ ] **Step 5: 驗證編譯通過**

Run: `go build ./...`
Expected: 成功

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(F032): add GET /api/overview/{id} endpoint"
```

---

### Task 3: 後端 overview endpoint 測試

**Files:**
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫 TestGetOverview 測試**

在 `server_test.go` 最後方新增：

```go
func TestGetOverview(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 建立 docs/design/ 下的 spec/plan
	designDir := filepath.Join(ws.Root, "docs", "design")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "test-feat-spec.md"), []byte("# Spec\ntest spec content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "test-feat-plan.md"), []byte("# Plan\ntest plan content"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.ID != "test-feat" {
		t.Errorf("ID = %s, want test-feat", info.ID)
	}
	if info.Name != "Test Feature" {
		t.Errorf("Name = %s, want Test Feature", info.Name)
	}
	if info.Spec != "# Spec\ntest spec content" {
		t.Errorf("Spec = %q, want spec content", info.Spec)
	}
	if info.SpecSource != "docs/design/test-feat-spec.md" {
		t.Errorf("SpecSource = %q, want docs/design/test-feat-spec.md", info.SpecSource)
	}
	if info.Plan != "# Plan\ntest plan content" {
		t.Errorf("Plan = %q, want plan content", info.Plan)
	}
	if info.PlanSource != "docs/design/test-feat-plan.md" {
		t.Errorf("PlanSource = %q, want docs/design/test-feat-plan.md", info.PlanSource)
	}
}
```

- [ ] **Step 2: 寫 TestGetOverview_NotFound 測試**

```go
func TestGetOverview_NotFound(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/nonexistent", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 3: 寫 TestGetOverview_NoDocs 測試**

```go
func TestGetOverview_NoDocs(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Spec != "" {
		t.Errorf("Spec should be empty, got %q", info.Spec)
	}
	if info.SpecSource != "" {
		t.Errorf("SpecSource should be empty, got %q", info.SpecSource)
	}
}
```

- [ ] **Step 4: 寫 TestGetOverview_YAMLPathOverride 測試**

```go
func TestGetOverview_YAMLPathOverride(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 建立自訂路徑的 spec
	specDir := filepath.Join(ws.Root, "custom")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "my-spec.md"), []byte("custom spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 更新 feature YAML 加上 spec 欄位
	f, _ := ws.LoadFeature("test-feat")
	f.Spec = "custom/my-spec.md"
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Spec != "custom spec" {
		t.Errorf("Spec = %q, want 'custom spec'", info.Spec)
	}
	if info.SpecSource != "custom/my-spec.md" {
		t.Errorf("SpecSource = %q, want custom/my-spec.md", info.SpecSource)
	}
}
```

- [ ] **Step 5: 跑測試確認全部通過**

Run: `go test ./internal/server/ -run TestGetOverview -v`
Expected: 4 個測試全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/server_test.go
git commit -m "test(F032): add overview endpoint tests"
```

---

### Task 4: multi.go 向後相容 + prefix routing

**Files:**
- Modify: `internal/server/multi.go`

- [ ] **Step 1: 加向後相容路由**

在 `multi.go` 的 `mux.HandleFunc("/api/events/", ...)` 上方（約 line 239）新增：

```go
mux.HandleFunc("/api/overview/", func(w http.ResponseWriter, r *http.Request) {
	entries := reg.List()
	if len(entries) == 1 {
		ws := reg.Get(entries[0].ID)
		featureID := strings.TrimPrefix(r.URL.Path, "/api/overview/")
		handleOverview(ws, featureID, w)
		return
	}
	compatError(w, len(entries), "/api/project/{id}/api/overview/{featureId}")
})
```

prefix routing 已自動由 `/api/project/{id}/...` handler 處理（line 305-321 的 `entry.mux.ServeHTTP(w, r)` 會轉發到 NewMux 裡已註冊的 `/api/overview/`）。

- [ ] **Step 2: 驗證編譯通過**

Run: `go build ./...`
Expected: 成功

- [ ] **Step 3: Commit**

```bash
git add internal/server/multi.go
git commit -m "feat(F032): add overview backward-compat route to multi mux"
```

---

### Task 5: multi.go overview 測試

**Files:**
- Modify: `internal/server/multi_test.go`

- [ ] **Step 1: 寫 TestMultiMux_OverviewPrefixRoute 測試**

在 `multi_test.go` 最後方新增：

```go
func TestMultiMux_OverviewPrefixRoute(t *testing.T) {
	ws := setupMultiWorkspace(t, "overview-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/project/"+id+"/api/overview/feat-1", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.ID != "feat-1" {
		t.Errorf("ID = %s, want feat-1", info.ID)
	}
}

func TestMultiMux_OverviewBackwardCompat(t *testing.T) {
	ws := setupMultiWorkspace(t, "single-ov")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/overview/feat-1", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: 跑測試確認通過**

Run: `go test ./internal/server/ -run TestMultiMux_Overview -v`
Expected: 2 個測試 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/multi_test.go
git commit -m "test(F032): add multi mux overview routing tests"
```

---

### Task 6: 前端 Overview tab

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 修改 HTML header 的 tab 順序**

將 `index.html` line 142-145 的 detail-tabs 區塊改為（Overview 放第一個）：

```html
<div id="detail-tabs" class="flex gap-4 mt-3 border-t pt-2" style="border-color:var(--border)">
  <button class="detail-tab text-xs font-semibold pb-1 border-b-2 border-emerald-500 text-emerald-400" data-tab="overview" onclick="switchDetailTab('overview')">Overview</button>
  <button class="detail-tab text-xs font-semibold pb-1 border-b-2 border-transparent text-zinc-500 hover:text-zinc-300" data-tab="messages" onclick="switchDetailTab('messages')">Messages</button>
  <button class="detail-tab text-xs font-semibold pb-1 border-b-2 border-transparent text-zinc-500 hover:text-zinc-300" data-tab="logs" onclick="switchDetailTab('logs')">Logs</button>
</div>
```

- [ ] **Step 2: 加 overview panel HTML**

在 `<div id="messages" ...>` 上方加：

```html
<div id="overview-panel" class="hidden"></div>
```

- [ ] **Step 3: 新增 overview 快取變數和渲染函式**

在 JS 區塊的 `let activeDetailTab = 'messages';` 上方新增：

```javascript
const overviewCache = {};

async function loadOverview(fid) {
  const el = document.getElementById('overview-panel');
  if (activeDetailTab === 'overview') el.classList.remove('hidden');

  if (overviewCache[fid]) { renderOverview(overviewCache[fid], el); return; }

  el.innerHTML = '<div class="text-zinc-600 text-sm mt-8 text-center">Loading...</div>';
  try {
    const resp = await fetch(apiBase() + '/api/overview/' + fid);
    if (!resp.ok) { el.innerHTML = '<div class="text-red-400 text-sm mt-8 text-center">Failed to load overview</div>'; return; }
    const data = await resp.json();
    overviewCache[fid] = data;
    renderOverview(data, el);
  } catch { el.innerHTML = '<div class="text-red-400 text-sm mt-8 text-center">Connection error</div>'; }
}

function renderOverview(d, el) {
  let html = '';

  // Description
  if (d.description) {
    html += `<div class="mb-6"><div class="text-xs font-bold text-zinc-500 uppercase tracking-wider mb-2">Description</div>`;
    html += `<div class="md-body text-sm" style="color:var(--text-2)">${typeof marked!=='undefined'?marked.parse(d.description):'<pre>'+esc(d.description)+'</pre>'}</div></div>`;
  }

  // Feature Details
  const details = [];
  if (d.priority) details.push(`<div class="flex gap-2"><span class="text-zinc-500 w-20 flex-shrink-0">Priority</span><span>${d.priority}</span></div>`);
  if (d.repos && Object.keys(d.repos).length) {
    const repoList = Object.entries(d.repos).map(([k,v]) => `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:var(--bg-hover)">${esc(k)} → ${esc(v)}</span>`).join(' ');
    details.push(`<div class="flex gap-2"><span class="text-zinc-500 w-20 flex-shrink-0">Repos</span><div class="flex flex-wrap gap-1">${repoList}</div></div>`);
  }
  if (d.depends && d.depends.length) {
    const depList = d.depends.map(dep => `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:var(--bg-hover)">${esc(dep)}</span>`).join(' ');
    details.push(`<div class="flex gap-2"><span class="text-zinc-500 w-20 flex-shrink-0">Depends</span><div class="flex flex-wrap gap-1">${depList}</div></div>`);
  }
  if (d.rules && d.rules.length) {
    const ruleList = d.rules.map(r => `<li class="text-sm" style="color:var(--text-2)">${esc(r)}</li>`).join('');
    details.push(`<div class="flex gap-2"><span class="text-zinc-500 w-20 flex-shrink-0">Rules</span><ul class="list-disc list-inside">${ruleList}</ul></div>`);
  }
  if (details.length) {
    html += `<div class="mb-6"><div class="text-xs font-bold text-zinc-500 uppercase tracking-wider mb-2">Feature Details</div><div class="space-y-2">${details.join('')}</div></div>`;
  }

  // Subtasks
  if (d.subtasks && d.subtasks.length) {
    const stHtml = d.subtasks.map(st => {
      const icon = st.status === 'done' ? '<span class="text-emerald-400">✓</span>' : '<span class="text-zinc-600">○</span>';
      const desc = st.description ? `<div class="text-[11px] ml-5" style="color:var(--text-3)">${esc(st.description)}</div>` : '';
      return `<div>${icon} <span class="text-sm" style="color:${st.status==='done'?'var(--text-3)':'var(--text-2)'}">${esc(st.name)}</span>${desc}</div>`;
    }).join('');
    html += `<div class="mb-6"><div class="text-xs font-bold text-zinc-500 uppercase tracking-wider mb-2">Subtasks</div><div class="space-y-1">${stHtml}</div></div>`;
  }

  // Spec
  if (d.spec) {
    html += renderDocSection('Spec', d.spec, d.specSource);
  }

  // Plan
  if (d.plan) {
    html += renderDocSection('Plan', d.plan, d.planSource);
  }

  if (!html) html = '<div class="text-zinc-600 text-sm mt-8 text-center">No overview data</div>';
  el.innerHTML = html;
}

function renderDocSection(title, content, source) {
  const id = 'doc-' + title.toLowerCase();
  const srcLine = source ? `<span class="text-[11px] ml-2" style="color:var(--text-4)">${esc(source)}</span>` : '';
  return `<div class="mb-6"><div class="flex items-center gap-2 cursor-pointer" onclick="toggleDocSection('${id}')"><span class="text-xs font-bold text-zinc-500 uppercase tracking-wider">${esc(title)}</span>${srcLine}<span id="${id}-chevron" class="text-zinc-600 text-xs ml-auto">▶</span></div><div id="${id}-body" class="mt-2 md-body text-sm hidden" style="color:var(--text-2);max-height:70vh;overflow-y:auto"></div></div>`;
}

function toggleDocSection(id) {
  const body = document.getElementById(id + '-body');
  const chevron = document.getElementById(id + '-chevron');
  const opening = body.classList.contains('hidden');
  body.classList.toggle('hidden');
  chevron.classList.toggle('open');
  chevron.textContent = opening ? '▼' : '▶';
  if (opening && !body.dataset.rendered) {
    const key = id.replace('doc-', '');
    const data = overviewCache[current];
    if (data) {
      const content = data[key] || '';
      body.innerHTML = typeof marked !== 'undefined' ? marked.parse(content) : '<pre>' + esc(content) + '</pre>';
      body.dataset.rendered = '1';
    }
  }
}
```

- [ ] **Step 4: 修改 loadDetail 函式預設為 overview**

修改 `loadDetail` 函式（約 line 1061）。把 `activeDetailTab = 'messages'` 改為 `activeDetailTab = 'overview'`，並加上 overview panel 的顯示邏輯：

```javascript
async function loadDetail(t) {
  document.getElementById('dashboard').classList.add('hidden');
  document.getElementById('header').classList.remove('hidden');
  document.getElementById('messages').innerHTML = ''; lastMsgCount = 0;
  document.getElementById('h-id').textContent = t.id;
  document.getElementById('h-name').textContent = t.name;
  document.getElementById('h-badge').innerHTML = badge(t.status, t.phase, t.active);
  const meta = [];
  if (t.phase) meta.push(`<span>${PHASE_ICON[t.phase]||'○'} ${t.phase}</span>`);
  if (t.round) meta.push(`<span>⟳ Round ${t.round}</span>`);
  if (t.runners && t.runners.length) {
    meta.push(`<span>⬡ ${t.runners.map(r => `<span style="color:${runnerColor(r)}">${esc(r)}</span>`).join(' · ')}</span>`);
  } else if (t.runner) {
    meta.push(`<span>⬡ ${t.runner}</span>`);
  }
  document.getElementById('h-meta').innerHTML = meta.join('<span class="text-zinc-700">·</span>');
  disconnectLogSSE();
  activeDetailTab = 'overview';
  document.getElementById('overview-panel').classList.remove('hidden');
  document.getElementById('overview-panel').innerHTML = '';
  document.getElementById('messages').classList.add('hidden');
  document.getElementById('logs-panel').classList.add('hidden');
  document.querySelectorAll('.detail-tab').forEach(b => {
    b.classList.toggle('border-emerald-500', b.dataset.tab === 'overview');
    b.classList.toggle('text-emerald-400', b.dataset.tab === 'overview');
    b.classList.toggle('border-transparent', b.dataset.tab !== 'overview');
    b.classList.toggle('text-zinc-500', b.dataset.tab !== 'overview');
  });
  loadOverview(t.id);
}
```

- [ ] **Step 5: 修改 switchDetailTab 函式支援 overview**

更新 `switchDetailTab`（約 line 1135）：

```javascript
function switchDetailTab(tab) {
  activeDetailTab = tab;
  document.querySelectorAll('.detail-tab').forEach(b => {
    const active = b.dataset.tab === tab;
    b.classList.toggle('border-emerald-500', active);
    b.classList.toggle('text-emerald-400', active);
    b.classList.toggle('border-transparent', !active);
    b.classList.toggle('text-zinc-500', !active);
  });
  document.getElementById('overview-panel').classList.toggle('hidden', tab !== 'overview');
  document.getElementById('messages').classList.toggle('hidden', tab !== 'messages');
  document.getElementById('logs-panel').classList.toggle('hidden', tab !== 'logs');
  if (tab === 'overview' && current) loadOverview(current);
  if (tab === 'messages' && current) loadMessages(current);
  if (tab === 'logs' && current) loadLogs(current);
}
```

- [ ] **Step 6: 驗證編譯通過**

Run: `go build ./...`
Expected: 成功（index.html 是 embed 的，會一起編譯）

- [ ] **Step 7: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F032): add Overview tab as default detail view"
```

---

### Task 7: 更新文件

**Files:**
- Modify: `docs/guide/dashboard.md`

- [ ] **Step 1: 在 REST API 表格加 overview endpoint**

在 `docs/guide/dashboard.md` 的 REST 表格中，`/api/messages/{id}` 行上方加一行：

```markdown
| `/api/overview/{id}` | GET | Get feature overview (YAML fields + spec/plan content) |
```

- [ ] **Step 2: Commit**

```bash
git add docs/guide/dashboard.md
git commit -m "docs(F032): add overview endpoint to dashboard API table"
```

---

### Task 8: 端到端驗證

- [ ] **Step 1: 跑完整測試**

Run: `go test ./... -v`
Expected: 所有測試通過

- [ ] **Step 2: 跑 lint**

Run: `go vet ./...`
Expected: 無 warning

- [ ] **Step 3: 啟動 dashboard 手動驗證**

Run: `go run ./cmd/4x live -w`

在瀏覽器中：
1. 點選任意 feature → 預設顯示 Overview tab
2. 確認 description、subtasks、rules、repos、depends 正確渲染
3. 有 spec/plan 的 feature → 確認可展開、markdown 正確渲染
4. 沒有 spec/plan 的 feature → 確認不顯示空區塊
5. 切換到 Messages / Logs tab → 功能正常
6. 切回 Overview → 用快取不重複 fetch

- [ ] **Step 4: 最終 commit（如有修正）**
