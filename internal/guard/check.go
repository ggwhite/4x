package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/verify"
)

// CheckResult 是 `4x check` 的結果
type CheckResult struct {
	Pass   bool     `json:"pass"`
	Errors []string `json:"errors"`
	Warns  []string `json:"warnings"`
	// RetryableErrors 計數可透過重跑同一 role 修正的錯誤數。
	// 當 RetryableErrors == len(Errors) 且 > 0 時，所有錯誤都可重試修復。
	RetryableErrors int `json:"retryableErrors,omitempty"`
	// SelfModTouched 表示本輪變更觸及受保護路徑（self-mod guard）。
	SelfModTouched bool `json:"selfModTouched,omitempty"`
	// SelfModPaths 是觸及的受保護檔案路徑。
	SelfModPaths []string `json:"selfModPaths,omitempty"`
	// SelfModDiffLines 是受保護路徑變更的總行數（diff-budget 依此判斷）。
	SelfModDiffLines int `json:"selfModDiffLines,omitempty"`
}

// AllRetryable 回傳 true 如果所有錯誤都可透過重跑同一 role 修正。
func (r CheckResult) AllRetryable() bool {
	return !r.Pass && r.RetryableErrors > 0 && r.RetryableErrors == len(r.Errors)
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
	checkTaskBriefChallenge(ws, featureID, &r)
	checkBaseline(ws, featureID, &r)
	checkScope(ws, featureID, detector, &r)
	checkDocDeletion(ws, featureID, detector, &r)
	checkSelfMod(ws, featureID, detector, &r)
	checkDependencies(ws, featureID, &r)
	checkBacklogDrift(ws, featureID, &r)
	checkSymlinks(ws, featureID, &r)
	checkBuildGate(ws, featureID, &r)
	checkDocsGate(ws, featureID, &r)
	checkTestStrategyVerifyTypes(ws, featureID, &r)
	checkACChecksSchema(ws, featureID, &r)
	checkDesignerYAMLMod(ws, featureID, &r)

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
	designerEnabled, designReviewerEnabled, testerEnabled := true, false, true
	if cfg, cfgErr := ws.ReadConfig(); cfgErr == nil {
		if feature, featErr := ws.LoadFeature(featureID); featErr == nil {
			if _, pc, perr := protocol.ResolveProfile(cfg, feature, state.Profile); perr == nil {
				designerEnabled = pc.EnablesRole(protocol.RoleDesigner)
				designReviewerEnabled = state.Profile != "" && pc.EnablesRole(protocol.RoleDesignReviewer)
				testerEnabled = pc.EnablesRole(protocol.RoleTester)
			}
		}
	}

	needsDesignOutputs := map[protocol.Phase]bool{
		protocol.PhaseCoding:        true,
		protocol.PhaseReviewing:     true,
		protocol.PhaseTesting:       true,
		protocol.PhaseDeepReviewing: true,
		protocol.PhaseAmending:      true,
		protocol.PhaseAccepting:     true,
		protocol.PhasePendingReview: true,
		protocol.PhaseDone:          true,
	}
	if needsDesignOutputs[state.Phase] && designerEnabled {
		required = append(required, protocol.TaskBrief, protocol.Criteria)
	}
	if needsDesignOutputs[state.Phase] && designReviewerEnabled {
		required = append(required, protocol.DesignReviewReport)
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

// challengeHeading 是 task-brief 強制的前提挑戰段落標題（forcing function，見 F160 DR-1/DR-2）。
const challengeHeading = "## Premise Challenge"

// checkTaskBriefChallenge 對 task-brief.md 做「前提挑戰段落」的純結構檢查（DR-2）：
// 存在一行 trim 後完全等於 "## Premise Challenge" 的標題，且該標題之後、到下一個 "## " 標題
// （或檔尾）之間至少有一行非空白且非標題的內容。空段落（標題下直接接另一個 "## " 或檔尾）視為不通過。
//
// 此檢查刻意只驗結構、不判斷內容是否「真的是前提挑戰」、不呼叫 LLM。task-brief.md 不存在或為空時
// 直接 return（缺檔由 checkRequiredFiles 負責，避免重複報錯，DR-8）。違規累加 RetryableErrors
// （同輪 Designer 重跑即可補段落，比照 checkACChecksSchema 慣例）。
func checkTaskBriefChallenge(ws *protocol.Workspace, featureID string, r *CheckResult) {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.TaskBrief)
	if missingOrEmpty(path) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == challengeHeading {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: missing %q section (required forcing function — list at least one challenged premise, or state 無可挑戰前提 with a reason)",
			protocol.TaskBrief, challengeHeading))
		r.RetryableErrors++
		return
	}

	hasContent := false
	for _, line := range lines[headingIdx+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if trimmed != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: empty %q section — the heading must be followed by at least one non-empty line of content",
			protocol.TaskBrief, challengeHeading))
		r.RetryableErrors++
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
	// verify.json 的缺失/空/壞格式/未通過全部由下方讀檔區涵蓋，故不放進 required 迴圈，
	// 避免同一個缺失檔案被 required 迴圈與讀檔區各報一條 error。
	required := []pathLabel{
		{filepath.Join(roundDir, protocol.TestReport), filepath.Join(protocol.RoundsDir, fmt.Sprintf("round-%d", round), protocol.TestReport)},
		{filepath.Join(featureDir, protocol.FinalReport), protocol.FinalReport},
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

	ts, tsErr := ws.ReadTestStrategy(featureID)
	if tsErr != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("warning: failed to read test-strategy.yaml: %v (falling back to defaults)", tsErr))
	}
	checkACEvidence(ts, evidence, r)
	checkScreenshotRequirement(ws, ts, evidence, r)
	checkManualChecks(ts, evidence, r)
	checkSelfModTestGate(ws, featureID, r)
}

