// search.js — dashboard 前端 client-side 搜尋。
//
// 兩個獨立模組，只搜尋「目前已載入」的內容，不打後端：
//   - LogSearch：Ctrl+F 風格，於 #log-viewer 命中處包 <mark> 高亮，支援上一個/下一個
//     跨檔案連續跳轉並 scrollIntoView 置中；資料來源為單檔 textContent 或 multiLogBuffers。
//   - MessageSearch：比對記憶體中的 message 文字（content + role + label），不符卡片直接
//     display:none 隱藏，保留其他卡片展開/收合狀態。
//
// 兩者共用不分大小寫 substring 比對（toLowerCase().includes()，不支援 regex），輸入以
// debounce 150ms 收斂避免長內容每個按鍵都重繪。reapply()/clear() 皆冪等，用當下 DOM 是否
// 還存在對應 id 做 guard，容器被替換（切換到別的 log/feature）時直接 no-op。

const SEARCH_DEBOUNCE_MS = 150;

// findAllMatches 回傳 needle 在 haystack 中所有不重疊命中的起始 index（不分大小寫）。
function findAllMatches(haystack, needle) {
  const positions = [];
  if (!needle) return positions;
  const hay = haystack.toLowerCase();
  const nee = needle.toLowerCase();
  let i = 0;
  while ((i = hay.indexOf(nee, i)) !== -1) {
    positions.push(i);
    i += nee.length;
  }
  return positions;
}

// buildMarkedHTML 依命中位置把 text 切段，命中處包 <mark>，非命中段用 esc() 跳脫，
// 產出可直接塞進 innerHTML 的字串（目前選中筆的 .current class 由 _highlightCurrent 事後套用）。
function buildMarkedHTML(text, positions, len) {
  let html = '';
  let last = 0;
  for (const p of positions) {
    html += esc(text.slice(last, p));
    html += '<mark>' + esc(text.slice(p, p + len)) + '</mark>';
    last = p + len;
  }
  html += esc(text.slice(last));
  return html;
}

const LogSearch = {
  query: '',
  matches: [],   // [{file, pos}]，file 為 null 代表單檔模式；順序即 DOM 內 <mark> 順序
  current: -1,   // 目前選中筆在 matches 中的 index

  // reapply 依目前搜尋框內容重算命中並重建高亮。scroll=true 時捲動到目前選中筆。
  // 供 viewLog（切檔）、SSE append 新內容、renderMultiLog 收尾呼叫；query 為空則還原。
  reapply(scroll) {
    const viewer = document.getElementById('log-viewer');
    if (!viewer) return;
    const input = document.getElementById('log-search-input');
    const q = input ? input.value : '';
    this.query = q;
    if (!q) { this.clear(); return; }
    // 記住重算前目前選中筆的身份（file + pos）。SSE 對「非最後排序檔案」append 新命中時，
    // matches 會在中段插入，若只保留數字 current 會靜默指向另一段文字；重算後用此身份找回
    // 新 index，找不到才 fallback。單檔模式因命中一律尾端追加，身份不位移故行為不變。
    const prev = (this.current >= 0 && this.current < this.matches.length) ? this.matches[this.current] : null;
    const matches = [];
    const multi = (typeof multiLogActive !== 'undefined' && multiLogActive);
    if (multi) {
      const files = Object.keys(multiLogBuffers).sort();
      let html = '';
      files.forEach(f => {
        const buf = multiLogBuffers[f] || '';
        const positions = findAllMatches(buf, q);
        positions.forEach(pos => matches.push({ file: f, pos }));
        html += logMultiSectionHTML(f, buildMarkedHTML(buf, positions, q.length));
      });
      viewer.innerHTML = html;
    } else {
      const text = viewer.textContent;
      const positions = findAllMatches(text, q);
      positions.forEach(pos => matches.push({ file: null, pos }));
      viewer.innerHTML = buildMarkedHTML(text, positions, q.length);
    }
    this.matches = matches;
    if (matches.length === 0) {
      this.current = -1;
    } else if (prev) {
      const idx = matches.findIndex(m => m.file === prev.file && m.pos === prev.pos);
      this.current = idx >= 0 ? idx : 0;
    } else {
      this.current = 0;
    }
    this._highlightCurrent(scroll);
    this._updateCount();
  },

  // clear 還原純顯示：單檔模式把含 <mark> 的 innerHTML 壓回純 textContent（避免破壞
  // SSE 的 textContent += 累加），多檔模式改交回 renderMultiLog 重繪乾淨內容。
  clear() {
    this.query = '';
    this.matches = [];
    this.current = -1;
    const viewer = document.getElementById('log-viewer');
    if (viewer) {
      if (typeof multiLogActive !== 'undefined' && multiLogActive) {
        if (typeof renderMultiLog === 'function') renderMultiLog();
      } else if (viewer.querySelector('mark')) {
        viewer.textContent = viewer.textContent;
      }
    }
    this._updateCount();
  },

  // resetPosition 讓下次 reapply 從第一筆重新開始（切換 log 檔案時用）。
  resetPosition() { this.current = -1; },

  next() {
    if (!this.matches.length) return;
    this.current = (this.current + 1) % this.matches.length;
    this._highlightCurrent(true);
    this._updateCount();
  },

  prev() {
    if (!this.matches.length) return;
    this.current = (this.current - 1 + this.matches.length) % this.matches.length;
    this._highlightCurrent(true);
    this._updateCount();
  },

  _highlightCurrent(scroll) {
    const viewer = document.getElementById('log-viewer');
    if (!viewer) return;
    const marks = viewer.querySelectorAll('mark');
    marks.forEach((el, i) => el.classList.toggle('current', i === this.current));
    if (scroll && this.current >= 0 && marks[this.current]) {
      marks[this.current].scrollIntoView({ block: 'center' });
    }
  },

  _updateCount() {
    const el = document.getElementById('log-search-count');
    if (!el) return;
    const total = this.matches.length;
    el.textContent = total ? `${this.current + 1}/${total}` : (this.query ? '0/0' : '');
  },
};

