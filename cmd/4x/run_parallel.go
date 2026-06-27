package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// runReviewTestParallel 在 reviewing phase 同時跑 reviewer 與 tester（皆 read-only、共用
// worktree），兩者完成後合併判定。回傳 (cont, err)：cont 為 true 表示主迴圈應 continue
// 接手後續 phase（deep-reviewing 或 amending）；cont 為 false 且 err 為 nil 表示已落入
// 終止狀態（blocked / needs-attention），主迴圈應 break；err 非 nil 表示 hard error 直接中止。
func runReviewTestParallel(ctx context.Context, rc *runContext, s *protocol.State, pc protocol.ProfileConfig) (bool, error) {
	ws := rc.ws
	runnerWs := rc.runnerWs
	feature := rc.feature
	cfg := rc.cfg
	ops := rc.ops
	newRunner := rc.newRunner
	manualRunner := rc.manualRunner
	runOverrides := rc.runOverrides

	featureID := feature.ID
	round := s.Round

	resolveErr := func(what string, err error) (bool, error) {
		stopState(ws, featureID, s, "model-error", fmt.Sprintf("%s resolution failed: %v", what, err))
		return false, fmt.Errorf("%s resolution failed: %w", what, err)
	}

	reviewRunnerManual, reviewModelManual := protocol.EffectiveManual(runOverrides, protocol.PhaseReviewing, manualRunner)
	testRunnerManual, testModelManual := protocol.EffectiveManual(runOverrides, protocol.PhaseTesting, manualRunner)

	reviewRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseReviewing, reviewRunnerManual)
	if err != nil {
		return resolveErr("reviewer runner", err)
	}
	reviewModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseReviewing, protocol.RoleReviewer, reviewRunner, reviewModelManual)
	if err != nil {
		return resolveErr("reviewer model", err)
	}
	testRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseTesting, testRunnerManual)
	if err != nil {
		return resolveErr("tester runner", err)
	}
	testModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseTesting, protocol.RoleTester, testRunner, testModelManual)
	if err != nil {
		return resolveErr("tester model", err)
	}

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}

	type runOutcome struct {
		role       protocol.Role
		runnerName string
		model      string
		result     *runner.Result
		err        error
	}

	runRole := func(role protocol.Role, runnerName, model string) runOutcome {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: protocol.PhaseReviewing, Role: role, Round: round,
			Runner: runnerName, Model: model,
		})
		promptText, err := prompt.Generate(promptCtxFromRun(rc), role, round, 0, runnerName)
		if err != nil {
			promptText = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
		}
		logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(role)))
		r := newRunner(runnerName, logPath, model)
		res, runErr := r.Run(ctx, promptText)
		return runOutcome{role: role, runnerName: runnerName, model: model, result: res, err: runErr}
	}

	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	fmt.Printf("[round %d] reviewing — running reviewer (%s) + tester (%s) in parallel\n", round, reviewRunner, testRunner)

	var wg sync.WaitGroup
	outcomes := make([]runOutcome, 2)
	wg.Add(2)
	go func() { defer wg.Done(); outcomes[0] = runRole(protocol.RoleReviewer, reviewRunner, reviewModel) }()
	go func() { defer wg.Done(); outcomes[1] = runRole(protocol.RoleTester, testRunner, testModel) }()
	wg.Wait()

	if stopSync != nil {
		stopSync()
	}
	if runnerWs.Root != ws.Root {
		if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
		}
	}

	// runner 執行錯誤：context cancel → interrupted；其餘 → runner-error needs-attention。
	for _, o := range outcomes {
		if o.err != nil {
			if ctx.Err() == context.Canceled {
				stopState(ws, featureID, s, "interrupted", fmt.Sprintf("%s interrupted by signal (round %d)", o.role, round))
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			stopState(ws, featureID, s, "runner-error", fmt.Sprintf("%s runner failed (round %d): %v", o.role, round, o.err))
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: o.runnerName, Model: o.model,
			})
			return false, o.err
		}
	}

	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: o.runnerName, Model: o.model,
		})
	}

	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			stopState(ws, featureID, s, "hard-error", fmt.Sprintf("%s runner returned hard error (exit 2) during parallel review (round %d)", o.role, round))
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			s.Phase = protocol.PhaseBlocked
			stopState(ws, featureID, s, "soft-fail", fmt.Sprintf("%s runner returned soft-fail (exit %d) during parallel review (round %d)", o.role, runner.ExitSoftFail, round))
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
			return false, nil
		}
	}

	guardResult := guard.Check(ws, featureID, ops)
	if !guardResult.Pass {
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		guardMsg := strings.Join(guardResult.Errors, "; ")
		s.StopReason = "guard-fail"
		s.StopMessage = guardMsg
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "guard-fail", Phase: protocol.PhaseReviewing, Round: round,
			Detail: guardMsg, Runner: s.Runner,
		})
		return false, nil
	}

	// 合併判定。先確認 reviewer 與 tester 的 artifact 完整。
	reviewReport := filepath.Join(ws.RoundDir(featureID, round), protocol.ReviewReport)
	if _, err := os.Stat(reviewReport); err != nil {
		return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+protocol.ReviewReport)
	}

	// tester escalation 優先處理（可回 designer 或停下等人）。
	if esc := readEscalation(ws, featureID, round); esc.Needed {
		if isDesignerEscalation(esc.Reason) {
			return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
		}
		return parallelNeedsAttention(ws, featureID, s, esc.Reason)
	}

	reviewOK := reviewPassed(ws, featureID, round, protocol.ReviewReport)
	vs := checkVerify(ws, featureID, round)

	if !reviewOK {
		return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
	}
	if vs != verifyOK {
		testReportOK := reviewPassed(ws, featureID, round, protocol.TestReport)
		if testReportOK {
			msg := "verify.json missing but test-report verdict is PASS — tester likely could not run `4x verify`"
			if vs == verifyFailed {
				msg = "verify.json passed=false but test-report verdict is PASS — review the failing verify commands"
			}
			return parallelNeedsAttention(ws, featureID, s, msg)
		}
		return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
	}

	// 兩者皆 PASS：tester 必須備齊 final-report 等抵達 accepting 的 artifact。
	if testGuard := guard.CheckTestingToAccepting(ws, featureID, round); !testGuard.Pass {
		return parallelNeedsAttention(ws, featureID, s, strings.Join(testGuard.Errors, "; "))
	}

	// 沿合法邊兩跳：reviewing→testing→deep-reviewing，由主迴圈在 deep-reviewing 接手。
	if cont, err := parallelTransition(ws, featureID, s, protocol.PhaseTesting, protocol.RoleTester); !cont || err != nil {
		return cont, err
	}
	return parallelTransition(ws, featureID, s, protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer)
}

