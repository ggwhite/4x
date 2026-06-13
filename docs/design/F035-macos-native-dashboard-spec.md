# F035: macOS Native Dashboard

## Summary

將現有的陽春 WKWebView wrapper 增強為完整的 macOS native app shell，包含自動管理 `4x live` server、menu bar status item + popover、macOS 通知、Dock badge、選單列與鍵盤快捷鍵。Web UI 內容仍由 `4x live` server 提供，native 層只負責系統整合。

## Context

- 現有 macOS app（`dashboard/macos/Sources/main.swift`）只是 WKWebView 載入 localhost web UI，加了 native folder picker 和 title sync
- `4x live` Go server 已提供完整的 REST API + SSE endpoints，包含 process management
- 參考 `Tyche/Kairos/tools/dct-dashboard` 的 native shell 模式（menu bar、popover、通知、Makefile build）

## Architecture

```
┌──────────────────────────────────┐
│        4x Live.app (Swift)       │
│  ┌────────────┐  ┌────────────┐  │
│  │ Main Window │  │  Popover   │  │
│  │ (WKWebView) │  │ (WKWebView)│  │
│  └──────┬──────┘  └─────┬──────┘  │
│         │               │         │
│  ┌──────┴───────────────┴──────┐  │
│  │      Native Shell Layer     │  │
│  │  - Server subprocess mgmt  │  │
│  │  - SSE event listener      │  │
│  │  - Notification dispatch   │  │
│  │  - Dock badge update       │  │
│  │  - Menu bar + shortcuts    │  │
│  └──────────────┬──────────────┘  │
└─────────────────┼─────────────────┘
                  │ HTTP / SSE
       ┌──────────┴──────────┐
       │  4x live server     │
       │  (Go subprocess)    │
       └─────────────────────┘
```

Native app 不重寫 UI，不直接讀 `.4x/` 目錄，所有資料透過 Go server API 取得。

## Features

### 1. App 生命週期與 Server 管理

- App 啟動時自動 spawn `4x live -p <port>` 作為 child process
- `4x` binary 路徑解析：查 `PATH` → fallback `$(go env GOPATH)/bin/4x`
- 預設 port 4567（與 `4x live` 預設一致），可透過 app launch argument `--port` 覆蓋
- Server 就緒偵測：poll `GET /api/projects` 直到 HTTP 200，每 500ms 重試
- App 退出時送 SIGTERM 給 child process，等 3 秒後 SIGKILL
- Server process 意外死亡時自動重啟，最多 3 次，間隔 1s / 2s / 4s

### 2. Menu Bar Status Item + Popover

- 常駐 menu bar，顯示 4x icon + active run 數量
- Icon 狀態：idle 灰色靜態 / running 綠色 + pulse 動畫
- 點擊 status item 顯示 popover（NSPopover），再點或失焦收起
- Popover 用獨立 `popover.html`，透過 `4x live` API 拉資料，WKWebView 渲染
- Popover 內容：
  - 摘要卡片（running / pending / done 數量）
  - Active tasks 清單（feature name + role badge + 經過時間）
  - 「Open Dashboard」按鈕 → 打開或聚焦主視窗
  - 「Quit」按鈕
- 資料更新：popover 可見時每 3 秒 refresh，隱藏時不 poll

### 3. 通知系統

- App 首次啟動時請求 `UNUserNotificationCenter` 權限
- Swift 端建 `EventSource` 類別，定期 poll `/api/tasks` 取得 active feature 清單，為每個 active feature 訂閱 `/sse/events/{featureId}`，feature 結束時關閉對應 connection
- 觸發通知的事件：
  - `transition` phase → `pending-review`：「{featureId} {name} 等待你的確認」
  - `run-error`：「{featureId} {name} 執行失敗」
  - `run-complete`：「{featureId} {name} 完成」
- 點擊通知 → 打開主視窗並切換到該 feature tab（透過 `evaluateJavaScript`）

### 4. Dock Badge

- 顯示 active run 數量（`NSApp.dockTile.badgeLabel`）
- 0 個時清空 badge
- 資料來源：每 5 秒 poll `GET /api/runs`，與 popover refresh 共用計時器

### 5. 選單列與鍵盤快捷鍵

| 選單 | 項目 | 快捷鍵 |
|---|---|---|
| **4x Live** | About 4x Live | — |
| | Quit 4x Live | ⌘Q |
| **File** | New Feature… | ⌘N |
| **View** | Reload | ⌘R |
| | Toggle Sidebar | ⌘⇧S |
| **Window** | Minimize | ⌘M |
| | Zoom | — |

- 選單動作透過 `webView.evaluateJavaScript()` 橋接到 web UI
- 「New Feature…」觸發 web UI 的 new feature modal

### 6. 保留的現有功能

- Native folder picker（`NSOpenPanel`）→ 透過 `nativeOpenFolder` message handler
- Window frame autosave（`setFrameAutosaveName`）
- Title sync（從 web UI 讀取當前 project name 更新視窗標題）

## Build System

從 Package.swift 改為 Makefile + `swiftc`：

```makefile
build:
	swiftc -framework WebKit -framework AppKit -framework UserNotifications \
	  -O Sources/*.swift -o 4x-live

app: build
	# 建立 4x Live.app bundle
	# 包含 Info.plist, AppIcon.icns, popover.html, menu bar icons
```

### 檔案結構

```
dashboard/macos/
├── Makefile
├── Info.plist
├── Sources/
│   ├── AppDelegate.swift      # 生命週期、視窗、選單
│   ├── ServerManager.swift    # spawn/kill 4x live server
│   ├── StatusItem.swift       # Menu bar icon + popover
│   ├── EventListener.swift    # SSE 訂閱 + 通知派發
│   └── main.swift             # entry point
├── Resources/
│   ├── popover.html           # Popover web UI
│   ├── AppIcon.icns
│   ├── icon-idle.png          # Menu bar idle icon
│   └── icon-running.png       # Menu bar running icon
└── Package.swift              # 移除（改用 Makefile）
```

## Non-Goals

- 不用 SwiftUI 重寫 UI — web UI 由 `4x live` server 提供
- 不直接讀 `.4x/` 目錄 — 所有資料透過 Go server API
- 不做 Electron 版（那是另一個 feature）
- 不做 auto-update 機制

## Dependencies

- macOS 13+（Ventura）
- `4x` CLI binary 必須可在 PATH 中找到
- `4x live` server 的現有 REST + SSE API（不需新增 endpoint）

## Testing

- 手動驗證：啟動 app → server 自動起來 → web UI 載入 → 執行 feature → 通知出現 → Dock badge 更新
- Server 異常測試：手動 kill server process → 觀察自動重啟
- 無 `4x` binary 測試：移除 PATH → 觀察 app 顯示錯誤提示
