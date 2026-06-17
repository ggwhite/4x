function applyTheme(id) {
  settings.theme = id;
  document.documentElement.setAttribute('data-theme', id === 'apple-dark' ? '' : id);
  saveSettings(); renderThemeGrid();
}
function renderThemeGrid() {
  const el = document.getElementById('gs-theme-grid');
  if (!el) return;
  el.innerHTML = THEMES.map(th => `<div style="text-align:center"><div class="theme-card ${settings.theme===th.id?'selected':''}" onclick="applyTheme('${th.id}')" style="background:${th.bg}"><div class="dots"><span style="background:#ff5f57"></span><span style="background:#febc2e"></span><span style="background:#28c840"></span></div><div class="lines"><span style="background:${th.line};width:70%"></span><span style="background:${th.line};width:50%"></span><span style="background:${th.line};width:60%"></span></div></div><div style="font-size:11px;color:var(--text-3);margin-top:6px">${th.name}</div></div>`).join('');
}
function applyFont() {
  document.documentElement.style.setProperty('--font-content', settings.fontContent+'px');
  document.documentElement.style.setProperty('--font-code', settings.fontCode+'px');
}
function adjFont(type, delta) {
  if (type==='content') settings.fontContent = Math.min(24, Math.max(12, settings.fontContent+delta));
  else settings.fontCode = Math.min(20, Math.max(10, settings.fontCode+delta));
  saveSettings(); applyFont();
}
function adjRefresh(delta) {
  settings.refresh = Math.min(30, Math.max(1, settings.refresh+delta));
  saveSettings();
  const rv = document.getElementById('gs-refresh-val');
  if (rv) rv.textContent = settings.refresh+'s';
  startRefreshTimer();
}
function startRefreshTimer() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = setInterval(() => {
    if (!activeProjectId) return;
    load();
    if (!current) return;
    if (activeDetailTab === 'messages') loadMessages(current);
    if (activeDetailTab === 'logs') loadLogs(current);
  }, settings.refresh*1000);
}

// ── Project Settings ──────────────────────────────────────────────────────────

let _projectSettings = null;
let _psActiveTab = 'form';
let _psAutoSaveTimer = null;

// autoSave 在表單變更後 debounce 300ms 觸發靜默存檔
function autoSave() {
  if (_psActiveTab !== 'form') return;
  clearTimeout(_psAutoSaveTimer);
  _psAutoSaveTimer = setTimeout(async () => {
    const payload = collectFormData();
    if (!payload.project || !payload.project.name) return;
    try {
      const resp = await fetch(apiBase() + '/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (resp.ok) {
        _projectSettings = await resp.json();
        showAutoSaveIndicator();
      }
    } catch(err) { /* 靜默失敗，不影響使用者操作 */ }
  }, 300);
}

function showAutoSaveIndicator() {
  let ind = document.getElementById('ps-autosave-ind');
  if (!ind) return;
  ind.textContent = t('settings.saved');
  ind.style.opacity = '1';
  clearTimeout(ind._fadeTimer);
  ind._fadeTimer = setTimeout(() => { ind.style.opacity = '0'; }, 1500);
}

async function openProjectSettings() {
  if (!activeProjectId) { showToast(t('projectSettings.noProject')); return; }
  try {
    const resp = await fetch(apiBase() + '/api/settings');
    if (!resp.ok) { showToast(t('projectSettings.loadFailed').replace('{error}', await resp.text())); return; }
    _projectSettings = await resp.json();
    if (!_supportedRunners.length) await loadSupportedRunners();
    _psActiveTab = 'form';
    renderProjectSettingsForm();
    const si = document.getElementById('ps-search-input');
    if (si) { si.value = ''; filterPSFields(''); }
    document.getElementById('ps-search-bar').style.display = '';
    document.getElementById('project-settings-modal').classList.add('open');
    const panel = document.querySelector('#project-settings-modal .modal-panel');
    const header = document.getElementById('ps-sticky-header');
    if (panel && header) {
      panel.onscroll = () => {
        header.style.boxShadow = panel.scrollTop > 4 ? '0 2px 12px rgba(0,0,0,.5)' : '';
      };
    }
  } catch(err) { showToast(t('toast.connectionError').replace('{error}', err.message)); }
}

function closeProjectSettings() {
  document.getElementById('project-settings-modal').classList.remove('open');
  _projectSettings = null;
}

function validatePSJson(value) {
  const errEl = document.getElementById('ps-json-error');
  try { JSON.parse(value); errEl.style.display = 'none'; }
  catch(e) { errEl.textContent = 'Invalid JSON: ' + e.message; errEl.style.display = 'block'; }
}