// checkTestStrategyVerifyTypes 在 test-strategy.yaml 存在時，驗證 ac_verify_map 的值都是合法的 verify_type。
// 這讓 Designer 跑完 4x check 時就能發現非法值，不用等到 Tester 階段才炸。
func checkTestStrategyVerifyTypes(ws *protocol.Workspace, featureID string, r *CheckResult) {
	ts, err := ws.ReadTestStrategy(featureID)
	if err != nil || len(ts.ACVerifyMap) == 0 {
		return
	}
	for acID, vt := range ts.ACVerifyMap {
		if _, valid := acVerifyTypes[vt]; !valid {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("test-strategy.yaml: ac_verify_map[%s] has invalid verify_type %q (valid: unit-test, integration, execution, inspection, skip, e2e-screenshot)", acID, vt))
		}
	}
}

// checkACChecksSchema 在 test-strategy.yaml 宣告 ac_checks 時（opt-in 執行式判定），
// 對每條 check 命令做假驗證 lint，並強制完整性（DR-3 presence-based 契約）：
//   - 未宣告 ac_checks（len==0）→ 立即 return（舊格式向後相容，不因缺 ac_checks 而擋）。
//   - 已宣告 → (a) lint 每條命令（空命令列 / LintACCheck 回非空即擋）；
//     (b) ac_verify_map 非空時，其每一條 execution 類（needsExec）AC 必須有非空 ac_checks
//     entry，缺者擋；inspection/skip 類豁免。ac_verify_map 為空時跳過完整性強制（DR-3 邊界）。
//
// 這讓 Designer 跑 4x check 當下就抓到假驗證與「宣告 ac_checks 卻漏綁 execution AC」，
// 不用拖到 testing。每個新增 error 比照既有分支 RetryableErrors++（Designer 同輪可修）。
func checkACChecksSchema(ws *protocol.Workspace, featureID string, r *CheckResult) {
	ts, err := ws.ReadTestStrategy(featureID)
	if err != nil || len(ts.ACChecks) == 0 {
		return
	}

	// (a) lint 每條命令。
	for acID, cmds := range ts.ACChecks {
		if len(cmds) == 0 {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("test-strategy.yaml: ac_checks[%s] has no commands", acID))
			r.RetryableErrors++
			continue
		}
		for _, cmd := range cmds {
			if reason := verify.LintACCheck(cmd); reason != "" {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("test-strategy.yaml: ac_checks[%s] command %q rejected: %s", acID, cmd, reason))
				r.RetryableErrors++
			}
		}
	}

	// (b) 完整性強制：ac_verify_map 非空時，每條 execution 類 AC 必須綁 ac_checks。
	if len(ts.ACVerifyMap) == 0 {
		return
	}
	for acID, vt := range ts.ACVerifyMap {
		vtype, valid := acVerifyTypes[vt]
		if !valid || !vtype.needsExec {
			continue
		}
		if len(ts.ACChecks[acID]) == 0 {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"test-strategy.yaml: AC %s (verify_type=%s) has no ac_checks entry — when ac_checks is declared, every execution-type AC must bind at least one executable check", acID, vt))
			r.RetryableErrors++
		}
	}
}

// acVerifyType 定義合法的 AC 驗證類型。needsExec 表示是否需要執行輸出作為 evidence。
type acVerifyType struct{ needsExec bool }

