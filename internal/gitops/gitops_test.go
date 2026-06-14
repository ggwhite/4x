package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureGitignore_ExactMatch(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# .worktrees/ is managed\nnode_modules/\n"), 0o644)
	ensureGitignore(root, ".worktrees/")
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	content := string(data)
	// Should have added .worktrees/ as a separate line
	lines := strings.Split(content, "\n")
	found := false
	for _, line := range lines {
		if strings.TrimSpace(line) == ".worktrees/" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("should add .worktrees/ entry when only present in comment, got:\n%s", content)
	}
}

func TestEnsureGitignore_AlreadyExists(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n.worktrees/\n"), 0o644)
	ensureGitignore(root, ".worktrees/")
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if strings.Count(string(data), ".worktrees/") != 1 {
		t.Errorf("should not duplicate, got:\n%s", data)
	}
}

func TestCopyFileIfNewer_SourceMissing(t *testing.T) {
	root := t.TempDir()
	copied, err := CopyFileIfNewer(filepath.Join(root, "nope.txt"), filepath.Join(root, "dst.txt"))
	if err != nil {
		t.Fatalf("source missing should be silent success, got err: %v", err)
	}
	if copied {
		t.Error("source missing should report copied=false")
	}
	if _, statErr := os.Stat(filepath.Join(root, "dst.txt")); statErr == nil {
		t.Error("dst should not be created when source missing")
	}
}

func TestCopyFileIfNewer_DestMissing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "sub", "dst.txt")
	os.WriteFile(src, []byte("hello"), 0o644)

	copied, err := CopyFileIfNewer(src, dst)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !copied {
		t.Error("dst missing should report copied=true")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "hello" {
		t.Errorf("dst content = %q, want hello", data)
	}
}

func TestCopyFileIfNewer_SourceOlderSkips(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "dst.txt")
	os.WriteFile(src, []byte("old"), 0o644)
	os.WriteFile(dst, []byte("newer"), 0o644)

	// 讓 src 的 mtime 明顯早於 dst
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	copied, err := CopyFileIfNewer(src, dst)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if copied {
		t.Error("source older than dst should skip (copied=false)")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "newer" {
		t.Errorf("dst should be untouched, got %q", data)
	}
}

func TestCopyFileIfNewer_SourceNewerCopies(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "dst.txt")
	os.WriteFile(dst, []byte("stale"), 0o644)
	os.WriteFile(src, []byte("fresh"), 0o644)

	// 讓 dst 的 mtime 明顯早於 src
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(dst, old, old); err != nil {
		t.Fatal(err)
	}

	copied, err := CopyFileIfNewer(src, dst)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !copied {
		t.Error("source newer than dst should copy (copied=true)")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "fresh" {
		t.Errorf("dst content = %q, want fresh", data)
	}
}