function switchPSTab(tab) {
  _psActiveTab = tab;
  const formEl = document.getElementById('ps-form');
  const jsonEl = document.getElementById('ps-json');
  const mergedEl = document.getElementById('ps-merged');
  const tabForm = document.getElementById('ps-tab-form');
  const tabJSON = document.getElementById('ps-tab-json');
  const tabMerged = document.getElementById('ps-tab-merged');
  const saveBtn = document.getElementById('ps-save-btn');
  const activeStyle = {background:'var(--accent)',color:'#000',border:'none',fontWeight:'600'};
  const inactiveStyle = {background:'var(--bg-input)',color:'var(--text-2)',border:'1px solid var(--border)',fontWeight:''};
  function applyStyle(el, style) { Object.assign(el.style, style); }

  if (tab === 'form') {
    const editor = document.getElementById('ps-json-editor');
    if (editor.value.trim()) {
      try {
        _projectSettings = JSON.parse(editor.value);
        document.getElementById('ps-json-error').style.display = 'none';
      } catch(e) {
        document.getElementById('ps-json-error').textContent = t('projectSettings.invalidJson').replace('{error}', e.message);
        document.getElementById('ps-json-error').style.display = 'block';
        return;
      }
    }
    formEl.style.display = ''; jsonEl.style.display = 'none'; mergedEl.style.display = 'none';
    applyStyle(tabForm, activeStyle); applyStyle(tabJSON, inactiveStyle); applyStyle(tabMerged, inactiveStyle);
    document.getElementById('ps-search-bar').style.display = '';
    if (saveBtn) saveBtn.style.display = '';
    renderProjectSettingsForm();
  } else if (tab === 'json') {
    const obj = collectFormData();
    const editor = document.getElementById('ps-json-editor');
    editor.value = JSON.stringify(obj, null, 2);
    document.getElementById('ps-json-error').style.display = 'none';
    formEl.style.display = 'none'; jsonEl.style.display = ''; mergedEl.style.display = 'none';
    applyStyle(tabJSON, activeStyle); applyStyle(tabForm, inactiveStyle); applyStyle(tabMerged, inactiveStyle);
    document.getElementById('ps-search-bar').style.display = 'none';
    if (saveBtn) saveBtn.style.display = '';
    editor.oninput = function() { validatePSJson(this.value); };
  } else if (tab === 'merged') {
    formEl.style.display = 'none'; jsonEl.style.display = 'none'; mergedEl.style.display = '';
    applyStyle(tabMerged, activeStyle); applyStyle(tabForm, inactiveStyle); applyStyle(tabJSON, inactiveStyle);
    document.getElementById('ps-search-bar').style.display = 'none';
    if (saveBtn) saveBtn.style.display = 'none';
    loadMergedConfig();
  }
}

async function loadMergedConfig() {
  const el = document.getElementById('ps-merged-content');
  if (!el) return;
  try {
    const resp = await fetch(apiBase() + '/api/merged-config');
    if (!resp.ok) { el.textContent = 'Error: ' + await resp.text(); return; }
    el.textContent = JSON.stringify(await resp.json(), null, 2);
  } catch(e) { el.textContent = 'Error: ' + e.message; }
}

// ── Global Settings ────────────────────────────────────────────────────────────

let _globalSettings = null;
let _gsActiveTab = 'form';

async function openGlobalSettings() {
  try {
    await loadSupportedRunners();
    const resp = await fetch(apiBase() + '/api/user-config');
    if (!resp.ok) { showToast('Failed to load global settings: ' + await resp.text()); return; }
    _globalSettings = await resp.json();
    _gsActiveTab = 'form';
    renderGlobalSettingsForm();
    renderGlobalSettingsAppearance();
    populateAddRunnerSelect();
    document.getElementById('global-settings-modal').classList.add('open');
  } catch(e) { showToast('Connection error: ' + e.message); }
}

function renderGlobalSettingsAppearance() {
  const rv = document.getElementById('gs-refresh-val');
  if (rv) rv.textContent = settings.refresh + 's';
  renderThemeGrid();
  const cfgLocale = _globalSettings && _globalSettings.locale ? _globalSettings.locale : _currentLocale;
  const sel = document.getElementById('gs-locale-select');
  if (sel) {
    sel.innerHTML = SUPPORTED_LOCALES.map(l =>
      `<option value="${l}"${l === cfgLocale ? ' selected' : ''}>${LOCALE_NAMES[l] || l}</option>`
    ).join('');
  }
  const nCb = document.getElementById('gs-notifications');
  const nKnob = document.getElementById('gs-notifications-knob');
  if (nCb) {
    const on = _globalSettings && _globalSettings.notifications === false ? false : true;
    nCb.checked = on;
    if (nKnob) {
      nKnob.style.left = on ? '22px' : '2px';
      nKnob.previousElementSibling.style.background = on ? 'var(--accent)' : 'var(--bg-input)';
    }
  }
}

