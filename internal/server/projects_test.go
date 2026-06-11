package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRecentProjects_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recent-projects.json")
	rp, err := LoadRecentProjects(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rp.Projects) != 0 {
		t.Errorf("projects = %d, want 0", len(rp.Projects))
	}
}

func TestSaveAndLoadRecentProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recent-projects.json")

	rp := &RecentProjects{}
	rp.Touch("/tmp/project-a")
	rp.Touch("/tmp/project-b")

	if err := SaveRecentProjects(path, rp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadRecentProjects(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(loaded.Projects))
	}
	if loaded.Projects[0].Path != "/tmp/project-b" {
		t.Errorf("first = %s, want /tmp/project-b", loaded.Projects[0].Path)
	}
}

func TestRecentProjects_TouchExisting(t *testing.T) {
	rp := &RecentProjects{}
	rp.Touch("/tmp/a")
	rp.Touch("/tmp/b")
	rp.Touch("/tmp/a")

	if len(rp.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(rp.Projects))
	}
	if rp.Projects[0].Path != "/tmp/a" {
		t.Errorf("first = %s, want /tmp/a", rp.Projects[0].Path)
	}
}

func TestRecentProjects_MaxLimit(t *testing.T) {
	rp := &RecentProjects{}
	for i := 0; i < 25; i++ {
		rp.Touch(filepath.Join("/tmp", "p"+string(rune('a'+i))))
	}
	if len(rp.Projects) != 20 {
		t.Errorf("projects = %d, want 20 (max)", len(rp.Projects))
	}
}

func TestRecentProjects_Remove(t *testing.T) {
	rp := &RecentProjects{}
	rp.Touch("/tmp/a")
	rp.Touch("/tmp/b")
	rp.Remove("/tmp/a")

	if len(rp.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(rp.Projects))
	}
	if rp.Projects[0].Path != "/tmp/b" {
		t.Errorf("remaining = %s, want /tmp/b", rp.Projects[0].Path)
	}
}

func TestDefaultRecentProjectsPath(t *testing.T) {
	path, err := DefaultRecentProjectsPath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".4x", "recent-projects.json")
	if path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
}
