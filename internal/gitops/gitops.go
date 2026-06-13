// Package gitops 封裝所有 git 操作，根據 workspace config 決定 monorepo 或 multi-repo 模式。
package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// Ops 封裝所有 git 操作，根據 workspace config 決定 monorepo 或 multi-repo 模式。
type Ops interface {
	SetupWorktree(featureID string) (wtRoot string, err error)
	Commit(wtRoot, featureID, featureName string, round int) error
	Merge(featureID, featureName string) MergeResult
	Cleanup(featureID string) error
	DetectChangedRepos() []string
	CaptureBaseline(featureID string, featureRepos []string) error
	IsMultiRepo() bool
}

// MergeResult 描述 Merge 操作的結果。
type MergeResult struct {
	Skipped      bool
	Conflict     bool
	Error        string
	Files        []string
	ConflictRepo string
}

// New 根據 workspace config 建立對應的 Ops 實作。
func New(root string, ws *protocol.Workspace, cfg protocol.Config) Ops {
	if len(cfg.Workspace.Repos) > 0 {
		return &multiRepo{root: root, ws: ws, cfg: cfg}
	}
	return &monoRepo{root: root, ws: ws}
}

// Dir 回傳 worktree 組合目錄的路徑（兩種模式共用）。
func Dir(root, featureID string) string {
	return filepath.Join(root, ".worktrees", "4x", featureID)
}

// Branch 回傳 feature 對應的 branch 名稱（兩種模式共用）。
func Branch(featureID string) string {
	return "4x/" + featureID
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func ensureGitignore(root, entry string) {
	path := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}

func copyFileIfExists(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, data, 0o644)
}

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
