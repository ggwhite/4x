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
