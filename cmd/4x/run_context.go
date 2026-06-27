package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/health"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// runContext 收納 run loop 整個生命週期的共用依賴，避免在 runLoop、runReviewTestParallel、
// runDeepReviewPhase 等函式間重複傳遞 8-10 個相同的參數。
type runContext struct {
	ws             *protocol.Workspace
	runnerWs       *protocol.Workspace
	feature        feat.Feature
	cfg            protocol.Config
	ops            gitops.Ops
	newRunner      func(runnerName string, logPath string, model string) runner.Runner
	commitStrategy string
	manualRunner   string
	runOverrides   map[protocol.Phase]protocol.PhaseSpec
	totalTokens    int
	totalCostUSD   float64
}

func (rc *runContext) featureID() string {
	return rc.feature.ID
}

// loop 是重構後的主迴圈，語意與原 runLoop 完全相同。
func (rc *runContext) loop(ctx context.Context, s protocol.State) error {
	if rc.ops == nil {
		rc.ops = gitops.New(rc.ws.Root, rc.ws, rc.cfg)
	}
	featureID := rc.featureID()

	profileName, pc, err := protocol.ResolveProfile(rc.cfg, rc.feature, s.Profile)
	if err != nil {
		return err
	}
	s.Profile = profileName

	if err := rc.initPhase(ctx, &s); err != nil {
		return err
	}

	cleanStaleArtifact(rc.ws, featureID, s.Phase, s.Round)

	if err := rc.ws.ClearStopSignal(featureID); err != nil {
		slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
	}

	designerEscalations := 0
	const maxDesignerEscalations = 2

	var commitWG sync.WaitGroup
	defer commitWG.Wait()

	var pending *promptPrefetch
	var learningsUsageUpdated bool

	for s.Active {
		if shouldBreak := rc.checkStopSignals(ctx, &s); shouldBreak {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			break
		}

		phase := s.Phase
		role := state.PhaseToRole(phase)

		if isTerminalPhase(phase) {
			break
		}

		if !learningsUsageUpdated && phase != protocol.PhaseDesigning && phase != protocol.PhaseDesignReviewing {
			updateLearningsUsage(rc.ws, featureID)
			learningsUsageUpdated = true
		}

		if skipped, err := rc.tryPassThrough(&s, role, pc, profileName); skipped || err != nil {
			if err != nil {
				return err
			}
			continue
		}

		if routed, cont, err := rc.routePhase(ctx, &s, pc); routed {
			if err != nil {
				return err
			}
			if cont {
				continue
			}
			break
		}

		if stop := rc.checkShouldStop(&s); stop {
			return nil
		}

		rc.clearStaleEscalation(phase, &s)
		rc.clearStaleFinalReport(phase, &s)

		if stop, err := rc.runHealthCheck(ctx, &s); err != nil {
			return err
		} else if stop {
			return nil
		}

		if err := rc.captureBaseline(&s); err != nil {
			return err
		}

		phaseRunner, model, err := rc.resolveRunnerAndModel(phase, role, pc, &s)
		if err != nil {
			return err
		}

		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
			Runner: phaseRunner, Model: model,
		})

		slog.Info("phase transition", "feature", featureID, "phase", phase, "role", role, "round", s.Round, "runner", phaseRunner, "model", model)

		prompt := rc.resolvePrompt(&pending, role, &s, phaseRunner)

		logPath := filepath.Join(runner.LogDir(rc.ws, featureID), runner.LogFileName(s.Round, string(role)))
		r := rc.newRunner(phaseRunner, logPath, model)

		commitWG.Wait()

		rc.syncToWorktree(featureID, s.Round)

		var stopSync func()
		if rc.runnerWs.Root != rc.ws.Root {
			stopSync = startLiveSync(rc.runnerWs, rc.ws, featureID, s.Round)
		}

		if model != "" {
			fmt.Printf("[round %d] %s (%s) — invoking %s (model: %s)\n", s.Round, phase, role, phaseRunner, model)
		} else {
			fmt.Printf("[round %d] %s (%s) — invoking %s\n", s.Round, phase, role, phaseRunner)
		}

		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "started")
		invokeStart := time.Now()
		result, runErr := r.Run(ctx, prompt)
		invokeDur := time.Since(invokeStart)
		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "completed", "duration_ms", invokeDur.Milliseconds())

		if stopSync != nil {
			stopSync()
		}
		rc.syncFromWorktree(featureID, s.Round)

		stats := parseRunStatsFromLog(logPath)
		rc.totalTokens += stats.Tokens
		rc.totalCostUSD += stats.CostUSD
		switch {
		case stats.CostUSD > 0:
			fmt.Printf("  → $%.4f, %s\n", stats.CostUSD, invokeDur.Truncate(time.Second))
		case stats.Tokens > 0:
			fmt.Printf("  → %s tokens, %s\n", formatTokens(stats.Tokens), invokeDur.Truncate(time.Second))
		default:
			fmt.Printf("  → %s\n", invokeDur.Truncate(time.Second))
		}
		tokens := stats.Tokens

		if runErr != nil {
			return rc.handleRunnerError(ctx, &s, phase, role, phaseRunner, model, runErr)
		}

		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status:     fmt.Sprintf("exit-%d", result.ExitCode),
			Runner:     phaseRunner, Model: model,
			TokensUsed: tokens, DurationMs: invokeDur.Milliseconds(),
		})

		if err := rc.handleExitResult(result, &s, phase, role, phaseRunner); err != nil {
			return err
		}
		if !s.Active && (s.Phase == protocol.PhaseBlocked) {
			break
		}

		if err := rc.handlePostCoder(ctx, &s, phase, role, phaseRunner, &commitWG); err != nil {
			return err
		}
		if !s.Active {
			break
		}

		next, nextRole, stopReason := nextPhaseAfter(rc.ws, featureID, s)

		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
		}

		if err := rc.executeTransitionHooks(ctx, &s, next, "pre"); err != nil {
			return err
		}

		if next == protocol.PhaseDesigning && phase != protocol.PhaseInit {
			designerEscalations++
			if designerEscalations > maxDesignerEscalations {
				next = protocol.PhaseNeedsAttention
				nextRole = role
				stopReason = fmt.Sprintf("escalation-loop: designer escalated %d times in round %d", designerEscalations, s.Round)
				fmt.Printf("  ⚠ stopping: coder↔designer escalation loop detected (%d times)\n", designerEscalations)
			}
		}

		newState, err := state.Transition(s, next, nextRole)
		if err != nil {
			return fmt.Errorf("loop transition %s→%s: %w", s.Phase, next, err)
		}

		if next == protocol.PhaseAmending {
			applyAmendingProgress(rc.ws, featureID, &newState, s.Round)
		}

		s = newState
		if stopReason != "" {
			s.Active = false
			s.StopMessage = stopReason
			if strings.HasPrefix(stopReason, "missing-artifact:") {
				s.StopReason = "missing-artifact"
			} else if strings.HasPrefix(stopReason, "escalation-loop:") {
				s.StopReason = "escalation"
			} else {
				s.StopReason = "escalation"
			}
		}
		if err := rc.ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)

		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner,
		})

		if err := rc.executeTransitionHooks(ctx, &s, next, "post"); err != nil {
			return err
		}

		if s.Active {
			pending = rc.prefetchPrompt(s, pc)
		}
	}

	return rc.finalize(s)
}

