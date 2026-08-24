package learning

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadStore_NotExist_ReturnsEmpty(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "nope", "learnings.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Version != StoreVersion {
		t.Errorf("expected version %d, got %d", StoreVersion, s.Version)
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
	added, _ := s.Harvest("F042-test", "", learnings)
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
	added, _ := s.Harvest("F043", "acceptor", learnings)
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
	added, _ := s.Harvest("F044", "coder", learnings)
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
}

func TestMarkCandidatesStale(t *testing.T) {
	old := time.Now().Add(-40 * 24 * time.Hour)
	newStore := func() Store {
		return Store{Version: 1, Entries: []Entry{
			{ID: "C001", Status: StatusCandidate, UsedCount: 0, CreatedAt: old},        // 老、未用 → stale
			{ID: "C002", Status: StatusCandidate, UsedCount: 2, CreatedAt: old},        // 老、有用 → 不動
			{ID: "A001", Status: StatusActive, UsedCount: 0, CreatedAt: old},           // active → 不動
			{ID: "P001", Status: StatusPromoted, UsedCount: 0, CreatedAt: old},         // promoted → 不動
			{ID: "C003", Status: StatusCandidate, UsedCount: 0, CreatedAt: time.Now()}, // 剛建立 → 不動
		}}
	}

	t.Run("老且未用的 candidate 被標 stale，其餘不動", func(t *testing.T) {
		s := newStore()
		marked := s.MarkCandidatesStale(30)
		if marked != 1 {
			t.Errorf("marked = %d, want 1", marked)
		}
		want := map[string]Status{
			"C001": StatusStale,
			"C002": StatusCandidate,
			"A001": StatusActive,
			"P001": StatusPromoted,
			"C003": StatusCandidate,
		}
		for _, e := range s.Entries {
			if e.Status != want[e.ID] {
				t.Errorf("%s status = %s, want %s", e.ID, e.Status, want[e.ID])
			}
		}
	})

	t.Run("maxIdleDays=0 為停用，回傳 0 且不動任何條目", func(t *testing.T) {
		s := newStore()
		marked := s.MarkCandidatesStale(0)
		if marked != 0 {
			t.Errorf("marked = %d, want 0", marked)
		}
		if s.Entries[0].Status != StatusCandidate {
			t.Errorf("C001 should stay candidate when disabled, got %s", s.Entries[0].Status)
		}
	})
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
		{ID: "L001", Status: StatusStale, CreatedAt: time.Now().Add(-91 * 24 * time.Hour)},
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
	added, _ := s.Harvest("F115-test", "coder", learnings)
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
	added, _ := s.Harvest("F061", "reviewer", []RetroLearning{
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
	added, _ := s.Harvest("F061", "reviewer", []RetroLearning{
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
	added, _ := s.Harvest("F061", "tester", []RetroLearning{
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
	added, _ := s.Harvest("F117-test", "coder", []RetroLearning{
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

	added, _ := s.Harvest("F101", "reviewer", []RetroLearning{
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

	added, _ := s.Harvest("F100", "reviewer", []RetroLearning{
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
	if s.Entries[0].ActivatedAt.IsZero() {
		t.Error("L001 should have ActivatedAt set after promotion")
	}
	if s.Entries[1].Status != StatusActive {
		t.Errorf("L002 should stay active, got %s", s.Entries[1].Status)
	}
	if s.Entries[2].Status != StatusCandidate {
		t.Errorf("L003 should remain candidate, got %s", s.Entries[2].Status)
	}
}

func TestHarvest_CrossFeaturePromotion_SetsActivatedAt(t *testing.T) {
	s := Store{Version: 1}
	s.Harvest("F100", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always run gofmt and go vet before commit"},
	})
	if !s.Entries[0].ActivatedAt.IsZero() {
		t.Error("candidate should not have ActivatedAt")
	}

	s.Harvest("F101", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "run gofmt and go vet before every commit"},
	})
	if s.Entries[0].Status != StatusActive {
		t.Fatalf("expected promotion to active, got %s", s.Entries[0].Status)
	}
	if s.Entries[0].ActivatedAt.IsZero() {
		t.Error("promoted entry should have ActivatedAt set")
	}
}

// recurrenceEvidence 產生「與 target 內容相似（Jaccard >= RecurrenceSimilarityThreshold）、
// 同 category、來自相異 feature」的 candidate 條目，供 ReevaluateIneffective 的 recurrence 判定使用。
func recurrenceEvidence(ids, features []string, cat Category) []Entry {
	out := make([]Entry, 0, len(ids))
	for i := range ids {
		out = append(out, Entry{
			ID: ids[i], Status: StatusCandidate, Category: cat,
			Content:       "wrap errors from " + features[i] + " helper",
			SourceFeature: features[i], CreatedAt: time.Now(),
		})
	}
	return out
}

func TestReevaluateIneffective_MeetsAllConditions(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)
	s := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, ActivatedAt: old, SourceFeature: "F001", CreatedAt: old},
	}, recurrenceEvidence([]string{"L010", "L011", "L012"}, []string{"F050", "F051", "F052"}, CategoryCodeQuality)...)}
	marked, reset := s.ReevaluateIneffective()
	if marked != 1 || reset != 0 {
		t.Errorf("expected marked=1 reset=0, got marked=%d reset=%d", marked, reset)
	}
	if !s.Entries[0].Ineffective {
		t.Error("L001 should be marked ineffective")
	}
}

func TestReevaluateIneffective_NotEnoughUsage(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)
	s := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 2, ActivatedAt: old, SourceFeature: "F001", CreatedAt: old},
	}, recurrenceEvidence([]string{"L010", "L011", "L012"}, []string{"F050", "F051", "F052"}, CategoryCodeQuality)...)}
	marked, _ := s.ReevaluateIneffective()
	if marked != 0 {
		t.Errorf("expected 0 marked (UsedCount < 3), got %d", marked)
	}
	if s.Entries[0].Ineffective {
		t.Error("L001 should NOT be marked ineffective")
	}
}

