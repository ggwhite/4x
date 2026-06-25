package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

func reviewPassed(ws *protocol.Workspace, featureID string, round int, reportFile string) bool {
	roundDir := ws.RoundDir(featureID, round)
	return reviewPassedAtPath(filepath.Join(roundDir, reportFile))
}

func reviewPassedAtPath(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	result := parseReviewVerdict(string(data))
	// CONDITIONAL PASS（有 warning 但無 critical）視為通過，不觸發 amending。
	// Warning 級別的問題不值得整輪 coder→reviewer→tester 重跑。
	return result.Passed && result.CriticalCount == 0
}

// parseReviewVerdict 從 review-report.md 擷取 verdict 與 critical/warning issue 計數
func parseReviewVerdict(content string) protocol.ReviewResult {
	lines := strings.Split(content, "\n")
	var result protocol.ReviewResult
	inVerdict := false
	verdictFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		// 只計行首的 issue tag（### [WARNING] 或 [WARNING] 開頭），
		// 避免把正文中引述上一輪 issue 的文字誤計為本輪 issue。
		if strings.HasPrefix(upper, "[CRITICAL]") || strings.HasPrefix(upper, "### [CRITICAL]") ||
			strings.HasPrefix(upper, "#### [CRITICAL]") {
			result.CriticalCount++
		}
		if strings.HasPrefix(upper, "[WARNING]") || strings.HasPrefix(upper, "### [WARNING]") ||
			strings.HasPrefix(upper, "#### [WARNING]") {
			result.WarningCount++
		}

		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && !verdictFound && trimmed != "" {
			// strip markdown bold/italic（**PASS** → PASS）
			clean := strings.ToUpper(strings.Trim(trimmed, "*_"))
			if strings.HasPrefix(clean, "PASS") || strings.HasPrefix(clean, "CONDITIONAL PASS") {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
}

// reviewFailCount 回傳指定 round 的 review-report.md 失敗計數（critical + warning）。
// 供 run loop 判斷連續輪次是否有進展（失敗數下降）使用；report 不存在時回 0。
func reviewFailCount(ws *protocol.Workspace, featureID string, round int) int {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.ReviewReport))
	if err != nil {
		return 0
	}
	result := parseReviewVerdict(string(data))
	return result.CriticalCount + result.WarningCount
}

type verifyStatus int

const (
	verifyMissing verifyStatus = iota
	verifyFailed
	verifyOK
)

// checkVerify 檢查 verify.json 狀態：missing（檔案不存在或無法解析）、failed（passed=false）、ok（passed=true）
func checkVerify(ws *protocol.Workspace, featureID string, round int) verifyStatus {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return verifyMissing
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil {
		return verifyMissing
	}
	if !ve.Passed {
		return verifyFailed
	}
	return verifyOK
}

// cleanStaleArtifact 只清除當前 phase 的「半成品」output artifact。
// resume 場景下（SIGKILL、runner-error、crash），runner 可能寫了不完整的 report，
// 若不清除，nextPhaseAfter 會誤認 phase 完成而跳過該步驟。
// 反之，已完成的 report 一律保留——crash 重啟不得讓前一輪已驗收的產出消失。
// 完整性判準依 artifact 種類，由各 *Complete helper 判斷。
func cleanStaleArtifact(ws *protocol.Workspace, featureID string, phase protocol.Phase, round int) {
	roundDir := ws.RoundDir(featureID, round)
	switch phase {
	case protocol.PhaseCoding, protocol.PhaseAmending:
		removeIfIncomplete(filepath.Join(roundDir, protocol.CoderReport), coderReportComplete)
	case protocol.PhaseReviewing:
		removeIfIncomplete(filepath.Join(roundDir, protocol.ReviewReport), reviewReportComplete)
	case protocol.PhaseDesignReviewing:
		removeIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport), reviewReportComplete)
	case protocol.PhaseTesting:
		// test-report 與 verify.json 成對；verify.json 可解析即視為該 phase 完整。
		verifyPath := filepath.Join(roundDir, protocol.VerifyFile)
		if verifyEvidenceComplete(verifyPath) {
			return
		}
		os.Remove(filepath.Join(roundDir, protocol.TestReport))
		os.Remove(verifyPath)
	case protocol.PhaseDeepReviewing:
		removeIfIncomplete(filepath.Join(roundDir, protocol.DeepReviewReport), reviewReportComplete)
	case protocol.PhaseAccepting:
		removeIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport), nonEmptyFile)
	}
}

// removeIfIncomplete 在 path 指向的 artifact 不完整時移除它（讓該步驟重跑）；完整則原樣保留。
// complete 回傳該檔是否為完整產出。檔案不存在則無事可做。
func removeIfIncomplete(path string, complete func(string) bool) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	if complete(path) {
		return
	}
	os.Remove(path)
}

// coderReportComplete 判斷 coder-report.md 是否完整：非空且含 template 終止區段標記 `## Verification`。
func coderReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Verification")
}

// reviewReportComplete 判斷 review / deep-review report 是否完整：非空且含可解析的 `## Verdict` 區段。
func reviewReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return reportHasVerdict(string(data))
}

// verifyEvidenceComplete 判斷 verify.json 是否完整：可成功 unmarshal 成 protocol.VerifyEvidence
// （與 verifyPassed 相同的解析路徑，但不要求 passed=true——FAIL 的 evidence 同樣是完整產出）。
func verifyEvidenceComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var ve protocol.VerifyEvidence
	return json.Unmarshal(data, &ve) == nil
}

// nonEmptyFile 判斷 path 指向的檔案是否存在且去除空白後非空。
func nonEmptyFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(data)) > 0
}

// reportHasVerdict 判斷 review-report 內容是否含可解析的 `## Verdict` 區段。
// 與 parseReviewVerdict 的掃描方式一致：在 `## Verdict` header 後找到首個非空行即算辨識成功。
func reportHasVerdict(content string) bool {
	lines := strings.Split(content, "\n")
	inVerdict := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && trimmed != "" {
			return true
		}
	}
	return false
}
