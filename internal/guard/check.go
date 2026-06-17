package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// CheckResult 是 `4x check` 的結果
type CheckResult struct {
	Pass   bool     `json:"pass"`
	Errors []string `json:"errors"`
	Warns  []string `json:"warnings"`
}

// ScopeDetector 偵測哪些 repo 有 uncommitted changes，由 gitops.Ops 實作。
// featureID 用來定位該 feature 的 worktree，使偵測限定在 worktree 內的變更。
type ScopeDetector interface {
	DetectChangedRepos(featureID string) []string
}

// Check 執行所有 guardrail 檢查。detector 為 nil 時 fallback 到本地 git diff。
func Check(ws *protocol.Workspace, featureID string, detector ScopeDetector) CheckResult {
	r := CheckResult{Pass: true}

	checkRequiredFiles(ws, featureID, &r)
	checkBaseline(ws, featureID, &r)
	checkScope(ws, featureID, detector, &r)
	checkDependencies(ws, featureID, &r)
	checkBacklogDrift(ws, featureID, &r)

	return r
}

// CheckDependencies 檢查 feature 的所有 dependency 是否已完成，用於 run 啟動前的 gate
func CheckDependencies(ws *protocol.Workspace, featureID string) CheckResult {
	r := CheckResult{Pass: true}
	checkDependencies(ws, featureID, &r)
	return r
}

func checkDependencies(ws *protocol.Workspace, featureID string, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot load feature YAML: %v", err))
		return
	}
	if len(feature.Depends) == 0 {
		return
	}

	var notDone []string
	for _, depID := range feature.Depends {
		dep, err := ws.LoadFeature(depID)
		if err != nil {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("dependency %q: cannot load feature: %v", depID, err))
			continue
		}
		if dep.Status != feat.StatusDone && dep.Status != feat.StatusAbandoned {
			notDone = append(notDone, fmt.Sprintf("%s (status: %s)", depID, dep.Status))
		}
	}
	if len(notDone) > 0 {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("dependencies not done: %s", strings.Join(notDone, ", ")))
	}
}

func checkBacklogDrift(ws *protocol.Workspace, featureID string, r *CheckResult) {
	drift, err := ws.CompareBacklogMirror()
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot compare %s: %v", protocol.BacklogFile, err))
		return
	}
	for _, d := range drift {
		if d.FeatureID == featureID {
			r.Warns = append(r.Warns, d.Message)
		}
	}
}

// checkRequiredFiles 確認必要的協議檔案存在
func checkRequiredFiles(ws *protocol.Workspace, featureID string, r *CheckResult) {
	dir := ws.FeatureDir(featureID)
	required := []string{protocol.StateFile}

	state, err := ws.ReadState(featureID)
	if err != nil {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("cannot read state.json: %v", err))
		return
	}

	// 依 active profile 決定哪些 role 的產出物為必要：profile 未啟用 designer 時不要求
	// task-brief/criteria；未啟用 tester 時不要求 tester 交付物。profile 無法解析時退回
	// 全部必要（向後相容，等同 full）。
	designerEnabled, testerEnabled := true, true
	if cfg, cfgErr := ws.ReadConfig(); cfgErr == nil {
		if feature, featErr := ws.LoadFeature(featureID); featErr == nil {
			if _, pc, perr := protocol.ResolveProfile(cfg, feature, state.Profile); perr == nil {
				designerEnabled = pc.EnablesRole(protocol.RoleDesigner)
				testerEnabled = pc.EnablesRole(protocol.RoleTester)
			}
		}
	}

	needsDesignOutputs := map[protocol.Phase]bool{
		protocol.PhaseCoding:        true,
		protocol.PhaseReviewing:     true,
		protocol.PhaseTesting:       true,
		protocol.PhaseAmending:      true,
		protocol.PhaseAccepting:     true,
		protocol.PhasePendingReview: true,
		protocol.PhaseDone:          true,
	}
	if needsDesignOutputs[state.Phase] && designerEnabled {
		required = append(required, protocol.TaskBrief, protocol.Criteria)
	}
	if testerEnabled && (state.Phase == protocol.PhaseAccepting || state.Phase == protocol.PhasePendingReview || state.Phase == protocol.PhaseDone) {
		roundDir := ws.RoundDir(featureID, state.Round)
		if _, err := os.Stat(roundDir); err == nil {
			checkTestingToAccepting(ws, featureID, state.Round, r)
		}
	}

	for _, f := range required {
		path := filepath.Join(dir, f)
		if missingOrEmpty(path) {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("required file missing: %s", f))
		}
	}
}

