package batch

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ggwhite/4x/internal/feature"
)

// ScheduleEntry 是 batch-plan.json 裡每個 feature 的排程
type ScheduleEntry struct {
	FeatureID     string   `json:"featureId"`
	Slot          int      `json:"slot"`
	CanStartAfter []string `json:"canStartAfter"`
}

// Cluster 是有依賴或共用 repo 的 feature 群
type Cluster struct {
	ID       string     `json:"id"`
	Features []string   `json:"features"`
	Chains   [][]string `json:"chains"`
}

// BatchPlan 是 batch 的完整執行計畫
type BatchPlan struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Clusters    []Cluster       `json:"clusters"`
	Schedule    []ScheduleEntry `json:"schedule"`
}

// PlanBatch 從 features 產生完整的 batch 執行計畫
func PlanBatch(features []feature.Feature, hubRepos []string, maxChainLength int) (*BatchPlan, error) {
	if maxChainLength <= 0 {
		maxChainLength = 4
	}

	idx := make(map[string]int)
	for i, f := range features {
		idx[f.ID] = i
	}

	adj := buildDependencyGraph(features, idx)

	if cycle := detectCycle(features, adj); cycle != nil {
		return nil, fmt.Errorf("dependency cycle detected: %v", cycle)
	}

	uf := newUnionFind(len(features))
	mergeByDependency(features, idx, uf)
	mergeBySharedRepos(features, hubRepos, uf)

	clusters := buildClusters(features, uf, adj, idx, maxChainLength)
	schedule := buildSchedule(features, clusters, idx)

	return &BatchPlan{
		GeneratedAt: time.Now().UTC(),
		Clusters:    clusters,
		Schedule:    schedule,
	}, nil
}

func buildDependencyGraph(features []feature.Feature, idx map[string]int) [][]int {
	adj := make([][]int, len(features))
	for i, f := range features {
		for _, depID := range f.Depends {
			if j, ok := idx[depID]; ok {
				adj[j] = append(adj[j], i)
			}
		}
	}
	return adj
}

