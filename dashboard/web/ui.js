const SR_COLORS = {'scope-change':'#f59e0b','runner-error':'#ef4444','hard-error':'#ef4444','soft-fail':'#f59e0b','interrupted':'#a78bfa','escalation':'#f59e0b','model-error':'#ef4444','guard-fail':'#ef4444','no-progress':'#f59e0b','missing-artifact':'#ef4444','health-check-failed':'#ef4444','scope-exceed':'#f59e0b','self-heal-exhausted':'#f59e0b'};
const PRIO_LABELS = {0:{l:'P0',c:'#f87171',bg:'rgba(248,113,113,.15)'},1:{l:'P1',c:'#fb923c',bg:'rgba(251,146,60,.12)'},2:{l:'P2',c:'#facc15',bg:'rgba(250,204,21,.10)'},3:{l:'P3',c:'#60a5fa',bg:'rgba(96,165,250,.10)'},4:{l:'P4',c:'#a1a1aa',bg:'rgba(161,161,170,.10)'},5:{l:'P5',c:'#71717a',bg:'rgba(113,113,122,.08)'}};

function renderTabs() {
  const el = document.getElementById('tabs');
  el.innerHTML = openTabs.map(tab => {
    const a = tab.id === activeProjectId;
    return `<div class="tab-item ${a?'active':''}" onclick="switchTab('${escAttr(tab.id)}')"><span>${esc(tab.name)}</span><span class="tab-close" onclick="event.stopPropagation();closeTab('${escAttr(tab.id)}')">&times;</span></div>`;
  }).join('');
}
function renderVersionInfo(info) {
  const el = document.getElementById('version-display');
  if (!el || !info || !info.version) return;
  let html = `<span class="version-tag">v${esc(info.version)}</span>`;
  if (info.updateAvailable && info.latest) {
    const href = info.releaseUrl ? ` href="${escAttr(info.releaseUrl)}" target="_blank" rel="noopener"` : '';
    html += `<a class="update-badge"${href}>v${esc(info.latest)}</a>`;
  }
  el.innerHTML = html;
}
function switchTab(pid) { activeProjectId=pid; current=null; sessionStorage.setItem('4x-current', ''); renderedMsgKeys.clear(); disconnectSSE(); saveTabState(); renderTabs(); goHome(); }
function closeTab(pid) {
  openTabs = openTabs.filter(tb => tb.id !== pid);
  if (activeProjectId === pid) { activeProjectId = openTabs.length > 0 ? openTabs[0].id : null; current = null; sessionStorage.setItem('4x-current', ''); disconnectSSE(); }
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
  const browseBtn = document.querySelector('[onclick="toggleBrowse()"]');
  if (window._isNativeApp && window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.nativeOpenFolder) {
    if (browseBtn) { browseBtn.onclick = function() { window.webkit.messageHandlers.nativeOpenFolder.postMessage('open'); }; }
  } else if (window.__TAURI__) {
    if (browseBtn) { browseBtn.onclick = async function() {
      try {
        const selected = await window.__TAURI__.dialog.open({ directory: true, multiple: false, title: 'Select a 4x project folder' });
        if (selected) { document.getElementById('path-input').value = selected; addProjectFromInput(); }
      } catch(e) { console.error('folder picker error', e); }
    }; }
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
  const input = document.getElementById('path-input'), errorEl = document.getElementById('path-error'), path = input.value.trim().replace(/^["']|["']$/g, '');
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
  document.getElementById('path-input').value = path;
  addProjectFromInput();
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
    const sep = data.current.includes('\\') ? '\\' : '/';
    const parts = data.current.split(sep).filter(Boolean);
    let parent;
    if (parts.length > 1) {
      const up = parts.slice(0, -1).join(sep);
      parent = sep === '\\' ? (up.endsWith(':') ? up + sep : up) : '/' + up;
    } else {
      parent = sep === '\\' ? parts[0] + sep : '/';
    }
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
  current = item.id; sessionStorage.setItem('4x-current', item.id); sessionStorage.removeItem('4x-detail-tab'); load(); loadDetail(item);
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
  else if (e.key==='?' && !e.metaKey && !e.ctrlKey && document.activeElement.tagName!=='INPUT' && document.activeElement.tagName!=='TEXTAREA') { e.preventDefault(); showShortcutsHelp(); }
  else if (e.key==='Escape') {
    const hm = document.getElementById('help-modal');
    if (hm && hm.classList.contains('open')) { hm.remove(); }
    else if (document.getElementById('global-settings-modal').classList.contains('open')) closeGlobalSettings();
    else if (document.getElementById('project-settings-modal').classList.contains('open')) closeProjectSettings();
    else if (document.getElementById('picker-modal').classList.contains('open')) closeProjectPicker();
    else if (document.getElementById('search-modal').classList.contains('open')) closeSearch();
  }
});

function showShortcutsHelp(initialTab) {
  if (document.getElementById('help-modal')) { document.getElementById('help-modal').remove(); return; }
  const kbd = (k) => `<kbd style="font-size:12px;padding:3px 8px;border-radius:6px;background:var(--bg-input);border:1px solid var(--border);color:var(--text-1);font-family:ui-monospace,monospace;min-width:36px;text-align:center">${esc(k)}</kbd>`;
  const row = (label, key) => `<div style="display:flex;justify-content:space-between;align-items:center;padding:8px 0;border-bottom:1px solid var(--border)"><span style="color:var(--text-2);font-size:13px">${esc(label)}</span>${kbd(key)}</div>`;
  const h2 = (text) => `<div style="font-size:13px;font-weight:700;color:var(--text-1);margin:16px 0 8px">${esc(text)}</div>`;
  const p = (text) => `<div style="font-size:13px;color:var(--text-2);line-height:1.6;margin:8px 0">${text}</div>`;

  const tabs = {
    overview: {
      label: t('help.overview') || 'Overview',
      icon: '◎',
      content: h2(t('help.whatIs4x') || 'What is 4x?')
        + p(t('help.whatIs4xDesc') || '4x is a multi-role AI development loop that orchestrates Design → Code → Review → Test phases. Like 4X strategy games, it conquers codebases through specialized roles.')
        + h2(t('help.workflow') || 'Workflow')
        + p('1. <b>Designer</b> — ' + (t('help.designerDesc') || 'reads spec/plan, produces task brief'))
        + p('2. <b>Coder</b> — ' + (t('help.coderDesc') || 'implements tasks following TDD'))
        + p('3. <b>Reviewer</b> — ' + (t('help.reviewerDesc') || 'reviews code for bugs and quality'))
        + p('4. <b>Tester</b> — ' + (t('help.testerDesc') || 'runs verification commands'))
        + p('5. <b>Deep Review</b> — ' + (t('help.deepReviewDesc') || 'final architectural review'))
        + h2(t('help.dashboard') || 'Dashboard')
        + p(t('help.dashboardDesc') || 'This dashboard shows real-time status of all features across projects. The sidebar lists features grouped by status; the main area shows overview, messages, and logs.')
    },
    cli: {
      label: 'CLI',
      icon: '⌘',
      content: h2(t('help.cliCommands') || 'CLI Commands')
        + p('<code>4x new</code> — ' + (t('help.cmdNew') || 'Create a new feature'))
        + p('<code>4x run</code> — ' + (t('help.cmdRun') || 'Run the Design→Code→Review→Test loop'))
        + p('<code>4x status</code> — ' + (t('help.cmdStatus') || 'Show feature status'))
        + p('<code>4x clean</code> — ' + (t('help.cmdClean') || 'Remove workspace artifacts for completed features'))
        + p('<code>4x live</code> — ' + (t('help.cmdLive') || 'Start the dashboard server'))
        + p('<code>4x check</code> — ' + (t('help.cmdCheck') || 'Run guardrail checks'))
        + p('<code>4x verify</code> — ' + (t('help.cmdVerify') || 'Run verify commands from test-strategy.yaml'))
        + p('<code>4x prompt</code> — ' + (t('help.cmdPrompt') || 'Generate a role prompt for the current phase'))
        + p('<code>4x doctor</code> — ' + (t('help.cmdDoctor') || 'Check settings and workspace health'))
    },
    shortcuts: {
      label: t('shortcuts.title') || 'Shortcuts',
      icon: '⌨',
      content: h2(t('help.navigation') || 'Navigation')
        + row(t('shortcuts.search') || 'Search', '⌘ K')
        + row(t('shortcuts.settings') || 'Settings', '⌘ ,')
        + row(t('shortcuts.globalSettings') || 'Global Settings', '⌘ ⇧ ,')
        + h2(t('help.view') || 'View')
        + row(t('shortcuts.reload') || 'Reload', '⌘ R')
        + row('Full Screen', '⌃ ⌘ F')
        + h2(t('help.general') || 'General')
        + row(t('help.helpGuide') || 'Help Guide', '?')
        + row(t('shortcuts.close') || 'Close dialog', 'Esc')
    },
    phases: {
      label: t('help.phases') || 'Phases',
      icon: '↻',
      content: h2(t('help.stateMachine') || 'State Machine')
        + p(t('help.stateMachineDesc') || 'Features progress through phases:')
        + p('<code>init → designing → coding → reviewing → testing → deep-reviewing → accepting → done</code>')
        + p(t('help.amendingDesc') || 'If review or testing finds issues, the feature goes back to <code>amending</code> then returns to <code>coding</code>.')
        + h2(t('help.terminalStates') || 'Terminal States')
        + p('<b>done</b> — ' + (t('help.doneDesc') || 'Feature completed successfully'))
        + p('<b>abandoned</b> — ' + (t('help.abandonedDesc') || 'Feature was abandoned'))
        + p('<b>blocked</b> — ' + (t('help.blockedDesc') || 'Feature is blocked, needs attention'))
    }
  };

  const tabKeys = Object.keys(tabs);
  let active = initialTab && tabs[initialTab] ? initialTab : 'overview';

  const overlay = document.createElement('div');
  overlay.id = 'help-modal';
  overlay.className = 'modal-backdrop open';
  const panel = document.createElement('div');
  panel.className = 'modal-panel fade-in';
  panel.style.cssText = 'width:560px;max-height:80vh;display:flex;flex-direction:column';

  function render() {
    panel.innerHTML = '<div style="padding:20px 24px 0;flex-shrink:0">'
      + '<div style="font-size:16px;font-weight:700;margin-bottom:12px">4x Guide</div>'
      + '<div style="display:flex;gap:0;border-bottom:1px solid var(--border)">'
      + tabKeys.map(k => {
        const isActive = k === active;
        return `<button data-tab="${k}" style="padding:8px 14px;font-size:12px;font-weight:600;border:none;background:none;cursor:pointer;color:${isActive?'var(--accent)':'var(--text-3)'};border-bottom:2px solid ${isActive?'var(--accent)':'transparent'};transition:all .15s">${tabs[k].icon} ${tabs[k].label}</button>`;
      }).join('')
      + '</div></div>'
      + '<div style="padding:8px 24px 24px;overflow-y:auto;flex:1">' + tabs[active].content + '</div>';
    panel.querySelectorAll('[data-tab]').forEach(btn => {
      btn.onmouseover = () => { if (btn.dataset.tab !== active) btn.style.color = 'var(--text-2)'; };
      btn.onmouseout = () => { if (btn.dataset.tab !== active) btn.style.color = 'var(--text-3)'; };
      btn.onclick = () => { active = btn.dataset.tab; render(); };
    });
  }

  render();
  overlay.appendChild(panel);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
  document.body.appendChild(overlay);
}

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
  current=null; sessionStorage.setItem('4x-current', ''); renderedMsgKeys.clear(); disconnectSSE(); disconnectLogSSE(); stopLogsRefresh();
  if (_logDurTimer) { clearInterval(_logDurTimer); _logDurTimer = null; }
  document.getElementById('header').classList.add('hidden');
  document.getElementById('overview-panel').classList.add('hidden');
  document.getElementById('messages').classList.add('hidden');
  document.getElementById('messages').innerHTML = '';
  document.getElementById('screenshots-panel').classList.add('hidden');
  document.getElementById('screenshots-panel').innerHTML = '';
  const lp = document.getElementById('logs-panel');
  lp.classList.add('hidden'); lp.style.display = 'none';
  const main = document.getElementById('main');
  main.style.overflowY = 'auto'; main.style.display = ''; main.style.flexDirection = '';
  document.getElementById('dashboard').classList.remove('hidden');
  activeDetailTab = 'overview';
  sessionStorage.removeItem('4x-detail-tab');
  if (activeProjectId) { load(); renderDashboard(lastTasks); } else renderProjectPicker();
}
// requestNotificationPermission 在支援 Web Notification 且尚未決定權限時請求授權；
// 不支援（WKWebView / 舊瀏覽器）或已決定時靜默 return。native bridge 環境不需此權限。
function requestNotificationPermission() {
  if (notifyHasNativeBridge()) return;
  if (typeof window.Notification === 'undefined') return;
  if (Notification.permission === 'default') {
    try { Notification.requestPermission(); } catch (e) {}
  }
}

// notifyHasNativeBridge 回報目前是否在 macOS native app 或 Tauri 容器中（具備原生通知 bridge）。
function notifyHasNativeBridge() {
  return !!(window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.nativeNotify) || !!window.__TAURI__;
}

// notifyOS 依執行環境分派通知：macOS native app 走 nativeNotify bridge、Tauri 走 invoke('notify')、
// 一般瀏覽器走 Web Notification API。各路徑在不支援 / 未授權時優雅降級，不丟錯。
function notifyOS(level, title, body) {
  try {
    if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.nativeNotify) {
      window.webkit.messageHandlers.nativeNotify.postMessage({ title: title, body: body, level: level });
      return;
    }
    if (window.__TAURI__) {
      // Tauri v2 全域為 __TAURI__.core.invoke；相容 v1 的 __TAURI__.tauri.invoke。
      const invoke = (window.__TAURI__.core && window.__TAURI__.core.invoke)
        || (window.__TAURI__.tauri && window.__TAURI__.tauri.invoke)
        || window.__TAURI__.invoke;
      if (invoke) { invoke('notify', { title: title, body: body }); return; }
    }
    if (typeof window.Notification !== 'undefined' && Notification.permission === 'granted') {
      new Notification(title, { body: body });
    }
  } catch (e) {}
}