// CheckTestingToAccepting 驗證 Tester 交付物完整，供 testing 進 accepting 前使用。
func CheckTestingToAccepting(ws *protocol.Workspace, featureID string, round int) CheckResult {
	r := CheckResult{Pass: true}
	checkTestingToAccepting(ws, featureID, round, &r)
	return r
}

func checkTestingToAccepting(ws *protocol.Workspace, featureID string, round int, r *CheckResult) {
	roundDir := ws.RoundDir(featureID, round)
	featureDir := ws.FeatureDir(featureID)

	type pathLabel struct {
		path  string
		label string
	}
	required := []pathLabel{
		{filepath.Join(roundDir, protocol.VerifyFile), filepath.Join(protocol.RoundsDir, fmt.Sprintf("round-%d", round), protocol.VerifyFile)},
		{filepath.Join(roundDir, protocol.TestReport), filepath.Join(protocol.RoundsDir, fmt.Sprintf("round-%d", round), protocol.TestReport)},
		{filepath.Join(featureDir, protocol.FinalReport), protocol.FinalReport},
		{filepath.Join(featureDir, protocol.CommitPlan), protocol.CommitPlan},
	}
	for _, f := range required {
		if missingOrEmpty(f.path) {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("required file missing: %s", f.label))
		}
	}

	verifyPath := filepath.Join(roundDir, protocol.VerifyFile)
	data, err := os.ReadFile(verifyPath)
	if err != nil {
		r.Pass = false
		if os.IsNotExist(err) {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s not found — the tester likely could not run `4x verify` (check that 4x is in PATH)", protocol.VerifyFile))
		} else {
			r.Errors = append(r.Errors, fmt.Sprintf("cannot read %s: %v", protocol.VerifyFile, err))
		}
		return
	}
	var evidence protocol.VerifyEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("invalid verify.json: %v", err))
		return
	}
	if !evidence.Passed {
		r.Pass = false
		r.Errors = append(r.Errors, "verify.json did not pass")
	}

	// W7：交叉驗證 tester 自報的 passed 與各 command 實際 exit code。
	// passed==true 但任一 command 的 exit code 與預期不符視為謊報。
	// ExpectedExitCode 未設定時預設為 0（向後相容）。
	if evidence.Passed {
		for _, c := range evidence.Commands {
			expected := 0
			if c.ExpectedExitCode != nil {
				expected = *c.ExpectedExitCode
			}
			if c.ExitCode != expected {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf(
					"verify.json claims passed but command %q exited %d (expected %d)", c.Command, c.ExitCode, expected))
			}
		}
	}
}

func missingOrEmpty(path string) bool {
	info, err := os.Stat(path)
	return err != nil || info.IsDir() || info.Size() == 0
}

// checkBaseline 確認沒有 baseline 之前就存在的 dirty files 被混入
func checkBaseline(ws *protocol.Workspace, featureID string, r *CheckResult) {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	data, err := os.ReadFile(path)
	if err != nil {
		r.Warns = append(r.Warns, "no baseline.json found, skipping baseline check")
		return
	}

	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("invalid baseline.json: %v", err))
		return
	}

	for _, repo := range baseline.Repos {
		if len(repo.DirtyFiles) > 0 {
			r.Warns = append(r.Warns, fmt.Sprintf(
				"repo %s had dirty files at baseline: %s",
				repo.Name, strings.Join(repo.DirtyFiles, ", "),
			))
		}
	}
}

// checkScope 確認 diff 落在允許的 repo/path 內
func checkScope(ws *protocol.Workspace, featureID string, detector ScopeDetector, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot load feature YAML: %v", err))
		return
	}

	if len(feature.Repos) == 0 {
		return
	}

	allowedRepos := make(map[string]bool)
	for _, repo := range feature.Repos {
		allowedRepos[repo] = true
	}

	var changedRepos []string
	if detector != nil {
		changedRepos = detector.DetectChangedRepos(featureID)
	} else {
		changedRepos = detectChangedRepos(gitops.ScopeRoot(ws.Root, featureID))
	}
	for _, repo := range changedRepos {
		if !allowedRepos[repo] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("scope violation: repo %q not in feature repos", repo))
		}
	}
}

// detectChangedRepos 找出哪些子目錄有 uncommitted changes。
// 合併 tracked 變更（git diff HEAD，含 staged + unstaged）與 untracked 檔案
// （git ls-files --others --exclude-standard），後者是 git diff HEAD 涵蓋不到的缺口。
// 兩條指令各自獨立容錯：單一指令失敗不影響另一指令已找到的變更。
func detectChangedRepos(root string) []string {
	repoSet := make(map[string]bool)

	collect := func(out []byte) {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "/", 2)
			if len(parts) > 0 {
				repoSet[parts[0]] = true
			}
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
