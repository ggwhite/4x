# 設定

## 專案設定（`.4x/settings.json`）

由 `4x init` 建立。包含專案中繼資料、runner 定義和角色模型對應。

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus",
      "tty": true
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project 區段

| 欄位 | 說明 |
|---|---|
| `name` | 專案名稱（從目錄自動偵測） |
| `language` | 偵測到的語言 |
| `build` | 建置命令 |
| `test` | 測試命令 |
| `lint` | Lint 命令 |
| `setup` | 初始設定命令（例如 `docker-compose up -d`） |
| `docs` | 供 Designer 參考的文件檔案路徑 |
| `rules` | 注入到角色 prompt 的專案特定規則 |

### Runner 設定

| 欄位 | 說明 |
|---|---|
| `command` | 可執行檔名稱 |
| `args` | 參數。`{prompt}` 和 `{promptFile}` 會在執行時被替換。`{model}` 會被替換為該角色的模型。 |
| `model` | 此 runner 的預設模型 |
| `model_map` | 角色模型名稱與 runner 專用名稱的對應表（例：`{"opus": "claude-opus-4-5-20250514"}`）。查找順序：角色 model → model_map 翻譯 → 回退原名。 |
| `tty` | 使用 PTY 擷取輸出（Claude Code 等有 ANSI 輸出的 CLI 工具需要） |
| `stdin` | 透過 stdin 而非參數傳送 prompt（Codex 使用） |
| `quiet` | 抑制 runner 的終端 stdout 輸出；輸出仍會寫入 log 檔案 |

如果 `args` 中沒有 `{model}`，runner 會自動附加 `--model <model>`。

### 角色設定

| 欄位 | 說明 |
|---|---|
| `model` | 此角色的模型名稱 |
| `deep_model` | 對抗式審查的模型（僅限 reviewer） |
| `instructions` | 注入到角色 prompt 的額外指示 |
| `includes` | 要引入角色 prompt 的檔案 |

### 其他設定欄位

| 欄位 | 說明 |
|---|---|
| `hub_repos` | 共用的 repository（用於批次 DAG 分組） |
| `isolation` | 設為 `"worktree"` 以在 git worktree 中執行 feature |
| `max_concurrent_runs` | 透過儀表板伺服器的最大同時執行數 |
| `commit` | Commit 策略：`"per-round"`（預設）、`"on-done"` 或 `"never"` |

## 使用者設定（`~/.4x/settings.json`）

全域使用者偏好設定。透過 `4x config` 管理。

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

### 語系

設定角色 prompt 指示的語言。支援的值：

| 值 | 語言 |
|---|---|
| `en` | 英文（預設） |
| `zh-TW` | 繁體中文 |
| `zh-CN` | 簡體中文 |
| `ja` | 日文 |
| `ko` | 韓文 |
| `es` | 西班牙文 |
| `fr` | 法文 |
| `de` | 德文 |
| `pt` | 葡萄牙文 |
| `ru` | 俄文 |
| `vi` | 越南文 |

如果未明確設定，語系也會從 `LANG` 環境變數推斷。