function gsSetNotifications(on) {
  if (_globalSettings) _globalSettings.notifications = on;
  const knob = document.getElementById('gs-notifications-knob');
  if (knob) {
    knob.style.left = on ? '22px' : '2px';
    knob.previousElementSibling.style.background = on ? 'var(--accent)' : 'var(--bg-input)';
  }
}

function closeGlobalSettings() {
  document.getElementById('global-settings-modal').classList.remove('open');
  _globalSettings = null;
}

function renderGlobalSettingsForm() {
  if (!_globalSettings) return;
  const runnerEl = document.getElementById('gs-default-runner');
  if (runnerEl) {
    const cur = _globalSettings.default_runner || '';
    runnerEl.innerHTML = _supportedRunners.map(r =>
      `<option value="${esc(r.name)}"${r.name === cur ? ' selected' : ''}>${esc(cap(r.name))}</option>`
    ).join('');
  }
  renderGSRunners();
}

function gsSetLocale(val) {
  switchLocale(val);
  if (_globalSettings) _globalSettings.locale = val || undefined;
}

function renderGSRunners() {
  const container = document.getElementById('gs-runners-container');
  if (!container) return;
  const runners = _globalSettings && _globalSettings.runners ? _globalSettings.runners : {};
  const names = Object.keys(runners);
  if (names.length === 0) { container.innerHTML = ''; return; }
  container.innerHTML = names.map(name => {
    const rc = runners[name] || {};
    return `<div style="padding:12px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;display:flex;flex-direction:column;gap:8px">
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span style="font-size:13px;font-weight:600;color:var(--accent)">${esc(name)}</span>
        <button onclick="gsRemoveRunner('${esc(name)}')" style="font-size:11px;color:#f87171;background:none;border:none;cursor:pointer">Remove</button>
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
        <div><label style="font-size:10px;color:var(--text-3)">Command</label><input value="${esc(rc.command||'')}" onchange="gsUpdateRunner('${esc(name)}','command',this.value)" style="width:100%;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box"></div>
        <div><label style="font-size:10px;color:var(--text-3)">Model</label><input value="${esc(rc.model||'')}" onchange="gsUpdateRunner('${esc(name)}','model',this.value)" style="width:100%;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box"></div>
      </div>
    </div>`;
  }).join('');
}

let _supportedRunners = [];

async function loadSupportedRunners() {
  try {
    const resp = await fetch(apiBase() + '/api/supported-runners');
    if (resp.ok) _supportedRunners = await resp.json();
  } catch {}
}

function populateAddRunnerSelect() {
  const sel = document.getElementById('gs-add-runner-select');
  if (!sel) return;
  const existing = _globalSettings && _globalSettings.runners ? Object.keys(_globalSettings.runners) : [];
  const available = _supportedRunners.filter(r => !existing.includes(r.name));
  sel.innerHTML = '<option value="" disabled selected>Select runner...</option>' +
    available.map(r => `<option value="${esc(r.name)}">${esc(cap(r.name))}</option>`).join('');
  sel.disabled = available.length === 0;
}

function gsAddRunner() {
  const sel = document.getElementById('gs-add-runner-select');
  const name = sel ? sel.value : '';
  if (!name) return;
  const preset = _supportedRunners.find(r => r.name === name);
  if (!preset) return;
  if (!_globalSettings) _globalSettings = {};
  if (!_globalSettings.runners) _globalSettings.runners = {};
  _globalSettings.runners[name] = Object.assign({}, preset.config);
  renderGSRunners();
  populateAddRunnerSelect();
}

function gsRemoveRunner(name) {
  if (_globalSettings && _globalSettings.runners) {
    delete _globalSettings.runners[name];
    if (Object.keys(_globalSettings.runners).length === 0) delete _globalSettings.runners;
  }
  renderGSRunners();
  populateAddRunnerSelect();
}

function gsUpdateRunner(name, field, value) {
  if (!_globalSettings) _globalSettings = {};
  if (!_globalSettings.runners) _globalSettings.runners = {};
  if (!_globalSettings.runners[name]) _globalSettings.runners[name] = {};
  _globalSettings.runners[name][field] = value || undefined;
}

