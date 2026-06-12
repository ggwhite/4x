# F029 — Auto-merge on Done

## 問題

Feature 跑完停在 `pending-review` 後，user 確認沒問題要結案時，需要手動執行多個步驟：

1. `4x done <id>` — 標記完成
2. `git merge 4x/<id>` — 合併 worktree branch
3. `git worktree remove .worktrees/4x/<id>` — 清理 worktree
4. `git branch -d 4x/<id>` — 刪除 branch

這四步是機械操作，應該由 `4x done` 一步完成。

## 目標

1. `4x done <id>` 自動 merge worktree branch 回 main 並清理
2. 有衝突時 abort 並引導 user 手動解決後用 `4x merge <id>` 完成
3. Dashboard "Mark Done" 按鈕同樣自動 merge + cleanup
4. 沒有 worktree 的 feature 行為不變（只做 state transition）

## 設計

### `4x done <id>` 增強

在現有 `markDone()` 結束後，偵測 worktree 並嘗試 merge：

```
markDone(ws, featureID)              // 已有：pending-review → done
mergeWorktree(ws.Root, featureID)    // 新增：merge + cleanup
```

`mergeWorktree` 邏輯：

1. 檢查 `.worktrees/4x/<featureID>/` 是否存在，不存在就跳過
2. `git merge --no-commit 4x/<featureID>` 嘗試 merge
3. 檢查是否有衝突（exit code）
   - 無衝突：`git commit` + `git worktree remove` + `git branch -d` → 印成功訊息
   - 有衝突：`git merge --abort` → 印衝突檔案列表 + 引導訊息

衝突時的輸出：

```
Feature <id> marked as done.
Merge conflict — resolve manually:
  conflict: path/to/file.go
  conflict: path/to/other.go
Worktree: .worktrees/4x/<id>
After resolving: 4x merge <id>
```

### `4x merge <id>` 新增

user 在 worktree 裡解完衝突後，用這個指令完成 merge + cleanup：

1. 檢查 feature phase 是 `done`（必須先跑過 `4x done`）
2. 檢查 `.worktrees/4x/<featureID>/` 存在
3. 在 worktree 裡 commit 解完衝突的結果：`git -C <worktree> add -A && git -C <worktree> commit`
4. 回 main：`git merge 4x/<featureID>`
   - 成功：`git worktree remove` + `git branch -d` → 印成功訊息
   - 仍有衝突：報錯退出

### `POST /api/done` 增強

`handlePostDone` 在完成 state transition 後，呼叫相同的 `mergeWorktree` 邏輯。

回傳格式：

- 成功（含 merge）：`200 {"status":"done","merged":true}`
- 成功（無 worktree）：`200 {"status":"done","merged":false}`
- 衝突：`200 {"status":"done","merge_conflict":true,"conflicts":["file1","file2"]}`

注意：state transition 一定成功（`done` 已標記），merge 衝突不影響 done 狀態。所以即使衝突也回 200，讓前端知道 done 成功但 merge 需要人工處理。

### Dashboard 前端

`markDone()` 成功後，檢查 response 的 `merge_conflict`：

- `merged: true`：正常 reload
- `merge_conflict: true`：alert 顯示衝突檔案列表 + 引導訊息，仍 reload（因為 done 已生效）

### 偵測 worktree

直接檢查 `.worktrees/4x/<featureID>/` 目錄是否存在。不依賴 config 或 state，簡單可靠。

worktree branch 命名固定為 `4x/<featureID>`（由 `setupWorktree` 建立時決定）。

### merge commit message

```
Merge branch '4x/<featureID>' — <feature-name>
```

Feature name 從 feature YAML 讀取。

### 共用邏輯

`mergeWorktree` 和 `cleanupWorktree` 提取為 `cmd/4x/` package 內的共用函式，`done.go`、`merge.go`、`server.go` 都呼叫同一份邏輯。

因為 `server.go` 在 `internal/server/` package，無法直接呼叫 `cmd/4x/` 的函式。解法：把 merge/cleanup 邏輯放在 server 可以 import 的地方。兩個選項：

- 放 `internal/protocol/` — 但 merge 是 git 操作，不屬於 protocol
- 放新的 `internal/worktree/` package — 語意清楚

選擇：新增 `internal/worktree/` package，提供 `Merge(root, featureID)` 和 `Cleanup(root, featureID)` 函式。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/worktree/merge.go` | 新增：Merge、Cleanup、ConflictFiles 函式 |
| `cmd/4x/done.go` | markDone 後呼叫 worktree.Merge |
| `cmd/4x/merge.go` | 新增 `4x merge` subcommand |
| `cmd/4x/main.go` | 註冊 newMergeCmd |
| `internal/server/server.go` | handlePostDone 加 merge + cleanup |
| `internal/server/static/index.html` | markDone 回應處理加 conflict 提示 |

## 不做的事

- 不做自動 rebase
- 不改 `4x run` 的 worktree 建立邏輯
- 非 worktree feature 不受影響（`4x done` 只做 state transition）
- `4x merge` 不做 state transition（必須先 `4x done`）
