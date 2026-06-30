package learning

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStore_NotExist_ReturnsEmpty(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "nope", "learnings.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("expected version 1, got %d", s.Version)
	}
	if len(s.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(s.Entries))
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learnings.json")
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryDesign, Content: "a", Status: StatusActive, CreatedAt: time.Now()},
	}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].ID != "L001" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestHarvest_AddsAndDeduplicates(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors"},
		{Category: CategoryTesting, Content: "test edge cases"},
		{Category: CategoryCodeQuality, Content: "always wrap errors"}, // 重複
	}
	added := s.Harvest("F042-test", "", learnings)
	if added != 2 {
		t.Errorf("expected 2 added, got %d", added)
	}
	if len(s.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.Entries))
	}
	if s.Entries[0].ID != "L001" || s.Entries[1].ID != "L002" {
		t.Errorf("unexpected IDs: %s, %s", s.Entries[0].ID, s.Entries[1].ID)
	}
	if s.Entries[0].SourceFeature != "F042-test" {
		t.Errorf("expected source_feature F042-test, got %s", s.Entries[0].SourceFeature)
	}
	if s.Entries[0].Status != StatusCandidate {
		t.Errorf("expected candidate status, got %s", s.Entries[0].Status)
	}
}

func TestHarvest_SkipsDuplicateWithExisting(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Content: "always wrap errors", Category: CategoryCodeQuality, Status: StatusActive},
	}}
	learnings := []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors"},
		{Category: CategoryTesting, Content: "new learning"},
	}
	added := s.Harvest("F043", "acceptor", learnings)
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
	if len(s.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.Entries))
	}
	if s.Entries[1].ID != "L002" {
		t.Errorf("expected L002, got %s", s.Entries[1].ID)
	}
}

func TestHarvest_SkipsInvalidCategory(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: "invalid", Content: "should be skipped"},
		{Category: CategoryDesign, Content: "valid one"},
		{Category: CategoryDesign, Content: ""}, // 空 content 也跳過
	}
	added := s.Harvest("F044", "coder", learnings)
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
}

func TestMarkStale_MarksOldEntries(t *testing.T) {
	old := time.Now().Add(-91 * 24 * time.Hour)
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, CreatedAt: old, LastUsed: old},
		{ID: "L002", Status: StatusActive, CreatedAt: time.Now()},
		{ID: "L003", Status: StatusPromoted, CreatedAt: old, LastUsed: old},
	}}
	s.MarkStale(DefaultStaleDays)
	if s.Entries[0].Status != StatusStale {
		t.Errorf("L001 should be stale, got %s", s.Entries[0].Status)
	}
	if s.Entries[1].Status != StatusActive {
		t.Errorf("L002 should still be active, got %s", s.Entries[1].Status)
	}
	if s.Entries[2].Status != StatusPromoted {
		t.Errorf("L003 (promoted) should not be changed, got %s", s.Entries[2].Status)
	}
}

func TestActiveEntries(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryDesign, Content: "a"},
		{ID: "L002", Status: StatusStale, Category: CategoryDesign, Content: "b"},
		{ID: "L003", Status: StatusActive, Category: CategoryTesting, Content: "c"},
		{ID: "L004", Status: StatusPromoted, Category: CategoryDesign, Content: "d"},
	}}
	active := s.ActiveEntries()
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
	if active[0].ID != "L001" || active[1].ID != "L003" {
		t.Errorf("unexpected active IDs: %v", active)
	}
}

func TestPromoteRemovePrune(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, CreatedAt: time.Now().Add(-91 * 24 * time.Hour)},
		{ID: "L002", Status: StatusActive, CreatedAt: time.Now()},
	}}
	if err := s.Promote("L002"); err != nil {
		t.Fatal(err)
	}
	if s.Entries[1].Status != StatusPromoted {
		t.Errorf("expected L002 promoted")
	}
	if err := s.Promote("nope"); err == nil {
		t.Error("expected error for unknown id")
	}

	s.MarkStale(DefaultStaleDays)
	removed := s.Prune()
	if removed != 1 {
		t.Errorf("expected 1 pruned, got %d", removed)
	}
	if len(s.Entries) != 1 || s.Entries[0].ID != "L002" {
		t.Errorf("expected only L002 remaining, got %+v", s.Entries)
	}

	if err := s.Remove("L002"); err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 0 {
		t.Errorf("expected empty after remove")
	}
	if err := s.Remove("L002"); err == nil {
		t.Error("expected error removing missing id")
	}
}

