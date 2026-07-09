// Live session bearer token: 於載入時從 URL 的 ?token= 讀出、存記憶體變數（非 localStorage，
// 避免跨分頁殘留），並立即用 history.replaceState 從網址列移除（避免經 Referer / 書籤外洩）。
let liveToken = '';
(function bootstrapLiveToken() {
  try {
    const params = new URLSearchParams(location.search);
    const tok = params.get('token');
    if (tok) {
      liveToken = tok;
      history.replaceState({}, '', location.pathname);
    }
  } catch (e) { /* no-op */ }
})();

// sseUrl 為 EventSource（無法帶 header）附加 ?token=：liveToken 非空時 append，
// 既有 URL 已含 '?' 則用 '&token='，否則 '?token='；空則原樣回傳。
function sseUrl(url) {
  if (!liveToken) return url;
  return url + (url.indexOf('?') >= 0 ? '&token=' : '?token=') + encodeURIComponent(liveToken);
}

// 覆寫 window.fetch：liveToken 非空且目標為 dashboard 端點（相對路徑或以 /api/、/sse/ 開頭，
// 且非公開豁免路徑 /api/version、/api/locales*）時，注入 Authorization: Bearer header。
(function wrapFetch() {
  const nativeFetch = window.fetch.bind(window);
  function isDashboardTarget(url) {
    if (typeof url !== 'string') return false;
    if (/^https?:\/\//i.test(url)) return false; // 絕對外部 URL 不注入
    if (url.startsWith('/api/version')) return false;
    if (url === '/api/locales' || url.startsWith('/api/locales/')) return false;
    return url.startsWith('/api/') || url.startsWith('/sse/') || !url.startsWith('/');
  }
  window.fetch = function (input, init) {
    const target = typeof input === 'string' ? input : (input && input.url);
    if (liveToken && isDashboardTarget(target)) {
      init = init || {};
      const headers = new Headers(init.headers || (typeof input === 'object' && input && input.headers) || {});
      if (!headers.has('Authorization')) headers.set('Authorization', 'Bearer ' + liveToken);
      init.headers = headers;
    }
    return nativeFetch(input, init);
  };
})();

// i18n
let _i18nDict = {};
let _currentLocale = 'en';
const SUPPORTED_LOCALES = [];
const LOCALE_NAMES = {
  en: 'English', 'zh-TW': '繁體中文', 'zh-CN': '简体中文',
  ja: '日本語', ko: '한국어', es: 'Español'
};

function t(key) {
  return _i18nDict[key] || key;
}

function detectLocale() {
  const nav = navigator.language || 'en';
  if (LOCALE_NAMES[nav]) return nav;
  const prefix = nav.split('-')[0];
  const match = Object.keys(LOCALE_NAMES).find(k => k.startsWith(prefix));
  return match || 'en';
}

async function loadLocale(lang) {
  try {
    const resp = await fetch('/api/locales/' + lang);
    if (resp.ok) {
      _i18nDict = await resp.json();
      _currentLocale = lang;
      return true;
    }
  } catch {}
  if (lang !== 'en') return loadLocale('en');
  return false;
}

function applyI18n() {
  document.querySelectorAll('[data-i18n]').forEach(el => {
    el.textContent = t(el.dataset.i18n);
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    el.placeholder = t(el.dataset.i18nPlaceholder);
  });
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    el.title = t(el.dataset.i18nTitle);
  });
}

async function switchLocale(lang) {
  const loaded = await loadLocale(lang);
  if (!loaded) return;
  applyI18n();
  document.documentElement.lang = lang;
  if (current && activeProjectId) {
    const task = lastTasks.find(f => f.id === current);
    if (task) {
      const tab = activeDetailTab;
      await loadDetail(task);
      switchDetailTab(tab);
    }
  } else if (activeProjectId) {
    renderDashboard(lastTasks);
  } else {
    renderProjectPicker();
  }
  if (activeProjectId) renderSidebar();
}

// Global state
let projects = [];
let activeProjectId = null;
let openTabs = [];
let allTabTasks = {};
let current = sessionStorage.getItem('4x-current') || null;
let lastTasks = [];
const renderedMsgKeys = new Map();
let sseSource = null;
let _lastNotifyKey = null;
let searchIdx = 0;
let searchFiltered = [];
let refreshTimer = null;
let activeRuns = [];
let lastBatchStatus = null;

