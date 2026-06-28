package orchestrator

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// NextPhaseAfter 根據目前 phase 和 artifacts 決定下一個 phase，第三個回傳值為 escalation 停止原因
func NextPhaseAfter(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.Phase, protocol.Role, string) {
	switch s.Phase {
	case protocol.PhaseDesigning:
		brief := filepath.Join(ws.FeatureDir(featureID), protocol.TaskBrief)
		if _, err := os.Stat(brief); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.TaskBrief
		}
		criteria := filepath.Join(ws.FeatureDir(featureID), protocol.Criteria)
		if _, err := os.Stat(criteria); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.Criteria
		}
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer, ""

	case protocol.PhaseDesignReviewing:
		report := filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DesignReviewReport
		}
		if ReviewPassedAtPath(report) {
			return protocol.PhaseCoding, protocol.RoleCoder, ""
		}
		return protocol.PhaseDesigning, protocol.RoleDesigner, ""

	case protocol.PhaseCoding, protocol.PhaseAmending:
		if esc := ReadEscalation(ws, featureID, s.Round); esc.Needed {
			if IsDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.CoderReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.CoderReport
		}
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""

	case protocol.PhaseReviewing:
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.ReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.ReviewReport
		}
		if ReviewPassed(ws, featureID, s.Round, protocol.ReviewReport) {
			return protocol.PhaseTesting, protocol.RoleTester, ""
		}
		return protocol.PhaseAmending, protocol.RoleCoder, ""

	case protocol.PhaseTesting:
		if esc := ReadEscalation(ws, featureID, s.Round); esc.Needed {
			if IsDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
		if result.Pass {
			return protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer, ""
		}
		if vs := CheckVerify(ws, featureID, s.Round); vs != VerifyOK {
			if ReviewPassed(ws, featureID, s.Round, protocol.TestReport) {
				msg := "verify.json missing but test-report verdict is PASS — tester likely could not run `4x verify`"
				if vs == VerifyFailed {
					msg = "verify.json passed=false but test-report verdict is PASS — review the failing verify commands"
				}
				return protocol.PhaseNeedsAttention, "", msg
			}
			return protocol.PhaseAmending, protocol.RoleCoder, ""
		}
		return protocol.PhaseNeedsAttention, "", strings.Join(result.Errors, "; ")

	case protocol.PhaseDeepReviewing:
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.DeepReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DeepReviewReport
		}
		if ReviewPassed(ws, featureID, s.Round, protocol.DeepReviewReport) {
			return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
		}
		return protocol.PhaseNeedsAttention, "", "deep-review self-heal exhausted"

	case protocol.PhaseAccepting:
		report := filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.FinalReport
		}
		if !FinalReportPassed(report) {
			return protocol.PhaseNeedsAttention, "", "final-report: needs-attention"
		}
		return protocol.PhasePendingReview, "", ""

	default:
		return protocol.PhaseDone, "", ""
	}
}

// SuccessorPhase 回傳成功路徑上的下一個 phase 與其 role，皆為合法 state 邊。
// 用於 pass-through 跳過未啟用的 role；pending-review 的 role 為空字串。
func SuccessorPhase(p protocol.Phase) (protocol.Phase, protocol.Role) {
	switch p {
	case protocol.PhaseDesigning:
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer
	case protocol.PhaseDesignReviewing:
		return protocol.PhaseCoding, protocol.RoleCoder
	case protocol.PhaseCoding:
		return protocol.PhaseReviewing, protocol.RoleReviewer
	case protocol.PhaseReviewing:
		return protocol.PhaseTesting, protocol.RoleTester
	case protocol.PhaseTesting:
		return protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer
	case protocol.PhaseDeepReviewing:
		return protocol.PhaseAccepting, protocol.RoleAcceptor
	case protocol.PhaseAccepting:
		return protocol.PhasePendingReview, ""
	default:
		return p, state.PhaseToRole(p)
	}
}

// IsTerminalPhase 判斷 phase 是否為終止狀態（主迴圈應 break）
func IsTerminalPhase(phase protocol.Phase) bool {
	return phase == protocol.PhaseDone ||
		phase == protocol.PhasePendingReview ||
		phase == protocol.PhaseBlocked ||
		phase == protocol.PhaseNeedsAttention ||
		phase == protocol.PhaseAbandoned
}