function switchGSTab(tab) {
  _gsActiveTab = tab;
  const formEl = document.getElementById('gs-form');
  const jsonEl = document.getElementById('gs-json');
  const tabForm = document.getElementById('gs-tab-form');
  const tabJSON = document.getElementById('gs-tab-json');
  const activeStyle = {background:'var(--accent)',color:'#000',border:'none',fontWeight:'600'};
  const inactiveStyle = {background:'var(--bg-input)',color:'var(--text-2)',border:'1px solid var(--border)',fontWeight:''};
  function applyStyle(el, style) { Object.assign(el.style, style); }

  if (tab === 'form') {
    const editor = document.getElementById('gs-json-editor');
    if (editor.value.trim()) {
      try {
        _globalSettings = JSON.parse(editor.value);
        document.getElementById('gs-json-error').style.display = 'none';
      } catch(e) {
        document.getElementById('gs-json-error').textContent = 'Invalid JSON: ' + e.message;
        document.getElementById('gs-json-error').style.display = 'block';
        return;
      }
    }
    formEl.style.display = ''; jsonEl.style.display = 'none';
    applyStyle(tabForm, activeStyle); applyStyle(tabJSON, inactiveStyle);
    renderGlobalSettingsForm();
  } else {
    formEl.style.display = 'none'; jsonEl.style.display = '';
    applyStyle(tabJSON, activeStyle); applyStyle(tabForm, inactiveStyle);
    const gsEditor = document.getElementById('gs-json-editor');
    gsEditor.value = JSON.stringify(_globalSettings || {}, null, 2);
    document.getElementById('gs-json-error').style.display = 'none';
    gsEditor.oninput = function() {
      const errEl = document.getElementById('gs-json-error');
      try { JSON.parse(this.value); errEl.style.display = 'none'; }
      catch(e) { errEl.textContent = 'Invalid JSON: ' + e.message; errEl.style.display = 'block'; }
    };
  }
}

async function saveGlobalSettings() {
  let payload;
  if (_gsActiveTab === 'json') {
    const editor = document.getElementById('gs-json-editor');
    try {
      payload = JSON.parse(editor.value);
      document.getElementById('gs-json-error').style.display = 'none';
    } catch(e) {
      document.getElementById('gs-json-error').textContent = 'Invalid JSON: ' + e.message;
      document.getElementById('gs-json-error').style.display = 'block';
      return;
    }
  } else {
    payload = Object.assign({}, _globalSettings || {});
    const runnerEl = document.getElementById('gs-default-runner');
    const localeSelect = document.getElementById('gs-locale-select');
    if (runnerEl) payload.default_runner = runnerEl.value.trim() || undefined;
    if (localeSelect) payload.locale = localeSelect.value || undefined;
  }
  try {
    const resp = await fetch(apiBase() + '/api/user-config', {
      method: 'PUT',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(payload),
    });
    if (resp.ok) {
      _globalSettings = await resp.json();
      closeGlobalSettings();
      showToast('Global settings saved');
    } else {
      showToast('Save failed: ' + await resp.text());
    }
  } catch(e) { showToast('Connection error: ' + e.message); }
}

function renderTagList(containerId, items) {
  const arr = Array.isArray(items) ? items : [];
  return `<div id="${containerId}-wrap" style="display:flex;flex-wrap:wrap;gap:6px;align-items:center;padding:6px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;min-height:36px">
    ${arr.map((v, i) => `<span style="display:inline-flex;align-items:center;gap:4px;padding:2px 8px;background:var(--bg-hover);border:1px solid var(--border);border-radius:4px;font-size:12px">${esc(v)}<button type="button" onclick="removeTag('${containerId}',${i})" style="background:none;border:none;color:var(--text-4);cursor:pointer;padding:0;font-size:14px;line-height:1">&times;</button></span>`).join('')}
    <input id="${containerId}-input" type="text" placeholder="${t('field.add')}" style="border:none;outline:none;background:transparent;color:var(--text-1);font-size:12px;font-family:inherit;min-width:80px;flex:1" onkeydown="if(event.key==='Enter'){event.preventDefault();addTag('${containerId}');}">
  </div>`;
}

