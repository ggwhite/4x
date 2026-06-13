# F038: 4x doctor — Runner Health Check & LLM Usage

## 目標

新增 `4x doctor` subcommand，一次檢視所有 runner 的安裝狀態與 LLM 使用量。
同時在 dashboard 新增獨立 Doctor 頁面，提供 web 端的即時檢視。

## 動機

4x 支援多個 LLM runner（claude、codex、gemini、agy、copilot、cursor），
使用者需要快速確認：
1. 哪些 runner CLI 有安裝、版本是否正確
2. 各 LLM 的歷史使用量與成本

目前沒有統一入口——使用者得逐一 `which` + 進各 CLI 跑 `/usage`。
`4x doctor` 把這些整合成一個指令。

## 設計

### 兩階段檢查

#### 階段一：Runner 偵測

對 `settings.json` 中每個 runner：
1. `which <command>` — 確認 CLI 存在
2. `<command> --version` — 取得版本號
3. 解析版本字串（各 CLI 格式不同，取第一個 semver-like 子串）

輸出：每個 runner 的 name、command、installed（bool）、version（string）。

#### 階段二：Usage 報告

Shell out `ccusage daily --json`，解析 JSON 輸出。

ccusage 回傳格式：
```json
{
  "daily": [
    {
      "agent": "all",
      "period": "2026-06-12",
      "inputTokens": 221810,
      "outputTokens": 8137,
      "cacheReadTokens": 426739,
      "cacheCreationTokens": 0,
      "totalTokens": 663361,
      "totalCost": 0.176,
      "modelsUsed": ["claude-opus-4-6"],
      "metadata": { "agents": ["claude"] },
      "modelBreakdowns": [
        {
          "modelName": "claude-opus-4-6",
          "inputTokens": 221810,
          "outputTokens": 8137,
          "cacheReadTokens": 426739,
          "cacheCreationTokens": 0,
          "cost": 0.176
        }
      ]
    }
  ]
}
```

ccusage 未安裝時：usage 區塊顯示安裝提示 `npm i -g ccusage`，不 error。

### CLI 介面

```
4x doctor [flags]

Flags:
  --json    JSON 輸出
```

純文字輸出範例：
```
── Runners (5/6 installed) ──
  RUNNER    COMMAND   STATUS      VERSION
  claude    claude    ✓ installed  2.1.175
  codex     codex     ✓ installed  0.139.0
  gemini    gemini    ✓ installed  0.46.0
  agy       agy       ✓ installed  1.0.7
  copilot   copilot   ✓ installed  1.0.61
  cursor    agent     ✗ not found  -

── Usage (via ccusage) ──
  DATE        AGENTS         TOKENS      COST
  2026-06-10  claude         1.2M        $3.42
  2026-06-11  claude,codex   2.8M        $8.15
  2026-06-12  claude,gemini  850K        $2.30
                             ─────       ─────
  Total                      4.85M       $13.87
```

JSON 輸出格式：
```json
{
  "runners": [
    {
      "name": "claude",
      "command": "claude",
      "installed": true,
      "version": "2.1.175"
    }
  ],
  "usage": null,
  "ccusageAvailable": false,
  "ccusageHint": "npm i -g ccusage"
}
```

`usage` 欄位在 ccusage 可用時直接放 ccusage 的 `daily` 陣列，不做二次轉換。

### Dashboard

#### API

```
GET /api/doctor
```

回傳與 CLI `--json` 相同結構的 JSON。
Server 端邏輯：呼叫 `internal/doctor` 的同一組函式。

#### 前端

在 dashboard 導航列新增 "Doctor" 連結，點擊進入獨立頁面。

頁面分兩區：
- **上半部：Runner 狀態** — 卡片列表，每張卡片顯示 runner name、command、版本、綠燈（installed）或紅燈（not found）
- **下半部：Usage 表格** — 按日期列出使用量，每列可展開看 model breakdown

ccusage 未安裝時下半部顯示安裝引導。

### 套件結構

