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
    if (activeDetailTab === 'screenshots') loadScreenshots(current);
    if (activeDetailTab !== 'messages') loadTotalCost(current);
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
    const sections = document.getElementById('ps-settings-sections');
    if (sections) sections.style.display = '';
    renderProfilesTab();
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
  const sections = document.getElementById('ps-settings-sections');
  if (sections) sections.style.display = 'none';
  closeProfileEditor();
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
  const settingsSections = document.getElementById('ps-settings-sections');
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
    if (settingsSections) settingsSections.style.display = '';
    applyStyle(tabForm, activeStyle); applyStyle(tabJSON, inactiveStyle); applyStyle(tabMerged, inactiveStyle);
    document.getElementById('ps-search-bar').style.display = '';
    if (saveBtn) saveBtn.style.display = '';
    renderProjectSettingsForm();
    renderProfilesTab();
  } else if (tab === 'json') {
    const obj = collectFormData();
    const editor = document.getElementById('ps-json-editor');
    editor.value = JSON.stringify(obj, null, 2);
    document.getElementById('ps-json-error').style.display = 'none';
    formEl.style.display = 'none'; jsonEl.style.display = ''; mergedEl.style.display = 'none';
    if (settingsSections) settingsSections.style.display = 'none';
    applyStyle(tabJSON, activeStyle); applyStyle(tabForm, inactiveStyle); applyStyle(tabMerged, inactiveStyle);
    document.getElementById('ps-search-bar').style.display = 'none';
    if (saveBtn) saveBtn.style.display = '';
    editor.oninput = function() { validatePSJson(this.value); };
  } else if (tab === 'merged') {
    formEl.style.display = 'none'; jsonEl.style.display = 'none'; mergedEl.style.display = '';
    if (settingsSections) settingsSections.style.display = 'none';
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

// psTierField — 渲染 runner 的 model tier 編輯器（tier 名稱 → model id 的 key-value 列表）。
// tier 名稱本為自由字串（見 internal/protocol/model.go ResolveTierModel），不限 opus/sonnet，
// 這裡讓使用者可直接新增/刪除，不用切去 JSON 分頁手改。
function psTierField(runnerName, tiers) {
  const entries = Object.entries(tiers || {});
  return `<div style="margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);display:block;margin-bottom:6px">Tiers</label>
    <div id="ps-tiers-${escAttr(runnerName)}-wrap" style="display:flex;flex-direction:column;gap:6px">
      ${entries.map(([k, v]) => psTierRow(runnerName, k, v)).join('')}
    </div>
    <div style="display:flex;gap:6px;margin-top:6px">
      <input id="ps-tiers-${escAttr(runnerName)}-newkey" type="text" placeholder="tier name (e.g. fable5)" style="flex:1;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none" onkeydown="if(event.key==='Enter'){event.preventDefault();addTier('${escAttr(runnerName)}');}">
      <input id="ps-tiers-${escAttr(runnerName)}-newval" type="text" placeholder="model id" style="flex:1;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none" onkeydown="if(event.key==='Enter'){event.preventDefault();addTier('${escAttr(runnerName)}');}">
      <button type="button" onclick="addTier('${escAttr(runnerName)}')" style="padding:6px 12px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;font-size:12px;color:var(--text-2);cursor:pointer">${t('field.add')}</button>
    </div>
  </div>`;
}

function psTierRow(runnerName, key, value) {
  return `<div data-tier-row style="display:flex;gap:6px;align-items:center">
    <input value="${escAttr(key)}" data-tier-field="key" onchange="autoSave()" placeholder="tier name" style="flex:1;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box">
    <input value="${escAttr(value)}" data-tier-field="value" onchange="autoSave()" placeholder="model id" style="flex:1;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box">
    <button type="button" onclick="this.closest('[data-tier-row]').remove();autoSave();" style="background:none;border:none;color:var(--text-4);cursor:pointer;padding:0 4px;font-size:16px;line-height:1">&times;</button>
  </div>`;
}

// addTier — 從「新增 tier」輸入列讀取 key/value，插入一列新 tier row 並觸發 autoSave。
function addTier(runnerName) {
  const keyInput = document.getElementById(`ps-tiers-${runnerName}-newkey`);
  const valInput = document.getElementById(`ps-tiers-${runnerName}-newval`);
  if (!keyInput || !valInput) return;
  const key = keyInput.value.trim();
  const val = valInput.value.trim();
  if (!key || !val) return;
  keyInput.value = '';
  valInput.value = '';
  const wrap = document.getElementById(`ps-tiers-${runnerName}-wrap`);
  if (!wrap) return;
  wrap.insertAdjacentHTML('beforeend', psTierRow(runnerName, key, val));
  autoSave();
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
  html += psTagField(t('field.docsCheck'), 'ps-proj-docs_check', proj.docs_check);
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
  const profileNames = Object.keys(s.profiles || {});
  // 內建 profile 永遠可選（即使專案 settings.json 沒自訂 profiles 區段）。
  BUILTIN_PROFILES.forEach(n => { if (!profileNames.includes(n)) profileNames.push(n); });
  html += `<div class="ps-field-row" data-label="default profile" data-key="ps-default-profile" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
    <label style="font-size:13px;color:var(--text-2);min-width:160px">${t('projectSettings.defaultProfile')}</label>
    <select id="ps-default-profile" onchange="autoSave()" style="flex:1;max-width:400px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:6px 10px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none">
      <option value="">${t('projectSettings.none')}</option>
      ${profileNames.map(n => `<option value="${escAttr(n)}"${s.default_profile===n?' selected':''}>${esc(n)}</option>`).join('')}
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
      ${psTierField(name, rc.tiers)}
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
  const roleNames = ['designer', 'design-reviewer', 'coder', 'reviewer', 'tester', 'deep-reviewer', 'acceptor'];
  html += `<div style="margin-bottom:20px"><div class="settings-label">${t('projectSettings.roles')}</div>`;
  roleNames.forEach(rn => {
    const rc = roles[rn] || {};
    html += `<div style="border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px">
      <div style="font-size:13px;font-weight:600;color:var(--text-1);margin-bottom:10px;text-transform:capitalize">${rn}</div>
      ${psField(t('field.model'), 'ps-role-model-'+rn, rc.model)}
      ${rn === 'reviewer' || rn === 'deep-reviewer' ? psField(t('field.deepModel'), 'ps-role-deepmodel-'+rn, rc.deep_model) : ''}
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
  ${psTierField(name, {})}
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

// collectFormData 以 _projectSettings 的深拷貝為底，只覆寫表單實際管理的欄位。
// 表單沒有對應 UI 的既有欄位（profiles、evolution、self_mod_guard、notifications、
// roles.*.max_fix_rounds 等）維持原值，不會因為使用者動了一個不相干的欄位就被整段砍掉——
// 過去用全新 {} 起手直接蓋掉 PUT /api/settings（全量替換）時會把這些欄位靜默清空。
function collectFormData() {
  const obj = JSON.parse(JSON.stringify(_projectSettings || {}));

  const proj = {};
  // name 是必填，只在非空時設定
  const nameVal = (document.getElementById('ps-proj-name') || {}).value?.trim();
  if (nameVal) proj['name'] = nameVal;
  // description / language 是選填，空字串也要送出讓後端覆蓋舊值（避免無法清空）
  proj['description'] = (document.getElementById('ps-proj-desc') || {}).value?.trim() ?? '';
  proj['language'] = (document.getElementById('ps-proj-lang') || {}).value?.trim() ?? '';
  const tagFields = ['build','test','lint','setup','docs','docs_check','rules','includes'];
  tagFields.forEach(f => { const items = getTagItems('ps-proj-'+f); if (items.length) proj[f] = items; });
  obj.project = proj;

  const defRunner = (document.getElementById('ps-default') || {}).value?.trim();
  obj.default_runner = defRunner || undefined;
  const defProfile = (document.getElementById('ps-default-profile') || {}).value?.trim();
  obj.default_profile = defProfile || undefined;
  const isolation = (document.getElementById('ps-isolation') || {}).value?.trim();
  obj.isolation = isolation || undefined;
  const maxRunsEl = document.getElementById('ps-maxruns');
  if (maxRunsEl) { const v = parseInt(maxRunsEl.value); obj.max_concurrent_runs = v > 0 ? v : undefined; }
  const commit = (document.getElementById('ps-commit') || {}).value?.trim();
  obj.commit = commit || undefined;
  const parallelRT = (document.getElementById('ps-parallel-review-test') || {}).checked;
  obj.parallel_review_test = !!parallelRT;
  const autoDiscover = (document.getElementById('ps-auto-discover') || {}).checked;
  obj.auto_discover_features = !!autoDiscover;
  const hubRepos = getTagItems('ps-hubrepos');
  obj.hub_repos = hubRepos.length ? hubRepos : undefined;
  const topRules = getTagItems('ps-rules');
  obj.rules = topRules.length ? topRules : undefined;

  // Runners — runner 的存在與否完全由 DOM 決定（新增/移除走 addRunner/removeRunner），
  // 但個別 runner 內表單沒管理到的欄位（如 quiet、transient_retries）從既有設定繼承。
  const runners = {};
  const runnerWrap = document.getElementById('ps-runners-wrap');
  if (runnerWrap) {
    runnerWrap.querySelectorAll(':scope > [id^="ps-runner-"]').forEach(el => {
      const name = el.id.replace('ps-runner-', '');
      if (!name) return;
      const rc = Object.assign({}, (obj.runners && obj.runners[name]) || {});
      const cmd = (document.getElementById('ps-runner-cmd-'+name) || {}).value?.trim();
      if (cmd) rc.command = cmd; else delete rc.command;
      const args = getTagItems('ps-runner-args-'+name);
      if (args.length) rc.args = args; else delete rc.args;
      const model = (document.getElementById('ps-runner-model-'+name) || {}).value?.trim();
      if (model) rc.model = model; else delete rc.model;
      const outputFormat = (document.getElementById('ps-runner-output-format-'+name) || {}).value?.trim();
      if (outputFormat) rc.output_format = outputFormat; else delete rc.output_format;
      const stdin = (document.getElementById('ps-runner-stdin-'+name) || {}).checked;
      if (stdin) rc.stdin = true; else delete rc.stdin;
      const tty = (document.getElementById('ps-runner-tty-'+name) || {}).checked;
      if (tty) rc.tty = true; else delete rc.tty;
      const tiersWrap = document.getElementById('ps-tiers-'+name+'-wrap');
      if (tiersWrap) {
        const tiers = {};
        tiersWrap.querySelectorAll(':scope > [data-tier-row]').forEach(row => {
          const k = row.querySelector('[data-tier-field="key"]').value.trim();
          const v = row.querySelector('[data-tier-field="value"]').value.trim();
          if (k && v) tiers[k] = v;
        });
        if (Object.keys(tiers).length) rc.tiers = tiers; else delete rc.tiers;
      }
      runners[name] = rc;
    });
  }
  obj.runners = Object.keys(runners).length ? runners : undefined;

  // Roles — 固定 7 個 role 名稱由表單管理欄位覆寫，未管理到的欄位（max_fix_rounds、
  // parallel_reviewers、angles_per_reviewer、angle_mapping）從既有設定繼承；
  // 表單清單外若還有其他自訂 role key 也原樣保留。
  const roleNames = ['designer', 'design-reviewer', 'coder', 'reviewer', 'tester', 'deep-reviewer', 'acceptor'];
  const rolesObj = Object.assign({}, obj.roles || {});
  roleNames.forEach(rn => {
    const rc = Object.assign({}, rolesObj[rn] || {});
    const model = (document.getElementById('ps-role-model-'+rn) || {}).value?.trim();
    if (model) rc.model = model; else delete rc.model;
    if (rn === 'reviewer' || rn === 'deep-reviewer') {
      const dm = (document.getElementById('ps-role-deepmodel-'+rn) || {}).value?.trim();
      if (dm) rc.deep_model = dm; else delete rc.deep_model;
    }
    if (rn === 'tester') {
      const screenshotDir = (document.getElementById('ps-role-screenshot-dir-'+rn) || {}).value?.trim();
      if (screenshotDir) rc.screenshot_dir = screenshotDir; else delete rc.screenshot_dir;
    }
    const instr = getTagItems('ps-role-instr-'+rn);
    if (instr.length) rc.instructions = instr; else delete rc.instructions;
    const includes = getTagItems('ps-role-includes-'+rn);
    if (includes.length) rc.includes = includes; else delete rc.includes;
    if (Object.keys(rc).length) rolesObj[rn] = rc; else delete rolesObj[rn];
  });
  obj.roles = Object.keys(rolesObj).length ? rolesObj : undefined;

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

// ── Dashboard Settings UI ────────────────────────────────────────────────────

let _profilesData = [];
let _profileEditorMode = null; // 'create' or 'edit'
let _profileEditorName = null;
let _profileDragSource = null;
const _selectablePhases = ['designing', 'design-reviewing', 'coding', 'reviewing', 'deep-reviewing', 'testing', 'accepting'];

async function loadSupportedRunners() {
  try {
    const resp = await fetch(apiBase() + '/api/supported-runners');
    if (resp.ok) {
      _supportedRunners = await resp.json();
    }
  } catch(err) { console.warn('Failed to load supported runners:', err); }
}

// renderProfilesTab — 列出所有 profiles，每筆顯示名稱、啟用 phase 數量、操作按鈕
function renderProfilesTab() {
  const container = document.getElementById('settings-profiles-section');
  if (!container) return;

  const profiles = (_projectSettings && _projectSettings.profiles) || {};
  const entries = Object.entries(profiles);
  let html = '<div style="display:flex;flex-direction:column;gap:10px">';
  if (entries.length === 0) {
   html += `<div style="padding:12px;border:1px dashed var(--border);border-radius:8px;color:var(--text-3)">${esc(t('projectSettings.noProfiles'))}</div>`;
  } else {
   for (const [name, profile] of entries) {
     const phaseCount = Array.isArray(profile.phases) ? profile.phases.length : 0;
     html += `<div style="display:flex;justify-content:space-between;align-items:center;padding:10px 12px;border:1px solid var(--border);border-radius:8px;background:var(--bg-input)">
       <div style="min-width:0">
         <strong style="color:var(--text-1)">${esc(name)}</strong>
         <span style="color:var(--text-3);margin-left:12px">${esc(t('projectSettings.phasesCount').replace('{count}', phaseCount))}</span>
       </div>
       <div style="display:flex;gap:8px;flex-shrink:0">
         <button onclick="openProfileEditor('${escAttr(name)}')" style="padding:4px 8px;background:var(--accent);color:#000;border:none;border-radius:6px;cursor:pointer;font-size:12px;font-weight:600">${esc(t('projectSettings.edit'))}</button>
         <button onclick="deleteProfile('${escAttr(name)}')" style="padding:4px 8px;background:#ef4444;color:white;border:none;border-radius:6px;cursor:pointer;font-size:12px">${esc(t('projectSettings.delete'))}</button>
       </div>
     </div>`;
   }
  }
  html += `</div><button onclick="openProfileEditor('')" style="margin-top:12px;padding:8px 12px;background:var(--accent);color:#000;border:none;border-radius:8px;cursor:pointer;font-size:12px;font-weight:600">${esc(t('projectSettings.addProfile'))}</button>`;
  container.innerHTML = html;
}

// openProfileEditor — 開啟 profile 編輯面板
function openProfileEditor(name) {
  _profileEditorMode = name ? 'edit' : 'create';
  _profileEditorName = name;

  const editor = document.getElementById('profile-editor-modal');
  if (!editor) return;

  const profile = name && _projectSettings && _projectSettings.profiles && _projectSettings.profiles[name] ? _projectSettings.profiles[name] : {};

  const nameInput = editor.querySelector('#profile-editor-name');
  if (nameInput) {
   nameInput.value = name || '';
   nameInput.readOnly = _profileEditorMode === 'edit';
  }

  const phasesContainer = editor.querySelector('#profile-editor-phases');
  if (phasesContainer) {
   const existingPhases = Array.isArray(profile.phases) ? profile.phases : [];
   const orderedPhases = [];
   const seen = new Set();
   existingPhases.forEach(ps => {
     if (ps && _selectablePhases.includes(ps.phase) && !seen.has(ps.phase)) {
       orderedPhases.push(ps.phase);
       seen.add(ps.phase);
     }
   });
   _selectablePhases.forEach(phase => {
     if (!seen.has(phase)) orderedPhases.push(phase);
   });

   let html = '';
   orderedPhases.forEach(phase => {
     const phaseData = existingPhases.find(p => p.phase === phase) || {};
     const isChecked = phase === 'coding' || !!phaseData.phase;
     const isDisabled = phase === 'coding';

     html += `<div data-phase-row data-phase="${escAttr(phase)}" draggable="true" style="display:grid;grid-template-columns:auto 1fr auto;gap:10px;align-items:start;padding:10px 12px;border:1px solid var(--border);border-radius:8px;margin-bottom:8px;background:var(--bg-input);cursor:grab">
       <div style="display:flex;align-items:center;gap:8px;min-width:0">
         <span aria-hidden="true" style="color:var(--text-4);font-size:16px;line-height:1">⋮⋮</span>
         <input type="checkbox" id="phase-${escAttr(phase)}" ${isChecked ? 'checked' : ''} ${isDisabled ? 'disabled' : ''} onchange="updatePhaseSelection('${escAttr(phase)}')">
         <label for="phase-${escAttr(phase)}" style="font-size:13px;color:var(--text-1);text-transform:capitalize">${esc(phase)}</label>
       </div>
       <div style="display:flex;flex-direction:column;gap:6px;min-width:0">
         <div id="phase-runner-${escAttr(phase)}"></div>
         <div id="phase-model-${escAttr(phase)}"></div>
       </div>
       <div style="font-size:11px;color:var(--text-4);padding-top:3px">${isDisabled ? 'required' : ''}</div>
     </div>`;
   });
   phasesContainer.innerHTML = html;
   orderedPhases.forEach(phase => {
     const phaseData = existingPhases.find(p => p.phase === phase) || {};
     renderPhaseRunnerPicker(phase, phaseData.runner || '', phaseData.model || '');
   });
   initProfileDragSort();
  }

  editor.classList.add('open');
}

// closeProfileEditor — 關閉編輯面板
function closeProfileEditor() {
  const editor = document.getElementById('profile-editor-modal');
  if (editor) editor.classList.remove('open');
  _profileEditorMode = null;
  _profileEditorName = null;
  _profileDragSource = null;
}

// renderPhaseRunnerPicker — per-phase runner/model 選擇器
function renderPhaseRunnerPicker(phaseId, currentRunner, currentModel) {
  const runnerContainer = document.getElementById(`phase-runner-${phaseId}`);
  if (runnerContainer) {
    let html = '<label style="font-size:10px;color:var(--text-3);display:block;margin-bottom:2px">Runner</label>';
    html += `<select id="phase-runner-select-${escAttr(phaseId)}" style="width:100%;padding:6px 8px;font-size:12px;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;color:var(--text-1);font-family:inherit;outline:none">`;
    html += '<option value="">(inherit default_runner)</option>';
    _supportedRunners.forEach(runner => {
      html += `<option value="${escAttr(runner.name)}"${currentRunner === runner.name ? ' selected' : ''}>${esc(runner.name)}</option>`;
    });
    html += '</select>';
    runnerContainer.innerHTML = html;
  }

  const modelContainer = document.getElementById(`phase-model-${phaseId}`);
  if (modelContainer) {
    let html = '<label style="font-size:10px;color:var(--text-3);display:block;margin-bottom:2px">Model Tier</label>';
    html += `<select id="phase-model-select-${escAttr(phaseId)}" style="width:100%;padding:6px 8px;font-size:12px;background:var(--bg-hover);border:1px solid var(--border);border-radius:6px;color:var(--text-1);font-family:inherit;outline:none">`;
    html += '<option value="">(inherit)</option>';
    // tier 名稱取自實際 runner 設定（內建 preset + 專案覆寫），而非寫死清單，
    // 才會跟 runners.*.tiers / model_tiers 實際可解析的值一致。
    const runnerName = currentRunner || (_projectSettings && _projectSettings.default_runner) ||
      (_supportedRunners[0] && _supportedRunners[0].name) || '';
    const preset = _supportedRunners.find(r => r.name === runnerName);
    const builtinTiers = (preset && preset.config && preset.config.tiers) ? Object.keys(preset.config.tiers) : [];
    const projectRunner = _projectSettings && _projectSettings.runners && _projectSettings.runners[runnerName];
    const projectTiers = (projectRunner && projectRunner.tiers) ? Object.keys(projectRunner.tiers) : [];
    const tierNames = [...new Set([...builtinTiers, ...projectTiers])];
    tierNames.forEach(tier => {
      html += `<option value="${escAttr(tier)}"${currentModel === tier ? ' selected' : ''}>${esc(tier)}</option>`;
    });
    html += '</select>';
    modelContainer.innerHTML = html;
  }
}

// updatePhaseSelection — 在複選框變更時更新
function updatePhaseSelection(phase) {
  // 確保 coding 始終勾選
  if (phase === 'coding') {
    const checkbox = document.getElementById('phase-coding');
    if (checkbox) checkbox.checked = true;
  }
}

function initProfileDragSort() {
  const list = document.getElementById('profile-editor-phases');
  if (!list) return;

  list.querySelectorAll('[data-phase-row]').forEach(row => {
    row.addEventListener('dragstart', e => {
      _profileDragSource = row;
      row.style.opacity = '0.5';
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', row.dataset.phase || '');
      }
    });
    row.addEventListener('dragend', () => {
      row.style.opacity = '';
      _profileDragSource = null;
    });
    row.addEventListener('dragover', e => {
      e.preventDefault();
      if (!_profileDragSource || _profileDragSource === row) return;
      const rect = row.getBoundingClientRect();
      const before = e.clientY < rect.top + rect.height / 2;
      if (before) {
        list.insertBefore(_profileDragSource, row);
      } else {
        list.insertBefore(_profileDragSource, row.nextSibling);
      }
    });
    row.addEventListener('drop', e => e.preventDefault());
  });
}

// saveProfile — 呼叫 PUT /api/settings/profiles/{name} 儲存
async function saveProfile() {
  const nameInput = document.querySelector('#profile-editor-name');
  const name = nameInput ? nameInput.value.trim() : '';
  
  if (!name) {
    showToast(t('projectSettings.profileNameRequired'));
    return;
  }

  const phases = [];
  const rows = document.querySelectorAll('#profile-editor-phases [data-phase-row]');
  rows.forEach(row => {
    const phase = row.dataset.phase || '';
    if (!phase) return;
    const checkbox = row.querySelector(`input[type="checkbox"]`);
    if (!checkbox || !checkbox.checked) return;
    const runner = (document.getElementById(`phase-runner-select-${phase}`) || {}).value || '';
    const model = (document.getElementById(`phase-model-select-${phase}`) || {}).value || '';
    const spec = { phase };
    if (runner) spec.runner = runner;
    if (model) spec.model = model;
    phases.push(spec);
  });

  const payload = { phases };

  try {
    const resp = await fetch(apiBase() + `/api/settings/profiles/${encodeURIComponent(name)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      showToast(t('projectSettings.saveFailed').replace('{error}', await resp.text()));
      return;
    }
    _projectSettings = await resp.json();
    closeProfileEditor();
    renderProfilesTab();
    showToast(t('projectSettings.profileSaved'));
  } catch(err) {
    showToast(t('toast.connectionError').replace('{error}', err.message));
  }
}

// deleteProfile — 刪除 profile，需確認
async function deleteProfile(name) {
  if (!confirm(t('projectSettings.deleteProfileConfirm').replace('{name}', name))) return;

  try {
    const resp = await fetch(apiBase() + `/api/settings/profiles/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    });
    if (!resp.ok) {
      showToast(t('projectSettings.deleteFailed').replace('{error}', await resp.text()));
      return;
    }
    _projectSettings = await resp.json();
    renderProfilesTab();
    showToast(t('projectSettings.profileDeleted'));
  } catch(err) {
    showToast(t('toast.connectionError').replace('{error}', err.message));
  }
}
