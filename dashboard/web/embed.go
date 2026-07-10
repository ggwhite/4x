// Package web 是 4x dashboard 三平台共用的前端來源。
//
// 同一份前端資產（HTML/JS/CSS/locales）同時被三種殼載入：
//   - Go server（internal/server）透過本 package 的 Assets 直接 embed 並提供靜態檔；
//   - macOS Swift WKWebView 與 Windows/Linux 的 Tauri webview 則經由內嵌的 4x server
//     以 HTTP 載入同一份資產。
//
// 維護前端時只需改動本目錄，三平台即同步生效。
package web

import "embed"

// Assets 是 dashboard 前端的唯一資產來源，root 即前端根目錄
// （index.html 等檔位於 FS 根層，locale JSON 位於 locales/ 之下）。
//
//go:embed index.html core.js init.js ui.js search.js settings.js style.css locales favicon.ico apple-touch-icon.png icon-192.png icon-512.png
var Assets embed.FS