func TestReevaluateIneffective_TooRecent(t *testing.T) {
	recent := time.Now().Add(-10 * 24 * time.Hour)
	s := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, ActivatedAt: recent, SourceFeature: "F001", CreatedAt: recent},
	}, recurrenceEvidence([]string{"L010", "L011", "L012"}, []string{"F050", "F051", "F052"}, CategoryCodeQuality)...)}
	marked, _ := s.ReevaluateIneffective()
	if marked != 0 {
		t.Errorf("expected 0 marked (too recent), got %d", marked)
	}
}

func TestReevaluateIneffective_NoCategoryContinuation(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, ActivatedAt: old, SourceFeature: "F001", CreatedAt: old},
		{ID: "L010", Status: StatusCandidate, Category: CategoryTesting, Content: "wrap errors from F050 helper",
			SourceFeature: "F050", CreatedAt: time.Now()},
		{ID: "L011", Status: StatusCandidate, Category: CategoryDesign, Content: "wrap errors from F051 helper",
			SourceFeature: "F051", CreatedAt: time.Now()},
		{ID: "L012", Status: StatusCandidate, Category: CategoryOps, Content: "wrap errors from F052 helper",
			SourceFeature: "F052", CreatedAt: time.Now()},
	}}
	marked, _ := s.ReevaluateIneffective()
	if marked != 0 {
		t.Errorf("expected 0 marked (no same-category recurrence), got %d", marked)
	}
}

func TestReevaluateIneffective_FallsBackToCreatedAt(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)
	s := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, CreatedAt: old, SourceFeature: "F001"},
	}, recurrenceEvidence([]string{"L010", "L011", "L012"}, []string{"F050", "F051", "F052"}, CategoryCodeQuality)...)}
	marked, _ := s.ReevaluateIneffective()
	if marked != 1 {
		t.Errorf("expected 1 marked (fallback to CreatedAt), got %d", marked)
	}
}

