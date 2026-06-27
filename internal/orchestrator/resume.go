package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

// NeedsResumeRecovery 判斷 state 是否需要 resume recovery（依 phase + round 決定）。
// blocked / needs-attention 任何 round 都校正；已進入 coding 後的工作 phase 且 round > 0 校正。
func NeedsResumeRecovery(s protocol.State) bool {
	if s.Phase == protocol.PhaseBlocked || s.Phase == protocol.PhaseNeedsAttention {
		return true
	}
	if s.Round <= 0 {
		return false
	}
	switch s.Phase {
	case protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseTesting,
		protocol.PhaseDeepReviewing, protocol.PhaseAmending, protocol.PhaseAccepting:
		return true
	default:
		return false
	}
}

// SmartResumePhase 檢查當前 round 的 artifacts 決定 resume 起點。
// 已完成的步驟不重跑；只從第一個缺失或失敗的步驟開始。
func SmartResumePhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) (protocol.Phase, protocol.Role, protocol.SubPhase) {
	if round == 0 {
		return protocol.PhaseDesigning, protocol.RoleDesigner, ""
	}
	roundDir := ws.RoundDir(featureID, round)

	designReviewPath := filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport)
	if ReviewReportComplete(designReviewPath) {
		if !ReviewPassedAtPath(designReviewPath) {
			return protocol.PhaseDesigning, protocol.RoleDesigner, ""
		}
	} else if _, err := os.Stat(designReviewPath); err == nil {
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer, ""
	}

	if !CoderReportComplete(filepath.Join(roundDir, protocol.CoderReport)) {
		return protocol.PhaseCoding, protocol.RoleCoder, ""
	}

	if !ReviewReportComplete(filepath.Join(roundDir, protocol.ReviewReport)) {
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""
	}
	if !ReviewPassed(ws, featureID, round, protocol.ReviewReport) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	if !VerifyEvidenceComplete(filepath.Join(roundDir, protocol.VerifyFile)) {
		return protocol.PhaseTesting, protocol.RoleTester, ""
	}
	if CheckVerify(ws, featureID, round) != VerifyOK {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	if !ReviewReportComplete(filepath.Join(roundDir, protocol.DeepReviewReport)) {
		sub := DeepResumeSubPhase(ws, featureID, round, cfg)
		role := protocol.RoleDeepReviewer
		if sub == protocol.SubPhaseSynthesizing {
			role = protocol.RoleSynthesizer
		}
		return protocol.PhaseDeepReviewing, role, sub
	}
	if !ReviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
}

// DeepResumeSubPhase 在 deep-review report 不完整時，依磁碟上的 partial 檔推斷 crash 中斷的子步驟
func DeepResumeSubPhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) protocol.SubPhase {
	want := len(protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount))
	if want <= 1 {
		return protocol.SubPhaseReviewing
	}
	if len(prompt.MissingDeepPartials(ws.RoundDir(featureID, round), want)) > 0 {
		return protocol.SubPhaseReviewing
	}
	return protocol.SubPhaseSynthesizing
}

// IsDesignerEscalation 判斷 escalation 是否應回到 Designer 而非停下來等人
func IsDesignerEscalation(reason string) bool {
	return reason == "spec-mismatch" || reason == "criteria-wrong" || reason == "scope-change"
}

// ReadEscalation 讀取指定 round 的 escalation.json，檔案不存在時回零值（Needed=false）
func ReadEscalation(ws *protocol.Workspace, featureID string, round int) protocol.Escalation {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.EscalationFile))
	if err != nil {
		return protocol.Escalation{}
	}
	var esc protocol.Escalation
	if err := json.Unmarshal(data, &esc); err != nil {
		esc.Needed = true
		esc.Reason = fmt.Sprintf("malformed escalation.json: %v", err)
		return esc
	}
	return esc
}

// CaptureBaselineOnce 在首次 coding 前擷取 baseline（已存在則跳過）
func CaptureBaselineOnce(ws *protocol.Workspace, ops gitops.Ops, featureID string, featureRepos []string) error {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("baseline path is a directory: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check baseline: %w", err)
	}
	if err := ops.CaptureBaseline(featureID, featureRepos); err != nil {
		return fmt.Errorf("capture baseline: %w", err)
	}
	return nil
}