func (rc *runContext) initPhase(ctx context.Context, s *protocol.State) error {
	if s.Phase != protocol.PhaseInit {
		return nil
	}
	featureID := rc.featureID()
	hookLogDir := filepath.Join(rc.ws.FeatureDir(featureID), "hook-logs")
	initHooks := resolveHooks(rc.cfg, rc.feature, protocol.PhaseDesigning)
	if err := executePhaseHooks(ctx, rc.ws, featureID, s, initHooks["pre"], protocol.PhaseDesigning, "pre", hookLogDir); err != nil {
		return err
	}

	ns, err := state.Transition(*s, protocol.PhaseDesigning, protocol.RoleDesigner)
	if err != nil {
		return err
	}
	*s = ns
	if err := rc.ws.WriteState(featureID, *s); err != nil {
		return fmt.Errorf("write state (init→designing): %w", err)
	}
	if err := rc.ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
		slog.Warn("sync feature status failed", "feature", featureID, "phase", s.Phase, "error", err)
	}

	if err := executePhaseHooks(ctx, rc.ws, featureID, s, initHooks["post"], protocol.PhaseDesigning, "post", hookLogDir); err != nil {
		return err
	}
	return nil
}

// checkStopSignals 檢查 context cancel 與 MCP stop 請求，回傳 true 表示應跳出主迴圈。
func (rc *runContext) checkStopSignals(ctx context.Context, s *protocol.State) bool {
	featureID := rc.featureID()
	if ctx.Err() != nil {
		s.Active = false
		s.StopReason = "interrupted"
		s.StopMessage = fmt.Sprintf("%s phase interrupted by signal (round %d)", s.Phase, s.Round)
		if err := rc.ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		return true
	}

	if rc.ws.StopRequested(featureID) {
		s.Active = false
		s.StopReason = "mcp-stop"
		if err := rc.ws.ClearStopSignal(featureID); err != nil {
			slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
		}
		if err := rc.ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		return true
	}
	return false
}

