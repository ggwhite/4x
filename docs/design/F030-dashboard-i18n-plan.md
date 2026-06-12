# F030: Dashboard i18n Implementation Plan

**Goal:** 為 Dashboard 加入 6 語言 i18n 支援（en, zh-TW, zh-CN, ja, ko, es），翻譯檔為 JSON + Go embed，前端用 `t(key)` 函式替換所有硬寫字串。

**Architecture:** 翻譯 JSON 放 `internal/server/static/locales/`，Go embed 嵌入 binary。Server 新增兩個 API（`/api/locales` 清單、`/api/locales/{lang}` 內容）。前端啟動時 fetch 對應 locale，用 `t(key)` + `data-i18n` attribute 替換靜態文字。Settings 面板加 Language 下拉。

**Tech Stack:** Go (embed, net/http), vanilla JS, JSON

**Spec:** `docs/design/F030-dashboard-i18n-spec.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/server/static/locales/en.json` | 英文翻譯基準檔 |
| Create | `internal/server/static/locales/zh-TW.json` | 繁體中文翻譯 |
| Create | `internal/server/static/locales/zh-CN.json` | 简体中文翻译 |
| Create | `internal/server/static/locales/ja.json` | 日本語翻訳 |
| Create | `internal/server/static/locales/ko.json` | 한국어 번역 |
| Create | `internal/server/static/locales/es.json` | Traducción al español |
| Modify | `internal/server/server.go` | 新增 locale embed + API handlers |
| Modify | `internal/server/multi.go` | 在 multi-project mux 註冊 locale routes |
| Modify | `internal/server/server_test.go` | 新增 locale API 測試 |
| Modify | `internal/server/multi_test.go` | 新增 multi mux locale 測試 |
| Modify | `internal/server/static/index.html` | i18n runtime + 字串抽取 + Language 下拉 |
| Create | `scripts/check-i18n.sh` | CI key 完整性驗證腳本 |
| Modify | `Makefile` | 加 `check-i18n` target |

---

## Task Summary

### Task 1 — en.json 基準翻譯檔
建立 `internal/server/static/locales/en.json`，盤點 index.html 所有 UI 文字，使用 dot notation key，含 `{placeholder}` 佔位符。

### Task 2 — 5 個語言翻譯檔
建立 zh-TW、zh-CN、ja、ko、es 的 JSON，key set 與 en.json 完全一致，翻譯品質使用 native UI 慣用語。

### Task 3 — Go server locale API
- `server.go`：新增 `//go:embed static/locales/*.json`、`localeFS`、`supportedLocales`、`handleGetLocales`、`handleGetLocale`
- `multi.go`：在 `NewMultiMux` 中同樣註冊 `/api/locales` 與 `/api/locales/` routes

### Task 4 — 前端 i18n runtime
在 index.html 加入 `t()`、`detectLocale()`、`loadLocale()`、`applyI18n()`、`switchLocale()` 函式。修改 `init()` 先載入 locale 再 loadProjects。

### Task 5 — HTML 靜態字串抽取
把 HTML 硬寫的使用者可見文字改成 `data-i18n` / `data-i18n-placeholder` / `data-i18n-title` attribute。

### Task 6 — JS 動態字串替換
把 JS 字面值用 `t('key')` 包裹，含參數者用 `.replace('{x}', value)`。

### Task 7 — Settings 面板 Language 下拉
新增 `<select id="locale-select">` 到 Settings modal，修改 `openSettings()` 動態產生選項，強化 `switchLocale()` 即時 re-render。

### Task 8 — CI key 完整性驗證
建立 `scripts/check-i18n.sh`，Makefile 加 `check-i18n` target。

### Task 9 — 全量驗證 + Feature YAML
全量 build/vet/test 通過，更新 feature YAML spec/plan 路徑為 F030。

### Task 10 — 文件同步
更新 `docs/guide/dashboard.md` 新增 `/api/locales` API 文件。
