# F027 — Live Settings Editor

## 問題

修改 `.4x/settings.json` 需要手動編輯 JSON 檔案，容易打錯格式、漏欄位。Dashboard 應提供視覺化編輯介面。

## 目標

1. Dashboard 提供 VSCode 風格的全頁設定頁，分區表單編輯所有 settings 欄位
2. Auto-save：欄位變更後立即寫入，無需手動按 Save
3. 提供 JSON 原始碼模式供進階使用者直接編輯
4. 儲存前備份原檔為 `settings.json.bak`
5. 未知欄位保留不刪（完整 JSON roundtrip）

## 設計

### 入口

- Header 右側加齒輪 icon（⚙），點擊開啟設定頁
- `Cmd+,`（macOS）/ `Ctrl+,`（其他）快捷鍵開啟
- 設定頁覆蓋整個右側內容區，關閉後回到之前的 feature 詳情

### 頁面佈局

```
┌─────────────────────────────────────────────────┐
│ [🔍 搜尋設定...]                    [JSON 模式] │
├──────────┬──────────────────────────────────────┤
│ Project  │  Project Name                        │
│ Runners  │  專案名稱                             │
│ Roles    │  ┌──────────────────────────┐         │
│ General  │  │ 4x                      │         │
│          │  └──────────────────────────┘         │
│          │                                      │
│          │  Description                         │
│          │  專案描述                              │
│          │  ┌──────────────────────────┐         │
│          │  │ Multi-role AI dev loop   │         │
│          │  └──────────────────────────┘         │
│          │                                      │
│          │  Language                             │
│          │  ┌──────────────────────────┐         │
│          │  │ go                       │         │
│          │  └──────────────────────────┘         │
│          │  ...                                  │
└──────────┴──────────────────────────────────────┘
```

### 分區欄位

#### Project

| 欄位 | 型別 | 控件 |
|---|---|---|
| name | string | text input（必填） |
| description | string | textarea |
| language | string | text input |
| setup | string[] | tag list（可新增/刪除） |
| build | string[] | tag list |
| test | string[] | tag list |
| lint | string[] | tag list |
| docs | string[] | tag list |
| rules | string[] | tag list（多行，每項一行） |
| includes | string[] | tag list |

#### Runners

動態 map，每個 runner 是一個可折疊的區塊：

```
▼ claude                                    [🗑]
  Command    ┌─────────────────┐
             │ claude          │
             └─────────────────┘
  Args       [--dangerously-skip-permissions] [-p] [{prompt}]  [+]
  Model      ┌─────────────────┐
             │ opus            │
             └─────────────────┘
  □ stdin    ☑ tty

▼ codex                                     [🗑]
  ...

[+ Add Runner]
```

- 每個 runner 顯示 key 名稱作為標題
- command、model 是 text input
- args 是 tag list
- stdin、tty 是 checkbox
- 可刪除整個 runner（確認對話框）
- 底部「Add Runner」按鈕，輸入 key 名稱後展開空白表單

#### Roles

動態 map，結構同 Runners：

```
▼ designer                                  [🗑]
  Model       ┌──────────┐
              │ opus     │
              └──────────┘
  Deep Model  ┌──────────┐
              │          │
              └──────────┘
  Instructions
    ┌────────────────────────────────────────┐
    │ 開始前先檢查 docs/design/...           │  [🗑]
    │ spec 是設計規格...                      │  [🗑]
    │                                        │  [+]
    └────────────────────────────────────────┘
  Includes
    [CLAUDE.md]  [+]

[+ Add Role]
```

- model、deep_model 是 text input
- instructions 是有序列表，每項一行 textarea，可新增/刪除
- includes 是 tag list

#### General

| 欄位 | 型別 | 控件 |
|---|---|---|
| default_runner | string | select（從 runners 的 key 列表動態生成） |
| isolation | string | select：`worktree` / `branch` / 空值 |
| max_concurrent_runs | number | number input |
| commit | string | text input |
| rules | string[] | tag list |
| hub_repos | string[] | tag list |

### JSON 模式

頂部右側「JSON 模式」toggle 開關：

- **關（預設）**：分區表單模式
- **開**：整頁替換為單一 monospace textarea，顯示完整 settings.json（格式化 2-space indent）
- 切換到 JSON 模式時從 server 重新 GET 最新資料
- JSON 模式下修改後 blur textarea 觸發 auto-save
- JSON parse 失敗時：textarea 邊框變紅 + 顯示錯誤訊息，不送 PUT
- 從 JSON 模式切回表單模式時重新 GET 並渲染表單

### 搜尋

頂部搜尋列，輸入時即時過濾：

- 比對每個欄位的 label 和 JSON key（case-insensitive）
- 不匹配的欄位隱藏，匹配的欄位所在分類自動展開
- 清空搜尋恢復全部顯示
- 僅在表單模式下生效，JSON 模式用瀏覽器原生 Cmd+F

### Auto-save

- 欄位 blur（失焦）或 tag 新增/刪除時觸發
- debounce 300ms，避免連續操作多次寫入
- 流程：收集完整 JSON → `PUT /api/settings` → 成功時短暫顯示 "Saved" 提示（1.5s 淡出）
- 失敗時顯示紅色錯誤訊息，不清除使用者的修改

### API

#### `GET /api/settings`

回傳 settings.json 完整內容：

```json
200 {"project":{...},"runners":{...},"roles":{...},...}
```

#### `PUT /api/settings`

接收完整 settings.json，server 端處理：

1. 驗證 JSON 合法性
2. 驗證 `project.name` 非空
3. 備份現有 `settings.json` 為 `settings.json.bak`（覆寫之前的 bak）
4. 寫入新的 `settings.json`

```
200 {"ok":true}
400 {"error":"project.name is required"}
400 {"error":"invalid JSON: ..."}
```

### 未知欄位保留

前端永遠以 GET 拿到的完整 JSON 為底，只修改表單觸及的欄位，PUT 送回完整物件。任何不在表單中的欄位（例如未來新增的 config key）會原封不動地 roundtrip 回去。

實作方式：前端維護一個 `configData` 物件（GET 的原始結果），表單欄位變更時直接改這個物件對應的 key，PUT 時送整個 `configData`。

### 快捷鍵

```js
document.addEventListener('keydown', e => {
  if ((e.metaKey || e.ctrlKey) && e.key === ',') {
    e.preventDefault();
    openSettings();
  }
});
```

設定頁開啟時按 `Escape` 關閉回到之前畫面。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/server/server.go` | 新增 `GET /api/settings` 和 `PUT /api/settings` handler |
| `internal/server/server_test.go` | 新增 settings API 測試 |
| `internal/server/static/index.html` | 新增設定頁 UI（齒輪 icon、表單、JSON 模式、搜尋、auto-save） |

## 不做的事

- 不做 undo / redo history
- 不做 settings sync（多 dashboard 同步）
- 不做 schema-driven 動態表單生成（欄位在前端寫死，跟 Config struct 對應）
- 不做 settings diff / compare
- 不做欄位層級的 description tooltip（YAGNI，欄位名稱已經夠清楚）
