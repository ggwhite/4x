# F039: Dashboard UI Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Polish 4x Live dashboard visuals to match DCT Live quality — improved Frost theme, glass effects, role emoji, phase badges, card styling.

**Architecture:** All changes in a single file `internal/server/static/index.html`. CSS variable updates for Frost theme, JS constant updates for roles/phases, and HTML template string updates for card rendering.

**Tech Stack:** Tailwind CSS (CDN), vanilla JS, CSS custom properties

---

### Task 1: Improve Frost Theme CSS & Set as Default

**Files:**
- Modify: `internal/server/static/index.html:11-45` (CSS variables)
- Modify: `internal/server/static/index.html:46` (body styles)
- Modify: `internal/server/static/index.html:456` (DEFAULTS)

- [ ] **Step 1: Update Frost CSS variables with gradient and glass support**

Add `--bg-gradient` and `--glass` variables to Frost theme. Change `:root` (Apple Dark) to keep its current look. Add Frost-specific gradient.

```css
/* Line 11-16: Update :root to add new variables */
:root {
    --bg-body: #0f0f0f; --bg-card: rgba(24,24,27,.5); --bg-sidebar: #0f0f0f;
    --bg-hover: rgba(39,39,42,.3); --bg-input: rgba(39,39,42,.6);
    --text-1: #e5e5e5; --text-2: #a1a1aa; --text-3: #71717a; --text-4: #52525b;
    --border: rgba(39,39,42,.8); --border-light: rgba(63,63,70,.5);
    --accent: #10b981; --font-content: 15px; --font-code: 13px;
    --bg-gradient: #0f0f0f; --glass: rgba(24,24,27,.6); --glow: rgba(16,185,129,.3);
}

/* Line 28-33: Update Frost with gradient, glass, glow */
[data-theme="frost"] {
    --bg-body: #0f172a; --bg-card: rgba(30,41,59,.5); --bg-sidebar: #0f172a;
    --bg-hover: rgba(51,65,85,.3); --bg-input: rgba(51,65,85,.5);
    --text-1: #e2e8f0; --text-2: #94a3b8; --text-3: #64748b; --text-4: #475569;
    --border: rgba(51,65,85,.6); --border-light: rgba(71,85,105,.4); --accent: #38bdf8;
    --bg-gradient: linear-gradient(180deg, #0c1222 0%, #0f172a 40%, #111827 100%);
    --glass: rgba(15,23,42,.6); --glow: rgba(56,189,248,.3);
}
```

