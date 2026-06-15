package batch

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func TestPlanBatch_SingleFeature(t *testing.T) {
	features := []feature.Feature{
		{ID: "feat-a", Name: "A"},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(plan.Clusters))
	}
	if len(plan.Schedule) != 1 {
		t.Fatalf("schedule = %d, want 1", len(plan.Schedule))
	}
	if plan.Schedule[0].FeatureID != "feat-a" {
		t.Errorf("schedule[0] = %s, want feat-a", plan.Schedule[0].FeatureID)
	}
}

func TestPlanBatch_IndependentFeatures(t *testing.T) {
	features := []feature.Feature{
		{ID: "a", Repos: []string{"repo-1"}},
		{ID: "b", Repos: []string{"repo-2"}},
		{ID: "c", Repos: []string{"repo-3"}},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Clusters) != 3 {
		t.Errorf("clusters = %d, want 3 (all independent)", len(plan.Clusters))
	}
}

func TestPlanBatch_SharedRepoMergesClusters(t *testing.T) {
	features := []feature.Feature{
		{ID: "a", Repos: []string{"shared"}},
		{ID: "b", Repos: []string{"shared"}},
		{ID: "c", Repos: []string{"other"}},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Clusters) != 2 {
		t.Errorf("clusters = %d, want 2 (a+b merged, c separate)", len(plan.Clusters))
	}
}