var acVerifyTypes = map[string]acVerifyType{
	"unit-test":      {needsExec: true},
	"integration":    {needsExec: true},
	"execution":      {needsExec: true},
	"inspection":     {needsExec: false},
	"skip":           {needsExec: false},
	"e2e-screenshot": {needsExec: false},
}

var executionPattern = regexp.MustCompile(`(\$\s|\bPASS\b|\bFAIL\b|^ok\s|--- |exit code|→|stdout|stderr|\d+\.\d+s)`)

// checkACEvidence 檢查 verify.json 的 per-AC evidence mapping：每個 AC 都必須 passed 且有 evidence。
// ac_results 為空時阻擋（舊格式 verify.json 不通過此檢查）。
// 若 test-strategy.yaml 有 ac_verify_map，依 verify_type 檢查 evidence 品質。
// verifyMap 為 nil 時只做基礎檢查（passed + evidence 非空），不做 execution pattern 檢查。
func checkACEvidence(ts protocol.TestStrategy, evidence protocol.VerifyEvidence, r *CheckResult) {
	if len(evidence.ACResults) == 0 {
		r.Pass = false
		r.Errors = append(r.Errors, "verify.json missing ac_results: every acceptance criterion must have evidence")
		r.RetryableErrors++
		return
	}

	verifyMap := ts.ACVerifyMap

	// ac_checks-bound AC 的權威判定不可靠 ac_results 是否被 Tester 保留：只要
	// test-strategy 宣告了某 AC 的 ac_checks，verify.json 就必須有對應 entry。
	// 若整筆缺失（被 Tester 刪除或手動編輯掉），直接擋下——否則刪 entry 即可繞過 exit-code 重算。
	resultByID := make(map[string]struct{}, len(evidence.ACResults))
	for _, ac := range evidence.ACResults {
		resultByID[ac.ID] = struct{}{}
	}
	for acID, cmds := range ts.ACChecks {
		if len(cmds) == 0 {
			continue
		}
		if _, ok := resultByID[acID]; !ok {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: has ac_checks but verify.json ac_results missing check results — do not hand-edit; re-run 4x verify", acID))
			r.RetryableErrors++
		}
	}

	// ac_checks-bound AC 以 exit code 重算為權威；任何一條重算失敗時，top-level
	// passed=true 即為謊報（手改 top-level 不可繞過 guard）——收集後統一檢查。
	var acChecksFailed []string

	for _, ac := range evidence.ACResults {
		// ac_checks-bound AC：以實際 exit code 為權威判定，豁免 prose executionPattern 檢查。
		if len(ts.ACChecks[ac.ID]) > 0 {
			if !checkACChecksConsistency(ts.ACChecks[ac.ID], ac, r) {
				acChecksFailed = append(acChecksFailed, ac.ID)
			}
			continue
		}
		vt := ""
		if verifyMap != nil {
			if v, ok := verifyMap[ac.ID]; ok {
				vt = v
			} else {
				vt = "execution" // map exists but AC not listed → strict default
			}
		}
		// verifyMap == nil → vt stays "" → basic checks only (backward compatible)

		if vt != "" {
			vtype, valid := acVerifyTypes[vt]
			if !valid {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("%s: invalid verify_type %q", ac.ID, vt))
				r.RetryableErrors++
				continue
			}
			if vt == "skip" {
				continue
			}

			if !ac.Passed {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("%s failed", ac.ID))
				r.RetryableErrors++
			}
			if len(ac.Evidence) == 0 {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("%s: no evidence", ac.ID))
				r.RetryableErrors++
				continue
			}
			if vtype.needsExec {
				hasExecEvidence := false
				for _, e := range ac.Evidence {
					if executionPattern.MatchString(e) {
						hasExecEvidence = true
						break
					}
				}
				if !hasExecEvidence {
					r.Pass = false
					r.Errors = append(r.Errors, fmt.Sprintf(
						"%s: verify_type=%s but evidence has no execution output (need command results, not code references)", ac.ID, vt))
					r.RetryableErrors++
				}
			}
		} else {
			if !ac.Passed {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("%s failed", ac.ID))
				r.RetryableErrors++
			}
			if len(ac.Evidence) == 0 {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("%s: no evidence", ac.ID))
				r.RetryableErrors++
			}
		}
	}

	if evidence.Passed && len(acChecksFailed) > 0 {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"verify.json claims passed=true but ac_checks recomputation failed for %s — top-level passed is recomputed by 4x verify, do not hand-edit",
			strings.Join(acChecksFailed, ", ")))
		r.RetryableErrors++
	}
}