// maybeNotifyFromEvents 取最新一筆 event，若帶有 notify 等級且尚未通知過，依文案推播 OS 通知。
async function maybeNotifyFromEvents(fid) {
  if (!notifyHasNativeBridge() && (typeof window.Notification === 'undefined' || Notification.permission !== 'granted')) return;
  try {
    const events = await (await fetch('/api/events/' + fid)).json();
    if (!Array.isArray(events) || events.length === 0) return;
    const ev = events[events.length - 1];
    if (!ev || !ev.notify) return;
    const key = fid + '|' + (ev.ts || '') + '|' + (ev.type || '');
    if (key === _lastNotifyKey) return;
    _lastNotifyKey = key;
    const title = '4x · ' + fid;
    const body = notifyBody(ev);
    notifyOS(ev.notify, title, body);
  } catch (e) {}
}

// notifyBody 依 event 型別組通知內文，走 i18n 文案並帶入 phase / detail 摘要。
function notifyBody(ev) {
  switch (ev.type) {
    case 'guard-fail': return t('notifications.guardFail');
    case 'escalation': return t('notifications.escalation');
    case 'run-end':
      if (ev.notify === 'success') return t('notifications.runDone');
      if (ev.status === 'interrupted') return t('notifications.runInterrupted');
      return t('notifications.runFailed');
    default: return t('notifications.runDone');
  }
}

