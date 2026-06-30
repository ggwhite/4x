package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestLearnPromote(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "test", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	if err := loaded.Promote("L001"); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := learning.LoadStore(storePath)
	if reloaded.Entries[0].Status != learning.StatusPromoted {
		t.Errorf("expected promoted, got %s", reloaded.Entries[0].Status)
	}
}

func TestLearnRemove(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "a", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "b", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	if err := loaded.Remove("L001"); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 || reloaded.Entries[0].ID != "L002" {
		t.Errorf("expected only L002, got %+v", reloaded.Entries)
	}
}

func TestLearnPrune(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	old := time.Now().Add(-91 * 24 * time.Hour)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "old", Status: learning.StatusActive, CreatedAt: old},
		{ID: "L002", Category: learning.CategoryDesign, Content: "new", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	loaded.MarkStale(learning.DefaultStaleDays)
	removed := loaded.Prune()
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(reloaded.Entries))
	}
}

func TestLearnAdd_Success(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "ops", "--content", "always set GOWORK=off in worktree"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	loaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	e := loaded.Entries[0]
	if e.SourceFeature != "manual" {
		t.Errorf("expected sourceFeature=manual, got %q", e.SourceFeature)
	}
	if e.SourceRole != "user" {
		t.Errorf("expected sourceRole=user, got %q", e.SourceRole)
	}
	if e.Category != learning.CategoryOps {
		t.Errorf("expected category=ops, got %q", e.Category)
	}
}

func TestLearnAdd_InvalidCategory(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "bogus", "--content", "test content"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid category")
	}
	if got := err.Error(); !strings.Contains(got, "invalid category") {
		t.Errorf("expected 'invalid category' in error, got %q", got)
	}
}

func TestLearnAdd_FuzzyDuplicate(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryOps, Content: "always set GOWORK=off in worktree", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "ops", "--content", "always set GOWORK=off in worktree"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
	if got := err.Error(); !strings.Contains(got, "similar learning already exists: L001") {
		t.Errorf("expected 'similar learning already exists: L001', got %q", got)
	}

	loaded, _ := learning.LoadStore(storePath)
	if len(loaded.Entries) != 1 {
		t.Errorf("expected store unchanged with 1 entry, got %d", len(loaded.Entries))
	}
}

func TestLearnAdd_JSON(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnAddCmd()
		cmd.SetArgs([]string{"--category", "ops", "--content", "test json output", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	var result struct {
		ID    string `json:"id"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if !result.Added {
		t.Error("expected added=true")
	}
	if result.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestFindLearningsPath(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	path, err := findLearningsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	if path != want {
		t.Errorf("expected %s, got %s", want, path)
	}
}
