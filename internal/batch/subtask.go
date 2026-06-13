package batch

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// BuildSubtaskGraph 解析 subtask depends 欄位，建立鄰接表（依賴方向：被依賴者 → 依賴者）。
// subtask A depends B → 邊 B→A（B 完成後 A 才能跑）。
// 若 depends 引用不存在的 subtask ID，回傳 error 並含出問題的 subtask ID 與未知的 dep ID。
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

// DetectSubtaskCycle 用三色 DFS 偵測 subtask 依賴圖中的環形依賴。
// 有環回傳環路徑（subtask ID slice，長度 ≥ 2）；無環回傳 nil。
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

// SubtaskFrontier 回傳所有前置已完成的未完成 subtask ID。
// 內部先建圖、偵測環（有環回傳 error），再過濾出 frontier。
// subtask status == "done" 視為已完成。
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