function addTag(containerId) {
  const input = document.getElementById(containerId + '-input');
  const val = input.value.trim();
  if (!val) return;
  input.value = '';
  // re-render the tag list with the new item
  const wrap = document.getElementById(containerId + '-wrap');
  if (!wrap) return;
  const chips = wrap.querySelectorAll('span');
  const items = [];
  chips.forEach(chip => { const text = chip.firstChild ? chip.firstChild.textContent : ''; if (text) items.push(text); });
  items.push(val);
  wrap.outerHTML = renderTagList(containerId, items).match(/<div[^>]*>[\s\S]*<\/div>/)[0];
  autoSave();
  // Re-attach: find the new input
  setTimeout(() => { const ni = document.getElementById(containerId + '-input'); if (ni) ni.focus(); }, 0);
}

function removeTag(containerId, idx) {
  const wrap = document.getElementById(containerId + '-wrap');
  if (!wrap) return;
  const chips = wrap.querySelectorAll('span');
  const items = [];
  chips.forEach(chip => { const text = chip.firstChild ? chip.firstChild.textContent : ''; if (text) items.push(text); });
  items.splice(idx, 1);
  const newHtml = renderTagList(containerId, items);
  const tmp = document.createElement('div'); tmp.innerHTML = newHtml;
  wrap.replaceWith(tmp.firstElementChild);
  autoSave();
}

function getTagItems(containerId) {
  const wrap = document.getElementById(containerId + '-wrap');
  if (!wrap) return [];
  const chips = wrap.querySelectorAll('span');
  const items = [];
  chips.forEach(chip => { const text = chip.firstChild ? chip.firstChild.textContent.trim() : ''; if (text) items.push(text); });
  // also check the input
  const inp = document.getElementById(containerId + '-input');
  if (inp && inp.value.trim()) items.push(inp.value.trim());
  return items;
}

function psField(label, id, value, required) {
  return `<div class="ps-field-row" data-label="${escAttr(label.toLowerCase())}" data-key="${escAttr(id.toLowerCase())}" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:120px">${label}${required ? ' <span style="color:#f87171">*</span>' : ''}</label>
    <input id="${id}" type="text" value="${escAttr(value||'')}" onblur="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
  </div>`;
}

function psTagField(label, id, items) {
  return `<div class="ps-field-row" data-label="${escAttr(label.toLowerCase())}" data-key="${escAttr(id.toLowerCase())}" style="margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);display:block;margin-bottom:6px">${label}</label>
    ${renderTagList(id, items)}
  </div>`;
}

function psCheckbox(label, id, checked) {
  return `<div class="ps-field-row" data-label="${escAttr(label.toLowerCase())}" data-key="${escAttr(id.toLowerCase())}" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${label}</label>
    <label class="toggle"><input id="${id}" type="checkbox"${checked?' checked':''} onchange="autoSave()"><span class="slider"></span></label>
  </div>`;
}

