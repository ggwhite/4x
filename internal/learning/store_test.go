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
	added := s.Harvest("F042-test", learnings)
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
	if s.Entries[0].Status != StatusActive {
		t.Errorf("expected active status, got %s", s.Entries[0].Status)
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
	added := s.Harvest("F043", learnings)
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
	added := s.Harvest("F044", learnings)
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
	if got := CategoriesForRole("coder"); len(got) != 3 {
		t.Errorf("expected 3 categories for coder, got %v", got)
	}
	if got := CategoriesForRole("acceptor"); len(got) != 1 || got[0] != CategoryProcess {
		t.Errorf("expected [process] for acceptor, got %v", got)
	}
	if got := CategoriesForRole("unknown"); got != nil {
		t.Errorf("expected nil for unknown role, got %v", got)
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