// TestReevaluateIneffective_AlreadyMarked 驗證雙向重評對「已標記」條目的處置：
// 證據仍成立則維持 true 且不重複計數；證據不成立（只剩單一相異 feature）則撤銷旗標並計入 reset。
func TestReevaluateIneffective_AlreadyMarked(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)

	// 證據仍成立：兩個相異 feature 提供相似內容。
	stillValid := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, ActivatedAt: old, SourceFeature: "F001", Ineffective: true},
	}, recurrenceEvidence([]string{"L010", "L011"}, []string{"F050", "F051"}, CategoryCodeQuality)...)}
	marked, reset := stillValid.ReevaluateIneffective()
	if marked != 0 || reset != 0 {
		t.Errorf("already-marked entry with standing evidence: marked=%d reset=%d, want 0/0", marked, reset)
	}
	if !stillValid.Entries[0].Ineffective {
		t.Error("L001 should stay ineffective while evidence stands")
	}

	// 證據不成立：只有單一相異 feature，未達 RecurrenceMinDistinctFeatures。
	evidenceGone := Store{Version: 1, Entries: append([]Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryCodeQuality, Content: "wrap errors",
			UsedCount: 5, ActivatedAt: old, SourceFeature: "F001", Ineffective: true},
	}, recurrenceEvidence([]string{"L010"}, []string{"F050"}, CategoryCodeQuality)...)}
	marked, reset = evidenceGone.ReevaluateIneffective()
	if marked != 0 || reset != 1 {
		t.Errorf("already-marked entry with evidence gone: marked=%d reset=%d, want 0/1", marked, reset)
	}
	if evidenceGone.Entries[0].Ineffective {
		t.Error("L001 should be reset to false when evidence is gone")
	}
}

