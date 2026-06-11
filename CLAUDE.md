# 4x — Multi-Role AI Development Loop

## Quick Start

```bash
make build          # 編譯 CLI 至 bin/4x
make test           # go test ./...
make lint           # go vet ./...
```

## Architecture

三層架構，CLI 層不呼叫 LLM：

```
cmd/4x/             CLI entry point (Cobra)
internal/
  protocol/          .4x/ 檔案格式、Workspace 讀寫、型別定義
  state/             狀態機（phase transition）
  guard/             Guardrail 檢查（scope/baseline/required files）
  batch/             Batch DAG 排程
  server/            WebSocket server（Dashboard 用）
plugins/
  claude-code/       Claude Code skill + workflow.js
dashboard/
  macos/             Swift native app
schemas/             JSON Schema（state/event/feature）
templates/           Role prompt templates（.md.tmpl）
docs/                權威設計規格（見 docs/AGENTS.md 索引）
```

## State Machine

```
init → designing → coding → reviewing → testing → accepting → done
                     ↑          ↓           ↓
                     └── amending ←─────────┘
any → blocked / needs-attention
```

合法轉換定義在 `internal/state/machine.go`。

## Protocol (.4x/ directory)

所有 role 間的通訊走 `.4x/` 目錄內的檔案。Schema 定義在 `internal/protocol/types.go`，常量在 `internal/protocol/workspace.go`。

## Development Rules

- Go 1.26+，遵循 gofmt
- Cobra 作為 CLI 框架，每個 subcommand 一個檔案 `cmd/4x/{cmd}.go`
- 內部 package 放 `internal/`，不 export 給外部
- CLI 層嚴禁呼叫 LLM — 所有 AI 互動由 plugin 負責
- `docs/` 是權威設計規格，索引見 `docs/AGENTS.md`；實作與規格衝突時以規格為準
- 測試用 Go 標準 testing package，測試檔放與被測程式同目錄

## Verification

每次改動後至少跑：

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

## Plugin Development

Plugin 合約見 `docs/reference/plugin-contract.md`。Claude Code plugin 在 `plugins/claude-code/`：
- `SKILL.md` — skill 定義（觸發方式、流程）
- `workflow.js` — Claude Code Workflow 腳本

## Docs Routing

```
docs/design/
├── {feature-id}-spec.md    ← 設計規格（brainstorming 產出）
├── {feature-id}-plan.md    ← 實作計畫（brainstorming 產出）
```

- superpowers brainstorming spec/plan 存到 `docs/design/`，不存到 `docs/superpowers/`
- 架構文件：`docs/architecture/`
- 參考資料：`docs/reference/`

## Current State

見 `progress.md` 了解目前進度，`feature_list.json` 列出待做功能。
