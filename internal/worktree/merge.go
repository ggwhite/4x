// Package worktree 提供 git worktree 的 merge 與 cleanup 操作，供 CLI 與 server 共用。
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MergeResult 描述 Merge 操作的結果。
type MergeResult struct {
	Skipped  bool
	Conflict bool
	Error    string
	Files    []string
}

// Merge 嘗試將 worktree branch 4x/<featureID> merge 回 root 所在的當前 branch。
// 若 worktree 目錄不存在，回傳 Skipped: true。
// merge 成功則呼叫 Cleanup 清理 worktree 與 branch。
// 發生衝突時 abort merge（保留 worktree 供手動解決），回傳 Conflict: true。
// 其他 git 錯誤（dirty tree、index.lock 等）回傳 Error 並 abort。
func Merge(root, featureID, featureName string) MergeResult {
	wtDir := Dir(root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)

	curBranch, err := exec.Command("git", "-C", root, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return MergeResult{Error: "cannot determine current branch (detached HEAD?)"}
	}
	if strings.TrimSpace(string(curBranch)) == branch {
		return MergeResult{Error: fmt.Sprintf("current branch is %s — switch to main/master first", branch)}
	}

	msg := fmt.Sprintf("Merge branch '%s' — %s", branch, featureName)
	out, err := exec.Command("git", "-C", root, "merge", "--no-ff", "-m", msg, branch).CombinedOutput()
	if err != nil {
		files := conflictFiles(root)
		exec.Command("git", "-C", root, "merge", "--abort").Run()
		if len(files) > 0 {
			return MergeResult{Conflict: true, Files: files}
		}
		return MergeResult{Error: strings.TrimSpace(string(out))}
	}
	_ = out

	Cleanup(root, featureID)
	return MergeResult{}
}

// Cleanup 移除 worktree 目錄並刪除對應的 branch。
// worktree remove 失敗時以 --force 重試；branch 刪除失敗僅 ignore（例如已刪）。
func Cleanup(root, featureID string) error {
	wtDir := Dir(root, featureID)
	branch := Branch(featureID)

	if out, err := exec.Command("git", "-C", root, "worktree", "remove", wtDir).CombinedOutput(); err != nil {
		exec.Command("git", "-C", root, "worktree", "remove", "--force", wtDir).Run()
		if _, statErr := os.Stat(wtDir); statErr == nil {
			return fmt.Errorf("worktree remove failed: %s", string(out))
		}
	}

	exec.Command("git", "-C", root, "branch", "-D", branch).Run()
	return nil
}

// Dir 回傳 worktree 目錄的絕對路徑。
func Dir(root, featureID string) string {
	return filepath.Join(root, ".worktrees", "4x", featureID)
}

// Branch 回傳 feature 對應的 worktree branch 名稱。
func Branch(featureID string) string {
	return "4x/" + featureID
}

// conflictFiles 透過 git diff 取得目前 merge 衝突的檔案列表。
func conflictFiles(root string) []string {
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}