// TestEntryConfidenceRoundTripAndLegacyFallback 驗證 confidence 欄位 round-trip，
// 且舊 JSON（無 confidence）載入不失敗、version 不變、以 fallback score 參與排序（AC-1）。
func TestEntryConfidenceRoundTripAndLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "learnings.json")

	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Category: CategoryDesign, Content: "with confidence", Status: StatusActive, Confidence: 0.7, CreatedAt: time.Now()},
	}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"confidence": 0.7`) {
		t.Errorf("confidence field not serialized: %s", data)
	}
	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Entries[0].Confidence != 0.7 {
		t.Errorf("confidence round-trip lost: got %v", loaded.Entries[0].Confidence)
	}

	// 舊 JSON fixture（無 confidence 欄位）。
	legacy := `{"version":1,"entries":[{"id":"L009","category":"design","content":"legacy","created_at":"2024-01-01T00:00:00Z","used_count":2,"status":"active"}]}`
	legacyPath := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	ls, err := LoadStore(legacyPath)
	if err != nil {
		t.Fatalf("legacy load failed: %v", err)
	}
	if ls.Version != StoreVersion {
		t.Errorf("legacy load should migrate to v%d, got %d", StoreVersion, ls.Version)
	}
	e := ls.Entries[0]
	if e.Confidence != 0 {
		t.Errorf("legacy confidence should stay 0, got %v", e.Confidence)
	}
	want := InitialConfidence + 2*ConfidenceReinforceStep // 0.3 + 2*0.1 = 0.5
	if e.SortScore() != want {
		t.Errorf("legacy SortScore = %v, want %v", e.SortScore(), want)
	}
}

// TestUpdateUsageReinforcesConfidence 驗證 UpdateUsage 對命中 entry 同時更新 LastUsed、
// UsedCount++、提升 Confidence，未命中 entry 完全不變（AC-2）。
func TestUpdateUsageReinforcesConfidence(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, UsedCount: 1, Confidence: 0.4},
		{ID: "L002", Status: StatusActive, UsedCount: 5, Confidence: 0.6},
	}}
	s.UpdateUsage([]string{"L001"})

	if s.Entries[0].UsedCount != 2 {
		t.Errorf("L001 used_count = %d, want 2", s.Entries[0].UsedCount)
	}
	if s.Entries[0].LastUsed.IsZero() {
		t.Error("L001 last_used should be set")
	}
	if s.Entries[0].Confidence <= 0.4 {
		t.Errorf("L001 confidence should increase from 0.4, got %v", s.Entries[0].Confidence)
	}
	// 未命中 entry 不變。
	if s.Entries[1].UsedCount != 5 || s.Entries[1].Confidence != 0.6 || !s.Entries[1].LastUsed.IsZero() {
		t.Errorf("L002 should be untouched, got %+v", s.Entries[1])
	}
}

// TestDemoteInactiveActive 驗證 active demote：超過門檻的 active 依 LastUsed/ActivatedAt/CreatedAt
// 三種時間基準改回 candidate；promoted/stale/既有 candidate 不動；maxIdleDays<=0 停用（AC-3）。
func TestDemoteInactiveActive(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	recent := time.Now().Add(-1 * 24 * time.Hour)

	newStore := func() Store {
		return Store{Version: 1, Entries: []Entry{
			{ID: "A1", Status: StatusActive, LastUsed: old},                    // 依 LastUsed → demote
			{ID: "A2", Status: StatusActive, ActivatedAt: old},                 // LastUsed 零 → ActivatedAt → demote
			{ID: "A3", Status: StatusActive, CreatedAt: old},                   // LastUsed/ActivatedAt 零 → CreatedAt → demote
			{ID: "A4", Status: StatusActive, LastUsed: recent, CreatedAt: old}, // 近期命中 → 不動
			{ID: "P1", Status: StatusPromoted, CreatedAt: old},                 // promoted → 不動
			{ID: "S1", Status: StatusStale, CreatedAt: old},                    // stale → 不動
			{ID: "C1", Status: StatusCandidate, CreatedAt: old},                // candidate → 不動
		}}
	}

	t.Run("超過門檻 demote，其餘不動", func(t *testing.T) {
		s := newStore()
		n := s.DemoteInactiveActive(90)
		if n != 3 {
			t.Errorf("demoted = %d, want 3", n)
		}
		want := map[string]Status{
			"A1": StatusCandidate, "A2": StatusCandidate, "A3": StatusCandidate,
			"A4": StatusActive, "P1": StatusPromoted, "S1": StatusStale, "C1": StatusCandidate,
		}
		for _, e := range s.Entries {
			if e.Status != want[e.ID] {
				t.Errorf("%s status = %s, want %s", e.ID, e.Status, want[e.ID])
			}
		}
	})

	t.Run("maxIdleDays<=0 停用", func(t *testing.T) {
		s := newStore()
		if n := s.DemoteInactiveActive(0); n != 0 {
			t.Errorf("disabled demote should return 0, got %d", n)
		}
		if s.Entries[0].Status != StatusActive {
			t.Errorf("A1 should stay active when disabled, got %s", s.Entries[0].Status)
		}
	})
}

// TestLoadStore_LegacyConfidenceMigratesToV2 驗證舊 store（無 confidence 欄位）載入時會由 F187 的
// v1→v2 migration 抬升版本；F119 當初「version 須維持 1」的守門意圖已由 F187 取代。
// confidence 欄位缺席仍須以零值載入且不報錯——migration 只動 Version 與 Ineffective，不波及 Confidence。
func TestLoadStore_LegacyConfidenceMigratesToV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "learnings.json")
	legacy := `{"version":1,"entries":[{"id":"L001","category":"testing","content":"x","created_at":"2024-01-01T00:00:00Z","used_count":0,"status":"candidate"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != StoreVersion {
		t.Errorf("version = %d, want %d", s.Version, StoreVersion)
	}
	if s.Entries[0].Confidence != 0 {
		t.Errorf("legacy confidence should stay 0 after migration, got %v", s.Entries[0].Confidence)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	// 重新載入證明 migration 冪等：版本已是 v2，不會第二次觸發。
	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != StoreVersion {
		t.Errorf("reloaded version = %d, want %d", reloaded.Version, StoreVersion)
	}
	if reloaded.MigrationApplied() {
		t.Error("migration should not re-trigger on an already-v2 store")
	}
}

// TestHarvestInitialAndFuzzyPromotionConfidence 驗證 Harvest 來源端 confidence：新 candidate 有初始
// confidence；fuzzy match 到不同 feature candidate 升 active 時設 ActivatedAt 並提升 confidence（AC-13）。
func TestHarvestInitialAndFuzzyPromotionConfidence(t *testing.T) {
	// 新 candidate 初始 confidence。
	s := Store{Version: 1}
	added, _ := s.Harvest("F001", "coder", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "wrap errors with context always"},
	})
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	if s.Entries[0].Confidence != InitialConfidence {
		t.Errorf("new candidate confidence = %v, want %v", s.Entries[0].Confidence, InitialConfidence)
	}

	// fuzzy match 到不同 feature candidate（confidence 為零值的舊資料）→ 升 active、設 ActivatedAt、提升 confidence。
	// 內容須為 Jaccard 相似但非 exact/normalized 相同，否則會走前置的完全去重而不觸發升級。
	s2 := Store{Version: 1, Entries: []Entry{
		{ID: "L001", SourceFeature: "F001", Category: CategoryCodeQuality, Content: "always wrap errors with returned context", Status: StatusCandidate, Confidence: 0},
	}}
	before := len(s2.Entries)
	s2.Harvest("F002", "reviewer", []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors with context"},
	})
	if len(s2.Entries) != before {
		t.Errorf("fuzzy match should not add new entry, got %d entries", len(s2.Entries))
	}
	e := s2.Entries[0]
	if e.Status != StatusActive {
		t.Errorf("fuzzy-matched candidate should be promoted to active, got %s", e.Status)
	}
	if e.ActivatedAt.IsZero() {
		t.Error("promoted candidate should have ActivatedAt set")
	}
	if e.Confidence == 0 {
		t.Error("promoted candidate must not keep zero-value confidence")
	}
}

