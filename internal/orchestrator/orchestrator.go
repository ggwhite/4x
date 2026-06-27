package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/health"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// Config 收納 run loop 所需的外部依賴與設定，由 CLI 層組裝後傳入
type Config struct {
	Ws             *protocol.Workspace
	RunnerWs       *protocol.Workspace
	Feature        feat.Feature
	Cfg            protocol.Config
	Ops            gitops.Ops
	NewRunner      func(runnerName string, logPath string, model string) runner.Runner
	CommitStrategy string
	ManualRunner   string
	RunOverrides   map[protocol.Phase]protocol.PhaseSpec
	// ForceAllAngles 為 true 時強制 deep review 跑全部角度，忽略 angle mapping 篩選。
	ForceAllAngles bool
}

// Result 收納 run loop 的執行結果，供 CLI 層印出摘要
type Result struct {
	TotalTokens  int
	TotalCostUSD float64
}

// Runner 是 run loop 的編排引擎，收納生命週期的共用依賴
type Runner struct {
	Config
	totalTokens  int
	totalCostUSD float64
}

// NewRunner 建立新的 Runner 實例
func NewRunner(cfg Config) *Runner {
	return &Runner{Config: cfg}
}

func (r *Runner) featureID() string {
	return r.Feature.ID
}

// promptCtx 從 Runner 組裝 prompt.Context
func (r *Runner) promptCtx() *prompt.Context {
	return &prompt.Context{
		Ws:       r.Ws,
		RunnerWs: r.RunnerWs,
		Feature:  r.Feature,
		Cfg:      r.Cfg,
	}
}

// RunStats 是從 runner log 解析出的執行統計
type RunStats struct {
	Tokens  int
	CostUSD float64
}

// CheckDependencies 檢查 feature 的依賴是否已滿足，未通過時回傳 error
func CheckDependencies(ws *protocol.Workspace, featureID string) error {
	result := guard.CheckDependencies(ws, featureID)
	if !result.Pass {
		for _, e := range result.Errors {
			slog.Warn("dependency blocked", "feature", featureID, "reason", e)
		}
		return fmt.Errorf("feature %s has unmet dependencies", featureID)
	}
	return nil
}

// PhaseToRole 回傳指定 phase 對應的 role
func PhaseToRole(phase protocol.Phase) protocol.Role {
	return state.PhaseToRole(phase)
}

// RecoverState 檢查是否需要 resume recovery，若需要則依 artifacts 推斷正確的 phase 並修正 state。
// 回傳修正後的 state（可能與輸入相同）與 error。
func RecoverState(ws *protocol.Workspace, featureID string, s protocol.State, cfg protocol.Config) (protocol.State, error) {
	if !NeedsResumeRecovery(s) {
		return s, nil
	}
	resumePhase, resumeRole, resumeSub := SmartResumePhase(ws, featureID, s.Round, cfg)
	if resumePhase != s.Phase {
		fmt.Printf("  recovering %s → %s (round %d, max rounds: %d)\n", s.Phase, resumePhase, s.Round, s.MaxRounds)
		ns, err := state.RecoverTo(s, resumePhase, resumeRole)
		if err != nil {
			return s, fmt.Errorf("recovery transition %s → %s: %w", s.Phase, resumePhase, err)
		}
		s = ns
	}
	s.SubPhase = resumeSub
	return s, nil
}