func isTerminalPhase(phase protocol.Phase) bool {
	return phase == protocol.PhaseDone ||
		phase == protocol.PhasePendingReview ||
		phase == protocol.PhaseBlocked ||
		phase == protocol.PhaseNeedsAttention ||
		phase == protocol.PhaseAbandoned
}

// tryPassThrough 在 role 不在 active profile 時跳過該 phase。
// 回傳 (true, nil) 表示已跳過（主迴圈應 continue）；(false, nil) 表示不適用（走一般路徑）；
// (_, err) 表示 transition / write 失敗。
func (rc *runContext) tryPassThrough(s *protocol.State, role protocol.Role, pc protocol.ProfileConfig, profileName string) (bool, error) {
	if role == "" || pc.EnablesRole(role) {
		return false, nil
	}
	featureID := rc.featureID()
	phase := s.Phase
	next, nextRole := successorPhase(phase)
	newState, err := state.Transition(*s, next, nextRole)
	if err != nil {
		return false, fmt.Errorf("pass-through transition %s→%s: %w", phase, next, err)
	}
	*s = newState
	if err := rc.ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (skip %s): %w", phase, err)
	}
	logSyncErr(rc.ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	rc.ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-skipped", Phase: phase, Role: role, Round: s.Round,
		Runner: s.Runner, Detail: "role not in profile " + profileName,
	})
	fmt.Printf("[round %d] %s — skipped (not in profile %s)\n", s.Round, phase, profileName)
	return true, nil
}

// routePhase 處理需要特殊路徑的 phase（parallel review/test、deep-reviewing）。
// 回傳 (routed, cont, err)：routed 為 true 表示該 phase 已由特殊路徑接管；
// 此時 cont 為 true 表示主迴圈應 continue，cont 為 false 表示應 break。
// routed 為 false 表示一般 phase，主迴圈繼續走一般路徑。
func (rc *runContext) routePhase(ctx context.Context, s *protocol.State, pc protocol.ProfileConfig) (routed, cont bool, err error) {
	phase := s.Phase

	if phase == protocol.PhaseReviewing && rc.cfg.ParallelReviewTest &&
		pc.EnablesRole(protocol.RoleReviewer) && pc.EnablesRole(protocol.RoleTester) {
		cont, err = runReviewTestParallel(ctx, rc, s, pc)
		return true, cont, err
	}

	if phase == protocol.PhaseDeepReviewing {
		cont, err = rc.runDeepReviewPhase(ctx, s)
		return true, cont, err
	}

	return false, false, nil
}

func (rc *runContext) checkShouldStop(s *protocol.State) bool {
	featureID := rc.featureID()
	if stop, reason := state.ShouldStop(*s); stop {
		s.Active = false
		s.StopReason = "no-progress"
		s.StopMessage = reason
		s.Phase = protocol.PhaseNeedsAttention
		if err := rc.ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		if err := rc.ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
			slog.Warn("sync feature status failed", "feature", featureID, "error", err)
		}
		rc.ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner, Notify: protocol.NotifyWarning})
		slog.Info("run stopped", "feature", featureID, "reason", reason, "round", s.Round)
		fmt.Printf("  stopped: %s\n", reason)
		return true
	}
	return false
}

func (rc *runContext) clearStaleEscalation(phase protocol.Phase, s *protocol.State) {
	if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
		os.Remove(filepath.Join(rc.ws.RoundDir(rc.featureID(), s.Round), protocol.EscalationFile))
	}
}