// distinctHarvestContents 回傳彼此 Jaccard 遠低於 FuzzyDupThreshold 的 learning 內容，
// 讓桶上限測試不會被 Harvest 的三層去重先攔下而測不到第四層。
func distinctHarvestContents() []string {
	return []string{
		"always run gofmt before committing go code",
		"prefer table driven tests for enum coverage",
		"wrap errors with context at every boundary",
		"avoid global mutable state inside packages",
		"document exported symbols with godoc comments",
		"check goroutine leaks in server integration suites",
		"pin dependency versions in the module file",
		"measure allocations before optimizing hot loops",
	}
}

// reviewLearnings 把 contents 包成 category=review 的 RetroLearning 列表。
func reviewLearnings(contents []string) []RetroLearning {
	out := make([]RetroLearning, 0, len(contents))
	for _, c := range contents {
		out = append(out, RetroLearning{Category: CategoryReview, Content: c})
	}
	return out
}

// recurrenceTarget 回傳 recurrence 測試共用的 target：active、process、UsedCount=5、
// ActivatedAt 為 60 天前、SourceFeature=F100。
func recurrenceTarget() Entry {
	old := time.Now().Add(-60 * 24 * time.Hour)
	return Entry{
		ID: "L001", Status: StatusActive, Category: CategoryProcess,
		Content: "alpha beta gamma delta", UsedCount: 5,
		ActivatedAt: old, CreatedAt: old, SourceFeature: "F100",
	}
}

// TestHasRecurrenceEvidence_SingleFeatureNotEnough 驗證所有候選證據都來自同一個 SourceFeature 時
// 不構成 recurrence——舊規則只看「最近 3 筆有同 category」，單一 feature 一輪吐出多條就會誤判（AC-1）。
func TestHasRecurrenceEvidence_SingleFeatureNotEnough(t *testing.T) {
	entries := []Entry{recurrenceTarget()}
	for i := 0; i < 6; i++ {
		entries = append(entries, Entry{
			ID: fmt.Sprintf("L1%02d", i), Status: StatusCandidate, Category: CategoryProcess,
			Content:       fmt.Sprintf("alpha beta gamma epsilon-%d", i),
			SourceFeature: "F200", CreatedAt: time.Now().Add(-24 * time.Hour),
		})
	}
	s := Store{Version: StoreVersion, Entries: entries}

	marked, _ := s.ReevaluateIneffective()
	if marked != 0 {
		t.Errorf("marked = %d, want 0 (all evidence from a single source_feature)", marked)
	}
	if s.Entries[0].Ineffective {
		t.Error("target should not be marked ineffective")
	}
}

