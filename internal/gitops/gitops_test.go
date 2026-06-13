package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