// applyAmendingProgress 在轉入 amending 時更新 W1 無進展追蹤欄位（就地修改 st）。
// 以 reviewRound（轉換前的 review 輪次）讀 review-report.md 的失敗計數，與上輪基準比較：
// 持平或更差 → ConsecutiveNoProgress++；改善 → 歸零。首輪（尚無基準且 cur > 0）僅建立
// LastFailCount，不 increment。序列路徑與平行 review/test 路徑共用此 helper，確保
// ShouldStop（ConsecutiveNoProgress >= 3）在兩種模式下行為一致，不因路徑漂移而失效。
func applyAmendingProgress(ws *protocol.Workspace, featureID string, st *protocol.State, reviewRound int) {
	cur := reviewFailCount(ws, featureID, reviewRound)
	// 首輪 amending（基準為 0 且本輪有失敗）僅建立基準；額外要求 cur > 0 避免
	// review-report 缺失/格式異常使 cur 恆為 0 時「首輪」條件每輪成立、永遠無法遞增。
	if st.LastFailCount == 0 && st.ConsecutiveNoProgress == 0 && cur > 0 {
		// 僅建立基準，不 increment
	} else if cur >= st.LastFailCount {
		st.ConsecutiveNoProgress++
	} else {
		st.ConsecutiveNoProgress = 0
	}
	st.LastFailCount = cur
}

// parallelTransition 執行一次合法 state 轉換並寫回，供平行 review/test 合併後推進 phase。
// 轉入 amending 時套用與序列路徑相同的 W1 無進展追蹤（applyAmendingProgress），
// 用轉換前的 round 讀 review 失敗計數，確保 ShouldStop 在平行模式下同樣生效。
func parallelTransition(ws *protocol.Workspace, featureID string, s *protocol.State, to protocol.Phase, role protocol.Role) (bool, error) {
	reviewRound := s.Round
	newState, err := state.Transition(*s, to, role)
	if err != nil {
		return false, fmt.Errorf("parallel transition %s→%s: %w", s.Phase, to, err)
	}
	if to == protocol.PhaseAmending {
		applyAmendingProgress(ws, featureID, &newState, reviewRound)
	}
	*s = newState
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (%s): %w", s.Phase, err)
	}
	logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// parallelNeedsAttention 把 state 落入 needs-attention 並寫回，回傳 (false, nil) 讓主迴圈 break。
func parallelNeedsAttention(ws *protocol.Workspace, featureID string, s *protocol.State, reason string) (bool, error) {
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	s.StopMessage = reason
	if strings.HasPrefix(reason, "missing-artifact:") {
		s.StopReason = "missing-artifact"
	} else {
		s.StopReason = "escalation"
	}
	logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	return false, nil
}