// TestHasRecurrenceEvidence_SameCategoryDissimilarContent 驗證同 category 但內容不相似不構成
// recurrence。3 條證據來自相異 feature 且 status 明寫為 candidate，唯一不成立的條件就是 Jaccard
// < RecurrenceSimilarityThreshold——status 若留零值會被 status 過濾先擋掉，測不到相似度閘門（AC-2）。
func TestHasRecurrenceEvidence_SameCategoryDissimilarContent(t *testing.T) {
	entries := []Entry{recurrenceTarget()}
	for i, feature := range []string{"F201", "F202", "F203"} {
		entries = append(entries, Entry{
			ID: fmt.Sprintf("L2%02d", i), Status: StatusCandidate, Category: CategoryProcess,
			Content:       fmt.Sprintf("zeta eta theta iota-%d", i),
			SourceFeature: feature, CreatedAt: time.Now().Add(-24 * time.Hour),
		})
	}
	s := Store{Version: StoreVersion, Entries: entries}

	marked, _ := s.ReevaluateIneffective()
	if marked != 0 {
		t.Errorf("marked = %d, want 0 (same category but dissimilar content)", marked)
	}
	if s.Entries[0].Ineffective {
		t.Error("target should not be marked ineffective")
	}
}

// TestHasRecurrenceEvidence_TwoDistinctFeaturesSimilarContent 驗證恰好
// RecurrenceMinDistinctFeatures 個相異 feature 各提供一條同 category、相似內容的條目時
// recurrence 成立（AC-3）。
func TestHasRecurrenceEvidence_TwoDistinctFeaturesSimilarContent(t *testing.T) {
	entries := []Entry{recurrenceTarget()}
	for i, feature := range []string{"F301", "F302"} {
		entries = append(entries, Entry{
			ID: fmt.Sprintf("L3%02d", i), Status: StatusCandidate, Category: CategoryProcess,
			Content:       fmt.Sprintf("alpha beta gamma epsilon-%d", i),
			SourceFeature: feature, CreatedAt: time.Now().Add(-24 * time.Hour),
		})
	}
	s := Store{Version: StoreVersion, Entries: entries}

	marked, reset := s.ReevaluateIneffective()
	if marked != 1 || reset != 0 {
		t.Errorf("marked=%d reset=%d, want 1/0", marked, reset)
	}
	if !s.Entries[0].Ineffective {
		t.Error("target should be marked ineffective when recurrence evidence stands")
	}
}

// TestRecurrenceThreshold_BelowFuzzyDupThreshold 釘住兩個門檻的大小關係：harvest 期去重保證
// store 內任兩條 Jaccard < FuzzyDupThreshold，recurrence 門檻若被調到相撞就永遠不成立（AC-4）。
func TestRecurrenceThreshold_BelowFuzzyDupThreshold(t *testing.T) {
	recurrence, fuzzy := float64(RecurrenceSimilarityThreshold), float64(FuzzyDupThreshold)
	if recurrence >= fuzzy {
		t.Errorf("RecurrenceSimilarityThreshold (%v) must be strictly below FuzzyDupThreshold (%v)", recurrence, fuzzy)
	}
	if RecurrenceMinDistinctFeatures < 2 {
		t.Errorf("RecurrenceMinDistinctFeatures = %d, want >= 2", RecurrenceMinDistinctFeatures)
	}
}

// TestReevaluateIneffective_ResetsWhenEvidenceGone 驗證 Ineffective 可逆：三條件不再全部成立時
// 旗標被撤銷並計入 reset，而非停留在終態（AC-5）。
func TestReevaluateIneffective_ResetsWhenEvidenceGone(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour)
	s := Store{Version: StoreVersion, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryProcess, Content: "alpha beta gamma delta",
			UsedCount: 5, ActivatedAt: old, CreatedAt: old, SourceFeature: "F100", Ineffective: true},
	}}

	marked, reset := s.ReevaluateIneffective()
	if marked != 0 || reset != 1 {
		t.Errorf("marked=%d reset=%d, want 0/1", marked, reset)
	}
	if s.Entries[0].Ineffective {
		t.Error("ineffective flag should be reset once evidence is gone")
	}
}

