package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMultiRepo_SetupWorktree_GoWorkPruned 驗證 AC-3：feature 只宣告 core 時，
// worktree 內的 go.work 只保留 ./core 的 use，指向未 checkout 的 gate 被移除。
func TestMultiRepo_SetupWorktree_GoWorkPruned(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-prune", []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wtPath, "go.work"))
	if err != nil {
		t.Fatalf("go.work should exist in worktree: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "core") {
		t.Errorf("go.work should keep core use:\n%s", content)
	}
	if strings.Contains(content, "gate") {
		t.Errorf("go.work should drop gate use (not checked out):\n%s", content)
	}
}

// TestMultiRepo_SetupWorktree_GoWorkOmittedWhenEmpty 驗證 AC-4：裁切後無任何 use 保留時，
// worktree 不存在 go.work，也不存在 go.work.sum。
func TestMultiRepo_SetupWorktree_GoWorkOmittedWhenEmpty(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	// go.work 只 use 一個永遠不會被 checkout 的 ghost 目錄。
	os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\nuse ./ghost\n"), 0o644)
	os.WriteFile(filepath.Join(root, "go.work.sum"), []byte("dummy sum\n"), 0o644)

	wtPath, err := ops.SetupWorktree("feat-empty", []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "go.work")); !os.IsNotExist(err) {
		t.Errorf("go.work should NOT exist when all uses pruned, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "go.work.sum")); !os.IsNotExist(err) {
		t.Errorf("go.work.sum should NOT exist when go.work omitted, stat err = %v", err)
	}
}

// TestMultiRepo_SetupWorktree_NoGoWork 驗證 AC-5：無 go.work 的 multi-repo workspace
// 行為與改動前一致——不建立 go.work，其餘根目錄檔案照舊複製。
func TestMultiRepo_SetupWorktree_NoGoWork(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	os.Remove(filepath.Join(root, "go.work"))
	os.WriteFile(filepath.Join(root, "rootfile.txt"), []byte("hi\n"), 0o644)

	wtPath, err := ops.SetupWorktree("feat-nogowork", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "go.work")); !os.IsNotExist(err) {
		t.Errorf("go.work should NOT be created when workspace has none, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "rootfile.txt")); err != nil {
		t.Errorf("other root files should still be copied: %v", err)
	}
}
