package main

import (
	"path/filepath"
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