func (rc *runContext) clearStaleFinalReport(phase protocol.Phase, s *protocol.State) {
	if phase == protocol.PhaseTesting || phase == protocol.PhaseAmending {
		os.Remove(filepath.Join(rc.ws.FeatureDir(rc.featureID()), protocol.FinalReport))
	}
}

// runHealthCheck 在 testing phase 啟動前跑環境 health check。
// 回傳 (true, nil) 表示 health check 失敗且已 escalate（主迴圈應 return nil）；
// 回傳 (false, nil) 表示通過或不適用；回傳 (_, err) 表示 hard error。
func (rc *runContext) runHealthCheck(ctx context.Context, s *protocol.State) (bool, error) {
	if s.Phase != protocol.PhaseTesting {
		return false, nil
	}
	featureID := rc.featureID()
	testStrat, tsErr := rc.ws.ReadTestStrategy(featureID)
	if tsErr != nil {
		slog.Warn("read test-strategy failed", "feature", featureID, "error", tsErr)
	}
	hc := health.ResolveHealthCheck(rc.cfg.HealthCheck, testStrat.HealthCheck)
	if hc == nil {
		return false, nil
	}
	fmt.Printf("[round %d] testing — running health check\n", s.Round)
	if err := health.RunHealthCheck(*hc, healthCheckExecutor(ctx, hc.Timeout)); err != nil {
		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "health-check-failed", Phase: s.Phase, Role: protocol.RoleTester,
			Round: s.Round, Detail: err.Error(), Runner: s.Runner,
		})
		newState, transErr := state.Transition(*s, protocol.PhaseNeedsAttention, "")
		if transErr != nil {
			return false, fmt.Errorf("health check transition: %w", transErr)
		}
		*s = newState
		stopState(rc.ws, featureID, s, "health-check-failed", err.Error())
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
		slog.Info("run stopped", "feature", featureID, "reason", "health-check-failed", "round", s.Round)
		fmt.Printf("  health check failed, escalated to needs-attention\n")
		return true, nil
	}
	fmt.Printf("[round %d] testing — health check passed\n", s.Round)
	return false, nil
}

func (rc *runContext) captureBaseline(s *protocol.State) error {
	if s.Phase == protocol.PhaseCoding && s.Round == 1 {
		return captureBaselineOnce(rc.ws, rc.ops, rc.featureID(), rc.feature.Repos)
	}
	return nil
}

func (rc *runContext) resolveRunnerAndModel(phase protocol.Phase, role protocol.Role, pc protocol.ProfileConfig, s *protocol.State) (string, string, error) {
	featureID := rc.featureID()
	runnerManual, modelManual := protocol.EffectiveManual(rc.runOverrides, phase, rc.manualRunner)
	phaseRunner, err := protocol.ResolvePhaseRunner(rc.cfg, rc.feature, pc, phase, runnerManual)
	if err != nil {
		stopState(rc.ws, featureID, s, "runner-error", fmt.Sprintf("runner resolution for %s failed: %v", phase, err))
		return "", "", fmt.Errorf("runner resolution failed: %w", err)
	}
	model, err := protocol.ResolvePhaseModel(rc.cfg, rc.feature, pc, phase, role, phaseRunner, modelManual)
	if err != nil {
		stopState(rc.ws, featureID, s, "model-error", fmt.Sprintf("model resolution for %s failed: %v", role, err))
		return "", "", fmt.Errorf("model resolution failed: %w", err)
	}
	return phaseRunner, model, nil
}

func (rc *runContext) resolvePrompt(pending **promptPrefetch, role protocol.Role, s *protocol.State, phaseRunner string) string {
	featureID := rc.featureID()
	var prompt string
	gotPrefetch := false
	if *pending != nil && (*pending).role == role && (*pending).round == s.Round {
		res := <-(*pending).ch
		if res.err == nil {
			prompt = res.prompt
			gotPrefetch = true
		}
	}
	*pending = nil
	if !gotPrefetch {
		p, gerr := generatePrompt(rc, role, s.Round, 0, phaseRunner)
		if gerr != nil {
			p = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
		}
		prompt = p
	}
	return prompt
}

func (rc *runContext) syncToWorktree(featureID string, round int) {
	if rc.runnerWs.Root != rc.ws.Root {
		syncFeatureToWorktree(rc.ws, rc.runnerWs, featureID, round)
	}
}