// RunLoop 執行 design-code-review-test 主迴圈，回傳執行結果與錯誤
func (r *Runner) RunLoop(ctx context.Context, s protocol.State) (*Result, error) {
	if r.Ops == nil {
		r.Ops = gitops.New(r.Ws.Root, r.Ws, r.Cfg)
	}
	featureID := r.featureID()

	profileName, pc, err := protocol.ResolveProfile(r.Cfg, r.Feature, s.Profile)
	if err != nil {
		return nil, err
	}
	s.Profile = profileName

	if err := r.initPhase(ctx, &s); err != nil {
		return nil, err
	}

	CleanStaleArtifact(r.Ws, featureID, s.Phase, s.Round)

	if err := r.Ws.ClearStopSignal(featureID); err != nil {
		slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
	}

	designerEscalations := 0
	const maxDesignerEscalations = 2

	var commitWG sync.WaitGroup
	defer commitWG.Wait()

	var pending *prompt.Prefetch
	var learningsUsageUpdated bool

	for s.Active {
		if shouldBreak := r.checkStopSignals(ctx, &s); shouldBreak {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			break
		}

		phase := s.Phase
		role := state.PhaseToRole(phase)

		if IsTerminalPhase(phase) {
			break
		}

		if !learningsUsageUpdated && phase != protocol.PhaseDesigning && phase != protocol.PhaseDesignReviewing {
			prompt.UpdateLearningsUsage(r.Ws, featureID)
			learningsUsageUpdated = true
		}

		if skipped, err := r.tryPassThrough(&s, role, pc, profileName); skipped || err != nil {
			if err != nil {
				return nil, err
			}
			continue
		}

		if routed, cont, err := r.routePhase(ctx, &s, pc); routed {
			if err != nil {
				return nil, err
			}
			if cont {
				continue
			}
			break
		}

		if stop := r.checkShouldStop(&s); stop {
			result := &Result{TotalTokens: r.totalTokens, TotalCostUSD: r.totalCostUSD}
			return result, nil
		}

		r.clearStaleEscalation(phase, &s)
		r.clearStaleFinalReport(phase, &s)

		if stop, err := r.runHealthCheck(ctx, &s); err != nil {
			return nil, err
		} else if stop {
			result := &Result{TotalTokens: r.totalTokens, TotalCostUSD: r.totalCostUSD}
			return result, nil
		}

		if err := r.captureBaseline(&s); err != nil {
			return nil, err
		}

		phaseRunner, model, err := r.resolveRunnerAndModel(phase, role, pc, &s)
		if err != nil {
			return nil, err
		}

		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
			Runner: phaseRunner, Model: model,
		})

		slog.Info("phase transition", "feature", featureID, "phase", phase, "role", role, "round", s.Round, "runner", phaseRunner, "model", model)

		promptText := r.resolvePrompt(&pending, role, &s, phaseRunner)

		logPath := filepath.Join(runner.LogDir(r.Ws, featureID), runner.LogFileName(s.Round, string(role)))
		rn := r.NewRunner(phaseRunner, logPath, model)

		commitWG.Wait()

		r.syncToWorktree(featureID, s.Round)

		var stopSync func()
		if r.RunnerWs.Root != r.Ws.Root {
			stopSync = StartLiveSync(r.RunnerWs, r.Ws, featureID, s.Round)
		}

		if model != "" {
			fmt.Printf("[round %d] %s (%s) — invoking %s (model: %s)\n", s.Round, phase, role, phaseRunner, model)
		} else {
			fmt.Printf("[round %d] %s (%s) — invoking %s\n", s.Round, phase, role, phaseRunner)
		}

		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "started")
		invokeStart := time.Now()
		result, runErr := rn.Run(ctx, promptText)
		invokeDur := time.Since(invokeStart)
		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "completed", "duration_ms", invokeDur.Milliseconds())

		if stopSync != nil {
			stopSync()
		}
		r.syncFromWorktree(featureID, s.Round)

		stats := ParseRunStatsFromLog(logPath)
		r.totalTokens += stats.Tokens
		r.totalCostUSD += stats.CostUSD
		switch {
		case stats.CostUSD > 0:
			fmt.Printf("  → $%.4f, %s\n", stats.CostUSD, invokeDur.Truncate(time.Second))
		case stats.Tokens > 0:
			fmt.Printf("  → %s tokens, %s\n", FormatTokens(stats.Tokens), invokeDur.Truncate(time.Second))
		default:
			fmt.Printf("  → %s\n", invokeDur.Truncate(time.Second))
		}
		tokens := stats.Tokens

		if runErr != nil {
			return nil, r.handleRunnerError(ctx, &s, phase, role, phaseRunner, model, runErr)
		}

		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status: fmt.Sprintf("exit-%d", result.ExitCode),
			Runner: phaseRunner, Model: model,
			TokensUsed: tokens, DurationMs: invokeDur.Milliseconds(),
		})

		if err := r.handleExitResult(result, &s, phase, role, phaseRunner); err != nil {
			return nil, err
		}
		if !s.Active && (s.Phase == protocol.PhaseBlocked) {
			break
		}

		if err := r.handlePostCoder(ctx, &s, phase, role, phaseRunner, &commitWG); err != nil {
			return nil, err
		}
		if !s.Active {
			break
		}

		next, nextRole, stopReason := NextPhaseAfter(r.Ws, featureID, s)

		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
		}

		if err := r.executeTransitionHooks(ctx, &s, next, "pre"); err != nil {
			return nil, err
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
			return nil, fmt.Errorf("loop transition %s→%s: %w", s.Phase, next, err)
		}

		if next == protocol.PhaseAmending {
			ApplyAmendingProgress(r.Ws, featureID, &newState, s.Round)
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
		if err := r.Ws.WriteState(featureID, s); err != nil {
			return nil, fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)

		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner,
		})

		if err := r.executeTransitionHooks(ctx, &s, next, "post"); err != nil {
			return nil, err
		}

		if s.Active {
			pending = r.prefetchPrompt(s, pc)
		}
	}

	if err := r.finalize(s); err != nil {
		return nil, err
	}

	if (s.Phase == protocol.PhasePendingReview || s.Phase == protocol.PhaseDone) && prompt.NeedConsolidate(r.Ws) {
		r.runConsolidate(ctx, s)
	}

	return &Result{TotalTokens: r.totalTokens, TotalCostUSD: r.totalCostUSD}, nil
}