function showToast(msg, type='error', duration=5000) {
  const c = document.getElementById('toast-container');
  const el = document.createElement('div');
  el.className = `toast toast-${type}`;
  el.textContent = msg;
  c.appendChild(el);
  setTimeout(() => { el.style.opacity='0'; el.style.transition='opacity .3s'; setTimeout(() => el.remove(), 300); }, duration);
}

function apiBase() { return activeProjectId ? '/api/project/' + activeProjectId : ''; }
function sseBase() { return activeProjectId ? '/sse/project/' + activeProjectId : ''; }
function saveTabState() { localStorage.setItem('4x-tabs', JSON.stringify({ tabs: openTabs, active: activeProjectId })); }
function loadTabState() { try { const s = JSON.parse(localStorage.getItem('4x-tabs') || '{}'); return { tabs: s.tabs || [], active: s.active || null }; } catch { return { tabs: [], active: null }; } }

// BUILTIN_PROFILES — 內建 pipeline profile，對應 internal/protocol/profile.go 的 DefaultProfiles()。
// 專案 settings.json 沒自訂 profiles 時，這些永遠可選。
const BUILTIN_PROFILES = ['full', 'lite', 'normal', 'quick'];

const DEFAULTS = { theme: 'frost', fontContent: 15, fontCode: 13, refresh: 3 };
let settings = { ...DEFAULTS, ...JSON.parse(localStorage.getItem('4x-settings') || '{}') };
function saveSettings() { localStorage.setItem('4x-settings', JSON.stringify(settings)); }

const THEMES = [
  { id: 'apple-dark', name: 'Apple Dark', bg: '#0f0f0f', fg: '#e5e5e5', line: '#333' },
  { id: 'midnight',   name: 'Midnight',   bg: '#0a0e1a', fg: '#c8d6e5', line: '#1e3a5f' },
  { id: 'noir',       name: 'Noir',       bg: '#000000', fg: '#a0a0a0', line: '#222' },
  { id: 'frost',      name: 'Frost',      bg: '#1e1e20', fg: '#c8c8cd', line: '#3a3a40' },
  { id: 'light',      name: 'Light',      bg: '#f5f5f5', fg: '#18181b', line: '#ddd' },
  { id: 'paper',      name: 'Paper',      bg: '#faf8f5', fg: '#1c1917', line: '#d6cfc7' },
];