func (rc *runContext) syncFromWorktree(featureID string, round int) {
	if rc.runnerWs.Root != rc.ws.Root {
		if serr := syncFeatureFromWorktree(rc.runnerWs, rc.ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
		}
	}
}

func (rc *runContext) handleRunnerError(ctx context.Context, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner, model string, runErr error) error {
	featureID := rc.featureID()
	if ctx.Err() == context.Canceled {
		stopState(rc.ws, featureID, s, "interrupted", fmt.Sprintf("%s (%s) interrupted by signal (round %d)", role, phase, s.Round))
		return ctx.Err()
	}
	s.Phase = protocol.PhaseNeedsAttention
	stopState(rc.ws, featureID, s, "runner-error", fmt.Sprintf("%s runner failed during %s (round %d): %v", role, phase, s.Round, runErr))
	rc.ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: phase, Role: role, Round: s.Round,
		Status: "error", Detail: runErr.Error(),
		Runner: phaseRunner, Model: model,
	})
	return runErr
}

// handleExitResult 處理 runner 正常結束後的 hard error 與 soft fail。
// 回傳 non-nil error 表示 hard error（主迴圈應 return）；
// soft fail 時就地修改 s 並回 nil（主迴圈檢查 s.Active/Phase 決定 break）。
func (rc *runContext) handleExitResult(result *runner.Result, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner string) error {
	featureID := rc.featureID()
	if runner.IsHardError(result) {
		stopState(rc.ws, featureID, s, "hard-error", fmt.Sprintf("%s runner returned hard error (exit 2) during %s (round %d)", role, phase, s.Round))
		return fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		stopState(rc.ws, featureID, s, "soft-fail", fmt.Sprintf("%s runner returned soft-fail (exit %d) during %s (round %d)", role, runner.ExitSoftFail, phase, s.Round))
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
	}
	return nil
}

// handlePostCoder 在 coding/amending phase runner 完成後跑 guard check 與背景 auto-commit。
// guard 失敗時就地修改 s（Active=false, Phase=NeedsAttention）並回 nil。
func (rc *runContext) handlePostCoder(ctx context.Context, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner string, commitWG *sync.WaitGroup) error {
	if phase != protocol.PhaseCoding && phase != protocol.PhaseAmending {
		return nil
	}
	featureID := rc.featureID()
	guardResult := guard.Check(rc.ws, featureID, rc.ops)

	if guardResult.SelfModTouched {
		s.SelfModTouched = true
		s.SelfModPaths = guardResult.SelfModPaths
		logStateWriteErr(rc.ws.WriteState(featureID, *s), featureID, s.Phase)
		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "self-mod-detected", Phase: phase, Role: role, Round: s.Round,
			Detail: strings.Join(guardResult.SelfModPaths, ", "), Runner: phaseRunner, Notify: protocol.NotifyWarning,
		})
	}

	if !guardResult.Pass {
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		guardMsg := strings.Join(guardResult.Errors, "; ")
		s.StopReason = "guard-fail"
		s.StopMessage = guardMsg
		logStateWriteErr(rc.ws.WriteState(featureID, *s), featureID, s.Phase)
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		rc.ws.AppendEvent(featureID, protocol.Event{
			Type: "guard-fail", Phase: phase, Role: role, Round: s.Round,
			Detail: guardMsg, Runner: phaseRunner, Notify: protocol.NotifyError,
		})
		return nil
	}

	if rc.commitStrategy == "per-round" && rc.runnerWs.Root != rc.ws.Root {
		commitWG.Add(1)
		go func(wtRoot string, round int) {
			defer commitWG.Done()
			if err := rc.ops.Commit(wtRoot, featureID, fmt.Sprintf("wip(%s): round %d", featureID, round)); err != nil {
				slog.Error("auto-commit failed", "feature", featureID, "round", round, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "strategy", "per-round")
			}
		}(rc.runnerWs.Root, s.Round)
	}
	return nil
}

func (rc *runContext) executeTransitionHooks(ctx context.Context, s *protocol.State, targetPhase protocol.Phase, timing string) error {
	featureID := rc.featureID()
	hooks := resolveHooks(rc.cfg, rc.feature, targetPhase)
	hookLogDir := filepath.Join(rc.ws.FeatureDir(featureID), "hook-logs")
	return executePhaseHooks(ctx, rc.ws, featureID, s, hooks[timing], targetPhase, timing, hookLogDir)
}