func (r *Runner) initPhase(ctx context.Context, s *protocol.State) error {
	if s.Phase != protocol.PhaseInit {
		return nil
	}
	featureID := r.featureID()
	hookLogDir := filepath.Join(r.Ws.FeatureDir(featureID), "hook-logs")
	initHooks := ResolveHooks(r.Cfg, r.Feature, protocol.PhaseDesigning)
	if err := ExecutePhaseHooks(ctx, r.Ws, featureID, s, initHooks["pre"], protocol.PhaseDesigning, "pre", hookLogDir); err != nil {
		return err
	}

	ns, err := state.Transition(*s, protocol.PhaseDesigning, protocol.RoleDesigner)
	if err != nil {
		return err
	}
	*s = ns
	if err := r.Ws.WriteState(featureID, *s); err != nil {
		return fmt.Errorf("write state (init→designing): %w", err)
	}
	if err := r.Ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
		slog.Warn("sync feature status failed", "feature", featureID, "phase", s.Phase, "error", err)
	}

	if err := ExecutePhaseHooks(ctx, r.Ws, featureID, s, initHooks["post"], protocol.PhaseDesigning, "post", hookLogDir); err != nil {
		return err
	}
	return nil
}

// checkStopSignals 檢查 context cancel 與 MCP stop 請求，回傳 true 表示應跳出主迴圈
func (r *Runner) checkStopSignals(ctx context.Context, s *protocol.State) bool {
	featureID := r.featureID()
	if ctx.Err() != nil {
		s.Active = false
		s.StopReason = "interrupted"
		s.StopMessage = fmt.Sprintf("%s phase interrupted by signal (round %d)", s.Phase, s.Round)
		if err := r.Ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		return true
	}

	if r.Ws.StopRequested(featureID) {
		s.Active = false
		s.StopReason = "mcp-stop"
		if err := r.Ws.ClearStopSignal(featureID); err != nil {
			slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
		}
		if err := r.Ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		return true
	}
	return false
}