// TestLoadStore_MigratesIneffectiveReset 驗證 v1 store 載入時執行一次性重設：
// 所有 ineffective 條目轉 false、ID 依原始順序寫入 IneffectiveResetIDs、版本抬升、MigrationApplied 為 true（AC-6）。
func TestLoadStore_MigratesIneffectiveReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learnings.json")
	fixture := `{"version":1,"entries":[
		{"id":"L001","source_feature":"F1","category":"ops","content":"a one","created_at":"2024-01-01T00:00:00Z","used_count":5,"status":"active","ineffective":true},
		{"id":"L002","source_feature":"F2","category":"ops","content":"b two","created_at":"2024-01-01T00:00:00Z","used_count":1,"status":"active"},
		{"id":"L003","source_feature":"F3","category":"ops","content":"c three","created_at":"2024-01-01T00:00:00Z","used_count":5,"status":"active","ineffective":true},
		{"id":"L004","source_feature":"F4","category":"ops","content":"d four","created_at":"2024-01-01T00:00:00Z","used_count":0,"status":"candidate"},
		{"id":"L005","source_feature":"F5","category":"ops","content":"e five","created_at":"2024-01-01T00:00:00Z","used_count":5,"status":"active","ineffective":true}
	]}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != StoreVersion {
		t.Errorf("version = %d, want %d", s.Version, StoreVersion)
	}
	if !s.MigrationApplied() {
		t.Error("MigrationApplied() = false, want true for a v1 store")
	}
	for _, e := range s.Entries {
		if e.Ineffective {
			t.Errorf("%s should have been reset to ineffective=false", e.ID)
		}
	}
	want := []string{"L001", "L003", "L005"}
	if len(s.IneffectiveResetIDs) != len(want) {
		t.Fatalf("IneffectiveResetIDs = %v, want %v", s.IneffectiveResetIDs, want)
	}
	for i, id := range want {
		if s.IneffectiveResetIDs[i] != id {
			t.Errorf("IneffectiveResetIDs[%d] = %s, want %s", i, s.IneffectiveResetIDs[i], id)
		}
	}
}

// TestLoadStore_V2NoMigration 驗證 v2 store 不重跑重設：ineffective 維持 true、
// IneffectiveResetIDs 維持磁碟原值、MigrationApplied 為 false（AC-7）。
func TestLoadStore_V2NoMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learnings.json")
	fixture := `{"version":2,"entries":[
		{"id":"L001","source_feature":"F1","category":"ops","content":"a one","created_at":"2024-01-01T00:00:00Z","used_count":5,"status":"active","ineffective":true}
	],"ineffective_reset_ids":["L900"]}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.MigrationApplied() {
		t.Error("MigrationApplied() = true, want false for a v2 store")
	}
	if !s.Entries[0].Ineffective {
		t.Error("v2 store must keep ineffective=true untouched")
	}
	if len(s.IneffectiveResetIDs) != 1 || s.IneffectiveResetIDs[0] != "L900" {
		t.Errorf("IneffectiveResetIDs = %v, want [L900]", s.IneffectiveResetIDs)
	}
}

