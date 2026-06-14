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
	Commit(wtRoot, featureID, msg string) error
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

// copyFileIfExists 複製檔案；來源不存在視為靜默成功（回 nil），
// 僅讀寫失敗（如 disk full、目標不可寫）才回傳 error，讓上游能記錄真因。
func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// CopyFileIfExists 複製檔案，來源不存在時靜默忽略（回 nil）；讀寫失敗回傳 error。供外部 package 使用。
func CopyFileIfExists(src, dst string) error {
	return copyFileIfExists(src, dst)
}

// syncDotDirContents 將 mainRoot 的 .4x/settings.json 和 plugins/ 複製到 dotDir。
func syncDotDirContents(mainRoot, dotDir string) {
	os.MkdirAll(dotDir, 0o755)

	src := filepath.Join(mainRoot, protocol.DirName, protocol.ConfigFile)
	dst := filepath.Join(dotDir, protocol.ConfigFile)
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(dst, data, 0o644)
	}

	srcPlugins := filepath.Join(mainRoot, protocol.DirName, "plugins")
	dstPlugins := filepath.Join(dotDir, "plugins")
	if entries, err := os.ReadDir(srcPlugins); err == nil {
		os.MkdirAll(dstPlugins, 0o755)
		for _, e := range entries {
			if !e.IsDir() {
				copyFileIfExists(filepath.Join(srcPlugins, e.Name()), filepath.Join(dstPlugins, e.Name()))
			}
		}
	}
}

// captureRepoBaseline 擷取單一 repo 的 baseline 狀態，若非 git repo 則回傳 nil。
func captureRepoBaseline(fullPath, name, repoPath string) *protocol.BaselineRepo {
	if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
		return nil
	}
	head := gitOutput(fullPath, "rev-parse", "HEAD")
	branch := gitOutput(fullPath, "rev-parse", "--abbrev-ref", "HEAD")
	statusOut := gitOutput(fullPath, "status", "--short")

	var dirty []string
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			dirty = append(dirty, line)
		}
	}
	return &protocol.BaselineRepo{
		Name:       name,
		Path:       repoPath,
		Branch:     branch,
		Head:       head,
		DirtyFiles: dirty,
	}
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