const S2T_S = '万与丑专业丛东丝丢两严丧个丰临为丽举么义乌乐乔习书买乱争亏云亘亚产亩亲亿仅从仑仓仪们价众优伙会伛伞伟传伤伥伦伧伪伫体余佣佥侠侣侥侦侧侨侩侪侬俣俦俭债倾偬偻偾傥傧储催傻像僵党兜兰关兴兹养兽冁内冈冉写军农冯冲决况冻净凉凌减凑凤凫凭凯击凿刍划刘则刚创删别刬刭刮制刹券刺刻剀剂剑剥剧劝办务劢动励劲劳势勋勐勚匀匦匮区医华协单卖卢卤卧卫却卺厂厅历厉压厌厍厕厘厢厣厦厩厮县叁参双发变叙叠只台叹叽吁后吓吕吗吣吨听启吴呐呒呓呕呖呗员呙呛呜咏咙咛咝咤咸响哑哒哓哔哕哗哙哜哝哟唛唝唠唡唢唣唤唦唿啧啬啭啮啯啰啴啸喂喷嗫嗳嘘嘤嘱噜噼嚣嚯园困围囱坏坚坛坜坝坞坟坠垄垅垆垒垦垧垩垫垭垯垲垴埘埙埚埝域堑堕塆墙壮声壳壶壸处备复够头夸夹夺奁奂奋奖奥妆妇妈妩妪姗姜娄娅娆娇娈娱婴婵婶媪嫒嫔嫱嬷孙学孪宁宝实宠审宪宫宽宾寝对寻导寿将尔尘尝尧尴层屃屉届属屡屦屿岁岂岖岗岘岙岚岛岭岽岿峃峄峡峣峤峥崂崃崄崭崱嵘嵚嵝巅巩巯币帅师帏帐帘帜帧帮帱帻帼幂干并广庄庆庇床庐庑库应庙庞废廪开异弃弑张弥弦弯弹强归当录彝彟彦彩彻径徕御忆忏忧忾怀态怂怃怄怅怆怜总怼怿恋恳恶恸恹恻恼恽悍悦悫悬悭悯惊惧惨惩惫惬惭惮惯愠愤愦愿慑慭憷懑懒懔懵戆戋戏戗战戬户扑扦执扩扪扫扬扰找承抚抟抠抡抢护报担拟拢拣拥拦拧拨择挂挚挛挜挝挞挟挠挡挣挤挥挦捞损捡换捣据捻掳掴掷掸掺掼揽搀搁搂搅携摄摅摆摇摈摊撑撵撷撸撺擞攒敌敛数斋斓斩断无旧时旷旸昙昼显晋晒晓晔晕晖暂暧术朮杂权条来杨杩杰极构枞枢枣枥枧枨枪枫枭柜柠柽栀栅标栈栉栊栋栌栎栏树栖样栾桠桡桢档桤桥桦桧桨桩梦梼梾检棁棂椁椟椠椤椭楼榄榆榇榈榉槚槛槟槠横樯樱橥橱橹机杀杂权杆条杰板松极构析枪果柜某样根格桂档案桥梁梦检楼标横模';
const S2T_T = '萬與醜專業叢東絲丟兩嚴喪個豐臨為麗舉麼義烏樂喬習書買亂爭虧雲亙亞產畝親億僅從崙倉儀們價眾優夥會傴傘偉傳傷倀倫傖偽佇體餘傭僉俠侶僥偵側僑儈儕儂俁儔儉債傾傯僂憤傖儲催傻像僵黨兜蘭關興茲養獸囅內岡冉寫軍農馮衝決況凍淨涼凌減湊鳳鳧憑凱擊鑿芻劃劉則剛創刪別剗剄刮製剎券刺刻剴劑劍剝劇勸辦務勱動勵勁勞勢勳猛勚勻匭匱區醫華協單賣盧鹵臥衛卻卺廠廳歷厲壓厭厙廁釐廂厴廈廄廝縣參參雙發變敘疊隻臺嘆嘰籲後嚇呂嗎吣噸聽啟吳吶嘸囈嘔嚦唄員咼嗆嗚詠嚨嚀噝吒鹹響啞噠嘵嗶噦嘩嘖噲嚌噥唲嘜嚜嘮嗊嗩嚸喚唰呼嘖嗇囀齧噯囉嘽嘯餵噴囁噯噓嚶囑嚕劈囂謔園困圍囪壞堅壇壢壩塢墳墜壟壟壚壘墾坰堊墊埡垯塏堖塒塤堝埝域塹墮塆牆壯聲殼壺壼處備複夠頭誇夾奪奩奐奮獎奧妝婦媽嫵嫗姍薑婁婭嬈嬌孌娛嬰嬋嬸媼嬡嬪嬙嬤孫學孿寧寶實寵審憲宮寬賓寢對尋導壽將爾塵嘗堯尷層屓屜屆屬屢屨嶼歲豈嶇崗峴嶴嵐島嶺崠嶸峃嶧峽嶢嶠崢嶗崍嶄嶄崱嶸嶔嶁巔鞏巰幣帥師幃帳簾幟幀幫幬幘幗冪乾並廣莊慶庇床廬廡庫應廟龐廢廩開異棄弒張彌弦彎彈強歸當錄彝彠彥彩徹徑徠禦憶懺憂愾懷態慫憮慪悵愴憐總懟懌戀懇惡慟懨惻惱惲悍悅愨懸慳憫驚懼慘懲憊愜慚憚慣慍憤憒願懾慭憷懣懶懍懵戇戔戲戧戰戩戶撲扦執擴捫掃揚擾找承撫摶摳掄搶護報擔擬攏揀擁攔擰撥擇掛摯攣挾撾撻挾撓擋掙擠揮攈撈損撿換擣據撚擄摑擲撣摻摜攬攙擱摟攪攜攝攄擺搖擯攤撐攆擷擼攛擻攢敵斂數齋斕斬斷無舊時曠暘曇晝顯晉曬曉曄暈暉暫曖術術雜權條來楊榪傑極構樅樞棗櫪梘棖槍楓梟櫃檸檉梔柵標棧櫛櫳棟櫨櫟欄樹棲樣欒椏橈楨檔榿橋樺檜槳樁夢檮棶檢棁欞槨櫝槧欏橢樓欖榆櫬櫚櫸檟檻櫫櫧橫檣櫻櫫櫥櫓機殺雜權桿條傑板鬆極構析槍果櫃某樣根格桂檔案橋樑夢檢樓標橫模';
const _cjkMap = {};
for (let i = 0; i < S2T_S.length && i < S2T_T.length; i++) { _cjkMap[S2T_T[i]] = S2T_S[i]; if (!_cjkMap[S2T_S[i]]) _cjkMap[S2T_S[i]] = S2T_S[i]; }
function normCJK(str) { let out = ''; for (const ch of str) out += _cjkMap[ch] || ch; return out.toLowerCase(); }
function fuzzyScore(query, text) { const nq = normCJK(query), nt = normCJK(text); if (!nq) return 1; if (nt === nq) return 100; if (nt.startsWith(nq)) return 90; const idx = nt.indexOf(nq); if (idx >= 0) return 80 - idx; let qi = 0; for (let ti = 0; ti < nt.length && qi < nq.length; ti++) { if (nt[ti] === nq[qi]) qi++; } return qi === nq.length ? 50 - (nt.length - nq.length) : 0; }
function fuzzyMatch(query, text) { return fuzzyScore(query, text) > 0; }

