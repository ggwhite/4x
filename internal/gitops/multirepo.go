package gitops

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// 第二道防線：目錄型與 go.work/go.work.sum 的宣告在此擋下（主 gate 是 4x check 的
	// checkSharedPaths，兩處呼叫同一個 ValidateSharedPathsInRoot，不寫兩份規則）。
	// 刻意放在下方 os.Stat(wtDir) 的冪等 early-return 之前，讓 resume 情境也擋得住；
	// 回錯誤時不得移除既有 worktree——那會刪掉 Coder 尚未 commit 的工作。
	sharedPaths := sharedPathsFor(m.ws, featureID)
	if err := ValidateSharedPathsInRoot(m.root, sharedPaths); err != nil {
		return "", err
	}
	// 基線寫入失敗只 warn：沒有基線只會讓 drift 偵測 fail-open，不該讓整個 feature 起不來。
	// 此時 Designer 多半尚未宣告（len(sharedPaths) == 0 → no-op），真正建出基線的時點
	// 是 designing 收尾那次 4x check；保留這次呼叫是為了「使用者事先在主工作區宣告」的形態。
	if err := UpsertSharedPathsBaseline(m.root, featureID, sharedPaths); err != nil {
		slog.Warn("shared_paths: upsert baseline failed", "feature", featureID, "err", err)
	}

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
	runPostScaffold(m.cfg, m.ws, wtDir, featureID)
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
		// go.work / go.work.sum 由 copyGoWork 專責處理（裁切 use，避免指向未 checkout 的目錄）。
		if isGoWorkFile(name) {
			continue
		}
		copyFileIfExists(filepath.Join(m.root, name), filepath.Join(wtDir, name)) //nolint:errcheck // best-effort 複製 dot 檔，缺檔可接受
	}

	m.copyGoWork(wtDir)
}

