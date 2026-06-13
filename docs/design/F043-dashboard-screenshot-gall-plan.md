# F043: Dashboard Screenshot Gallery — Implementation Plan

## Overview

6 個 subtask 依序實作，前 3 個是後端（protocol → settings → server），後 3 個是前端 + CLI。

## Task 1: Protocol Screenshot Type

**Subtask**: `protocol-screenshot-type`
**Files**: `internal/protocol/types.go`, `internal/protocol/workspace.go`, `internal/protocol/workspace_test.go`

### 1.1 新增 Screenshot struct

在 `internal/protocol/types.go` 的 `VerifyEvidence` 之前加：

```go
type Screenshot struct {
    Path        string `json:"path"`
    Step        string `json:"step"`
    Description string `json:"description"`
}
```

### 1.2 VerifyEvidence 加欄位

```go
type VerifyEvidence struct {
    Passed      bool            `json:"passed"`
    Round       int             `json:"round"`
    Role        Role            `json:"role"`
    Commands    []VerifyCommand `json:"commands"`
    Screenshots []Screenshot    `json:"screenshots,omitempty"`
}
```

### 1.3 截圖發現函式

在 `internal/protocol/workspace.go` 新增：

```go
// ScreenshotGroup 是按 round 分組的截圖集合
type ScreenshotGroup struct {
    Round       int          `json:"round"`
    Screenshots []Screenshot `json:"screenshots"`
}

// DiscoverScreenshots 掃描 feature 的截圖，從 verify.json 和目錄兩個來源合併。
func (w *Workspace) DiscoverScreenshots(featureID, screenshotDir string) ([]ScreenshotGroup, error)
```

邏輯：
1. 遍歷 `.4x/{featureID}/rounds/round-*/verify.json`，解析 `screenshots` 欄位，保留 round 號
2. 將 `screenshotDir` 中的 `{feature-id}` 替換為 featureID，掃描 `.png/.jpg/.webp`
3. 目錄掃描到的檔案若不在 verify.json 裡（以 `filepath.Base` 去重），歸入 round 1
4. 目錄掃描到的檔案用檔名解析 step 和 description：`01-sidebar-groups.png` → step="01", description="sidebar groups"
5. 按 round 排序，每組內按 step 排序
6. 目錄不存在不報錯，回傳空

### 1.4 測試

在 `internal/protocol/workspace_test.go` 新增：
- `TestDiscoverScreenshots`：建 temp workspace，放 verify.json 和圖片檔，驗證分組邏輯
- `TestDiscoverScreenshotsNoDir`：目錄不存在時回傳空
- `TestDiscoverScreenshotsMerge`：verify.json 和目錄都有的檔案不重複

### Verify

```bash
go build ./cmd/4x && go vet ./... && go test ./internal/protocol/...
```

---

## Task 2: Settings Screenshot Dir

**Subtask**: `settings-screenshot-dir`
**Files**: `internal/protocol/types.go`（Config 相關）, settings editor UI

### 2.1 Config 型別

在 `RoleConfig` struct 加欄位（如果 RoleConfig 存在的話）。若 roles 是 `map[string]json.RawMessage`，則在讀取時解析 `screenshot_dir`。

查看現有 Config 和 RoleConfig 定義，找到最適合的位置加 `ScreenshotDir string`。

### 2.2 預設值

```go
const DefaultScreenshotDir = ".4x/e2e/{feature-id}/screenshot/"
```

### 2.3 Settings Editor

在 tester role 的設定 UI 加一個 `screenshot_dir` 輸入欄位。讀現有 settings editor 的 pattern（`settings.js`），用同樣方式加欄位。

### Verify

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

---

## Task 3: Server Screenshot API

**Subtask**: `server-screenshot-api`
**Files**: `internal/server/server.go`

### 3.1 註冊路由

在 `NewMux` 加兩個 handler：

```go
mux.HandleFunc("/api/features/", func(w http.ResponseWriter, r *http.Request) {
    // 路由分發到 screenshots 列表或 serve 圖片
})
```

或更精確地用 path 解析：
- `/api/features/{id}/screenshots` → 列表
- `/api/features/{id}/screenshots/{filename}` → serve 圖片

### 3.2 列表 handler

```go
func handleScreenshots(ws *protocol.Workspace, featureID string, w http.ResponseWriter)
```

