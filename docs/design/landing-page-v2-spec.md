# Landing Page v2 — Design Spec

## 背景

`docs/index.html`（GitHub Pages landing page）內容停留在 v0.2.0 vintage（最後一次修改是
2026-06-22 的「rename brew formula」commit），目前版本已是 v0.3.7，中間 15 個版本沒有反映在
landing page 上。v0.3.0 之後新增的重點功能（multi-repo/worktree、AC evidence mapping、
selective deep review、learnings 自動整併、cost per phase）完全沒有被提及。

## 目標

產出一份 v2 草稿頁面供使用者審閱，尚不決定是否覆蓋 v1；使用者看過草稿後再決定要不要把 v2
換成正式版。

## 現況盤點

- Hero 區塊統計數字寫死：`72+ Features Shipped`（實際 `.4x/features/` 有 107 個 feature，
  104 個 done）、`6 AI Runners`（仍準確：Claude Code / Codex / Gemini / Copilot / Cursor /
  Antigravity）、`3 Platforms`（未變動）。
- 6 語言 i18n（en/ja/ko/es/zh-TW/zh-CN）內嵌在同一個 `<script>` 的物件裡，**沒有任何自動化
  檢查**覆蓋這個檔案——`make check-i18n` 只查 `dashboard/web/locales`，
  `make check-guide-i18n` 只查 `docs/guide/`。每次改文案都要手動同步各語言版本。
- Section 順序（v1）：Hero → Why 4x（4 張卡）→ How It Works → Quick Start → Config
  Examples → Demo → Dashboard → Supported Runners → Self-Evolution → Footer。
- `docs/assets/` 目前沒有 multi-repo/worktree、quality gates、cost-per-phase 的專屬截圖，
  只有既有 dashboard 截圖（`dashboard-overview.png`/`dashboard-messages.png`/
  `dashboard-logs.png`/`dashboard-run.png`）。
- Supported Runners section 的 Antigravity icon（`docs/index.html:414`）有一個孤兒
  `</svg>` 收尾標籤（`<img ...></svg>`，`<img>` 前面沒有對應的 `<svg>` 開標籤），屬既有
  markup 小 bug，順手修掉。

## 方案決策（已與使用者確認）

1. **範圍**：全面改版（非只修過時事實），narrative 與版面都可調整。
2. **v2 i18n 範圍**：先只做 `en` + `zh-TW`，其餘 4 語言待確定要把 v2 換成正式版後再補齊。
3. **要主打的新賣點**（v0.3.0+）：multi-repo/worktree、self-evolution/learnings、quality
   gates（AC evidence mapping + selective deep review）、cost 可視化（cost per phase）。
4. **視覺風格**：沿用現有暗色 tech 風（dark theme + gradient text + code-block 視覺語言），
   不重新設計配色/字型/版面骨架。
5. **新賣點編排**：每個新賣點各自獨立成一個完整 section（比照現有 Dashboard section 的
   規格——標題 + 說明 + 範例內容），而非塞進既有 Why 4x 卡片網格，也不是全部揉在一個
   section 裡。

## 設計

### 檔案與技術方式

新建 `docs/index-v2.html`，獨立於現有 `docs/index.html`（v1 完全不動，仍是正式對外頁面）。
沿用現有技術棧：單一 HTML 檔、Tailwind CDN、inline `<script>` i18n dict，暗色 tech 視覺
風格不變。

### Section 順序

```
Hero（更新統計數字）
Why 4x（4 張卡，原封不動）
[NEW] Multi-repo & Worktree Isolation
[NEW] Quality Gates（AC evidence + selective deep review）
[NEW] Cost Visibility（cost per phase）
How It Works（不動）
Quick Start（不動）
Config Examples（不動）
Demo（不動）
Dashboard（不動）
Supported Runners（不動；修 Antigravity icon 孤兒 `</svg>` 標籤）
Self-Evolution（內容強化：補 learnings 自動整併細節，不新增卡片/不改版面）
Footer（不動）
```

3 個新 section 集中放在「Why 4x」後面：4 張卡先勾住人，緊接著用 3 個完整 section 證明
「這是為真實專案打造的」，再進入 How It Works 講機制、Quick Start 讓人動手試。

### Hero 統計數字

`72+ Features Shipped` → `100+`（用整數而非精確的 107，減少之後每次改動都要回頭更新這個
數字的維護成本）。`6 AI Runners`、`3 Platforms` 維持不變。

### 三個新 section 內容

- **Multi-repo & Worktree Isolation**：說明 workspace 可橫跨多個獨立 git repo + hub_repo
  （如共用 docs），每個 feature 在自己的 git worktree 裡跑，互不干擾。用
  `settings.json` 的 `workspace.repos` 設定片段當範例程式碼（比照 Config Examples
  section 的 code-block 呈現方式，不是截圖——目前沒有現成的 multi-repo 截圖）。
- **Quality Gates**：說明每個 acceptance criterion 都強制要有 evidence（`verify.json` 的
  `ac_results`），deep review 依 diff 影響範圍自動選角度而非每次跑全部 11 個。用
  `verify.json` 片段當範例程式碼。
- **Cost Visibility**：說明 dashboard 顯示每個 phase 的 token 消耗與耗時。v2 草稿先用
  現有 `dashboard-overview.png` 佔位；若之後決定把 v2 換成正式版，再補一張聚焦 cost 的
  專屬截圖。

### Self-Evolution 強化

在既有 3 張卡（History Mining / Value Gate / Evolve Driver）之外，文案補一句提到
「learnings 超過 30 條時自動用 AI 判斷語意重複並整併」。不新增卡片、不改版面。

### i18n

v2 只做 `en` + `zh-TW` 兩語言。其餘 4 語言（ja/ko/es/zh-CN）留到確定要把 v2 換成正式版
之後再補齊，避免文案還在改就先翻 6 份白工。

## 不做的事

- 不改配色、字型、版面骨架（沿用暗色 tech 風）。
- 不動 v1（`docs/index.html`），v2 是獨立檔案供審閱。
- 不在這次順便補齊 ja/ko/es/zh-CN 翻譯。
- 不因為這次改版去動 `docs/guide/` 底下的完整使用手冊（已有獨立的 i18n 同步檢查機制）。

## 驗收方式

- `docs/index-v2.html` 可獨立開啟（本機或 GitHub Pages preview），版面/新 section 呈現
  正常，語言切換（en/zh-TW）正常運作。
- Hero 統計數字、6 個新賣點相關文案在 en/zh-TW 兩語言都有對應翻譯，無殘留佔位字串。
- Supported Runners section 的 Antigravity icon markup 修正後，SVG/圖片正常顯示，無
  瀏覽器 console 錯誤。
