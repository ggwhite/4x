# CLI 參考

所有 feature-id 參數支援不分大小寫的前綴匹配。`4x run f001`、`4x run F001-user` 和 `4x run F001` 都會解析為 `F001-user-authentication-w`。模糊的前綴會產生列出匹配項的錯誤。

---

## `4x init`

在當前目錄初始化一個 `.4x/` workspace。

```
4x init
```

- 自動偵測專案語言和 build/test/lint 命令
- 建立包含 4 個預設 runner（claude、codex、gemini、agy）的 `.4x/settings.json`
- 將內嵌的 plugin 檔部署到 `.4x/plugins/`
- 在根層級檔案（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md）加入 `@import` 行
- 如果 `.4x/` 已存在則報錯

---

## `4x new <title>`

建立一個新的 feature。

```
4x new "Feature title" [--repo <repo>...] [--json]
```

| 旗標 | 說明 |
|---|---|
| `--repo` | 範圍內的 repository（可重複用於多 repo feature） |
| `--json` | 以 JSON 格式輸出 |

建立 `.4x/features/F{NNN}-{slug}.yaml`，狀態為 `not-started`。

---

## `4x run <feature-id>`

執行一個 feature 的 Design-Code-Review-Test 迴圈。

```
4x run <feature-id> [flags]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--runner` | 設定預設值 | Runner plugin 名稱 |
| `--max-rounds` | `5` | 最大迴圈迭代次數 |
| `--timeout` | `3600` | 每階段逾時秒數 |
| `--dry-run` | `false` | 印出角色 prompt 但不呼叫 LLM |
| `--json` | `false` | 啟動執行並立即以 JSON 格式回傳 |

迴圈驅動：init → designing → coding → reviewing → testing → accepting → pending-review。Review 失敗時，code 會再跑一輪。Test 失敗時，迴圈重新進入 coding。

如果 feature 處於 `blocked` 或 `needs-attention` 階段，會根據當前角色自動恢復到適當的恢復階段。

自動檢查依賴閘門 — 如果被依賴的 feature 未完成則阻擋。

如果設定中設了 `isolation: "worktree"`，會在 `.worktrees/4x/<feature-id>/` 的 git worktree 中執行。

---

## `4x status [feature-id]`

顯示 feature 狀態。

```
4x status              # 所有 feature，按狀態分組
4x status <feature-id> # 單一 feature 詳情含子任務
4x status --pending    # 篩選 pending-review 的 feature
4x status --json       # 以 JSON 格式輸出
```

| 旗標 | 說明 |
|---|---|
| `--pending` | 篩選 pending-review 的 feature |
| `--json` | 以 JSON 格式輸出 |

分組：Running、Review、Pending、Todo、Done（done 最多顯示 5 個）。包含 backlog drift 警告。

---

## `4x check <feature-id>`

執行 guardrail 檢查但不轉換狀態。

```
4x check <feature-id> [--json]
```

| 旗標 | 說明 |
|---|---|
| `--json` | 以 JSON 格式輸出結果 |

檢查項目：必要檔案、基線、範圍、依賴、backlog drift。通過時 exit 0，失敗時 exit 1。

---

## `4x transition <feature-id>`

強制狀態轉換。

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| 旗標 | 說明 |
|---|---|
| `--to` | 目標階段（必填） |
| `--role` | 執行轉換的角色 |
| `--json` | 以 JSON 格式輸出 |

驗證轉換是否合乎狀態機規則。如果狀態不存在則自動初始化。`testing → accepting` 轉換會執行額外的閘門（verify.json、test-report.md、final-report.md、commit-plan.md 必須存在且驗證必須通過）。

---

## `4x event <feature-id>`

在 `events.jsonl` 中追加一個事件。

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| 旗標 | 說明 |
|---|---|
| `--type` | 事件類型（必填） |
| `--role` | 觸發事件的角色 |
| `--round` | 輪次編號 |
| `--action` | 動作名稱 |
| `--detail` | 額外的說明文字 |

---

## `4x prompt <feature-id>`

印出一個 feature 的角色 prompt。

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| 旗標 | 說明 |
|---|---|
| `--role` | 目標角色（省略時從當前狀態推斷） |
| `--round` | 輪次編號 |

支援語系注入（來自使用者設定或 `LANG` 環境變數）、規劃文件自動引入（`docs/design/{id}-spec.md` 和 `{id}-plan.md`）、以及專案/角色 include。

---

## `4x done <feature-id>`

將一個 pending-review 的 feature 標記為完成。

```
4x done <feature-id>
```

僅在 feature 處於 `pending-review` 階段時有效。其他階段會報錯。

---

## `4x config`

管理使用者層級的設定（`~/.4x/settings.json`）。

```
4x config list          # 顯示所有使用者設定
4x config get <key>     # 取得一個值
4x config set <key> <value>  # 設定一個值
```

目前支援的 key：`locale`。

---

## `4x upgrade`

將內嵌的 plugin 檔重新部署到既有專案。

```
4x upgrade [--dry-run]
```

| 旗標 | 說明 |
|---|---|
| `--dry-run` | 只報告差異，不寫入檔案 |

每個檔案會報告為 created、updated 或 current。

---

## `4x batch`

多個 feature 的批次操作。

### `4x batch plan`

產生依賴感知的執行計畫。

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--dry-run` | `false` | 印出排程但不寫入檔案 |
| `--max-chain` | `4` | 每個 cluster 的最大鏈長度 |

寫入 `.4x/batch-plan.json`。

### `4x batch next`

顯示下一個可執行的 feature（根據計畫和當前狀態）。

```
4x batch next
```

### `4x batch run`

按依賴順序循序執行合格的 feature。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| 旗標 | 預設值 | 說明 |
|---|---|---|
| `--runner` | 設定預設值 | Runner plugin 名稱 |
| `--max-rounds` | `5` | 每個 feature 的最大輪次 |
| `--timeout` | `3600` | 每階段逾時秒數 |

在 feature 之間檢查 `.4x/batch-stop` 檔案以實現優雅停機。

### `4x batch stop`

通知正在執行的批次在當前 feature 完成後停止。

```
4x batch stop
```

建立一個 `.4x/batch-stop` 信號檔。

---

## `4x live [path...]`

啟動 4x Live 儀表板伺服器。

```
4x live [path...] [flags]
```

| 旗標 | 短旗標 | 預設值 | 說明 |
|---|---|---|---|
| `--port` | `-p` | `4567` | 伺服器 port |
| `--web` | `-w` | `false` | 在瀏覽器中開啟 |
| `--app` | `-a` | `false` | 開啟 macOS 原生 app |

不帶路徑時，從 `~/.4x/recent-projects.json` 載入最近的專案（LRU，最多 20 個）。帶路徑時，每個路徑作為一個專案分頁開啟。
