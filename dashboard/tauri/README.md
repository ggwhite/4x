# 4x Live — Tauri 殼（Windows / Linux）

本目錄是 4x dashboard 的 **Windows / Linux** 桌面殼，使用 [Tauri v2](https://tauri.app/)。

> **macOS 不使用本殼**：macOS 維持 `dashboard/macos/` 的 Swift 原生 WKWebView 殼。
> 三平台共用同一份前端 `dashboard/web/`，由內嵌的 `4x` server 提供 API。

## 架構

- 前端：`tauri.conf.json` 的 `build.frontendDist` 指向 `../../web`（即 `dashboard/web/`），
  與 macOS / Go server 共用同一份資產。
- 後端：`4x` binary 以 **sidecar**（`bundle.externalBin`）內嵌，
  app 啟動時 spawn `4x live --port=4567`，視窗再導向 `http://localhost:4567`。
- Rust 程式（`src-tauri/src/main.rs`）僅負責 spawn sidecar + 載入 URL，**不含任何業務邏輯**。

## 準備 sidecar binary

Tauri sidecar 需要帶 target triple 後綴的 binary，放在 `src-tauri/binaries/`：

```
src-tauri/binaries/4x-x86_64-pc-windows-msvc.exe   # Windows
src-tauri/binaries/4x-x86_64-unknown-linux-gnu     # Linux
```

CI（`.github/workflows/desktop.yml`）會交叉編譯對應平台的 `4x` 並放到此處。

## Build

```bash
# 需先安裝 Rust + Tauri CLI（cargo install tauri-cli --version '^2')
cd dashboard/tauri
cargo tauri build            # 依當前平台產出 .msi（Windows）或 .AppImage（Linux）
```

產物 target 由 `tauri.conf.json` 的 `bundle.targets`（`msi`、`appimage`）決定。

## Icons

`src-tauri/icons/icon.png` 目前為 placeholder。正式發佈前請用
`cargo tauri icon path/to/logo.png` 產生各平台所需的完整 icon 集。
