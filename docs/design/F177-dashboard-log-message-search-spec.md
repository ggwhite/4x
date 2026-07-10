# F177 — Dashboard log/message 搜尋功能

## 概述

4x dashboard 是 AppKit + WKWebView 外殼（`dashboard/macos/Sources/main.swift`），實際 UI 是內嵌 Go
server 提供的原生 JS + HTML（`dashboard/web/`），非 SwiftUI。目前 log 檢視區（`#log-viewer`，SSE
持續累加的大段純文字）與 message 時間軸（`#messages`，`renderMsgCard` 產出的可展開卡片）都沒有搜尋
功能，使用者在長 log 或大量訊息中找內容只能手動捲動。

本 feature 純前端 client-side 實作，只搜尋「目前已載入」的內容，不新增後端 API，不做跨檔案/跨 round
的伺服器端搜尋。

## 架構總覽

新增 `dashboard/web/search.js`，放兩個獨立但共用 substring-match 工具函式的模組：`LogSearch`（Ctrl+F
風格）與 `MessageSearch`（卡片過濾）。

`index.html` 在 `#logs-panel` 和 `#messages` 上方各加一個常駐 `<input>` 搜尋框（log 區另外加計數文字
與上一個/下一個按鈕）。

`ui.js` 既有的 render 收尾處呼叫對應的 `reapply()`/`clear()`：

- `viewLog()`、`connectLogSSE` 收到新內容、`renderMultiLog()` → `LogSearch.reapply()`
- `loadMessages()` 增量渲染收尾 → `MessageSearch.reapply()`
- 切換 log 檔案 / 切換 feature（`renderedMsgKeys` 被清空重建）→ 對應 `clear()`

比對邏輯統一：不分大小寫（`toLowerCase().includes()`），不支援 regex（YAGNI），debounce 150ms 後才
重新計算，避免長內容每個按鍵都重繪卡頓。

## Log 區搜尋（Ctrl+F 風格）

**UI**：`#logs-panel` 上方 `<input id="log-search-input">` + 計數 `<span id="log-search-count">3/12</span>`
+ 上一個/下一個按鈕，支援 Enter/Shift+Enter 快捷鍵切換。

**資料來源**：
- 單檔模式：`#log-viewer` 目前的 `textContent`（`viewLog()` 抓下來的全文 + SSE 陸續 append 的內容）。
- Multi-log 並列模式：`multiLogBuffers`（`Record<string,string>`），每個檔案的 buffer 各自跑一次比對，
  「上一個/下一個」跨檔案連續編號（例如檔案 A 3 筆 + 檔案 B 5 筆 = 共 8 筆，可跳過檔案邊界），計數
  顯示「目前第幾筆 / 總筆數」。

**高亮實作**：找到所有匹配位置後，把該檔案文字依位置切段，命中處包 `<mark>`，目前選中的那一筆額外加
class `mark.current`（例如黃底變橘底），`textContent` 換成組好的 `innerHTML`。切換到下一筆時呼叫
`document.querySelector('mark.current').scrollIntoView({block:'center'})`。

不採用瀏覽器原生 `window.find()`：該 API 各瀏覽器行為不一致、無法精準控制「目前第幾筆/上一筆下一筆」，
且 WKWebView 支援度不穩定。

**邊界情況**：
- 清空搜尋框（或切換到別的 log 檔）時呼叫 `LogSearch.clear()`，把 `innerHTML` 還原成純 `textContent`。
  這一步是必要的：SSE append 邏輯用 `textContent +=`，若當下是含 `<mark>` 的 `innerHTML` 混合狀態，
  直接 append 會把 DOM 結構弄壞，必須先復原成純文字。
- debounce callback 觸發時若目標容器已被替換（切換到別的 log/feature），直接 no-op return，不噴錯。
- 超長 log（數萬行）：`<mark>` 重建是整段字串一次做完，若之後發現效能問題（例如單檔 > 5MB）再考慮
  分段處理；目前先不預先優化，屬已知限制。

## Message 區搜尋（過濾隱藏）

**UI**：`#messages` 上方 `<input id="msg-search-input">` + 計數（例如「顯示 4/17 則」）+ 空結果提示
`<div id="msg-search-empty">`（預設 `hidden`）。

**資料來源**：比對來源不是 DOM 文字，而是 `loadMessages()` 抓回來、存在記憶體裡的 `data.messages`
陣列本身（每筆含 `role`/`label`/`content`/`round`...）。這樣即使卡片是收合狀態、`marked.parse()`
還沒把 markdown 轉成 HTML，一樣能比對到完整內容。

**比對邏輯**：對每筆 message 做
`(m.content + ' ' + m.role + ' ' + m.label).toLowerCase().includes(query)`。

**過濾實作**：不匹配的卡片（既有 DOM 節點，`renderMsgCard` 產出、有 `data-msg-key`）直接
`style.display = 'none'`，不重新渲染整個列表，保留其他卡片的展開/收合狀態。空字串時全部還原
`display=''`。所有卡片都被過濾掉時顯示 `#msg-search-empty`「沒有符合「xxx」的訊息」。

**邊界情況**：
- 切換 feature/round 時呼叫 `MessageSearch.clear()`，避免殘留上一個 feature 的搜尋比對結果誤套到
  新資料上（輸入框文字本身保留，方便使用者切換後沿用同關鍵字，會重新算一次 match）。

## 錯誤處理 / 邊界情況彙總

- 空關鍵字：兩區都直接還原原始顯示，不特別處理成錯誤。
- 特殊字元關鍵字（`.` `*` `(` 等）：用 `includes()` 而非 regex，字面比對，不需要 escape。
- 兩區的 `reapply()`/`clear()` 都是冪等的，用當下 DOM 是否還存在該 id 做 guard。

## 測試策略（Playwright）

Feature YAML 已設 `profile: dashboard`，Coder 完成後由 Tester 依 `templates/profiles/web.md` 既有
web profile 規範跑 Playwright（headless、隔離 workspace、隨機 port 起獨立 `4x live`，測試腳本存
`.4x/run/F177-dashboard-log-message-search/e2e/`，截圖存對應 screenshot 目錄，未安裝時先
`npx playwright install chromium`）。

驗收項目（每項至少一張截圖佐證）：

- **AC-1**：Log 單檔模式輸入關鍵字 → 命中處變黃底，計數顯示正確，Enter/按鈕可跳下一筆並捲動置中
- **AC-2**：Log SSE 持續輸出時，搜尋高亮不因新內容 append 而消失或錯位
- **AC-3**：Multi-log 並列模式下搜尋可跨檔案連續跳轉，計數含所有檔案總和
- **AC-4**：Message 輸入關鍵字 → 不符卡片隱藏、符合的維持原本收合/展開狀態不變
- **AC-5**：Message 搜尋比對含 role/label（例如打「coder」能篩出該角色訊息）
- **AC-6**：清空關鍵字兩區都完整還原原始顯示
- **AC-7**：切換 log 檔案或切換 feature 時搜尋狀態正確重置，無 console error

## 不做的事

- Regex 搜尋
- 跨檔案/跨 round 的伺服器端全域搜尋（目前只搜已載入內容）
- 新增後端 API 或修改 SSE 協定
- 修改 macOS AppKit 外殼（`Sources/main.swift`）
- 搜尋歷史 / 最近搜尋記錄
