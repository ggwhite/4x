# F034: Global Settings and Run

## Overview

將目前全塞在 project-level `.4x/settings.json` 的設定拆分為 user-level 和 project-level 兩層，各自只放屬於自己範圍的欄位。`4x run` 時做 deep merge（project 覆蓋 user），Dashboard 設定畫面只顯示對應層級的欄位。

## Settings 歸屬

### User-level (`~/.4x/settings.json`)

使用者偏好與環境相關設定，跨專案共用：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `locale` | string | 語言偏好（已存在） |
| `theme` | string | Dashboard 主題 |
| `default_runner` | string | 預設 runner 名稱 |
| `runners` | map[string]RunnerConfig | runner 定義（command、args、model_map 等） |
| `roles` | map[string]RoleConfig | 各角色的 model 預設值 |

### Project-level (`.4x/settings.json`)

專案特定設定：

| 欄位 | 範圍 | 說明 |
|---|---|---|
| `project` | project-only | 專案 metadata（name、build、test、lint 等） |
| `isolation` | project-only | worktree 隔離策略 |
| `commit` | project-only | commit 策略（per-round / on-done / never） |
| `max_concurrent_runs` | project-only | 最大並行 run 數 |
| `hub_repos` | project-only | 共用 repo 列表 |
| `rules` | project-only | 專案紅線 |
| `default_runner` | 可覆蓋 user | 專案預設 runner |
| `runners` | 可覆蓋 user | 專案專屬 runner 設定 |
| `roles` | 可覆蓋 user | 專案專屬 role model 設定 |

## UserConfig 擴展

現有 `UserConfig` struct 從只有 `Locale` 擴展：

```go
type UserConfig struct {
    Locale        string                   `json:"locale,omitempty"`
    Theme         string                   `json:"theme,omitempty"`
    DefaultRunner string                   `json:"default_runner,omitempty"`
    Runners       map[string]RunnerConfig  `json:"runners,omitempty"`
    Roles         map[string]RoleConfig    `json:"roles,omitempty"`
}
```

範例 `~/.4x/settings.json`：

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

## Deep Merge 邏輯

新增 `MergeConfig(user UserConfig, project Config) Config`。

### 合併優先序：project > user > 零值

| 欄位 | 邏輯 |
|---|---|
| `DefaultRunner` | project 有設 → 用 project；沒設 → 用 user |
| `Runners` | 以 runner name 為 key，逐個 runner 做欄位級 merge |
| `Roles` | 以 role name 為 key，逐個 role 做欄位級 merge |
| project-only 欄位 | 不參與 merge，直接用 project 的值 |

### Runner 欄位級 merge

user 為底，project 非零值欄位覆蓋：

```
user.claude  = {command: "/usr/local/bin/claude", args: [...], tty: true}
project.claude = {model: "opus"}
→ final.claude = {command: "/usr/local/bin/claude", args: [...], tty: true, model: "opus"}
```

各欄位規則：

| 欄位 | 規則 |
|---|---|
| `command` | project 非空 → 用 project |
| `args` | project 非 nil → 整組替換（不 append） |
| `model` | project 非空 → 用 project |
| `tiers` | key 級 merge：project 的 key 覆蓋 user 同名 key，user 獨有 key 保留 |
| `tty`, `stdin`, `quiet` | project 有設 → 用 project |

`args` 整組替換的理由：args 順序和組合是一個整體，混合兩層的 args 沒有意義。

### Role 欄位級 merge

同 runner 邏輯，以 role name 為 key：

```
user.designer  = {model: "opus"}
project.coder  = {model: "sonnet"}
→ final = {designer: {model: "opus"}, coder: {model: "sonnet"}}
```

同一個 role 在兩層都有時，project 非零值欄位覆蓋 user。

## 呼叫時機

merge 發生在讀完兩層設定之後、使用 config 之前：

```
ws.ReadConfig()              // project config
protocol.ReadUserConfig()    // user config
cfg = protocol.MergeConfig(userCfg, projectCfg)
```

### 受影響的進入點

| 進入點 | 行為 |
|---|---|
| `4x run` | merge 後用 final config 選 runner、resolve model |
| `4x prompt` | merge 後用 final config 產 prompt |
| `4x batch` | 內部呼叫 run，間接受益 |
| `4x status` | 不需 merge，只讀 state.json |
| `4x config` | 只操作 user-level |
| `4x live` | project editor 顯示原始 project config；global editor 顯示原始 user config |

## `4x config` CLI 擴展

支援 dot notation 操作所有 user-level 欄位：

```bash
# 基本欄位
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude

# runner 設定
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true

# role 設定
4x config set role.designer.model opus
4x config set role.reviewer.deep_model opus

# 查詢
4x config get runner.claude.command
4x config list
```

`args`（陣列）不支援 CLI set，回錯誤提示使用者直接編輯 JSON。

## Dashboard

### Global settings editor

- **入口**：sidebar 或 header 加 "Global Settings" 連結，跟 project settings gear icon 平行
- **編輯範圍**：只顯示 `UserConfig` 欄位（locale、theme、default_runner、runners、roles）
- **UI**：form + JSON 雙模式，跟 project settings editor 一致
- **讀寫**：`~/.4x/settings.json`，寫前備份 `.bak`

### API

| Method | Path | 說明 |
|---|---|---|
| GET | `/api/user-config` | 讀取 user config |
| PUT | `/api/user-config` | 寫入 user config（備份 .bak） |
| GET | `/api/merged-config` | 讀取 merge 後的 final config（唯讀） |

### Project settings editor 行為

- 顯示 `.4x/settings.json` 的**原始內容**，不是 merge 後的結果
- project 沒寫 runners 時，runners 區塊就是空的，不填入 user-level 的值
- 底部或另一個 tab 提供 merged config 唯讀 view，讓使用者確認最終生效的設定

## Boolean merge 注意事項

Go 的 `bool` 零值是 `false`，無法區分「沒設」和「設為 false」。`tty`、`stdin`、`quiet` 這三個 bool 欄位在 merge 時需要特殊處理：

方案：RunnerConfig 的 bool 欄位改為 `*bool`（pointer），nil 表示沒設，non-nil 表示明確設定。這樣 merge 時可以正確判斷 project 是否有意覆蓋。

```go
type RunnerConfig struct {
    Command  string            `json:"command,omitempty"`
    Args     []string          `json:"args,omitempty"`
    Model    string            `json:"model,omitempty"`
    Tiers    map[string]string `json:"tiers,omitempty"`
    Stdin    *bool             `json:"stdin,omitempty"`
    Tty      *bool             `json:"tty,omitempty"`
    Quiet    *bool             `json:"quiet,omitempty"`
}
```
