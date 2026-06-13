# F043: Dashboard Screenshot Gallery — Spec

## Problem

Tester role 產出的 e2e 截圖散落在 `.4x/e2e/{feature-id}/screenshot/`，但 4x 本身（CLI + Dashboard + Protocol）完全不認得這些檔案。Review 時必須手動到檔案系統找截圖，無法在 dashboard 直接看到測試 evidence。

## Goal

讓 dashboard 能按 round 分組顯示 e2e 測試截圖，點擊可放大檢視。支援自訂截圖路徑，CLI status 顯示截圖統計。

## Design Decisions

| 決策 | 選擇 | 原因 |
|---|---|---|
| 截圖發現方式 | verify.json `screenshots` 欄位 + 目錄掃描 fallback | 新截圖走 protocol，舊截圖也能顯示 |
| 沒有 round 資訊的舊截圖 | 歸入 Round 1 | 向下相容，所有截圖都能顯示 |
| 縮圖產生 | CSS 原生縮放 | 不引入圖片處理 library |
| Lightbox | vanilla JS | 不加新依賴 |
| 截圖檔嵌入方式 | runtime 讀取 `.4x/` 目錄 | 不用 go:embed，截圖是動態產生的 |

## Protocol Changes

### verify.json 新增 `screenshots` 欄位

```json
{
  "passed": true,
  "round": 1,
  "role": "tester",
  "commands": [...],
  "screenshots": [
    {
      "path": "e2e/F021-dashboard-control-panel/screenshot/01-sidebar-groups.png",
      "step": "01",
      "description": "sidebar groups"
    }
  ]
}
```

- `path`：相對於 `.4x/` 的路徑
- `step`：序號字串（"01", "02"...），用於排序
- `description`：截圖描述，由 tester 填寫

### Screenshot type（`internal/protocol/types.go`）

```go
type Screenshot struct {
    Path        string `json:"path"`
    Step        string `json:"step"`
    Description string `json:"description"`
}
```

`VerifyEvidence` 新增欄位：

```go
type VerifyEvidence struct {
    Passed      bool            `json:"passed"`
    Round       int             `json:"round"`
    Role        Role            `json:"role"`
    Commands    []VerifyCommand `json:"commands"`
    Screenshots []Screenshot    `json:"screenshots,omitempty"`
}
```

## Settings

### `tester.screenshot_dir`

在 `.4x/settings.json` 的 `roles.tester` 區塊新增：

```json
{
  "roles": {
    "tester": {
      "model": "sonnet",
      "screenshot_dir": ".4x/e2e/{feature-id}/screenshot/",
      "instructions": [...]
    }
  }
}
```

- 預設值：`.4x/e2e/{feature-id}/screenshot/`（現行慣例）
- 支援 `{feature-id}` 變數替換
- settings editor UI 可編輯此欄位

### 截圖發現邏輯（優先序）

1. 讀各 round 的 `verify.json`，取 `screenshots` 欄位 → 有 round 資訊
2. 掃描 `screenshot_dir`（替換變數後）找 `.png/.jpg/.webp` → 歸入 Round 1
3. 兩者合併去重（以 path 為 key）

## Server API

### `GET /api/features/{id}/screenshots`

回傳該 feature 所有截圖，按 round 分組：

```json
{
  "groups": [
    {
      "round": 1,
      "screenshots": [
        {
          "path": "e2e/F021/screenshot/01-sidebar-groups.png",
          "step": "01",
          "description": "sidebar groups",
          "filename": "01-sidebar-groups.png",
          "url": "/api/features/F021/screenshots/01-sidebar-groups.png"
        }
      ]
    }
  ],
  "total": 4
}
```

Server 邏輯：
1. 遍歷 `.4x/{feature-id}/rounds/round-*/verify.json`，收集 `screenshots` 欄位
2. 讀 settings 取得 `screenshot_dir`，替換 `{feature-id}`，掃描目錄
3. 目錄掃描到的截圖若不在 verify.json 裡，歸入 Round 1
4. 按 round 分組，每組內按 step 排序

