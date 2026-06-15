function renderTabs() {
  const el = document.getElementById('tabs');
  el.innerHTML = openTabs.map(tab => {
    const a = tab.id === activeProjectId;
    return `<div class="tab-item ${a?'active':''}" onclick="switchTab('${escAttr(tab.id)}')"><span>${esc(tab.name)}</span><span class="tab-close" onclick="event.stopPropagation();closeTab('${escAttr(tab.id)}')">&times;</span></div>`;
  }).join('');
}
function switchTab(pid) { activeProjectId=pid; current=null; lastMsgCount=0; disconnectSSE(); saveTabState(); renderTabs(); goHome(); }
function closeTab(pid) {
  openTabs = openTabs.filter(tb => tb.id !== pid);
  if (activeProjectId === pid) { activeProjectId = openTabs.length > 0 ? openTabs[0].id : null; current = null; disconnectSSE(); }
  saveTabState(); renderTabs();
  if (activeProjectId) load(); else renderProjectPicker();
}
function addTab(project) {
  if (!openTabs.find(tb => tb.id === project.id)) openTabs.push({ id: project.id, name: project.name });
  activeProjectId = project.id; saveTabState(); renderTabs(); load();
}

async function loadProjects() { try { projects = await (await fetch('/api/projects')).json() || []; } catch { projects = []; } }
function showProjectPicker() {
  document.getElementById('picker-modal').classList.add('open');
  document.getElementById('path-input').value = '';
  document.getElementById('path-error').style.display = 'none';
  renderRecentList();
  document.getElementById('path-input').focus();
  if (window._isNativeApp && window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.nativeOpenFolder) {
    const btn = document.getElementById('picker-open-btn');
    if (btn) { btn.textContent = t('picker.browseEllipsis'); btn.onclick = function() { window.webkit.messageHandlers.nativeOpenFolder.postMessage('open'); }; }
  }
}
function closeProjectPicker() { document.getElementById('picker-modal').classList.remove('open'); }
function renderRecentList() {
  const el = document.getElementById('recent-list');
  const unopened = projects.filter(p => !openTabs.find(tb => tb.id === p.id));
  if (unopened.length === 0) { el.innerHTML = `<div style="padding:24px;text-align:center;color:var(--text-4);font-size:13px">${t('picker.noRecent')}</div>`; return; }
  el.innerHTML = unopened.map(p => `<div class="search-item" onclick="openExistingProject('${escAttr(p.id)}')"><div style="flex:1;min-width:0"><div style="font-size:13px;font-weight:600;color:var(--text-1)">${esc(p.name)}</div><div style="font-size:11px;color:var(--text-4);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(p.path)}</div></div><span style="font-size:11px;color:var(--text-4)">${t('dashboard.tasks').replace('{count}', p.taskCount)}</span></div>`).join('');
}
function openExistingProject(id) { const p = projects.find(x => x.id === id); if (p) { closeProjectPicker(); addTab(p); } }
async function addProjectFromInput(forceInit) {
  const input = document.getElementById('path-input'), errorEl = document.getElementById('path-error'), path = input.value.trim();
  if (!path) return;
  try {
    const body = forceInit ? { path, init: true } : { path };
    const resp = await fetch('/api/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (resp.status === 409) {
      const info = await resp.json();
      showInitConfirm(info.path);
      return;
    }
    if (!resp.ok) { errorEl.textContent = await resp.text(); errorEl.style.display = 'block'; return; }
    const data = await resp.json(); await loadProjects();
    const p = projects.find(x => x.id === data.id); if (p) { closeProjectPicker(); addTab(p); }
  } catch { errorEl.textContent = t('picker.connectionError'); errorEl.style.display = 'block'; }
}
function showInitConfirm(path) {
  document.getElementById('init-modal-path').textContent = path;
  document.getElementById('init-modal').classList.add('open');
}
function cancelInitConfirm() { document.getElementById('init-modal').classList.remove('open'); }
function confirmInit() { cancelInitConfirm(); addProjectFromInput(true); }
async function addProjectFromNative(path) {
  const resp = await fetch('/api/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) });
  if (resp.ok) { const data = await resp.json(); await loadProjects(); const p = projects.find(x => x.id === data.id); if (p) { closeProjectPicker(); addTab(p); } }
}
async function toggleBrowse() {
  const panel = document.getElementById('browse-panel');
  if (panel.style.display !== 'none') { panel.style.display = 'none'; return; }
  const input = document.getElementById('path-input');
  await browseTo(input.value.trim() || '~');
}
async function browseTo(dir) {
  const panel = document.getElementById('browse-panel');
  const list = document.getElementById('browse-list');
  const pathEl = document.getElementById('browse-path');
  try {
    const resp = await fetch('/api/browse?path=' + encodeURIComponent(dir));
    if (!resp.ok) { list.innerHTML = `<div style="padding:16px 24px;color:#f87171;font-size:12px">${await resp.text()}</div>`; panel.style.display = 'block'; return; }
    const data = await resp.json();
    pathEl.textContent = data.current;
    document.getElementById('path-input').value = data.current;
    const parent = data.current.split('/').slice(0, -1).join('/') || '/';
    let html = '';
    if (data.is4x) {
      html += `<div style="padding:10px 24px;background:rgba(16,185,129,.1);border-bottom:1px solid var(--border);display:flex;align-items:center;gap:8px">
        <span style="font-size:9px;padding:2px 6px;border-radius:4px;background:var(--accent);color:#000;font-weight:700">4x</span>
        <span style="flex:1;font-size:13px;color:var(--text-1);font-weight:600">${t('picker.is4xProject')}</span>
        <button onclick="addProjectFromInput()" style="padding:6px 14px;background:var(--accent);color:#000;border:none;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer">${t('picker.openProject')}</button>
      </div>`;
    }
    html += `<div class="search-item" onclick="browseTo('${escAttr(parent)}')" style="color:var(--text-3)">${t('browse.up')}</div>`;
    (data.dirs || []).forEach(d => {
      const badge = d.is4x ? '<span style="font-size:9px;padding:2px 6px;border-radius:4px;background:var(--accent);color:#000;font-weight:700;margin-left:6px">4x</span>' : '';
      html += `<div class="search-item" style="display:flex;align-items:center;gap:8px">
        <span style="flex:1;min-width:0;cursor:pointer" onclick="browseTo('${escAttr(d.path)}')">${esc(d.name)}${badge}</span>
        ${d.is4x ? `<button onclick="event.stopPropagation();document.getElementById('path-input').value='${escAttr(d.path)}';addProjectFromInput()" style="padding:4px 10px;background:var(--accent);color:#000;border:none;border-radius:6px;font-size:11px;font-weight:600;cursor:pointer;flex-shrink:0">${t('picker.open')}</button>` : ''}
      </div>`;
    });
    if (!(data.dirs || []).length && !data.is4x) html += `<div style="padding:16px 24px;color:var(--text-4);font-size:12px">${t('picker.noSubdirs')}</div>`;
    list.innerHTML = html;
    panel.style.display = 'block';
  } catch { list.innerHTML = `<div style="padding:16px 24px;color:#f87171;font-size:12px">${t('picker.connectionError')}</div>`; panel.style.display = 'block'; }
}
function renderProjectPicker() {
  document.getElementById('dashboard').innerHTML = `<div style="display:flex;align-items:center;justify-content:center;min-height:60vh;flex-direction:column;gap:16px"><div style="font-size:24px;font-weight:700;color:var(--text-1)">4x Live</div><div style="font-size:14px;color:var(--text-3)">${t('app.selectProject')}</div><button onclick="showProjectPicker()" style="margin-top:8px;padding:10px 24px;background:var(--accent);color:#000;border:none;border-radius:8px;font-size:14px;font-weight:600;cursor:pointer">${t('app.openProject')}</button></div>`;
  document.getElementById('dashboard').classList.remove('hidden');
  document.getElementById('header').classList.add('hidden');
  document.getElementById('messages').classList.add('hidden');
}

async function loadAllTabTasks() {
  for (const tab of openTabs) {
    try { const tasks = await (await fetch('/api/project/' + tab.id + '/api/tasks')).json(); allTabTasks[tab.id] = (tasks || []).map(f => ({ ...f, _projectId: tab.id, _projectName: tab.name })); }
    catch { allTabTasks[tab.id] = []; }
  }
}
async function openSearch() {
  document.getElementById('search-modal').classList.add('open');
  const inp = document.getElementById('search-input'); inp.value = ''; inp.focus(); searchIdx = 0;
  await loadAllTabTasks(); renderSearchResults('');
}
function closeSearch() { document.getElementById('search-modal').classList.remove('open'); }
function onSearchInput() { searchIdx = 0; renderSearchResults(document.getElementById('search-input').value); }
function renderSearchResults(query) {
  const el = document.getElementById('search-results');
  let pool = [], scopeId = null, actualQuery = query;
  const sm = query.match(/^@(\S+)\s*(.*)/);
  if (sm) { actualQuery = sm[2]; const tab = openTabs.find(tb => tb.name.toLowerCase().includes(sm[1].toLowerCase()) || tb.id.toLowerCase().includes(sm[1].toLowerCase())); if (tab) scopeId = tab.id; }
  for (const tab of openTabs) { if (scopeId && tab.id !== scopeId) continue; pool.push(...(allTabTasks[tab.id] || [])); }
  searchFiltered = actualQuery ? pool.map(task => ({...task, _score: Math.max(fuzzyScore(actualQuery, task.id), fuzzyScore(actualQuery, task.name))})).filter(task => task._score > 0).sort((a, b) => b._score - a._score) : pool;
  if (searchIdx >= searchFiltered.length) searchIdx = Math.max(0, searchFiltered.length - 1);
  el.innerHTML = searchFiltered.map((task, i) => {
    const isActive = task.active && task.phase && task.phase !== 'done';
    const pl = openTabs.length > 1 ? `<span style="padding:1px 6px;font-size:10px;background:var(--bg-hover);border-radius:4px;color:var(--text-3)">${esc(task._projectName)}</span>` : '';
    let b; if (isActive) b=`<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(16,185,129,.15);color:#34d399;border:1px solid rgba(16,185,129,.3);border-radius:99px">${t('status.inProgress')}</span>`;
    else if (task.status==='ready-for-review') b=`<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(245,158,11,.15);color:#fbbf24;border:1px solid rgba(245,158,11,.3);border-radius:99px">${t('status.review')}</span>`;
    else if (task.status==='done') b=`<span style="padding:2px 8px;font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:99px">${t('status.done')}</span>`;
    else if (task.status==='abandoned') b=`<span style="padding:2px 8px;font-size:10px;color:var(--text-4);border:1px solid var(--border);border-radius:99px;text-decoration:line-through">${t('status.abandoned')}</span>`;
    else if (task.status==='blocked') b=`<span style="padding:2px 8px;font-size:10px;color:#f87171;border:1px solid rgba(248,113,113,.3);border-radius:99px">${t('status.blocked')}</span>`;
    else b=`<span style="padding:2px 8px;font-size:10px;color:var(--text-4);border:1px solid var(--border);border-radius:99px">${t('status.notStarted')}</span>`;
    return `<div class="search-item ${i===searchIdx?'active':''}" onclick="selectSearch(${i})" onmouseenter="highlightSearch(${i})">${pl}<span style="font-size:13px;font-weight:600;color:${isActive?'#34d399':'var(--accent)'};min-width:80px">${esc(task.id)}</span><span style="flex:1;font-size:13px;color:var(--text-2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(task.name)}</span>${b}</div>`;
  }).join('');
}
function highlightSearch(idx) { const items = document.getElementById('search-results').children; if (items[searchIdx]) items[searchIdx].classList.remove('active'); searchIdx = idx; if (items[searchIdx]) items[searchIdx].classList.add('active'); }
function selectSearch(idx) {
  const item = searchFiltered[idx]; if (!item) return; closeSearch();
  if (item._projectId && item._projectId !== activeProjectId) switchTab(item._projectId);
  current = item.id; load(); loadDetail(item);
}
function onSearchKey(e) {
  if (e.key==='ArrowDown') { e.preventDefault(); searchIdx=Math.min(searchIdx+1,searchFiltered.length-1); renderSearchResults(document.getElementById('search-input').value); }
  else if (e.key==='ArrowUp') { e.preventDefault(); searchIdx=Math.max(searchIdx-1,0); renderSearchResults(document.getElementById('search-input').value); }
  else if (e.key==='Enter') { e.preventDefault(); selectSearch(searchIdx); }
  else if (e.key==='Escape') { closeSearch(); }
}

document.addEventListener('keydown', e => {
  if ((e.metaKey||e.ctrlKey) && e.key==='k') { e.preventDefault(); openSearch(); }
  else if ((e.metaKey||e.ctrlKey) && !e.shiftKey && e.key===',') { e.preventDefault(); activeProjectId?openProjectSettings():openGlobalSettings(); }
  else if ((e.metaKey||e.ctrlKey) && e.shiftKey && e.key===',') { e.preventDefault(); openGlobalSettings(); }
  else if (e.key==='Escape') {
    if (document.getElementById('global-settings-modal').classList.contains('open')) closeGlobalSettings();
    else if (document.getElementById('project-settings-modal').classList.contains('open')) closeProjectSettings();
    else if (document.getElementById('picker-modal').classList.contains('open')) closeProjectPicker();
    else if (document.getElementById('search-modal').classList.contains('open')) closeSearch();
  }
});

function badge(status, phase, active) {
  if (active && phase && phase!=='done') return `<span class="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-semibold bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 rounded-full"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span>${t('status.inProgress')}</span>`;
  if (status==='ready-for-review') return `<span class="px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded-full">${t('status.review')}</span>`;
  if (status==='done') return `<span class="px-2 py-0.5 text-[10px] text-zinc-500 border border-zinc-700/50 rounded-full">${t('status.done')}</span>`;
  if (status==='abandoned') return `<span class="px-2 py-0.5 text-[10px] text-zinc-600 border border-zinc-800/50 rounded-full line-through">${t('status.abandoned')}</span>`;
  if (status==='blocked') return `<span class="px-2 py-0.5 text-[10px] text-red-400 border border-red-500/30 rounded-full">${t('status.blocked')}</span>`;
  return `<span class="px-2 py-0.5 text-[10px] text-zinc-600 border border-zinc-800 rounded-full">${t('status.backlog')}</span>`;
}

function showConfirm(title, message, onConfirm) {
  let modal = document.getElementById('confirm-modal');
  if (!modal) {
    modal = document.createElement('div');
    modal.id = 'confirm-modal';
    modal.className = 'modal-backdrop';
    document.body.appendChild(modal);
  }
  modal.innerHTML = `<div class="modal-panel fade-in" style="width:460px;max-width:90vw"><div class="p-5"><div class="font-bold text-sm mb-2">${title}</div><div class="text-sm text-zinc-400">${message}</div></div><div class="flex justify-end gap-2 px-5 pb-4"><button id="confirm-cancel" class="px-4 py-1.5 text-xs rounded border transition-colors" style="border-color:var(--border);color:var(--text-3)" onmouseenter="this.style.background='var(--hover)'" onmouseleave="this.style.background='transparent'">${t('common.cancel')}</button><button id="confirm-ok" class="px-4 py-1.5 text-xs rounded bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition-colors">${t('common.confirm')}</button></div></div>`;
  modal.classList.add('open');
  modal.querySelector('#confirm-cancel').onclick = () => modal.classList.remove('open');
  modal.querySelector('#confirm-ok').onclick = () => { modal.classList.remove('open'); onConfirm(); };
  modal.onclick = (e) => { if (e.target === modal) modal.classList.remove('open'); };
}
async function markDone(fid) {
  showConfirm(t('markDone.title'), t('markDone.message').replace('{id}', `<strong>${esc(fid)}</strong>`), async () => {
    const res = await fetch(apiBase()+'/api/done', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id:fid})});
    if (!res.ok) { showToast(t('toast.failed').replace('{error}', await res.text())); return; }
    const data = await res.json();
    if (data.merge_conflict) {
      showToast(t('toast.mergeConflicts').replace('{conflicts}', (data.conflicts||[]).join('\n')).replace('{id}', fid), 'warn', 10000);
    } else if (data.merge_error) {
      showToast(t('toast.mergeFailed').replace('{error}', data.merge_error), 'warn', 10000);
    } else {
      showToast(t('toast.doneSuccess').replace('{id}', fid), 'success', 3000);
    }
    load(); if (!current) renderDashboard(lastTasks);
  });
}
function goHome() {
  current=null; lastMsgCount=0; disconnectSSE(); disconnectLogSSE(); stopLogsRefresh();
  if (_logDurTimer) { clearInterval(_logDurTimer); _logDurTimer = null; }
  document.getElementById('header').classList.add('hidden');
  document.getElementById('overview-panel').classList.add('hidden');
  document.getElementById('messages').classList.add('hidden');
  document.getElementById('messages').innerHTML = '';
  const lp = document.getElementById('logs-panel');
  lp.classList.add('hidden'); lp.style.display = 'none';
  document.getElementById('dashboard').classList.remove('hidden');
  activeDetailTab = 'overview';
  if (activeProjectId) { load(); renderDashboard(lastTasks); } else renderProjectPicker();
}
function connectSSE(fid) { disconnectSSE(); sseSource = new EventSource(sseBase()+'/events/'+fid); sseSource.onmessage = () => { loadMessages(fid); }; }
function disconnectSSE() { if (sseSource) { sseSource.close(); sseSource = null; } }

