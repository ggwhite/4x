package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/enrich"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// deepReviewParams 收納 runDeepReviewPhase 在 setup 階段解析出的參數，
// 避免在 sub-method 間重複傳遞 runner/model/profile。
type deepReviewParams struct {
	pc         protocol.ProfileConfig
	deepRunner string
	deepModel  string
}

// runDeepReviewPhase 在 deep-reviewing phase 內執行自癒循環：先跑 deep reviewer，FAIL 時
// 不回主迴圈，而是在同一 phase 內反覆 spawn mini-coder（只修被點名的 issue）與 re-verifier
// （只驗舊 issue + 掃本輪新 diff），通過才推進 accepting；最多跑 max_fix_rounds 輪，超過則
// 維持 FAIL 報告並 escalate 到 needs-attention。
//
// 回傳 (cont, err)：cont 為 true 表示主迴圈應 continue（已推進 accepting 或跳過 deep review）；
// cont 為 false 且 err 為 nil 表示已落入終止狀態（needs-attention / blocked），主迴圈應 break；
// err 非 nil 表示 hard error 或 context cancel，直接中止。
func (r *Runner) runDeepReviewPhase(ctx context.Context, s *protocol.State) (bool, error) {
	dp, ok, err := r.deepReviewSetup(s)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}

	if cont, err := r.deepReviewRun(ctx, s, dp); err != nil || !cont {
		return cont, err
	}

	if r.deepReviewClearsToAccepting(s.Round, dp.pc) {
		AutoDiscoverFeatures(ctx, r.Ws, r.Feature, r.Cfg, s.Round, r.newEnrichRunner(dp.deepRunner, s.Round))
		return deepTransitionAccepting(r.Ws, r.featureID(), s, dp.pc)
	}

	return r.deepReviewSelfHeal(ctx, s, dp)
}

// deepReviewSetup 解析 profile、runner、model；回傳 (params, ok, err)。
// ok 為 false 時表示已跳過 deep review（直接推進 accepting）或已寫入終止狀態。
func (r *Runner) deepReviewSetup(s *protocol.State) (deepReviewParams, bool, error) {
	featureID := r.featureID()

	_, pc, err := protocol.ResolveProfile(r.Cfg, r.Feature, s.Profile)
	if err != nil {
		StopState(r.Ws, featureID, s, "profile-error", fmt.Sprintf("deep-reviewer profile resolution failed: %v", err))
		return deepReviewParams{}, false, fmt.Errorf("resolve profile: %w", err)
	}

	deepRunnerManual, _ := protocol.EffectiveManual(r.RunOverrides, protocol.PhaseDeepReviewing, r.ManualRunner)
	deepRunner, err := protocol.ResolvePhaseRunner(r.Cfg, r.Feature, pc, protocol.PhaseDeepReviewing, deepRunnerManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "runner-error", fmt.Sprintf("deep-reviewer runner resolution failed: %v", err))
		return deepReviewParams{}, false, fmt.Errorf("deep runner resolution failed: %w", err)
	}

	deepModel, err := protocol.ResolveDeepModel(r.Cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("deep-reviewer model resolution failed: %v", err))
		return deepReviewParams{}, false, fmt.Errorf("deep model resolution failed: %w", err)
	}
	if deepModel == "" {
		var tierErr error
		deepModel, tierErr = protocol.ResolveTierModel(r.Cfg, deepRunner, protocol.DefaultDeepTier)
		if tierErr != nil {
			slog.Warn("deep-reviewer tier model resolution failed, falling back", "runner", deepRunner, "error", tierErr)
		}
	}
	if deepModel == "" {
		newState, err := state.Transition(*s, protocol.PhaseAccepting, protocol.RoleAcceptor)
		if err != nil {
			return deepReviewParams{}, false, fmt.Errorf("skip deep-review transition: %w", err)
		}
		*s = newState
		if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
			return deepReviewParams{}, false, fmt.Errorf("write state (skip deep-review): %w", werr)
		} else if yielded {
			return deepReviewParams{}, false, nil
		}
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner, Detail: fmt.Sprintf("deep_model not configured and runner cannot resolve default tier %q", protocol.DefaultDeepTier),
		})
		fmt.Printf("[round %d] deep-reviewing — skipped (runner cannot resolve default tier %q)\n", s.Round, protocol.DefaultDeepTier)
		return deepReviewParams{}, false, nil
	}

	if rcfg, ok := r.Cfg.Roles[string(protocol.RoleReviewer)]; !ok || rcfg.DeepModel == "" {
		fmt.Printf("[round %d] deep-reviewing — using default tier %q (no explicit deep_model configured)\n", s.Round, protocol.DefaultDeepTier)
	}

	return deepReviewParams{pc: pc, deepRunner: deepRunner, deepModel: deepModel}, true, nil
}

