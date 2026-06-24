package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// needsResumeRecovery 判斷 state 是否需要 resume recovery（依 phase + round 決定）。
// 規則：
//   - blocked / needs-attention：任何 round 都校正（維持既有 recovery 行為）。
//   - 已進入 coding 後的工作 phase（coding/reviewing/testing/deep-reviewing/amending/accepting）
//     且 round > 0：校正，使 state.json 與磁碟 artifacts 一致。
//   - init / designing / pending-review / done 等：不校正，維持原 resume 行為。
func needsResumeRecovery(s protocol.State) bool {
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

// smartResumePhase 檢查當前 round 的 artifacts 決定 resume 起點。
// 已完成的步驟不重跑；只從第一個缺失或失敗的步驟開始。
// 「已完成」採與 cleanStaleArtifact 相同的完整性判準（*Complete helper），而非裸存在性檢查：
// crash 發生於當前 phase、report 寫到一半時，半成品檔雖存在但不完整，必須回該 phase 重跑，
// 不可因檔案存在就把 phase 往前推進。
func smartResumePhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) (protocol.Phase, protocol.Role, protocol.SubPhase) {
	if round == 0 {
		return protocol.PhaseDesigning, protocol.RoleDesigner, ""
	}
	roundDir := ws.RoundDir(featureID, round)

	designReviewPath := filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport)
	if reviewReportComplete(designReviewPath) {
		if !reviewPassedAtPath(designReviewPath) {
			return protocol.PhaseDesigning, protocol.RoleDesigner, ""
		}
	} else if _, err := os.Stat(designReviewPath); err == nil {
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer, ""
	}

	if !coderReportComplete(filepath.Join(roundDir, protocol.CoderReport)) {
		return protocol.PhaseCoding, protocol.RoleCoder, ""
	}

	if !reviewReportComplete(filepath.Join(roundDir, protocol.ReviewReport)) {
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""
	}
	if !reviewPassed(ws, featureID, round, protocol.ReviewReport) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	// testing 的完整性以 verify.json 可解析為準（與 cleanStaleArtifact 一致）；
	// test-report 與 verify.json 成對產出，verify.json 不完整即代表該 phase 未跑完。
	if !verifyEvidenceComplete(filepath.Join(roundDir, protocol.VerifyFile)) {
		return protocol.PhaseTesting, protocol.RoleTester, ""
	}
	if !verifyPassed(ws, featureID, round) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	if !reviewReportComplete(filepath.Join(roundDir, protocol.DeepReviewReport)) {
		// deep-review report 不完整：依磁碟上的 partial 狀態推斷中斷在哪個子步驟，
		// 讓 resume 只補跑缺少的部分（partial 全到齊 → synthesizer；否則 → sub-reviewer）。
		sub := deepResumeSubPhase(ws, featureID, round, cfg)
		role := protocol.RoleDeepReviewer
		if sub == protocol.SubPhaseSynthesizing {
			role = protocol.RoleSynthesizer
		}
		return protocol.PhaseDeepReviewing, role, sub
	}
	if !reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
		// deep-review FAIL → amending（同輪修正、Round++），與正常流程的
		// parallelTransition(..., PhaseAmending, ...) 及上方 review / verify FAIL 路徑一致；
		// 不再回傳 PhaseCoding（會被誤判為開新 coding 輪而覆蓋前輪報告）。
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
}

// deepResumeSubPhase 在 deep-review report 不完整時，依磁碟上的 partial 檔推斷 crash 中斷的子步驟：
//   - want<=1（單 agent 模式，無 partial）→ SubPhaseReviewing（重跑單一 deep-reviewer）。
//   - 有任何 partial 缺失/不完整 → SubPhaseReviewing（補跑缺少的 sub-reviewer）。
//   - partial 全到齊但 report 不完整 → SubPhaseSynthesizing（只重跑 synthesizer）。
//
// want 用與 runDeepReviewPhase 完全相同的純函式重算（GroupReviewAngles 輸入相同 → 輸出相同），
// 確保 resume 推斷的並行度與當初執行時一致。
func deepResumeSubPhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) protocol.SubPhase {
	want := len(protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount))
	if want <= 1 {
		return protocol.SubPhaseReviewing
	}
	if len(missingDeepPartials(ws.RoundDir(featureID, round), want)) > 0 {
		return protocol.SubPhaseReviewing
	}
	return protocol.SubPhaseSynthesizing
}

// isDesignerEscalation 判斷 escalation 是否應回到 Designer 而非停下來等人
// spec-mismatch / criteria-wrong 是 Designer 能自行修正的問題
func isDesignerEscalation(reason string) bool {
	return reason == "spec-mismatch" || reason == "criteria-wrong" || reason == "scope-change"
}

func readEscalation(ws *protocol.Workspace, featureID string, round int) protocol.Escalation {
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

func captureBaselineOnce(ws *protocol.Workspace, ops gitops.Ops, featureID string, featureRepos []string) error {
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
