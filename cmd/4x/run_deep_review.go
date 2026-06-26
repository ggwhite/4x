package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ggwhite/4x/internal/enrich"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// runDeepReviewPhase 在 deep-reviewing phase 內執行自癒循環：先跑 deep reviewer，FAIL 時
// 不回主迴圈，而是在同一 phase 內反覆 spawn mini-coder（只修被點名的 issue）與 re-verifier
// （只驗舊 issue + 掃本輪新 diff），通過才推進 accepting；最多跑 max_fix_rounds 輪，超過則
// 維持 FAIL 報告並 escalate 到 needs-attention。
//
// 回傳 (cont, err)：cont 為 true 表示主迴圈應 continue（已推進 accepting 或跳過 deep review）；
// cont 為 false 且 err 為 nil 表示已落入終止狀態（needs-attention / blocked），主迴圈應 break；
// err 非 nil 表示 hard error 或 context cancel，直接中止。
func runDeepReviewPhase(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, commitStrategy string, manualRunner string, runOverrides map[protocol.Phase]protocol.PhaseSpec) (bool, error) {
	featureID := feature.ID
	round := s.Round

	// active profile 用於解析 mini-coder 的 coder model 與 deep-reviewing phase 的 runner 覆寫。
	_, pc, err := protocol.ResolveProfile(cfg, feature, s.Profile)
	if err != nil {
		stopState(ws, featureID, s, "profile-error", fmt.Sprintf("deep-reviewer profile resolution failed: %v", err))
		return false, fmt.Errorf("resolve profile: %w", err)
	}

	// deep-reviewing phase 的 runner 依覆寫優先序解析（含 per-phase 臨時覆寫）；其下所有子 role
	// （deep-reviewer、mini-coder、re-verifier、synthesizer）皆共用此 runner，model 行為各自維持既有語意。
	deepRunnerManual, _ := protocol.EffectiveManual(runOverrides, protocol.PhaseDeepReviewing, manualRunner)
	deepRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseDeepReviewing, deepRunnerManual)
	if err != nil {
		stopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-reviewer runner resolution failed: %v", err))
		return false, fmt.Errorf("deep runner resolution failed: %w", err)
	}

	// 1. 解析 deep_model（deep_model 掛在 reviewer role 上）；未設定時 fallback 到 DefaultDeepTier。
	deepModel, err := protocol.ResolveDeepModel(cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		stopState(ws, featureID, s, "model-error", fmt.Sprintf("deep-reviewer model resolution failed: %v", err))
		return false, fmt.Errorf("deep model resolution failed: %w", err)
	}
	if deepModel == "" {
		// Fallback：未明確設定 deep_model 時，嘗試用 DefaultDeepTier（"opus"）解析。
		deepModel, _ = protocol.ResolveTierModel(cfg, deepRunner, protocol.DefaultDeepTier)
	}
	if deepModel == "" {
		newState, err := state.Transition(*s, protocol.PhaseAccepting, protocol.RoleAcceptor)
		if err != nil {
			return false, fmt.Errorf("skip deep-review transition: %w", err)
		}
		*s = newState
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (skip deep-review): %w", err)
		}
		logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: round,
			Runner: s.Runner, Detail: fmt.Sprintf("deep_model not configured and runner cannot resolve default tier %q", protocol.DefaultDeepTier),
		})
		fmt.Printf("[round %d] deep-reviewing — skipped (runner cannot resolve default tier %q)\n", round, protocol.DefaultDeepTier)
		return true, nil
	}

	// 若 deep_model 是 fallback 解析的（非明確設定），印提示訊息。
	if rc, ok := cfg.Roles[string(protocol.RoleReviewer)]; !ok || rc.DeepModel == "" {
		fmt.Printf("[round %d] deep-reviewing — using default tier %q (no explicit deep_model configured)\n", round, protocol.DefaultDeepTier)
	}

	// 2. 跑 deep reviewer：依設定走平行 N sub-reviewer + synthesizer，或 fallback 單 agent。
	// SubPhaseReviewing 在分支前設定，平行與單 agent fallback 兩條路徑共用。
	s.Role = protocol.RoleDeepReviewer
	s.SubPhase = protocol.SubPhaseReviewing
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (deep-reviewer): %w", err)
	}
	groups := protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount)
	if len(groups) > 1 {
		// 平行模式：N sub-reviewer 各寫 partial report，synthesizer 合併成 deep-review-report.md。
		if ok, err := runDeepReviewParallel(ctx, ws, runnerWs, feature, cfg, s, ops, newRunner, deepRunner, deepModel, groups, round); !ok || err != nil {
			return ok, err
		}
	} else {
		// fallback 單 agent：deep reviewer 直接輸出 deep-review-report.md（現行行為）。
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleDeepReviewer, deepRunner, deepModel, runner.LogFileName(round, string(protocol.RoleDeepReviewer)), round, 0); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
			return ok, err
		}
	}
	reportPath := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	if _, statErr := os.Stat(reportPath); statErr != nil {
		return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+protocol.DeepReviewReport)
	}

	// 3. PASS → accepting。
	if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
		autoDiscoverFeatures(ctx, ws, feature, cfg, round, newEnrichRunner(ws, cfg, deepRunner, feature, newRunner, round))
		return deepTransitionAccepting(ws, featureID, s)
	}

	// 4. FAIL → 內部自癒循環。
	maxFix := protocol.ResolveMaxFixRounds(cfg, protocol.RoleDeepReviewer)
	// mini-coder 的 model 沿用 coding phase 的解析（含 coding 的 per-phase 臨時 model 覆寫）。
	_, coderModelManual := protocol.EffectiveManual(runOverrides, protocol.PhaseCoding, manualRunner)
	coderModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseCoding, protocol.RoleCoder, deepRunner, coderModelManual)
	if err != nil {
		stopState(ws, featureID, s, "model-error", fmt.Sprintf("coder model resolution failed: %v", err))
		return false, fmt.Errorf("coder model resolution failed: %w", err)
	}
	reviewModel, err := protocol.ResolveModel(cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		stopState(ws, featureID, s, "model-error", fmt.Sprintf("reviewer model resolution failed: %v", err))
		return false, fmt.Errorf("reviewer model resolution failed: %w", err)
	}

	for iter := 1; iter <= maxFix; iter++ {
		fmt.Printf("[round %d] deep-reviewing — self-heal iteration %d/%d\n", round, iter, maxFix)

		// 4a. mini-coder（model = coder model，不用昂貴 deep_model），phase 維持 deep-reviewing。
		s.Role = protocol.RoleMiniCoder
		s.SubPhase = protocol.SubPhaseFixing
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (mini-coder): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleMiniCoder, deepRunner, coderModel, runner.DeepFixLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}

		// mini-coder 改了 source code：worktree + per-round 模式下比照 coder 即時 commit。
		if commitStrategy == "per-round" && runnerWs.Root != ws.Root {
			if err := ops.Commit(runnerWs.Root, featureID, fmt.Sprintf("wip(%s): round %d deep-fix %d", featureID, round, iter)); err != nil {
				slog.Error("auto-commit deep-fix failed", "feature", featureID, "round", round, "iteration", iter, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "iteration", iter, "strategy", "deep-fix")
			}
		}

		// 4b. guard 檢查：mini-coder 改動超出原始 scope → 寫 FAIL 報告 + escalation，停下等人。
		if guardResult := guard.Check(ws, featureID, ops); !guardResult.Pass {
			reason := strings.Join(guardResult.Errors, "; ")
			writeDeepReviewFailReport(ws, featureID, round, "scope-exceed", reason)
			writeDeepEscalation(ws, featureID, round, "scope-change", "mini-coder scope-exceed: "+reason)
			s.Phase = protocol.PhaseNeedsAttention
			stopState(ws, featureID, s, "scope-exceed", "deep-fix scope exceeded: " + reason)
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleMiniCoder,
				Round: round, Detail: s.StopMessage, Runner: s.Runner,
			})
			return false, nil
		}

		// 4c. re-verifier（model = reviewer model，scoped 驗證，不用昂貴 opus），read-only。
		s.Role = protocol.RoleReVerifier
		s.SubPhase = protocol.SubPhaseReverifying
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (re-verifier): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleReVerifier, deepRunner, reviewModel, runner.DeepReverifyLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleReVerifier); !ok || err != nil {
			return ok, err
		}

		// 4d. re-verifier 已把 deep-review-report.md 的 Verdict 改 PASS → accepting。
		if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
			autoDiscoverFeatures(ctx, ws, feature, cfg, round, newEnrichRunner(ws, cfg, deepRunner, feature, newRunner, round))
			return deepTransitionAccepting(ws, featureID, s)
		}
	}

	// 5. 跑滿 maxFix 仍 FAIL → 維持 FAIL 報告 + escalate 到 needs-attention。
	writeDeepEscalation(ws, featureID, round, "blocker",
		fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	s.Phase = protocol.PhaseNeedsAttention
	stopState(ws, featureID, s, "self-heal-exhausted", fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "escalation", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer,
		Round: round, Detail: s.StopMessage, Runner: s.Runner, Notify: protocol.NotifyWarning,
	})
	fmt.Printf("[round %d] deep-reviewing — self-heal exhausted (%d iterations), escalating\n", round, maxFix)
	return false, nil
}