// tryPassThrough 在 role 不在 active profile 時跳過該 phase。
// 回傳 (true, nil) 表示已跳過（主迴圈應 continue）；(false, nil) 表示不適用（走一般路徑）；
// (_, err) 表示 transition / write 失敗。
func (r *Runner) tryPassThrough(s *protocol.State, role protocol.Role, pc protocol.ProfileConfig, profileName string) (bool, error) {
	if role == "" || pc.EnablesRole(role) {
		return false, nil
	}
	featureID := r.featureID()
	phase := s.Phase
	next, nextRole := SuccessorPhase(phase)
	newState, err := state.Transition(*s, next, nextRole)
	if err != nil {
		return false, fmt.Errorf("pass-through transition %s→%s: %w", phase, next, err)
	}
	*s = newState
	if err := r.Ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (skip %s): %w", phase, err)
	}
	LogSyncErr(r.Ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	r.Ws.AppendEvent(featureID, protocol.Event{
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
func (r *Runner) routePhase(ctx context.Context, s *protocol.State, pc protocol.ProfileConfig) (routed, cont bool, err error) {
	phase := s.Phase

	if phase == protocol.PhaseReviewing && r.Cfg.ParallelReviewTest &&
		pc.EnablesRole(protocol.RoleReviewer) && pc.EnablesRole(protocol.RoleTester) {
		cont, err = RunReviewTestParallel(ctx, r, s, pc)
		return true, cont, err
	}

	if phase == protocol.PhaseDeepReviewing {
		cont, err = r.runDeepReviewPhase(ctx, s)
		return true, cont, err
	}

	return false, false, nil
}

func (r *Runner) checkShouldStop(s *protocol.State) bool {
	featureID := r.featureID()
	if stop, reason := state.ShouldStop(*s); stop {
		s.Active = false
		s.StopReason = "no-progress"
		s.StopMessage = reason
		s.Phase = protocol.PhaseNeedsAttention
		if err := r.Ws.WriteState(featureID, *s); err != nil {
			slog.Warn("write state failed", "feature", featureID, "error", err)
		}
		if err := r.Ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
			slog.Warn("sync feature status failed", "feature", featureID, "error", err)
		}
		r.Ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner, Notify: protocol.NotifyWarning})
		slog.Info("run stopped", "feature", featureID, "reason", reason, "round", s.Round)
		fmt.Printf("  stopped: %s\n", reason)
		return true
	}
	return false
}

func (r *Runner) clearStaleEscalation(phase protocol.Phase, s *protocol.State) {
	if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
		os.Remove(filepath.Join(r.Ws.RoundDir(r.featureID(), s.Round), protocol.EscalationFile))
	}
}

func (r *Runner) clearStaleFinalReport(phase protocol.Phase, s *protocol.State) {
	if phase == protocol.PhaseTesting || phase == protocol.PhaseAmending {
		os.Remove(filepath.Join(r.Ws.FeatureDir(r.featureID()), protocol.FinalReport))
	}
}

// runHealthCheck 在 testing phase 啟動前跑環境 health check。
// 回傳 (true, nil) 表示 health check 失敗且已 escalate（主迴圈應 return nil）；
// 回傳 (false, nil) 表示通過或不適用；回傳 (_, err) 表示 hard error。
func (r *Runner) runHealthCheck(ctx context.Context, s *protocol.State) (bool, error) {
	if s.Phase != protocol.PhaseTesting {
		return false, nil
	}
	featureID := r.featureID()
	testStrat, tsErr := r.Ws.ReadTestStrategy(featureID)
	if tsErr != nil {
		slog.Warn("read test-strategy failed", "feature", featureID, "error", tsErr)
	}
	hc := health.ResolveHealthCheck(r.Cfg.HealthCheck, testStrat.HealthCheck)
	if hc == nil {
		return false, nil
	}
	fmt.Printf("[round %d] testing — running health check\n", s.Round)
	if err := health.RunHealthCheck(*hc, healthCheckExecutor(ctx, hc.Timeout)); err != nil {
		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "health-check-failed", Phase: s.Phase, Role: protocol.RoleTester,
			Round: s.Round, Detail: err.Error(), Runner: s.Runner,
		})
		newState, transErr := state.Transition(*s, protocol.PhaseNeedsAttention, "")
		if transErr != nil {
			return false, fmt.Errorf("health check transition: %w", transErr)
		}
		*s = newState
		StopState(r.Ws, featureID, s, "health-check-failed", err.Error())
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
		slog.Info("run stopped", "feature", featureID, "reason", "health-check-failed", "round", s.Round)
		fmt.Printf("  health check failed, escalated to needs-attention\n")
		return true, nil
	}
	fmt.Printf("[round %d] testing — health check passed\n", s.Round)
	return false, nil
}

func (r *Runner) captureBaseline(s *protocol.State) error {
	if s.Phase == protocol.PhaseCoding && s.Round == 1 {
		return CaptureBaselineOnce(r.Ws, r.Ops, r.featureID(), r.Feature.Repos)
	}
	return nil
}

