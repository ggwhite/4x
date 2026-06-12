# F030: Dashboard i18n Multi-Language Support — Spec

## Overview

為 Dashboard Web UI 加入 6 語言 i18n 支援。翻譯檔為獨立 JSON，透過 Go embed 嵌入 binary，前端透過 API 載入對應語言。macOS Swift wrapper 不改動。

## Supported Locales

`en`, `zh-TW`, `zh-CN`, `ja`, `ko`, `es`

## Architecture

### 翻譯檔結構

```
internal/server/static/locales/
  en.json
  zh-TW.json
  zh-CN.json
  ja.json
  ko.json
  es.json
```

JSON 格式為 flat key-value，用 dot notation 分 namespace：

```json
{
  "nav.features": "Features",
  "nav.settings": "Settings",
  "feature.status.coding": "Coding",
  "settings.language": "Language",
  "settings.theme": "Theme"
}
```

`en.json` 為基準檔，其他語言的 key set 必須與其一致。

### Go Server 變動

**新增 embed：**

```go
//go:embed static/locales/*.json
var localeFS embed.FS
```

**新增 API：**

| Endpoint | Method | 說明 |
|----------|--------|------|
| `/api/locales` | GET | 回傳支援的 locale 清單 `["en","zh-TW","zh-CN","ja","ko","es"]` |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON；`lang` 不存在則 fallback 回 `en.json` |

Response headers：
- `Content-Type: application/json`
- `Cache-Control: public, max-age=31536000, immutable`

### 前端 i18n 機制

**Locale 決定順序：**

1. `localStorage.getItem('4x-locale')`（使用者手動選過）
2. `navigator.language` 匹配支援的 locale
3. fallback `en`

**語言匹配邏輯：**

先精確比對，再比對語言前綴。例：`navigator.language = "ja-JP"` → 精確找 `ja-JP` 無結果 → 前綴找 `ja` 命中。

**翻譯函式 `t(key)`：**

- 啟動時 fetch `/api/locales/{lang}`，存入全域 dict
- `t(key)` 查 dict，miss 則 fallback 回 key 本身
- 靜態文字：HTML 元素加 `data-i18n` attribute，頁面載入時一次性替換
- 動態文字：JS 程式碼直接呼叫 `t(key)`

**切換語言：**

- 寫入 `localStorage('4x-locale', lang)`
- 重新 fetch 翻譯 JSON
- 刷新所有 `[data-i18n]` 元素
- 不需 reload 整頁

### 字串抽取範圍

**翻譯：**
- 導航列、tab 標題
- 狀態標籤（Coding, Reviewing, Done 等）
- Settings 面板所有 label
- 按鈕文字、空狀態提示、確認對話框

**不翻譯：**
- Feature name、description（專案資料）
- Log 內容、event 訊息（runner 產生的原始輸出）
- API 回傳的 data 欄位

### Settings 面板 Language 下拉

- 位置：Settings 面板，Theme 選擇器旁
- 啟動時 fetch `GET /api/locales` 動態產生選項
- 選項顯示語言原生名稱：`English`、`繁體中文`、`简体中文`、`日本語`、`한국어`、`Español`
- 當前值從 localStorage 讀取，無值則顯示偵測到的系統語言
- 切換後立即生效

### macOS Swift Wrapper

- 不改動任何 Swift code
- `window.title` 透過現有 `startTitleSync` 機制從 web 層取得翻譯後標題
- NSOpenPanel message 維持英文（影響極小，僅加專案時出現一次）

## 驗證

### CI key 完整性驗證

整合進 `make lint` 或獨立 `make check-i18n`：

- 讀 `en.json` 的 key set 為基準
- 比對其他 5 個 JSON 的 key set
- 缺少 key → error（CI fail）
- 多出 key → warning
- JSON 格式不合法 → error

### Go 測試

- `server_test.go` 新增測試：
  - `GET /api/locales` 回傳正確清單
  - `GET /api/locales/en` 回傳合法 JSON
  - `GET /api/locales/zh-TW` 回傳合法 JSON
  - `GET /api/locales/nonexistent` fallback 回 en
  - Response headers 正確

### 手動驗證

- 切換語言後所有可見文字正確翻譯
- 重新整理頁面後語言選擇保留
- 系統語言偵測 fallback 正確
- 不支援的語言 fallback 回 en

## Scope 外

- Per-project locale override
- URL query string 帶 locale
- Swift 層 NSLocalizedString
- RTL 語言支援