// deepReviewRun 執行 deep review（平行或單 agent），回傳 (cont, err)。
// cont 為 true 表示 deep-review-report.md 已產出，caller 接續 PASS/FAIL 判定。
func (r *Runner) deepReviewRun(ctx context.Context, s *protocol.State, dp deepReviewParams) (bool, error) {
	featureID := r.featureID()
	round := s.Round

	s.Role = protocol.RoleDeepReviewer
	s.SubPhase = protocol.SubPhaseReviewing
	if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
		return false, fmt.Errorf("write state (deep-reviewer): %w", werr)
	} else if yielded {
		return false, nil
	}

	selectedAngles, selection := r.selectDeepReviewAngles()
	writeAngleSelection(r.Ws, featureID, round, selection)

	angleCount := len(selectedAngles)
	fmt.Printf("[round %d] deep-reviewing — selected %d/%d angles\n", round, angleCount, protocol.DeepReviewAngleCount)

	groups := protocol.GroupSelectedAngles(
		protocol.ResolveParallelReviewers(r.Cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(r.Cfg, protocol.RoleDeepReviewer),
		selectedAngles)

	prompt.MarkLearningsUsed(r.Ws.DotDir(), prompt.LoadLearningsForRole(r.Ws.DotDir(), protocol.RoleDeepReviewer))

	if len(groups) > 1 {
		if ok, err := r.runDeepReviewParallel(ctx, s, dp.deepRunner, dp.deepModel, groups, round); !ok || err != nil {
			return ok, err
		}
	} else {
		var opts []prompt.Option
		if angleCount < protocol.DeepReviewAngleCount {
			opts = append(opts, prompt.WithSelectedAngles(selectedAngles))
		}
		if ok, err := r.runDeepSubRole(ctx, s,
			protocol.RoleDeepReviewer, dp.deepRunner, dp.deepModel, runner.LogFileName(round, string(protocol.RoleDeepReviewer)), round, 0, opts...); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(r.Ws, featureID, s, r.Ops, protocol.RoleDeepReviewer); !ok || err != nil {
			return ok, err
		}
	}
	reportPath := filepath.Join(r.Ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	if _, statErr := os.Stat(reportPath); statErr != nil {
		return parallelNeedsAttention(r.Ws, featureID, s, "missing-artifact: "+protocol.DeepReviewReport)
	}
	return true, nil
}

// selectDeepReviewAngles 依 diff 影響的檔案路徑選出應執行的 deep review 角度。
// ForceAllAngles（CLI flag）或 Feature.DeepReviewAllAngles（YAML）為 true 時回傳全部角度。
func (r *Runner) selectDeepReviewAngles() ([]int, protocol.AngleSelection) {
	forceAll := r.ForceAllAngles || r.Feature.DeepReviewAllAngles
	if forceAll {
		return protocol.AllAngles(), protocol.AngleSelection{
			SelectedAngles: protocol.AllAngles(),
			TotalAngles:    protocol.DeepReviewAngleCount,
			ForceAll:       true,
		}
	}

	mapping := protocol.ResolveAngleMapping(r.Cfg)
	files := r.Ops.DetectChangedFiles(r.featureID())
	selected, matches := protocol.SelectDeepReviewAngles(mapping, files)

	return selected, protocol.AngleSelection{
		SelectedAngles: selected,
		TotalAngles:    protocol.DeepReviewAngleCount,
		ForceAll:       false,
		Matches:        matches,
	}
}

// writeAngleSelection 把角度選擇結果寫入 round artifact。
func writeAngleSelection(ws *protocol.Workspace, featureID string, round int, sel protocol.AngleSelection) {
	data, err := json.MarshalIndent(sel, "", "  ")
	if err != nil {
		slog.Warn("marshal angle selection failed", "feature", featureID, "error", err)
		return
	}
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewAnglesFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("write angle selection failed", "feature", featureID, "error", err)
	}
}

