package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

type monoRepo struct {
	root string
	ws   *protocol.Workspace
}

func (m *monoRepo) IsMultiRepo() bool { return false }

func (m *monoRepo) SetupWorktree(featureID string) (string, error) {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	ensureGitignore(m.root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		m.ensureDotDir(wtDir)
		return wtDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", err
	}

	out, err := exec.Command("git", "-C", m.root, "worktree", "add", wtDir, "-b", branch).CombinedOutput()
	if err != nil {
		out2, err2 := exec.Command("git", "-C", m.root, "worktree", "add", wtDir, branch).CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("git worktree add: %s\n%s", string(out), string(out2))
		}
	}

	m.ensureDotDir(wtDir)
	return wtDir, nil
}

func (m *monoRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)
	if info, err := os.Lstat(dotDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			os.Remove(dotDir)
		}
	}
	syncDotDirContents(m.root, dotDir)
}

func (m *monoRepo) Commit(wtPath, featureID, msg string) error {
	if out, err := exec.Command("git", "-C", wtPath, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", string(out), err)
	}
	if exec.Command("git", "-C", wtPath, "diff", "--cached", "--quiet").Run() == nil {
		return nil
	}
	if out, err := exec.Command("git", "-C", wtPath, "commit", "-m", msg).CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", string(out), err)
	}
	fmt.Printf("  committed: %s\n", msg)
	return nil
}

func (m *monoRepo) Merge(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)

	curBranch, err := exec.Command("git", "-C", m.root, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return MergeResult{Error: "cannot determine current branch (detached HEAD?)"}
	}
	if strings.TrimSpace(string(curBranch)) == branch {
		return MergeResult{Error: fmt.Sprintf("current branch is %s — switch to main/master first", branch)}
	}

	out, err := exec.Command("git", "-C", m.root, "merge", "--squash", branch).CombinedOutput()
	if err != nil {
		files := conflictFiles(m.root)
		exec.Command("git", "-C", m.root, "merge", "--abort").Run()
		if len(files) > 0 {
			return MergeResult{Conflict: true, Files: files}
		}
		return MergeResult{Error: strings.TrimSpace(string(out))}
	}

	msg := fmt.Sprintf("feat(%s): %s", featureID, featureName)
	if out, err := exec.Command("git", "-C", m.root, "commit", "-m", msg).CombinedOutput(); err != nil {
		return MergeResult{Error: strings.TrimSpace(string(out))}
	}

	m.Cleanup(featureID)
	return MergeResult{}
}

func (m *monoRepo) Cleanup(featureID string) error {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	if out, err := exec.Command("git", "-C", m.root, "worktree", "remove", wtDir).CombinedOutput(); err != nil {
		exec.Command("git", "-C", m.root, "worktree", "remove", "--force", wtDir).Run()
		if _, statErr := os.Stat(wtDir); statErr == nil {
			return fmt.Errorf("worktree remove failed: %s", string(out))
		}
	}

	exec.Command("git", "-C", m.root, "branch", "-D", branch).Run()
	return nil
}

func (m *monoRepo) DetectChangedRepos() []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = m.root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	repoSet := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "/", 2)
		if len(parts) > 0 {
			repoSet[parts[0]] = true
		}
	}

	var repos []string
	for r := range repoSet {
		repos = append(repos, r)
	}
	return repos
}

func (m *monoRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	if len(featureRepos) == 0 {
		featureRepos = []string{"."}
	}
	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for _, repoPath := range featureRepos {
		fullPath := filepath.Join(m.root, repoPath)
		if repoPath == "." {
			fullPath = m.root
		}
		if br := captureRepoBaseline(fullPath, repoPath, repoPath); br != nil {
			baseline.Repos = append(baseline.Repos, *br)
		}
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
