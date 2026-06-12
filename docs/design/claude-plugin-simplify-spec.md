# Claude Code Plugin 簡化 — 移除 Workflow，統一 per-phase runner

## 背景

Claude Code plugin 目前有兩條執行路線：

1. **Skill + Workflow 路線**：在 Claude Code session 內觸發 SKILL.md，用 `workflow.js` 編排所有 role（designer/coder/reviewer/tester/acceptor），subagent 間靠檔案溝通
2. **`4x run` per-phase 路線**：Go 的 `runLoop` 每個 phase spawn 一次 `claude -p {prompt}`，跟 agy/gemini 完全一致

實際使用中只走路線 2。路線 1 帶來了多個問題：
- Workflow subagent context 斷裂，無法看到完整上下文
- 用 raw echo 寫 state.json 繞過 `4x transition`，導致 pending-review gate 失效（剛修的 bug）
- 維護兩套編排邏輯，架構不一致

## 目標

刪除 Workflow 路線，讓 Claude Code plugin 與 agy/gemini 架構統一：一個 context 檔提供 role 契約，`4x run` 負責編排。

## 變更範圍

### 1. 新增 `plugins/claude-code/CLAUDE.md`

內容對標 `plugins/agy/AGY.md`，提供：
- Protocol 說明（`.4x/` 目錄結構）
- Role 契約表（讀/寫/禁止）
- Guardrail 檢查指引
- Exit code 約定
- Escalation 格式

### 2. 刪除

- `plugins/claude-code/SKILL.md` — skill 定義（不再需要）
- `plugins/claude-code/workflow.js` — workflow 腳本（不再需要）

### 3. 更新 `plugins/embed.go`

```go
//go:embed claude-code/CLAUDE.md codex/AGENTS.md gemini/GEMINI.md agy/AGY.md cursor/.cursorrules copilot/AGENTS.md copilot/workflow.js
```

### 4. 更新 `cmd/4x/deploy.go`

```go
case "claude":
    return []pluginDeploy{
        {EmbedPath: "claude-code/CLAUDE.md", PluginName: "CLAUDE.md", RootFile: "CLAUDE.md"},
    }
```

只部署一個 context 檔，不再部署 workflow.js。

### 5. 更新 `CLAUDE.md`（專案根）

Plugin Development 段落移除 SKILL.md 和 workflow.js 的提及，改為描述 `CLAUDE.md` context 檔。Architecture 段落更新 `plugins/claude-code/` 描述。

## 不變的部分

- `cmd/4x/run.go` 的 `runLoop`、`prompt.go` 的 template、`resolveModel` — 已經正常運作
- `settings.json` 的 runners.claude 設定 — 不變
- 其他 runner plugin — 不受影響
- Dashboard、state machine、guardrail — 不受影響

## 驗證

- `go build ./cmd/4x && go vet ./... && go test ./...` 通過
- `4x run <feature> --runner claude` 行為不變（本來就走 per-phase）
- `4x init` 後 `.4x/plugins/CLAUDE.md` 內容正確、根目錄 `CLAUDE.md` 有 `@import`