```
cmd/4x/doctor.go                — Cobra subcommand，呼叫 internal/doctor
internal/doctor/
  doctor.go                     — Report 組裝（DetectRunners + FetchUsage）
  detect.go                     — RunnerHealth 偵測邏輯
  usage.go                      — ccusage shell out + JSON 解析
  types.go                      — RunnerHealth / UsageDailyEntry / DoctorReport 型別
  doctor_test.go                — 單元測試
internal/server/
  server.go                     — 新增 GET /api/doctor handler
  static/index.html             — Doctor 頁面 UI
```

### 型別定義

```go
// RunnerHealth 是單一 runner 的健康狀態
type RunnerHealth struct {
    Name      string `json:"name"`
    Command   string `json:"command"`
    Installed bool   `json:"installed"`
    Version   string `json:"version,omitempty"`
}

// UsageModelBreakdown 是單一 model 的用量明細
type UsageModelBreakdown struct {
    ModelName           string  `json:"modelName"`
    InputTokens         int64   `json:"inputTokens"`
    OutputTokens        int64   `json:"outputTokens"`
    CacheReadTokens     int64   `json:"cacheReadTokens"`
    CacheCreationTokens int64   `json:"cacheCreationTokens"`
    Cost                float64 `json:"cost"`
}

// UsageDailyEntry 是 ccusage daily 的單日資料
type UsageDailyEntry struct {
    Period              string                `json:"period"`
    Agent               string                `json:"agent"`
    InputTokens         int64                 `json:"inputTokens"`
    OutputTokens        int64                 `json:"outputTokens"`
    CacheReadTokens     int64                 `json:"cacheReadTokens"`
    CacheCreationTokens int64                 `json:"cacheCreationTokens"`
    TotalTokens         int64                 `json:"totalTokens"`
    TotalCost           float64               `json:"totalCost"`
    ModelsUsed          []string              `json:"modelsUsed"`
    Metadata            map[string]any        `json:"metadata"`
    ModelBreakdowns     []UsageModelBreakdown `json:"modelBreakdowns"`
}

// DoctorReport 是 4x doctor 的完整報告
type DoctorReport struct {
    Runners           []RunnerHealth    `json:"runners"`
    Usage             []UsageDailyEntry `json:"usage"`
    CcusageAvailable  bool              `json:"ccusageAvailable"`
    CcusageHint       string            `json:"ccusageHint,omitempty"`
}
```

### Runner 偵測細節

版本字串解析策略（各 CLI 格式不同）：

| CLI | `--version` 輸出 | 擷取方式 |
|-----|------------------|---------|
| claude | `2.1.175 (Claude Code)` | 取第一個 token |
| codex | `codex-cli 0.139.0` | 取最後一個 token |
| gemini | `0.46.0` | 整行即版本 |
| agy | `1.0.7` | 整行即版本 |
| copilot | `GitHub Copilot CLI 1.0.61.\n...` | regex `\d+\.\d+\.\d+` |

統一策略：用 regex `(\d+\.\d+[\.\d]*)` 從 `--version` 輸出的前 200 bytes 擷取第一個 match。

### 邊界情況

- **settings.json 不存在**（未 init）：`4x doctor` 仍可執行，runners 列表為空，只跑 ccusage
- **runner command 是 shell function**（如 agy）：`which` 抓不到，改用 `command -v` 偵測
- **ccusage 超時**：設 10 秒 timeout，超時視為不可用
- **ccusage 輸出格式變更**：JSON 解析失敗時 fallback 顯示 raw output

## 不做的事

- 不自己解析各 CLI 的本地檔案（JSONL/SQLite）——全靠 ccusage
- 不做 usage 的持久化快取——每次 doctor 即時查詢
- 不做 runner 自動修復——只診斷，不治療
- 不過濾特定 runner 的 usage——ccusage 回傳全部 agent 資料，直接顯示

## 驗證標準

1. `4x doctor` 正確列出所有 runner 安裝狀態與版本
2. ccusage 可用時，顯示完整 daily usage 表格
3. ccusage 不可用時，顯示安裝提示而非 error
4. `4x doctor --json` 輸出可被 jq 解析
5. `GET /api/doctor` 回傳與 CLI --json 相同結構
6. Dashboard Doctor 頁面正確渲染 runner 狀態卡片與 usage 表格
7. `go build && go vet && go test` 全過