func (r *Runner) resolveRunnerAndModel(phase protocol.Phase, role protocol.Role, pc protocol.ProfileConfig, s *protocol.State) (string, string, error) {
	featureID := r.featureID()
	runnerManual, modelManual := protocol.EffectiveManual(r.RunOverrides, phase, r.ManualRunner)
	phaseRunner, err := protocol.ResolvePhaseRunner(r.Cfg, r.Feature, pc, phase, runnerManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "runner-error", fmt.Sprintf("runner resolution for %s failed: %v", phase, err))
		return "", "", fmt.Errorf("runner resolution failed: %w", err)
	}
	model, err := protocol.ResolvePhaseModel(r.Cfg, r.Feature, pc, phase, role, phaseRunner, modelManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("model resolution for %s failed: %v", role, err))
		return "", "", fmt.Errorf("model resolution failed: %w", err)
	}
	return phaseRunner, model, nil
}

func (r *Runner) resolvePrompt(pending **prompt.Prefetch, role protocol.Role, s *protocol.State, phaseRunner string) string {
	featureID := r.featureID()
	var promptText string
	gotPrefetch := false
	if *pending != nil && (*pending).Role == role && (*pending).Round == s.Round {
		res := <-(*pending).Ch
		if res.Err == nil {
			promptText = res.Prompt
			gotPrefetch = true
		}
	}
	*pending = nil
	if !gotPrefetch {
		p, gerr := prompt.Generate(r.promptCtx(), role, s.Round, 0, phaseRunner)
		if gerr != nil {
			p = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
		}
		promptText = p
	}
	return promptText
}

func (r *Runner) syncToWorktree(featureID string, round int) {
	if r.RunnerWs.Root != r.Ws.Root {
		SyncFeatureToWorktree(r.Ws, r.RunnerWs, featureID, round)
	}
}

func (r *Runner) syncFromWorktree(featureID string, round int) {
	if r.RunnerWs.Root != r.Ws.Root {
		if serr := SyncFeatureFromWorktree(r.RunnerWs, r.Ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
		}
	}
}

func (r *Runner) handleRunnerError(ctx context.Context, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner, model string, runErr error) error {
	featureID := r.featureID()
	if ctx.Err() == context.Canceled {
		StopState(r.Ws, featureID, s, "interrupted", fmt.Sprintf("%s (%s) interrupted by signal (round %d)", role, phase, s.Round))
		return ctx.Err()
	}
	s.Phase = protocol.PhaseNeedsAttention
	StopState(r.Ws, featureID, s, "runner-error", fmt.Sprintf("%s runner failed during %s (round %d): %v", role, phase, s.Round, runErr))
	r.Ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: phase, Role: role, Round: s.Round,
		Status: "error", Detail: runErr.Error(),
		Runner: phaseRunner, Model: model,
	})
	return runErr
}

// handleExitResult 處理 runner 正常結束後的 hard error 與 soft fail。
// 回傳 non-nil error 表示 hard error（主迴圈應 return）；
// soft fail 時就地修改 s 並回 nil（主迴圈檢查 s.Active/Phase 決定 break）。
func (r *Runner) handleExitResult(result *runner.Result, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner string) error {
	featureID := r.featureID()
	if runner.IsHardError(result) {
		StopState(r.Ws, featureID, s, "hard-error", fmt.Sprintf("%s runner returned hard error (exit 2) during %s (round %d)", role, phase, s.Round))
		return fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		StopState(r.Ws, featureID, s, "soft-fail", fmt.Sprintf("%s runner returned soft-fail (exit %d) during %s (round %d)", role, runner.ExitSoftFail, phase, s.Round))
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
	}
	return nil
}

