package orchestrator

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// VerifyStatus 表示 verify.json 的檢查結果
type VerifyStatus int

const (
	VerifyMissing VerifyStatus = iota
	VerifyFailed
	VerifyOK
)

// ReviewPassed 檢查指定 round 的 review report 是否通過（PASS 且無 critical issue）
func ReviewPassed(ws *protocol.Workspace, featureID string, round int, reportFile string) bool {
	roundDir := ws.RoundDir(featureID, round)
	return ReviewPassedAtPath(filepath.Join(roundDir, reportFile))
}

// ReviewPassedAtPath 檢查指定路徑的 review report 是否通過
func ReviewPassedAtPath(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	result := ParseReviewVerdict(string(data))
	// CONDITIONAL PASS（有 warning 但無 critical）視為通過，不觸發 amending。
	return result.Passed && result.CriticalCount == 0
}

// ParseReviewVerdict 從 review-report.md 擷取 verdict 與 critical/warning issue 計數
func ParseReviewVerdict(content string) protocol.ReviewResult {
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
			clean := strings.ToUpper(strings.Trim(trimmed, "*_"))
			if strings.HasPrefix(clean, "PASS") || strings.HasPrefix(clean, "CONDITIONAL PASS") {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
}

// ReviewFailCount 回傳指定 round 的 review-report.md 失敗計數（critical + warning）。
// 供 run loop 判斷連續輪次是否有進展（失敗數下降）使用；report 不存在時回 0。
func ReviewFailCount(ws *protocol.Workspace, featureID string, round int) int {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.ReviewReport))
	if err != nil {
		return 0
	}
	result := ParseReviewVerdict(string(data))
	return result.CriticalCount + result.WarningCount
}

// FinalReportPassed 讀取 final-report.md 並檢查 ## Status 段落是否為 ready-for-review。
// 檔案不存在或 Status 為 needs-attention 時回傳 false。
func FinalReportPassed(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	inStatus := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Status") {
			inStatus = true
			continue
		}
		if inStatus && trimmed != "" {
			return strings.HasPrefix(strings.ToLower(trimmed), "ready-for-review")
		}
		if inStatus && strings.HasPrefix(trimmed, "## ") {
			break
		}
	}
	return false
}

// CheckVerify 檢查 verify.json 狀態：VerifyMissing（檔案不存在或無法解析）、VerifyFailed（passed=false）、VerifyOK（passed=true）
func CheckVerify(ws *protocol.Workspace, featureID string, round int) VerifyStatus {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return VerifyMissing
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil {
		return VerifyMissing
	}
	if !ve.Passed {
		return VerifyFailed
	}
	for _, ac := range ve.ACResults {
		if !ac.Passed {
			return VerifyFailed
		}
	}
	return VerifyOK
}

// CleanStaleArtifact 只清除當前 phase 的「半成品」output artifact。
// resume 場景下（SIGKILL、runner-error、crash），runner 可能寫了不完整的 report，
// 若不清除，NextPhaseAfter 會誤認 phase 完成而跳過該步驟。
func CleanStaleArtifact(ws *protocol.Workspace, featureID string, phase protocol.Phase, round int) {
	roundDir := ws.RoundDir(featureID, round)
	switch phase {
	case protocol.PhaseCoding, protocol.PhaseAmending:
		RemoveIfIncomplete(filepath.Join(roundDir, protocol.CoderReport), CoderReportComplete)
	case protocol.PhaseReviewing:
		RemoveIfIncomplete(filepath.Join(roundDir, protocol.ReviewReport), ReviewReportComplete)
	case protocol.PhaseDesignReviewing:
		RemoveIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport), ReviewReportComplete)
	case protocol.PhaseTesting:
		verifyPath := filepath.Join(roundDir, protocol.VerifyFile)
		if VerifyEvidenceComplete(verifyPath) {
			return
		}
		os.Remove(filepath.Join(roundDir, protocol.TestReport))
		os.Remove(verifyPath)
	case protocol.PhaseDeepReviewing:
		RemoveIfIncomplete(filepath.Join(roundDir, protocol.DeepReviewReport), ReviewReportComplete)
	case protocol.PhaseFixing:
		RemoveIfIncomplete(filepath.Join(roundDir, protocol.FixerReport), CoderReportComplete)
	case protocol.PhaseAccepting:
		RemoveIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport), NonEmptyFile)
	default:
		// 其他 phase 無 output artifact 需清除
	}
}

// RemoveIfIncomplete 在 path 指向的 artifact 不完整時移除它（讓該步驟重跑）；完整則原樣保留
func RemoveIfIncomplete(path string, complete func(string) bool) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	if complete(path) {
		return
	}
	os.Remove(path)
}

// CoderReportComplete 判斷 coder-report.md 是否完整：非空且含 `## Verification`
func CoderReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Verification")
}

// ReviewReportComplete 判斷 review / deep-review report 是否完整：非空且含可解析的 `## Verdict` 區段
func ReviewReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return ReportHasVerdict(string(data))
}

// VerifyEvidenceComplete 判斷 verify.json 是否完整：可成功 unmarshal 成 protocol.VerifyEvidence
func VerifyEvidenceComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var ve protocol.VerifyEvidence
	return json.Unmarshal(data, &ve) == nil
}

// NonEmptyFile 判斷 path 指向的檔案是否存在且去除空白後非空
func NonEmptyFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(data)) > 0
}

// ReportHasVerdict 判斷 review-report 內容是否含可解析的 `## Verdict` 區段
func ReportHasVerdict(content string) bool {
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