// checkACChecksConsistency 對綁定 ac_checks 的單一 AC 以 exit code 為權威重算 passed，
// 回傳重算結果（true = 該 AC 實際通過）。這類 AC 有真 exit code，故豁免 prose
// executionPattern 檢查，但依序強制：
//   - checks 為空 → 擋（防 Tester 覆寫掉 CLI 寫的 Checks）；
//   - 記錄的 command 序列與 test-strategy.yaml 宣告的 ac_checks 不一致 → 擋
//     （SoT 比對，防整組換成 `true` 這類假命令）；
//   - ac.Passed 與重算不一致 → 擋（LLM 謊報偵測）；
//   - 重算失敗 → 擋（AC 未通過就是未通過，不因 passed 欄位一致而放行）；
//     ctx 逾時/取消（check 的 Error 非空）給明確訊息，而非籠統的 AC failed。
//
// 重算採 verify.ACChecksPassed 的全執行語意——skipped 條目不得充當通過。
func checkACChecksConsistency(expectedCmds []string, ac protocol.ACEvidence, r *CheckResult) bool {
	if len(ac.Checks) == 0 {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: has ac_checks but verify.json ac_results missing check results — do not hand-edit; re-run 4x verify", ac.ID))
		r.RetryableErrors++
		return false
	}

	mismatch := len(ac.Checks) != len(expectedCmds)
	if !mismatch {
		for i := range ac.Checks {
			if ac.Checks[i].Command != expectedCmds[i] {
				mismatch = true
				break
			}
		}
	}
	if mismatch {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: verify.json check commands do not match test-strategy.yaml ac_checks — do not hand-edit; re-run 4x verify", ac.ID))
		r.RetryableErrors++
		return false
	}

	recomputed := verify.ACChecksPassed(ac.Checks)
	if ac.Passed != recomputed {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: verify.json claims passed=%v but ac_checks exit codes say %v", ac.ID, ac.Passed, recomputed))
		r.RetryableErrors++
	}
	if !recomputed {
		r.Pass = false
		r.RetryableErrors++
		interrupted := false
		for _, c := range ac.Checks {
			if c.Error != "" && !c.Skipped && c.ExitCode != 0 {
				interrupted = true
				r.Errors = append(r.Errors, fmt.Sprintf(
					"%s: check %q did not finish (%s) — not a code failure; re-run 4x verify with a larger --timeout", ac.ID, c.Command, c.Error))
			}
		}
		if !interrupted {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: ac_checks failed — all checks must run and exit 0 (exit codes are authoritative)", ac.ID))
		}
	}
	return recomputed
}

// checkScreenshotRequirement 驗證宣告 verify_type=e2e-screenshot 的 AC 有留下截圖證據，
// 只檢查檔案是否存在（不比對內容），round 內任一截圖檔存在即視為所有此類 AC 皆有證據。
func checkScreenshotRequirement(ws *protocol.Workspace, ts protocol.TestStrategy, evidence protocol.VerifyEvidence, r *CheckResult) {
	resultMap := make(map[string]protocol.ACEvidence, len(evidence.ACResults))
	for _, ac := range evidence.ACResults {
		resultMap[ac.ID] = ac
	}
	for acID, vt := range ts.ACVerifyMap {
		if vt != "e2e-screenshot" {
			continue
		}
		if ac, ok := resultMap[acID]; !ok || !ac.Passed {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: verify_type=e2e-screenshot but AC missing or not passed in ac_results", acID))
			r.RetryableErrors++
			continue
		}
		if !hasExistingScreenshot(ws.Root, evidence.Screenshots) {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: verify_type=e2e-screenshot but no screenshot file found — tester must capture and record a screenshot, not skip", acID))
			r.RetryableErrors++
		}
	}
}

// hasExistingScreenshot 檢查截圖清單裡是否至少一個檔案真的存在於磁碟。
// NormalizeScreenshotPath 會剝掉 ".4x/" 前綴，故預設截圖目錄需依序嘗試 root 與
// root/.4x 兩個基準才能解析回正確位置（比照 internal/server/screenshots.go 慣例）。
func hasExistingScreenshot(root string, shots []feat.Screenshot) bool {
	for _, s := range shots {
		if filepath.IsAbs(s.Path) {
			if info, err := os.Stat(s.Path); err == nil && !info.IsDir() {
				return true
			}
			continue
		}
		normalized := feat.NormalizeScreenshotPath(s.Path)
		if normalized == "" {
			continue
		}
		candidates := []string{
			filepath.Join(root, normalized),
			filepath.Join(root, protocol.DirName, normalized),
		}
		for _, full := range candidates {
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

// checkManualChecks 驗證 test-strategy.yaml 的 manual_checks 全部有對應的執行結果。
// manual_checks 為空時靜默通過。
func checkManualChecks(ts protocol.TestStrategy, evidence protocol.VerifyEvidence, r *CheckResult) {
	if len(ts.ManualChecks) == 0 {
		return
	}
	resultMap := make(map[string]protocol.ManualCheckResult, len(evidence.ManualCheckResults))
	for _, mr := range evidence.ManualCheckResults {
		resultMap[mr.ID] = mr
	}
	for _, mc := range ts.ManualChecks {
		mr, ok := resultMap[mc.ID]
		if !ok {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"manual_check %q has no result in verify.json — tester must execute it, not skip", mc.ID))
			r.RetryableErrors++
			continue
		}
		if len(mr.Evidence) == 0 {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"manual_check %q has empty evidence — tester must record actual execution output", mc.ID))
			r.RetryableErrors++
		}
		if !mr.Passed {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("manual_check %q failed", mc.ID))
			r.RetryableErrors++
		}
	}
}