function classify(tasks) {
  const g = { running: [], review: [], pending: [], todo: [], done: [] };
  (tasks||[]).forEach(f => { const a = f.active && f.phase && f.phase!=='done'; if (a) g.running.push(f); else if (f.status==='ready-for-review' || f.phase==='pending-review') g.review.push(f); else if (f.status==='done' || f.status==='abandoned') g.done.push(f); else if (f.status==='in-progress' || f.status==='needs-attention') g.pending.push(f); else g.todo.push(f); });
  g.todo.sort((a, b) => { const pa = a.priority != null ? a.priority : 999, pb = b.priority != null ? b.priority : 999; return pa - pb || a.id.localeCompare(b.id); });
  g.done.sort((a, b) => (b.updatedAt || '').localeCompare(a.updatedAt || ''));
  return g;
}

// openFeatureDetail 由 DAG 節點點擊呼叫，開啟對應 feature 的詳情面板（與 task card 同一路徑）。
function openFeatureDetail(fid) {
  const task = lastTasks.find(t => t.id === fid);
  if (!task) return;
  current = fid;
  load();
  loadDetail(task);
}

// dagNodeColor 依 feature 狀態決定 DAG 節點配色：done=綠、running=藍、blocked/needs-attention=紅、
// ready-for-review=琥珀、其餘（todo/in-progress）=灰。
function dagNodeColor(task) {
  const running = task.active && task.phase && task.phase !== 'done';
  if (running) return { bg: 'rgba(59,130,246,.15)', border: 'rgba(59,130,246,.5)', dot: '#3b82f6', text: '#93c5fd' };
  if (task.status === 'done') return { bg: 'rgba(16,185,129,.15)', border: 'rgba(16,185,129,.5)', dot: '#10b981', text: '#6ee7b7' };
  if (task.status === 'blocked' || task.status === 'needs-attention') return { bg: 'rgba(239,68,68,.15)', border: 'rgba(239,68,68,.5)', dot: '#ef4444', text: '#fca5a5' };
  if (task.status === 'ready-for-review') return { bg: 'rgba(245,158,11,.15)', border: 'rgba(245,158,11,.5)', dot: '#f59e0b', text: '#fcd34d' };
  return { bg: 'rgba(113,113,122,.12)', border: 'rgba(113,113,122,.4)', dot: '#a1a1aa', text: '#d4d4d8' };
}