// handlePostCoder 在 coding/amending phase runner 完成後跑 guard check 與背景 auto-commit。
// guard 失敗時就地修改 s（Active=false, Phase=NeedsAttention）並回 nil。
func (r *Runner) handlePostCoder(ctx context.Context, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner string, commitWG *sync.WaitGroup) error {
	if phase != protocol.PhaseCoding && phase != protocol.PhaseAmending {
		return nil
	}
	featureID := r.featureID()
	guardResult := guard.Check(r.Ws, featureID, r.Ops)

	if guardResult.SelfModTouched {
		s.SelfModTouched = true
		s.SelfModPaths = guardResult.SelfModPaths
		LogStateWriteErr(r.Ws.WriteState(featureID, *s), featureID, s.Phase)
		r.Ws.AppendEvent(featureID, protocol.Event{
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
		LogStateWriteErr(r.Ws.WriteState(featureID, *s), featureID, s.Phase)
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "guard-fail", Phase: phase, Role: role, Round: s.Round,
			Detail: guardMsg, Runner: phaseRunner, Notify: protocol.NotifyError,
		})
		return nil
	}

	if r.CommitStrategy == "per-round" && r.RunnerWs.Root != r.Ws.Root {
		commitWG.Add(1)
		go func(wtRoot string, round int) {
			defer commitWG.Done()
			if err := r.Ops.Commit(wtRoot, featureID, fmt.Sprintf("wip(%s): round %d", featureID, round)); err != nil {
				slog.Error("auto-commit failed", "feature", featureID, "round", round, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "strategy", "per-round")
			}
		}(r.RunnerWs.Root, s.Round)
	}
	return nil
}

func (r *Runner) executeTransitionHooks(ctx context.Context, s *protocol.State, targetPhase protocol.Phase, timing string) error {
	featureID := r.featureID()
	hooks := ResolveHooks(r.Cfg, r.Feature, targetPhase)
	hookLogDir := filepath.Join(r.Ws.FeatureDir(featureID), "hook-logs")
	return ExecutePhaseHooks(ctx, r.Ws, featureID, s, hooks[timing], targetPhase, timing, hookLogDir)
}

func (r *Runner) prefetchPrompt(s protocol.State, pc protocol.ProfileConfig) *prompt.Prefetch {
	nextRole := state.PhaseToRole(s.Phase)
	if !prompt.PrefetchablePhase(s.Phase, r.Cfg) || nextRole == "" || !pc.EnablesRole(nextRole) {
		return nil
	}
	ch := make(chan prompt.Result, 1)
	pf := &prompt.Prefetch{Role: nextRole, Round: s.Round, Ch: ch}
	go func(role protocol.Role, round int) {
		p, gerr := prompt.Generate(r.promptCtx(), role, round, 0, r.ManualRunner)
		ch <- prompt.Result{Prompt: p, Err: gerr}
	}(nextRole, s.Round)
	return pf
}

func (r *Runner) finalize(s protocol.State) error {
	featureID := r.featureID()
	switch s.Phase {
	case protocol.PhasePendingReview:
		prompt.HarvestLearnings(r.Ws, featureID)
		s.Active = false
		s.StopReason = "pending-review"
		LogStateWriteErr(r.Ws.WriteState(featureID, s), featureID, s.Phase)
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhasePendingReview), featureID, protocol.PhasePendingReview)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		LogStateWriteErr(r.Ws.WriteState(featureID, s), featureID, s.Phase)
		LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseDone), featureID, protocol.PhaseDone)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			if s.StopMessage == "" {
				s.StopMessage = fmt.Sprintf("%s stopped with %s (round %d)", featureID, s.Phase, s.Round)
			}
			LogStateWriteErr(r.Ws.WriteState(featureID, s), featureID, s.Phase)
		}
	}
	return nil
}

