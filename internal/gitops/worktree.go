package gitops

import (
	"os"
	"path/filepath"
)

// WorktreeInfo 描述某個目錄所屬的 git worktree 狀態。
type WorktreeInfo struct {
	// Root 是該目錄所屬 worktree 的 top-level 根目錄（git rev-parse --show-toplevel）。
	Root string
	// IsLinked 為 true 表示這是 git worktree add 產生的 linked worktree；
	// false 表示主工作區（main working tree）。
	IsLinked bool
}

// DetectWorktree 偵測 dir 所屬的 git worktree，回傳其 top-level 根目錄與是否為 linked worktree。
//
// scope 檢查在 worktree 隔離模式下，git 操作的根目錄會被解析到 main workspace，
// 導致誤把 main 的未提交變更算進 feature 的 changed repos。此 helper 讓呼叫端能
// 把 git 操作限定在 worktree 自身的根目錄。
//
// 判定方式：以 git rev-parse --git-dir 與 --git-common-dir 是否相異判定是否為 linked
// worktree（linked worktree 的 git-dir 指向 <common>/.git/worktrees/<name>，與 common
// dir 不同）。dir 不在 git repo 內時回傳 ok=false。
func DetectWorktree(dir string) (WorktreeInfo, bool) {
	top := gitOutput(dir, "rev-parse", "--show-toplevel")
	if top == "" {
		return WorktreeInfo{}, false
	}
	gitDir := gitOutput(dir, "rev-parse", "--git-dir")
	commonDir := gitOutput(dir, "rev-parse", "--git-common-dir")
	isLinked := gitDir != "" && commonDir != "" &&
		resolveGitPath(dir, gitDir) != resolveGitPath(dir, commonDir)
	return WorktreeInfo{Root: top, IsLinked: isLinked}, true
}

// ScopeRoot 回傳掃描 feature scope 變更時應使用的根目錄。
// 若 feature 的 worktree 目錄存在且確為 linked worktree，回傳其 top-level 根目錄；
// 否則回退到 fallback（通常為 main workspace 根），維持非 worktree 情境的既有行為。
func ScopeRoot(fallback, featureID string) string {
	wtDir := Dir(fallback, featureID)
	if info, ok := DetectWorktree(wtDir); ok && info.IsLinked {
		return info.Root
	}
	return fallback
}

// ScopeRoots 回傳掃描 feature scope 變更時應使用的根目錄清單，涵蓋 multi-repo 情境。
//
// feature 的 worktree 容器目錄（Dir(fallback, featureID)）在 mono-repo 下本身就是
// linked worktree；在 multi-repo 下容器目錄不是 git repo，各 sub-repo 的 linked
// worktree 是其子目錄（見 multiRepo.SetupWorktree）。對容器目錄整體判定 IsLinked 在
// multi-repo 下會誤判為 false（git 往上層找到主 repo），故容器本身非 linked worktree
// 時進一步掃描子目錄，逐一判定是否為 linked worktree。
//
// mono-repo（含非 worktree 情境）回傳結果恆為單元素 slice，與 ScopeRoot 回傳值一致，
// 呼叫端對其迭代等同執行一次，行為不變。
func ScopeRoots(fallback, featureID string) []string {
	wtDir := Dir(fallback, featureID)
	if info, ok := DetectWorktree(wtDir); ok && info.IsLinked {
		return []string{info.Root}
	}
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return []string{fallback}
	}
	var roots []string
	for _, e := range entries {
		sub := filepath.Join(wtDir, e.Name())
		if info, ok := DetectWorktree(sub); ok && info.IsLinked {
			roots = append(roots, info.Root)
		}
	}
	if len(roots) == 0 {
		return []string{fallback}
	}
	return roots
}

// resolveGitPath 將 git rev-parse 回傳的 git-dir / git-common-dir 解析成可比較的絕對路徑。
// rev-parse 可能回相對路徑（相對於 base），故先補成絕對再 EvalSymlinks 消除 /var → /private/var
// 等 symlink 差異，確保兩邊比較一致。
func resolveGitPath(base, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