// renderDag 由 lastTasks 建出 dependency DAG，純 SVG 渲染（無外部圖表 library）。
// 節點為 feature、邊為 depends（被依賴者指向依賴者）；依 depends 深度分層，同層水平排列。
// 無任何依賴關係時回空字串（不顯示空圖）。
function renderDag(tasks) {
  const all = tasks || [];
  const allById = {};
  all.forEach(t => { allById[t.id] = t; });
  const undone = all.filter(t => t.status !== 'done' && t.status !== 'abandoned');

  const edges = [];
  const involved = new Set();
  undone.forEach(t => {
    (t.depends || []).forEach(d => {
      if (allById[d]) { edges.push([d, t.id]); involved.add(d); involved.add(t.id); }
    });
  });
  if (edges.length === 0) return '';

  const byId = {};
  involved.forEach(id => { if (allById[id]) byId[id] = allById[id]; });

  const nodes = [...involved].map(id => byId[id]);
  const depth = {};
  function calcDepth(id, seen) {
    if (depth[id] != null) return depth[id];
    if (seen.has(id)) return 0;
    seen.add(id);
    let d = 0;
    (byId[id].depends || []).forEach(dep => { if (byId[dep]) d = Math.max(d, calcDepth(dep, seen) + 1); });
    seen.delete(id);
    depth[id] = d;
    return d;
  }
  nodes.forEach(n => calcDepth(n.id, new Set()));

  const layers = {};
  nodes.forEach(n => { (layers[depth[n.id]] = layers[depth[n.id]] || []).push(n); });
  const layerKeys = Object.keys(layers).map(Number).sort((a, b) => a - b);
  layerKeys.forEach(k => layers[k].sort((a, b) => a.id.localeCompare(b.id)));

  const NW = 200, NH = 42, GX = 32, GY = 78;
  const pos = {};
  let maxCols = 0;
  layerKeys.forEach(k => { maxCols = Math.max(maxCols, layers[k].length); });
  layerKeys.forEach((k, li) => {
    layers[k].forEach((n, ci) => { pos[n.id] = { x: ci * (NW + GX) + GX, y: li * GY + 16 }; });
  });
  const width = Math.max(maxCols * (NW + GX) + GX, NW + GX * 2);
  const height = layerKeys.length * GY + 16;

  let svgEdges = '';
  edges.forEach(([from, to]) => {
    const p = pos[from], c = pos[to];
    if (!p || !c) return;
    const x1 = p.x + NW / 2, y1 = p.y + NH, x2 = c.x + NW / 2, y2 = c.y;
    const my = (y1 + y2) / 2;
    svgEdges += `<path class="dag-edge" d="M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}" marker-end="url(#dag-arrow)"/>`;
  });

  let svgNodes = '';
  nodes.forEach(n => {
    const p = pos[n.id], col = dagNodeColor(n);
    const name = esc((n.name || '').slice(0, 20));
    const clipId = 'dag-clip-' + n.id.replace(/[^a-zA-Z0-9]/g, '_');
    svgNodes += `<g class="dag-node" onclick="openFeatureDetail('${escAttr(n.id)}')" transform="translate(${p.x},${p.y})">
      <clipPath id="${clipId}"><rect width="${NW - 4}" height="${NH}" rx="8"/></clipPath>
      <rect width="${NW}" height="${NH}" rx="8" fill="${col.bg}" stroke="${col.border}" stroke-width="1.5"/>
      <circle cx="15" cy="${NH / 2}" r="4" fill="${col.dot}"/>
      <text x="28" y="18" class="dag-node-id" fill="${col.text}" clip-path="url(#${clipId})">${esc(n.id)}</text>
      <text x="28" y="33" class="dag-node-name" clip-path="url(#${clipId})">${name}</text>
    </g>`;
  });

  return `<div class="dash-card mb-4" id="dag-view"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-3">${t('dag.title')}</div>
    <div class="dag-scroll"><svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" xmlns="http://www.w3.org/2000/svg">
      <defs><marker id="dag-arrow" markerWidth="9" markerHeight="9" refX="7" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 Z" fill="var(--text-4)"/></marker></defs>
      ${svgEdges}${svgNodes}
    </svg></div></div>`;
}