// runConsolidate 在 harvest 後呼叫 AI 整理語意重複的 learnings。
// 屬 nice-to-have，任何錯誤只 warn 不影響 feature 結果。
func (r *Runner) runConsolidate(ctx context.Context, s protocol.State) {
	if err := prompt.PrepareConsolidateInput(r.Ws); err != nil {
		slog.Warn("prepare consolidate input failed", "error", err)
		return
	}

	tmpl, err := prompt.LoadRoleTemplate(r.Ws.DotDir(), protocol.RoleConsolidator)
	if err != nil {
		slog.Warn("load consolidate template failed", "error", err)
		return
	}
	locale, localeName := prompt.ResolveLocale()
	var b strings.Builder
	data := prompt.Data{
		Role:       protocol.RoleConsolidator,
		Config:     r.Cfg,
		DotDir:     r.Ws.DotDir(),
		Locale:     locale,
		LocaleName: localeName,
	}
	if err := tmpl.Execute(&b, data); err != nil {
		slog.Warn("render consolidate prompt failed", "error", err)
		return
	}

	runnerName := r.Cfg.Default
	rcfg, ok := r.Cfg.Runners[runnerName]
	if !ok {
		slog.Warn("consolidate: default runner not found", "runner", runnerName)
		return
	}
	logPath := filepath.Join(r.Ws.DotDir(), "consolidate.log")
	model := ""
	if m, merr := protocol.ResolveModel(r.Cfg, runnerName, protocol.RoleReviewer); merr == nil {
		model = m
	}
	cr := runner.NewRunner(r.Ws, runnerName, rcfg, 120*time.Second, logPath, model)

	os.Remove(filepath.Join(r.Ws.DotDir(), protocol.ConsolidateResultFile))

	slog.Info("running learnings consolidation")
	if _, rerr := cr.Run(ctx, b.String()); rerr != nil {
		slog.Warn("consolidate runner failed", "error", rerr)
		return
	}

	merged, removed, aerr := prompt.ApplyConsolidateResult(r.Ws)
	if aerr != nil {
		slog.Warn("apply consolidate result failed", "error", aerr)
		return
	}
	if merged+removed > 0 {
		slog.Info("learnings consolidated", "merged", merged, "removed", removed)
	}
}

// StopState 設定 state 的停止欄位並寫入磁碟，統一處理散布在多處的 stop-state boilerplate。
// 呼叫者仍需自行 return（因回傳型別不同）。
func StopState(ws *protocol.Workspace, featureID string, s *protocol.State, reason, message string) {
	s.Active = false
	s.StopReason = reason
	s.StopMessage = message
	LogStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
}

// LogStateWriteErr 在終止／失敗／收尾路徑記錄 WriteState 失敗，取代靜默的 `_ =` 丟棄。
// 這些路徑不改變回傳或控制流（state 已盡力寫出），但失敗必須留下可排查的記錄，
// 避免磁碟 state.json 與記憶體狀態不一致時毫無線索。err 為 nil 時不做任何事。
func LogStateWriteErr(err error, featureID string, phase protocol.Phase) {
	if err != nil {
		slog.Error("write state failed", "feature", featureID, "phase", phase, "error", err)
	}
}

// LogSyncErr 在終止／失敗／收尾路徑記錄 SyncFeatureStatus 失敗，語意同 LogStateWriteErr。
// feature_list.json 狀態同步失敗只影響 dashboard 顯示、不阻斷流程，故僅記錄不回傳。
func LogSyncErr(err error, featureID string, phase protocol.Phase) {
	if err != nil {
		slog.Error("sync feature status failed", "feature", featureID, "phase", phase, "error", err)
	}
}