func (rc *runContext) prefetchPrompt(s protocol.State, pc protocol.ProfileConfig) *promptPrefetch {
	nextRole := state.PhaseToRole(s.Phase)
	if !prefetchablePhase(s.Phase, rc.cfg) || nextRole == "" || !pc.EnablesRole(nextRole) {
		return nil
	}
	ch := make(chan promptResult, 1)
	pf := &promptPrefetch{role: nextRole, round: s.Round, ch: ch}
	go func(role protocol.Role, round int) {
		p, gerr := generatePrompt(rc, role, round, 0, rc.manualRunner)
		ch <- promptResult{prompt: p, err: gerr}
	}(nextRole, s.Round)
	return pf
}

func (rc *runContext) finalize(s protocol.State) error {
	featureID := rc.featureID()
	switch s.Phase {
	case protocol.PhasePendingReview:
		harvestLearnings(rc.ws, featureID)
		s.Active = false
		s.StopReason = "pending-review"
		logStateWriteErr(rc.ws.WriteState(featureID, s), featureID, s.Phase)
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, protocol.PhasePendingReview), featureID, protocol.PhasePendingReview)
		rc.printRunSummary(featureID, s.Round)
		fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, s.Round, featureID)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		logStateWriteErr(rc.ws.WriteState(featureID, s), featureID, s.Phase)
		logSyncErr(rc.ws.SyncFeatureStatus(featureID, protocol.PhaseDone), featureID, protocol.PhaseDone)
		rc.printRunSummary(featureID, s.Round)
		fmt.Printf("\nFeature %s complete (%d rounds)\n", featureID, s.Round)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			if s.StopMessage == "" {
				s.StopMessage = fmt.Sprintf("%s stopped with %s (round %d)", featureID, s.Phase, s.Round)
			}
			logStateWriteErr(rc.ws.WriteState(featureID, s), featureID, s.Phase)
		}
	}
	return nil
}

func (rc *runContext) printRunSummary(featureID string, rounds int) {
	switch {
	case rc.totalCostUSD > 0:
		fmt.Printf("\n── %s: %d rounds, $%.4f total ──\n", featureID, rounds, rc.totalCostUSD)
	case rc.totalTokens > 0:
		fmt.Printf("\n── %s: %d rounds, %s tokens total ──\n", featureID, rounds, formatTokens(rc.totalTokens))
	}
}

// runStats 是從 runner log 解析出的執行統計
type runStats struct {
	Tokens  int
	CostUSD float64
}

// parseRunStatsFromLog 從 runner log 尾端解析 token 使用量與成本。
// 支援兩種格式：
//   - stream-json: [result] success (325.5s, $2.2826)
//   - 傳統 Claude Code: tokens used\n73,204
func parseRunStatsFromLog(logPath string) runStats {
	f, err := os.Open(logPath)
	if err != nil {
		return runStats{}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return runStats{}
	}

	readSize := int64(4096)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	if _, err := f.Seek(-readSize, io.SeekEnd); err != nil {
		return runStats{}
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	var stats runStats
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// stream-json: [result] success (325.5s, $2.2826)
		if strings.HasPrefix(trimmed, "[result] ") {
			if idx := strings.Index(trimmed, "$"); idx >= 0 {
				costStr := trimmed[idx+1:]
				costStr = strings.TrimRight(costStr, ")")
				if c, err := strconv.ParseFloat(costStr, 64); err == nil {
					stats.CostUSD = c
				}
			}
		}

		// 傳統 Claude Code: tokens used\n73,204
		if trimmed == "tokens used" && i+1 < len(lines) {
			raw := strings.ReplaceAll(strings.TrimSpace(lines[i+1]), ",", "")
			if n, err := strconv.Atoi(raw); err == nil {
				stats.Tokens = n
			}
		}
	}
	return stats
}

// formatTokens 把 token 數量格式化為帶千分位逗號的字串（如 73204 → "73,204"）。
func formatTokens(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	start := len(s) % 3
	if start > 0 {
		b.WriteString(s[:start])
	}
	for i := start; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