// runDeepReviewParallel 在 deep-reviewing phase 內平行 spawn len(groups) 個 sub-reviewer，
// 各自只跑分配到的 review angle 並寫出 deep-review-partial-<i>.md，全部完成後再 spawn 一個
// synthesizer 把所有 partial report 合併成單一 deep-review-report.md（格式與單 agent 完全相同）。
// 全程維持 deep-reviewing phase。sub-reviewer 與 synthesizer 皆 read-only，共用同一 worktree。
//
// 回傳 (ok, err)：語意同 runDeepSubRole；ok 為 true 時 deep-review-report.md 已產出，
// caller 接續走 reviewPassed → accepting / self-heal 分支。
func runDeepReviewParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, runnerName, deepModel string, groups [][]int, round int) (bool, error) {
	featureID := feature.ID

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}
	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}
	// cleanup 停掉 live sync 並把 worktree 內的 report 同步回主 workspace；可安全重複呼叫。
	cleanup := func() {
		if stopSync != nil {
			stopSync()
			stopSync = nil
		}
		if runnerWs.Root != ws.Root {
			if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
				slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
			}
		}
	}

	type runOutcome struct {
		index  int
		result *runner.Result
		err    error
	}

	// resume：跳過已完整寫出 partial 的 sub-reviewer，只補跑缺少的 index。
	// missing 為空時整個 sub-reviewer 階段跳過，直接進 synthesizer
	//（涵蓋「synthesizer 掛掉、partial 都在 → 只重跑 synthesizer」）。
	// partial index 與 angle group 的對應固定（idx=i+1 → groups[idx-1]），補跑時沿用原分配。
	missing := missingDeepPartials(runnerWs.RoundDir(featureID, round), len(groups))

	outcomes := make([]runOutcome, len(missing))
	if len(missing) > 0 {
		fmt.Printf("[round %d] deep-reviewing — running %d parallel sub-reviewers (%s, model: %s)\n", round, len(missing), runnerName, deepModel)

		var wg sync.WaitGroup
		for slot, idx := range missing {
			wg.Add(1)
			go func(slot, idx int) {
				defer wg.Done()
				angles := groups[idx-1]
				partialName := deepReviewPartialName(idx)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
					Runner: runnerName, Model: deepModel,
				})
				prompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleDeepReviewer, round, 0, runnerName,
					withParallelDeepReviewer(idx, len(groups), angles, partialName))
				if perr != nil {
					prompt = fmt.Sprintf("You are deep sub-reviewer %d for feature %s, round %d. Read .4x/%s/ for context.", idx, featureID, round, featureID)
				}
				logPath := filepath.Join(runner.LogDir(ws, featureID), runner.DeepReviewerLogFileName(round, idx))
				r := newRunner(runnerName, logPath, deepModel)
				res, runErr := r.Run(ctx, prompt)
				outcomes[slot] = runOutcome{index: idx, result: res, err: runErr}
			}(slot, idx)
		}
		wg.Wait()
	} else {
		fmt.Printf("[round %d] deep-reviewing — all %d partials present, resuming at synthesizer (%s)\n", round, len(groups), runnerName)
	}

	// runner 執行錯誤分類：context cancel → interrupted；其餘 → runner-error needs-attention。
	for _, o := range outcomes {
		if o.err != nil {
			cleanup()
			if ctx.Err() == context.Canceled {
				stopState(ws, featureID, s, "interrupted", fmt.Sprintf("deep-reviewer interrupted by signal (round %d)", round))
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			stopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-reviewer runner failed (round %d): %v", round, o.err))
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: runnerName, Model: deepModel,
			})
			return false, o.err
		}
	}
	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: runnerName, Model: deepModel,
		})
	}
	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			cleanup()
			stopState(ws, featureID, s, "hard-error", fmt.Sprintf("deep-reviewer runner returned hard error (exit 2) (round %d)", round))
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			cleanup()
			s.Phase = protocol.PhaseNeedsAttention
			stopState(ws, featureID, s, "deep-reviewer-soft-fail", fmt.Sprintf("deep-reviewer runner returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round))
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			return false, nil
		}
	}

	// 驗證每個 sub-reviewer 都寫出 partial report，並讀入完整內文供 synthesizer 內嵌。
	var partials []includeContent
	for i := 1; i <= len(groups); i++ {
		name := deepReviewPartialName(i)
		data, rerr := os.ReadFile(filepath.Join(runnerWs.RoundDir(featureID, round), name))
		if rerr != nil {
			cleanup()
			return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+name)
		}
		partials = append(partials, includeContent{Path: name, Content: string(data)})
	}

	// synthesizer 合併所有 partial report 成單一 deep-review-report.md。
	s.Role = protocol.RoleSynthesizer
	s.SubPhase = protocol.SubPhaseSynthesizing
	if err := ws.WriteState(featureID, *s); err != nil {
		cleanup()
		return false, fmt.Errorf("write state (synthesizer): %w", err)
	}
	// synthesizer 只做文本合併、不讀原始碼，用獨立的便宜 model（預設 sonnet tier，
	// 可由 roles.synthesizer.model 覆寫）。解析失敗時 fallback 回 deepModel，不中斷 run。
	synthModel := deepModel
	if m, mErr := protocol.ResolveModel(cfg, runnerName, protocol.RoleSynthesizer); mErr == nil {
		synthModel = m
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Runner: runnerName, Model: synthModel,
	})
	synthPrompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleSynthesizer, round, 0, runnerName,
		withSynthesizerReports(partials))
	if perr != nil {
		synthPrompt = fmt.Sprintf("You are the deep review synthesizer for feature %s, round %d. Read .4x/%s/ for context.", featureID, round, featureID)
	}
	synthLog := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(protocol.RoleSynthesizer)))
	synthRunner := newRunner(runnerName, synthLog, synthModel)
	fmt.Printf("[round %d] deep-reviewing (synthesizer) — invoking %s (model: %s)\n", round, runnerName, synthModel)
	synthRes, synthErr := synthRunner.Run(ctx, synthPrompt)
	if synthErr != nil {
		cleanup()
		if ctx.Err() == context.Canceled {
			stopState(ws, featureID, s, "interrupted", fmt.Sprintf("deep-review synthesizer interrupted by signal (round %d)", round))
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		stopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-review synthesizer runner failed (round %d): %v", round, synthErr))
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
			Status: "error", Detail: synthErr.Error(), Runner: runnerName, Model: synthModel,
		})
		return false, synthErr
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Status: fmt.Sprintf("exit-%d", synthRes.ExitCode), Runner: runnerName, Model: synthModel,
	})
	if runner.IsHardError(synthRes) {
		cleanup()
		stopState(ws, featureID, s, "hard-error", fmt.Sprintf("deep-review synthesizer returned hard error (exit 2) (round %d)", round))
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(synthRes) {
		cleanup()
		s.Phase = protocol.PhaseNeedsAttention
		stopState(ws, featureID, s, "synthesizer-soft-fail", fmt.Sprintf("deep-review synthesizer returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round))
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		return false, nil
	}

	cleanup()
	// sub-reviewer 與 synthesizer 皆 read-only：跑一次 guardrail 確認沒越界改檔。
	if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
		return ok, err
	}
	return true, nil
}

// runDeepSubRole 在 deep-reviewing phase 內 spawn 一個子 role（deep-reviewer / mini-coder /
// re-verifier），處理 phase-start/run-end event、prompt 產生、runner 執行與 worktree 同步，
// 並分類 context cancel / runner error / hard error / soft fail。phase 全程維持 deep-reviewing。
//
// 回傳 (ok, err)：ok 為 true 表示 runner 正常結束，caller 可繼續；ok 為 false 且 err 為 nil
// 表示已寫入終止狀態（needs-attention / blocked）；err 非 nil 表示 hard error 或 cancel。
func runDeepSubRole(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, newRunner func(runnerName string, logPath string, model string) runner.Runner, role protocol.Role, runnerName, model, logName string, round, iteration int) (bool, error) {
	featureID := feature.ID

	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Runner: runnerName, Model: model,
	})

	prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, round, iteration, runnerName)
	if err != nil {
		prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
	}
	logPath := filepath.Join(runner.LogDir(ws, featureID), logName)
	r := newRunner(runnerName, logPath, model)

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}
	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	if model != "" {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s (model: %s)\n", round, role, runnerName, model)
	} else {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s\n", round, role, runnerName)
	}

	result, runErr := r.Run(ctx, prompt)

	if stopSync != nil {
		stopSync()
	}
	if runnerWs.Root != ws.Root {
		if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "role", role, "error", serr)
		}
	}

	if runErr != nil {
		if ctx.Err() == context.Canceled {
			stopState(ws, featureID, s, "interrupted", fmt.Sprintf("deep-reviewing (%s) interrupted by signal (round %d)", role, round))
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		stopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-reviewing (%s) runner failed (round %d): %v", role, round, runErr))
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
			Status: "error", Detail: runErr.Error(), Runner: runnerName, Model: model,
		})
		return false, runErr
	}

	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Status: fmt.Sprintf("exit-%d", result.ExitCode), Runner: runnerName, Model: model,
	})

	if runner.IsHardError(result) {
		stopState(ws, featureID, s, "hard-error", fmt.Sprintf("deep-reviewing (%s) runner returned hard error (exit 2) (round %d)", role, round))
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		stopState(ws, featureID, s, "soft-fail", fmt.Sprintf("deep-reviewing (%s) runner returned soft-fail (exit %d) (round %d)", role, runner.ExitSoftFail, round))
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
		return false, nil
	}
	return true, nil
}