// ParseRunStatsFromLog 從 runner log 尾端解析 token 使用量與成本。
// 支援兩種格式：
//   - stream-json: [result] success (325.5s, $2.2826)
//   - 傳統 Claude Code: tokens used\n73,204
func ParseRunStatsFromLog(logPath string) RunStats {
	f, err := os.Open(logPath)
	if err != nil {
		return RunStats{}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return RunStats{}
	}

	readSize := int64(4096)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	if _, err := f.Seek(-readSize, io.SeekEnd); err != nil {
		return RunStats{}
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	var stats RunStats
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

// FormatTokens 把 token 數量格式化為帶千分位逗號的字串（如 73204 → "73,204"）
func FormatTokens(n int) string {
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

// ParsePhaseOverrides 解析 --phase-override 旗標的原始字串為 per-phase 臨時覆寫 map。
//
// 每筆格式為 <phase>:<runner>:<model>，三段以冒號分隔；runner / model 任一可留空代表
// 不覆寫該維度，但兩者不可同時為空。phase 必須通過 IsSelectablePhase，重複 phase 視為錯誤。
func ParsePhaseOverrides(raw []string) (map[protocol.Phase]protocol.PhaseSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[protocol.Phase]protocol.PhaseSpec, len(raw))
	for _, entry := range raw {
		parts := strings.SplitN(entry, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid --phase-override %q: expected <phase>:<runner>:<model>", entry)
		}
		phase := protocol.Phase(strings.TrimSpace(parts[0]))
		rn := strings.TrimSpace(parts[1])
		model := strings.TrimSpace(parts[2])
		if !protocol.IsSelectablePhase(phase) {
			return nil, fmt.Errorf("invalid --phase-override %q: %q is not a selectable phase", entry, phase)
		}
		if rn == "" && model == "" {
			return nil, fmt.Errorf("invalid --phase-override %q: runner and model cannot both be empty", entry)
		}
		if _, dup := out[phase]; dup {
			return nil, fmt.Errorf("invalid --phase-override %q: phase %q overridden more than once", entry, phase)
		}
		out[phase] = protocol.PhaseSpec{Phase: string(phase), Runner: rn, Model: model}
	}
	return out, nil
}

// WorktreeExitHints 依 run 結束時的最終 phase 與 commit 策略，回傳要印給使用者的
// worktree 相關提示行。done/pending-review 回傳 branch 與 merge 指令；
// 其餘結束狀態回傳 worktree 路徑與清理提示。wtPath 為空時回 nil。
func WorktreeExitHints(wtPath, featureID string, finalPhase protocol.Phase, commitStrategy string) []string {
	if wtPath == "" {
		return nil
	}
	if finalPhase == protocol.PhaseDone || finalPhase == protocol.PhasePendingReview {
		if commitStrategy == "never" {
			return nil
		}
		return []string{
			fmt.Sprintf("  branch: 4x/%s", featureID),
			fmt.Sprintf("  to merge: git merge 4x/%s && git worktree remove %s && git branch -d 4x/%s", featureID, wtPath, featureID),
		}
	}
	return []string{
		fmt.Sprintf("  worktree preserved at: %s (state: %s)", wtPath, finalPhase),
		fmt.Sprintf("  inspect changes there; when done clean up with: git worktree remove %s && git branch -d 4x/%s", wtPath, featureID),
	}
}

// DeferRunCleanup 在 run 結束時將 state 標為 inactive 並寫入 run-end event。
// 用於 defer 區塊，確保中斷時 state.json 與 event log 一致。
func DeferRunCleanup(ws *protocol.Workspace, featureID string) {
	cur, err := ws.ReadState(featureID)
	if err != nil {
		return
	}
	if cur.Active {
		cur.Active = false
		cur.Pid = 0
		if cur.StopReason == "" {
			cur.StopReason = "process-exit"
			cur.StopMessage = fmt.Sprintf("process exited unexpectedly during %s (round %d)", cur.Phase, cur.Round)
		}
		LogStateWriteErr(ws.WriteState(featureID, cur), featureID, cur.Phase)
		LogSyncErr(ws.SyncFeatureStatus(featureID, cur.Phase), featureID, cur.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type:   "run-end",
			Phase:  cur.Phase,
			Role:   cur.Role,
			Round:  cur.Round,
			Status: "interrupted",
			Detail: cur.StopReason,
			Runner: cur.Runner,
			Notify: protocol.NotifyWarning,
		})
	}
}

// StartBackgroundRun 以 background 方式啟動 run 子程序，將其 stdout/stderr 導向 logPath，
// 讓早期錯誤（config 載入、worktree setup、runner not found 等）可事後檢視。
func StartBackgroundRun(binPath string, args []string, dir, logPath string) (*os.Process, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open run log: %w", err)
	}
	bgCmd := exec.Command(binPath, args...)
	bgCmd.Dir = dir
	bgCmd.Stdin = nil
	bgCmd.Stdout = logFile
	bgCmd.Stderr = logFile
	if err := bgCmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start run: %w", err)
	}
	logFile.Close()
	go bgCmd.Wait()
	return bgCmd.Process, nil
}

// healthCheckExecutor 回傳一個執行單一 health check command 的 executor，
// 每個 command 以 sh -c 執行並套用 per-command timeout（timeoutSec <= 0 時預設 30 秒），
// 失敗時把 command 與輸出寫到 stderr 方便排查。
func healthCheckExecutor(ctx context.Context, timeoutSec int) func(cmd string) error {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return func(cmd string) error {
		cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cmdCtx, "sh", "-c", cmd).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  health check failed: %s\n%s\n", cmd, string(out))
		}
		return err
	}
}