// copyGoWork 把 workspace 根目錄的 go.work 裁切後寫入 wtDir，只保留實際 checkout 進 worktree
// （wtDir/<rel>/go.mod 存在）的 module。無 go.work 時什麼都不做（保持既有行為）；裁切後無任何
// use 保留時整個 omit go.work（連同 go.work.sum 一併不寫），讓各 module 以 standalone go.mod build。
func (m *multiRepo) copyGoWork(wtDir string) {
	data, err := os.ReadFile(filepath.Join(m.root, "go.work"))
	if err != nil {
		return
	}
	out, anyKept := filterGoWorkUses(string(data), func(rel string) bool {
		_, statErr := os.Stat(filepath.Join(wtDir, rel, "go.mod"))
		return statErr == nil
	})
	if !anyKept {
		return
	}
	os.WriteFile(filepath.Join(wtDir, "go.work"), []byte(out), 0o644)
	copyFileIfExists(filepath.Join(m.root, "go.work.sum"), filepath.Join(wtDir, "go.work.sum")) //nolint:errcheck // best-effort 複製 sum，缺檔可接受
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
		slog.Info("committed", "repo", name, "message", msg)
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

	// multirepo 佈局的 .4x/ 位於 hub root、不在任何 cfg.Workspace.Repos 的 repo 目錄內，
	// 下方 preflight 只逐一檢查 repoPath，故此處對 m.root 執行一次自管路徑前置 commit
	// （root 非 git repo 時 best-effort 靜默略過），讓 preflight 攔截到的必定是使用者的變更。
	commitSelfManaged(m.root, featureID)

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

		// preflight：此迴圈在任何 repo 的 merge --squash 之前就跑完，任一 repo 主工作區有
		// tracked 的未 commit 變更即中止、不觸碰任何 repo，杜絕下方 reset --hard HEAD / reset
		// --hard done.head（含跨 repo 回滾）誤刪與本次 merge 無關的既有修改。
		if workingTreeDirty(repoPath) {
			return MergeResult{Error: fmt.Sprintf("%s: uncommitted changes in working tree, aborting merge", name)}
		}

		head := gitOutput(repoPath, "rev-parse", "HEAD")
		preHeads = append(preHeads, repoHead{name: name, repoPath: repoPath, head: head})
	}

	// shared_paths preflight：主工作區的宣告檔案相對快照基線有 drift 就中止，不觸碰任何 repo
	// （語意比照上方 workingTreeDirty 的 per-repo preflight）。merge-back 一律以 worktree 版覆寫
	// 主工作區，drift 時直接寫下去會蓋掉平行 feature 剛落地的內容，故此處只中止、不做三方合併。
	// 判定與 note 產生共用 sharedPathsPreflight，與 PushAndOpenMR 同一份實作。
	sharedPaths := sharedPathsFor(m.ws, featureID)
	sharedNotes, abortErr := sharedPathsPreflight(m.root, wtDir, featureID, sharedPaths)
	if abortErr != "" {
		return MergeResult{Error: abortErr}
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

	// merge-back 必須在 Cleanup 之前：worktree 一旦被刪，根層 shared_path 的改動就永久消失。
	// 衝突與 squash/commit 失敗兩條路徑在上方已 return 而不觸及 Cleanup，worktree 與其中的
	// 改動都保留，故刻意不在那些路徑做 merge-back。
	spMerged, spNotes := mergeBackSharedPaths(m.root, wtDir, featureID, msg, sharedPaths)
	sharedNotes = append(sharedNotes, spNotes...)

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 merge 結果
	return MergeResult{SharedPathsMerged: spMerged, SharedPathsNotes: sharedNotes}
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

	// shared_paths preflight：與 Merge 共用 sharedPathsPreflight，不寫第二份。此路徑沒有
	// commitSelfManaged 前置呼叫，主工作區的 .4x/ 可能仍是髒的；因為 merge-back 是 path-scoped
	// commit，不影響正確性。宣告直接取自上方已載入的 feat，不再呼叫 sharedPathsFor 重讀 YAML——
	// 重讀除了多一次 parse，還會讓 YAML 壞掉時的 log 指錯根因（那邊對同一個失敗是靜默忽略）。
	sharedPaths := feat.SharedPaths
	sharedNotes, abortErr := sharedPathsPreflight(m.root, wtDir, featureID, sharedPaths)
	if abortErr != "" {
		return MergeResult{Error: abortErr}
	}

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

	// merge-back 放在 len(errs) > 0 之後、兩條 Cleanup 之前：!anyAhead 路徑同樣會刪 worktree，
	// 根層 shared_path 的改動一樣會消失，故兩條都要做。push/OpenMR 有失敗時不做——
	// worktree 保留供重試，資料未遺失。
	spMerged, spNotes := mergeBackSharedPaths(m.root, wtDir, featureID, title, sharedPaths)
	sharedNotes = append(sharedNotes, spNotes...)
	if len(spMerged) > 0 {
		// 這筆 commit 進的是主工作區根 repo，PushAndOpenMR 只 push 各 workspace repo 的
		// feature branch，它永遠不會出現在任何 MR 裡——必須明講，否則使用者以為 MR 涵蓋全部。
		sharedNotes = append(sharedNotes, "shared-path commit landed in the main workspace root repo; not pushed and not part of any MR")
	}

	if !anyAhead {
		m.Cleanup(featureID) //nolint:errcheck // 無 commit 可失，清理安全
		return MergeResult{Skipped: true, SharedPathsMerged: spMerged, SharedPathsNotes: sharedNotes}
	}
	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 MR 結果
	return MergeResult{MRUrls: mrUrls, SharedPathsMerged: spMerged, SharedPathsNotes: sharedNotes}
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

// GenerateReviewPackage 為 multi-repo workspace 逐 repo 產出 review package。單一 baseCommit
// 無法套用到多個各自獨立 git history 的 repo，故此模式改用 baseline.json 記錄的 per-repo Head
// 作為每個 repo 各自的 base（同樣在首次進 coding 時擷取，語意與 monorepo 的 baseCommit 一致）；
// baseCommit 參數在此模式下不使用。
func (m *multiRepo) GenerateReviewPackage(featureID, _ string) (string, error) {
	baseline, err := loadBaseline(m.ws, featureID)
	if err != nil {
		return "", fmt.Errorf("load baseline for review package: %w", err)
	}
	repoHead := make(map[string]string, len(baseline.Repos))
	for _, br := range baseline.Repos {
		repoHead[br.Name] = br.Head
	}

	wtDir := Dir(m.root, featureID)
	_, wtErr := os.Stat(wtDir)
	wtActive := wtErr == nil

	// 依 repo 名稱排序，鎖定 deterministic 迭代順序：Go map range 順序隨機，
	// 若沿用會讓「跨 repo 共享預算下哪個 repo 先消耗 / 被截斷」不可重現、AC-3 測試 flaky。
	names := make([]string, 0, len(m.cfg.Workspace.Repos))
	for name := range m.cfg.Workspace.Repos {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Review Package\n\n")
	found := false
	// budget 在 repo loop 之前初始化一次，loop 內各 repo 共享同一預算池（跨 repo 共享上限）。
	budget := reviewPackageContentBudget
	for _, name := range names {
		rc := m.cfg.Workspace.Repos[name]
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		if isLinkedWorktree(wtRepoDir) {
			repoPath = wtRepoDir
		} else if wtActive {
			continue
		}
		section := reviewPackageSection(repoPath, repoHead[name], "###", &budget)
		if section == "" {
			continue
		}
		found = true
		fmt.Fprintf(&b, "## Repo: %s\n\n%s\n", name, section)
	}
	if !found {
		return "", fmt.Errorf("no diff produced for feature %s", featureID)
	}
	return b.String(), nil
}
