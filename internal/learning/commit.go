package learning

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// CommitIfDirty 檢查 learnings.json 是否有未 commit 的變更，若有則自動 git add + commit。
// root 為 git repo 根目錄，dotDir 為 .4x/ 目錄的絕對路徑。
func CommitIfDirty(root, dotDir string) {
	lpath := filepath.Join(dotDir, "learnings.json")
	if _, err := os.Stat(lpath); err != nil {
		return
	}
	rel, _ := filepath.Rel(root, lpath)
	cmd := exec.Command("git", "diff", "--quiet", "--", rel)
	cmd.Dir = root
	if cmd.Run() == nil {
		return
	}
	add := exec.Command("git", "add", rel)
	add.Dir = root
	if err := add.Run(); err != nil {
		slog.Warn("git add learnings.json failed", "error", err)
		return
	}
	ci := exec.Command("git", "commit", "-m", "chore: update learnings.json")
	ci.Dir = root
	if err := ci.Run(); err != nil {
		slog.Warn("git commit learnings.json failed", "error", err)
	}
}
