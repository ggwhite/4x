package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMonoDetectChangedRepos_DetectsUntracked 驗證 F081 Task 1：
// monoRepo.DetectChangedRepos 須涵蓋 untracked 新檔（git add 之前），
// 否則在範圍外 repo 新增整個檔案會被 scope guard 漏掉而靜默繞過。
func TestMonoDetectChangedRepos_DetectsUntracked(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	// 新增一個全新、尚未 git add 的子目錄檔案（untracked）。
	dir := filepath.Join(root, "lib")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ops.DetectChangedRepos("feat-x")
	if !contains(got, "lib") {
		t.Errorf("untracked new file under lib/ should be detected, got %v", got)
	}
}

// TestMonoDetectChangedRepos_SkipsRootLevelFiles 驗證 F081 Task 3 一致性：
// 根目錄檔案（路徑無 "/"）不是 repo，不可被當成 repo 名稱回報，
// 否則 checkScope 會誤判 scope violation（false positive）。
func TestMonoDetectChangedRepos_SkipsRootLevelFiles(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	// 根目錄新檔（untracked），路徑無 "/"。
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ops.DetectChangedRepos("feat-x")
	if contains(got, "README.md") {
		t.Errorf("root-level file must not be reported as a repo, got %v", got)
	}
}

// TestMultiDetectChangedRepos_DetectsUntracked 驗證 F081 Task 2：
// multiRepo.DetectChangedRepos 須對每個 repo 額外檢查 untracked 新檔，
// 只要 diff 或 ls-files 任一非空即視為該 repo 有變更。
func TestMultiDetectChangedRepos_DetectsUntracked(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	// 在 gate repo 內新增 untracked 檔案。
	if err := os.WriteFile(filepath.Join(root, "gate", "new.go"), []byte("package gate\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ops.DetectChangedRepos("feat-x")
	if !contains(got, "gate") {
		t.Errorf("untracked new file in gate repo should be detected, got %v", got)
	}
	if contains(got, "core") {
		t.Errorf("core repo has no changes, should not be reported, got %v", got)
	}
}
