# F036: Electron Cross-Platform Dashboard

> **Supersedes F035.** 原 F035 規劃以 Swift 做 macOS-only native app，但因 UI 本身就是 web（Go server 提供），維護兩套 native shell 的成本不值得。本 feature 統一用 Electron 涵蓋 macOS + Windows + Linux 三平台。

## Summary

以 Electron 為三平台統一桌面 dashboard app，取代 F035 的 Swift macOS-only 方案。提供 system tray、原生通知、badge、自動管理 `4x live` server、選單快捷鍵、auto-update，所有平台行為一致。

## Context

- 現有 macOS app（`dashboard/macos/Sources/main.swift`）是陽春 WKWebView wrapper（~100 行），F035 增強功能尚未實作
- `4x live` Go server 已提供完整的 REST API + SSE endpoints
- Web UI 由 Go server 的 `internal/server/static/` 提供
- `dashboard/electron/` 目錄已預留
- 統一 Electron 方案消除了 Swift/Electron 雙軌維護的成本

## Architecture

```
┌──────────────────────────────────────┐
│     4x Live (Electron)               │
│  ┌──────────────┐  ┌──────────────┐  │
│  │ Main Window  │  │  Tray Quick  │  │
│  │(BrowserWindow)│  │    View      │  │
│  └──────┬───────┘  └──────┬───────┘  │
│         │                 │          │
│  ┌──────┴─────────────────┴───────┐  │
│  │       Main Process (Node)      │  │
│  │  - ServerManager (spawn/kill)  │  │
│  │  - TrayManager (icon+menu)     │  │
│  │  - NotificationManager (SSE)   │  │
│  │  - AutoUpdater                 │  │
│  └──────────────┬─────────────────┘  │
└─────────────────┼────────────────────┘
                  │ HTTP / SSE
       ┌──────────┴──────────┐
       │  4x live server     │
       │  (Go subprocess)    │
       └─────────────────────┘
```

核心原則：
- Electron 不重寫 UI，web UI 由 `4x live` server 提供
- 不直接讀 `.4x/` 目錄，所有資料透過 Go server API
- Main process 負責系統整合（tray、通知、server 生命週期），renderer 只負責顯示

## Features

### 1. App 生命週期與 Server 管理

- App 啟動時自動 spawn `4x live -p <port>` 作為 child process（`child_process.spawn`）
- Binary 路徑解析順序：
  1. 環境變數 `FOURX_BIN` 若有設定，優先使用
  2. 查 `PATH`（`which` / Windows 上 `where`）
  3. Fallback `$(go env GOPATH)/bin/4x`
  4. 都找不到 → 顯示錯誤對話框引導使用者安裝
- 預設 port 4567，可透過 CLI arg `--port` 或 settings 檔覆蓋
- Server 就緒偵測：poll `GET /api/projects` 直到 HTTP 200，每 500ms 重試，最多 30 秒後 timeout 報錯
- App 退出：送 SIGTERM（Windows 用 `taskkill`），等 3 秒後 SIGKILL
- 自動重啟：server process 意外死亡時重啟，最多 3 次，間隔 1s / 2s / 4s（exponential backoff）
- Windows 跨平台差異：用 `process.kill()` + `taskkill /F /PID` 作為 fallback

### 2. System Tray

- 常駐系統匣，顯示 4x icon + tooltip 帶 active run 數量
- Icon 狀態：
  - idle：灰色靜態 icon
  - running：綠色 icon，用 icon 交替切換模擬動畫（每 800ms）
- 右鍵選單（context menu）：
  - 摘要行：「Running: 2 / Pending: 1 / Done: 5」（動態更新）
  - `Open Dashboard` → 打開或聚焦主視窗
  - `Separator`
  - `Quit 4x Live`
- 左鍵行為：toggle 主視窗顯示/隱藏
- 資料更新：每 5 秒 poll `/api/runs` 取得數量

> 與 F035 差異：F035 macOS 版使用 NSPopover 顯示獨立 mini UI。Electron 的 Tray 在 Linux 上對 popover/BrowserWindow 定位支援不一致，改用 context menu 摘要 + toggle 主視窗的方式更穩定跨平台。

### 3. 通知系統

- 使用 Electron 內建 `Notification` API（底層走 Windows Toast / libnotify）
- Main process 用 `eventsource` npm 套件訂閱 `/sse/events/{featureId}`
- 訂閱管理：
  - 啟動時 poll `/api/tasks` 取得 active feature 清單
  - 每個 active feature 建立一條 SSE connection
  - Feature 結束時關閉對應 connection
  - 每 30 秒重新檢查 active feature 清單，處理新增/移除
- 觸發通知的事件：
  - `transition` phase → `pending-review`：「{featureId} {name} 等待你的確認」
  - `run-error`：「{featureId} {name} 執行失敗」
  - `run-complete`：「{featureId} {name} 完成」
- 點擊通知 → 打開主視窗並導向該 feature（透過 `webContents.executeJavaScript` 切換 tab）
- Windows 跨平台差異：Toast 通知需要 Application User Model ID（electron-builder 自動設定）

### 4. Taskbar Badge