func TestUpdateUsage(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, UsedCount: 2},
		{ID: "L002", Status: StatusActive},
	}}
	s.UpdateUsage([]string{"L001"})
	if s.Entries[0].UsedCount != 3 {
		t.Errorf("expected used_count 3, got %d", s.Entries[0].UsedCount)
	}
	if s.Entries[0].LastUsed.IsZero() {
		t.Error("expected last_used set")
	}
	if s.Entries[1].UsedCount != 0 {
		t.Errorf("L002 should be untouched, got %d", s.Entries[1].UsedCount)
	}
}

func TestCategoriesForRole(t *testing.T) {
	containsOps := func(cats []Category) bool {
		for _, c := range cats {
			if c == CategoryOps {
				return true
			}
		}
		return false
	}

	if got := CategoriesForRole("coder"); len(got) != 4 || !containsOps(got) {
		t.Errorf("expected 4 categories including ops for coder, got %v", got)
	}
	if got := CategoriesForRole("tester"); !containsOps(got) {
		t.Errorf("expected ops in tester categories, got %v", got)
	}
	if got := CategoriesForRole("fixer"); !containsOps(got) {
		t.Errorf("expected ops in fixer categories, got %v", got)
	}
	if got := CategoriesForRole("designer"); containsOps(got) {
		t.Errorf("designer should not have ops, got %v", got)
	}
	if got := CategoriesForRole("reviewer"); containsOps(got) {
		t.Errorf("reviewer should not have ops, got %v", got)
	}
	if got := CategoriesForRole("deep-reviewer"); containsOps(got) {
		t.Errorf("deep-reviewer should not have ops, got %v", got)
	}
	if got := CategoriesForRole("design-reviewer"); containsOps(got) {
		t.Errorf("design-reviewer should not have ops, got %v", got)
	}
	if got := CategoriesForRole("acceptor"); len(got) != 1 || got[0] != CategoryProcess {
		t.Errorf("expected [process] for acceptor, got %v", got)
	}
	if got := CategoriesForRole("unknown"); got != nil {
		t.Errorf("expected nil for unknown role, got %v", got)
	}
}

func TestIsValidCategory_Ops(t *testing.T) {
	if !IsValidCategory(CategoryOps) {
		t.Error("ops should be a valid category")
	}
	if !IsValidCategory("ops") {
		t.Error("string 'ops' should be a valid category")
	}
}

func TestHarvest_OpsCategory(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: CategoryOps, Content: "在 worktree 內跑 go build 前必須設 GOWORK=off"},
	}
	added := s.Harvest("F115-test", "coder", learnings)
	if added != 1 {
		t.Errorf("expected 1 added for ops category, got %d", added)
	}
	if len(s.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(s.Entries))
	}
	if s.Entries[0].Category != CategoryOps {
		t.Errorf("expected category ops, got %s", s.Entries[0].Category)
	}
	if s.Entries[0].Status != StatusCandidate {
		t.Errorf("expected candidate status, got %s", s.Entries[0].Status)
	}
}

func TestApplyConsolidation_MergeAndRemove(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryCodeQuality, Content: "gofmt before commit", Status: StatusActive, UsedCount: 3},
		{ID: "L002", Category: CategoryCodeQuality, Content: "run gofmt -w before committing", Status: StatusActive, UsedCount: 1},
		{ID: "L003", Category: CategoryTesting, Content: "obsolete advice", Status: StatusActive, UsedCount: 0},
		{ID: "L004", Category: CategoryDesign, Content: "keep this one", Status: StatusActive, UsedCount: 5},
	}}

	actions := []ConsolidateAction{
		{ID: "L002", Action: "merge", MergeID: "L001", Content: "always run gofmt -w before commit to avoid review FAIL", Reason: "both about gofmt"},
		{ID: "L003", Action: "remove", Reason: "now enforced by CI"},
	}

	merged, removed := s.ApplyConsolidation(actions)
	if merged != 1 {
		t.Errorf("expected 1 merged, got %d", merged)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	if len(s.Entries) != 3 {
		t.Fatalf("expected 3 entries after remove, got %d", len(s.Entries))
	}
	if s.Entries[0].Content != "always run gofmt -w before commit to avoid review FAIL" {
		t.Errorf("L001 content not updated: %s", s.Entries[0].Content)
	}
	if s.Entries[0].UsedCount != 3 {
		t.Errorf("L001 used_count should stay 3, got %d", s.Entries[0].UsedCount)
	}
	if s.Entries[1].Status != StatusStale {
		t.Errorf("L002 should be stale after merge, got %s", s.Entries[1].Status)
	}

	for _, e := range s.Entries {
		if e.ID == "L003" {
			t.Error("L003 should have been removed")
		}
	}
	if s.Entries[2].ID != "L004" {
		t.Errorf("L004 should be intact, got %s", s.Entries[2].ID)
	}
}