function connectSSE(fid) { disconnectSSE(); _lastNotifyKey = null; requestNotificationPermission(); sseSource = new EventSource(sseBase()+'/events/'+fid); sseSource.onmessage = () => { loadMessages(fid); maybeNotifyFromEvents(fid); refreshCurrentDetail(); }; }
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
  sessionStorage.setItem('4x-current', fid);
  sessionStorage.removeItem('4x-detail-tab');
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

  const adj = {};
  involved.forEach(id => { adj[id] = []; });
  edges.forEach(([a, b]) => { adj[a].push(b); adj[b].push(a); });

  const visited = new Set();
  const components = [];
  involved.forEach(id => {
    if (visited.has(id)) return;
    const comp = [];
    const queue = [id];
    visited.add(id);
    while (queue.length) {
      const cur = queue.shift();
      comp.push(cur);
      adj[cur].forEach(nb => { if (!visited.has(nb)) { visited.add(nb); queue.push(nb); } });
    }
    components.push(comp);
  });
  components.sort((a, b) => a[0].localeCompare(b[0]));

  const NW = 200, NH = 42, GX = 32, GY = 78, COMP_GAP = 48, MAX_WIDTH = 5 * (NW + GX);
  const pos = {};

  const compMeta = components.map(comp => {
    const depth = {};
    function calcDepth(id, seen) {
      if (depth[id] != null) return depth[id];
      if (seen.has(id)) return 0;
      seen.add(id);
      let d = 0;
      (byId[id].depends || []).forEach(dep => { if (byId[dep] && comp.includes(dep)) d = Math.max(d, calcDepth(dep, seen) + 1); });
      seen.delete(id);
      depth[id] = d;
      return d;
    }
    comp.forEach(id => calcDepth(id, new Set()));

    const layers = {};
    comp.forEach(id => { const d = depth[id]; (layers[d] = layers[d] || []).push(byId[id]); });
    const layerKeys = Object.keys(layers).map(Number).sort((a, b) => a - b);
    layerKeys.forEach(k => layers[k].sort((a, b) => a.id.localeCompare(b.id)));

    let maxCols = 0;
    layerKeys.forEach(k => { maxCols = Math.max(maxCols, layers[k].length); });

    return { comp, layers, layerKeys, maxCols, w: maxCols * (NW + GX), h: layerKeys.length * GY + 16 };
  });

  let curX = GX, curY = 0, rowH = 0, totalW = 0;
  compMeta.forEach(cm => {
    if (curX > GX && curX + cm.w > MAX_WIDTH) {
      curY += rowH + COMP_GAP;
      curX = GX;
      rowH = 0;
    }
    cm.layerKeys.forEach((k, li) => {
      cm.layers[k].forEach((n, ci) => {
        pos[n.id] = { x: curX + ci * (NW + GX), y: curY + li * GY + 16 };
      });
    });
    totalW = Math.max(totalW, curX + cm.w);
    rowH = Math.max(rowH, cm.h);
    curX += cm.w + COMP_GAP;
  });

  const width = Math.max(totalW + GX, NW + GX * 2);
  const height = curY + rowH;

  let svgEdges = '';
  edges.forEach(([from, to]) => {
    const p = pos[from], c = pos[to];
    if (!p || !c) return;
    const x1 = p.x + NW / 2, y1 = p.y + NH, x2 = c.x + NW / 2, y2 = c.y;
    const my = (y1 + y2) / 2;
    svgEdges += `<path class="dag-edge" d="M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}" marker-end="url(#dag-arrow)"/>`;
  });

  let svgNodes = '';
  involved.forEach(id => {
    const n = byId[id], p = pos[id], col = dagNodeColor(n);
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

  return `<div class="dash-card mt-8 mb-4" id="dag-view"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-3">${t('dag.title')}</div>
    <div class="dag-scroll"><svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" xmlns="http://www.w3.org/2000/svg">
      <defs><marker id="dag-arrow" markerWidth="9" markerHeight="9" refX="7" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 Z" fill="var(--text-4)"/></marker></defs>
      ${svgEdges}${svgNodes}
    </svg></div></div>`;
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
  const doneCost = g.done.reduce((s, f) => s + (f.costUsd || 0), 0);
  const activeTab = openTabs.find(tb => tb.id === activeProjectId);
  const pName = activeTab ? activeTab.name : '4x Live';
  el.innerHTML = `<div class="flex items-center gap-3 mb-6"><span class="text-lg font-bold">${esc(pName)}</span><span class="ml-auto px-3 py-1 text-xs rounded-full" style="border:1px solid var(--border);color:var(--text-2)">${t('dashboard.tasks').replace('{count}', total)}</span></div>
<div class="grid grid-cols-5 gap-4 mb-8">
<div class="dash-card text-center"><div class="text-3xl font-bold text-emerald-400">${g.running.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.running')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-amber-400">${g.review.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.review')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-blue-400">${g.pending.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.pending')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-purple-400">${g.todo.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.todo')}</div></div>
<div class="dash-card text-center"><div class="text-3xl font-bold text-green-400">${g.done.length}</div><div class="text-xs dash-muted mt-1 uppercase tracking-wider">${t('sidebar.done')}</div></div></div>
<div class="grid grid-cols-2 gap-4 mb-8">
<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4">${t('dashboard.status')}</div><div class="flex items-center gap-6"><div class="relative w-28 h-28 flex-shrink-0"><div class="w-28 h-28 rounded-full" style="background:${donut}"></div><div class="absolute inset-3 rounded-full dash-donut-center flex items-center justify-center flex-col"><span class="text-xl font-bold">${total}</span><span class="text-[10px] dash-muted">${t('dashboard.total')}</span></div></div><div class="space-y-2 text-xs"><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>${t('sidebar.running')}<span class="ml-auto dash-sub font-bold">${g.running.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>${t('sidebar.review')}<span class="ml-auto dash-sub font-bold">${g.review.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-blue-500"></span>${t('sidebar.pending')}<span class="ml-auto dash-sub font-bold">${g.pending.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-purple-500"></span>${t('sidebar.todo')}<span class="ml-auto dash-sub font-bold">${g.todo.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-green-500"></span>${t('sidebar.done')}<span class="ml-auto dash-sub font-bold">${g.done.length}</span></div></div></div></div>
<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4">${t('dashboard.roundsDist')}</div><div class="space-y-3">${Object.entries(buckets).map(([l,c])=>{const p=maxB?(c/maxB)*100:0;const co={'1':'#10b981','2':'#3b82f6','3-4':'#f59e0b','5+':'#ef4444'};return `<div class="flex items-center gap-3"><span class="text-xs dash-muted w-8 text-right">${l}R</span><div class="flex-1 h-5 dash-bar-bg rounded overflow-hidden"><div class="h-full rounded" style="width:${p}%;background:${co[l]}"></div></div><span class="text-xs font-bold w-6 text-right" style="color:var(--text-1)">${c}</span></div>`;}).join('')}</div>${doneTasks.length>0?`<div class="text-[10px] dash-muted mt-3">${t('dashboard.avgRounds').replace('{avg}', (doneTasks.reduce((s,d)=>s+d.round,0)/doneTasks.length).toFixed(1))}</div>`:''}</div></div>
${recent.length>0?`<div class="dash-card"><div class="text-[10px] font-bold dash-muted uppercase tracking-wider mb-4 flex items-center justify-between"><span>${t('dashboard.recentCompletions')}</span>${doneCost>0?`<span class="normal-case font-semibold text-emerald-400/70">${t('dashboard.recentCompletionsCost').replace('{amount}', formatCost(doneCost))}</span>`:''}</div><div class="space-y-2">${recent.map(f=>{const pt=f.priority!=null&&PRIO_LABELS[f.priority]?`<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${PRIO_LABELS[f.priority].bg};color:${PRIO_LABELS[f.priority].c};font-weight:600">${PRIO_LABELS[f.priority].l}</span>`:'';const dur=f.createdAt&&f.updatedAt?formatDuration(f.createdAt,f.updatedAt):'';return `<div class="flex items-center gap-2 py-1.5 cursor-pointer rounded px-2 -mx-2 transition-colors" onmouseenter="this.style.background='var(--bg-hover)'" onmouseleave="this.style.background=''" onclick="openFeatureDetail('${f.id}')"><span class="text-emerald-500/60 text-xs">✓</span>${pt}<span class="text-xs font-semibold text-emerald-400/80">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span>${runnerTags(f.runners)}${dur?`<span class="text-[10px] dash-muted">⏱ ${dur}</span>`:''}${f.round?`<span class="text-[10px] dash-muted">${f.round}R</span>`:''}${f.costUsd>0?`<span class="text-[10px] dash-muted">${formatCost(f.costUsd)}</span>`:''}</div>`;}).join('')}</div></div>`:''}
${g.running.length>0?`<div class="rounded-xl border border-emerald-500/20 bg-emerald-950/20 p-5 mt-4"><div class="text-[10px] font-bold text-emerald-500/70 uppercase tracking-wider mb-4">${t('dashboard.currentlyRunning')}</div><div class="space-y-2">${g.running.map(f=>`<div class="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-emerald-900/20 rounded px-2 -mx-2 transition-colors" onclick="openFeatureDetail('${f.id}')"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span><span class="text-xs font-semibold text-emerald-400">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span><span class="text-[10px] text-emerald-400/70">${f.phase||''}</span>${f.round?`<span class="text-[10px] dash-muted">R${f.round}</span>`:''}</div>`).join('')}</div></div>`:''}
${g.review.length>0?`<div class="rounded-xl border border-amber-500/20 bg-amber-950/20 p-5 mt-4"><div class="text-[10px] font-bold text-amber-500/70 uppercase tracking-wider mb-4">${t('dashboard.pendingReview')}</div><div class="space-y-2">${g.review.map(f=>`<div class="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-amber-900/20 rounded px-2 -mx-2 transition-colors" onclick="openFeatureDetail('${f.id}')"><span class="text-amber-400 text-xs">⏳</span><span class="text-xs font-semibold text-amber-400">${f.id}</span><span class="text-xs dash-sub truncate flex-1">${esc(f.name)}</span>${f.round?`<span class="text-[10px] dash-muted">${f.round}R</span>`:''}<button class="px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors" onclick="event.stopPropagation();markDone('${f.id}')">${t('status.done')}</button></div>`).join('')}</div></div>`:''}
${renderDag(tasks)}`;
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
  const phaseRoleMap = {designing:'designer','design-reviewing':'design-reviewer',coding:'coder',reviewing:'reviewer','deep-reviewing':'deep-reviewer',testing:'tester',accepting:'acceptor',amending:'coder'};
  const roleInfo = ROLES[phaseRoleMap[task.phase]] || {};
  const parallelTester = isActive && task.phase === 'reviewing' ? ROLES['tester'] : null;
  const emoji = isActive && roleInfo.emoji ? roleInfo.emoji + ' ' : '';
  let pi = '';
  if (isActive) {
    const dotStyle = pc ? `background:${pc.color};box-shadow:0 0 4px ${pc.border}` : 'background:#34d399';
    const subPhaseSuffix = task.subPhase && task.phase === 'deep-reviewing' ? ` (${task.subPhase})` : '';
    const phaseText = parallelTester ? `${emoji}reviewing ${parallelTester.emoji} testing` : `${emoji}${task.phase}${subPhaseSuffix}`;
    pi = `<div class="flex items-center gap-1.5 mt-1.5 flex-wrap"><span class="w-1.5 h-1.5 rounded-full pulse-dot" style="${dotStyle}"></span><span class="text-[11px]" style="color:${pc?pc.color:'#34d399'}">${phaseText}</span><span class="text-[11px] text-zinc-600">${timePart}</span></div>`;
  } else if (hasState && (roundPart || timePart)) {
    const parts = [roundPart, timePart.replace(/^ · /, '')].filter(Boolean).join(' · ');
    pi = `<div class="flex items-center gap-1.5 mt-1.5"><span class="text-[11px] text-zinc-600">${parts}</span></div>`;
  }
  if (!isActive && task.stopReason && task.stopReason !== 'pending-review' && task.stopReason !== 'done') {
    const srColor = SR_COLORS[task.stopReason] || '#a1a1aa';
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
  const prioTag = task.priority != null && PRIO_LABELS[task.priority] ? `<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:${PRIO_LABELS[task.priority].bg};color:${PRIO_LABELS[task.priority].c};font-weight:600">${PRIO_LABELS[task.priority].l}</span>` : '';
  const profileTag = task.profile ? `<span style="font-size:9px;padding:1px 5px;border-radius:4px;background:rgba(45,212,191,.12);color:#2dd4bf;font-weight:600">${esc(task.profile)}</span>` : '';
  let docTags = '';
  if (task.status !== 'done') {
    const specTag = task.hasSpec ? '<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(139,92,246,.12);color:#a78bfa">spec</span>' : '';
    const planTag = task.hasPlan ? '<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(59,130,246,.12);color:#60a5fa">plan</span>' : '';
    docTags = specTag + planTag;
  }
  const depTag = (task.depends && task.depends.length && task.status !== 'done') ? task.depends.map(d => {const dt=lastTasks.find(t=>t.id===d||t.id.startsWith(d+'-'));const done=dt&&dt.status==='done';return done?`<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(16,185,129,.12);color:#34d399">✓ ${d}</span>`:`<span style="font-size:9px;padding:1px 4px;border-radius:4px;background:rgba(251,146,60,.10);color:#fb923c">→ ${d}</span>`;}).join('') : '';
  const tagsLine = (prioTag || profileTag || docTags || depTag) ? `<div class="flex gap-1 mt-1 flex-wrap">${prioTag}${profileTag}${docTags}${depTag}</div>` : '';
  return `<div class="${cls}" style="${cardStyle}" onclick="openFeatureDetail('${task.id}')" onmouseenter="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='1')" onmouseleave="this.querySelector('.play-btn')&&(this.querySelector('.play-btn').style.opacity='0')"><div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="flex items-center gap-2"><span class="text-[13px] font-medium truncate flex-1">${esc(task.name)}</span>${badge}</div><div class="text-[11px] text-zinc-600 mt-0.5">${task.id}</div>${pi}${tagsLine}${rtLine}</div>${doneBtn || actionBtn}</div></div>`;
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
  const [tasks, runs] = await Promise.all([
    fetch(apiBase()+'/api/tasks').then(r => r.json()).catch(() => []),
    fetch(apiBase()+'/api/runs').then(r => r.ok ? r.json() : []).catch(() => []),
  ]);
  lastTasks = tasks || [];
  activeRuns = runs || [];
  renderSidebar();
  if (!current) renderDashboard(lastTasks);
  else {
    const task = lastTasks.find(t => t.id === current);
    if (!task) { current = null; sessionStorage.setItem('4x-current', ''); renderDashboard(lastTasks); }
    else if (document.getElementById('header').classList.contains('hidden')) loadDetail(task);
    else updateDetailHeader(task);
  }
}

let _refreshPending = false;
async function refreshCurrentDetail() {
  if (!current || _refreshPending) return;
  _refreshPending = true;
  try {
    const [tasks, runs] = await Promise.all([
      fetch(apiBase()+'/api/tasks').then(r => r.json()).catch(() => []),
      fetch(apiBase()+'/api/runs').then(r => r.ok ? r.json() : []).catch(() => []),
    ]);
    lastTasks = tasks || [];
    activeRuns = runs || [];
    renderSidebar();
    const task = lastTasks.find(t => t.id === current);
    if (task) updateDetailHeader(task);
  } finally { _refreshPending = false; }
}

function updateDetailHeader(task) {
  const el = (id) => document.getElementById(id);
  el('h-badge').innerHTML = badge(task.status, task.phase, task.active);
  const isRunning = task.active && task.phase && task.phase !== 'done';
  const hRunId = getRunId(task.id);
  let hAction = '';
  if (isRunning && hRunId) {
    hAction = `<button class="w-7 h-7 flex items-center justify-center rounded text-red-400 hover:bg-red-500/20 transition-colors" onclick="stopRun('${hRunId}')" title="${t('run.stop')}">■</button>`;
  } else if (task.status !== 'done') {
    hAction = `<button class="w-7 h-7 flex items-center justify-center rounded hover:bg-emerald-500/20 transition-colors" style="color:var(--accent)" onclick="openRunModal('${task.id}')" title="${t('run.run')}">▶</button>`;
  }
  el('h-play-stop').innerHTML = hAction;
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
  if (task.pid) meta.push(`<span class="text-zinc-600">pid ${task.pid}</span>`);
  el('h-meta').innerHTML = meta.join('<span class="text-zinc-700">·</span>');
  const existingAlert = document.getElementById('h-stop-alert');
  if (existingAlert) existingAlert.remove();
  if (!isRunning && task.stopReason && task.stopReason !== 'pending-review' && task.stopReason !== 'done') {
    const c = SR_COLORS[task.stopReason] || '#ef4444';
    const msg = task.stopMessage || task.stopReason;
    const alert = document.createElement('div');
    alert.id = 'h-stop-alert';
    alert.style.cssText = `margin-top:8px;padding:8px 12px;border-radius:8px;font-size:12px;line-height:1.5;background:${c}12;border:1px solid ${c}30;color:${c}`;
    alert.innerHTML = `<strong style="font-size:11px;text-transform:uppercase;letter-spacing:.05em">⚠ ${esc(task.stopReason)}</strong>` + (msg !== task.stopReason ? `<div style="margin-top:4px;color:var(--text-2);font-size:12px">${esc(msg)}</div>` : '');
    document.getElementById('header').appendChild(alert);
  }
}

async function loadDetail(task) {
  document.getElementById('dashboard').classList.add('hidden');
  document.getElementById('header').classList.remove('hidden');
  document.getElementById('messages').innerHTML = ''; renderedMsgKeys.clear(); currentLogFile = null; multiLogActive = false; multiLogBuffers = {};
  document.getElementById('screenshots-panel').innerHTML = ''; _lastScreenshotHash = '';
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

  const existingAlert = document.getElementById('h-stop-alert');
  if (existingAlert) existingAlert.remove();
  if (!isRunning && task.stopReason && task.stopReason !== 'pending-review' && task.stopReason !== 'done') {
    const c = SR_COLORS[task.stopReason] || '#ef4444';
    const msg = task.stopMessage || task.stopReason;
    const alert = document.createElement('div');
    alert.id = 'h-stop-alert';
    alert.style.cssText = `margin-top:8px;padding:8px 12px;border-radius:8px;font-size:12px;line-height:1.5;background:${c}12;border:1px solid ${c}30;color:${c}`;
    alert.innerHTML = `<strong style="font-size:11px;text-transform:uppercase;letter-spacing:.05em">⚠ ${esc(task.stopReason)}</strong>` + (msg !== task.stopReason ? `<div style="margin-top:4px;color:var(--text-2);font-size:12px">${esc(msg)}</div>` : '');
    document.getElementById('header').appendChild(alert);
  }

  disconnectLogSSE();
  disconnectSSE();
  const savedTab = sessionStorage.getItem('4x-detail-tab') || 'overview';
  document.getElementById('overview-panel').innerHTML = '';
  switchDetailTab(savedTab);
}

// formatCost 統一金額顯示格式，供訊息卡片、header 總花費、dashboard 最近完成列表共用。
function formatCost(amount) {
  return amount > 0 ? '$' + amount.toFixed(4) : '';
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
  const costTag = m.costUsd > 0
    ? `<span class="text-[10px] text-zinc-600 flex-shrink-0">${formatCost(m.costUsd)}</span>`
    : m.tokensUsed > 0
      ? `<span class="text-[10px] text-zinc-600 flex-shrink-0">${fmtTokens(m.tokensUsed)}</span>`
      : '';
  header.innerHTML = `<span class="text-xs font-semibold flex-shrink-0" style="color:${r.color}">${emoji} ${r.name}</span><span class="text-xs text-zinc-600 flex-shrink-0">${m.label}${m.round?' · Round '+m.round:''}</span>${previewText}${modelTag}${dur}${costTag}<span class="msg-chevron text-zinc-600 text-xs ml-auto flex-shrink-0">▶</span>`;
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

// renderHeaderTotalCost 把總花費顯示在 detail-tabs 列最右邊，不論目前在哪個分頁
// （總覽／訊息／日誌／截圖）都看得到，跟訊息頁內的 renderTotalCost 共用同一個
// totalCostUSD 數值，避免兩處各自定義格式造成不一致。
function renderHeaderTotalCost(totalCostUSD) {
  const el = document.getElementById('detail-total-cost');
  if (!el) return;
  el.textContent = totalCostUSD > 0 ? t('app.totalCost').replace('{amount}', formatCost(totalCostUSD)) : '';
}

// loadTotalCost 供非訊息分頁（總覽／日誌／截圖）獨立取得總花費，訊息分頁本身已在
// loadMessages 內順便拿到這個值，不需要重複呼叫。
async function loadTotalCost(id) {
  try {
    const data = await (await fetch(apiBase()+'/api/messages/'+id)).json();
    renderHeaderTotalCost(data.totalCostUSD || 0);
  } catch {}
}

async function loadMessages(id) {
  const el = document.getElementById('messages');
  if (activeDetailTab === 'messages') el.classList.remove('hidden');
  const data = await (await fetch(apiBase()+'/api/messages/'+id)).json();
  const list = data.messages || [];
  const totalCostUSD = data.totalCostUSD || 0;
  renderHeaderTotalCost(totalCostUSD);
  if (list.length === 0) {
    el.querySelectorAll('[data-msg-key]').forEach(node => node.remove());
    renderedMsgKeys.clear();
    if (!el.querySelector('.msg-empty')) {
      const emptyEl = document.createElement('div');
      emptyEl.className = 'msg-empty text-zinc-600 text-sm mt-8 text-center';
      emptyEl.textContent = t('app.noArtifacts');
      el.appendChild(emptyEl);
    }
    return;
  }
  const empty = el.querySelector('.msg-empty'); if (empty) empty.remove();
  let added = false;
  list.forEach(m => {
    const key = m.file || m.label;
    const hash = key + ':' + m.content.length + ':' + m.content.slice(0, 200);
    const prev = renderedMsgKeys.get(key);
    if (prev === hash) return;
    const card = renderMsgCard(m);
    card.dataset.msgKey = key;
    if (prev) {
      const old = el.querySelector(`[data-msg-key="${CSS.escape(key)}"]`);
      if (old) old.replaceWith(card);
    } else {
      el.appendChild(card);
      added = true;
    }
    renderedMsgKeys.set(key, hash);
  });
  if (added) el.lastElementChild.scrollIntoView({ behavior: 'smooth', block: 'end' });
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
  document.getElementById('screenshots-panel').classList.toggle('hidden', tab !== 'screenshots');
  const logsPanel = document.getElementById('logs-panel');
  const main = document.getElementById('main');
  if (tab === 'logs') {
    logsPanel.classList.remove('hidden'); logsPanel.style.display = 'flex';
    main.style.overflowY = 'hidden'; main.style.display = 'flex'; main.style.flexDirection = 'column';
  } else {
    logsPanel.classList.add('hidden'); logsPanel.style.display = 'none';
    main.style.overflowY = 'auto'; main.style.display = ''; main.style.flexDirection = '';
  }
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
    const depends = d.depends.map(dep => {
      const dt = lastTasks.find(t => t.id === dep);
      const done = dt && dt.status === 'done';
      return done
        ? `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:rgba(16,185,129,.12);color:#34d399;border:1px solid rgba(16,185,129,.18)">✓ ${esc(dep)}</span>`
        : `<span class="inline-block px-2 py-0.5 rounded text-[11px]" style="background:rgba(251,146,60,.10);color:#fb923c;border:1px solid rgba(251,146,60,.15)">${esc(dep)}</span>`;
    }).join(' ');
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
  sessionStorage.setItem('4x-detail-tab', tab);
  setDetailTabUI(tab);
  if (tab !== 'messages') disconnectSSE();
  if (tab !== 'logs') { disconnectLogSSE(); currentLogFile = null; stopLogsRefresh(); }
  if (tab === 'overview' && current) loadOverview(current);
  if (tab === 'messages' && current) { connectSSE(current); loadMessages(current); }
  if (tab === 'logs' && current) { loadLogs(current); startLogsRefresh(current); }
  if (tab === 'screenshots' && current) loadScreenshots(current);
  if (tab !== 'messages' && current) loadTotalCost(current);
}

let _lastScreenshotHash = '';
async function loadScreenshots(fid) {
  const el = document.getElementById('screenshots-panel');
  const isEmpty = !el.innerHTML || el.innerHTML.includes('text-center');
  if (isEmpty) el.innerHTML = `<div class="text-zinc-600 text-sm mt-8 text-center">${t('common.loading')}</div>`;
  try {
    const resp = await fetch(apiBase()+'/api/features/'+fid+'/screenshots');
    if (!resp.ok) { _lastScreenshotHash = ''; el.innerHTML = `<div class="text-zinc-600 text-sm mt-8 text-center">${t('screenshots.none')}</div>`; return; }
    const data = await resp.json();
    if (!data.groups || data.groups.length === 0) { _lastScreenshotHash = ''; el.innerHTML = `<div class="text-zinc-600 text-sm mt-8 text-center">${t('screenshots.none')}</div>`; return; }
    const hash = JSON.stringify(data.groups);
    if (hash === _lastScreenshotHash) return;
    _lastScreenshotHash = hash;
    renderScreenshots(data.groups, el);
  } catch {
    el.innerHTML = `<div class="text-red-400 text-sm mt-8 text-center">${t('picker.connectionError')}</div>`;
  }
}

let _lbItems = [];
function renderScreenshots(groups, el) {
  let html = '';
  _lbItems = [];
  groups.sort((a, b) => b.round - a.round);
  groups.forEach(g => {
    const title = t('screenshots.round').replace('{round}', g.round);
    html += `<div class="mb-6"><div class="text-[10px] font-bold uppercase tracking-wider mb-3" style="color:var(--text-3)">${esc(title)}</div>`;
    html += '<div class="grid gap-3" style="grid-template-columns:repeat(auto-fill,minmax(240px,1fr))">';
    g.screenshots.forEach(s => {
      const desc = s.description || s.filename;
      const imgUrl = apiBase() + s.url;
      const idx = _lbItems.length;
      _lbItems.push({url: imgUrl, title: desc});
      html += `<div class="rounded-lg overflow-hidden cursor-pointer transition-transform hover:scale-[1.02]" style="background:var(--bg-2);border:1px solid var(--border)" onclick="openLightbox(${idx})">`;
      html += `<img src="${escAttr(imgUrl)}" alt="${escAttr(desc)}" loading="lazy" style="width:100%;display:block;max-height:200px;object-fit:cover">`;
      html += `<div class="px-3 py-2"><div class="text-[11px] truncate" style="color:var(--text-2)">${esc(desc)}</div>`;
      if (s.step) html += `<div class="text-[10px]" style="color:var(--text-4)">Step ${esc(s.step)}</div>`;
      html += '</div></div>';
    });
    html += '</div></div>';
  });
  el.innerHTML = html;
}

function openLightbox(idx) {
  let cur = idx;
  const overlay = document.createElement('div');
  overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,.85);z-index:9999;display:flex;align-items:center;justify-content:center;backdrop-filter:blur(4px)';
  const img = document.createElement('img');
  img.style.cssText = 'max-width:90vw;max-height:90vh;border-radius:8px;box-shadow:0 8px 32px rgba(0,0,0,.5);cursor:zoom-out';
  const cap = document.createElement('div');
  cap.style.cssText = 'position:fixed;bottom:24px;left:50%;transform:translateX(-50%);color:#fff;font-size:13px;background:rgba(0,0,0,.6);padding:6px 16px;border-radius:8px;pointer-events:none';
  const counter = document.createElement('div');
  counter.style.cssText = 'position:fixed;top:20px;right:24px;color:rgba(255,255,255,.5);font-size:12px;pointer-events:none';
  const btnStyle = 'position:fixed;top:50%;transform:translateY(-50%);background:rgba(255,255,255,.1);border:none;color:#fff;font-size:28px;width:44px;height:44px;border-radius:50%;cursor:pointer;display:flex;align-items:center;justify-content:center;transition:background .15s';
  const prevBtn = document.createElement('button');
  prevBtn.style.cssText = btnStyle + ';left:16px';
  prevBtn.innerHTML = '&#8249;';
  prevBtn.onmouseenter = () => prevBtn.style.background = 'rgba(255,255,255,.25)';
  prevBtn.onmouseleave = () => prevBtn.style.background = 'rgba(255,255,255,.1)';
  const nextBtn = document.createElement('button');
  nextBtn.style.cssText = btnStyle + ';right:16px';
  nextBtn.innerHTML = '&#8250;';
  nextBtn.onmouseenter = () => nextBtn.style.background = 'rgba(255,255,255,.25)';
  nextBtn.onmouseleave = () => nextBtn.style.background = 'rgba(255,255,255,.1)';

  function show() {
    const item = _lbItems[cur];
    img.src = item.url; img.alt = item.title;
    cap.textContent = item.title;
    counter.textContent = `${cur + 1} / ${_lbItems.length}`;
    prevBtn.style.visibility = cur > 0 ? 'visible' : 'hidden';
    nextBtn.style.visibility = cur < _lbItems.length - 1 ? 'visible' : 'hidden';
  }
  function nav(delta) { cur = Math.max(0, Math.min(_lbItems.length - 1, cur + delta)); show(); }
  function close() { overlay.remove(); document.removeEventListener('keydown', onKey); }

  prevBtn.onclick = (e) => { e.stopPropagation(); nav(-1); };
  nextBtn.onclick = (e) => { e.stopPropagation(); nav(1); };
  img.onclick = (e) => { e.stopPropagation(); close(); };
  overlay.onclick = close;

  function onKey(e) {
    if (e.key === 'Escape') close();
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') nav(-1);
    else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') nav(1);
  }

  overlay.append(img, cap, counter, prevBtn, nextBtn);
  document.body.appendChild(overlay);
  document.addEventListener('keydown', onKey);
  show();
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
// 讓 designer-2 / deep-reviewer-1 / deep-fix-2 等都能對應到 ROLES map 的同一個顏色與名稱。
function logBaseRole(name) {
  return name.replace(/^round-\d+-/, '').replace(/\.log$/, '')
    .replace(/-\d+$/, '');
}

let _renderMultiLogRAF = null;
function scheduleRenderMultiLog() {
  if (_renderMultiLogRAF) return;
  _renderMultiLogRAF = requestAnimationFrame(() => { _renderMultiLogRAF = null; renderMultiLog(); });
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
        scheduleRenderMultiLog();
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
let _runOverrides = {};       // phase -> {runner, model}（本次 run 的臨時覆寫）
let _runMergedConfig = null;  // 快取 merged-config 供 pipeline 渲染（runners + tiers）
async function openRunModal(fid) {
  _runModalFid = fid;
  _runRounds = 5;
  _runOverrides = {};
  document.getElementById('run-modal-fid').textContent = fid;
  document.getElementById('run-rounds-val').textContent = '5';
  document.getElementById('run-extra-prompt').value = '';
  const sel = document.getElementById('run-profile');
  sel.innerHTML = '';
  try {
    const s = await fetch(apiBase()+'/api/merged-config').then(r => r.json());
    _runMergedConfig = s;
    const profiles = s.profiles || {};
    const names = Object.keys(profiles);
    // 內建 profile 永遠可選（即使 profiles 區段為空）。
    BUILTIN_PROFILES.forEach(n => { if (!names.includes(n)) names.push(n); });
    const featProfile = (lastTasks.find(t => t.id === fid) || {}).profile;
    const def = featProfile || s.default_profile || names[0];
    names.forEach(n => { const o = document.createElement('option'); o.value = n; o.textContent = cap(n); if (n === def) o.selected = true; sel.appendChild(o); });
  } catch { _runMergedConfig = null; sel.innerHTML = '<option value="full">Full</option>'; }
  document.getElementById('run-modal').classList.add('open');
  renderRunPipeline();
}
function closeRunModal() { document.getElementById('run-modal').classList.remove('open'); _runModalFid = null; }
function adjRunRounds(d) {
  _runRounds = Math.max(1, Math.min(99, _runRounds + d));
  document.getElementById('run-rounds-val').textContent = _runRounds;
}

// onRunProfileChange — 切換 profile 後清掉與新 profile 無關（被停用）的 override，再重繪。
async function onRunProfileChange() { renderRunPipeline(); }

// renderRunPipeline — 以目前 profile + _runOverrides 打 /api/run/preview，渲染每個 phase
// 一列：phase 名、解析後 runner（帶色）、解析後 model，並附 per-phase 覆寫控制。
async function renderRunPipeline() {
  const container = document.getElementById('run-pipeline-preview');
  if (!container || !_runModalFid) return;
  const profile = document.getElementById('run-profile').value;
  container.innerHTML = `<div style="font-size:12px;color:var(--text-4)">${esc(t('run.pipelineLoading'))}</div>`;
  let pipeline;
  try {
    const overrides = Object.entries(_runOverrides).map(([phase, ov]) => ({ phase, runner: ov.runner || '', model: ov.model || '' }));
    const res = await fetch(apiBase()+'/api/run/preview', {
      method: 'POST', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ featureId: _runModalFid, profile, overrides }),
    });
    if (!res.ok) throw new Error(await res.text());
    pipeline = await res.json();
  } catch (e) {
    container.innerHTML = `<div style="font-size:12px;color:var(--danger,#ef4444)">${esc(t('run.pipelineError'))}: ${esc(String(e.message||e))}</div>`;
    return;
  }
  // 切換 profile 後，丟掉已不在 pipeline 中（被停用）的 phase 覆寫。
  const activePhases = new Set(pipeline.map(p => p.phase));
  Object.keys(_runOverrides).forEach(ph => { if (!activePhases.has(ph)) delete _runOverrides[ph]; });

  const runners = (_runMergedConfig && _runMergedConfig.runners) || {};
  const runnerNames = Object.keys(runners);
  container.innerHTML = pipeline.map(p => {
    const ov = _runOverrides[p.phase] || {};
    const tiers = (runners[p.runner] && runners[p.runner].tiers) ? Object.keys(runners[p.runner].tiers) : [];
    const runnerOpts = `<option value="">${esc(t('run.phaseDefault'))}</option>` +
      runnerNames.map(rn => `<option value="${escAttr(rn)}"${ov.runner === rn ? ' selected' : ''}>${esc(cap(rn))}</option>`).join('');
    const modelOpts = `<option value="">${esc(t('run.phaseDefault'))}</option>` +
      tiers.map(tr => `<option value="${escAttr(tr)}"${ov.model === tr ? ' selected' : ''}>${esc(tr)}</option>`).join('');
    const selStyle = 'flex:1;min-width:0;padding:4px 6px;font-size:11px;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;color:var(--text-1);font-family:inherit;outline:none';
    const pc = PHASE_COLORS[p.phase] || {color:'var(--text-1)',bg:'var(--bg-input)',border:'var(--border)'};
    return `<div style="border:1px solid ${pc.border};border-radius:8px;padding:8px 10px;background:${pc.bg}">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
        <span style="font-size:12px;font-weight:600;color:${pc.color};text-transform:capitalize;flex:1">${esc(p.phase)}</span>
        <span class="runner-tag" style="border-color:${runnerColor(p.runner)}40;color:${runnerColor(p.runner)}">${esc(cap(p.runner))}</span>
        <span style="font-size:11px;color:var(--text-3)">${esc(p.model)}</span>
      </div>
      <div style="display:flex;gap:6px">
        <select onchange="setRunOverride('${escAttr(p.phase)}','runner',this.value)" style="${selStyle}">${runnerOpts}</select>
        <select onchange="setRunOverride('${escAttr(p.phase)}','model',this.value)" style="${selStyle}">${modelOpts}</select>
      </div>
    </div>`;
  }).join('');
}

// setRunOverride — 寫入／清除某 phase 的 runner 或 model 臨時覆寫，再重繪 pipeline。
function setRunOverride(phase, dim, value) {
  const ov = _runOverrides[phase] || {};
  if (value) ov[dim] = value; else delete ov[dim];
  if (ov.runner || ov.model) _runOverrides[phase] = ov; else delete _runOverrides[phase];
  renderRunPipeline();
}

async function submitRun() {
  if (!_runModalFid) return;
  const profile = document.getElementById('run-profile').value;
  const overrides = Object.entries(_runOverrides).map(([phase, ov]) => ({ phase, runner: ov.runner || '', model: ov.model || '' }));
  const body = { featureId: _runModalFid, profile, overrides, maxRounds: _runRounds };
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
  document.getElementById('new-feat-priority').value = '';
  document.getElementById('new-feat-custom-id').value = '';
  document.getElementById('new-feat-depends').value = '';
  document.getElementById('new-feat-rules').value = '';
  document.getElementById('new-feat-subtasks-list').innerHTML = '';
  document.getElementById('new-feat-advanced').hidden = true;
  document.getElementById('new-feat-adv-arrow').textContent = '▶';
  document.getElementById('new-feature-modal').classList.add('open');
  setTimeout(() => document.getElementById('new-feat-name').focus(), 100);
}
function closeNewFeature() { document.getElementById('new-feature-modal').classList.remove('open'); }
function toggleAdvanced() {
  const el = document.getElementById('new-feat-advanced');
  const isHidden = el.style.display === 'none';
  el.style.display = isHidden ? 'flex' : 'none';
  document.getElementById('new-feat-adv-arrow').textContent = isHidden ? '▼' : '▶';
}
function addSubtaskRow() {
  const list = document.getElementById('new-feat-subtasks-list');
  const row = document.createElement('div');
  row.style.cssText = 'display:flex;gap:6px;margin-bottom:4px;align-items:center';
  const inputStyle = 'background:var(--bg-input);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box';
  row.innerHTML = `
    <input type="text" placeholder="id" class="st-id" style="width:80px;${inputStyle}">
    <input type="text" placeholder="name" class="st-name" style="flex:1;${inputStyle}">
    <button type="button" onclick="this.parentElement.remove()" style="background:none;border:none;color:var(--text-3);cursor:pointer;font-size:14px;padding:2px 6px">✕</button>`;
  list.appendChild(row);
}
async function submitNewFeature(andRun) {
  const name = document.getElementById('new-feat-name').value.trim();
  if (!name) return;
  const description = document.getElementById('new-feat-desc').value.trim();
  const priorityVal = document.getElementById('new-feat-priority').value;
  const customId = document.getElementById('new-feat-custom-id').value.trim();
  const dependsRaw = document.getElementById('new-feat-depends').value.trim();
  const rulesRaw = document.getElementById('new-feat-rules').value.trim();

  const body = { name, description };
  if (priorityVal !== '') body.priority = parseInt(priorityVal, 10);
  if (customId) body.customId = customId;
  if (dependsRaw) body.depends = dependsRaw.split(',').map(s => s.trim()).filter(Boolean);
  if (rulesRaw) body.rules = rulesRaw.split(',').map(s => s.trim()).filter(Boolean);

  const subtaskRows = document.querySelectorAll('#new-feat-subtasks-list > div');
  if (subtaskRows.length > 0) {
    const subtasks = [];
    subtaskRows.forEach(row => {
      const id = row.querySelector('.st-id').value.trim();
      const stName = row.querySelector('.st-name').value.trim();
      if (id && stName) subtasks.push({ id, name: stName, status: 'pending' });
    });
    if (subtasks.length > 0) body.subtasks = subtasks;
  }

  closeNewFeature();
  const res = await fetch(apiBase()+'/api/new', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
  if (!res.ok) { showToast(t('toast.createFailed').replace('{error}', await res.text())); return; }
  const data = await res.json();
  await load();
  if (andRun && data.id) openRunModal(data.id);
}

async function openCleanDialog() {
  const base = apiBase();
  const title = t('clean.title');
  const msg = t('clean.warning');

  const overlay = document.createElement('div');
  overlay.className = 'modal-backdrop open';
  const dialog = document.createElement('div');
  dialog.className = 'modal-panel fade-in';
  dialog.style.cssText = 'width:420px';
  dialog.innerHTML = `
    <div style="padding:20px 24px 12px">
      <div style="font-size:15px;font-weight:700;margin-bottom:8px">${esc(title)}</div>
      <div style="font-size:13px;color:var(--text-2);line-height:1.5">⚠ ${esc(msg)}</div>
    </div>
    <div style="padding:12px 24px 20px;display:flex;justify-content:flex-end;gap:8px">
      <button id="clean-cancel-btn" style="padding:8px 16px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;font-size:13px;color:var(--text-2);cursor:pointer">${t('common.cancel')}</button>
      <button id="clean-confirm-btn" style="padding:8px 16px;background:#dc2626;color:#fff;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer">${t('common.confirm')}</button>
    </div>`;
  overlay.appendChild(dialog);
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
  dialog.querySelector('#clean-cancel-btn').onclick = close;
  dialog.querySelector('#clean-confirm-btn').onclick = async function() {
    this.disabled = true;
    this.textContent = '...';
    try {
      const resp = await fetch(base + '/api/clean', { method: 'POST' });
      const data = await resp.json();
      close();
      if (data.cleaned > 0) {
        showToast(t('clean.success').replace('{count}', data.cleaned).replace('{size}', data.freed_human), 'info');
      } else {
        showToast(t('clean.nothingToClean'), 'info');
      }
      load();
    } catch (e) {
      showToast(t('toast.failed').replace('{error}', e.message));
      close();
    }
  };
}