// checkSelfModTestGate 在 testing → accepting 閘門加上 self-mod test-gate：
// 若本 feature 觸及受保護路徑且 policy 要求測試，則受保護的 .go 變更必須附帶受保護的 _test.go 變更，
// 否則擋下（verify 本身是否通過已由上游邏輯涵蓋，此處不重複）。
func checkSelfModTestGate(ws *protocol.Workspace, featureID string, r *CheckResult) {
	s, err := ws.ReadState(featureID)
	if err != nil || !s.SelfModTouched {
		return
	}
	cfg, cfgErr := ws.ReadConfig()
	if cfgErr != nil {
		cfg = protocol.Config{}
	}
	policy := ResolveSelfMod(cfg)
	if !policy.RequireTests {
		return
	}
	if !HasAccompanyingTests(s.SelfModPaths, policy.ProtectedPaths) {
		r.Pass = false
		r.Errors = append(r.Errors,
			"self-mod: changes to protected paths require accompanying passing tests")
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
		// fail-closed：無法載入 feature 範圍時不可靜默放行，否則任何範圍外
		// 變更都會被略過（YAML 壞掉即等於關閉 scope guard）。
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("cannot load feature YAML for scope check: %v", err))
		return
	}

	if len(feature.Repos) == 0 {
		return
	}

	allowedRepos := make(map[string]bool)
	for _, repo := range feature.Repos {
		allowedRepos[repo] = true
	}

	// hub repo（EffectiveHubRepos）依設計是跨 feature 共用，永遠不算 scope violation，
	// 不論它是否列在 feature.Repos。ReadConfig 失敗時 hubRepos 維持空 map（fail-safe），
	// 不因讀 config 失敗而靜默放行所有 repo。
	hubRepos := make(map[string]bool)
	if cfg, cfgErr := ws.ReadConfig(); cfgErr == nil {
		for _, h := range protocol.EffectiveHubRepos(cfg) {
			hubRepos[h] = true
		}
	}

	// e2e repo（test-strategy.yaml e2e_repos）在 testing phase（含）之後的合法寫入放行，
	// 比照 hubRepos 的排除機制。讀 state 失敗時 phase 視為空字串（fail-safe → 一律不放行）；
	// 讀 test-strategy 失敗時 e2eRepoSet 維持空（fail-safe，比照 hubRepos 讀 config 失敗維持空 map
	// 的慣例，不因讀檔失敗而靜默放行）。
	var phase protocol.Phase
	if st, stErr := ws.ReadState(featureID); stErr == nil {
		phase = st.Phase
	}
	e2eRepoSet := make(map[string]bool)
	if ts, tsErr := ws.ReadTestStrategy(featureID); tsErr == nil {
		for _, repo := range ts.E2ERepos {
			e2eRepoSet[repo] = true
		}
	}
	// amending 在 state machine 中有 testing 前後兩類來源，純 phase 名稱無法區分；
	// 以「本 run 是否已跑過 testing」（任一 round 有 verify.json）作為額外閘門（見 DR-4）。
	e2eAllowed := e2eAllowedPhase(phase) ||
		(phase == protocol.PhaseAmending && testingHasRun(ws, featureID))

	var changedRepos []string
	if detector != nil {
		changedRepos = detector.DetectChangedRepos(featureID)
	} else {
		changedRepos = detectChangedRepos(gitops.ScopeRoot(ws.Root, featureID))
	}
	for _, repo := range changedRepos {
		if hubRepos[repo] {
			continue
		}
		if e2eAllowed && e2eRepoSet[repo] {
			continue
		}
		if !allowedRepos[repo] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("scope violation: repo %q not in feature repos", repo))
		}
	}
}

