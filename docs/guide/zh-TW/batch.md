# 批次模式

以依賴感知的順序執行多個 feature。

## 工作流程

```bash
# 1. 產生執行計畫
4x batch plan

# 2. 查看下一個要執行的
4x batch next

# 3. 執行所有合格的 feature
4x batch run --runner claude

# 4. 優雅停止（完成當前 feature 後停）
4x batch stop
```

## 規劃

`4x batch plan` 分析所有未完成的 feature 並產生 `.4x/batch-plan.json`：

1. **依賴 DAG** — 從 feature 的 `depends` 欄位建立有向圖
2. **環偵測** — 如果存在循環依賴則報錯
3. **Union-Find 分群** — 將共用 repository 的 feature 分到同一群
4. **拓撲排序** — 在每個 cluster 內排序 feature
5. **鏈排程** — 拆分過長的依賴鏈（最大長度可用 `--max-chain` 設定）

```bash
# 預覽計畫
4x batch plan --dry-run

# 限制鏈長度
4x batch plan --max-chain 3
```

範例輸出：

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## 執行

`4x batch run` 按依賴順序循序執行 feature：

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- 使用 commit 策略 `"never"`（你在 review 後手動 commit）
- 在 feature 之間檢查 `.4x/batch-stop` 檔案
- 跳過依賴未完成的 feature
- 每個 feature 完成後報告進度

## 停止

```bash
4x batch stop
```

建立一個 `.4x/batch-stop` 信號檔。批次會完成當前 feature，然後優雅結束。

## Merge Conflict

當自動 merge 發生 conflict 時，批次會暫停並寫入 `.4x/batch-conflict.json`，記錄 feature、衝突的 repo（多 repo 模式）和受影響的檔案。worktree 會保留以便解決 conflict。此信號檔讓[儀表板](dashboard.md)能呈現 conflict 並提供**繼續批次**動作——底層會清除信號檔並重啟 `4x batch run`。從 CLI，解決檔案衝突後執行 `4x merge <id>`，然後重跑 `4x batch run` 繼續。Conflict 檔在每次批次執行開始時自動清除。

## 執行報告

每次批次執行結束時（無論正常完成、停止、中斷或 crash）都會寫入 `.4x/batch-report.json`。報告記錄整體統計（total / completed / failed / remaining）、runner、總耗時，以及每個 feature 的名稱、最終狀態、耗時、輪次數和停止原因。

`outcome` 欄位記錄執行結束的方式：

- `completed` — 所有 feature 都完成了
- `stopped` — 你按下停止（`.4x/batch-stop`）或自動 merge conflict 暫停了執行
- `interrupted` — 批次程序收到 `SIGTERM`/`SIGINT`；報告記錄當時正在執行的 feature
- `crashed` — 批次程序 panic；報告為盡力而為並包含 `panicMessage`

[儀表板](dashboard.md)在無批次執行時讀取此檔案，顯示「最近批次報告」摘要卡片，可展開查看每 feature 詳情。報告僅在執行停止後寫入，不在每 feature 的執行迴圈內寫入，因此不會增加批次吞吐量的額外負擔。

## 檢查進度

```bash
# 查看下一個 feature
4x batch next

# 所有 feature 的概覽
4x status
```