func detectCycle(features []feature.Feature, adj [][]int) []string {
	n := len(features)
	color := make([]int, n) // 0=white, 1=gray, 2=black
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
	}

	var cyclePath []string
	var dfs func(u int) bool
	dfs = func(u int) bool {
		color[u] = 1
		for _, v := range adj[u] {
			if color[v] == 1 {
				path := []string{features[v].ID}
				for p := u; p != v; p = parent[p] {
					path = append(path, features[p].ID)
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

	for i := 0; i < n; i++ {
		if color[i] == 0 {
			if dfs(i) {
				return cyclePath
			}
		}
	}
	return nil
}

// topoSort 回傳 cluster 內的拓撲排序（Kahn's algorithm）。
//
// depends 仍是硬約束（拓撲序不可違反）；當多個節點同時 ready（in-degree 0）時，
// 依 Feature.Priority 決定出隊順序——數字小者優先，nil priority 視為最低優先（最大值），
// priority 相同再依 feature ID 穩定排序，避免 map 迭代造成的非決定性。
func topoSort(features []feature.Feature, indices []int, adj [][]int) []int {
	inCluster := make(map[int]bool)
	for _, i := range indices {
		inCluster[i] = true
	}

	inDegree := make(map[int]int)
	for _, i := range indices {
		inDegree[i] = 0
	}
	for _, u := range indices {
		for _, v := range adj[u] {
			if inCluster[v] {
				inDegree[v]++
			}
		}
	}

	var ready []int
	for _, i := range indices {
		if inDegree[i] == 0 {
			ready = append(ready, i)
		}
	}

	var sorted []int
	for len(ready) > 0 {
		// 在所有 ready 節點中挑選 priority 最高（數字最小）者出隊。
		sort.Slice(ready, func(i, j int) bool {
			return featureLess(features[ready[i]], features[ready[j]])
		})
		u := ready[0]
		ready = ready[1:]
		sorted = append(sorted, u)
		for _, v := range adj[u] {
			if !inCluster[v] {
				continue
			}
			inDegree[v]--
			if inDegree[v] == 0 {
				ready = append(ready, v)
			}
		}
	}
	return sorted
}

// featureLess 定義同時 ready 的 feature 出隊優先序：priority 小者優先（nil 視為最大），
// priority 相同則依 feature ID 穩定排序。
func featureLess(a, b feature.Feature) bool {
	pa, pb := priorityValue(a.Priority), priorityValue(b.Priority)
	if pa != pb {
		return pa < pb
	}
	return a.ID < b.ID
}

// priorityValue 將 *int priority 轉為可比較值，nil 視為最低優先（math.MaxInt）。
func priorityValue(p *int) int {
	if p == nil {
		return math.MaxInt
	}
	return *p
}

// clusterSortKey 回傳 cluster 的跨群排序鍵：成員中最高優先（最小 priorityValue）與最小 feature ID。
func clusterSortKey(features []feature.Feature, members []int) (int, string) {
	minPrio := math.MaxInt
	minID := ""
	for _, m := range members {
		if p := priorityValue(features[m].Priority); p < minPrio {
			minPrio = p
		}
		if minID == "" || features[m].ID < minID {
			minID = features[m].ID
		}
	}
	return minPrio, minID
}

func buildClusters(features []feature.Feature, uf *unionFind, adj [][]int, idx map[string]int, maxChainLen int) []Cluster {
	groups := make(map[int][]int)
	for i := range features {
		root := uf.find(i)
		groups[root] = append(groups[root], i)
	}

	roots := make([]int, 0, len(groups))
	for r := range groups {
		roots = append(roots, r)
	}
	// 跨 cluster 的排程順序也依 priority：以 cluster 內最高優先（priorityValue 最小）的 feature
	// 代表整個 cluster，相同則以 cluster 內最小 feature ID 穩定排序。這讓彼此獨立（各自成 cluster）
	// 的 feature 也能由 priority 決定先後，而非沿用 union-find root 的插入順序。
	sort.Slice(roots, func(i, j int) bool {
		pi, idi := clusterSortKey(features, groups[roots[i]])
		pj, idj := clusterSortKey(features, groups[roots[j]])
		if pi != pj {
			return pi < pj
		}
		return idi < idj
	})

	var clusters []Cluster
	for ci, root := range roots {
		indices := groups[root]
		sorted := topoSort(features, indices, adj)

		chains := splitChains(sorted, adj, maxChainLen)

		featureIDs := make([]string, len(sorted))
		for i, si := range sorted {
			featureIDs[i] = features[si].ID
		}

		chainIDs := make([][]string, len(chains))
		for i, chain := range chains {
			chainIDs[i] = make([]string, len(chain))
			for j, si := range chain {
				chainIDs[i][j] = features[si].ID
			}
		}

		clusters = append(clusters, Cluster{
			ID:       fmt.Sprintf("cluster-%d", ci),
			Features: featureIDs,
			Chains:   chainIDs,
		})
	}
	return clusters
}

// splitChains 把排序後的 indices 切成 maxLen 長的 chain
func splitChains(sorted []int, adj [][]int, maxLen int) [][]int {
	if len(sorted) == 0 {
		return nil
	}

	assigned := make(map[int]bool)
	var chains [][]int

	childOf := make(map[int][]int)
	parentOf := make(map[int]int)
	inSet := make(map[int]bool)
	for _, i := range sorted {
		inSet[i] = true
	}
	for _, u := range sorted {
		for _, v := range adj[u] {
			if inSet[v] {
				childOf[u] = append(childOf[u], v)
				parentOf[v] = u
			}
		}
	}

	for _, start := range sorted {
		if assigned[start] {
			continue
		}
		chain := []int{start}
		assigned[start] = true
		cur := start
		for len(chain) < maxLen {
			found := false
			for _, next := range childOf[cur] {
				if !assigned[next] {
					chain = append(chain, next)
					assigned[next] = true
					cur = next
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
		chains = append(chains, chain)
	}
	return chains
}

func buildSchedule(features []feature.Feature, clusters []Cluster, idx map[string]int) []ScheduleEntry {
	var schedule []ScheduleEntry
	for slot, cluster := range clusters {
		for _, chain := range cluster.Chains {
			for i, fID := range chain {
				var after []string
				if i > 0 {
					after = append(after, chain[i-1])
				}
				fi := idx[fID]
				for _, depID := range features[fi].Depends {
					alreadyAdded := false
					for _, a := range after {
						if a == depID {
							alreadyAdded = true
							break
						}
					}
					if !alreadyAdded {
						if _, ok := idx[depID]; ok {
							after = append(after, depID)
						}
					}
				}
				if after == nil {
					after = []string{}
				}
				schedule = append(schedule, ScheduleEntry{
					FeatureID:     fID,
					Slot:          slot,
					CanStartAfter: after,
				})
			}
		}
	}
	return schedule
}

// mergeByDependency 把有依賴的 features 合入同 cluster
func mergeByDependency(features []feature.Feature, idx map[string]int, uf *unionFind) {
	for i, f := range features {
		for _, depID := range f.Depends {
			if j, ok := idx[depID]; ok {
				uf.union(i, j)
			}
		}
	}
}

// mergeBySharedRepos 把共用 non-hub repo 的 features 合入同 cluster
func mergeBySharedRepos(features []feature.Feature, hubRepos []string, uf *unionFind) {
	hubSet := make(map[string]bool)
	for _, r := range hubRepos {
		hubSet[r] = true
	}
	for i := 0; i < len(features); i++ {
		for j := i + 1; j < len(features); j++ {
			for _, r := range features[i].Repos {
				if hubSet[r] {
					continue
				}
				for _, r2 := range features[j].Repos {
					if r == r2 {
						uf.union(i, j)
						goto nextPair
					}
				}
			}
		nextPair:
		}
	}
}

type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (uf *unionFind) find(i int) int {
	if uf.parent[i] != i {
		uf.parent[i] = uf.find(uf.parent[i])
	}
	return uf.parent[i]
}

func (uf *unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}
