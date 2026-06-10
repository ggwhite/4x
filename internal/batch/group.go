package batch

import "github.com/ggwhite/4x/internal/protocol"

// Group 是分群後的一組 feature（共用 worktree，鏈式接力）
type Group struct {
	ID       string
	Features []protocol.Feature
	IsBridge bool
}

// Plan 是 batch 的完整執行計畫
type Plan struct {
	Bridges []Group `json:"bridges"`
	Groups  []Group `json:"groups"`
}

// PlanBatch 對待跑的 features 做 Union-Find 分群
func PlanBatch(features []protocol.Feature, hubRepos []string) Plan {
	hubSet := make(map[string]bool)
	for _, r := range hubRepos {
		hubSet[r] = true
	}

	leafRepos := func(f protocol.Feature) map[string]bool {
		m := make(map[string]bool)
		for r := range f.Repos {
			if !hubSet[r] {
				m[r] = true
			}
		}
		return m
	}

	// Union-Find
	parent := make(map[int]int)
	for i := range features {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for i := 0; i < len(features); i++ {
		for j := i + 1; j < len(features); j++ {
			li, lj := leafRepos(features[i]), leafRepos(features[j])
			for r := range li {
				if lj[r] {
					union(i, j)
					break
				}
			}
		}
	}

	// 依 root 分群
	groups := make(map[int][]int)
	for i := range features {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	// 偵測 bridge：移除某個 feature 會讓群數增加
	bridges := detectBridges(features, hubSet, parent, find)
	bridgeSet := make(map[int]bool)
	for _, idx := range bridges {
		bridgeSet[idx] = true
	}

	var plan Plan
	groupID := 0

	// Bridge wave
	for _, idx := range bridges {
		plan.Bridges = append(plan.Bridges, Group{
			ID:       string(rune('A' + groupID)),
			Features: []protocol.Feature{features[idx]},
			IsBridge: true,
		})
		groupID++
	}

	// Normal groups（排除 bridge）
	for _, indices := range groups {
		var filtered []protocol.Feature
		for _, idx := range indices {
			if !bridgeSet[idx] {
				filtered = append(filtered, features[idx])
			}
		}
		if len(filtered) == 0 {
			continue
		}
		plan.Groups = append(plan.Groups, Group{
			ID:       string(rune('A' + groupID)),
			Features: filtered,
		})
		groupID++
	}

	return plan
}

// detectBridges 用移除法偵測橋接 feature
func detectBridges(features []protocol.Feature, hubSet map[string]bool, parent map[int]int, find func(int) int) []int {
	if len(features) <= 2 {
		return nil
	}

	leafRepos := func(f protocol.Feature) map[string]bool {
		m := make(map[string]bool)
		for r := range f.Repos {
			if !hubSet[r] {
				m[r] = true
			}
		}
		return m
	}

	originalGroupCount := countGroups(len(features), find)

	var bridges []int
	for skip := range features {
		// 重做 Union-Find，跳過 skip
		p := make(map[int]int)
		for i := range features {
			p[i] = i
		}
		var f2 func(int) int
		f2 = func(i int) int {
			if p[i] != i {
				p[i] = f2(p[i])
			}
			return p[i]
		}
		union2 := func(a, b int) {
			ra, rb := f2(a), f2(b)
			if ra != rb {
				p[ra] = rb
			}
		}
		for i := 0; i < len(features); i++ {
			if i == skip {
				continue
			}
			for j := i + 1; j < len(features); j++ {
				if j == skip {
					continue
				}
				li, lj := leafRepos(features[i]), leafRepos(features[j])
				for r := range li {
					if lj[r] {
						union2(i, j)
						break
					}
				}
			}
		}

		newCount := countGroupsExcluding(len(features), f2, skip)
		if newCount > originalGroupCount {
			bridges = append(bridges, skip)
		}
	}
	return bridges
}

func countGroups(n int, find func(int) int) int {
	roots := make(map[int]bool)
	for i := 0; i < n; i++ {
		roots[find(i)] = true
	}
	return len(roots)
}

func countGroupsExcluding(n int, find func(int) int, skip int) int {
	roots := make(map[int]bool)
	for i := 0; i < n; i++ {
		if i == skip {
			continue
		}
		roots[find(i)] = true
	}
	return len(roots)
}