// deepGuardCheck 在 deep-reviewing phase 內對 read-only 子 role（deep-reviewer / re-verifier）
// 跑 guardrail 檢查；失敗時落入 needs-attention 並回傳 (false, nil)。
func deepGuardCheck(ws *protocol.Workspace, featureID string, s *protocol.State, ops gitops.Ops, role protocol.Role) (bool, error) {
	guardResult := guard.Check(ws, featureID, ops)
	if guardResult.Pass {
		return true, nil
	}
	s.Phase = protocol.PhaseNeedsAttention
	guardMsg := strings.Join(guardResult.Errors, "; ")
	stopState(ws, featureID, s, "guard-fail", guardMsg)
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: role,
		Round: s.Round, Detail: guardMsg, Runner: s.Runner,
	})
	return false, nil
}

// deepTransitionAccepting 把 state 從 deep-reviewing 推進到 accepting 並寫回，
// 供自癒循環在 deep review PASS 時放行。
func deepTransitionAccepting(ws *protocol.Workspace, featureID string, s *protocol.State) (bool, error) {
	newState, err := state.Transition(*s, protocol.PhaseAccepting, protocol.RoleAcceptor)
	if err != nil {
		return false, fmt.Errorf("deep-review→accepting transition: %w", err)
	}
	*s = newState
	// 離開 deep-reviewing：清空 subPhase，使續跑的主迴圈持有的 *s 與磁碟一致。
	s.SubPhase = ""
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (accepting): %w", err)
	}
	logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// autoDiscoverFeatures 在 final deep review PASS 後執行：parse deep-review-report.md 的
