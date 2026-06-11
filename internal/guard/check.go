package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// CheckResult 是 `4x check` 的結果
type CheckResult struct {
	Pass   bool     `json:"pass"`
	Errors []string `json:"errors"`
	Warns  []string `json:"warnings"`
}

// Check 執行所有 guardrail 檢查
func Check(ws *protocol.Workspace, featureID string) CheckResult {
	r := CheckResult{Pass: true}

	checkRequiredFiles(ws, featureID, &r)
	checkBaseline(ws, featureID, &r)
	checkScope(ws, featureID, &r)
	checkBacklogDrift(ws, featureID, &r)

	return r
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

	needsDesignOutputs := map[protocol.Phase]bool{
		protocol.PhaseCoding:    true,
		protocol.PhaseReviewing: true,
		protocol.PhaseTesting:   true,
		protocol.PhaseAmending:  true,
		protocol.PhaseAccepting: true,
		protocol.PhaseDone:      true,
	}
	if needsDesignOutputs[state.Phase] {
		required = append(required, protocol.TaskBrief, protocol.Criteria)
	}
	if state.Phase == protocol.PhaseAccepting || state.Phase == protocol.PhaseDone {
		checkTestingToAccepting(ws, featureID, state.Round, r)
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

	required := map[string]string{
		filepath.Join(roundDir, protocol.VerifyFile):    filepath.Join(protocol.RoundsDir, fmt.Sprintf("round-%d", round), protocol.VerifyFile),
		filepath.Join(roundDir, protocol.TestReport):    filepath.Join(protocol.RoundsDir, fmt.Sprintf("round-%d", round), protocol.TestReport),
		filepath.Join(featureDir, protocol.FinalReport): protocol.FinalReport,
		filepath.Join(featureDir, protocol.CommitPlan):  protocol.CommitPlan,
	}
	for path, label := range required {
		if missingOrEmpty(path) {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("required file missing: %s", label))
		}
	}

	verifyPath := filepath.Join(roundDir, protocol.VerifyFile)
	data, err := os.ReadFile(verifyPath)
	if err != nil {
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
func checkScope(ws *protocol.Workspace, featureID string, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot load feature YAML: %v", err))
		return
	}

	if len(feature.Repos) == 0 {
		return
	}

	allowedRepos := make(map[string]bool)
	for repo := range feature.Repos {
		allowedRepos[repo] = true
	}

	changedRepos := detectChangedRepos(ws.Root)
	for _, repo := range changedRepos {
		if !allowedRepos[repo] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("scope violation: repo %q not in feature repos", repo))
		}
	}
}

// detectChangedRepos 找出哪些子目錄有 uncommitted changes
func detectChangedRepos(root string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = root
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

// CaptureBaseline 捕獲所有 repo 的 HEAD snapshot
func CaptureBaseline(ws *protocol.Workspace, featureID string, repoPaths []string) error {
	baseline := protocol.Baseline{}

	for _, repoPath := range repoPaths {
		fullPath := filepath.Join(ws.Root, repoPath)
		if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
			continue
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

		baseline.Repos = append(baseline.Repos, protocol.BaselineRepo{
			Name:       repoPath,
			Path:       repoPath,
			Branch:     branch,
			Head:       head,
			DirtyFiles: dirty,
		})
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
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
