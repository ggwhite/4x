# 設定

## 專案設定（`.4x/settings.json`）

由 `4x init` 建立。包含專案中繼資料、runner 定義和角色模型對應。

你也可以從 **4x Live 儀表板**視覺化編輯此檔案——點擊「4x Live」標題旁的齒輪圖示（⚙），或按 `Cmd+Shift+,`。編輯器支援表單視圖和原始 JSON 視圖，驗證必填欄位，寫入前將先前設定備份到 `settings.json.bak`。

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
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
      "model": "opus",
      "output_format": "stream-json"
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
  "default_runner": "claude",
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
| `description` | 專案描述（選用） |
| `docs` | 供 Designer 參考的文件檔案路徑 |
| `rules` | 注入到角色 prompt 的專案特定規則 |
| `includes` | 要引入角色 prompt 的檔案 |

### Runner 設定

| 欄位 | 說明 |
|---|---|
| `command` | 可執行檔名稱 |
| `args` | 參數。`{prompt}` 和 `{promptFile}` 會在執行時被替換。`{model}` 會被替換為該角色的模型。 |
| `model` | 此 runner 的預設模型 |
| `tiers` | tier 名稱到 runner 專用模型名稱的對應表（例：`{"opus": "claude-opus-4-5-20250514"}`）。查找順序：角色 model → tiers 翻譯 → 回退原名。 |
| `output_format` | 設為 `"stream-json"` 時，runner stdout 會解析成人類可讀 `.log` 與原始 `.stream.jsonl`。 |
| `tty` | 使用 PTY 擷取輸出。`output_format` 為 `"stream-json"` 時會略過 PTY。 |
| `stdin` | 透過 stdin 而非參數傳送 prompt（Codex 使用） |
| `quiet` | 抑制 runner 的終端 stdout 輸出；輸出仍會寫入 log 檔案 |

如果 `args` 中沒有 `{model}`，runner 會自動附加 `--model <model>`。

### 角色設定

| 欄位 | 說明 |
|---|---|
| `model` | 此角色的模型名稱 |
| `deep_model` | 對抗式審查的模型（僅限 reviewer）。**`deep-reviewing` 階段執行的必要條件** — 未設定時該階段被跳過，流程直接從 `testing` 跳到 `accepting`。 |
| `max_fix_rounds` | `deep-reviewing` 階段的最大自我修復迭代次數（僅限 deep-reviewer；預設 2）。每次迭代執行範圍限定的 mini-coder + re-verifier；超過上限則升級到 `needs-attention`。 |
| `instructions` | 注入到角色 prompt 的額外指示 |
| `includes` | 要引入角色 prompt 的檔案 |
| `screenshot_dir` | Tester 截圖的目錄路徑 |
| `parallel_reviewers` | deep review 的平行子審查者數量（僅限 deep-reviewer；<=1 退回單一 agent 模式） |
| `angles_per_reviewer` | 每個子審查者的審查角度數（僅限 deep-reviewer；0 表示自動均分） |

### 其他設定欄位

