package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

type monoRepo struct {
	root string
	ws   *protocol.Workspace
}

func (m *monoRepo) IsMultiRepo() bool { return false }

func (m *monoRepo) SetupWorktree(featureID string, _ []string) (string, error) {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	ensureGitignore(m.root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		m.ensureDotDir(wtDir)
		return wtDir, nil
	}

	syncUpstream(m.root)

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
	// syncFeatureToWorktree 會從 main 複製 feature YAML 讓 runner 讀，但 main 也會透過
	// SyncFeatureStatus 更新同一份 YAML（status 欄位）。若把複製結果 commit 到 feature branch，
	// merge --squash 時幾乎保證衝突。此處 unstage 整個 features 目錄，讓 YAML 只留在 working
	// tree 供下一輪 runner 讀取，不進 feature branch 的 commit。
	featuresPath := filepath.Join(protocol.DirName, protocol.FeaturesDir)
	exec.Command("git", "-C", wtPath, "reset", "HEAD", "--", featuresPath).Run()
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
		if len(files) == 0 {
			// --squash 不建立 MERGE_HEAD，merge --abort 會靜默失敗；用 reset --hard 確保還原
			exec.Command("git", "-C", m.root, "merge", "--abort").Run()
			exec.Command("git", "-C", m.root, "reset", "--hard", "HEAD").Run()
			return MergeResult{Error: strings.TrimSpace(string(out))}
		}
		autoResolveFeatureYAML(m.root, files)
		files = conflictFiles(m.root)
		if len(files) > 0 {
			exec.Command("git", "-C", m.root, "merge", "--abort").Run()
			exec.Command("git", "-C", m.root, "reset", "--hard", "HEAD").Run()
			return MergeResult{Conflict: true, Files: files}
		}
	}

	msg := fmt.Sprintf("feat(%s): %s", featureID, featureName)
	if out, err := exec.Command("git", "-C", m.root, "commit", "-m", msg).CombinedOutput(); err != nil {
		if !isNothingToCommit(string(out)) {
			// commit 失敗（如 pre-commit hook 拒絕）時，merge --squash 已把變更 stage 進
			// main 的 index/working tree。比照衝突路徑的 merge --abort，這裡以 reset --hard
			// 還原到 merge 前狀態，避免 staged 變更殘留污染 main（multirepo.go 已有對等處理）。
			exec.Command("git", "-C", m.root, "reset", "--hard", "HEAD").Run()
			return MergeResult{Error: strings.TrimSpace(string(out))}
		}
	}

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 merge 結果
	return MergeResult{}
}

// PushAndOpenMR push feature branch 並開 MR/PR，取代 Merge 供 issue_tracker.enabled 時使用。
// 依 D5：用 committed-commits-ahead（相對 baseline target branch）判定是否有變更要開 MR，
// 不可用 DetectChangedFiles（那偵測的是 worktree 內 uncommitted 變更，done 時恆為空）。
// 依 D6：push/OpenMR 失敗時保留 worktree 供重試，只有成功才 Cleanup。
func (m *monoRepo) PushAndOpenMR(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)
	target := "main"
	if baseline, err := loadBaseline(m.ws, featureID); err == nil && len(baseline.Repos) > 0 && baseline.Repos[0].Branch != "" {
		target = baseline.Repos[0].Branch
	}

	out, err := exec.Command("git", "-C", m.root, "rev-list", "--count", target+".."+branch).Output()
	if err != nil {
		// rev-list 失敗（如 target 無法解析）不可當「0 commits」處理：那會觸發下方的
		// 破壞性 Cleanup，把含 commits 的 feature branch 連同 worktree 一起刪掉。
		// 保留 worktree 回 Error，供使用者修正 target 後重跑（同 D6 的失敗保留語意）。
		return MergeResult{Error: fmt.Sprintf("cannot determine commits ahead of %s: %v", target, err)}
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if count == 0 {
		m.Cleanup(featureID) //nolint:errcheck // 無 commit 可失，清理安全
		return MergeResult{Skipped: true}
	}

	if out, err := exec.Command("git", "-C", m.root, "push", "origin", branch).CombinedOutput(); err != nil {
		return MergeResult{Error: fmt.Sprintf("push: %s", strings.TrimSpace(string(out)))}
	}

	feat, _ := m.ws.LoadFeature(featureID)
	body := featureName
	for _, ir := range feat.Issues {
		if ir.Repo == "." && ir.ID != "" {
			body = fmt.Sprintf("Closes #%s\n\n%s", ir.ID, featureName)
			break
		}
	}

	// glab 用「當前 checkout 的 branch」當 MR source（見 vcshub.glabHub.OpenMR GoDoc），
	// m.root 全程停在 base branch（worktree 才 checkout 在 branch），故用 wtDir 呼叫，
	// 確保 glab 看到的當前 branch 就是 feature branch，避免 source==target。
	title := fmt.Sprintf("feat(%s): %s", featureID, featureName)
	url, err := vcshubNew(wtDir).OpenMR(wtDir, branch, target, title, body)
	if err != nil {
		return MergeResult{Error: err.Error()}
	}

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 MR 結果
	return MergeResult{MRUrls: map[string]string{".": url}}
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

// DetectChangedRepos 找出 feature 的 worktree（若存在）內哪些子目錄有 uncommitted 變更。
// worktree 隔離模式下 feature 變更在 <root>/.worktrees/4x/<featureID>，而非 m.root；
// 故先以 featureID 解析出 worktree 根，再執行 git 指令，避免誤掃 main workspace 的變更。
// 合併 tracked 變更（git diff HEAD）與 untracked 新檔（git ls-files --others），後者是
// git diff HEAD 涵蓋不到的缺口；兩條指令各自獨立容錯，單一失敗不影響另一條的結果。
func (m *monoRepo) DetectChangedRepos(featureID string) []string {
	root := ScopeRoot(m.root, featureID)
	repoSet := make(map[string]bool)

	collect := func(out []byte) {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "/", 2)
			// 根目錄檔案（go.mod、Makefile 等，路徑無 "/"）不是 repo，跳過避免誤判。
			if len(parts) < 2 {
				continue
			}
			repoSet[parts[0]] = true
		}
	}

	diffCmd := exec.Command("git", "diff", "--name-only", "HEAD")
	diffCmd.Dir = root
	if out, err := diffCmd.Output(); err == nil {
		collect(out)
	}

	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = root
	if out, err := untrackedCmd.Output(); err == nil {
		collect(out)
	}

	var repos []string
	for r := range repoSet {
		if r == protocol.DirName {
			continue
		}
		repos = append(repos, r)
	}
	return repos
}

// DetectChangedFiles 回傳 feature worktree 內檔案層的變更清單（路徑相對 scope root）。
// 沿用 DetectChangedRepos 的 worktree 解析（ScopeRoot），供 self-mod guard 做受保護路徑前綴比對與 diff-budget。
func (m *monoRepo) DetectChangedFiles(featureID string) []protocol.ChangedFile {
	return changedFilesIn(ScopeRoot(m.root, featureID), "")
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
