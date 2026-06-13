# F039: Dashboard UI Polish — Match DCT Live Visual Quality

## Goal

將 4x Live dashboard 的視覺品質提升到 DCT Live 的水準。改良 Frost 主題作為預設主題，加入漸層背景、glass 質感、彩色 badge、角色 emoji 等視覺細節。

## Scope

僅修改 `internal/server/static/index.html`（CSS + JS），不動 Go server 程式碼、API、或資料結構。

## 設計決策

### 1. 改良 Frost 主題為預設

- 背景從平面 `#0f172a` 改為 `linear-gradient(180deg, #0c1222, #0f172a, #111827)`
- Sidebar header 加 `backdrop-filter: blur(12px)` + 半透明背景
- Feature detail header 同樣加 glass 效果
- 綠色 pulse dot 加 `box-shadow` 發光
- Frost 設為預設主題（目前預設 Apple Dark）

### 2. Sidebar Feature 卡片

- 加 phase badge：phase 首字母 + round/maxRounds（如 `C2/5` = Coding Round 2 of 5）
- Badge 配色依 phase：
  - Designer (D): 紫色 `#c084fc`
  - Coder (C): 青色 `#22d3ee`
  - Reviewer (R): 綠色 `#4ade80`
  - Tester (T): 橘色 `#fb923c`
  - Acceptor (A): 黃色 `#facc15`
- 卡片背景改為漸層 + glass：`linear-gradient(135deg, rgba(role-color, .06), rgba(15,23,42,.3))` + `backdrop-filter: blur(8px)`
- Running 卡片邊框用角色顏色的低透明度
- Section header 的計數改為彩色 pill badge（如 RUNNING 用綠底綠字、PENDING REVIEW 用橘底橘字）

### 3. 角色標籤加 Emoji

在 ROLES 常數加入 emoji：

| Role | Emoji | Color |
|---|---|---|
| Designer | 🎨 | `#c084fc` (紫) |
| Coder | 💻 | `#22d3ee` (青) |
| Reviewer | 🔍 | `#4ade80` (綠) |
| Deep Reviewer | 🔍 | `#4ade80` (綠) |
| Tester | 🧪 | `#fb923c` (橘) |
| Acceptor | ⭐ | `#facc15` (黃) |

Messages tab 的訊息卡片 header 顯示 emoji + 角色名。

### 4. 訊息卡片（Messages Tab）

- 每個角色訊息用角色顏色的漸層邊框
- 卡片背景：`linear-gradient(135deg, rgba(role-color, .06), rgba(15,23,42, .3))`
- Header 和 body 之間用角色顏色的低透明度 border 分隔
- 統一圓角 12px

### 5. Overview Tab

- Description、Feature Details、Subtasks 各自包在 glass 卡片裡（`rgba(30,41,59,.3)` + border + blur）
- Priority 值用彩色 pill badge
- Repos 用 tag 樣式
- Subtasks 完成項目加刪除線 + 綠色打勾，未完成用空心圓
- Spec/Plan 展開區的 border 風格統一

### 6. 程式碼區塊與 Markdown

- `.md-body pre` 背景從 `rgba(0,0,0,.3)` 加深為 `rgba(0,0,0,.4)`
- `.md-body code` 背景略調深
- 行間距微調

### 7. 整體 Native 感

- 統一圓角：卡片 12px、badge 6-8px、按鈕 8px
- 間距更寬鬆：section 間 24px、卡片內 14-16px padding
- hover 效果加 transition
- 所有主題保留（Apple Dark、Midnight、Noir、Frost、Light、Paper），只改 Frost 的細節和預設值

## 不做的事

- 不加驗證狀態 badge 行（header 的 build ✓ test ✓ 等）
- 不改 API 或資料結構
- 不加新的 tab 或功能
- 不改其他主題（Apple Dark、Midnight 等維持現狀）

## 影響的檔案

- `internal/server/static/index.html` — CSS 變數、ROLES 常數、renderTaskItem、renderSidebar、renderOverview、message 渲染