1. 從 settings 讀 `screenshot_dir`，若空用預設值
2. 呼叫 `ws.DiscoverScreenshots(featureID, dir)`
3. 為每張截圖算出 `url` 和 `filename`
4. JSON 回傳 `{groups: [...], total: N}`

### 3.3 Serve 圖片 handler

```go
func handleServeScreenshot(ws *protocol.Workspace, featureID, filename string, w http.ResponseWriter)
```

1. `filepath.Base(filename)` 防 path traversal
2. 在 `screenshot_dir`（替換變數後）找檔案
3. 設定 Content-Type：`.png` → `image/png`，`.jpg/.jpeg` → `image/jpeg`，`.webp` → `image/webp`
4. `http.ServeFile` 或直接 `io.Copy`

### 3.4 Multi-project 支援

檢查 `multi.go` 是否需要對應的路由代理。若 multi mux 用 prefix strip 後轉發到 single mux，則不需要額外處理。

### Verify

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

手動驗證：啟動 `4x live`，curl `/api/features/F021-dashboard-control-panel/screenshots` 確認回傳正確 JSON。

---

## Task 4: Dashboard Screenshots Tab

**Subtask**: `dashboard-screenshot-tab`
**Files**: `internal/server/static/index.html`, `internal/server/static/ui.js`, `internal/server/static/style.css`

### 4.1 HTML

在 `index.html` 的 `detail-tabs` div 加 Screenshots button：

```html
<button class="detail-tab ..." data-tab="screenshots"
  onclick="switchDetailTab('screenshots')" style="display:none"
  id="screenshots-tab-btn">Screenshots</button>
```

在 `#main` 加 panel：

```html
<div id="screenshots-panel" class="hidden"></div>
```

### 4.2 CSS（style.css）

```css
.screenshot-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px;
}
.screenshot-thumb {
  width: 100%;
  aspect-ratio: 4/3;
  object-fit: cover;
  border-radius: 8px;
  cursor: pointer;
  transition: transform 0.15s;
}
.screenshot-thumb:hover {
  transform: scale(1.03);
}
```

### 4.3 JS（ui.js）

#### setDetailTabUI 更新

加上 `screenshots-panel` 的 hidden toggle。

#### switchDetailTab 更新

加上 `if (tab === 'screenshots' && current) loadScreenshots(current);`

#### loadScreenshots(fid)

```javascript
async function loadScreenshots(fid) {
  const panel = document.getElementById('screenshots-panel');
  const resp = await fetch(apiBase()+'/api/features/'+fid+'/screenshots');
  const data = await resp.json();
  if (!data.groups || data.total === 0) {
    panel.innerHTML = '<div class="text-zinc-600 text-sm text-center mt-8">No screenshots</div>';
    return;
  }
  panel.innerHTML = data.groups.map(g => `
    <div class="mb-6">
      <h3 class="text-sm font-semibold mb-3" style="color:var(--text-2)">Round ${g.round}</h3>
      <div class="screenshot-grid">
        ${g.screenshots.map((s, i) => `
          <div class="text-center">
            <img src="${s.url}" class="screenshot-thumb"
              onclick="openLightbox('${escAttr(fid)}', ${g.round}, ${i})"
              loading="lazy" alt="${esc(s.description)}">
            <div class="text-xs mt-1" style="color:var(--text-4)">${esc(s.description)}</div>
          </div>
        `).join('')}
      </div>
    </div>
  `).join('');
}
```

#### Tab 可見性

在 `loadScreenshots` 或 feature 載入時檢查 total > 0，控制 `#screenshots-tab-btn` 的 `display`。
在 feature 切換時（`showDetail`）先 fetch screenshots count，決定 tab 是否顯示。

### Verify

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

手動驗證：`4x live`，開 F021，確認 Screenshots tab 出現且顯示截圖。

---

## Task 5: Dashboard Lightbox

**Subtask**: `dashboard-lightbox`
**Files**: `internal/server/static/index.html`, `internal/server/static/ui.js`, `internal/server/static/style.css`

### 5.1 HTML

在 `<body>` 末尾加 lightbox overlay：