function formatElapsed(iso) {
  if (!iso) return '';
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 0) return '';
  const s = Math.floor(ms/1000), m = Math.floor(s/60), h = Math.floor(m/60), d = Math.floor(h/24);
  if (d > 0) return `${d}d ${h%24}h`;
  if (h > 0) return `${h}h ${m%60}m`;
  if (m > 0) return `${m}m ${s%60}s`;
  return `${s}s`;
}
function fmtSec(s) {
  if (!s || s <= 0) return '';
  const m = Math.floor(s/60), h = Math.floor(m/60);
  if (h > 0) return `${h}h ${m%60}m`;
  if (m > 0) return `${m}m ${s%60}s`;
  return `${s}s`;
}
function formatDuration(startIso, endIso) {
  let ms;
  if (typeof startIso === 'number') {
    ms = startIso;
  } else {
    if (!startIso || !endIso) return '';
    ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  }
  if (ms < 0) return '';
  const s = Math.floor(ms/1000), m = Math.floor(s/60), h = Math.floor(m/60), d = Math.floor(h/24);
  if (d > 0) return `${d}d ${h%24}h`;
  if (h > 0) return `${h}h ${m%60}m`;
  if (m > 0) return `${m}m ${s%60}s`;
  return `${s}s`;
}
function esc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;'); }
function escAttr(s) { return s.replace(/&/g,'&amp;').replace(/'/g,'&#39;').replace(/"/g,'&quot;'); }

function fmtTokens(n) {
  if (n >= 1e9) return (n/1e9).toFixed(1) + 'B';
  if (n >= 1e6) return (n/1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n/1e3).toFixed(1) + 'K';
  return n.toString();
}

// Constants
const ROLES = {
  designer:{name:'Designer',emoji:'🎨',color:'#c084fc',bg:'rgba(192,132,252,.08)'},
  'design-reviewer':{name:'Design Review',emoji:'📐',color:'#e879f9',bg:'rgba(232,121,249,.08)'},
  coder:{name:'Coder',emoji:'💻',color:'#22d3ee',bg:'rgba(34,211,238,.08)'},
  reviewer:{name:'Reviewer',emoji:'🔍',color:'#4ade80',bg:'rgba(74,222,128,.08)'},
  'deep-reviewer':{name:'Deep Review',emoji:'🔍',color:'#4ade80',bg:'rgba(74,222,128,.08)'},
  tester:{name:'Tester',emoji:'🧪',color:'#fb923c',bg:'rgba(251,146,60,.08)'},
  acceptor:{name:'Acceptor',emoji:'⭐',color:'#facc15',bg:'rgba(250,204,21,.08)'},
  fixer:{name:'Fixer',emoji:'🔧',color:'#f87171',bg:'rgba(248,113,113,.08)'},
  'deep-fix':{name:'Deep Fix',emoji:'🔧',color:'#f472b6',bg:'rgba(244,114,182,.08)'},
  'deep-reverify':{name:'Deep Reverify',emoji:'🔬',color:'#a78bfa',bg:'rgba(167,139,250,.08)'},
  synthesizer:{name:'Synthesizer',emoji:'🧩',color:'#34d399',bg:'rgba(52,211,153,.08)'},
  'mini-coder':{name:'Mini Coder',emoji:'🔩',color:'#67e8f9',bg:'rgba(103,232,249,.08)'},
  're-verifier':{name:'Re-Verifier',emoji:'🔁',color:'#86efac',bg:'rgba(134,239,172,.08)'},
  gate:{name:'Gate',emoji:'🚦',color:'#fbbf24',bg:'rgba(251,191,36,.08)'},
  consolidator:{name:'Consolidator',emoji:'🗂️',color:'#60a5fa',bg:'rgba(96,165,250,.08)'},
  'round-summarizer':{name:'Round Summarizer',emoji:'📋',color:'#94a3b8',bg:'rgba(148,163,184,.08)'},
};
const PHASE_ICON = { designing:'◆','design-reviewing':'◆',coding:'◆',reviewing:'◆','deep-reviewing':'◆',fixing:'◆',testing:'◆',accepting:'◆',amending:'◆','pending-review':'⏳',done:'✓',abandoned:'✕',blocked:'✕','needs-attention':'!',init:'○','not-started':'○' };

const PHASE_COLORS = {
  designing:{letter:'D',color:'#c084fc',bg:'rgba(192,132,252,.15)',border:'rgba(192,132,252,.25)'},
  'design-reviewing':{letter:'DR',color:'#e879f9',bg:'rgba(232,121,249,.15)',border:'rgba(232,121,249,.25)'},
  coding:   {letter:'C',color:'#22d3ee',bg:'rgba(34,211,238,.15)',border:'rgba(34,211,238,.25)'},
  reviewing:{letter:'R',color:'#4ade80',bg:'rgba(74,222,128,.15)',border:'rgba(74,222,128,.25)'},
  'deep-reviewing':{letter:'DR',color:'#22c55e',bg:'rgba(34,197,94,.15)',border:'rgba(34,197,94,.25)'},
  testing:  {letter:'T',color:'#fb923c',bg:'rgba(251,146,60,.15)',border:'rgba(251,146,60,.25)'},
  accepting:{letter:'A',color:'#facc15',bg:'rgba(250,204,21,.15)',border:'rgba(250,204,21,.25)'},
  amending: {letter:'M',color:'#f87171',bg:'rgba(248,113,113,.15)',border:'rgba(248,113,113,.25)'},
};

const SECTION_COLORS = {
  running:{color:'#34d399',bg:'rgba(16,185,129,.2)'},
  review: {color:'#fbbf24',bg:'rgba(245,158,11,.15)'},
  pending:{color:'#60a5fa',bg:'rgba(59,130,246,.15)'},
  todo:   {color:'#c084fc',bg:'rgba(168,85,247,.15)'},
  done:   {color:'#4ade80',bg:'rgba(34,197,94,.1)'},
};

function cap(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : s; }
const RUNNER_COLORS = {claude:'#10b981',codex:'#3b82f6',gemini:'#f59e0b',copilot:'#a78bfa',cursor:'#ec4899'};
function runnerColor(name) {
  return RUNNER_COLORS[name] || '#71717a';
}
function runnerTags(runners) {
  if (!runners || !runners.length) return '';
  return runners.map(r => `<span class="runner-tag" style="border-color:${runnerColor(r)}40;color:${runnerColor(r)}">${esc(cap(r))}</span>`).join(' ');
}

let _versionInfo = null;
async function checkForUpdates() {
  try {
    const resp = await fetch('/api/version?check=true');
    if (!resp.ok) return null;
    _versionInfo = await resp.json();
    renderVersionInfo(_versionInfo);
    return _versionInfo;
  } catch { return null; }
}