// checkDocDeletion 偵測 uncommitted diff 中對受保護路徑（預設 docs/，可由 settings.json 的
// doc_guard.protected_paths 覆寫）的純刪除，並標為 hard failure（DR-7：不累加 RetryableErrors，
// 比照 checkScope 的邊界違規語意）。
//
// diff 窗口沿用既有 checkScope/self-mod 的 uncommitted `git diff HEAD`（DR-4），不用 merge-base；
// 「已 commit 的刪除」不在涵蓋範圍。用 `git diff --diff-filter=D --find-renames --name-only HEAD` 取
// 純刪除清單：--find-renames 把搬移重分類為 R、--diff-filter=D 只留 D，故 rename/move（含 docs 內
// 歸檔搬移）自然被排除（DR-5）。ReadConfig 失敗時用零值 Config → 套預設保護路徑（fail-safe 仍保護 docs/）。
// detector 參數為與 sibling guard 簽名對齊而保留；本檢查直接對 scope root 跑 git，不經 detector。
//
// 無條件執行（不綁 phase，DR-7）：無 uncommitted 刪除時為 no-op，故在任何 phase 呼叫皆安全。
func checkDocDeletion(ws *protocol.Workspace, featureID string, detector ScopeDetector, r *CheckResult) {
	cfg, err := ws.ReadConfig()
	if err != nil {
		cfg = protocol.Config{}
	}
	protectedPaths := ResolveDocProtectedPaths(cfg)

	for _, root := range gitops.ScopeRoots(ws.Root, featureID) {
		cmd := exec.Command("git", "diff", "--diff-filter=D", "--find-renames", "--name-only", "HEAD")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path := strings.TrimSpace(line)
			if path == "" {
				continue
			}
			if MatchProtected(path, protectedPaths) {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf(
					"doc-deletion violation: protected doc %q was deleted (archive/rename instead of deleting)", path))
			}
		}
	}
}

// e2eAllowedPhase 回報 phase 是否為 testing（含）之後、且不需額外判斷即可放行 e2e 產出的 phase。
// 這些 phase 只會在 Tester 跑過 testing 後才可能到達（deep-reviewing/fixing/accepting 依 state machine
// 皆在 testing 之後），故對宣告的 e2e repo 放行；coding/reviewing/designing 等 testing 之前的 phase 回 false。
// 注意：amending 不在此清單（它 testing 前後皆可達），由 checkScope 另以 testingHasRun 區分（見 DR-4）。
func e2eAllowedPhase(p protocol.Phase) bool {
	switch p {
	case protocol.PhaseTesting,
		protocol.PhaseDeepReviewing,
		protocol.PhaseFixing,
		protocol.PhaseAccepting,
		protocol.PhasePendingReview,
		protocol.PhaseDone:
		return true
	default:
		return false
	}
}

// testingHasRun 回報本 feature 的 run 中是否已有任一 round 產出 verify.json，
// 作為「Tester 已跑過 testing」的單調痕跡；掃描 ws.FeatureDir(featureID)/RoundsDir 下
// 任一 round-{n}/verify.json（protocol.VerifyFile）是否存在。讀目錄失敗或不存在時回 false（fail-safe）。
func testingHasRun(ws *protocol.Workspace, featureID string) bool {
	roundsDir := filepath.Join(ws.FeatureDir(featureID), protocol.RoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "round-") {
			continue
		}
		verifyPath := filepath.Join(roundsDir, e.Name(), protocol.VerifyFile)
		if _, err := os.Stat(verifyPath); err == nil {
			return true
		}
	}
	return false
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
			// 根目錄檔案（go.mod、Makefile 等，路徑無 "/"）不是 repo，
			// 不可當成 repo 名稱比對，否則會誤判 scope violation。
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

// isCommandMissing 判斷 vc 是否因指令本身不存在（而非指令執行後的真實失敗）而導致
// exit 127。build-gate 常見於某個 sub-repo root 沒安裝該語言/工具鏈（如 node_modules
// 未在該 root 裝），此時應優雅降級而非整體判 FAIL。
//
// 訊息比對故意只認寬鬆的 "not found"，不要求 "command not found"：executeCommand
// 用 `sh -c` 執行，不同 shell 對 exit 127 的措辭不同——macOS 的 /bin/sh（bash 相容）
// 印 "sh: cmd: command not found"，而 Linux（多數 CI，如 Ubuntu runner）的 /bin/sh 是
// dash，印的是 "sh: 1: cmd: not found"（沒有 "command" 字樣）。要求 "command not
// found" 在 dash 上永遠比對失敗，導致這個降級邏輯在 Linux CI 上完全失效。exit 127
// 已經是強訊號（shell 保留給「指令不存在」的慣例 exit code），故只需比對兩種 shell
// 共有的 "not found" 子字串即可，不會誤吃其他真實失敗的 exit 127。
func isCommandMissing(vc protocol.VerifyCommand) bool {
	if vc.ExitCode != 127 {
		return false
	}
	summary := strings.ToLower(vc.Summary)
	return strings.Contains(summary, "not found") || strings.Contains(summary, "no such file or directory")
}