// [NEW-FEATURE] 標記，去重、套數量上限後建立新 feature，並寫出 discovered-features.md 摘要。
// 只在兩個 deep review PASS return 點（首次 PASS、self-heal 後 re-verifier 改 PASS）被呼叫，
// 中間輪與 FAIL/needs-attention 路徑都到不了，因此等同「只在 final deep review 觸發」。
// 為 best-effort：任何錯誤只記 log，絕不中斷 accepting 轉換。
// r 為 enrichment 用的 runner，可為 nil：nil 或 cfg.EnrichDiscoveredFeatures 關閉時走原本的
// 薄 feature 路徑（向後相容）；開啟且 r 非 nil 時每個 candidate 先經 LLM enrichment 補強，
// 補強失敗（Discarded 或 runner error）的 candidate 不入庫，記入報告的 Enrichment Failed 段。
func autoDiscoverFeatures(ctx context.Context, ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, round int, r runner.Runner) {
	if !cfg.AutoDiscoverFeatures {
		return
	}

	reportPath := filepath.Join(ws.RoundDir(feature.ID, round), protocol.DeepReviewReport)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		slog.Warn("auto-discover: read deep-review report failed", "feature", feature.ID, "round", round, "error", err)
		return
	}

	cands := protocol.ParseDiscoveredFeatures(string(data))
	if len(cands) == 0 {
		return
	}

	existing, _ := ws.ListFeatures()
	kept := protocol.DedupeDiscovered(cands, existing)

	max := protocol.ResolveMaxDiscoveredFeatures(cfg)
	var capped []protocol.DiscoveredFeature
	if len(kept) > max {
		capped = kept[max:]
		kept = kept[:max]
	}

	// skipped 為被去重濾掉的候選（出現在 cands 但不在 kept/capped 中）。
	keptOrCapped := make(map[string]struct{})
	for _, d := range kept {
		keptOrCapped[d.Title] = struct{}{}
	}
	for _, d := range capped {
		keptOrCapped[d.Title] = struct{}{}
	}
	var skipped []protocol.DiscoveredFeature
	for _, c := range cands {
		if _, ok := keptOrCapped[c.Title]; !ok {
			skipped = append(skipped, c)
		}
	}

	var enricher *enrich.Enricher
	if cfg.EnrichDiscoveredFeatures && r != nil {
		enricher = enrich.New(ws, r, cfg.EnrichAutoApprove)
	}

	idf := feat.ResolveIDFormat(cfg.FeatureIDPrefix, cfg.FeatureIDDigits)

	var createdList []discoveredCreated
	var enrichFailed []protocol.DiscoveredFeature
	for _, d := range kept {
		next, nerr := feat.NextNumber(ws, idf)
		if nerr != nil {
			slog.Warn("auto-discover: next feature number failed", "feature", feature.ID, "title", d.Title, "error", nerr)
			continue
		}
		id := feat.GenerateFeatureID(next, d.Title, idf)

		var f feat.Feature
		if enricher != nil {
			result, eerr := enricher.Enrich(ctx, d)
			if eerr != nil {
				slog.Warn("auto-discover: enrichment error", "feature", feature.ID, "title", d.Title, "error", eerr)
				enrichFailed = append(enrichFailed, d)
				continue
			}
			if result.Discarded {
				slog.Info("auto-discover: enrichment discarded", "feature", feature.ID, "title", d.Title, "reason", result.Reason)
				enrichFailed = append(enrichFailed, d)
				continue
			}
			f = result.Feature
			f.ID = id
			f.Name = idf.FormatDisplayName(next, d.Title)
		} else {
			f = feat.Feature{
				ID:          id,
				Name:        idf.FormatDisplayName(next, d.Title),
				Description: d.Description,
				Status:      feat.StatusNotStarted,
			}
		}

		if serr := ws.SaveFeature(f); serr != nil {
			slog.Warn("auto-discover: save feature failed", "feature", feature.ID, "title", d.Title, "error", serr)
			continue
		}
		createdList = append(createdList, discoveredCreated{id: id, title: d.Title})
		ws.AppendEvent(feature.ID, protocol.Event{
			Type: "feature-discovered", Phase: protocol.PhaseDeepReviewing, Round: round, Detail: id,
		})
	}

	writeDiscoveredFeaturesReport(ws, feature.ID, createdList, skipped, capped, enrichFailed)

	fmt.Printf("[round %d] auto-discovered %d feature(s)\n", round, len(createdList))
}

