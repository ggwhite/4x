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

func (m *multiRepo) SetupWorktree(featureID string, featureRepos []string) (string, error) {
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

	repos := m.targetRepos(featureRepos)
	for name, rc := range repos {
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

// targetRepos 回傳 feature 宣告的 repo 子集；featureRepos 為空時回傳全部 workspace repos。
func (m *multiRepo) targetRepos(featureRepos []string) map[string]protocol.RepoConfig {
	if len(featureRepos) == 0 {
		return m.cfg.Workspace.Repos
	}
	allowed := make(map[string]bool, len(featureRepos))
	for _, r := range featureRepos {
		allowed[r] = true
	}
	result := make(map[string]protocol.RepoConfig, len(featureRepos))
	for name, rc := range m.cfg.Workspace.Repos {
		if allowed[name] {
			result[name] = rc
		}
	}
	return result
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
			hadConflicts := len(files) > 0
			if hadConflicts {
				autoResolveFeatureYAML(rh.repoPath, files)
				files = conflictFiles(rh.repoPath)
			}
			if len(files) > 0 || !hadConflicts {
				exec.Command("git", "-C", rh.repoPath, "merge", "--abort").Run()
				exec.Command("git", "-C", rh.repoPath, "reset", "--hard", "HEAD").Run()
				for _, done := range merged {
					exec.Command("git", "-C", done.repoPath, "reset", "--hard", done.head).Run()
				}
				if len(files) > 0 {
					return MergeResult{Conflict: true, ConflictRepo: rh.name, Files: files}
				}
				return MergeResult{Error: fmt.Sprintf("%s: %s", rh.name, strings.TrimSpace(string(out)))}
			}
		}
		if out, err := exec.Command("git", "-C", rh.repoPath, "commit", "-m", msg).CombinedOutput(); err != nil {
			if !isNothingToCommit(string(out)) {
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

// DetectChangedRepos 找出 feature 範圍內哪些 repo 有 uncommitted 變更。
// worktree 隔離模式下每個 repo 的工作目錄是 <worktreeRoot>/<name>（與 SetupWorktree
// 的佈局一致），而非 main workspace 下的 rc.Path；故先確認該 worktree 子目錄確為 linked
// worktree 再在其中執行 git 指令，否則回退到 main 的 rc.Path 維持非 worktree 情境的既有行為。
// tracked 變更（git diff HEAD）與 untracked 新檔（git ls-files --others）任一非空即視為變更，
// 後者是 git diff HEAD 涵蓋不到、會被靜默繞過的缺口。
func (m *multiRepo) DetectChangedRepos(featureID string) []string {
	wtDir := Dir(m.root, featureID)
	var changed []string
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		if wtRepoDir := filepath.Join(wtDir, name); isLinkedWorktree(wtRepoDir) {
			repoPath = wtRepoDir
		}
		diff := gitOutput(repoPath, "diff", "--name-only", "HEAD")
		untracked := gitOutput(repoPath, "ls-files", "--others", "--exclude-standard")
		if diff != "" || untracked != "" {
			changed = append(changed, name)
		}
	}
	return changed
}

// isLinkedWorktree 回報 dir 是否為一個存在的 linked git worktree。
func isLinkedWorktree(dir string) bool {
	info, ok := DetectWorktree(dir)
	return ok && info.IsLinked
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