// runGroupsAcrossRoots 對 roots 逐一執行 verify.RunGroups 並合併證據；multi-repo
// feature 的 roots 有多個 sub-repo worktree，須各自跑一輪 build/lint 才能涵蓋全部範圍。
// roots 只有一個元素時（mono-repo 恆常情況）等同直接呼叫 verify.RunGroups，行為不變。
//
// 對 isCommandMissing 的 command 做優雅降級：標記 Skipped 並排除於 pass/fail 判定，
// 同時回傳可讀的 warn 訊息供呼叫端記錄。build-gate group 的指令順序是 Build 在前、
// Lint 在後（見 verify.BuildGateGroups），runGroup 只會在某指令失敗時把「之後」的指令
// 標記 Skipped（ExitCode 維持零值），不會誤觸 isCommandMissing（要求 ExitCode==127），
// 故這裡的 post-processing 足以完整覆蓋，不會漏掉排在降級指令之後的其他指令。
//
// 已知前提／限制：本降級只處理「指令自身缺失（127）」。若排在最前的指令因缺失被降級，
// 其後的指令早已被 runGroup 以「前序失敗」語意標成 Skipped（ExitCode 零值）而根本沒執行，
// 此處會連帶視為 pass。實務上 build-gate 的 Build/Lint 通常共用同一 make/工具鏈，工具缺失
// 時兩者會一起 127 被降級；唯有「Build 與 Lint 工具鏈相異且僅 Build 缺失」的特殊佈局下，
// Lint 的真實失敗才可能被此連帶 skip 靜默遮蔽。目前 4x 目標專案未見此佈局，故不改變降級
// 語意；若日後出現，需改讓 build-gate 對 Build/Lint 使用獨立 group、互不連帶 skip。
func runGroupsAcrossRoots(ctx context.Context, groups []verify.Group, roots []string) (protocol.VerifyEvidence, []string) {
	merged := protocol.VerifyEvidence{Passed: true}
	var warns []string
	for _, root := range roots {
		// DR-5：build-gate/docs-gate 命令來自 settings.json（人工可信），刻意傳 nil allowlist
		// 不強制 verify allowlist，避免使用者設窄清單時意外擋掉自己的 build/docs gate。
		evidence := verify.RunGroups(ctx, groups, root, nil)
		for i := range evidence.Commands {
			cmd := &evidence.Commands[i]
			if isCommandMissing(*cmd) {
				cmd.Skipped = true
				warns = append(warns, fmt.Sprintf("build-gate: %q not found on root %s, skipped", cmd.Command, root))
			}
		}
		merged.Commands = append(merged.Commands, evidence.Commands...)
		if !verify.CommandsPassed(evidence.Commands) {
			merged.Passed = false
		}
	}
	return merged, warns
}

// checkBuildGate 在 coding/amending phase 時執行 settings.json 的 build + lint 指令，
// 結果寫入 build-gate.json。非 coding/amending phase 時不執行。
func checkBuildGate(ws *protocol.Workspace, featureID string, r *CheckResult) {
	state, err := ws.ReadState(featureID)
	if err != nil {
		return
	}
	if state.Phase != protocol.PhaseCoding && state.Phase != protocol.PhaseAmending {
		return
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: cannot read settings.json: %v", err))
		return
	}
	groups, err := verify.BuildGateGroups(cfg.Project)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: %v", err))
		return
	}

	roundDir := ws.RoundDir(featureID, state.Round)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: cannot create round dir: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	evidence, warns := runGroupsAcrossRoots(ctx, groups, gitops.ScopeRoots(ws.Root, featureID))
	r.Warns = append(r.Warns, warns...)
	evidence.Round = state.Round
	evidence.Role = protocol.RoleCoder

	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: marshal error: %v", err))
		return
	}
	outPath := filepath.Join(roundDir, protocol.BuildGateFile)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: write error: %v", err))
		return
	}

	if !evidence.Passed {
		r.Pass = false
		var failedCmds []string
		for _, cmd := range evidence.Commands {
			if cmd.ExitCode != 0 && !cmd.Skipped {
				failedCmds = append(failedCmds, fmt.Sprintf("%s (exit %d): %s", cmd.Command, cmd.ExitCode, cmd.Summary))
			}
		}
		r.Errors = append(r.Errors, fmt.Sprintf("build-gate failed: %s", strings.Join(failedCmds, "; ")))
	}
}