// renderBatchPanel 依 /api/batch/status 渲染 batch 控制區：Start/Stop/Continue、執行指示、
// 衝突提示卡與佇列進度（done 打勾、running 標記、waiting 顯示佇列位置）。
function renderBatchPanel(status) {
  const running = !!(status && status.running);
  const conflict = status && status.conflict;

  let controls;
  if (running) {
    controls = `<span class="batch-running-ind"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span>${t('batch.running')}</span>
      <button onclick="stopBatch()" class="batch-btn batch-btn-stop">${t('batch.stop')}</button>`;
  } else {
    controls = `<button onclick="startBatch()" class="batch-btn batch-btn-start">${t('batch.start')}</button>`;
  }

  let conflictCard = '';
  if (conflict) {
    const files = (conflict.files || []).map(f => `<li>${esc(f)}</li>`).join('');
    const repo = conflict.conflictRepo ? `<span class="batch-conflict-repo">${esc(conflict.conflictRepo)}</span>` : '';
    conflictCard = `<div class="batch-conflict">
      <div class="batch-conflict-title">⚠ ${t('batch.conflictTitle').replace('{id}', esc(conflict.featureId || ''))} ${repo}</div>
      ${files ? `<ul class="batch-conflict-files">${files}</ul>` : ''}
      <button onclick="continueBatch()" class="batch-btn batch-btn-continue">${t('batch.continue')}</button>
    </div>`;
  }

  // 上次 batch 報告：batch 沒在跑且有報告時，渲染可展開的摘要卡（outcome 色碼 + 完成/失敗/剩餘 + 總耗時）。
  let lastReportHtml = '';
  if (!running && status && status.lastReport) {
    const rep = status.lastReport;
    const OC = {
      completed:   { c: '#34d399', bg: 'rgba(52,211,153,.15)' },
      stopped:     { c: '#facc15', bg: 'rgba(250,204,21,.15)' },
      interrupted: { c: '#fb923c', bg: 'rgba(251,146,60,.15)' },
      crashed:     { c: '#f87171', bg: 'rgba(248,113,113,.15)' },
    };
    const oc = OC[rep.outcome] || OC.completed;
    const dur = formatDuration(rep.durationMs || 0);
    const rows = (rep.features || []).map(f => {
      const fdur = f.durationMs ? formatDuration(f.durationMs) : '—';
      const stop = f.stopReason ? `<span class="batch-report-stop">${esc(f.stopReason)}</span>` : '';
      return `<div class="batch-report-row"><span class="batch-report-row-id">${esc(f.id)}</span><span class="batch-report-row-name">${esc(f.name || '')}</span><span class="batch-report-row-status">${esc(f.finalStatus || '')}</span><span class="batch-report-row-meta">${t('batch.reportRounds').replace('{n}', f.rounds || 0)} · ${fdur}</span>${stop}</div>`;
    }).join('');
    const panic = rep.panicMessage ? `<div class="batch-report-panic">${t('batch.panicMessage')}: ${esc(rep.panicMessage)}</div>` : '';
    lastReportHtml = `<div class="batch-report">
      <button type="button" class="batch-report-head" onclick="toggleBatchReport()">
        <span id="batch-report-chevron" class="batch-report-chevron">${_batchReportOpen ? '▾' : '▸'}</span>
        <span class="batch-report-label">${t('batch.lastReport')}</span>
        <span class="batch-report-badge" style="color:${oc.c};background:${oc.bg}">${t('batch.outcome.' + rep.outcome) || rep.outcome}</span>
        <span class="batch-report-counts">${t('batch.reportCompleted').replace('{n}', rep.completed)} · ${t('batch.reportFailed').replace('{n}', rep.failed)} · ${t('batch.reportRemaining').replace('{n}', rep.remaining)}</span>
        <span class="batch-report-dur">${t('batch.reportDuration').replace('{d}', dur)}</span>
      </button>
      <div id="batch-report-detail" class="batch-report-detail${_batchReportOpen ? '' : ' hidden'}">${rows}${panic}</div>
    </div>`;
  }

  let queueHtml = '';
  if (status && status.queue && status.queue.length) {
    const PL={0:{l:'P0',c:'#f87171',bg:'rgba(248,113,113,.15)'},1:{l:'P1',c:'#fb923c',bg:'rgba(251,146,60,.12)'},2:{l:'P2',c:'#facc15',bg:'rgba(250,204,21,.10)'},3:{l:'P3',c:'#60a5fa',bg:'rgba(96,165,250,.10)'}};
    const completed = status.queue.filter(q => q.state === 'done' || q.state === 'error');
    const active = status.queue.filter(q => q.state !== 'done' && q.state !== 'error');
    let waitNum = 0;
    const renderItem = q => {
      let icon, cls;
      if (q.state === 'done') { icon = '✓'; cls = 'done'; }
      else if (q.state === 'error') { icon = '⚠'; cls = 'error'; }
      else if (q.state === 'running') { icon = '▶'; cls = 'running'; }
      else { waitNum++; icon = '#' + waitNum; cls = 'waiting'; }
      const pri = q.priority != null && PL[q.priority] ? `<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${PL[q.priority].bg};color:${PL[q.priority].c};font-weight:600">${PL[q.priority].l}</span>` : '';
      return `<div class="batch-q-item batch-q-${cls}"><span class="batch-q-icon">${icon}</span>${pri}<span class="batch-q-id">${esc(q.featureId)}</span><span class="batch-q-name">${esc(q.name || '')}</span></div>`;
    };
    queueHtml = active.map(renderItem).join('') + (completed.length ? `<div class="batch-q-divider"></div>` + completed.map(renderItem).join('') : '');
  }

  return `<div class="dash-card mb-4" id="batch-panel"><div class="flex items-center gap-2 mb-3">
      <span class="text-[10px] font-bold dash-muted uppercase tracking-wider">${t('batch.title')}</span>
      <div class="ml-auto flex items-center gap-2">${controls}</div>
    </div>${conflictCard}${lastReportHtml}${queueHtml ? `<div class="batch-queue">${queueHtml}</div>` : ''}</div>`;
}

// _batchReportOpen 記住「上次 batch 報告」展開狀態，跨輪詢重繪保持不變。
let _batchReportOpen = false;

// toggleBatchReport 展開/收合上次 batch 報告詳情（class toggle，不重繪整個面板以免閃爍）。
function toggleBatchReport() {
  _batchReportOpen = !_batchReportOpen;
  const d = document.getElementById('batch-report-detail');
  const c = document.getElementById('batch-report-chevron');
  if (d) d.classList.toggle('hidden', !_batchReportOpen);
  if (c) c.textContent = _batchReportOpen ? '▾' : '▸';
}

// startBatch 先彈確認對話框（避免誤觸），確認後 POST /api/batch/start 啟動 batch run。
function startBatch() {
  showConfirm(t('batch.confirmTitle'), t('batch.confirmMsg'), async () => {
    const res = await fetch(apiBase() + '/api/batch/start', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    if (!res.ok) { showToast(t('batch.startFailed').replace('{error}', await res.text())); return; }
    load();
  });
}

// stopBatch 送出 graceful 停止信號（batch 跑完當前 feature 後結束）。
function stopBatch() {
  fetch(apiBase() + '/api/batch/stop', { method: 'POST' }).then(async res => {
    if (!res.ok) { showToast(t('batch.stopFailed').replace('{error}', await res.text())); return; }
    load();
  });
}

// continueBatch 在使用者解完衝突後呼叫：後端先清衝突信號再重啟 batch。
function continueBatch() {
  fetch(apiBase() + '/api/batch/continue', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }).then(async res => {
    if (!res.ok) { showToast(t('batch.continueFailed').replace('{error}', await res.text())); return; }
    load();
  });
}