function renderProjectSettingsForm() {
  const s = _projectSettings || {};
  const proj = s.project || {};
  const runners = s.runners || {};
  const roles = s.roles || {};
  const runnerNames = Object.keys(runners);

  let html = '';

  // Project section
  html += `<div style="margin-bottom:20px"><div class="settings-label">${t('projectSettings.project')}</div>`;
  html += psField(t('field.name'), 'ps-proj-name', proj.name, true);
  html += psField(t('field.description'), 'ps-proj-desc', proj.description);
  html += psField(t('field.language'), 'ps-proj-lang', proj.language);
  html += psTagField(t('field.build'), 'ps-proj-build', proj.build);
  html += psTagField(t('field.test'), 'ps-proj-test', proj.test);
  html += psTagField(t('field.lint'), 'ps-proj-lint', proj.lint);
  html += psTagField(t('field.setup'), 'ps-proj-setup', proj.setup);
  html += psTagField(t('field.docs'), 'ps-proj-docs', proj.docs);
  html += psTagField(t('field.rules'), 'ps-proj-rules', proj.rules);
  html += psTagField(t('field.includes'), 'ps-proj-includes', proj.includes);
  html += `</div>`;

  // Other section
  html += `<div style="margin-bottom:20px"><div class="settings-label">${t('projectSettings.general')}</div>`;
  html += `<div class="ps-field-row" data-label="default runner" data-key="ps-default" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${t('projectSettings.defaultRunner')}</label>
    <select id="ps-default" onchange="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
      <option value="">${t('projectSettings.none')}</option>
      ${_supportedRunners.map(r => `<option value="${escAttr(r.name)}"${s.default_runner===r.name?' selected':''}>${esc(cap(r.name))}</option>`).join('')}
    </select>
  </div>`;
  html += `<div class="ps-field-row" data-label="isolation" data-key="ps-isolation" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${t('projectSettings.isolation')}</label>
    <select id="ps-isolation" onchange="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
      <option value=""${!s.isolation?' selected':''}>${t('projectSettings.none')}</option>
      <option value="worktree"${s.isolation==='worktree'?' selected':''}>worktree</option>
    </select>
  </div>`;
  html += `<div class="ps-field-row" data-label="max concurrent runs" data-key="ps-maxruns" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${t('projectSettings.maxConcurrentRuns')}</label>
    <input id="ps-maxruns" type="number" min="1" max="20" value="${s.max_concurrent_runs||1}" onblur="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
  </div>`;
  html += `<div class="ps-field-row" data-label="commit" data-key="ps-commit" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${t('projectSettings.commit')}</label>
    <select id="ps-commit" onchange="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
      <option value=""${!s.commit?' selected':''}>${t('projectSettings.none')}</option>
      <option value="per-round"${s.commit==='per-round'?' selected':''}>per-round</option>
      <option value="on-done"${s.commit==='on-done'?' selected':''}>on-done</option>
      <option value="never"${s.commit==='never'?' selected':''}>never</option>
    </select>
  </div>`;
  html += psCheckbox(t('field.parallelReviewTest'), 'ps-parallel-review-test', s.parallel_review_test);
  html += psCheckbox(t('field.autoDiscoverFeatures'), 'ps-auto-discover', s.auto_discover_features);
  html += psTagField(t('field.hubRepos'), 'ps-hubrepos', s.hub_repos);
  html += psTagField(t('field.rules'), 'ps-rules', s.rules);
  html += `</div>`;

  // Runners section
  html += `<div style="margin-bottom:20px"><div class="settings-label">${t('projectSettings.runners')}</div><div id="ps-runners-wrap">`;
  runnerNames.forEach(name => {
    const rc = runners[name] || {};
    html += `<div id="ps-runner-${escAttr(name)}" style="border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
        <span style="font-size:13px;font-weight:600;color:var(--text-1)">${esc(name)}</span>
        <button type="button" onclick="removeRunner('${escAttr(name)}')" style="padding:2px 8px;font-size:12px;background:none;border:1px solid var(--border);border-radius:6px;color:#f87171;cursor:pointer">${t('projectSettings.remove')}</button>
      </div>
      ${psField(t('field.command'), 'ps-runner-cmd-'+name, rc.command)}
      ${psTagField(t('field.args'), 'ps-runner-args-'+name, rc.args)}
      ${psField(t('field.model'), 'ps-runner-model-'+name, rc.model)}
      ${psField(t('field.outputFormat'), 'ps-runner-output-format-'+name, rc.output_format)}
      <div style="display:flex;gap:16px;margin-bottom:6px">
        <label style="font-size:13px;color:var(--text-2);display:flex;align-items:center;gap:8px"><label class="toggle"><input type="checkbox" id="ps-runner-stdin-${escAttr(name)}"${rc.stdin?' checked':''} onchange="autoSave()"><span class="slider"></span></label> stdin</label>
        <label style="font-size:13px;color:var(--text-2);display:flex;align-items:center;gap:8px"><label class="toggle"><input type="checkbox" id="ps-runner-tty-${escAttr(name)}"${rc.tty?' checked':''} onchange="autoSave()"><span class="slider"></span></label> tty</label>
      </div>
    </div>`;
  });
  html += `</div>
  <div style="display:flex;gap:8px;margin-top:8px">
    <input id="ps-new-runner-name" type="text" placeholder="${t('projectSettings.runnerPlaceholder')}" style="flex:1;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none" onkeydown="if(event.key==='Enter'){event.preventDefault();addRunner();}">
    <button type="button" onclick="addRunner()" style="padding:6px 14px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;font-size:13px;color:var(--text-2);cursor:pointer">${t('projectSettings.addRunner')}</button>
  </div></div>`;

  // Roles section
  const roleNames = ['designer', 'coder', 'reviewer', 'tester', 'acceptor'];
  html += `<div style="margin-bottom:20px"><div class="settings-label">${t('projectSettings.roles')}</div>`;
  roleNames.forEach(rn => {
    const rc = roles[rn] || {};
    html += `<div style="border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px">
      <div style="font-size:13px;font-weight:600;color:var(--text-1);margin-bottom:10px;text-transform:capitalize">${rn}</div>
      ${psField(t('field.model'), 'ps-role-model-'+rn, rc.model)}
      ${rn === 'reviewer' ? psField(t('field.deepModel'), 'ps-role-deepmodel-'+rn, rc.deep_model) : ''}
      ${rn === 'tester' ? psField(t('field.screenshotDir'), 'ps-role-screenshot-dir-'+rn, rc.screenshot_dir || '.4x/e2e/{feature-id}/screenshot/') : ''}
      ${psTagField(t('field.instructions'), 'ps-role-instr-'+rn, rc.instructions)}
      ${psTagField(t('field.includes'), 'ps-role-includes-'+rn, rc.includes)}
    </div>`;
  });
  html += `</div>`;

  document.getElementById('ps-form').innerHTML = html;
}