// checkDocsGate 在 coding/amending phase 執行 settings.json 的 docs_check 指令
// （如 make check-docs-sync、check-i18n），結果寫入 docs-gate.json。
//
// 與 build-gate 不同，docs-gate 為「非阻塞」：驗證失敗只記 WARN、不設 r.Pass=false，
// 以符合 F136 的「不阻塞 coder 主要工作流」約束。其價值在於產出框架權威 artifact——
// 現況是 coder 常憑記憶在 coder-report 寫「check-docs-sync OK」而非真實輸出，reviewer
// 得親自重跑才發現 NEEDS_UPDATE、多耗一輪；docs-gate 讓框架在 coding 階段就跑出可信結果，
// coder 於 4x check 輸出即看到 WARN、reviewer 也可直接讀 artifact 而不必重跑。
//
// docs_check 未設定時完全跳過（不記 warn），確保非 4x 專案（無此類指令）不受影響。
func checkDocsGate(ws *protocol.Workspace, featureID string, r *CheckResult) {
	state, err := ws.ReadState(featureID)
	if err != nil {
		return
	}
	if state.Phase != protocol.PhaseCoding && state.Phase != protocol.PhaseAmending {
		return
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("docs-gate: cannot read settings.json: %v", err))
		return
	}
	groups, err := verify.DocsGateGroups(cfg.Project)
	if err != nil {
		// docs_check 未設定屬正常（opt-in），靜默跳過不記 warn。
		return
	}

	roundDir := ws.RoundDir(featureID, state.Round)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("docs-gate: cannot create round dir: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	evidence, warns := runGroupsAcrossRoots(ctx, groups, gitops.ScopeRoots(ws.Root, featureID))
	r.Warns = append(r.Warns, warns...)
	evidence.Round = state.Round
	evidence.Role = protocol.RoleCoder

	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("docs-gate: marshal error: %v", err))
		return
	}
	outPath := filepath.Join(roundDir, protocol.DocsGateFile)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("docs-gate: write error: %v", err))
		return
	}

	if !evidence.Passed {
		var failedCmds []string
		for _, cmd := range evidence.Commands {
			if cmd.ExitCode != 0 && !cmd.Skipped {
				failedCmds = append(failedCmds, fmt.Sprintf("%s (exit %d)", cmd.Command, cmd.ExitCode))
			}
		}
		r.Warns = append(r.Warns, fmt.Sprintf("docs-gate: docs/i18n verification reported issues (see %s), fix before finishing: %s", protocol.DocsGateFile, strings.Join(failedCmds, "; ")))
	}
}

// checkSymlinks 掃描 feature 變更中是否包含 symlink。
// coder 使用 git add . 時容易把 node_modules symlink 等意外加入，此檢查在 guardrail
// 階段攔截：掃 uncommitted diff + untracked 的 Lstat，以及已 staged/committed 的
// git ls-files -s mode 120000。
func checkSymlinks(ws *protocol.Workspace, featureID string, r *CheckResult) {
	roots := gitops.ScopeRoots(ws.Root, featureID)
	multi := len(roots) > 1
	seen := make(map[string]bool)
	var symlinks []string

	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			symlinks = append(symlinks, path)
		}
	}

	for _, root := range roots {
		prefix := ""
		if multi {
			prefix = filepath.Base(root) + "/"
		}

		scanLstat := func(out []byte) {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line == "" {
					continue
				}
				info, err := os.Lstat(filepath.Join(root, line))
				if err != nil {
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					add(prefix + line)
				}
			}
		}

		diffCmd := exec.Command("git", "diff", "--name-only", "HEAD")
		diffCmd.Dir = root
		if out, err := diffCmd.Output(); err == nil {
			scanLstat(out)
		}

		untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
		untrackedCmd.Dir = root
		if out, err := untrackedCmd.Output(); err == nil {
			scanLstat(out)
		}

		lsCmd := exec.Command("git", "ls-files", "-s")
		lsCmd.Dir = root
		if out, err := lsCmd.Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.HasPrefix(line, "120000 ") {
					parts := strings.SplitN(line, "\t", 2)
					if len(parts) == 2 {
						add(prefix + parts[1])
					}
				}
			}
		}
	}

	if len(symlinks) > 0 {
		detail := strings.Join(symlinks, ", ")
		if len(detail) > 200 {
			detail = fmt.Sprintf("%s ... (%d total)", detail[:200], len(symlinks))
		}
		r.Warns = append(r.Warns, fmt.Sprintf(
			"symlinks detected in git: %s — verify these are intentional, not accidental (e.g. node_modules)", detail))
	}
}
