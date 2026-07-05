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

		syncUpstream(repoPath)

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
		copyFileIfExists(filepath.Join(m.root, name), filepath.Join(wtDir, name)) //nolint:errcheck // best-effort 複製 dot 檔，缺檔可接受
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

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 merge 結果
	return MergeResult{}
}

// PushAndOpenMR push feature branch 並對每個有 committed 變更的 repo 開 MR/PR，取代 Merge
// 供 issue_tracker.enabled 時使用。依 D5：逐 repo 用 committed-commits-ahead（相對該 repo
// baseline target branch）判定是否有變更，不可用 DetectChangedRepos（偵測的是 uncommitted
// 變更，done 時恆為空）。依 D6：partial-tolerant——單一 repo push/OpenMR 失敗不阻擋其他 repo，
// 但只有全部成功（errs 為空）才 Cleanup，避免部分失敗時遺失尚未 push 成功的 commit。
func (m *multiRepo) PushAndOpenMR(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)

	baselineBranch := make(map[string]string)
	if baseline, err := loadBaseline(m.ws, featureID); err == nil {
		for _, r := range baseline.Repos {
			baselineBranch[r.Name] = r.Branch
		}
	}

	feat, _ := m.ws.LoadFeature(featureID)
	issueByRepo := make(map[string]string, len(feat.Issues))
	for _, ir := range feat.Issues {
		issueByRepo[ir.Repo] = ir.ID
	}

	title := fmt.Sprintf("feat(%s): %s", featureID, featureName)

	mrUrls := make(map[string]string)
	var errs []string
	anyAhead := false
	for name, rc := range m.targetRepos(feat.Repos) {
		repoPath := filepath.Join(m.root, rc.Path)
		target := baselineBranch[name]
		if target == "" {
			target = "main"
		}

		out, err := exec.Command("git", "-C", repoPath, "rev-list", "--count", target+".."+branch).Output()
		if err != nil {
			// rev-list 失敗（如 target 無法解析）不可當「0 commits」處理：那會讓下方
			// !anyAhead 分支誤判整個 feature 無變更並 Cleanup 掉這個 repo 的 commits。
			// 記為 error，讓下方 len(errs)>0 分支保留 worktree 供重試。
			errs = append(errs, fmt.Sprintf("%s: cannot determine commits ahead of %s: %v", name, target, err))
			continue
		}
		count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		if count == 0 {
			continue
		}
		anyAhead = true

		if out, err := exec.Command("git", "-C", repoPath, "push", "origin", branch).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: push: %s", name, strings.TrimSpace(string(out))))
			continue
		}

		body := featureName
		if id := issueByRepo[name]; id != "" {
			body = fmt.Sprintf("Closes #%s\n\n%s", id, featureName)
		}

		// glab 用「當前 checkout 的 branch」當 MR source（見 vcshub.glabHub.OpenMR GoDoc），
		// repoPath（main workspace 下的 repo）全程停在 base branch，worktree 子目錄
		// wtDir/name 才 checkout 在 feature branch，故用它呼叫，避免 source==target。
		wtRepoDir := filepath.Join(wtDir, name)
		url, err := vcshubNew(wtRepoDir).OpenMR(wtRepoDir, branch, target, title, body)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		mrUrls[name] = url
	}

	// 只有 errs 為空才可能 Cleanup：rev-list 無法解析 target 時也會進 errs，
	// 避免把「無法判定是否有變更」誤當「真的沒有變更」而執行破壞性 Cleanup。
	if len(errs) > 0 {
		return MergeResult{MRUrls: mrUrls, Error: strings.Join(errs, "; ")}
	}
	if !anyAhead {
		m.Cleanup(featureID) //nolint:errcheck // 無 commit 可失，清理安全
		return MergeResult{Skipped: true}
	}
	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 MR 結果
	return MergeResult{MRUrls: mrUrls}
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
// worktree 再在其中執行 git 指令。
// worktree 目錄存在時，沒有 linked worktree 的 repo 代表不在此 feature 的工作範圍內，
// 跳過不檢查——否則會掃到主工作區的無關 dirty files 而誤判 scope violation。
// 非 worktree 情境則回退到 main 的 rc.Path 維持既有行為。
func (m *multiRepo) DetectChangedRepos(featureID string) []string {
	wtDir := Dir(m.root, featureID)
	_, wtErr := os.Stat(wtDir)
	wtActive := wtErr == nil
	var changed []string
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		if isLinkedWorktree(wtRepoDir) {
			repoPath = wtRepoDir
		} else if wtActive {
			continue
		}
		diff := gitOutput(repoPath, "diff", "--name-only", "HEAD")
		untracked := gitOutput(repoPath, "ls-files", "--others", "--exclude-standard")
		if diff != "" || untracked != "" {
			changed = append(changed, name)
		}
	}
	return changed
}

// DetectChangedFiles 回傳 feature 範圍內各 repo 的檔案層變更清單，路徑以 "<repo 名稱>/" 為前綴。
// worktree 隔離模式下每個 repo 的工作目錄是 <worktreeRoot>/<name>（與 DetectChangedRepos 一致），
// worktree 目錄存在時跳過沒有 linked worktree 的 repo，避免掃到主工作區的無關變更。
// 非 worktree 情境回退 main 的 rc.Path。供 self-mod guard 做受保護路徑前綴比對與 diff-budget。
func (m *multiRepo) DetectChangedFiles(featureID string) []protocol.ChangedFile {
	wtDir := Dir(m.root, featureID)
	_, wtErr := os.Stat(wtDir)
	wtActive := wtErr == nil
	var files []protocol.ChangedFile
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		if isLinkedWorktree(wtRepoDir) {
			repoPath = wtRepoDir
		} else if wtActive {
			continue
		}
		files = append(files, changedFilesIn(repoPath, name+"/")...)
	}
	return files
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
