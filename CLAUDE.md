@.4x/plugins/CLAUDE.md

# 4x — Multi-Role AI Development Loop

## Quick Start

```bash
make build          # 編譯 CLI 至 bin/4x
make test           # go test ./...
make lint           # go vet + gofmt + golangci-lint
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
  copilot/           Copilot CLI runner instructions
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
init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                                        ↑          ↓           ↓            ↓
                                        └── amending ←──────────┴────────────┘
design-reviewing → designing (FAIL)
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
make build && make test && make lint
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

每次改完程式碼、commit 前，跑以下兩個檢查：

```bash
make check-docs-sync          # 比對 git diff，列出需更新的 docs（可加 BASE=HEAD~3）
make check-i18n               # 檢查多國語系 key 是否同步（以 en.json 為基準）
```

- `check-docs-sync` 輸出 `NEEDS_UPDATE` → 只更新被點名的 doc 檔
- `check-i18n` 輸出 `ERROR: missing keys` → 補齊對應語系的缺漏 key
- 兩者都 OK → 不需額外動作
- **禁止手動掃描**：不要自己讀所有 doc 或 locale 檔來判斷是否需要更新，信任腳本輸出

CI 另外跑 `make check-docs` 驗證所有 subcommand 都出現在 `docs/guide/cli.md`。

## Feature Description 撰寫規則

feature 的 `description` 要讓 dashboard Overview 一眼看清全貌：

- **現狀**：用 1-2 句描述目前的問題或缺口
- **需求**：分點列出要做什麼，每點含具體細節（檔案路徑、欄位名、行為描述）
- **約束**：列出不能做的事、不引入的依賴、不改的行為
- **subtasks**：拆成可獨立驗收的子任務，每個有清楚的 id 和 name

`4x new` 支援 `--desc`、`--subtask`、`--rule`、`--depends`、`--priority` 一次建完。
ID 自動在 word boundary 截斷；用 `--id` 可指定完整 slug 不截斷。
參考範例：F034、F040 的 YAML description 格式。

## Troubleshooting

遇到環境、build、測試的坑時，先查 `docs/dev/troubleshoot.md`。解決新坑後 append 到該檔案。

## Current State

見 `progress.md` 了解目前進度，`feature_list.json` 列出待做功能。

## End of Session

Session 收尾時：

1. 檢查 `docs/reference/discovered-feature-gaps.md` 有沒有本次 session 新增、尚未處理（沒有 `[已開 FXXX]` 標記）的項目，提醒使用者決定是否要開新 feature。
2. 若解決過環境、工具、build 相關的坑，且 `docs/dev/troubleshoot.md` 尚無記錄，append 新條目。

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

@.4x/learnings-context.md