func TestApplyConsolidation_EmptyActions(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Content: "a"},
	}}
	merged, removed := s.ApplyConsolidation(nil)
	if merged != 0 || removed != 0 {
		t.Errorf("expected 0/0, got %d/%d", merged, removed)
	}
	if len(s.Entries) != 1 {
		t.Errorf("entries should be unchanged")
	}
}

func TestApplyConsolidation_InvalidIDs(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Content: "a"},
	}}
	actions := []ConsolidateAction{
		{ID: "L999", Action: "merge", MergeID: "L001", Reason: "bad source"},
		{ID: "L998", Action: "remove", Reason: "nonexistent"},
	}
	merged, removed := s.ApplyConsolidation(actions)
	if merged != 0 || removed != 0 {
		t.Errorf("expected 0/0 for invalid IDs, got %d/%d", merged, removed)
	}
	if len(s.Entries) != 1 {
		t.Errorf("entries should be unchanged")
	}
}

func TestHarvest_SetsSourceRole(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: CategoryCodeQuality, Content: "wrap errors with context"},
	}
	s.Harvest("F050", "reviewer", learnings)
	if len(s.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(s.Entries))
	}
	if s.Entries[0].SourceRole != "reviewer" {
		t.Errorf("expected source_role=reviewer, got %s", s.Entries[0].SourceRole)
	}
	if s.Entries[0].SourceFeature != "F050" {
		t.Errorf("expected source_feature=F050, got %s", s.Entries[0].SourceFeature)
	}
}

func TestParseRoleLearningsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "role-learnings.json")
	content := `{"role":"coder","learnings":[{"category":"tooling","content":"use make lint"}]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	role, got, err := ParseRoleLearningsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if role != "coder" {
		t.Errorf("expected role=coder, got %s", role)
	}
	if len(got) != 1 || got[0].Category != CategoryTooling {
		t.Errorf("unexpected parse result: %+v", got)
	}
}

func TestHarvest_FuzzyDedup_NormalizedMatch(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F060", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "Always wrap errors with context"},
	})
	added := s.Harvest("F061", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors with context"},
	})
	if added != 0 {
		t.Errorf("normalized duplicate should be skipped, got added=%d", added)
	}
	if len(s.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(s.Entries))
	}
}

func TestHarvest_FuzzyDedup_JaccardMatch(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F060", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always run gofmt and go vet before commit"},
	})
	added := s.Harvest("F061", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "run gofmt and go vet before every commit"},
	})
	if added != 0 {
		t.Errorf("fuzzy duplicate should be skipped (high Jaccard), got added=%d", added)
	}
}

func TestHarvest_FuzzyDedup_DifferentContent(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F060", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always run gofmt before commit"},
	})
	added := s.Harvest("F061", "tester", []RetroLearning{
		{Category: CategoryTesting, Content: "test database migrations with real schema"},
	})
	if added != 1 {
		t.Errorf("distinct content should be added, got added=%d", added)
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]bool
		want float64
	}{
		{"identical", map[string]bool{"a": true, "b": true}, map[string]bool{"a": true, "b": true}, 1.0},
		{"disjoint", map[string]bool{"a": true}, map[string]bool{"b": true}, 0.0},
		{"both empty", map[string]bool{}, map[string]bool{}, 1.0},
		{"partial", map[string]bool{"a": true, "b": true, "c": true}, map[string]bool{"a": true, "b": true, "d": true}, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JaccardSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("JaccardSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindSimilar_ExactMatch(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryDesign, Content: "always check error returns", Status: StatusActive},
	}}
	got := s.FindSimilar("always check error returns")
	if got == nil || got.ID != "L001" {
		t.Errorf("expected L001, got %v", got)
	}
}

func TestFindSimilar_NormalizedMatch(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryDesign, Content: "Always Check Error Returns", Status: StatusActive},
	}}
	got := s.FindSimilar("always check error returns")
	if got == nil || got.ID != "L001" {
		t.Errorf("expected L001, got %v", got)
	}
}

func TestFindSimilar_JaccardMatch(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryCodeQuality, Content: "always run gofmt and go vet before commit", Status: StatusActive},
	}}
	got := s.FindSimilar("run gofmt and go vet before every commit")
	if got == nil || got.ID != "L001" {
		t.Errorf("expected L001, got %v", got)
	}
}

func TestFindSimilar_NoMatch(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryDesign, Content: "always check error returns", Status: StatusActive},
	}}
	got := s.FindSimilar("test database migrations with real schema")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestParseRoleLearningsFile_NotExist(t *testing.T) {
	_, _, err := ParseRoleLearningsFile(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestParseRetroFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retro-learnings.json")
	if err := os.WriteFile(path, []byte(`{"learnings":[{"category":"testing","content":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseRetroFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Category != CategoryTesting {
		t.Errorf("unexpected parse result: %+v", got)
	}
}

func TestHarvest_NewEntryIsCandidate(t *testing.T) {
	s := Store{Version: 1}
	added := s.Harvest("F117-test", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "new learning for candidate test"},
	})
	if added != 1 {
		t.Fatalf("expected 1 added, got %d", added)
	}
	if s.Entries[0].Status != StatusCandidate {
		t.Errorf("expected candidate status, got %s", s.Entries[0].Status)
	}
}