// deepReviewClearsToAccepting 判斷本 round 的 deep-review-report.md 是否可直接放行
// accepting/fixing（不進 self-heal）。F144 補洞後的放行規則：
//   - 乾淨 PASS（DeepReviewCleanPass）→ 恆放行（F132「乾淨 PASS 跳 fixer」行為不變）。
//   - CONDITIONAL PASS 帶 warning 且 profile 啟用 fixing → 放行（交 fixer 處理，現況不變）。
//   - CONDITIONAL PASS 帶 warning 但未啟用 fixing → 回 false，改交 deepReviewSelfHeal 至少
//     嘗試修掉 warning（取代過去的靜默放行 accepting）。
//   - FAIL → 回 false，交 deepReviewSelfHeal（現況不變）。
func (r *Runner) deepReviewClearsToAccepting(round int, pc protocol.ProfileConfig) bool {
	if DeepReviewCleanPass(r.Ws, r.featureID(), round) {
		return true
	}
	return ReviewPassed(r.Ws, r.featureID(), round, protocol.DeepReviewReport) && pc.EnablesPhase(protocol.PhaseFixing)
}

// deepReviewSelfHeal 在 deep review FAIL 時跑內部自癒循環（mini-coder + re-verifier），
// 最多跑 maxFixRounds 輪。
func (r *Runner) deepReviewSelfHeal(ctx context.Context, s *protocol.State, dp deepReviewParams) (bool, error) {
	featureID := r.featureID()
	round := s.Round

	maxFix := protocol.ResolveMaxFixRounds(r.Cfg, protocol.RoleDeepReviewer)

	_, coderModelManual := protocol.EffectiveManual(r.RunOverrides, protocol.PhaseCoding, r.ManualRunner)
	coderModel, err := protocol.ResolvePhaseModel(r.Cfg, r.Feature, dp.pc, protocol.PhaseCoding, protocol.RoleCoder, dp.deepRunner, coderModelManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("coder model resolution failed: %v", err))
		return false, fmt.Errorf("coder model resolution failed: %w", err)
	}
	// mini-coder 預設沿用 coder model，但 roles.mini-coder.model 若有設定則優先。
	miniCoderModel, err := protocol.ResolveMiniCoderModel(r.Cfg, dp.deepRunner, coderModel)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("mini-coder model resolution failed: %v", err))
		return false, fmt.Errorf("mini-coder model resolution failed: %w", err)
	}
	reviewModel, err := protocol.ResolveModel(r.Cfg, dp.deepRunner, protocol.RoleReviewer)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("reviewer model resolution failed: %v", err))
		return false, fmt.Errorf("reviewer model resolution failed: %w", err)
	}

	for iter := 1; iter <= maxFix; iter++ {
		fmt.Printf("[round %d] deep-reviewing — self-heal iteration %d/%d\n", round, iter, maxFix)

		s.Role = protocol.RoleMiniCoder
		s.SubPhase = protocol.SubPhaseFixing
		if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
			return false, fmt.Errorf("write state (mini-coder): %w", werr)
		} else if yielded {
			return false, nil
		}
		if ok, err := r.runDeepSubRole(ctx, s,
			protocol.RoleMiniCoder, dp.deepRunner, miniCoderModel, runner.DeepFixLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}

		if r.CommitStrategy == "per-round" && r.RunnerWs.Root != r.Ws.Root {
			if err := r.Ops.Commit(r.RunnerWs.Root, featureID, fmt.Sprintf("wip(%s): round %d deep-fix %d", featureID, round, iter)); err != nil {
				slog.Error("auto-commit deep-fix failed", "feature", featureID, "round", round, "iteration", iter, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "iteration", iter, "strategy", "deep-fix")
			}
		}

		if guardResult := guard.Check(r.Ws, featureID, r.Ops); !guardResult.Pass {
			reason := strings.Join(guardResult.Errors, "; ")
			writeDeepReviewFailReport(r.Ws, featureID, round, "scope-exceed", reason)
			writeDeepEscalation(r.Ws, featureID, round, "scope-change", "mini-coder scope-exceed: "+reason)
			s.Phase = protocol.PhaseNeedsAttention
			StopState(r.Ws, featureID, s, "scope-exceed", "deep-fix scope exceeded: "+reason)
			LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			r.Ws.AppendEvent(featureID, protocol.Event{
				Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleMiniCoder,
				Round: round, Detail: s.StopMessage, Runner: s.Runner,
			})
			return false, nil
		}

		s.Role = protocol.RoleReVerifier
		s.SubPhase = protocol.SubPhaseReverifying
		if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
			return false, fmt.Errorf("write state (re-verifier): %w", werr)
		} else if yielded {
			return false, nil
		}
		if ok, err := r.runDeepSubRole(ctx, s,
			protocol.RoleReVerifier, dp.deepRunner, reviewModel, runner.DeepReverifyLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(r.Ws, featureID, s, r.Ops, protocol.RoleReVerifier); !ok || err != nil {
			return ok, err
		}

		if ReviewPassed(r.Ws, featureID, round, protocol.DeepReviewReport) {
			AutoDiscoverFeatures(ctx, r.Ws, r.Feature, r.Cfg, round, r.newEnrichRunner(dp.deepRunner, round))
			return deepTransitionAccepting(r.Ws, featureID, s, dp.pc)
		}
	}

	AutoDiscoverFeatures(ctx, r.Ws, r.Feature, r.Cfg, round, r.newEnrichRunner(dp.deepRunner, round))
	writeDeepEscalation(r.Ws, featureID, round, "blocker",
		fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	s.Phase = protocol.PhaseNeedsAttention
	StopState(r.Ws, featureID, s, "self-heal-exhausted", fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	r.Ws.AppendEvent(featureID, protocol.Event{
		Type: "escalation", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer,
		Round: round, Detail: s.StopMessage, Runner: s.Runner, Notify: protocol.NotifyWarning,
	})
	fmt.Printf("[round %d] deep-reviewing — self-heal exhausted (%d iterations), escalating\n", round, maxFix)
	return false, nil
}

// parallelOutcome 收納單個平行 sub-reviewer 的執行結果。
type parallelOutcome struct {
	index  int
	result *runner.Result
	err    error
}

// classifyOutcomes 一次遍歷所有平行 sub-reviewer 結果，分離出 runner error、hard error、
// soft fail 與成功的 outcome。回傳的三個 slice 保留原始順序，caller 只需對各類別做一次處理。
func classifyOutcomes(outcomes []parallelOutcome) (runErr *parallelOutcome, hardErr *parallelOutcome, softFail *parallelOutcome) {
	for i := range outcomes {
		o := &outcomes[i]
		if o.err != nil {
			return o, nil, nil
		}
		if runner.IsHardError(o.result) {
			if hardErr == nil {
				hardErr = o
			}
			continue
		}
		if runner.IsSoftFail(o.result) {
			if softFail == nil {
				softFail = o
			}
		}
	}
	return nil, hardErr, softFail
}

// runDeepReviewParallel 在 deep-reviewing phase 內平行 spawn len(groups) 個 sub-reviewer，
// 各自只跑分配到的 review angle 並寫出 deep-review-partial-<i>.md，全部完成後再 spawn 一個
// synthesizer 把所有 partial report 合併成單一 deep-review-report.md（格式與單 agent 完全相同）。
// 全程維持 deep-reviewing phase。sub-reviewer 與 synthesizer 皆 read-only，共用同一 worktree。
//
// 回傳 (ok, err)：語意同 runDeepSubRole；ok 為 true 時 deep-review-report.md 已產出，
// caller 接續走 reviewPassed → accepting / self-heal 分支。
func (r *Runner) runDeepReviewParallel(ctx context.Context, s *protocol.State, runnerName, deepModel string, groups [][]int, round int) (bool, error) {
	ws := r.Ws
	featureID := r.featureID()

	if r.RunnerWs.Root != ws.Root {
		SyncFeatureToWorktree(ws, r.RunnerWs, featureID, round)
	}
	var stopSync func()
	if r.RunnerWs.Root != ws.Root {
		stopSync = StartLiveSync(r.RunnerWs, ws, featureID, round)
	}
	cleanup := func() {
		if stopSync != nil {
			stopSync()
			stopSync = nil
		}
		if r.RunnerWs.Root != ws.Root {
			if serr := SyncFeatureFromWorktree(r.RunnerWs, ws, featureID, round); serr != nil {
				slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
			}
		}
	}

	missing := prompt.MissingDeepPartials(r.RunnerWs.RoundDir(featureID, round), len(groups))

	outcomes := make([]parallelOutcome, len(missing))
	if len(missing) > 0 {
		fmt.Printf("[round %d] deep-reviewing — running %d parallel sub-reviewers (%s, model: %s)\n", round, len(missing), runnerName, deepModel)

		var wg sync.WaitGroup
		for slot, idx := range missing {
			wg.Add(1)
			go func(slot, idx int) {
				defer wg.Done()
				angles := groups[idx-1]
				partialName := prompt.DeepReviewPartialName(idx)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
					Runner: runnerName, Model: deepModel, Index: idx,
				})
				promptText, perr := prompt.Generate(r.promptCtx(), protocol.RoleDeepReviewer, round, 0, runnerName,
					prompt.WithParallelDeepReviewer(idx, len(groups), angles, partialName))
				if perr != nil {
					promptText = fmt.Sprintf("You are deep sub-reviewer %d for feature %s, round %d. Read .4x/%s/ for context.", idx, featureID, round, featureID)
				}
				logPath := filepath.Join(runner.LogDir(ws, featureID), runner.DeepReviewerLogFileName(round, idx))
				rn := r.NewRunner(runnerName, logPath, deepModel)
				setReviewerExtraEnv(rn, protocol.RoleDeepReviewer, featureID, filepath.Join(ws.RoundDir(featureID, round), protocol.ReviewPackage))
				res, runErr := rn.Run(ctx, promptText)
				outcomes[slot] = parallelOutcome{index: idx, result: res, err: runErr}
			}(slot, idx)
		}
		wg.Wait()
	} else {
		fmt.Printf("[round %d] deep-reviewing — all %d partials present, resuming at synthesizer (%s)\n", round, len(groups), runnerName)
	}

	runErrOut, hardErr, softFail := classifyOutcomes(outcomes)
	if runErrOut != nil {
		cleanup()
		if ctx.Err() == context.Canceled {
			StopState(ws, featureID, s, "interrupted", fmt.Sprintf("deep-reviewer interrupted by signal (round %d)", round))
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		StopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-reviewer runner failed (round %d): %v", round, runErrOut.err))
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
			Status: "error", Detail: runErrOut.err.Error(), Runner: runnerName, Model: deepModel, Index: runErrOut.index,
		})
		return false, runErrOut.err
	}

	for _, o := range outcomes {
		subLog := filepath.Join(runner.LogDir(ws, featureID), runner.DeepReviewerLogFileName(round, o.index))
		subTokens, subCost, subCodex := r.runEndMetrics(runnerName, subLog)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: runnerName, Model: deepModel,
			TokensUsed: subTokens, CostUSD: subCost, Index: o.index, Codex: subCodex,
		})
	}

	if hardErr != nil {
		cleanup()
		StopState(ws, featureID, s, "hard-error", fmt.Sprintf("deep-reviewer runner returned hard error (exit 2) (round %d)", round))
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if softFail != nil {
		cleanup()
		s.Phase = protocol.PhaseNeedsAttention
		StopState(ws, featureID, s, "deep-reviewer-soft-fail", fmt.Sprintf("deep-reviewer runner returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round))
		LogSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		return false, nil
	}

	var partials []prompt.IncludeContent
	for i := 1; i <= len(groups); i++ {
		name := prompt.DeepReviewPartialName(i)
		data, rerr := os.ReadFile(filepath.Join(r.RunnerWs.RoundDir(featureID, round), name))
		if rerr != nil {
			cleanup()
			return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+name)
		}
		partials = append(partials, prompt.IncludeContent{Path: name, Content: string(data)})
	}

	ok, err := r.runSynthesizer(ctx, s, runnerName, deepModel, partials, round)
	if !ok || err != nil {
		cleanup()
		return ok, err
	}

	cleanup()
	if ok, err := deepGuardCheck(ws, featureID, s, r.Ops, protocol.RoleDeepReviewer); !ok || err != nil {
		return ok, err
	}
	return true, nil
}

// runSynthesizer spawn synthesizer 把所有 partial report 合併成單一 deep-review-report.md。
func (r *Runner) runSynthesizer(ctx context.Context, s *protocol.State, runnerName, deepModel string, partials []prompt.IncludeContent, round int) (bool, error) {
	ws := r.Ws
	featureID := r.featureID()

	s.Role = protocol.RoleSynthesizer
	s.SubPhase = protocol.SubPhaseSynthesizing
	if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
		return false, fmt.Errorf("write state (synthesizer): %w", werr)
	} else if yielded {
		return false, nil
	}

	synthModel := deepModel
	if m, mErr := protocol.ResolveModel(r.Cfg, runnerName, protocol.RoleSynthesizer); mErr == nil {
		synthModel = m
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Runner: runnerName, Model: synthModel,
	})
	synthPrompt, perr := prompt.Generate(r.promptCtx(), protocol.RoleSynthesizer, round, 0, runnerName,
		prompt.WithSynthesizerReports(partials))
	if perr != nil {
		synthPrompt = fmt.Sprintf("You are the deep review synthesizer for feature %s, round %d. Read .4x/%s/ for context.", featureID, round, featureID)
	}
	synthLog := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(protocol.RoleSynthesizer)))
	synthRunner := r.NewRunner(runnerName, synthLog, synthModel)
	fmt.Printf("[round %d] deep-reviewing (synthesizer) — invoking %s (model: %s)\n", round, runnerName, synthModel)
	synthStart := time.Now()
	synthRes, synthErr := synthRunner.Run(ctx, synthPrompt)
	synthDur := time.Since(synthStart)
	synthTokens, synthCost, synthCodex := r.runEndMetrics(runnerName, synthLog)
	if synthErr != nil {
		if ctx.Err() == context.Canceled {
			StopState(ws, featureID, s, "interrupted", fmt.Sprintf("deep-review synthesizer interrupted by signal (round %d)", round))
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		StopState(ws, featureID, s, "runner-error", fmt.Sprintf("deep-review synthesizer runner failed (round %d): %v", round, synthErr))
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
			Status: "error", Detail: synthErr.Error(), Runner: runnerName, Model: synthModel,
			TokensUsed: synthTokens, CostUSD: synthCost, DurationMs: synthDur.Milliseconds(), Codex: synthCodex,
		})
		return false, synthErr
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Status: fmt.Sprintf("exit-%d", synthRes.ExitCode), Runner: runnerName, Model: synthModel,
		TokensUsed: synthTokens, CostUSD: synthCost, DurationMs: synthDur.Milliseconds(), Codex: synthCodex,
	})
	if runner.IsHardError(synthRes) {
		StopState(ws, featureID, s, "hard-error", fmt.Sprintf("deep-review synthesizer returned hard error (exit 2) (round %d)", round))
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(synthRes) {
		s.Phase = protocol.PhaseNeedsAttention
		StopState(ws, featureID, s, "synthesizer-soft-fail", fmt.Sprintf("deep-review synthesizer returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round))
		LogSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		return false, nil
	}
	return true, nil
}

// runDeepSubRole 在 deep-reviewing phase 內 spawn 一個子 role（deep-reviewer / mini-coder /
// re-verifier）。薄 wrapper，委派給 phase-agnostic 的 runSubRole，phase 固定 deep-reviewing。
// opts 為可選的 prompt Option，用於注入角度選擇等參數。
func (r *Runner) runDeepSubRole(ctx context.Context, s *protocol.State, role protocol.Role, runnerName, model, logName string, round, iteration int, opts ...prompt.Option) (bool, error) {
	return r.runSubRole(ctx, s, protocol.PhaseDeepReviewing, role, runnerName, model, logName, round, iteration, opts...)
}

// runSubRole 在指定 phase 內 spawn 一個子 role，處理 phase-start/run-end event、prompt 產生、
// runner 執行、worktree 同步與 token/cost 統計累加，並分類 context cancel / runner error /
// hard error / soft fail。此機制與 phase 無關——event 的 Phase 欄位與人類可讀訊息前綴皆由傳入
// 的 phase 決定（訊息前綴用 string(phase)，恰為 "deep-reviewing" / "reviewing"）。deep-reviewing
// 自癒循環與 reviewing 同輪收斂共用同一實作，避免錯誤處理／統計邏輯雙邊漂移。
//
// 回傳 (ok, err)：ok 為 true 表示 runner 正常結束，caller 可繼續；ok 為 false 且 err 為 nil
// 表示已寫入終止狀態（needs-attention / blocked）；err 非 nil 表示 hard error 或 cancel。
func (r *Runner) runSubRole(ctx context.Context, s *protocol.State, phase protocol.Phase, role protocol.Role, runnerName, model, logName string, round, iteration int, opts ...prompt.Option) (bool, error) {
	ws := r.Ws
	featureID := r.featureID()
	label := string(phase)

	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: phase, Role: role, Round: round,
		Runner: runnerName, Model: model,
	})

	promptText, err := prompt.Generate(r.promptCtx(), role, round, iteration, runnerName, opts...)
	if err != nil {
		promptText = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
	}
	logPath := filepath.Join(runner.LogDir(ws, featureID), logName)
	rn := r.NewRunner(runnerName, logPath, model)
	setReviewerExtraEnv(rn, role, featureID, filepath.Join(ws.RoundDir(featureID, round), protocol.ReviewPackage))

	if r.RunnerWs.Root != ws.Root {
		SyncFeatureToWorktree(ws, r.RunnerWs, featureID, round)
	}
	var stopSync func()
	if r.RunnerWs.Root != ws.Root {
		stopSync = StartLiveSync(r.RunnerWs, ws, featureID, round)
	}

	if model != "" {
		fmt.Printf("[round %d] %s (%s) — invoking %s (model: %s)\n", round, label, role, runnerName, model)
	} else {
		fmt.Printf("[round %d] %s (%s) — invoking %s\n", round, label, role, runnerName)
	}

	invokeStart := time.Now()
	result, runErr := rn.Run(ctx, promptText)
	invokeDur := time.Since(invokeStart)

	if stopSync != nil {
		stopSync()
	}
	if r.RunnerWs.Root != ws.Root {
		if serr := SyncFeatureFromWorktree(r.RunnerWs, ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "role", role, "error", serr)
		}
	}

	tokens, costUSD, codexUsage := r.runEndMetrics(runnerName, logPath)

	if runErr != nil {
		if ctx.Err() == context.Canceled {
			StopState(ws, featureID, s, "interrupted", fmt.Sprintf("%s (%s) interrupted by signal (round %d)", label, role, round))
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		StopState(ws, featureID, s, "runner-error", fmt.Sprintf("%s (%s) runner failed (round %d): %v", label, role, round, runErr))
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: round,
			Status: "error", Detail: runErr.Error(), Runner: runnerName, Model: model,
			TokensUsed: tokens, CostUSD: costUSD, DurationMs: invokeDur.Milliseconds(), Codex: codexUsage,
		})
		return false, runErr
	}

	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: phase, Role: role, Round: round,
		Status: fmt.Sprintf("exit-%d", result.ExitCode), Runner: runnerName, Model: model,
		TokensUsed: tokens, CostUSD: costUSD, DurationMs: invokeDur.Milliseconds(), Codex: codexUsage,
	})

	if runner.IsHardError(result) {
		StopState(ws, featureID, s, "hard-error", fmt.Sprintf("%s (%s) runner returned hard error (exit 2) (round %d)", label, role, round))
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		StopState(ws, featureID, s, "soft-fail", fmt.Sprintf("%s (%s) runner returned soft-fail (exit %d) (round %d)", label, role, runner.ExitSoftFail, round))
		LogSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
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
	StopState(ws, featureID, s, "guard-fail", guardMsg)
	LogSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: role,
		Round: s.Round, Detail: guardMsg, Runner: s.Runner,
	})
	return false, nil
}