const MessageSearch = {
  query: '',

  // reapply 依搜尋框內容過濾 #messages 內的卡片：不符者 display:none，符合者維持原本
  // 展開/收合狀態；全部被濾掉時顯示空結果提示。供 loadMessages 增量渲染收尾呼叫。
  reapply() {
    const container = document.getElementById('messages');
    if (!container) return;
    const input = document.getElementById('msg-search-input');
    const q = input ? input.value.trim().toLowerCase() : '';
    this.query = q;
    const cards = container.querySelectorAll('[data-msg-key]');
    const total = cards.length;
    let visible = 0;
    cards.forEach(card => {
      if (!q) { card.style.display = ''; visible++; return; }
      const key = card.dataset.msgKey;
      const text = (typeof msgSearchText !== 'undefined' && msgSearchText.get(key)) || card.textContent.toLowerCase();
      const match = text.includes(q);
      card.style.display = match ? '' : 'none';
      if (match) visible++;
    });
    this._updateCount(visible, total, q);
  },

  // clear 隱藏空結果提示並清掉計數（不動搜尋框文字，方便切換 feature 後沿用同關鍵字，
  // 由後續 loadMessages → reapply 重新計算 match）。
  clear() {
    this.query = '';
    const countEl = document.getElementById('msg-search-count');
    if (countEl) countEl.textContent = '';
    const emptyEl = document.getElementById('msg-search-empty');
    if (emptyEl) emptyEl.classList.add('hidden');
  },

  _updateCount(visible, total, q) {
    const countEl = document.getElementById('msg-search-count');
    if (countEl) countEl.textContent = q ? t('search.msgCount').replace('{visible}', visible).replace('{total}', total) : '';
    const emptyEl = document.getElementById('msg-search-empty');
    if (emptyEl) {
      if (q && total > 0 && visible === 0) {
        emptyEl.textContent = t('search.msgEmpty').split('{query}').join(q);
        emptyEl.classList.remove('hidden');
      } else {
        emptyEl.classList.add('hidden');
      }
    }
  },
};

// initSearch 綁定兩區搜尋框的事件（debounce 輸入、Enter/Shift+Enter 切換、上/下按鈕）。
// 搜尋框為 index.html 靜態節點，script 置於 body 末端故載入時必定存在。
function initSearch() {
  const logInput = document.getElementById('log-search-input');
  if (logInput) {
    let logTimer = null;
    logInput.addEventListener('input', () => {
      clearTimeout(logTimer);
      logTimer = setTimeout(() => { LogSearch.resetPosition(); LogSearch.reapply(true); }, SEARCH_DEBOUNCE_MS);
    });
    logInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        if (e.shiftKey) LogSearch.prev(); else LogSearch.next();
      }
    });
    const prevBtn = document.getElementById('log-search-prev');
    const nextBtn = document.getElementById('log-search-next');
    if (prevBtn) prevBtn.addEventListener('click', () => LogSearch.prev());
    if (nextBtn) nextBtn.addEventListener('click', () => LogSearch.next());
  }

  const msgInput = document.getElementById('msg-search-input');
  if (msgInput) {
    let msgTimer = null;
    msgInput.addEventListener('input', () => {
      clearTimeout(msgTimer);
      msgTimer = setTimeout(() => MessageSearch.reapply(), SEARCH_DEBOUNCE_MS);
    });
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initSearch);
} else {
  initSearch();
}
