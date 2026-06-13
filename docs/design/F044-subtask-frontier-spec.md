# F044: Subtask Dependency Frontier

## 現狀

Feature YAML 的 subtask 已有 `depends` 欄位（`[]string`，存放被依賴的 subtask ID），但 batch scheduler 完全沒有解析這個欄位，所有 subtask 視為獨立。

## 需求

在 batch 模式執行 feature 時，解析 subtask 之間的依賴關係，只執行「frontier」——所有前置 subtask 已完成的未完成 subtask。

## 設計

### 新增 `internal/batch/subtask.go`

三個 exported 函式：

```go
// BuildSubtaskGraph 解析 subtask depends 欄位，建立鄰接表
// subtask A depends B → 邊 B→A（B 完成後 A 才能跑）
// 若 depends 引用不存在的 subtask ID，回傳 error
func BuildSubtaskGraph(subtasks []protocol.Subtask) (adj map[string][]string, err error)

// DetectSubtaskCycle 用三色 DFS 偵測環形依賴
// 有環回傳環路徑（ID slice），無環回傳 nil
func DetectSubtaskCycle(subtasks []protocol.Subtask, adj map[string][]string) []string

// SubtaskFrontier 回傳所有 depends 已完成的未完成 subtask ID
// 內部先建圖、偵測環（有環 error），再過濾出 frontier
// subtask status == "done" 視為已完成
func SubtaskFrontier(subtasks []protocol.Subtask) ([]string, error)
```

鄰接表用 `map[string][]string`（以 subtask ID 為 key），而非現有 feature DAG 用的 `[][]int`。理由：subtask 數量小（通常 < 20），用 string key 比 index 更直覺，不需要先建 ID→index 映射。

### 修改 `cmd/4x/batch.go` — `batch next`

`batch next` 的 JSON 輸出新增 `subtaskFrontier` 欄位：

```json
{
  "featureId": "F022",
  "slot": 1,
  "subtaskFrontier": ["cleanup-verify", "test-coverage"]
}
```

- feature 無 subtask → `"subtaskFrontier": []`
- 所有 subtask 都 done → `"subtaskFrontier": []`
- 有環形依賴 → `batch next` 報錯並印出環路徑

### 測試 `internal/batch/subtask_test.go`

| 案例 | 輸入 | 預期 |
|---|---|---|
| 無依賴 | A, B, C 全無 depends | frontier = [A, B, C] |
| 線性依賴 | A→B→C，A done | frontier = [B] |
| 菱形依賴 | A→B, A→C, B→D, C→D，A done | frontier = [B, C] |
| 環形依賴 | A→B→C→A | error 含環路徑 |
| 未知 ID | A depends "X"（不存在）| error |
| 全部完成 | A, B 都 done | frontier = [] |
| 部分完成 | A→B, A done, B not-started | frontier = [B] |

## 約束

- 不改 feature YAML schema（`internal/protocol/types.go` 的 Subtask struct 不動）
- 不改現有 feature 層級的 DAG 排程邏輯
- subtask status 判斷完成用 `== "done"`，與現有 `cmd/4x/subtask.go` 一致
