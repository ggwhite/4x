package batch

import (
	"fmt"

	"github.com/ggwhite/4x/internal/feature"
)

func subtaskCompleted(status string) bool {
	return status == string(feature.StatusDone) || status == string(feature.StatusReadyForReview)
}

// BuildSubtaskGraph 解析 subtask depends 欄位，建立鄰接表（依賴方向：被依賴者 → 依賴者）。
// subtask A depends B → 邊 B→A（B 完成後 A 才能跑）。
// 若有重複 subtask ID 或 depends 引用不存在的 ID，回傳 error。
func BuildSubtaskGraph(subtasks []feature.Subtask) (map[string][]string, error) {
	ids := make(map[string]bool, len(subtasks))
	for _, st := range subtasks {
		if ids[st.ID] {
			return nil, fmt.Errorf("duplicate subtask ID %q", st.ID)
		}
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
// 有環回傳環路徑（subtask ID slice，依實際依賴方向排列）；無環回傳 nil。
func DetectSubtaskCycle(subtasks []feature.Subtask, adj map[string][]string) []string {
	color := make(map[string]int) // 0=white, 1=gray, 2=black
	parent := make(map[string]string)

	var cyclePath []string
	var dfs func(u string) bool
	dfs = func(u string) bool {
		color[u] = 1
		for _, v := range adj[u] {
			if color[v] == 1 {
				path := []string{v}
				for p := u; p != v; p = parent[p] {
					path = append(path, p)
				}
				for i, j := 1, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				cyclePath = path
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
func SubtaskFrontier(subtasks []feature.Subtask) ([]string, error) {
	adj, err := BuildSubtaskGraph(subtasks)
	if err != nil {
		return nil, err
	}

	if cycle := DetectSubtaskCycle(subtasks, adj); cycle != nil {
		return nil, fmt.Errorf("circular dependency detected: %v", cycle)
	}

	doneSet := make(map[string]bool, len(subtasks))
	for _, st := range subtasks {
		if subtaskCompleted(st.Status) {
			doneSet[st.ID] = true
		}
	}

	frontier := []string{}
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