// filterPSFields 依搜尋字串即時過濾設定頁欄位，不匹配的列隱藏，匹配的所在 section 自動顯示
function filterPSFields(query) {
  const q = query.trim().toLowerCase();
  const form = document.getElementById('ps-form');
  if (!form) return;
  const rows = form.querySelectorAll('.ps-field-row');
  if (!q) {
    rows.forEach(r => { r.style.display = ''; });
    form.querySelectorAll('[data-section]').forEach(s => { s.style.display = ''; });
    return;
  }
  const visibleSections = new Set();
  rows.forEach(r => {
    const label = (r.dataset.label || '').toLowerCase();
    const key = (r.dataset.key || '').toLowerCase();
    const match = label.includes(q) || key.includes(q);
    r.style.display = match ? '' : 'none';
    if (match) {
      // 找父 section 容器並記錄
      let parent = r.parentElement;
      while (parent && parent !== form) {
        if (parent.dataset && parent.dataset.section) { visibleSections.add(parent); break; }
        parent = parent.parentElement;
      }
    }
  });
}

function addRunner() {
  const inp = document.getElementById('ps-new-runner-name');
  const name = inp.value.trim();
  if (!name) return;
  inp.value = '';
  const wrap = document.getElementById('ps-runners-wrap');
  const div = document.createElement('div');
  div.id = 'ps-runner-' + name;
  div.style.cssText = 'border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px';
  div.innerHTML = `<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
    <span style="font-size:13px;font-weight:600;color:var(--text-1)">${esc(name)}</span>
    <button type="button" onclick="removeRunner('${escAttr(name)}')" style="padding:2px 8px;font-size:12px;background:none;border:1px solid var(--border);border-radius:6px;color:#f87171;cursor:pointer">${t('projectSettings.remove')}</button>
  </div>
  ${psField(t('field.command'), 'ps-runner-cmd-'+name, '')}
  ${psTagField(t('field.args'), 'ps-runner-args-'+name, [])}
  ${psField(t('field.model'), 'ps-runner-model-'+name, '')}
  ${psField(t('field.outputFormat'), 'ps-runner-output-format-'+name, '')}
  <div style="display:flex;gap:16px;margin-bottom:6px">
    <label style="font-size:13px;color:var(--text-2);display:flex;align-items:center;gap:8px"><label class="toggle"><input type="checkbox" id="ps-runner-stdin-${escAttr(name)}" onchange="autoSave()"><span class="slider"></span></label> stdin</label>
    <label style="font-size:13px;color:var(--text-2);display:flex;align-items:center;gap:8px"><label class="toggle"><input type="checkbox" id="ps-runner-tty-${escAttr(name)}" onchange="autoSave()"><span class="slider"></span></label> tty</label>
  </div>`;
  wrap.appendChild(div);
  autoSave();
}

function removeRunner(name) {
  const el = document.getElementById('ps-runner-' + name);
  if (el) { el.remove(); autoSave(); }
}