- macOS：使用 `app.dock.setBadge(String(count))`，0 時清空
- Windows：使用 `BrowserWindow.setOverlayIcon()` 動態產生帶數字的 overlay icon（用 `nativeImage` 繪製）
- Linux：使用 `app.setBadgeCount()`（支援 Unity/KDE，其他 DE 降級為 tray tooltip 顯示）
- 顯示 active run 數量，0 時清空
- 資料來源：與 tray 共用同一個 polling 計時器（每 5 秒）

### 5. 選單列與鍵盤快捷鍵

使用 `Menu.buildFromTemplate()`：

| 選單 | 項目 | 快捷鍵 |
|---|---|---|
| **4x Live**（macOS only） | About 4x Live | — |
| | Quit 4x Live | `⌘Q` |
| **File** | New Feature… | `CmdOrCtrl+N` |
| | Quit 4x Live（Win/Linux） | `CmdOrCtrl+Q` |
| **View** | Reload | `CmdOrCtrl+R` |
| | Toggle Sidebar | `CmdOrCtrl+Shift+S` |
| | Toggle DevTools | `F12` |
| **Window** | Minimize | `CmdOrCtrl+M` |
| | Close | `CmdOrCtrl+W` |

- macOS 的 app name menu（About / Quit）由 Electron 自動處理，只需設定 `app.setAboutPanelOptions()`
- 選單動作透過 `webContents.executeJavaScript()` 橋接到 web UI
- 「New Feature…」觸發 web UI 的 new feature modal
- `Close` 行為：隱藏視窗而非退出 app（tray 仍在），真正退出走 Quit 或 tray 的 Quit

### 6. Auto-Update

- 使用 `electron-updater` 的 GitHub provider
- 啟動時檢查更新，每 6 小時再檢查一次
- 發現新版 → 背景下載 → 通知使用者「新版本已就緒，重啟後生效」
- 使用者可選擇立即重啟或下次自動套用

## Build System

### 工具鏈

- `electron-builder` 負責打包，`electron-updater` 負責 auto-update
- 開發：`npm run dev`（直接 `electron .`）
- 打包：`npm run dist`

### 打包產出

| 平台 | 格式 | 備註 |
|---|---|---|
| macOS | `.dmg` | 拖曳安裝，不做 code signing（開發者工具用途） |
| Windows | `.exe`（NSIS installer） | 含 uninstaller、可選安裝路徑 |
| Linux | `.AppImage` + `.deb` | AppImage 免安裝，deb 給 Debian/Ubuntu |

### CI/CD（GitHub Actions）

- 觸發條件：push tag `v*`
- Job matrix：`[macos-latest, windows-latest, ubuntu-latest]`
- Steps：checkout → setup node → npm ci → electron-builder --publish always
- 產出自動上傳到 GitHub Release
- 不做 code signing（目前規模不需要，macOS 需 `xattr -cr` 解除 Gatekeeper，Windows 會看到 SmartScreen 警告）

### 檔案結構

```
dashboard/electron/
├── package.json
├── electron-builder.yml        # 打包設定
├── src/
│   ├── main.js                 # Entry point、視窗建立、選單
│   ├── server-manager.js       # spawn/kill 4x live server
│   ├── tray-manager.js         # System tray icon + context menu
│   ├── notification-manager.js # SSE 訂閱 + 通知派發
│   ├── updater.js              # Auto-update 邏輯
│   └── preload.js              # Renderer preload（native bridge）
├── assets/
│   ├── icon.png                # App icon（256x256）
│   ├── icon.ico                # Windows icon
│   ├── icon.icns               # macOS icon
│   ├── tray-idle.png           # Tray 靜態 icon（16x16）
│   ├── tray-running-1.png      # Tray 動畫 frame 1
│   └── tray-running-2.png      # Tray 動畫 frame 2
├── .github/
│   └── workflows/
│       └── release.yml         # CI: tag push → build → publish
└── README.md
```

## Non-Goals

- 不做 code signing — 目前規模不需要
- 不重寫 web UI — 由 `4x live` server 提供
- 不 bundle 4x binary — 使用者自裝
- 不做 portable 版（.zip 免安裝）— 初版只做 installer

## Migration from F035

- F035 spec 標記為 superseded by F036
- `dashboard/macos/` 目錄在 F036 完成後移除
- 現有 `main.swift` 的功能（WKWebView wrapper、folder picker、title sync）全部由 Electron 版覆蓋

## Dependencies

- Node.js 18+
- Electron 35+（當前 stable）
- `electron-builder` + `electron-updater`
- `eventsource`（SSE client for Node.js）
- 使用者機器需有 `4x` CLI binary 在 PATH

## Testing

- 手動驗證：啟動 app → server 自動起來 → web UI 載入 → 執行 feature → 通知出現 → taskbar badge 更新
- Server 異常測試：手動 kill server process → 觀察自動重啟
- 無 4x binary 測試：移除 PATH → 觀察 app 顯示錯誤對話框
- Auto-update 測試：建立 draft release → 驗證 app 偵測到新版
- 跨平台 CI：GitHub Actions matrix 在 macOS + Windows + Ubuntu 上跑 `npm test`（基本啟動+退出測試）