// TestLoadStore_LegacyFieldsRoundTrip 驗證含全部現行 Entry JSON 欄位名的 v1 fixture 走
// LoadStore → Save → LoadStore 後，除 Version（1→2）與 Ineffective（migration 轉 false）外
// 每個欄位值都與原始 fixture 相同，且不出現未知欄位解析錯誤（AC-8）。
func TestLoadStore_LegacyFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "learnings.json")
	fixture := `{"version":1,"entries":[{"id":"L001","source_feature":"F100","source_role":"coder","category":"design","content":"legacy full field entry","created_at":"2024-01-01T00:00:00Z","activated_at":"2024-02-01T00:00:00Z","last_used":"2024-03-01T00:00:00Z","used_count":4,"status":"active","ineffective":true,"confidence":0.55}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := LoadStore(path)
	if err != nil {
		t.Fatalf("legacy load failed: %v", err)
	}
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if s.Version != StoreVersion {
		t.Errorf("version = %d, want %d", s.Version, StoreVersion)
	}
	if len(s.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(s.Entries))
	}
	e := s.Entries[0]
	mustTime := func(v string) time.Time {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	if e.ID != "L001" || e.SourceFeature != "F100" || e.SourceRole != "coder" {
		t.Errorf("identity fields lost: %+v", e)
	}
	if e.Category != CategoryDesign || e.Content != "legacy full field entry" {
		t.Errorf("category/content lost: %+v", e)
	}
	if !e.CreatedAt.Equal(mustTime("2024-01-01T00:00:00Z")) {
		t.Errorf("created_at = %v", e.CreatedAt)
	}
	if !e.ActivatedAt.Equal(mustTime("2024-02-01T00:00:00Z")) {
		t.Errorf("activated_at = %v", e.ActivatedAt)
	}
	if !e.LastUsed.Equal(mustTime("2024-03-01T00:00:00Z")) {
		t.Errorf("last_used = %v", e.LastUsed)
	}
	if e.UsedCount != 4 || e.Status != StatusActive || e.Confidence != 0.55 {
		t.Errorf("used_count/status/confidence lost: %+v", e)
	}
	if e.Ineffective {
		t.Error("ineffective should be false after the v1→v2 migration")
	}
}

// TestHarvest_CapsPerFeatureCategory 驗證單一 (SourceFeature, Category) 桶最多寫入
// MaxPerFeatureCategory 條，超出者只計入 skipped 不寫入（AC-12）。
func TestHarvest_CapsPerFeatureCategory(t *testing.T) {
	s := Store{Version: StoreVersion}
	added, skipped := s.Harvest("F300", "coder", reviewLearnings(distinctHarvestContents()))
	if added != MaxPerFeatureCategory {
		t.Errorf("added = %d, want %d", added, MaxPerFeatureCategory)
	}
	if want := len(distinctHarvestContents()) - MaxPerFeatureCategory; skipped != want {
		t.Errorf("skipped = %d, want %d", skipped, want)
	}
	if len(s.Entries) != MaxPerFeatureCategory {
		t.Errorf("entries = %d, want %d", len(s.Entries), MaxPerFeatureCategory)
	}
}

// TestHarvest_CapCountsExistingEntries 驗證桶上限對既有 store 條目一併計數、跨多次 Harvest
// 呼叫仍有效，且桶以 SourceFeature 分開計（AC-13）。
func TestHarvest_CapCountsExistingEntries(t *testing.T) {
	contents := distinctHarvestContents()
	s := Store{Version: StoreVersion}

	if added, skipped := s.Harvest("F300", "coder", reviewLearnings(contents[:2])); added != 2 || skipped != 0 {
		t.Fatalf("first harvest: added=%d skipped=%d, want 2/0", added, skipped)
	}
	added, skipped := s.Harvest("F300", "reviewer", reviewLearnings(contents[2:5]))
	if added != 1 || skipped != 2 {
		t.Errorf("second harvest: added=%d skipped=%d, want 1/2", added, skipped)
	}
	if got := countBucket(s, "F300", CategoryReview); got != MaxPerFeatureCategory {
		t.Errorf("(F300, review) bucket = %d, want %d", got, MaxPerFeatureCategory)
	}

	added, skipped = s.Harvest("F301", "coder", reviewLearnings(contents[5:8]))
	if added != 3 || skipped != 0 {
		t.Errorf("different feature harvest: added=%d skipped=%d, want 3/0", added, skipped)
	}
	if got := countBucket(s, "F301", CategoryReview); got != 3 {
		t.Errorf("(F301, review) bucket = %d, want 3", got)
	}
}

// countBucket 計算 store 內指定 (SourceFeature, Category) 桶的條目數。
func countBucket(s Store, sourceFeature string, cat Category) int {
	n := 0
	for _, e := range s.Entries {
		if e.SourceFeature == sourceFeature && e.Category == cat {
			n++
		}
	}
	return n
}
