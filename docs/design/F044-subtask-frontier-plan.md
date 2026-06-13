# F044: Subtask Dependency Frontier — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 batch scheduler 加入 subtask 層級的 DAG 解析與 frontier 計算，讓 `4x batch next` 回傳可執行的 subtask 清單。

**Architecture:** 新增 `internal/batch/subtask.go` 放三個 exported 函式（BuildSubtaskGraph、DetectSubtaskCycle、SubtaskFrontier），風格與既有 feature DAG（group.go）一致。修改 `cmd/4x/batch.go` 的 `batch next` 輸出 JSON 加入 `subtaskFrontier` 欄位。

**Tech Stack:** Go 1.26+, 標準 testing package

---

### Task 1: SubtaskFrontier 核心邏輯 — 建圖與環偵測

**Files:**
- Create: `internal/batch/subtask.go`
- Test: `internal/batch/subtask_test.go`

- [ ] **Step 1: 寫 BuildSubtaskGraph 的 failing test**

在 `internal/batch/subtask_test.go`：

```go
package batch

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestBuildSubtaskGraph_Basic(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"a", "b"}},
	}
	adj, err := BuildSubtaskGraph(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	// a → b, a → c, b → c
	if len(adj["a"]) != 2 {
		t.Errorf("adj[a] = %v, want 2 successors", adj["a"])
	}
	if len(adj["b"]) != 1 {
		t.Errorf("adj[b] = %v, want 1 successor", adj["b"])
	}
}

func TestBuildSubtaskGraph_UnknownDep(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"unknown"}},
	}
	_, err := BuildSubtaskGraph(subtasks)
	if err == nil {
		t.Error("expected error for unknown dependency")
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/batch/ -run TestBuildSubtaskGraph -v`
Expected: FAIL — `BuildSubtaskGraph` 不存在

- [ ] **Step 3: 實作 BuildSubtaskGraph**

在 `internal/batch/subtask.go`：

```go
package batch

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// BuildSubtaskGraph 解析 subtask depends 欄位，建立鄰接表（依賴方向：被依賴者 → 依賴者）
func BuildSubtaskGraph(subtasks []protocol.Subtask) (map[string][]string, error) {
	ids := make(map[string]bool, len(subtasks))
	for _, st := range subtasks {
		ids[st.ID] = true
	}

	adj := make(map[string][]string, len(subtasks))
	for _, st := range subtasks {
		for _, dep := range st.Depends {
			if !ids[dep] {
				return nil, fmt.Errorf("subtask %q depends on unknown subtask %q", st.ID, dep)
			}
			adj[dep] = append(adj[dep], st.ID)
		}
	}
	return adj, nil
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/batch/ -run TestBuildSubtaskGraph -v`
Expected: PASS

- [ ] **Step 5: 寫 DetectSubtaskCycle 的 failing test**

在 `internal/batch/subtask_test.go` 加入：

```go
func TestDetectSubtaskCycle_NoCycle(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	adj, _ := BuildSubtaskGraph(subtasks)
	cycle := DetectSubtaskCycle(subtasks, adj)
	if cycle != nil {
		t.Errorf("expected no cycle, got %v", cycle)
	}
}

func TestDetectSubtaskCycle_WithCycle(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"c"}},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	adj, _ := BuildSubtaskGraph(subtasks)
	cycle := DetectSubtaskCycle(subtasks, adj)
	if cycle == nil {
		t.Error("expected cycle detection")
	}
	if len(cycle) < 2 {
		t.Errorf("cycle path too short: %v", cycle)
	}
}
```

- [ ] **Step 6: 執行測試確認失敗**

Run: `go test ./internal/batch/ -run TestDetectSubtaskCycle -v`
Expected: FAIL — `DetectSubtaskCycle` 不存在

- [ ] **Step 7: 實作 DetectSubtaskCycle**

在 `internal/batch/subtask.go` 加入：

```go
// DetectSubtaskCycle 用三色 DFS 偵測 subtask 依賴圖中的環形依賴
func DetectSubtaskCycle(subtasks []protocol.Subtask, adj map[string][]string) []string {
	color := make(map[string]int) // 0=white, 1=gray, 2=black
	parent := make(map[string]string)

	var cyclePath []string
	var dfs func(u string) bool
	dfs = func(u string) bool {
		color[u] = 1
		for _, v := range adj[u] {
			if color[v] == 1 {
				cyclePath = []string{v, u}
				for p := u; parent[p] != "" && parent[p] != v; p = parent[p] {
					cyclePath = append(cyclePath, parent[p])
				}
				return true
			}
			if color[v] == 0 {
				parent[v] = u
				if dfs(v) {
					return true
				}
			}
		}
		color[u] = 2
		return false
	}

	for _, st := range subtasks {
		if color[st.ID] == 0 {
			if dfs(st.ID) {
				return cyclePath
			}
		}
	}
	return nil
}
```

- [ ] **Step 8: 執行測試確認通過**

Run: `go test ./internal/batch/ -run TestDetectSubtaskCycle -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/batch/subtask.go internal/batch/subtask_test.go
git commit -m "feat(F044): add subtask DAG builder and cycle detection"
```

---

### Task 2: SubtaskFrontier 計算

**Files:**
- Modify: `internal/batch/subtask.go`
- Modify: `internal/batch/subtask_test.go`

- [ ] **Step 1: 寫 SubtaskFrontier 的 failing tests**

在 `internal/batch/subtask_test.go` 加入：

