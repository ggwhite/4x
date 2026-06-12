# F023 — MCP Server

## 概述

為 4x CLI 加上 MCP (Model Context Protocol) server，讓外部 LLM agent（Claude Code、Cursor 等）透過結構化 tool call 操作 4x，不需要 parse CLI 文字輸出。

MCP server 是 **thin wrapper**：每個 tool 內部 exec `4x <cmd> --json`，parse stdout JSON 後回傳。不取代 plugin runner，不驅動 Design-Code-Review-Test loop。

## 架構

```
LLM Agent (Claude Code / Cursor / etc.)
  |  MCP protocol (stdio JSON-RPC 2.0)
  v
4x mcp  (cmd/4x/mcp.go → internal/mcp/)
  |  exec("4x", "<cmd>", "--json")
  v
4x CLI  (既有指令)
```

- Transport：stdio only（不做 HTTP/SSE）
- SDK：`github.com/modelcontextprotocol/go-sdk`（官方 Go SDK）
- MCP server name：`4x`，version 從 build info 取

## 兩階段實作

### 階段 A：CLI `--json` 補齊

在以下指令加 `--json` flag，輸出結構化 JSON 到 stdout。加了 `--json` 時不輸出任何非 JSON 文字（包括 warning）。

#### `4x status --json`

列出所有 features：

```json
{
  "features": [
    {
      "id": "F023-mcp-server",
      "name": "MCP server",
      "status": "in-progress",
      "phase": "coding",
      "role": "coder",
      "round": 2,
      "maxRounds": 5,
      "active": true,
      "runner": "claude"
    }
  ]
}
```

#### `4x status <id> --json`

單一 feature 詳情：

```json
{
  "feature": {
    "id": "F023-mcp-server",
    "name": "MCP server",
    "status": "in-progress",
    "description": "...",
    "priority": 0,
    "repos": {"self": "."},
    "subtasks": [{"id": "mcp-server", "name": "...", "status": "not-started"}],
    "rules": ["..."],
    "depends": []
  },
  "state": {
    "phase": "coding",
    "role": "coder",
    "round": 2,
    "maxRounds": 5,
    "active": true,
    "runner": "claude"
  }
}
```

state 為 null 表示尚未初始化（沒有 state.json）。

#### `4x new --json "feature name"`

```json
{
  "featureId": "F024-feature-name",
  "name": "feature name",
  "path": ".4x/features/F024-feature-name.yaml"
}
```

#### `4x transition <id> --to <phase> --json`

```json
{
  "featureId": "F023-mcp-server",
  "from": "coding",
  "to": "reviewing"
}
```

失敗時 exit code 非零，JSON 包含 error：

```json
{
  "error": "invalid transition from coding to done"
}
```

#### `4x run <id> --json`

啟動 run 後立刻回傳（不等 run 結束）：

```json
{
  "featureId": "F023-mcp-server",
  "runner": "claude",
  "maxRounds": 5,
  "pid": 12345
}
```

#### `4x check <id> --json`

已有，維持現有 JSON 格式。

### 階段 B：MCP Server

#### Subcommand

```bash
4x mcp            # 啟動 MCP server（stdio mode，blocking）
4x mcp --version  # 顯示 MCP server 版本資訊
```

不需要額外 flag — server 自動偵測 workspace（同其他 4x 指令的 Find walk-up 機制）。

#### 檔案結構

```
cmd/4x/mcp.go              # Cobra subcommand，建立 server + run
internal/mcp/
  server.go                 # MCP server 建立、tool 註冊
  tools.go                  # 6 個 tool handler
  exec.go                   # exec helper：跑 4x CLI 拿 JSON output
  exec_test.go              # exec helper 測試
  tools_test.go             # tool handler 測試（mock exec）
```

#### MCP Tools

| Tool | Description | 參數 | 對應 CLI |
|---|---|---|---|
| `4x_status` | 列出 features 及狀態 | `featureId?` string | `4x status [<id>] --json` |
| `4x_new` | 建立新 feature | `name` string, `description?` string | `4x new --json "<name>"` |
| `4x_run` | 啟動 Design-Code-Review-Test loop | `featureId` string, `runner?` string, `maxRounds?` int | `4x run <id> --json [--runner X] [--max-rounds N]` |
| `4x_stop` | 終止執行中的 run | `featureId` string | 寫 `state.Active = false`（見下方說明） |
| `4x_check` | 執行 guardrail 檢查 | `featureId` string | `4x check <id> --json` |
| `4x_transition` | 手動推進 phase | `featureId` string, `to` string | `4x transition <id> --to <phase> --json` |

#### Tool Handler 模式

每個 handler 遵循同一模式：

1. 從 MCP request 取參數
2. 組裝 `4x` CLI args（含 `--json`）
3. `exec.Command("4x", args...)` 執行
4. 讀 stdout，判斷 exit code
5. exit 0 → stdout JSON 作為 tool result 回傳
6. exit 非零 → 回傳 error content（含 stderr + stdout）

#### 4x_stop 實作

CLI 目前沒有 `run --stop` flag。stop 的機制是設定 `state.json` 的 `active: false`——run loop 每輪開始前檢查此欄位，false 時自動停止。

`4x_stop` tool handler 不走 exec 模式，改為直接讀寫 state.json：
1. 讀 `.4x/<featureId>/state.json`
2. 設 `active = false`、`stopReason = "mcp-stop"`
3. 寫回 state.json
4. 回傳 `{featureId, stopped: true}`

這是唯一不走 exec CLI 的 tool，因為 CLI 尚無對應指令。未來若加了 `4x stop` 指令，此 handler 改為 exec 模式。

#### exec helper

```go
// Run 執行 4x CLI 指令並回傳 stdout JSON
func Run(ctx context.Context, args ...string) (json.RawMessage, error)
```

- 自動加 `--json` 到 args
- timeout 透過 context 控制
- working directory 繼承 MCP server 的 cwd

#### 錯誤處理

- CLI exit 非零 → MCP tool result 的 `isError: true`，content 包含 error 訊息
- CLI 不存在 / exec 失敗 → MCP error response
- JSON parse 失敗 → 回傳原始 stdout 文字 + 警告

## 使用方式

### Claude Code

```json
{
  "mcpServers": {
    "4x": {
      "command": "4x",
      "args": ["mcp"]
    }
  }
}
```

### Cursor

```json
{
  "mcpServers": {
    "4x": {
      "command": "4x",
      "args": ["mcp"]
    }
  }
}
```

## 不做的事

- 不做 HTTP/SSE transport — stdio 足夠，且是 MCP 標準配置
- 不取代 plugin runner — MCP 是操作入口，plugin 負責 loop 內的 role 執行
- 不在 MCP layer 做 state validation — 交給 CLI 的 state machine
- 不做 MCP resources / prompts — 只做 tools
- 不做認證 — stdio mode 不需要

## 測試策略

- `internal/mcp/exec_test.go`：mock exec，驗證 args 組裝和 JSON parse
- `internal/mcp/tools_test.go`：mock exec helper，驗證每個 tool 的參數處理和 error mapping
- `cmd/4x/cli_test.go`：各指令 `--json` flag 的整合測試
- 手動驗證：在 Claude Code 中實際設定 MCP server 並呼叫 tools