function renderDashboard(tasks) {
  const el = document.getElementById('dashboard');
  if (current) { el.classList.add('hidden'); return; }
  el.classList.remove('hidden');
  const g = classify(tasks), total = tasks.length;
  const buckets = {'1':0,'2':0,'3-4':0,'5+':0};
  const doneTasks = g.done.filter(f => f.round > 0);
  doneTasks.forEach(f => { if (f.round<=1) buckets['1']++; else if (f.round===2) buckets['2']++; else if (f.round<=4) buckets['3-4']++; else buckets['5+']++; });
  const maxB = Math.max(1, ...Object.values(buckets));
  const rP=total?g.running.length/total:0, rvP=total?g.review.length/total:0, pP=total?g.pending.length/total:0, tP=total?g.todo.length/total:0;
  const a1=rP*360, a2=a1+rvP*360, a3=a2+pP*360, a4=a3+tP*360;
  const donut = total ? `conic-gradient(#10b981 0deg ${a1}deg,#f59e0b ${a1}deg ${a2}deg,#3b82f6 ${a2}deg ${a3}deg,#a78bfa ${a3}deg ${a4}deg,#22c55e ${a4}deg 360deg)` : 'conic-gradient(#27272a 0deg 360deg)';
  const recent = g.done.slice(0, 8);
  const activeTab = openTabs.find(tb => tb.id === activeProjectId);
  const pName = activeTab ? activeTab.name : '4x Live';
  el.innerHTML = `<div class="flex items-center gap-3 mb-6"><span class="text-lg font-bold">${esc(pName)}</span><span class="ml-auto px-3 py-1 text-xs rounded-full" style="border:1px solid var(--border);color:var(--text-2)">${t('dashboard.tasks').replace('{count}', total)}</span><button onclick="activeProjectId?openProjectSettings():openGlobalSettings()" title="${t('settings.titleShortcut')}" class="ml-2 transition-colors" style="color:var(--text-3)"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12.22 2h-.44a2 2 0 00-2 2v.18a2 2 0 01-1 1.73l-.43.25a2 2 0 01-2 0l-.15-.08a2 2 0 00-2.73.73l-.22.38a2 2 0 00.73 2.73l.15.1a2 2 0 011 1.72v.51a2 2 0 01-1 1.74l-.15.09a2 2 0 00-.73 2.73l.22.38a2 2 0 002.73.73l.15-.08a2 2 0 012 0l.43.25a2 2 0 011 1.73V20a2 2 0 002 2h.44a2 2 0 002-2v-.18a2 2 0 011-1.73l.43-.25a2 2 0 012 0l.15.08a2 2 0 002.73-.73l.22-.39a2 2 0 00-.73-2.73l-.15-.08a2 2 0 01-1-1.74v-.5a2 2 0 011-1.74l.15-.09a2 2 0 00.73-2.73l-.22-.38a2 2 0 00-2.73-.73l-.15.08a2 2 0 01-2 0l-.43-.25a2 2 0 01-1-1.73V4a2 2 0 00-2-2z"/><circle cx="12" cy="12" r="3"/></svg></button></div>
<div class="grid grid-cols-5 gap-4 mb-8">
<div class="dash-card text-center"><div class="text-3xl font-bold text-emerald-400">${g.running.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.running')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-amber-400">${g.review.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.review')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-blue-400">${g.pending.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.pending')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-purple-400">${g.todo.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.todo')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-green-400">${g.done.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.done')}</div></div></div>
${renderBatchPanel(lastBatchStatus)}
${renderDag(tasks)}
<div class="grid grid-cols-2 gap-4 mb-8">
<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4">${t('dashboard.status')}</div><div class="flex items-center gap-6"><div class="relative w-28 h-28 flex-shrink-0"><div class="w-28 h-28 rounded-full" style="background:${donut}"></div><div class="absolute inset-3 rounded-full dash-donut-center flex items-center justify-center flex-col"><span class="text-xl font-bold">${total}</span><span class="text-[10px] dash-muted">${t('dashboard.total')}</span></div></div><div class="space-y-2 text-xs"><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>${t('sidebar.running')}<span class="ml-auto dash-sub font-bold">${g.running.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>${t('sidebar.review')}<span class="ml-auto dash-sub font-bold">${g.review.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-blue-500"></span>${t('sidebar.pending')}<span class="ml-auto dash-sub font-bold">${g.pending.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-purple-500"></span>${t('sidebar.todo')}<span class="ml-auto dash-sub font-bold">${g.todo.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-green-500"></span>${t('sidebar.done')}<span class="ml-auto dash-sub font-bold">${g.done.length}</span></div></div></div></div>
<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4">${t('dashboard.roundsDist')}</div><div class="space-y-3">${Object.entries(buckets).map(([l,c])=>{const p=maxB?(c/maxB)*100:0;const co={'1':'#10b981','2':'#3b82f6','3-4':'#f59e0b','5+':'#ef4444'};return `<div class="flex items-center gap-3"><span class="text-xs dash-muted w-8 text-right">${l}R</span><div class="flex-1 h-5 dash-bar-bg rounded overflow-hidden"><div class="h-full rounded" style="width:${p}%;background:${co[l]}"></div></div><span class="text-xs font-bold w-6 text-right" style="color:var(--text-1)">${c}</span></div>`;}).join('')}</div>${doneTasks.length>0?`<div class="text-[10px] dash-muted mt-3">${t('dashboard.avgRounds').replace('{avg}', (doneTasks.reduce((s,d)=>s+d.round,0)/doneTasks.length).toFixed(1))}</div>`:''}</div></div>
${recent.length>0?`<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4">${t('dashboard.recentCompletions')}</div><div class="space-y-2">${recent.map(f=>{const PL={0:{l:'P0',c:'#f87171',bg:'rgba(248,113,113,.15)'},1:{l:'P1',c:'#fb923c',bg:'rgba(251,146,60,.12)'},2:{l:'P2',c:'#facc15',bg:'rgba(250,204,21,.10)'},3:{l:'P3',c:'#60a5fa',bg:'rgba(96,165,250,.10)'}};const pt=f.priority!=null&&PL[f.priority]?`<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${PL[f.priority].bg};color:${PL[f.priority].c};font-weight:600">${PL[f.priority].l}</span>`:'';const dur=f.createdAt&&f.updatedAt?formatDuration(f.createdAt,f.updatedAt):'';return `<div class="flex items-center gap-2 py-1.5 cursor-pointer rounded px-2 -mx-2 transition-colors" onmouseenter="this.style.background='var(--bg-hover)'" onmouseleave="this.style.background=''" onclick="current='${f.id}';load();loadDetail(${JSON.stringify(f).replace(/"/g,'&quot;')})"><span class="text-emerald-500/60 text-xs">✓</span>${pt}<span class="text-xs font-semibold text-emerald-400/80">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span>${runnerTags(f.runners)}${dur?`<span class="text-[10px] dash-muted">⏱ ${dur}</span>`:''}${f.round?`<span class="text-[10px] dash-muted">${f.round}R</span>`:''}</div>`;}).join('')}</div></div>`:''}
${g.running.length>0?`<div class="rounded-xl border border-emerald-500/20 bg-emerald-950/20 p-5 mt-4"><div class="text-[10px] font-bold text-emerald-500/70 uppercase tracking-wider mb-4">${t('dashboard.currentlyRunning')}</div><div class="space-y-2">${g.running.map(f=>`<div class="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-emerald-900/20 rounded px-2 -mx-2 transition-colors" onclick="current='${f.id}';load();loadDetail(${JSON.stringify(f).replace(/"/g,'&quot;')})"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span><span class="text-xs font-semibold text-emerald-400">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span><span class="text-[10px] text-emerald-400/70">${f.phase||''}</span>${f.round?`<span class="text-[10px] dash-muted">R${f.round}</span>`:''}</div>`).join('')}</div></div>`:''}
${g.review.length>0?`<div class="rounded-xl border border-amber-500/20 bg-amber-950/20 p-5 mt-4"><div class="text-[10px] font-bold text-amber-500/70 uppercase tracking-wider mb-4">${t('dashboard.pendingReview')}</div><div class="space-y-2">${g.review.map(f=>`<div class="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-amber-900/20 rounded px-2 -mx-2 transition-colors" onclick="current='${f.id}';load();loadDetail(${JSON.stringify(f).replace(/"/g,'&quot;')})"><span class="text-amber-400 text-xs">⏳</span><span class="text-xs font-semibold text-amber-400">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span>${f.round?`<span class="text-[10px] dash-muted">${f.round}R</span>`:''}<button class="px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors" onclick="event.stopPropagation();markDone('${f.id}')">${t('status.done')}</button></div>`).join('')}</div></div>`:''}`;
}

const _sectionCollapsed = { done: true };

function phaseBadge(phase, round) {
  const pc = PHASE_COLORS[phase];
  if (!pc || !round) return '';
  return `<span style="font-size:9px;padding:2px 7px;border-radius:6px;background:${pc.bg};color:${pc.color};font-weight:700;border:1px solid ${pc.border}">${pc.letter}${round}/5</span>`;
}