```html
<div id="lightbox" class="hidden" onclick="closeLightbox(event)">
  <button id="lb-prev" onclick="event.stopPropagation();lbNav(-1)">‹</button>
  <img id="lb-img" src="">
  <button id="lb-next" onclick="event.stopPropagation();lbNav(1)">›</button>
  <div id="lb-caption"></div>
  <button id="lb-close" onclick="closeLightbox()">&times;</button>
</div>
```

### 5.2 CSS

```css
#lightbox {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.85);
  display: flex; align-items: center; justify-content: center;
  z-index: 9999;
}
#lightbox.hidden { display: none; }
#lb-img {
  max-width: 90vw; max-height: 85vh;
  object-fit: contain; border-radius: 8px;
}
#lb-prev, #lb-next {
  position: absolute; top: 50%; transform: translateY(-50%);
  background: rgba(255,255,255,0.1); border: none; color: white;
  font-size: 32px; padding: 8px 16px; cursor: pointer; border-radius: 8px;
}
#lb-prev { left: 16px; }
#lb-next { right: 16px; }
#lb-caption {
  position: absolute; bottom: 24px; left: 50%; transform: translateX(-50%);
  color: rgba(255,255,255,0.8); font-size: 14px;
}
#lb-close {
  position: absolute; top: 16px; right: 16px;
  background: none; border: none; color: white; font-size: 28px; cursor: pointer;
}
```

### 5.3 JS

```javascript
let lbData = { fid: null, round: 0, index: 0, screenshots: [] };

function openLightbox(fid, round, index) {
  // 從已載入的截圖資料找到該 round 的 screenshots
  // 設定 lbData，顯示圖片
}

function closeLightbox(e) {
  if (e && e.target !== document.getElementById('lightbox')) return;
  document.getElementById('lightbox').classList.add('hidden');
}

function lbNav(dir) {
  // index += dir，循環，更新圖片和 caption
}

// ESC 鍵關閉
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeLightbox();
  if (e.key === 'ArrowLeft') lbNav(-1);
  if (e.key === 'ArrowRight') lbNav(1);
});
```

### Verify

手動驗證：點擊縮圖開啟 lightbox，左右鍵切換，ESC 關閉。

---

## Task 6: CLI Screenshot Count

**Subtask**: `cli-screenshot-count`
**Files**: `cmd/4x/status.go`, `docs/guide/cli.md`

### 6.1 status.go

在 subtask 列表之後，用 `ws.DiscoverScreenshots` 取得截圖資訊，輸出統計：

```go
groups, _ := ws.DiscoverScreenshots(f.ID, screenshotDir)
total := 0
var parts []string
for _, g := range groups {
    n := len(g.Screenshots)
    total += n
    parts = append(parts, fmt.Sprintf("round %d: %d", g.Round, n))
}
if total > 0 {
    fmt.Printf("\nScreenshots: %d (%s)\n", total, strings.Join(parts, ", "))
}
```

### 6.2 取得 screenshotDir

從 settings 讀 `tester.screenshot_dir`，空的用預設值。status.go 已經有讀 workspace 的邏輯，跟著現有 pattern 加。

### 6.3 docs 更新

`docs/guide/cli.md` status 段落加上 Screenshots 輸出說明。

### Verify

```bash
go build ./cmd/4x && go vet ./... && go test ./...
bin/4x status F021-dashboard-control-panel  # 應顯示 Screenshots: N
```

---

## Task 7: Tester Prompt Template

**Files**: `templates/tester.md.tmpl`

在 Output 段落加上 verify.json screenshots 欄位的說明：

```
## Screenshots in verify.json

If you take screenshots during testing, record them in verify.json:
```json
{
  "screenshots": [
    {"path": "e2e/{feature-id}/screenshot/01-description.png", "step": "01", "description": "what this shows"}
  ]
}
```

Path is relative to `.4x/`. Step is the numeric prefix from the filename.
```

注意：這個改動不對應獨立 subtask，跟 Task 1 一起做。

---

## Execution Order

```
Task 1 (protocol) → Task 2 (settings) → Task 3 (server API)
                                              ↓
                                    Task 4 (dashboard tab) → Task 5 (lightbox)
                                              ↓
                                    Task 6 (CLI status)
```

Task 1-3 是後端，可以在沒有前端的情況下用 curl 驗證。
Task 4-5 是前端，需要 `4x live` 手動測試。
Task 6 獨立，只依賴 Task 1 的 `DiscoverScreenshots`。
Task 7 跟 Task 1 一起做。