// deepTransitionAccepting 把 state 從 deep-reviewing 推進到下一個 phase 並寫回，
// 供自癒循環在 deep review PASS 時放行。若 profile 啟用 fixing phase 則轉到 fixing，
// 否則轉到 accepting（保持未啟用 fixing 的 profile 行為不變）。
func deepTransitionAccepting(ws *protocol.Workspace, featureID string, s *protocol.State, pc protocol.ProfileConfig) (bool, error) {
	targetPhase := protocol.PhaseAccepting
	targetRole := protocol.RoleAcceptor
	if pc.EnablesPhase(protocol.PhaseFixing) {
		targetPhase = protocol.PhaseFixing
		targetRole = protocol.RoleFixer
	}
	newState, err := state.Transition(*s, targetPhase, targetRole)
	if err != nil {
		return false, fmt.Errorf("deep-review→%s transition: %w", targetPhase, err)
	}
	*s = newState
	s.SubPhase = ""
	// CAS 寫入：deep review 全程數分鐘，期間若外部把 feature 終結（磁碟 Active=false），
	// 放棄推進 accepting/fixing、採用磁碟終態並回 (false, nil) 交主迴圈收尾，避免用 Active=true
	// 的舊快照把已終結的 feature 復活成 running。此函式收 ws 非 *Runner，故就地展開 CAS。
	persisted, err := ws.UpdateState(featureID, func(disk *protocol.State) error {
		if !disk.Active {
			return protocol.ErrSkipStateWrite
		}
		*disk = *s
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("write state (%s): %w", targetPhase, err)
	}
	if !persisted.Active {
		*s = persisted
		return false, nil
	}
	LogSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// newEnrichRunner 為 auto-discover enrichment 構造 runner：沿用 deep-review 的 runner 名稱與
// 較便宜的 reviewer model。enrichment 關閉時回 nil。
func (r *Runner) newEnrichRunner(deepRunner string, round int) runner.Runner {
	if !r.Cfg.EnrichDiscoveredFeatures {
		return nil
	}
	model, err := protocol.ResolveModel(r.Cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		model = ""
	}
	logPath := filepath.Join(runner.LogDir(r.Ws, r.featureID()), fmt.Sprintf("round-%d-enrich.log", round))
	return r.NewRunner(deepRunner, logPath, model)
}

// AutoDiscoverFeatures 在 final deep review PASS 後執行：parse deep-review-report.md 的
// [NEW-FEATURE] 標記，去重、套數量上限後建立新 feature，並寫出 discovered-features.md 摘要。
func AutoDiscoverFeatures(ctx context.Context, ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, round int, r runner.Runner) {
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

type discoveredCreated struct{ id, title string }

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

func writeDeepReviewFailReport(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	content := fmt.Sprintf("# Deep Review Report — Round %d\n\n## Summary\nFAIL — %s\n\n## Issues\n### [CRITICAL] %s\n%s\n\n## Verdict\nFAIL\n",
		round, reason, reason, detail)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Error("write deep-review FAIL report failed", "feature", featureID, "round", round, "error", err)
	}
}

func writeDeepEscalation(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	esc := protocol.Escalation{Needed: true, Reason: reason, Detail: detail}
	data, _ := json.Marshal(esc)
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.EscalationFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Error("write deep-review escalation failed", "feature", featureID, "round", round, "error", err)
	}
}