function collectFormData() {
  const proj = {};
  // name 是必填，只在非空時設定
  const nameVal = (document.getElementById('ps-proj-name') || {}).value?.trim();
  if (nameVal) proj['name'] = nameVal;
  // description / language 是選填，空字串也要送出讓後端覆蓋舊值（避免無法清空）
  proj['description'] = (document.getElementById('ps-proj-desc') || {}).value?.trim() ?? '';
  proj['language'] = (document.getElementById('ps-proj-lang') || {}).value?.trim() ?? '';
  const tagFields = ['build','test','lint','setup','docs','rules','includes'];
  tagFields.forEach(f => { const items = getTagItems('ps-proj-'+f); if (items.length) proj[f] = items; });

  const obj = { project: proj };

  const defRunner = (document.getElementById('ps-default') || {}).value?.trim();
  if (defRunner) obj.default_runner = defRunner;
  const isolation = (document.getElementById('ps-isolation') || {}).value?.trim();
  if (isolation) obj.isolation = isolation;
  const maxRunsEl = document.getElementById('ps-maxruns');
  if (maxRunsEl) { const v = parseInt(maxRunsEl.value); if (v > 0) obj.max_concurrent_runs = v; }
  const commit = (document.getElementById('ps-commit') || {}).value?.trim();
  if (commit) obj.commit = commit;
  const parallelRT = (document.getElementById('ps-parallel-review-test') || {}).checked;
  obj.parallel_review_test = !!parallelRT;
  const autoDiscover = (document.getElementById('ps-auto-discover') || {}).checked;
  obj.auto_discover_features = !!autoDiscover;
  const hubRepos = getTagItems('ps-hubrepos');
  if (hubRepos.length) obj.hub_repos = hubRepos;
  const topRules = getTagItems('ps-rules');
  if (topRules.length) obj.rules = topRules;

  // Runners
  const runners = {};
  const runnerWrap = document.getElementById('ps-runners-wrap');
  if (runnerWrap) {
    runnerWrap.querySelectorAll(':scope > [id^="ps-runner-"]').forEach(el => {
      const name = el.id.replace('ps-runner-', '');
      if (!name) return;
      const rc = {};
      const cmd = (document.getElementById('ps-runner-cmd-'+name) || {}).value?.trim();
      if (cmd) rc.command = cmd;
      const args = getTagItems('ps-runner-args-'+name);
      if (args.length) rc.args = args;
      const model = (document.getElementById('ps-runner-model-'+name) || {}).value?.trim();
      if (model) rc.model = model;
      const outputFormat = (document.getElementById('ps-runner-output-format-'+name) || {}).value?.trim();
      if (outputFormat) rc.output_format = outputFormat;
      const stdin = (document.getElementById('ps-runner-stdin-'+name) || {}).checked;
      if (stdin) rc.stdin = true;
      const tty = (document.getElementById('ps-runner-tty-'+name) || {}).checked;
      if (tty) rc.tty = true;
      runners[name] = rc;
    });
  }
  if (Object.keys(runners).length) obj.runners = runners;

  // Roles
  const roleNames = ['designer', 'coder', 'reviewer', 'tester', 'acceptor'];
  const rolesObj = {};
  roleNames.forEach(rn => {
    const rc = {};
    const model = (document.getElementById('ps-role-model-'+rn) || {}).value?.trim();
    if (model) rc.model = model;
    if (rn === 'reviewer') {
      const dm = (document.getElementById('ps-role-deepmodel-'+rn) || {}).value?.trim();
      if (dm) rc.deep_model = dm;
    }
    if (rn === 'tester') {
      const screenshotDir = (document.getElementById('ps-role-screenshot-dir-'+rn) || {}).value?.trim();
      if (screenshotDir) rc.screenshot_dir = screenshotDir;
    }
    const instr = getTagItems('ps-role-instr-'+rn);
    if (instr.length) rc.instructions = instr;
    const includes = getTagItems('ps-role-includes-'+rn);
    if (includes.length) rc.includes = includes;
    if (Object.keys(rc).length) rolesObj[rn] = rc;
  });
  if (Object.keys(rolesObj).length) obj.roles = rolesObj;

  return obj;
}

async function saveProjectSettings() {
  let payload;
  if (_psActiveTab === 'json') {
    const editor = document.getElementById('ps-json-editor');
    const errEl = document.getElementById('ps-json-error');
    try {
      payload = JSON.parse(editor.value);
      errEl.style.display = 'none';
    } catch(e) {
      errEl.textContent = t('projectSettings.invalidJson').replace('{error}', e.message);
      errEl.style.display = 'block';
      return;
    }
  } else {
    payload = collectFormData();
  }

  // Frontend validation
  if (!payload.project || !payload.project.name) {
    const nameEl = document.getElementById('ps-proj-name');
    if (nameEl) { nameEl.style.borderColor = '#f87171'; nameEl.focus(); }
    showToast(t('projectSettings.nameRequired'));
    return;
  }

  try {
    const resp = await fetch(apiBase() + '/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) { showToast(t('projectSettings.saveFailed').replace('{error}', await resp.text())); return; }
    // Reload settings
    _projectSettings = await resp.json();
    closeProjectSettings();
    // Brief success feedback
    const btn = document.querySelector('[onclick="saveProjectSettings()"]');
    if (btn) { const orig = btn.textContent; btn.textContent = t('projectSettings.saved'); setTimeout(() => { btn.textContent = orig; }, 1500); }
  } catch(err) { showToast(t('toast.connectionError').replace('{error}', err.message)); }
}
function initSettings() { applyTheme(settings.theme); applyFont(); }
