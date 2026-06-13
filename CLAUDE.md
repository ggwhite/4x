@.4x/plugins/CLAUDE.md

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
cmd/4x/             CLI entry point (Cobra)，每個 subcommand 一個檔案
internal/
  protocol/          .4x/ 檔案格式、Workspace 讀寫、型別定義
  state/             狀態機（phase transition）
  guard/             Guardrail 檢查（scope/baseline/required files）
  batch/             Batch DAG 排程
  runner/            Runner 介面與子程序執行
  server/            SSE + REST server（Dashboard 用）
plugins/
  claude-code/       Claude Code runner instructions
  gemini/            Gemini CLI runner instructions
  codex/             Codex CLI runner instructions
  agy/               Antigravity CLI runner instructions
  copilot/           Copilot CLI runner instructions + workflow.js
  cursor/            Cursor rules (.cursorrules)
  embed.go           go:embed 將 plugin 檔嵌入 binary
dashboard/
  macos/             Swift native app
schemas/             JSON Schema（state/event/feature）
templates/           Role prompt templates（.md.tmpl）
docs/                權威設計規格（見 docs/AGENTS.md 索引）
```

## State Machine

```
init → designing → coding → reviewing → deep-reviewing → testing → accepting → done
                     ↑          ↓              ↓
                     └── amending ←─────────────┘
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
- `CLAUDE.md` — runner context 檔（role 契約、protocol、guardrail）

## Docs Routing

```
docs/guide/               ← 使用說明書（人類可讀，`make check-docs` 驗證同步）
docs/design/
├── {feature-id}-spec.md  ← 設計規格（brainstorming 產出）
├── {feature-id}-plan.md  ← 實作計畫（brainstorming 產出）
```

- superpowers brainstorming spec/plan 存到 `docs/design/`，不存到 `docs/superpowers/`
- 架構文件：`docs/architecture/`
- 參考資料：`docs/reference/`

## Documentation Maintenance

改動 CLI 或核心功能時，必須同步更新 `docs/guide/`：

| 改了什麼 | 要更新 |
|---|---|
| 新增 / 刪除 subcommand（`cmd/4x/*.go`） | `docs/guide/cli.md` + README 指令表 |
| 修改 flag（新增、刪除、改預設值） | `docs/guide/cli.md` |
| 狀態機轉換（`internal/state/machine.go`） | `docs/guide/concepts.md` 狀態機段落 |
| 檔案協議（`.4x/` 目錄結構、檔名） | `docs/guide/concepts.md` 檔案協議段落 |
| Runner / Plugin（新增、改合約） | `docs/guide/runners.md` |
| Settings 欄位（新增、改結構） | `docs/guide/configuration.md` |
| Dashboard API（endpoint 增刪改） | `docs/guide/dashboard.md` |
| Batch 行為 | `docs/guide/batch.md` |

CI 跑 `make check-docs` 會檢查 subcommand 是否都出現在 `docs/guide/cli.md`，漏了會 fail。

## Feature Description 撰寫規則

feature 的 `description` 要讓 dashboard Overview 一眼看清全貌：

- **現狀**：用 1-2 句描述目前的問題或缺口
- **需求**：分點列出要做什麼，每點含具體細節（檔案路徑、欄位名、行為描述）
- **約束**：列出不能做的事、不引入的依賴、不改的行為
- **subtasks**：拆成可獨立驗收的子任務，每個有清楚的 id 和 name

`4x new` 支援 `--desc`、`--subtask`、`--rule`、`--depends`、`--priority` 一次建完。
ID 自動在 word boundary 截斷；用 `--id` 可指定完整 slug 不截斷。
參考範例：F034、F040 的 YAML description 格式。

## Current State

見 `progress.md` 了解目前進度，`feature_list.json` 列出待做功能。
