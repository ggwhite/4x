# 4x Design — Cross-platform Desktop Packaging

> F072：將 4x dashboard 打包成 macOS / Windows / Linux 三平台桌面 app。

---

## 1. 設計目標

讓 dashboard 以原生桌面 app 形式分發，使用者不必手動跑 `4x live`：app 啟動時自行
拉起內嵌的 `4x` server，並載入同一份前端。三平台共用單一 UI 維護點。

## 2. 共用前端層 `dashboard/web/`

前端資產（`index.html`、`core.js`、`init.js`、`ui.js`、`settings.js`、`style.css`、
`locales/*.json`）集中於 **`dashboard/web/`**，是三平台**唯一** UI 維護點。

- `dashboard/web/embed.go`（package `web`）以 `//go:embed` 匯出 `var Assets embed.FS`，
  FS root 即前端根目錄。
- Go server（`internal/server/server.go`、`multi.go`）直接以 `web.Assets` 提供靜態檔，
  locale 由 `web.Assets.ReadFile("locales/<lang>.json")` 讀取。
- `scripts/check-i18n.sh` 的 `LOCALES_DIR` 指向 `dashboard/web/locales`。

> 歷史：前端原本位於 `internal/server/static/`，F072 起搬移至 `dashboard/web/`，
> 讓 Swift 與 Tauri 殼也能共用同一來源。

## 3. 三平台殼

| 平台 | 殼 | 位置 | server 取得方式 |
|---|---|---|---|
| macOS | Swift 原生 WKWebView | `dashboard/macos/` | bundle 內 `Contents/MacOS/4x`，Swift 以 `Process` spawn `4x live` |
| Windows | Tauri v2 | `dashboard/tauri/` | sidecar（`bundle.externalBin`）spawn `4x live` |
| Linux | Tauri v2 | `dashboard/tauri/` | 同 Windows |

三殼啟動內嵌 server 後，皆載入 `http://localhost:<port>`（預設 4567），行為一致。
macOS 不使用 Tauri（保留 Swift 原生殼）；Tauri 的 Rust 僅負責 spawn sidecar + 載入 URL，
不含任何業務邏輯。

**預設 port 單一事實來源**：Go 端以 `internal/server.DefaultPort`（值 4567）為權威定義，
`cmd/4x/live.go` 的 `--port` flag 預設值即讀取此常量。Swift（`main.swift` 的
`serverPort`）與 Rust（`main.rs` 的 `DEFAULT_PORT`）在殼啟動、決定要 spawn 的 server
port 時必須各自持有一份等值的本地常量（跨語言編譯無法直接引用 Go 常量），
由 `internal/server/port_sync_test.go` 的測試在 `make test` 時自動比對三處字面值，
確保未來修改任一處都會被 CI 攔截，而非靜默漂移。前端（`dashboard/web/core.js`）
一律使用相對路徑呼叫 API/SSE，不需感知實際 port，因此不含 port 字面值，
同一測試檔內也對此做迴歸防護。

### macOS 細節

- `dashboard/macos/Sources/main.swift` 的 `launchEmbeddedServer()` 啟動與 Swift 執行檔
  同層的 `4x`；`applicationWillTerminate` 結束時 terminate 該子程序。
- 找不到 bundled `4x` 時 fallback 到 poll 既有外部 server（向後相容）。
- `scripts/package-macos.sh`：`lipo` 合併 arm64 + amd64 為 universal binary → `swift build`
  → 組 `4x Live.app` bundle（含 Info.plist）→ `hdiutil` 產 `dist/4x-Live.dmg`。
  Makefile target：`make package-macos`。

## 4. CI 打包

`.github/workflows/desktop.yml`（與 `.goreleaser.yml` 的純 CLI release 並存，
trigger 為 tag `v*` 或 `workflow_dispatch`）：

1. `build-binaries`（matrix：darwin/arm64、darwin/amd64、windows/amd64、linux/amd64）
   交叉編譯各平台 `4x` binary 並上傳 artifact。
2. `macos`：下載 darwin 兩 arch，跑 `make package-macos` 產 `.dmg`。
3. `windows`：取 windows binary 作 sidecar，`cargo tauri build` 產 `.msi`。
4. `linux`：取 linux binary 作 sidecar，`cargo tauri build` 產 `.AppImage`。