// discoveredCreated 記錄一筆已建立的 feature（id 與 title），供摘要報告列出。
type discoveredCreated struct{ id, title string }

// newEnrichRunner 為 auto-discover enrichment 構造 runner：沿用 deep-review 的 runner 名稱與
// 較便宜的 reviewer model，避免在 CLI 層直接呼叫 LLM。enrichment 關閉時回 nil，讓
// autoDiscoverFeatures 走原本的薄 feature 路徑。reviewer model 解析失敗時退回空字串（不帶 --model）。
func newEnrichRunner(ws *protocol.Workspace, cfg protocol.Config, deepRunner string, feature feat.Feature, newRunner func(string, string, string) runner.Runner, round int) runner.Runner {
	if !cfg.EnrichDiscoveredFeatures {
		return nil
	}
	model, err := protocol.ResolveModel(cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		model = ""
	}
	logPath := filepath.Join(runner.LogDir(ws, feature.ID), fmt.Sprintf("round-%d-enrich.log", round))
	return newRunner(deepRunner, logPath, model)
}

// writeDiscoveredFeaturesReport 寫出 .4x/{featureID}/discovered-features.md 摘要：
// 列出已建立、因重複略過、因超過上限略過的候選 feature。
func writeDiscoveredFeaturesReport(ws *protocol.Workspace, featureID string, created []discoveredCreated, skipped, capped, enrichFailed []protocol.DiscoveredFeature) {
	var b strings.Builder
	b.WriteString("# Discovered Features\n\n")

	b.WriteString("## Created\n")
	if len(created) == 0 {
		b.WriteString("None\n")
	} else {
		for _, c := range created {
			fmt.Fprintf(&b, "- %s — %s\n", c.id, c.title)
		}
	}

	b.WriteString("\n## Skipped (duplicate)\n")
	if len(skipped) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range skipped {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	b.WriteString("\n## Capped (over limit)\n")
	if len(capped) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range capped {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	b.WriteString("\n## Enrichment Failed (discarded)\n")
	if len(enrichFailed) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range enrichFailed {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	path := filepath.Join(ws.FeatureDir(featureID), "discovered-features.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		slog.Error("write discovered-features report failed", "feature", featureID, "error", err)
	}
}

// writeDeepReviewFailReport 由 CLI 在 deep-reviewing 終止場景（如 mini-coder scope-exceed）
// 直接寫出 FAIL 的 deep-review-report.md，標注原因供 dashboard 與 acceptor 辨識。
func writeDeepReviewFailReport(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	content := fmt.Sprintf("# Deep Review Report — Round %d\n\n## Summary\nFAIL — %s\n\n## Issues\n### [CRITICAL] %s\n%s\n\n## Verdict\nFAIL\n",
		round, reason, reason, detail)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Error("write deep-review FAIL report failed", "feature", featureID, "round", round, "error", err)
	}
}

// writeDeepEscalation 由 CLI 在 deep-reviewing 終止場景寫出 escalation.json，讓 resume 與
// dashboard 能辨識升級原因（scope-change / blocker）。
func writeDeepEscalation(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	esc := protocol.Escalation{Needed: true, Reason: reason, Detail: detail}
	data, _ := json.Marshal(esc)
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.EscalationFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Error("write deep-review escalation failed", "feature", featureID, "round", round, "error", err)
	}
}
