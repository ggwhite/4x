package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestF180TrackedPaths 用真實 temp git repo 驗證 TrackedPaths 的三類路徑分類：
// 已 commit 的檔在回傳 set 內；untracked 新檔與 .gitignore 排除的檔皆不在。
func TestF180TrackedPaths(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "test")
	runGit(t, dir, "config", "user.email", "test@test")

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(".gitignore", "ignored.go\n")
	write("tracked.go", "package x\n")
	runGit(t, dir, "add", ".gitignore", "tracked.go")
	runGit(t, dir, "commit", "-m", "init")
	// 這兩個檔案在 commit 之後才寫入：一個 untracked、一個被 .gitignore 排除。
	write("untracked.go", "package x\n")
	write("ignored.go", "package x\n")

	got := TrackedPaths(dir)
	if !got["tracked.go"] {
		t.Errorf("tracked.go should be tracked, got set=%v", got)
	}
	if got[".gitignore"] == false {
		t.Errorf(".gitignore should be tracked, got set=%v", got)
	}
	if got["untracked.go"] {
		t.Errorf("untracked.go must NOT be tracked")
	}
	if got["ignored.go"] {
		t.Errorf("gitignored ignored.go must NOT be tracked")
	}

	// git 執行失敗（非 git 目錄）→ 空集合、不 panic。
	if empty := TrackedPaths(t.TempDir()); len(empty) != 0 {
		t.Errorf("non-git dir should return empty set, got %v", empty)
	}
}
