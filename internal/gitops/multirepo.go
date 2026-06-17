package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

type multiRepo struct {
	root string
	ws   *protocol.Workspace
	cfg  protocol.Config
}

func (m *multiRepo) IsMultiRepo() bool { return true }

func (m *multiRepo) SetupWorktree(featureID string) (string, error) {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	ensureGitignore(m.root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		m.ensureDotDir(wtDir)
		return wtDir, nil
	}

	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return "", err
	}

	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)

		out, err := exec.Command("git", "-C", repoPath, "worktree", "add", wtRepoDir, "-b", branch).CombinedOutput()
		if err != nil {
			out2, err2 := exec.Command("git", "-C", repoPath, "worktree", "add", wtRepoDir, branch).CombinedOutput()
			if err2 != nil {
				m.cleanupPartial(wtDir, featureID)
				return "", fmt.Errorf("git worktree add %s: %s\n%s", name, string(out), string(out2))
			}
		}
	}

	m.copyWorkspaceFiles(wtDir)
	m.ensureDotDir(wtDir)
	return wtDir, nil
}

func (m *multiRepo) cleanupPartial(wtDir, featureID string) {
	branch := Branch(featureID)
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtRepoDir).Run()
		exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run()
	}
	os.RemoveAll(wtDir)
}

func (m *multiRepo) copyWorkspaceFiles(wtDir string) {
	repoDirs := make(map[string]bool)
	for _, rc := range m.cfg.Workspace.Repos {
		parts := strings.SplitN(rc.Path, "/", 2)
		repoDirs[parts[0]] = true
	}
	repoDirs[protocol.DirName] = true
	repoDirs[".worktrees"] = true
	repoDirs[".git"] = true

	entries, err := os.ReadDir(m.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if repoDirs[name] {
			continue
		}
		if e.IsDir() {
			continue
		}
		copyFileIfExists(filepath.Join(m.root, name), filepath.Join(wtDir, name))
	}
}

func (m *multiRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)
	syncDotDirContents(m.root, dotDir)
}

func (m *multiRepo) Commit(wtRoot, featureID, msg string) error {
	for name := range m.cfg.Workspace.Repos {
		repoDir := filepath.Join(wtRoot, name)
		if _, err := os.Stat(repoDir); err != nil {
			continue
		}
		if out, err := exec.Command("git", "-C", repoDir, "add", "-A").CombinedOutput(); err != nil {
			return fmt.Errorf("git add %s: %s: %w", name, string(out), err)
		}
		if exec.Command("git", "-C", repoDir, "diff", "--cached", "--quiet").Run() == nil {
			continue
		}
		if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", msg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit %s: %s: %w", name, string(out), err)
		}
		fmt.Printf("  committed [%s]: %s\n", name, msg)
	}
	return nil
}

func (m *multiRepo) Merge(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)
	msg := fmt.Sprintf("feat(%s): %s", featureID, featureName)

	type repoHead struct {
		name     string
		repoPath string
		head     string
	}

	var preHeads []repoHead
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)

		curBranch, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD").Output()
		if err != nil {
			return MergeResult{Error: fmt.Sprintf("%s: cannot determine current branch", name)}
		}
		if strings.TrimSpace(string(curBranch)) == branch {
			return MergeResult{Error: fmt.Sprintf("%s: current branch is %s — switch to main/master first", name, branch)}
		}

		head := gitOutput(repoPath, "rev-parse", "HEAD")
		preHeads = append(preHeads, repoHead{name: name, repoPath: repoPath, head: head})
	}

	var merged []repoHead
	for _, rh := range preHeads {
		if exec.Command("git", "-C", rh.repoPath, "rev-parse", "--verify", branch).Run() != nil {
			continue
		}

		out, err := exec.Command("git", "-C", rh.repoPath, "merge", "--squash", branch).CombinedOutput()
		if err != nil {
			files := conflictFiles(rh.repoPath)
			exec.Command("git", "-C", rh.repoPath, "merge", "--abort").Run()

			for _, done := range merged {
				exec.Command("git", "-C", done.repoPath, "reset", "--hard", done.head).Run()
			}

			if len(files) > 0 {
				return MergeResult{Conflict: true, ConflictRepo: rh.name, Files: files}
			}
			return MergeResult{Error: fmt.Sprintf("%s: %s", rh.name, strings.TrimSpace(string(out)))}
		}
		if out, err := exec.Command("git", "-C", rh.repoPath, "commit", "-m", msg).CombinedOutput(); err != nil {
			if !strings.Contains(string(out), "nothing to commit") {
				exec.Command("git", "-C", rh.repoPath, "reset", "--hard", rh.head).Run()
				for _, done := range merged {
					exec.Command("git", "-C", done.repoPath, "reset", "--hard", done.head).Run()
				}
				return MergeResult{Error: fmt.Sprintf("%s: %s", rh.name, strings.TrimSpace(string(out)))}
			}
		}
		merged = append(merged, rh)
	}

	m.Cleanup(featureID)
	return MergeResult{}
}