func TestPlanBatch_HubRepoNotMerged(t *testing.T) {
	features := []feature.Feature{
		{ID: "a", Repos: []string{"hub-repo"}},
		{ID: "b", Repos: []string{"hub-repo"}},
	}
	plan, err := PlanBatch(features, []string{"hub-repo"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Clusters) != 2 {
		t.Errorf("clusters = %d, want 2 (hub repo should not merge)", len(plan.Clusters))
	}
}

func TestPlanBatch_DependencyMergesClusters(t *testing.T) {
	features := []feature.Feature{
		{ID: "auth", Repos: []string{"repo-1"}},
		{ID: "api", Repos: []string{"repo-2"}, Depends: []string{"auth"}},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1 (dependency merges)", len(plan.Clusters))
	}
	if len(plan.Clusters[0].Chains) != 1 {
		t.Fatalf("chains = %d, want 1", len(plan.Clusters[0].Chains))
	}
	chain := plan.Clusters[0].Chains[0]
	if len(chain) != 2 || chain[0] != "auth" || chain[1] != "api" {
		t.Errorf("chain = %v, want [auth api]", chain)
	}
}

func TestPlanBatch_DependencyOrder(t *testing.T) {
	features := []feature.Feature{
		{ID: "auth"},
		{ID: "api", Depends: []string{"auth"}},
		{ID: "ui", Depends: []string{"api"}},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Schedule) != 3 {
		t.Fatalf("schedule = %d, want 3", len(plan.Schedule))
	}

	posOf := make(map[string]int)
	for i, s := range plan.Schedule {
		posOf[s.FeatureID] = i
	}
	if posOf["auth"] >= posOf["api"] {
		t.Error("auth should come before api")
	}
	if posOf["api"] >= posOf["ui"] {
		t.Error("api should come before ui")
	}
}

func intPtr(n int) *int { return &n }

// AC-4：兩個無依賴 feature priority 不同時，schedule 順序由 priority 決定（小者先）。
func TestPlanBatch_PriorityOrdersIndependentFeatures(t *testing.T) {
	features := []feature.Feature{
		{ID: "low-prio", Priority: intPtr(3)},
		{ID: "high-prio", Priority: intPtr(1)},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schedule[0].FeatureID != "high-prio" {
		t.Errorf("schedule[0] = %s, want high-prio (priority=1 first)", plan.Schedule[0].FeatureID)
	}
}

// AC-5：priority 相同（或皆 nil）時，順序穩定（依 feature ID），多次跑結果一致。
func TestPlanBatch_StableOrderWhenPriorityEqual(t *testing.T) {
	features := []feature.Feature{
		{ID: "zeta"},
		{ID: "alpha"},
		{ID: "mid", Priority: intPtr(2)},
		{ID: "kilo", Priority: intPtr(2)},
	}
	var first []string
	for run := 0; run < 5; run++ {
		plan, err := PlanBatch(features, nil, 4)
		if err != nil {
			t.Fatal(err)
		}
		order := make([]string, len(plan.Schedule))
		for i, s := range plan.Schedule {
			order[i] = s.FeatureID
		}
		if run == 0 {
			first = order
			continue
		}
		for i := range order {
			if order[i] != first[i] {
				t.Fatalf("non-deterministic order: run %d got %v, want %v", run, order, first)
			}
		}
	}
	// priority=2 的 kilo/mid 應排在 nil priority 的 alpha/zeta 之前，且同 priority 依 ID。
	pos := map[string]int{}
	for i, id := range first {
		pos[id] = i
	}
	if pos["mid"] >= pos["alpha"] || pos["kilo"] >= pos["zeta"] {
		t.Errorf("priority=2 features should precede nil-priority ones: %v", first)
	}
	if pos["kilo"] >= pos["mid"] {
		t.Errorf("equal priority should tie-break by ID (kilo before mid): %v", first)
	}
}

// AC-6：depends 為硬約束——即使依賴者 priority 較高，仍不得排在被依賴者之前。
func TestPlanBatch_PriorityNeverViolatesDependency(t *testing.T) {
	features := []feature.Feature{
		{ID: "a-depends-b", Priority: intPtr(0), Depends: []string{"b-base"}},
		{ID: "b-base", Priority: intPtr(9)},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, s := range plan.Schedule {
		pos[s.FeatureID] = i
	}
	if pos["b-base"] >= pos["a-depends-b"] {
		t.Errorf("b-base must precede a-depends-b despite lower priority: %v", plan.Schedule)
	}
}

func TestPlanBatch_CycleDetection(t *testing.T) {
	features := []feature.Feature{
		{ID: "a", Depends: []string{"c"}},
		{ID: "b", Depends: []string{"a"}},
		{ID: "c", Depends: []string{"b"}},
	}
	_, err := PlanBatch(features, nil, 4)
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestPlanBatch_MaxChainLength(t *testing.T) {
	features := []feature.Feature{
		{ID: "a"},
		{ID: "b", Depends: []string{"a"}},
		{ID: "c", Depends: []string{"b"}},
		{ID: "d", Depends: []string{"c"}},
		{ID: "e", Depends: []string{"d"}},
	}
	plan, err := PlanBatch(features, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Clusters {
		for _, chain := range c.Chains {
			if len(chain) > 2 {
				t.Errorf("chain length %d exceeds max 2: %v", len(chain), chain)
			}
		}
	}
}

func TestPlanBatch_ScheduleCanStartAfter(t *testing.T) {
	features := []feature.Feature{
		{ID: "base"},
		{ID: "dep", Depends: []string{"base"}},
	}
	plan, err := PlanBatch(features, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Schedule {
		if s.FeatureID == "base" && len(s.CanStartAfter) != 0 {
			t.Errorf("base should have no dependencies, got %v", s.CanStartAfter)
		}
		if s.FeatureID == "dep" {
			found := false
			for _, d := range s.CanStartAfter {
				if d == "base" {
					found = true
				}
			}
			if !found {
				t.Errorf("dep should depend on base, got %v", s.CanStartAfter)
			}
		}
	}
}

func TestUnionFind(t *testing.T) {
	uf := newUnionFind(5)
	uf.union(0, 1)
	uf.union(2, 3)

	if uf.find(0) != uf.find(1) {
		t.Error("0 and 1 should be in same set")
	}
	if uf.find(2) != uf.find(3) {
		t.Error("2 and 3 should be in same set")
	}
	if uf.find(0) == uf.find(2) {
		t.Error("0 and 2 should be in different sets")
	}
	if uf.find(0) == uf.find(4) {
		t.Error("0 and 4 should be in different sets")
	}

	uf.union(1, 3)
	if uf.find(0) != uf.find(2) {
		t.Error("after union(1,3), 0 and 2 should be in same set")
	}
}