function renderTaskItem(task) {
  const isActive = task.active && task.phase && task.phase!=='done', isSel = task.id===current;
  const pc = PHASE_COLORS[task.phase];
  let cls = 'p-3 rounded-xl cursor-pointer mb-1 transition-all duration-150 ';
  let cardStyle = '';
  if (isActive) {
    cls += 'border ';
    if (pc) {
      cardStyle = `background:linear-gradient(135deg,${pc.bg},var(--glass));backdrop-filter:blur(8px);border-color:${pc.border}`;
    } else {
      cardStyle = 'background:linear-gradient(135deg,rgba(16,185,129,.08),var(--glass));backdrop-filter:blur(8px);border-color:rgba(16,185,129,.25)';
    }
  } else if (isSel) {
    cls += 'border border-zinc-700/50 ';
    cardStyle = 'background:var(--glass)';
  } else if (task.status==='done') {
    cls += 'border border-transparent opacity-40 hover:opacity-70 ';
  } else if (task.status==='ready-for-review') {
    cls += 'border ';
    cardStyle = 'background:linear-gradient(135deg,rgba(251,191,36,.06),var(--glass));border-color:rgba(251,191,36,.2)';
  } else if (task.priority != null && task.priority <= 1) {
    cls += 'border ';
    const prioBorders = {0:'rgba(248,113,113,.2)',1:'rgba(251,146,60,.2)'};
    const prioBgs = {0:'rgba(248,113,113,.04)',1:'rgba(251,146,60,.04)'};
    cardStyle = `background:linear-gradient(135deg,${prioBgs[task.priority]||'transparent'},var(--glass));border-color:${prioBorders[task.priority]||'transparent'}`;
  } else if (task.priority != null && task.priority <= 2) {
    cls += 'border ';
    cardStyle = 'background:linear-gradient(135deg,rgba(250,204,21,.03),var(--glass));border-color:rgba(250,204,21,.15)';
  } else {
    cls += 'border border-transparent hover:border-zinc-700/30 ';
  }
  const hasState = task.phase || task.round;
  const elapsed = isActive && task.createdAt ? formatElapsed(task.createdAt) : '';
  const duration = !isActive && task.createdAt && task.updatedAt ? formatDuration(task.createdAt, task.updatedAt) : '';
  const timePart = elapsed ? ` · ⏱ ${elapsed}` : duration ? ` · ⏱ ${duration}` : '';
  const roundPart = task.round ? t('common.round').replace('{round}', task.round) : '';
  const phaseRoleMap = {designing:'designer',coding:'coder',reviewing:'reviewer','deep-reviewing':'deep-reviewer',testing:'tester',accepting:'acceptor',amending:'coder'};
  const roleInfo = ROLES[phaseRoleMap[task.phase]] || {};
  const parallelTester = isActive && task.phase === 'reviewing' ? ROLES['tester'] : null;
  const emoji = isActive && roleInfo.emoji ? roleInfo.emoji + ' ' : '';
  let pi = '';
  if (isActive) {
    const dotStyle = pc ? `background:${pc.color};box-shadow:0 0 4px ${pc.border}` : 'background:#34d399';
    const phaseText = parallelTester ? `${emoji}reviewing ${parallelTester.emoji} testing` : `${emoji}${task.phase}`;
    pi = `<div class="flex items-center gap-1.5 mt-1.5 flex-wrap"><span class="w-1.5 h-1.5 rounded-full pulse-dot" style="${dotStyle}"></span><span class="text-[11px]" style="color:${pc?pc.color:'#34d399'}">${phaseText}</span><span class="text-[11px] text-zinc-600">${timePart}</span></div>`;
  } else if (hasState && (roundPart || timePart)) {
    const parts = [roundPart, timePart.replace(/^ · /, '')].filter(Boolean).join(' · ');
    pi = `<div class="flex items-center gap-1.5 mt-1.5"><span class="text-[11px] text-zinc-600">${parts}</span></div>`;
  }
  if (!isActive && task.stopReason && task.stopReason !== 'pending-review' && task.stopReason !== 'done') {
    const srColors = {'scope-change':'#f59e0b','runner-error':'#ef4444','hard-error':'#ef4444','soft-fail':'#f59e0b','interrupted':'#a78bfa','escalation':'#f59e0b','model-error':'#ef4444'};
    const srColor = srColors[task.stopReason] || '#a1a1aa';
    pi += `<div class="flex items-center gap-1 mt-1"><span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${srColor}18;color:${srColor};font-weight:600">⚠ ${task.stopReason}</span></div>`;
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
  const PRIO_LABELS = {0:{l:'P0',c:'#f87171',bg:'rgba(248,113,113,.15)'},1:{l:'P1',c:'#fb923c',bg:'rgba(251,146,60,.12)'},2:{l:'P2',c:'#facc15',bg:'rgba(250,204,21,.10)'},3:{l:'P3',c:'#60a5fa',bg:'rgba(96,165,250,.10)'},4:{l:'P4',c:'#a1a1aa',bg:'rgba(161,161,170,.10)'},5:{l:'P5',c:'#71717a',bg:'rgba(113,113,122,.08)'}};
  const prioTag = task.priority != null && PRIO_LABELS[task.priority] ? `<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${PRIO_LABELS[task.priority].bg};color:${PRIO_LABELS[task.priority].c};font-weight:600">${PRIO_LABELS[task.priority].l}</span>` : '';
  const profileTag = task.profile ? `<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:rgba(45,212,191,.12);color:#2dd4bf;font-weight:600">${esc(task.profile)}</span>` : '';
  let docTags = '';
  if (task.status !== 'done') {
    const specTag = task.hasSpec ? '<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(139,92,246,.12);color:#a78bfa">spec</span>' : '';
    const planTag = task.hasPlan ? '<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(59,130,246,.12);color:#60a5fa">plan</span>' : '';
    docTags = specTag + planTag;
  }
  const depTag = (task.depends && task.depends.length && task.status !== 'done') ? task.depends.map(d => {const dt=lastTasks.find(t=>t.id===d);const done=dt&&dt.status==='done';return done?`<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(16,185,129,.12);color:#34d399">✓ ${d}</span>`:`<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(251,146,60,.10);color:#fb923c">→ ${d}</span>`;}).join('') : '';
  const tagsLine = (prioTag || profileTag || docTags || depTag) ? `<div class="flex gap-1 mt-1 flex-wrap">${prioTag}${profileTag}${docTags}${depTag}</div>` : '';
  return `<div class="${cls}" style="${cardStyle}" onclick="current='${task.id}';load();loadDetail(${JSON.stringify(task).replace(/"/g,'&quot;')})" onmouseenter="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='1')" onmouseleave="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='0')"><div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="flex items-center gap-2"><span class="text-[13px] font-medium truncate flex-1">${esc(task.name)}</span>${badge}</div><div class="text-[11px] text-zinc-600 mt-0.5">${task.id}</div>${pi}${tagsLine}${rtLine}</div>${doneBtn || actionBtn}</div></div>`;
}

function toggleSection(key) {
  _sectionCollapsed[key] = !_sectionCollapsed[key];
  renderSidebar();
}

let _popupSections = {};
function openSectionPopup(key) {
  const data = _popupSections[key];
  if (!data) return;
  const { title, items } = data;
  let modal = document.getElementById('section-popup');
  if (!modal) {
    modal = document.createElement('div');
    modal.id = 'section-popup';
    modal.className = 'modal-backdrop';
    modal.onclick = (e) => { if (e.target === modal) modal.classList.remove('open'); };
    document.body.appendChild(modal);
  }
  modal.innerHTML = `<div class="modal-panel fade-in" style="width:480px;max-height:80vh;display:flex;flex-direction:column"><div class="flex items-center p-4 border-b" style="border-color:var(--border)"><span class="font-bold">${title}</span><span class="ml-auto text-xs text-zinc-500">${t('common.items').replace('{count}', items.length)}</span></div><div class="flex-1 overflow-y-auto p-2">${items.map(f => renderTaskItem(f)).join('')}</div></div>`;
  modal.classList.add('open');
}

function renderSidebar() {
  const groups = classify(lastTasks);
  const topEl = document.getElementById('tl-top');
  const bottomEl = document.getElementById('tl-bottom');
  if (!topEl || !bottomEl) return;
  topEl.innerHTML = ''; bottomEl.innerHTML = '';

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

  function sectionItems(items, container) {
    items.forEach(f => {
      const wrapper = document.createElement('div');
      wrapper.innerHTML = renderTaskItem(f);
      container.appendChild(wrapper.firstChild);
    });
  }

  [
    { key: 'running', title: t('sidebar.running'), items: groups.running },
    { key: 'review', title: t('sidebar.review'), items: groups.review },
    { key: 'pending', title: t('sidebar.pending'), items: groups.pending },
  ].forEach(s => {
    if (!s.items.length) return;
    const collapsed = sectionHeader(s.key, s.title, s.items.length, s.items, topEl);
    if (!collapsed) sectionItems(s.items, topEl);
  });

  [
    { key: 'todo', title: t('sidebar.todo'), items: groups.todo },
    { key: 'done', title: t('sidebar.done'), items: groups.done },
  ].forEach(s => {
    if (!s.items.length) return;
    const collapsed = sectionHeader(s.key, s.title, s.items.length, s.items, bottomEl);
    if (!collapsed) sectionItems(s.items, bottomEl);
  });

  bottomEl.style.borderTop = (groups.todo.length || groups.done.length) ? '' : 'none';
}

async function load() {
  if (!activeProjectId) { renderProjectPicker(); return; }
  const [tasks, runs, batchStatus] = await Promise.all([
    fetch(apiBase()+'/api/tasks').then(r => r.json()).catch(() => []),
    fetch(apiBase()+'/api/runs').then(r => r.ok ? r.json() : []).catch(() => []),
    fetch(apiBase()+'/api/batch/status').then(r => r.ok ? r.json() : null).catch(() => null),
  ]);
  lastTasks = tasks || [];
  activeRuns = runs || [];
  lastBatchStatus = batchStatus;
  renderSidebar();
  if (!current) renderDashboard(lastTasks);
  else document.getElementById('dashboard').classList.add('hidden');
}

async function loadDetail(task) {
  document.getElementById('dashboard').classList.add('hidden');
  document.getElementById('header').classList.remove('hidden');
  document.getElementById('messages').innerHTML = ''; lastMsgCount = 0; currentLogFile = null; multiLogActive = false; multiLogBuffers = {};
  document.getElementById('h-id').textContent = task.id;
  document.getElementById('h-name').textContent = task.name;
  document.getElementById('h-badge').innerHTML = badge(task.status, task.phase, task.active);
  const isRunning = task.active && task.phase && task.phase !== 'done';
  const hRunId = getRunId(task.id);
  let hAction = '';
  if (isRunning && hRunId) {
    hAction = `<button class="w-7 h-7 flex items-center justify-center rounded text-red-400 hover:bg-red-500/20 transition-colors" onclick="stopRun('${hRunId}')" title="${t('run.stop')}">■</button>`;
  } else if (task.status !== 'done') {
    hAction = `<button class="w-7 h-7 flex items-center justify-center rounded hover:bg-emerald-500/20 transition-colors" style="color:var(--accent)" onclick="openRunModal('${task.id}')" title="${t('run.run')}">▶</button>`;
  }
  document.getElementById('h-play-stop').innerHTML = hAction;
  const meta = [];
  if (task.phase) {
    const pi = PHASE_ICON[task.phase]||'○';
    const phaseLabel = isRunning && task.phase === 'reviewing' ? `${pi} 🔍 reviewing 🧪 testing` : `${pi} ${task.phase}`;
    meta.push(`<span>${phaseLabel}</span>`);
  }
  if (task.round) meta.push(`<span>⟳ ${t('common.round').replace('{round}', task.round)}</span>`);
  if (isRunning && task.createdAt) {
    meta.push(`<span>⏱ ${formatElapsed(task.createdAt)}</span>`);
  } else if (!isRunning && task.createdAt && task.updatedAt) {
    meta.push(`<span>⏱ ${formatDuration(task.createdAt, task.updatedAt)}</span>`);
  }
  if (task.runners && task.runners.length) {
    meta.push(`<span>⬡ ${task.runners.map(r => `<span style="color:${runnerColor(r)}">${esc(cap(r))}</span>`).join(' · ')}</span>`);
  } else if (task.runner) {
    meta.push(`<span>⬡ ${cap(task.runner)}</span>`);
  }
  if (task.pid) {
    meta.push(`<span class="text-zinc-600">pid ${task.pid}</span>`);
  }
  document.getElementById('h-meta').innerHTML = meta.join('<span class="text-zinc-700">·</span>');
  disconnectLogSSE();
  disconnectSSE();
  activeDetailTab = 'overview';
  document.getElementById('overview-panel').innerHTML = '';
  setDetailTabUI('overview');
  loadOverview(task.id);
}

function renderMsgCard(m) {
  const r = ROLES[m.role] || {name:m.role,emoji:'',color:'#666',bg:'rgba(100,100,100,.05)'};
  const div = document.createElement('div');
  div.className = 'fade-in rounded-xl overflow-hidden mb-3';
  div.style.cssText = `border:1px solid ${r.color}15;background:linear-gradient(135deg,${r.bg},var(--glass));backdrop-filter:blur(8px)`;
  const header = document.createElement('div');
  header.className = 'flex items-center gap-2 px-4 py-2.5 cursor-pointer hover:brightness-125 transition-all sticky top-0 z-10';
  header.style.borderBottom = `1px solid ${r.color}10`;
  const lines = m.content.trim().split('\n').filter(l => l.trim());
  const preview = lines.length > 1 ? lines.find(l => !l.startsWith('#')) || lines[1] : '';
  const previewText = preview ? `<span class="text-xs text-zinc-600 truncate ml-2 flex-1">${esc(preview.slice(0,80))}</span>` : '';
  const emoji = r.emoji || '';
  const modelLabel = (m.model || 'Auto').replace(/^./, c => c.toUpperCase());
  const dur = m.duration > 0 ? `<span class="text-[10px] text-zinc-600 flex-shrink-0">${fmtSec(m.duration)}</span>` : '';
  const modelTag = `<span class="text-[10px] text-zinc-600 flex-shrink-0">${esc(modelLabel)}</span>`;
  header.innerHTML = `<span class="text-xs font-semibold flex-shrink-0" style="color:${r.color}">${emoji} ${r.name}</span><span class="text-xs text-zinc-600 flex-shrink-0">${m.label}${m.round?' · Round '+m.round:''}</span>${previewText}${modelTag}${dur}<span class="msg-chevron text-zinc-600 text-xs ml-auto flex-shrink-0">▶</span>`;
  const body = document.createElement('div');
  body.className = 'msg-body collapsed md-body px-4 py-3 overflow-y-auto';
  body.style.color = 'var(--text-2)';
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

async function loadMessages(id) {
  const el = document.getElementById('messages');
  if (activeDetailTab === 'messages') el.classList.remove('hidden');
  const msgs = await (await fetch(apiBase()+'/api/messages/'+id)).json();
  const list = msgs || [];
  if (list.length === lastMsgCount) return;
  if (list.length < lastMsgCount) { el.innerHTML = ''; lastMsgCount = 0; }
  const empty = el.querySelector('.msg-empty'); if (empty) empty.remove();
  const newMsgs = list.slice(lastMsgCount);
  newMsgs.forEach(m => el.appendChild(renderMsgCard(m)));
  lastMsgCount = list.length;
  if (list.length === 0) el.innerHTML = `<div class="msg-empty text-zinc-600 text-sm mt-8 text-center">${t('app.noArtifacts')}</div>`;
  if (newMsgs.length > 0) el.lastElementChild.scrollIntoView({ behavior: 'smooth', block: 'end' });
}

const overviewCache = {};
let activeDetailTab = 'overview';
let logSSE = null;
let currentLogFile = null;
// 未 pin 單檔時的多檔即時顯示：multiLogActive 標示處於「並列多個活躍 log」模式，
// multiLogBuffers 以 file 名為 key 累積各 log 內容，供 renderMultiLog 分區渲染。
let multiLogActive = false;
let multiLogBuffers = {};

function setDetailTabUI(tab) {
  document.querySelectorAll('.detail-tab').forEach(b => {
    const active = b.dataset.tab === tab;
    b.classList.toggle('border-transparent', !active);
    b.classList.toggle('text-zinc-500', !active);
    b.style.borderColor = active ? 'var(--accent)' : '';
    b.style.color = active ? 'var(--accent)' : '';
  });
  document.getElementById('overview-panel').classList.toggle('hidden', tab !== 'overview');
  document.getElementById('messages').classList.toggle('hidden', tab !== 'messages');
  const logsPanel = document.getElementById('logs-panel');
  if (tab === 'logs') { logsPanel.classList.remove('hidden'); logsPanel.style.display = 'flex'; }
  else { logsPanel.classList.add('hidden'); logsPanel.style.display = 'none'; }
}

async function loadOverview(fid) {
  const el = document.getElementById('overview-panel');
  if (activeDetailTab === 'overview') el.classList.remove('hidden');

  if (overviewCache[fid]) {
    renderOverview(overviewCache[fid], el);
    return;
  }

  el.innerHTML = `<div class="text-zinc-600 text-sm mt-8 text-center">${t('common.loading')}</div>`;
  try {
    const resp = await fetch(apiBase()+'/api/overview/'+fid);
    if (!resp.ok) {
      el.innerHTML = `<div class="text-red-400 text-sm mt-8 text-center">${t('overview.loadFailed')}</div>`;
      return;
    }
    const data = await resp.json();
    overviewCache[fid] = data;
    renderOverview(data, el);
  } catch {
    el.innerHTML = `<div class="text-red-400 text-sm mt-8 text-center">${t('picker.connectionError')}</div>`;
  }
}

function renderOverview(d, el) {
  let html = '';
  const card = (content) => `<div class="glass-card p-4 mb-4">${content}</div>`;
  const sectionTitle = (text) => `<div class="text-[10px] font-bold uppercase tracking-wider mb-3" style="color:var(--text-3)">${text}</div>`;

  if (d.description) {
    const safeDesc = d.description.replace(/</g, '&lt;').replace(/>/g, '&gt;');
    html += card(`${sectionTitle('Description')}<div class="md-body text-sm" style="color:var(--text-2)">${typeof marked !== 'undefined' ? marked.parse(safeDesc) : '<pre>'+esc(d.description)+'</pre>'}</div>`);
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
    html += card(`${sectionTitle('Feature Details')}<div class="space-y-2">${details.join('')}</div>`);
  }

  if (d.subtasks && d.subtasks.length) {
    const subtasks = d.subtasks.map(st => {
      const done = st.status === 'done';
      const icon = done ? '<span class="text-emerald-400">✓</span>' : '<span style="color:var(--text-4)">○</span>';
      const id = st.id ? `<span class="text-[11px]" style="color:${done?'var(--text-4)':'var(--text-3)'};${done?'text-decoration:line-through':''}">${esc(st.id)}</span>` : '';
      const nameStyle = done ? 'color:var(--text-4);text-decoration:line-through' : 'color:var(--text-2)';
      const desc = st.description ? `<div class="text-[11px] ml-5 mt-0.5" style="color:var(--text-3)">${esc(st.description)}</div>` : '';
      return `<div class="flex items-center gap-2">${icon} ${id} <span class="text-sm" style="${nameStyle}">${esc(st.name)}</span>${desc}</div>`;
    }).join('');
    html += card(`${sectionTitle('Subtasks')}<div class="space-y-1.5">${subtasks}</div>`);
  }

  if (d.spec) html += renderDocSection('Spec', d.specSource);
  if (d.plan) html += renderDocSection('Plan', d.planSource);

  el.innerHTML = html || `<div style="color:var(--text-4)" class="text-sm mt-8 text-center">No overview data</div>`;
}

function renderDocSection(title, source) {
  const id = 'doc-' + title.toLowerCase();
  const srcLine = source ? `<span class="text-[11px] truncate" style="color:var(--text-4)">${esc(source)}</span>` : '';
  return `<section class="mb-6"><button type="button" class="w-full flex items-center gap-2 text-left py-2 border-t" style="border-color:var(--border)" onclick="toggleDocSection('${id}')"><span class="text-xs font-bold uppercase tracking-wider" style="color:var(--text-3)">${esc(title)}</span>${srcLine}<span id="${id}-chevron" class="text-xs ml-auto" style="color:var(--text-4)">▶</span></button><div id="${id}-body" class="mt-2 md-body text-sm hidden" style="color:var(--text-2);max-height:70vh;overflow-y:auto"></div></section>`;
}

function toggleDocSection(id) {
  const body = document.getElementById(id + '-body');
  const chevron = document.getElementById(id + '-chevron');
  const opening = body.classList.contains('hidden');
  body.classList.toggle('hidden');
  chevron.textContent = opening ? '▼' : '▶';
  if (opening && !body.dataset.rendered) {
    const key = id.replace('doc-', '');
    const data = overviewCache[current];
    const content = data ? data[key] || '' : '';
    body.innerHTML = typeof marked !== 'undefined' ? marked.parse(content) : '<pre>' + esc(content) + '</pre>';
    body.dataset.rendered = '1';
  }
}

let _logsRefreshTimer = null;
function switchDetailTab(tab) {
  activeDetailTab = tab;
  setDetailTabUI(tab);
  if (tab !== 'messages') disconnectSSE();
  if (tab !== 'logs') { disconnectLogSSE(); currentLogFile = null; stopLogsRefresh(); }
  if (tab === 'overview' && current) loadOverview(current);
  if (tab === 'messages' && current) { connectSSE(current); loadMessages(current); }
  if (tab === 'logs' && current) { loadLogs(current); startLogsRefresh(current); }
}
function startLogsRefresh(fid) {
  stopLogsRefresh();
  _logsRefreshTimer = setInterval(() => { if (activeDetailTab === 'logs') loadLogs(fid); }, 10000);
}
function stopLogsRefresh() {
  if (_logsRefreshTimer) { clearInterval(_logsRefreshTimer); _logsRefreshTimer = null; }
}

async function loadLogs(fid) {
  const list = document.getElementById('logs-list');
  const viewer = document.getElementById('log-viewer');
  if (!currentLogFile && !multiLogActive) {
    viewer.textContent = '';
    viewer.classList.add('hidden');
  }
  const logs = await (await fetch(apiBase()+'/api/logs/'+fid)).json() || [];
  if (logs.length === 0) {
    list.innerHTML = `<div class="text-zinc-600 text-sm text-center mt-8">${t('logs.noLogs')}</div>`;
    if (!currentLogFile && !multiLogActive) viewer.classList.add('hidden');
    return;
  }
  list.innerHTML = logs.map(l => {
    const role = logBaseRole(l.name);
    const r = ROLES[role] || {name:role,color:'#666',bg:'rgba(100,100,100,.05)'};
    const kb = (l.size/1024).toFixed(1);
    const isLive = l.durationMs == null && l.startedAt;
    const dur = l.durationMs != null ? formatDuration(l.durationMs) : '';
    const durId = isLive ? `log-dur-${esc(l.name)}` : '';
    const active = (currentLogFile === l.name || (multiLogActive && multiLogBuffers[l.name] != null)) ? 'bg-zinc-800/50' : '';
    const durSpan = isLive
      ? `<span id="${durId}" class="text-[10px] text-emerald-400 ml-auto tabular-nums" data-started="${l.startedAt}"></span>`
      : dur ? `<span class="text-[10px] text-zinc-500 ml-auto">${dur}</span>` : '';
    return `<div class="flex items-center gap-3 px-3 py-2 rounded-lg cursor-pointer hover:bg-zinc-800/50 transition-colors ${active}" onclick="viewLog('${fid}','${escAttr(l.name)}')"><span class="text-xs font-semibold" style="color:${r.color}">${r.emoji||''} ${r.name}</span><span class="text-xs text-zinc-500">${esc(l.name)}</span>${durSpan}<span class="${!dur && !isLive ? 'ml-auto ' : ''}text-[10px] text-zinc-600">${kb}KB</span></div>`;
  }).join('');
  startLogDurationTimers();
  if (!logSSE) connectLogSSE(fid);
}

let _logDurTimer = null;
function startLogDurationTimers() {
  if (_logDurTimer) clearInterval(_logDurTimer);
  const tick = () => {
    document.querySelectorAll('[data-started]').forEach(el => {
      const ms = Date.now() - new Date(el.dataset.started).getTime();
      if (ms >= 0) el.textContent = formatDuration(ms);
    });
  };
  tick();
  _logDurTimer = setInterval(tick, 1000);
}

// logBaseRole 從 log 檔名取出 base role（去掉 round 前綴、副檔名與尾端迭代/索引號），
// 讓 deep-reviewer-1 / deep-fix-2 等都能對應到 ROLES map 的同一個顏色與名稱。
function logBaseRole(name) {
  return name.replace(/^round-\d+-/, '').replace(/\.log$/, '')
    .replace(/^(deep-(?:fix|reverify|reviewer))-\d+$/, '$1');
}

// renderMultiLog 把 multiLogBuffers 內所有活躍 log 分區並列渲染到 viewer，
// 每區一個帶 role 顏色的標題列，內容跟著該 log 即時累積。
function renderMultiLog() {
  const viewer = document.getElementById('log-viewer');
  const files = Object.keys(multiLogBuffers).sort();
  viewer.innerHTML = files.map(f => {
    const r = ROLES[logBaseRole(f)] || {name:f,color:'#888',emoji:''};
    return `<div class="text-[11px] font-semibold mt-3 mb-1 first:mt-0" style="color:${r.color}">▸ ${r.emoji||''} ${esc(r.name)} · ${esc(f)}</div><div>${esc(multiLogBuffers[f])}</div>`;
  }).join('');
  viewer.scrollTop = viewer.scrollHeight;
}

async function viewLog(fid, name) {
  currentLogFile = name;
  multiLogActive = false;
  const viewer = document.getElementById('log-viewer');
  viewer.classList.remove('hidden');
  const res = await fetch(apiBase()+'/api/logs/'+fid+'/'+name);
  viewer.textContent = await res.text();
  viewer.scrollTop = viewer.scrollHeight;
  connectLogSSE(fid, name);
}

function connectLogSSE(fid, file) {
  if (logSSE) { logSSE.close(); logSSE = null; }
  const multi = !file;
  multiLogActive = multi;
  if (multi) multiLogBuffers = {};
  const url = file ? sseBase()+'/logs/'+fid+'?file='+encodeURIComponent(file) : sseBase()+'/logs/'+fid;
  logSSE = new EventSource(url);
  const knownFiles = new Set();
  logSSE.onmessage = (e) => {
    const viewer = document.getElementById('log-viewer');
    try {
      const d = JSON.parse(e.data);
      if (!d.file) return;
      if (multi) {
        // 並列多檔模式：依 file 路由內容到各自 buffer，新檔出現時刷新左側列表。
        if (!knownFiles.has(d.file)) { knownFiles.add(d.file); loadLogs(fid); }
        viewer.classList.remove('hidden');
        multiLogBuffers[d.file] = (multiLogBuffers[d.file] || '') + d.content;
        renderMultiLog();
      } else {
        if (viewer.classList.contains('hidden')) return;
        if (currentLogFile && d.file !== currentLogFile) return;
        viewer.textContent += d.content;
        viewer.scrollTop = viewer.scrollHeight;
      }
    } catch(err) {}
  };
}

function disconnectLogSSE() {
  if (logSSE) { logSSE.close(); logSSE = null; }
  multiLogActive = false;
  multiLogBuffers = {};
}


function getRunId(featureId) {
  const run = activeRuns.find(r => r.featureId === featureId);
  return run ? run.id : null;
}

let _runModalFid = null;
let _runRounds = 5;
async function openRunModal(fid) {
  _runModalFid = fid;
  _runRounds = 5;
  document.getElementById('run-modal-fid').textContent = fid;
  document.getElementById('run-rounds-val').textContent = '5';
  document.getElementById('run-extra-prompt').value = '';
  const sel = document.getElementById('run-runner');
  sel.innerHTML = '';
  try {
    const s = await fetch(apiBase()+'/api/merged-config').then(r => r.json());
    const runners = s.runners || {};
    const names = Object.keys(runners);
    if (names.length === 0) names.push('claude');
    const def = s.default_runner || names[0];
    names.forEach(n => { const o = document.createElement('option'); o.value = n; o.textContent = cap(n); if (n === def) o.selected = true; sel.appendChild(o); });
  } catch { sel.innerHTML = '<option value="claude">Claude</option>'; }
  document.getElementById('run-modal').classList.add('open');
}
function closeRunModal() { document.getElementById('run-modal').classList.remove('open'); _runModalFid = null; }
function adjRunRounds(d) {
  _runRounds = Math.max(1, Math.min(99, _runRounds + d));
  document.getElementById('run-rounds-val').textContent = _runRounds;
}
async function submitRun() {
  if (!_runModalFid) return;
  const runner = document.getElementById('run-runner').value;
  const body = { featureId: _runModalFid, runner, maxRounds: _runRounds };
  closeRunModal();
  const res = await fetch(apiBase()+'/api/run', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
  if (!res.ok) { showToast(t('toast.runFailed').replace('{error}', await res.text())); return; }
  load();
}
async function stopRun(runId) {
  const res = await fetch(apiBase()+'/api/stop', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({id: runId}) });
  if (!res.ok) { showToast(t('toast.stopFailed').replace('{error}', await res.text())); return; }
  load();
}

function openNewFeature() {
  document.getElementById('new-feat-name').value = '';
  document.getElementById('new-feat-desc').value = '';
  document.getElementById('new-feature-modal').classList.add('open');
  setTimeout(() => document.getElementById('new-feat-name').focus(), 100);
}
function closeNewFeature() { document.getElementById('new-feature-modal').classList.remove('open'); }
async function submitNewFeature(andRun) {
  const name = document.getElementById('new-feat-name').value.trim();
  if (!name) return;
  const description = document.getElementById('new-feat-desc').value.trim();
  closeNewFeature();
  const res = await fetch(apiBase()+'/api/new', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({name, description}) });
  if (!res.ok) { showToast(t('toast.createFailed').replace('{error}', await res.text())); return; }
  const data = await res.json();
  await load();
  if (andRun && data.id) openRunModal(data.id);
}