func (m *multiRepo) Cleanup(featureID string) error {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	cleaned := make(map[string]bool)
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		cleaned[name] = true

		removeWorktreeDir(repoPath, wtRepoDir)
		exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run()
	}

	cleanOrphanedWorktrees(wtDir, branch, cleaned)
	os.RemoveAll(wtDir)
	return nil
}

// removeWorktreeDir 嘗試移除單一 worktree 目錄，先正常移除再 force，最後 os.RemoveAll 兜底。
func removeWorktreeDir(repoPath, wtRepoDir string) {
	if exec.Command("git", "-C", repoPath, "worktree", "remove", wtRepoDir).Run() == nil {
		return
	}
	if exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtRepoDir).Run() == nil {
		return
	}
	os.RemoveAll(wtRepoDir)
}

// cleanOrphanedWorktrees 掃描 wtDir 下尚未被清理的子目錄，
// 透過讀取 .git 檔還原 parent repo 並執行 worktree remove + branch 刪除。
func cleanOrphanedWorktrees(wtDir, branch string, cleaned map[string]bool) {
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || cleaned[e.Name()] {
			continue
		}
		subDir := filepath.Join(wtDir, e.Name())
		parentRepo := resolveWorktreeParent(subDir)
		if parentRepo != "" {
			removeWorktreeDir(parentRepo, subDir)
			exec.Command("git", "-C", parentRepo, "branch", "-D", branch).Run()
		} else {
			os.RemoveAll(subDir)
		}
	}
}

// resolveWorktreeParent 從 worktree 的 .git 檔解析出 parent repo 路徑。
func resolveWorktreeParent(wtDir string) string {
	data, err := os.ReadFile(filepath.Join(wtDir, ".git"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	gitdir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(wtDir, gitdir)
	}
	// gitdir = <parent>/.git/worktrees/<name>  →  往上兩層得到 <parent>/.git
	dotGit := filepath.Dir(filepath.Dir(gitdir))
	if filepath.Base(dotGit) != ".git" {
		return ""
	}
	return filepath.Dir(dotGit)
}

func (m *multiRepo) DetectChangedRepos() []string {
	var changed []string
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		out := gitOutput(repoPath, "diff", "--name-only", "HEAD")
		if out != "" {
			changed = append(changed, name)
		}
	}
	return changed
}

func (m *multiRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	repoPaths := protocol.ResolveFeatureRepoPaths(
		feature.Feature{Repos: featureRepos}, m.cfg, m.root,
	)
	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for name, fullPath := range repoPaths {
		repoPath := ""
		if rc, ok := m.cfg.Workspace.Repos[name]; ok {
			repoPath = rc.Path
		}
		if br := captureRepoBaseline(fullPath, name, repoPath); br != nil {
			baseline.Repos = append(baseline.Repos, *br)
		}
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