func TestHarvest_CrossFeatureFuzzyPromotes(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F100", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always run gofmt and go vet before commit"},
	})
	if s.Entries[0].Status != StatusCandidate {
		t.Fatalf("precondition: expected candidate, got %s", s.Entries[0].Status)
	}

	added := s.Harvest("F101", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "run gofmt and go vet before every commit"},
	})
	if added != 0 {
		t.Errorf("expected 0 added (fuzzy match), got %d", added)
	}
	if s.Entries[0].Status != StatusActive {
		t.Errorf("expected candidate promoted to active, got %s", s.Entries[0].Status)
	}
}

func TestHarvest_SameFeatureFuzzySkips(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F100", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always run gofmt and go vet before commit"},
	})

	added := s.Harvest("F100", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "run gofmt and go vet before every commit"},
	})
	if added != 0 {
		t.Errorf("expected 0 added (same feature fuzzy), got %d", added)
	}
	if s.Entries[0].Status != StatusCandidate {
		t.Errorf("same feature fuzzy should not promote, got %s", s.Entries[0].Status)
	}
}

func TestActiveEntries_ExcludesCandidate(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Content: "a"},
		{ID: "L002", Status: StatusCandidate, Content: "b"},
		{ID: "L003", Status: StatusActive, Content: "c"},
	}}
	active := s.ActiveEntries()
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
	for _, e := range active {
		if e.Status != StatusActive {
			t.Errorf("expected only active entries, got %s for %s", e.Status, e.ID)
		}
	}
}

func TestCandidateEntries(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Content: "a"},
		{ID: "L002", Status: StatusCandidate, Content: "b"},
		{ID: "L003", Status: StatusCandidate, Content: "c"},
		{ID: "L004", Status: StatusStale, Content: "d"},
	}}
	candidates := s.CandidateEntries()
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ID != "L002" || candidates[1].ID != "L003" {
		t.Errorf("unexpected candidate IDs: %s, %s", candidates[0].ID, candidates[1].ID)
	}
}

func TestPromoteCandidates(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusCandidate, Content: "a"},
		{ID: "L002", Status: StatusActive, Content: "b"},
		{ID: "L003", Status: StatusCandidate, Content: "c"},
	}}
	s.PromoteCandidates([]string{"L001", "L002"})
	if s.Entries[0].Status != StatusActive {
		t.Errorf("L001 should be promoted to active, got %s", s.Entries[0].Status)
	}
	if s.Entries[1].Status != StatusActive {
		t.Errorf("L002 should stay active, got %s", s.Entries[1].Status)
	}
	if s.Entries[2].Status != StatusCandidate {
		t.Errorf("L003 should remain candidate, got %s", s.Entries[2].Status)
	}
}
