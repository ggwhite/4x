# F041: Doctor Per-Runner Usage with Rate Limits

## 現狀

Doctor 頁面透過 ccusage 顯示 cost/tokens 彙總，但缺少最重要的資訊：**rate limit 百分比**（5h session、7d weekly 各用了多少 %）。每個 LLM runner 的 usage 資料來源不同，目前沒有統一呈現。

## 需求

### Claude Code — statusLine hook 取得百分比

- 新增 `4x _statusline` 隱藏子指令，作為 Claude Code 的 statusLine hook
- Claude Code 每次刷新會推送 JSON（含 `rate_limits.five_hour.used_percentage`、`rate_limits.seven_day.used_percentage`、`resets_at`）到 stdin
- Hook 提取 rate_limits，加上 `updated_at` timestamp，原子寫入 `~/.4x/usage/claude.json`
- stdout 回傳格式化 status line 文字給 Claude Code 顯示
- 執行時間 < 50ms，不做網路請求
- `4x doctor --install-hook` 安裝 hook 到 `~/.claude/settings.json`
- `4x doctor --uninstall-hook` 移除

### Claude Code — 合併資料

- 讀 `~/.4x/usage/claude.json`（百分比 + reset time），有效期 10 分鐘
- 搭配 ccusage claude blocks（cost、tokens、burn rate、projection、models）
- 合併為完整的 Claude usage card

### Codex — 直接讀 sqlite

- 唯讀開啟 `~/.codex/logs_2.sqlite`
- 查詢最新的 websocket event 中 `rate_limits.primary`（5h）和 `rate_limits.secondary`（7d）
- 取得 remaining/limit/reset，算出 % used
- 搭配 ccusage codex daily 取 cost/tokens 彙總

### Gemini — ccusage daily

- ccusage gemini daily --since 7d --json 取 cost/tokens 彙總
- 無 rate limit 資料

### 其他 runner（agy/copilot/cursor）

- 只顯示安裝狀態 + 版本
- 無 programmatic usage API 可用

## Dashboard UI

每個 runner 一張卡片，2 欄 grid 排列。

### Claude 卡片（資料最豐富）

- Header: 名稱、版本、安裝狀態
- 5H SESSION 區塊：進度條 + `XX% used` + reset time + remaining
- 數字 grid：cost、projected、tokens、burn rate
- Model badges
- 分隔線
- 7D WEEKLY 區塊：進度條 + `XX% used` + reset time + remaining
- 7d cost/tokens

### Codex 卡片

- Header: 名稱、版本
- 5H：進度條 + `XX% used`
- 7D：進度條 + `XX% used`
- cost/tokens 彙總

### Gemini 卡片

- Header + 7d cost/tokens

### 無資料 runner

- Header + "No usage data"

### 進度條顏色

- < 50%：綠 `#10b981`
- 50-80%：黃 `#f59e0b`
- \> 80%：紅 `#ef4444`

## CLI

`4x doctor` 同步更新，顯示跟 dashboard 一致的資訊：

```
── claude ✓ 2.1.177 ──
  5h   ██████░░░░░░░░░░░░░░ 12% used (4h13m left, resets 23:50)
       $34, 41.5M tok, $47/hr burn
  7d   ██░░░░░░░░░░░░░░░░░░ 7% used (1d11h left, resets Jun 15)
       $1349, 2.0B tok

── codex ✓ 0.139.0 ──
  5h   ░░░░░░░░░░░░░░░░░░░░ 1% used
  7d   ██████████░░░░░░░░░░ 49% used
       $82.73, 77.8M tok
```

## Bug Fix

- `loadDetail(task)` 沒有隱藏 `doctor-panel`，從 Doctor 頁面點進 feature 細節時 doctor 內容殘留
- 修法：在 `loadDetail` 開頭加 `document.getElementById('doctor-panel').classList.add('hidden')`

## 約束

- 不自己估算 rate limit 百分比，只用 Anthropic/OpenAI 官方給的值
- ccusage 是 optional 增強，沒裝時 graceful degrade
- statusLine hook 沒安裝時，Claude 退回只用 ccusage blocks（無百分比）
- 不做 real-time polling，Doctor 是按需查看
- `4x _statusline` 是隱藏子指令，不出現在 help

## 新增/修改檔案

| 檔案 | 說明 |
|---|---|
| `cmd/4x/statusline.go` | `_statusline` 隱藏子指令 |
| `cmd/4x/doctor.go` | CLI 輸出整合百分比 |
| `internal/doctor/types.go` | 新增 `RateLimit` 型別 |
| `internal/doctor/hook.go` | hook 安裝/移除 |
| `internal/doctor/claude.go` | 讀 `~/.4x/usage/claude.json` |
| `internal/doctor/codex.go` | 讀 `~/.codex/logs_2.sqlite` |
| `internal/doctor/usage.go` | ccusage 整合重構 |
| `internal/doctor/doctor.go` | 合併所有來源 |
| `internal/server/static/index.html` | Dashboard renderDoctor |
| `docs/guide/cli.md` | `--install-hook` / `--uninstall-hook` |
| `docs/guide/dashboard.md` | Doctor 頁面說明 |

## 資料流

```
Claude Code ──stdin JSON──→ 4x _statusline ──→ ~/.4x/usage/claude.json
                                                        ↓
4x doctor ←── merge ←── claude.json (%) + ccusage blocks (cost/tok)
          ←── merge ←── codex sqlite (%) + ccusage daily (cost/tok)
          ←── ccusage gemini daily (cost/tok)
          ←── detect only (agy/copilot/cursor)
                                                        ↓
                                              CLI output / API JSON
                                                        ↓
                                              Dashboard renderDoctor
```