### `GET /api/features/{id}/screenshots/{filename}`

Serve 單張圖片檔。

- 從 `screenshot_dir` 或 verify.json 的 `path` 解析實際檔案位置
- Content-Type 根據副檔名設定（`image/png`、`image/jpeg`、`image/webp`）
- 用 `filepath.Base` 防止 path traversal

## Dashboard UI

### Screenshots Tab

在 feature detail 的 tab bar 新增 Screenshots tab（與 Overview / Messages / Logs 同層）。

**顯示條件**：API 回傳 `total > 0` 時才顯示 tab；沒有截圖時 tab 隱藏。

**佈局**：

```
┌─ Round 1 ──────────────────────────────────────┐
│ ┌──────────┐ ┌──────────┐ ┌──────────┐         │
│ │          │ │          │ │          │         │
│ │  thumb   │ │  thumb   │ │  thumb   │         │
│ │          │ │          │ │          │         │
│ └──────────┘ └──────────┘ └──────────┘         │
│ 01-sidebar   02-play-btn   03-run-modal        │
└────────────────────────────────────────────────┘

┌─ Round 2 ──────────────────────────────────────┐
│ ┌──────────┐                                    │
│ │          │                                    │
│ │  thumb   │                                    │
│ │          │                                    │
│ └──────────┘                                    │
│ 01-fixed-bug                                    │
└────────────────────────────────────────────────┘
```

- 每組標題顯示 "Round N"
- 縮圖用 CSS `object-fit: cover`，固定尺寸（如 200×150px）
- 縮圖下方顯示 description（從檔名解析或 verify.json 取得）
- 縮圖排列用 CSS grid，自動換行

### Lightbox

點擊縮圖開啟全螢幕 lightbox overlay：

- 半透明黑色背景（`rgba(0,0,0,0.85)`）
- 圖片置中，`max-width: 90vw; max-height: 90vh; object-fit: contain`
- 左右箭頭切換同 round 內的截圖
- ESC 或點擊背景關閉
- 底部顯示描述和 step 編號

### 檔名解析規則

`{step}-{description}.png` → step = "01", description = "sidebar groups"

- 用第一個 `-` 分割
- step 保留原始字串
- description 把 `-` 替換為空格

## CLI

### `4x status` 截圖統計

在 subtask 列表之後顯示：

```
Screenshots: 4 (round 1: 3, round 2: 1)
```

- 用同樣的截圖發現邏輯（verify.json + 目錄掃描）
- 沒有截圖時不顯示此行

## Constraints

- 截圖檔案不用 `go:embed` — runtime 讀取 `.4x/` 目錄
- 不引入圖片處理 library — 縮圖用 CSS 原生縮放
- Lightbox 用 vanilla JS，不加新依賴
- 向下相容：沒有 `screenshots` 欄位或沒有截圖目錄時不報錯，只是不顯示
- 截圖 API 的 path traversal 防護：只允許讀取 `.4x/` 目錄下的圖片檔

## File Changes

| 檔案 | 改動 |
|---|---|
| `internal/protocol/types.go` | 新增 `Screenshot` struct，`VerifyEvidence` 加 `Screenshots` 欄位 |
| `internal/protocol/workspace.go` | 新增截圖發現函式 |
| `internal/server/server.go` | 新增 `/api/features/{id}/screenshots` 和 serve 圖片 endpoint |
| `internal/server/static/index.html` | 新增 Screenshots tab button 和 panel |
| `internal/server/static/ui.js` | 新增 `loadScreenshots()`、`renderScreenshots()`、lightbox 邏輯 |
| `internal/server/static/style.css` | 新增 screenshot grid、lightbox 樣式 |
| `cmd/4x/status.go` | 截圖統計輸出 |
| `templates/tester.md.tmpl` | 提示 tester 在 verify.json 寫入 screenshots 欄位 |
| `docs/guide/cli.md` | 更新 status 輸出說明 |
| `docs/guide/dashboard.md` | 新增 Screenshots tab 說明 |