Add `--bg-gradient`, `--glass`, `--glow` to the other themes too (matching their existing `--bg-body` so they don't break):

```css
/* Add to each theme block: */
/* midnight: */ --bg-gradient: #0a0e1a; --glass: rgba(15,23,42,.6); --glow: rgba(59,130,246,.3);
/* noir: */     --bg-gradient: #000; --glass: rgba(15,15,15,.8); --glow: rgba(161,161,170,.3);
/* light: */    --bg-gradient: #f5f5f5; --glass: rgba(255,255,255,.7); --glow: rgba(5,150,105,.3);
/* paper: */    --bg-gradient: #faf8f5; --glass: rgba(255,255,255,.8); --glow: rgba(180,83,9,.3);
```

- [ ] **Step 2: Update body style to use gradient**

```css
/* Line 46: Change background to use --bg-gradient */
body { background: var(--bg-gradient); color: var(--text-1); ... }
```

- [ ] **Step 3: Add glass utility styles**

After the existing CSS (before `</style>`), add:

```css
.glass { background: var(--glass); backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px); }
.glass-card { background: rgba(30,41,59,.3); backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px); border: 1px solid var(--border-light); border-radius: 12px; }
```

- [ ] **Step 4: Change default theme to frost**

```js
/* Line 456: Change default theme */
const DEFAULTS = { theme: 'frost', fontContent: 15, fontCode: 13, refresh: 3 };
```

- [ ] **Step 5: Add glass to sidebar header and detail header**

```html
<!-- Line 129: Add glass class to sidebar header -->
<div class="p-4 border-b flex items-center gap-2 cursor-pointer transition-colors glass" ...>

<!-- Line 147: Add glass class to detail header -->
<div id="header" class="border-b p-4 hidden glass" ...>
```

- [ ] **Step 6: Add glow to pulse dot**

```css
/* Update .pulse-dot */
.pulse-dot { animation: pulse-dot 1.5s ease-in-out infinite; box-shadow: 0 0 6px var(--glow); }
```

- [ ] **Step 7: Verify and commit**

Run: `go build ./... && go vet ./...`

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): improve Frost theme with gradient, glass, glow and set as default"
```

---

### Task 2: Add Emoji and Phase Colors to ROLES Constant

**Files:**
- Modify: `internal/server/static/index.html:1123-1133` (ROLES, PHASE_ICON constants)

- [ ] **Step 1: Update ROLES with emoji and new colors**

```js
const ROLES = {
  designer:{name:'Designer',emoji:'🎨',color:'#c084fc',bg:'rgba(192,132,252,.08)'},
  coder:{name:'Coder',emoji:'💻',color:'#22d3ee',bg:'rgba(34,211,238,.08)'},
  reviewer:{name:'Reviewer',emoji:'🔍',color:'#4ade80',bg:'rgba(74,222,128,.08)'},
  'deep-reviewer':{name:'Deep Review',emoji:'🔍',color:'#4ade80',bg:'rgba(74,222,128,.08)'},
  tester:{name:'Tester',emoji:'🧪',color:'#fb923c',bg:'rgba(251,146,60,.08)'},
  acceptor:{name:'Acceptor',emoji:'⭐',color:'#facc15',bg:'rgba(250,204,21,.08)'},
};
```

- [ ] **Step 2: Add PHASE_COLORS constant for badge coloring**

After ROLES, add:

```js
const PHASE_COLORS = {
  designing: {letter:'D',color:'#c084fc',bg:'rgba(192,132,252,.15)',border:'rgba(192,132,252,.25)'},
  coding:    {letter:'C',color:'#22d3ee',bg:'rgba(34,211,238,.15)',border:'rgba(34,211,238,.25)'},
  reviewing: {letter:'R',color:'#4ade80',bg:'rgba(74,222,128,.15)',border:'rgba(74,222,128,.25)'},
  testing:   {letter:'T',color:'#fb923c',bg:'rgba(251,146,60,.15)',border:'rgba(251,146,60,.25)'},
  accepting: {letter:'A',color:'#facc15',bg:'rgba(250,204,21,.15)',border:'rgba(250,204,21,.25)'},
  amending:  {letter:'M',color:'#f87171',bg:'rgba(248,113,113,.15)',border:'rgba(248,113,113,.25)'},
};
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): add emoji to ROLES and PHASE_COLORS for badge rendering"
```

---

### Task 3: Update Sidebar Section Headers with Colored Pill Badges

**Files:**
- Modify: `internal/server/static/index.html:1294-1307` (sectionHeader function)

- [ ] **Step 1: Add SECTION_COLORS constant**

After PHASE_COLORS, add:

```js
const SECTION_COLORS = {
  running: {color:'#34d399',bg:'rgba(16,185,129,.2)'},
  review:  {color:'#fbbf24',bg:'rgba(245,158,11,.15)'},
  pending: {color:'#60a5fa',bg:'rgba(59,130,246,.15)'},
  todo:    {color:'#c084fc',bg:'rgba(168,85,247,.15)'},
  done:    {color:'#4ade80',bg:'rgba(34,197,94,.1)'},
};
```

- [ ] **Step 2: Update sectionHeader to use colored pill**

Replace the count `<span>` in sectionHeader (line ~1300):

```js
function sectionHeader(key, title, count, items, container) {
    if (count === 0) return;
    const collapsed = !!_sectionCollapsed[key];
    const h = document.createElement('div');
    h.className = 'flex items-center gap-2 px-2 py-2 text-[10px] font-bold text-zinc-500 uppercase tracking-wider select-none';
    _popupSections[key] = { title, items };
    const sc = SECTION_COLORS[key] || {color:'var(--text-4)',bg:'var(--bg-hover)'};
    h.innerHTML = `<span class="cursor-pointer hover:text-zinc-300 transition-colors flex items-center gap-2 flex-1" data-action="toggle"><span style="display:inline-block;transform:rotate(${collapsed?'0':'90'}deg);transition:transform .15s;font-size:9px">▶</span>${title}</span><span style="background:${sc.bg};color:${sc.color};padding:1px 6px;border-radius:8px;font-size:9px">${count}</span><span class="cursor-pointer hover:text-zinc-300 transition-colors text-[10px] ml-1" data-action="popup" title="${t('common.showAll')}">↗</span>`;
    h.onclick = (e) => {
      const action = e.target.closest('[data-action]')?.dataset?.action;
      if (action === 'popup') openSectionPopup(key);
      else toggleSection(key);
    };
    container.appendChild(h);
    return collapsed;
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): colored pill badges on sidebar section headers"
```

---

### Task 4: Update Sidebar Feature Cards with Phase Badge and Glass

**Files:**
- Modify: `internal/server/static/index.html:1232-1263` (renderTaskItem function)

- [ ] **Step 1: Add phaseBadge helper function**

Before `renderTaskItem`, add:

```js
function phaseBadge(phase, round) {
  const pc = PHASE_COLORS[phase];
  if (!pc || !round) return '';
  return `<span style="font-size:9px;padding:2px 7px;border-radius:6px;background:${pc.bg};color:${pc.color};font-weight:700;border:1px solid ${pc.border}">${pc.letter}${round}/5</span>`;
}
```

- [ ] **Step 2: Rewrite renderTaskItem with glass cards and badge**

```js
function renderTaskItem(task) {
  const isActive = task.active && task.phase && task.phase!=='done', isSel = task.id===current;
  const pc = PHASE_COLORS[task.phase];
  let cls = 'p-3 rounded-xl cursor-pointer mb-1 transition-all duration-150 ';
  if (isActive) {
    const borderColor = pc ? pc.border : 'rgba(16,185,129,.25)';
    cls += `border border-[${borderColor}] `;
  } else if (isSel) cls+='border border-zinc-700/50 ';
  else if (task.status==='done') cls+='border border-transparent opacity-40 hover:opacity-70 ';
  else cls+='border border-transparent hover:border-zinc-700/30 ';

  let cardStyle = '';
  if (isActive && pc) {
    cardStyle = `background:linear-gradient(135deg,${pc.bg},rgba(15,23,42,.3));backdrop-filter:blur(8px);border-color:${pc.border}`;
  } else if (isActive) {
    cardStyle = 'background:linear-gradient(135deg,rgba(16,185,129,.08),rgba(15,23,42,.3));backdrop-filter:blur(8px);border-color:rgba(16,185,129,.25)';
  } else if (isSel) {
    cardStyle = 'background:var(--glass)';
  }

  const hasState = task.phase || task.round;
  const elapsed = isActive && task.createdAt ? formatElapsed(task.createdAt) : '';
  const duration = !isActive && task.createdAt && task.updatedAt ? formatDuration(task.createdAt, task.updatedAt) : '';
  const timePart = elapsed ? ` · ⏱ ${elapsed}` : duration ? ` · ⏱ ${duration}` : '';
  const roundPart = task.round ? t('common.round').replace('{round}', task.round) : '';

  const roleInfo = ROLES[task.phase?.replace('ing','er').replace('accept','acceptor')] || {};
  const emoji = isActive && roleInfo.emoji ? roleInfo.emoji + ' ' : '';

  let pi = '';
  if (isActive) {
    const dotStyle = pc ? `background:${pc.color};box-shadow:0 0 4px ${pc.border}` : 'background:#34d399';
    pi = `<div class="flex items-center gap-1.5 mt-1.5 flex-wrap"><span class="w-1.5 h-1.5 rounded-full pulse-dot" style="${dotStyle}"></span><span class="text-[11px]" style="color:${pc?pc.color:'#34d399'}">${emoji}${task.phase}</span><span class="text-[11px] text-zinc-600">${timePart}</span></div>`;
  } else if (hasState && (roundPart || timePart)) {
    const parts = [roundPart, timePart.replace(/^ · /, '')].filter(Boolean).join(' · ');
    pi = `<div class="flex items-center gap-1.5 mt-1.5"><span class="text-[11px] text-zinc-600">${parts}</span></div>`;
  }

  const badge = phaseBadge(task.phase, task.round);
  const di = task.status==='done' ? '<span class="text-emerald-500/60 text-xs">✓</span>' : '';
  const doneBtn = task.status==='ready-for-review' ? `<button class="ml-auto px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors" onclick="event.stopPropagation();markDone('${task.id}')">${t('sidebar.markDone')}</button>` : '';
  const runId = getRunId(task.id);
  let actionBtn = '';
  if (isActive && runId) {
    actionBtn = `<button class="ml-auto w-6 h-6 flex items-center justify-center rounded text-red-400 hover:bg-red-500/20 transition-colors text-xs" onclick="event.stopPropagation();stopRun('${runId}')" title="${t('run.stop')}">■</button>`;
  } else if (task.status !== 'done') {
    actionBtn = `<button class="ml-auto w-6 h-6 flex items-center justify-center rounded hover:bg-emerald-500/20 transition-colors text-xs play-btn" style="color:var(--accent);opacity:0" onclick="event.stopPropagation();openRunModal('${task.id}')" title="${t('run.run')}">▶</button>`;
  }
  const rt = runnerTags(task.runners);
  const rtLine = rt ? `<div class="flex gap-1 mt-1">${rt}</div>` : '';
  return `<div class="${cls}" style="${cardStyle}" onclick="current='${task.id}';load();loadDetail(${JSON.stringify(task).replace(/"/g,'&quot;')})" onmouseenter="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='1')" onmouseleave="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='0')"><div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="flex items-center gap-2"><span class="text-[13px] font-medium truncate flex-1">${esc(task.name)}</span>${badge}</div><div class="text-[11px] text-zinc-600 mt-0.5">${task.id}</div>${pi}${rtLine}</div>${doneBtn || actionBtn}</div></div>`;
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): glass cards with phase badge in sidebar"
```

---

### Task 5: Update Message Cards with Role Emoji and Glass Borders

**Files:**
- Modify: `internal/server/static/index.html:1396-1422` (renderMsgCard function)

- [ ] **Step 1: Update renderMsgCard**

```js
function renderMsgCard(m) {
  const r = ROLES[m.role] || {name:m.role,emoji:'',color:'#666',bg:'rgba(100,100,100,.05)'};
  const div = document.createElement('div');
  div.className = 'fade-in rounded-xl overflow-hidden mb-3';
  div.style.cssText = `border:1px solid ${r.color}15;background:linear-gradient(135deg,${r.bg},rgba(15,23,42,.3));backdrop-filter:blur(8px)`;

  const header = document.createElement('div');
  header.className = 'flex items-center gap-2 px-4 py-2.5 cursor-pointer hover:brightness-125 transition-all sticky top-0 z-10';
  header.style.borderBottom = `1px solid ${r.color}10`;

  const lines = m.content.trim().split('\n').filter(l => l.trim());
  const preview = lines.length > 1 ? lines.find(l => !l.startsWith('#')) || lines[1] : '';
  const previewText = preview ? `<span class="text-xs text-zinc-600 truncate ml-2 flex-1">${esc(preview.slice(0,80))}</span>` : '';
  const emoji = r.emoji || '';
  header.innerHTML = `<span class="text-xs font-semibold flex-shrink-0" style="color:${r.color}">${emoji} ${r.name}</span><span class="text-xs text-zinc-600 flex-shrink-0">${m.label}${m.round?' · Round '+m.round:''}</span>${previewText}<span class="msg-chevron text-zinc-600 text-xs ml-auto flex-shrink-0">▶</span>`;

  const body = document.createElement('div');
  body.className = 'msg-body collapsed md-body px-4 py-3 text-zinc-300 overflow-y-auto';

  header.onclick = () => {
    const opening = body.classList.contains('collapsed');
    if (opening && !body.dataset.rendered) {
      body.innerHTML = typeof marked !== 'undefined' ? marked.parse(m.content) : '<pre>' + esc(m.content) + '</pre>';
      body.dataset.rendered = '1';
    }
    body.classList.toggle('collapsed');
    header.querySelector('.msg-chevron').classList.toggle('open');
    if (opening) body.style.maxHeight = '60vh';
    else body.style.maxHeight = '0';
  };
  div.appendChild(header);
  div.appendChild(body);
  return div;
}
```

- [ ] **Step 2: Update log list role rendering to include emoji**

In `loadLogs` function (~line 1568), update the log list item:

```js
return `<div class="flex items-center gap-3 px-3 py-2 rounded-lg cursor-pointer hover:bg-zinc-800/50 transition-colors ${active}" onclick="viewLog('${fid}','${escAttr(l.name)}')"><span class="text-xs font-semibold" style="color:${r.color}">${r.emoji||''} ${r.name}</span><span class="text-xs text-zinc-500">${esc(l.name)}</span><span class="ml-auto text-[10px] text-zinc-600">${kb}KB</span></div>`;
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): message cards with role emoji and glass borders"
```

---

### Task 6: Update Overview Tab with Glass Cards

**Files:**
- Modify: `internal/server/static/index.html:1482-1528` (renderOverview function)

- [ ] **Step 1: Rewrite renderOverview with glass card sections**

```js
function renderOverview(d, el) {
  let html = '';
  const card = (content) => `<div class="glass-card p-4 mb-4">${content}</div>`;
  const sectionTitle = (text) => `<div class="text-[10px] font-bold uppercase tracking-wider mb-3" style="color:var(--text-3)">${text}</div>`;

  if (d.description) {
    html += card(`${sectionTitle(t('overview.description') || 'Description')}<div class="md-body text-sm" style="color:var(--text-2)">${typeof marked !== 'undefined' ? marked.parse(d.description) : '<pre>'+esc(d.description)+'</pre>'}</div>`);
  }

  const details = [];
  if (d.priority) {
    const pColors = {high:{color:'#f87171',bg:'rgba(239,68,68,.1)',border:'rgba(239,68,68,.15)'},medium:{color:'#fbbf24',bg:'rgba(245,158,11,.1)',border:'rgba(245,158,11,.15)'},low:{color:'#4ade80',bg:'rgba(74,222,128,.1)',border:'rgba(74,222,128,.15)'}};
    const pc = pColors[d.priority] || {color:'var(--text-2)',bg:'var(--bg-hover)',border:'var(--border)'};
    details.push(`<div class="flex gap-2 items-center"><span class="text-[11px] w-16 flex-shrink-0" style="color:var(--text-3)">Priority</span><span class="text-[11px] px-2 py-0.5 rounded" style="background:${pc.bg};color:${pc.color};border:1px solid ${pc.border}">${d.priority}</span></div>`);
  }
  if (d.repos && Object.keys(d.repos).length) {
    const repos = Object.entries(d.repos).map(([k,v]) => `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:rgba(56,189,248,.08);color:#7dd3fc;border:1px solid rgba(56,189,248,.12)">${esc(k)} → ${esc(v)}</span>`).join(' ');
    details.push(`<div class="flex gap-2 items-start"><span class="text-[11px] w-16 flex-shrink-0" style="color:var(--text-3)">Repos</span><div class="flex flex-wrap gap-1">${repos}</div></div>`);
  }
  if (d.depends && d.depends.length) {
    const depends = d.depends.map(dep => `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:var(--bg-hover);border:1px solid var(--border-light)">${esc(dep)}</span>`).join(' ');
    details.push(`<div class="flex gap-2 items-start"><span class="text-[11px] w-16 flex-shrink-0" style="color:var(--text-3)">Depends</span><div class="flex flex-wrap gap-1">${depends}</div></div>`);
  }
  if (d.rules && d.rules.length) {
    const rules = d.rules.map(rule => `<div class="text-sm" style="color:var(--text-2)">• ${esc(rule)}</div>`).join('');
    details.push(`<div class="flex gap-2 items-start"><span class="text-[11px] w-16 flex-shrink-0" style="color:var(--text-3)">Rules</span><div>${rules}</div></div>`);
  }
  if (details.length) {
    html += card(`${sectionTitle(t('overview.details') || 'Feature Details')}<div class="space-y-2">${details.join('')}</div>`);
  }

  if (d.subtasks && d.subtasks.length) {
    const subtasks = d.subtasks.map(st => {
      const done = st.status === 'done';
      const icon = done ? '<span class="text-emerald-400">✓</span>' : '<span style="color:var(--text-4)">○</span>';
      const id = st.id ? `<span class="text-[11px]" style="color:${done?'var(--text-4)':'var(--text-3)'}${done?';text-decoration:line-through':''}">${esc(st.id)}</span>` : '';
      const nameStyle = done ? 'color:var(--text-4);text-decoration:line-through' : 'color:var(--text-2)';
      const desc = st.description ? `<div class="text-[11px] ml-5 mt-0.5" style="color:var(--text-3)">${esc(st.description)}</div>` : '';
      return `<div class="flex items-center gap-2">${icon} ${id} <span class="text-sm" style="${nameStyle}">${esc(st.name)}</span>${desc}</div>`;
    }).join('');
    html += card(`${sectionTitle(t('overview.subtasks') || 'Subtasks')}<div class="space-y-1.5">${subtasks}</div>`);
  }

  if (d.spec) html += renderDocSection('Spec', d.specSource);
  if (d.plan) html += renderDocSection('Plan', d.planSource);

  el.innerHTML = html || `<div style="color:var(--text-4)" class="text-sm mt-8 text-center">No overview data</div>`;
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): overview tab with glass cards, priority badges, subtask styling"
```

---

### Task 7: Polish Code Blocks, Markdown, and Overall Feel

**Files:**
- Modify: `internal/server/static/index.html:50-66` (md-body CSS)
- Modify: `internal/server/static/index.html:86-91` (theme-card CSS)

- [ ] **Step 1: Update markdown CSS**

```css
.md-body pre { background: rgba(0,0,0,.4); padding: 1em 1.2em; border-radius: 8px; overflow-x: auto; margin: .6em 0; border: 1px solid rgba(255,255,255,.04); }
.md-body code { background: rgba(255,255,255,.08); padding: .15em .4em; border-radius: 4px; font-size: .9em; }
```

- [ ] **Step 2: Update card border-radius to 12px**

Update `renderTaskItem` cards from `rounded-lg` to `rounded-xl` (already done in Task 4).

Update `.msg-body` border-left removal (done in Task 5 — no longer uses `border-l-2`).

Update `modal-panel` border-radius to 12px:

```css
.modal-panel { ... border-radius: 12px; ... }
```

- [ ] **Step 3: Update detail header tab accent to use --accent**

```html
<!-- Line 156-158: Update tab buttons to use var(--accent) -->
<button class="detail-tab text-xs font-semibold py-1 border-b-2" style="border-color:var(--accent);color:var(--accent)" data-tab="overview" ...>
```

And update `switchDetailTab` / `setDetailTabUI` to use `var(--accent)` instead of hardcoded emerald:

Find the `setDetailTabUI` function and ensure active tabs use `--accent`.

- [ ] **Step 4: Final visual verification**

Run: `go build ./... && go vet ./... && go test ./...`

Start the dashboard: `bin/4x live -w` and verify in browser:
1. Default theme is Frost with gradient background
2. Sidebar sections have colored pill badges
3. Running cards have glass effect with phase badge
4. Message cards have role emoji and colored borders
5. Overview tab has glass card sections
6. Code blocks have deeper background

- [ ] **Step 5: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F039): polish code blocks, border-radius, and tab accent colors"
```