```go
func TestSubtaskFrontier_NoDeps(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started"},
		{ID: "c", Status: "not-started"},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 3 {
		t.Errorf("frontier = %v, want [a b c]", frontier)
	}
}

func TestSubtaskFrontier_Linear(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 1 || frontier[0] != "b" {
		t.Errorf("frontier = %v, want [b]", frontier)
	}
}

func TestSubtaskFrontier_Diamond(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"a"}},
		{ID: "d", Status: "not-started", Depends: []string{"b", "c"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 2 {
		t.Errorf("frontier = %v, want [b c]", frontier)
	}
	got := make(map[string]bool)
	for _, id := range frontier {
		got[id] = true
	}
	if !got["b"] || !got["c"] {
		t.Errorf("frontier = %v, want b and c", frontier)
	}
}

func TestSubtaskFrontier_AllDone(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "done", Depends: []string{"a"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 0 {
		t.Errorf("frontier = %v, want []", frontier)
	}
}

func TestSubtaskFrontier_CycleError(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"b"}},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
	}
	_, err := SubtaskFrontier(subtasks)
	if err == nil {
		t.Error("expected cycle error")
	}
}

func TestSubtaskFrontier_UnknownDepError(t *testing.T) {
	subtasks := []protocol.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"ghost"}},
	}
	_, err := SubtaskFrontier(subtasks)
	if err == nil {
		t.Error("expected unknown dep error")
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/batch/ -run TestSubtaskFrontier -v`
Expected: FAIL — `SubtaskFrontier` 不存在

- [ ] **Step 3: 實作 SubtaskFrontier**

在 `internal/batch/subtask.go` 加入：

```go
// SubtaskFrontier 回傳所有前置已完成的未完成 subtask ID
func SubtaskFrontier(subtasks []protocol.Subtask) ([]string, error) {
	adj, err := BuildSubtaskGraph(subtasks)
	if err != nil {
		return nil, err
	}

	if cycle := DetectSubtaskCycle(subtasks, adj); cycle != nil {
		return nil, fmt.Errorf("circular dependency detected: %v", cycle)
	}

	doneSet := make(map[string]bool, len(subtasks))
	for _, st := range subtasks {
		if st.Status == "done" {
			doneSet[st.ID] = true
		}
	}

	var frontier []string
	for _, st := range subtasks {
		if doneSet[st.ID] {
			continue
		}
		allDepsDone := true
		for _, dep := range st.Depends {
			if !doneSet[dep] {
				allDepsDone = false
				break
			}
		}
		if allDepsDone {
			frontier = append(frontier, st.ID)
		}
	}
	return frontier, nil
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/batch/ -run TestSubtaskFrontier -v`
Expected: PASS

- [ ] **Step 5: 跑全部 batch 測試確認沒破壞**

Run: `go test ./internal/batch/ -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/batch/subtask.go internal/batch/subtask_test.go
git commit -m "feat(F044): add SubtaskFrontier — compute ready subtasks from DAG"
```

---

### Task 3: 整合 `batch next` 輸出 subtaskFrontier

**Files:**
- Modify: `cmd/4x/batch.go:125-180`

- [ ] **Step 1: 修改 `batch next` — 載入 feature 並計算 frontier**

在 `cmd/4x/batch.go` 的 `newBatchNextCmd` 函式中，找到印出 feature ID 的位置（目前是 `fmt.Println(s.FeatureID)`），改為輸出 JSON：

```go
func newBatchNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Show the next eligible feature to run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
			data, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("no batch-plan.json found, run '4x batch plan' first")
			}

			var plan batch.BatchPlan
			if err := json.Unmarshal(data, &plan); err != nil {
				return fmt.Errorf("invalid batch-plan.json: %w", err)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}
			statusMap := make(map[string]protocol.Status)
			featureMap := make(map[string]protocol.Feature)
			for _, f := range features {
				statusMap[f.ID] = f.Status
				featureMap[f.ID] = f
			}

			for _, s := range plan.Schedule {
				if batchCompleted(statusMap[s.FeatureID]) {
					continue
				}
				allDone := true
				for _, dep := range s.CanStartAfter {
					if !batchCompleted(statusMap[dep]) {
						allDone = false
						break
					}
				}
				if allDone {
					result := struct {
						FeatureID        string   `json:"featureId"`
						Slot             int      `json:"slot"`
						SubtaskFrontier  []string `json:"subtaskFrontier"`
					}{
						FeatureID: s.FeatureID,
						Slot:      s.Slot,
					}

					if f, ok := featureMap[s.FeatureID]; ok && len(f.Subtasks) > 0 {
						frontier, err := batch.SubtaskFrontier(f.Subtasks)
						if err != nil {
							return fmt.Errorf("feature %s subtask dependency error: %w", s.FeatureID, err)
						}
						result.SubtaskFrontier = frontier
					}
					if result.SubtaskFrontier == nil {
						result.SubtaskFrontier = []string{}
					}

					out, err := json.MarshalIndent(result, "", "  ")
					if err != nil {
						return err
					}
					fmt.Println(string(out))
					return nil
				}
			}

			fmt.Println("No eligible features (all done or blocked by dependencies).")
			return nil
		},
	}
}
```

- [ ] **Step 2: 編譯確認沒有語法錯誤**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 3: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/batch.go
git commit -m "feat(F044): batch next outputs subtaskFrontier JSON"
```

---

### Task 4: 文件更新與收尾

**Files:**
- Modify: `.4x/features/F044-subtask-frontier.yaml`

- [ ] **Step 1: 跑 doc sync 檢查**

Run: `make check-docs-sync`

如果輸出 `NEEDS_UPDATE`，更新被點名的檔案。

- [ ] **Step 2: 更新 feature 狀態**

```bash
4x transition F044-subtask-frontier --to coding
```

- [ ] **Step 3: 最終驗證**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS
