# Runner 與 Plugin

## 什麼是 Runner？

Runner 是 4x CLI 和 AI 工具之間的橋梁。CLI 產生角色 prompt 並管理狀態；runner 將 prompt 傳送給 AI 並擷取輸出。

Runner 在 `.4x/settings.json` 的 `runners` 區段中設定。CLI 以子程序方式呼叫 runner。

## 內建 Runner

| Runner | AI 工具 | 模式 | 狀態 |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | 可用 |
| `codex` | OpenAI Codex CLI | Stdin | 可用 |
| `gemini` | Google Gemini CLI | Argument | 可用 |
| `agy` | Antigravity CLI | Argument | 可用 |
| `copilot` | GitHub Copilot CLI | Argument | 可用（需手動設定） |
| `cursor` | Cursor IDE | Rules file | 可用（需手動設定） |

`4x init` 預設會設定 claude、codex、gemini 和 agy。Copilot 和 cursor 需要手動加入 `settings.json`。

## Plugin 檔案

每個 runner 都有嵌入在 `4x` binary 中的指令檔。`4x init` 會將它們部署到 `.4x/plugins/` 並在根層級檔案中加入 import 行：

| Runner | Plugin 檔案 | 根層級 Import |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` + `workflow.js` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

此外，共用指令檔會部署到 `.4x/plugins/shared/` 供所有 runner 使用：

| 檔案 | 用途 |
|---|---|
| `shared/CREATOR.md` | Feature Creator 流程 — 引導 AI 透過 `4x new` 建立 feature |

更新 binary 後使用 `4x upgrade` 重新部署 plugin 檔。

## Runner 執行模型

```
4x run F001 --runner claude
    │
    ├── 為當前角色產生 prompt
    ├── 以 prompt 呼叫 runner 子程序
    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
    ├── 擷取輸出到 .4x/F001/logs/round-N-role.log
    ├── 檢查輸出 artifact
    └── 轉換狀態，重複
```

### Exit Code

| Code | 意義 | 動作 |
|---|---|---|
| 0 | 成功 | 進入下一階段 |
| 1 | 軟失敗 | Feature 移到 `blocked` |
| 2 | 硬錯誤 | 迴圈停止，需要關注 |
| timeout | 在限制時間內無回應 | 視為軟失敗 |

### Stream JSON 模式

設定 `output_format: "stream-json"` 的 runner 會寫入兩種檔案：dashboard tail 的人類可讀 `.log`，以及供除錯使用的原始 `.stream.jsonl`。Claude Code 預設使用此模式。

### PTY 模式

設定 `tty: true` 的 runner 使用偽終端機來擷取完整輸出，包含 ANSI 跳脫序列。一個有狀態的 ANSI 清除器會清理日誌檔。`output_format` 為 `"stream-json"` 時會略過此路徑。

### Stdin 模式

設定 `stdin: true` 的 runner（Codex）透過標準輸入而非命令列參數接收 prompt。

## 為不同角色使用不同模型

在 `.4x/settings.json` 中設定：

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

你也可以混搭 runner — 用 Claude 做 Design、Gemini 做 Code 等 — 透過手動使用不同的 `--runner` 旗標執行各階段，並用 `4x transition` 在階段間切換。

## 撰寫 Plugin

Plugin 遵循簡單的合約 — 讀取 `.4x/` 檔案、執行 AI 工作、將結果寫回：

1. 讀取 `.4x/features/{id}.yaml` 了解 feature
2. 讀取 `state.json` 了解當前階段
3. 讀取階段特定的輸入（task-brief.md、scope 等）
4. 執行工作（呼叫你的 LLM、執行工具）
5. 寫入階段特定的產出（coder-report.md、review-report.md 等）
6. 以適當的 exit code 結束（0 = 成功、1 = 軟失敗、2 = 硬錯誤）

不需要 SDK。不需要執行時期依賴。只有檔案。