| 欄位 | 說明 |
|---|---|
| `hub_repos` | 共用的 repository（排除在以 repo 為單位的範圍群組化之外） |
| `isolation` | 設為 `"worktree"` 以在 git worktree 中執行 feature |
| `max_concurrent_runs` | 透過儀表板伺服器的最大同時執行數 |
| `commit` | Commit 策略：`"per-round"`（預設）、`"on-done"` 或 `"never"` |
| `profiles` | 具名的 pipeline profile（角色子集）；見 [Profiles](#profiles) |
| `parallel_review_test` | 在 reviewing 階段同時執行 reviewer 和 tester（預設 `false`） |
| `auto_discover_features` | 從 deep review 報告中的 `[NEW-FEATURE]` 標記自動建立 feature（預設 `false`）；見 [Auto-Discover Features](#auto-discover-features) |
| `workspace` | 多 repo workspace 設定（repo 名稱 → 路徑映射） |
| `hooks` | 生命週期 hooks（以 hook 時機為 key，例如 post-run） |
| `health_check` | 全域的測試前環境檢查命令（可在 test-strategy.yaml 中依 feature 覆蓋） |
| `test_profiles` | 自訂或覆蓋的 test profile 定義（以 profile 名稱為 key） |
| `max_discovered_features` | 每次執行自動建立的最大 feature 數；未設定或 `<= 0` 時套用預設值（`3`） |

### Auto-Discover Features

當 `auto_discover_features` 為 `true` 時，執行迴圈會在最終 deep review 報告（`deep-review-report.md`）**通過**後解析它，將每個 `[NEW-FEATURE]` 標記轉為新的 feature YAML——捕獲 deep reviewer 發現的超出範圍問題，避免被埋在報告裡。

- **觸發時機**：僅在最終 deep review 通過時觸發（首次 PASS、或自我修復後的 PASS）。中間輪次、reviewer/tester 失敗、deep-review FAIL/needs-attention 路徑都不會觸發。
- **去重**：每個候選與所有既有 feature 的名稱+描述進行 token-overlap 相似度比較，也與同批已保留的候選比較。相似的候選會跳過。
- **上限**：每次執行最多建立 `max_discovered_features`（預設 `3`）個 feature；其餘記錄為 capped。
- **輸出**：在 `.4x/<feature-id>/` 下寫入 `discovered-features.md` 摘要，列出 created / skipped-as-duplicate / capped 的候選，每建立一個 feature 附加一個 `feature-discovered` 事件。

全部在 CLI 層完成（純文字解析 + 檔案寫入，無 LLM 呼叫），且不會阻擋轉換到 `accepting`——任何錯誤都以 best-effort 記錄。

### Profiles

Profile 選擇一個 feature 要執行哪些 phase，讓簡單 feature 跳過完整 pipeline。未列出的 phase 會被跳過——狀態沿合法邊推進但不呼叫 runner、不檢查 artifact、不執行 guard。`coding` 是唯一必須的 phase；缺少它的 profile 是設定錯誤。選用的 `design-reviewing` phase 只有在列入時才會執行，且其 `design-review-report.md` 必須 PASS 後才能進入 coding。

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

每個 phase 項目支援選用的 `runner` 和 `model` 覆蓋：

| 欄位 | 說明 |
|---|---|
| `phase` | Phase 名稱（必須是可選用的 phase：designing、design-reviewing、coding、reviewing、testing、deep-reviewing、accepting） |
| `runner` | 此 phase 的選用 runner 覆蓋 |
| `model` | 此 phase 的選用模型 tier 覆蓋 |

**選擇優先序：**

1. `4x run --profile <name>` — 明確覆蓋（先在 `profiles` 中查找，再查內建預設）。
2. 否則，若有 `profiles` 區段，依 feature 的 `priority` 自動選取：`null`/`0`/`1` → `full`、`2` → `normal`、`≥3` → `quick`。
3. 若無 `profiles` 區段，每個 feature 都跑 `full`（停用依 priority 自動選取——向後相容）。

三個內建 profile（`full`/`normal`/`quick`）即使沒有 `profiles` 區段也永遠可作為 fallback。啟用的 profile 名稱記錄在 feature state 中，並顯示在儀表板卡片上。

當 `parallel_review_test` 為 `true` 且啟用的 profile 同時包含 `reviewer` 和 `tester` 時，兩個唯讀角色會在 reviewing 階段於同一 worktree 中同時執行；兩者都通過才推進到 deep review，否則迴圈重新進入 coding。

## 使用者設定（`~/.4x/settings.json`）

全域使用者偏好與 runner 預設值。跨專案的設定透過 `4x config` 或儀表板的**全域設定**編輯器（側邊欄的 ⚙G 按鈕）管理。

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
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### 使用者設定欄位

| 欄位 | 說明 |
|---|---|
| `locale` | 角色 prompt 指示的語言 |
| `theme` | 儀表板主題（`dark`/`light`） |
| `default_runner` | 預設 runner 名稱（專案設定可覆蓋） |
| `runners` | Runner 定義（command、args、tty 等） |
| `roles` | 角色模型預設值 |
| `logLevel` | 最低 log 層級（debug/info/warn/error；預設 "info"；FOURX_LOG_LEVEL 環境變數可覆蓋） |
| `logRetainDays` | ~/.4x/logs/ 中 log 檔案的保留天數（預設 7） |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args` 是陣列欄位——直接編輯 `~/.4x/settings.json` 來設定。

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

## 設定合併

`4x run` 或 `4x prompt` 執行時，使用者層級和專案層級的設定會做深度合併：

- **優先序：** 專案 > 使用者 > 預設值
- **Runner 合併：** 依欄位——專案的非零欄位覆蓋使用者的。`args` 完全取代（不附加）。`tiers` 依 key 層級合併。
- **角色合併：** 依欄位——與 runner 相同。
- **專案專屬欄位：** 除了 `default_runner`、`runners` 和 `roles` 以外的所有欄位都是專案專屬的，不會被使用者設定覆蓋。

儀表板的專案設定編輯器顯示的是**原始**專案設定，不是合併後的結果。使用專案設定中的 **Merged** 分頁可查看合併後的最終有效設定。
