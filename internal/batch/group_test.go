package batch

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestPlanBatch_SingleFeature(t *testing.T) {
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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

func TestPlanBatch_CycleDetection(t *testing.T) {
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
	features := []protocol.Feature{
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
