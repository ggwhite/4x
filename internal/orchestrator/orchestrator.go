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

// NewRunner 建立新的 Runner 實例，並以 events.jsonl 的權威加總 seed totalCostUSD，
// 讓曾中斷重啟過的 feature 也能正確帶回歷史花費（新 feature 因無 events.jsonl 得到 0，無害）。
func NewRunner(cfg Config) *Runner {
	seedCost, _ := cfg.Ws.TotalCost(cfg.Feature.ID)
	return &Runner{Config: cfg, totalCostUSD: seedCost}
}

func (r *Runner) featureID() string {
	return r.Feature.ID
}

// promptCtx 從 Runner 組裝 prompt.Context
func (r *Runner) promptCtx() *prompt.Context {
	ctx := &prompt.Context{
		Ws:       r.Ws,
		RunnerWs: r.RunnerWs,
		Feature:  r.Feature,
		Cfg:      r.Cfg,
	}
	// F150：best-effort 帶入 state.json 的 profile，供 prompt 注入 profile-aware 段落；
	// 讀不到（如 run 尚未寫 state）則留空，走 ResolveProfile fallback。
	if s, err := r.Ws.ReadState(r.featureID()); err == nil {
		ctx.Profile = s.Profile
	}
	return ctx
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

// nextRoleIteration 回傳並遞增 role 在 round 內的執行次數。designer / design-reviewer
// 這類 role 在 design-reviewing FAIL 打回 designing 時，round 不會遞增，同 round 內
// 可能重複執行；用這個計數搭配 runner.IterationLogFileName 避免同名 log 檔案互相覆寫。
func nextRoleIteration(counts map[string]int, round int, role protocol.Role) int {
	key := fmt.Sprintf("%d-%s", round, role)
	counts[key]++
	return counts[key]
}

// archiveDesignArtifact 把 designer/design-reviewer 剛寫入的固定檔名 artifact
// （task-brief.md／acceptance-criteria.md／design-review-report.md）複製一份到
// design-rounds/round-<round>-<iteration>/。這些檔案不像 coding phase 有
// rounds/round-N/ 目錄保留歷史，design-reviewing FAIL 打回 designing 時會被下一輪
// 直接覆寫，dashboard message 區因此看不到過去輪次的內容；歸檔後 handleMessages
// 才能把每一輪都列出來。非 designer/design-reviewer 的 role 不受影響，直接 no-op。
func archiveDesignArtifact(ws *protocol.Workspace, featureID string, round, iteration int, role protocol.Role) {
	var files []string
	switch role {
	case protocol.RoleDesigner:
		files = []string{protocol.TaskBrief, protocol.Criteria}
	case protocol.RoleDesignReviewer:
		files = []string{protocol.DesignReviewReport}
	default:
		return
	}

	destDir := filepath.Join(ws.FeatureDir(featureID), protocol.DesignRoundsDir, fmt.Sprintf("round-%d-%d", round, iteration))
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), name))
		if err != nil {
			continue
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			slog.Warn("archive design artifact: mkdir failed", "feature", featureID, "dir", destDir, "error", err)
			return
		}
		if err := os.WriteFile(filepath.Join(destDir, name), data, 0o644); err != nil {
			slog.Warn("archive design artifact: write failed", "feature", featureID, "file", name, "error", err)
		}
	}
}

// RecoverState 檢查是否需要 resume recovery，若需要則依 artifacts 推斷正確的 phase 並修正 state。
// 回傳修正後的 state（可能與輸入相同）與 error。
// pc 為本次 run 解析出的 profile 設定，往下傳給 SmartResumePhase 對齊 Fixing 判斷。
func RecoverState(ws *protocol.Workspace, featureID string, s protocol.State, cfg protocol.Config, pc protocol.ProfileConfig) (protocol.State, error) {
	// per-run parallel 訊號只在 RunReviewTestParallel 的並行段落內為 true；process 若在段落內
	// 硬死，state.json 會殘留 parallelReview:true。resume 起點一律清除，避免之後每次
	// WriteState 一路帶著 stale 訊號。
	s.ParallelReview = false
	// 人為介入：`4x transition` / `4x retry` 手動設定的 phase 必須被尊重，直接照 state.json
	// 的 phase 派對應 role，不進 SmartResumePhase 依磁碟 artifacts 重推導回更早的 phase。
	if s.ManualPhase {
		s.ManualPhase = false // 消費後清除，避免下一輪真 crash 時仍被當成人為介入
		s.SubPhase = ""       // 人為指定 phase，從該 phase 起點重新開始
		if role := state.PhaseToRole(s.Phase); role != "" {
			s.Role = role
		}
		return s, nil
	}
	if !NeedsResumeRecovery(s) {
		return s, nil
	}
	resumePhase, resumeRole, resumeSub := SmartResumePhase(ws, featureID, s.Round, cfg, pc)
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
	roleRoundIter := map[string]int{}

	var commitWG sync.WaitGroup
	defer commitWG.Wait()

	var pending *prompt.Prefetch

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

		if phase == protocol.PhaseAccepting && s.Round >= 3 {
			r.runRoundSummarizer(ctx, s)
		}
		if phase == protocol.PhaseAccepting {
			r.generateAcceptanceSummary(s.Round)
		}

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

		iteration := nextRoleIteration(roleRoundIter, s.Round, role)
		logPath := filepath.Join(runner.LogDir(r.Ws, featureID), runner.IterationLogFileName(s.Round, string(role), iteration))
		rn := r.NewRunner(phaseRunner, logPath, model)
		setReviewerExtraEnv(rn, role, featureID, filepath.Join(r.Ws.RoundDir(featureID, s.Round), protocol.ReviewPackage))

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

		tokens, costUSD, codexUsage := r.runEndMetrics(phaseRunner, logPath)
		switch {
		case codexUsage != nil:
			fmt.Printf("  → codex 5h %s / 1wk %s, %s tokens, %s\n",
				formatPct(codexUsage.PrimaryPercent), formatPct(codexUsage.SecondaryPercent),
				FormatTokens(tokens), invokeDur.Truncate(time.Second))
		case costUSD > 0:
			fmt.Printf("  → $%.4f, %s\n", costUSD, invokeDur.Truncate(time.Second))
		case tokens > 0:
			fmt.Printf("  → %s tokens, %s\n", FormatTokens(tokens), invokeDur.Truncate(time.Second))
		default:
			fmt.Printf("  → %s\n", invokeDur.Truncate(time.Second))
		}

		if runErr != nil {
			return nil, r.handleRunnerError(ctx, &s, phase, role, phaseRunner, model, runErr)
		}

		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status: fmt.Sprintf("exit-%d", result.ExitCode),
			Runner: phaseRunner, Model: model,
			TokensUsed: tokens, CostUSD: costUSD, DurationMs: invokeDur.Milliseconds(),
			Codex: codexUsage,
		})

		// run 返回後護欄（Blocking 1 方案 b）：rn.Run 可能數分鐘，期間若外部把 feature 設為終態，
		// 此處採用磁碟終態並 break，讓 handleExitResult / handlePostCoder / 後續 transition commit
		// 都不執行——縮小（非消除）復活窗。真正關閉窗口的是各 mid-round 寫入本身改用 CAS：
		// handlePostCoder 的 SelfModTouched 寫入（guard.Check 期間被終結）、commitLoopState 的
		// transition 寫入、finalize 的終態寫入都在鎖內尊重磁碟 Active，外部終結後絕不用舊快照復活。
		if disk, yielded := r.externallyTerminated(); yielded {
			s = disk
			break
		}

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

		archiveDesignArtifact(r.Ws, featureID, s.Round, iteration, role)

		// F144：reviewing phase 偵測到 CONDITIONAL PASS 時，於同一 round、同一 phase 內派
		// mini-coder 收掉 warning 並重跑 reviewer 確認，再交回 NextPhaseAfter 照常轉換。
		// parallel review-test 路徑由 RunReviewTestParallel 接管（routePhase）並在其內收斂；
		// 未被 parallel 路徑接管（含 parallel_review_test=true 但 profile 缺 tester）一律走此處，
		// 與 routePhase 共用 parallelReviewRouted 判斷，確保 CONDITIONAL PASS 不會兩頭落空。
		if phase == protocol.PhaseReviewing && !r.parallelReviewRouted(pc) {
			cont, _, cerr := r.runReviewConvergence(ctx, &s, pc)
			if cerr != nil {
				return nil, cerr
			}
			if !cont {
				break
			}
		}

		next, nextRole, stopReason := NextPhaseAfter(r.Ws, featureID, s)

		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
		}

		if err := r.executeTransitionHooks(ctx, &s, next, "pre"); err != nil {
			// ExecutePhaseHooks 已在回傳 error 前把 state 轉去 needs-attention、
			// 設 Active=false、補 StopReason=<timing>-hook-fail 並持久化
			// （見 hook.go ExecutePhaseHooks），故此處不需（也不應）再呼叫
			// abortTransition，否則會用較不精確的 "transition-error" 覆寫掉
			// 已經更精確的 hook-fail StopReason。
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
			r.abortTransition(featureID, &s, phase, role, phaseRunner, err)
			return nil, fmt.Errorf("loop transition %s→%s: %w", phase, next, err)
		}

		if next == protocol.PhaseAmending {
			ApplyAmendingProgress(r.Ws, featureID, &newState, s.Round)
		}
		if next == protocol.PhaseTesting && phase == protocol.PhaseTesting {
			newState.GuardRetries++
			cleanupTesterRetry(r.Ws, featureID, s.Round)
		}
		if next == protocol.PhaseReviewing && (phase == protocol.PhaseCoding || phase == protocol.PhaseAmending) {
			r.generateReviewPackage(newState.Round, newState.BaseCommit)
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
		// post-phase transition commit 走 CAS 護欄：若磁碟 Active 已被外部清為 false
		// （run 進行中被 done/abandon/force-done 終結），放棄本次覆寫、採用磁碟終態並
		// 跳過該輪剩餘 transition 副作用（sync/event/post-hook）後 break——這筆 transition
		// 已被丟棄，不應記錄事件或跑 post-hook。
		persisted, yielded, err := r.commitLoopState(featureID, s)
		if err != nil {
			return nil, fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		if yielded {
			s = persisted
			break
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

	// 迴圈頂偵測「外部已終結」：run 進行期間若使用者透過 dashboard/CLI 把同一 feature
	// 設為終態（Active=false），採用該磁碟終態並 break，避免下一輪用記憶體舊快照復活。
	// ReadState 不加鎖、成本可忽略；讀失敗不誤判為終結（走原路徑）。
	if disk, yielded := r.externallyTerminated(); yielded {
		*s = disk
		return true
	}
	return false
}

// externallyTerminated 不加鎖重讀磁碟 state，偵測 feature 是否已被外部終結。
//
// 回傳 (disk, true) 表示磁碟 Active==false（已被 done/abandon/force-done 等外部路徑終結），
// 呼叫端應採用該磁碟終態；(disk, false) 表示仍 active、走原路徑；ReadState 失敗回
// (State{}, false)（不把讀失敗誤判為終結）。供迴圈頂與 rn.Run 返回後的護欄共用。
func (r *Runner) externallyTerminated() (protocol.State, bool) {
	disk, err := r.Ws.ReadState(r.featureID())
	if err != nil {
		return protocol.State{}, false
	}
	if !disk.Active {
		return disk, true
	}
	return disk, false
}

// commitLoopState 是 run loop 的 run-continuing 寫入 CAS 護欄：在加鎖臨界區重讀磁碟，
// 若磁碟 Active 已被外部清為 false（run 已被外部終結），放棄自己的覆寫、回 yielded==true
// 並帶回磁碟終態；否則才寫入自己算出的 next。
//
// 這消除 feature description 點名的核心情境：run 進行中被外部 done/abandon/force-done
// 終結後，orchestrator 用舊快照（Active=true）把終態復活成 running。
func (r *Runner) commitLoopState(featureID string, next protocol.State) (persisted protocol.State, yielded bool, err error) {
	persisted, err = r.Ws.UpdateState(featureID, func(disk *protocol.State) error {
		if !disk.Active {
			yielded = true
			return protocol.ErrSkipStateWrite
		}
		*disk = next
		return nil
	})
	return persisted, yielded, err
}

// writeActiveState 是 run-continuing 的「phase 內進度／子階段」寫入 CAS 護欄：加鎖重讀磁碟，
// 若磁碟 Active 已被外部清為 false（run 進行中被 done/abandon/force-done 終結），放棄自己的
// 覆寫、回 yielded==true 並把 *s 更新為磁碟終態；否則寫入記憶體快照 *s。
//
// deep-review 自癒循環與 review CONDITIONAL PASS 收斂會在數分鐘的子 run 之間反覆寫 role/sub-phase
// 進度；這些寫入原本是盲寫（WriteState），會用 Active=true 的舊快照把已被外部終結的 feature 復活，
// 且其後迴圈讀到自己寫回的 Active=true 便永不 yield。共用此 helper（語意同 commitLoopState）確保
// 各子階段寫入尊重臨界區內的磁碟終態，關閉復活窗；呼叫端在 yielded 時應中止本 phase、交主迴圈收尾。
func (r *Runner) writeActiveState(featureID string, s *protocol.State) (yielded bool, err error) {
	persisted, err := r.Ws.UpdateState(featureID, func(disk *protocol.State) error {
		if !disk.Active {
			yielded = true
			return protocol.ErrSkipStateWrite
		}
		*disk = *s
		return nil
	})
	if err != nil {
		return false, err
	}
	if yielded {
		*s = persisted
	}
	return yielded, nil
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

// parallelReviewRouted 回報 reviewing phase 是否會由 parallel review/test 路徑
// （RunReviewTestParallel）接管：parallel_review_test 開啟且 profile 同時啟用 reviewer
// 與 tester。routePhase 的路由條件與 RunLoop 的 serial 收斂 gate 都以此為唯一判斷來源，
// 兩處互補、避免條件漂移造成 CONDITIONAL PASS 收斂兩頭落空。
func (r *Runner) parallelReviewRouted(pc protocol.ProfileConfig) bool {
	return r.Cfg.ParallelReviewTest &&
		pc.EnablesRole(protocol.RoleReviewer) && pc.EnablesRole(protocol.RoleTester)
}

// routePhase 處理需要特殊路徑的 phase（parallel review/test、deep-reviewing）。
// 回傳 (routed, cont, err)：routed 為 true 表示該 phase 已由特殊路徑接管；
// 此時 cont 為 true 表示主迴圈應 continue，cont 為 false 表示應 break。
// routed 為 false 表示一般 phase，主迴圈繼續走一般路徑。
func (r *Runner) routePhase(ctx context.Context, s *protocol.State, pc protocol.ProfileConfig) (routed, cont bool, err error) {
	phase := s.Phase

	if phase == protocol.PhaseReviewing && r.parallelReviewRouted(pc) {
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
	if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending || phase == protocol.PhaseFixing {
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
	if s.Phase != protocol.PhaseCoding || s.Round != 1 {
		return nil
	}
	if err := CaptureBaselineOnce(r.Ws, r.Ops, r.featureID(), r.Feature.Repos); err != nil {
		return err
	}
	r.captureBaseCommit(s)
	return nil
}

// captureBaseCommit 在首次進入 coding phase 時記錄當下 HEAD 為 s.BaseCommit，供 review-package.md
// 計算 diff 的起點（僅 mono-repo 使用；multi-repo 各 repo 有獨立歷史，改用 baseline.json 的
// per-repo Head，見 gitops.multiRepo.GenerateReviewPackage）。已設定過就不重複擷取（冪等，
// 避免 resume 時 HEAD 已因先前輪次的 commit 前進而覆寫掉正確的 base）。取得失敗只 warn，
// 不阻斷流程——review package 生成階段會因 BaseCommit 為空而 fallback 跳過寫檔。
func (r *Runner) captureBaseCommit(s *protocol.State) {
	if s.BaseCommit != "" {
		return
	}
	root := gitops.ScopeRoot(r.Ws.Root, r.featureID())
	sha := gitops.HeadCommit(root)
	if sha == "" {
		return
	}
	s.BaseCommit = sha
	if err := r.Ws.WriteState(r.featureID(), *s); err != nil {
		slog.Warn("write state (base commit) failed", "feature", r.featureID(), "error", err)
	}
}

// generateReviewPackage 在 coding/amending → reviewing 轉換時，用 Ops 預算 baseCommit..HEAD 的
// commits/stat/diff 寫成 review-package.md，供 reviewer/deep-reviewer 讀檔取代自跑 git diff。
// 產生失敗（如尚無 baseCommit、無 diff）只 warn 不阻斷流程——reviewer template 在檔案不存在時
// 會 fallback 自跑 git diff（見 templates/reviewer.md.tmpl）。
func (r *Runner) generateReviewPackage(round int, baseCommit string) {
	featureID := r.featureID()
	content, err := r.Ops.GenerateReviewPackage(featureID, baseCommit)
	if err != nil || content == "" {
		slog.Warn("generate review package failed, reviewer will fallback to running git diff itself",
			"feature", featureID, "round", round, "error", err)
		return
	}
	roundDir := r.Ws.RoundDir(featureID, round)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		slog.Warn("mkdir round dir for review package failed", "feature", featureID, "round", round, "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(roundDir, protocol.ReviewPackage), []byte(content), 0o644); err != nil {
		slog.Warn("write review package failed", "feature", featureID, "round", round, "error", err)
		return
	}
	if strings.Contains(content, gitops.ReviewPackageTruncatedMarker) {
		slog.Warn("review package truncated: changed file contents exceeded budget, some files listed as paths only",
			"feature", featureID, "round", round, "budget_kb", 100)
	}
}

// generateAcceptanceSummary 在進 accepting phase 前（每次進入 accepting 都重新產生，維持最新）
// 解析本輪 verify.json / review-report.md / deep-review-report.md，彙整成 acceptance-summary.md，
// 供 Acceptor 讀取取代重複讀取原始報告全文。無資料可彙整或解析失敗時不寫檔，acceptor template
// 會 fallback 讀原始報告。
func (r *Runner) generateAcceptanceSummary(round int) {
	featureID := r.featureID()
	content := GenerateAcceptanceSummary(r.Ws, featureID, round)
	if content == "" {
		return
	}
	roundDir := r.Ws.RoundDir(featureID, round)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		slog.Warn("mkdir round dir for acceptance summary failed", "feature", featureID, "round", round, "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(roundDir, protocol.AcceptanceSummaryFile), []byte(content), 0o644); err != nil {
		slog.Warn("write acceptance summary failed", "feature", featureID, "round", round, "error", err)
	}
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
	prompt.MarkLearningsUsed(r.Ws.DotDir(), prompt.LoadLearningsForRole(r.Ws.DotDir(), role))
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

// abortTransition 處理「角色 run 已 exit-0 完成，但收尾（state.Transition 本身，
// 一個不寫入任何狀態的純函式）失敗」的情況：把 state 標為 needs-attention 並
// 持久化 Active=false。若不這麼做，state.json 會停在轉換前的舊 phase（Active
// 仍為 true），使 cmd/4x/run.go 的 defer DeferRunCleanup 兜底邏輯誤將這個「角色
// 其實已完成、只是收尾失敗」的狀況標記為 process-exit/interrupted，掩蓋角色已
// 完成的事實。注意：pre-transition hook 失敗不會呼叫本函式——
// executeTransitionHooks 內的 ExecutePhaseHooks 已自行完成同等的收尾
// （轉 needs-attention、Active=false、StopReason=<timing>-hook-fail、持久化），
// 這裡再呼叫只會用較不精確的 StopReason 覆寫掉它。
func (r *Runner) abortTransition(featureID string, s *protocol.State, phase protocol.Phase, role protocol.Role, phaseRunner string, cause error) {
	s.Phase = protocol.PhaseNeedsAttention
	StopState(r.Ws, featureID, s, "transition-error", fmt.Sprintf(
		"state transition out of %s failed after round %d's runner already completed: %v", phase, s.Round, cause))
	LogSyncErr(r.Ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	r.Ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: s.Phase, Role: role, Round: s.Round,
		Status: "error", Detail: cause.Error(), Runner: phaseRunner, Notify: protocol.NotifyError,
	})
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
		// CAS 寫入：guard.Check 可能耗時，期間若外部把 feature force-done（磁碟 Active=false），
		// 此處若盲寫 *s（Active=true）會復活已終結的 feature。yielded 時 *s 已被更新為磁碟終態，
		// 直接回 nil 交主迴圈的 `if !s.Active { break }` 收尾，不再記錄 self-mod event。
		yielded, werr := r.writeActiveState(featureID, s)
		LogStateWriteErr(werr, featureID, s.Phase)
		if yielded {
			return nil
		}
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
		r.finalizeWrite(featureID, s)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		r.finalizeWrite(featureID, s)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			if s.StopMessage == "" {
				s.StopMessage = fmt.Sprintf("%s stopped with %s (round %d)", featureID, s.Phase, s.Round)
			}
			r.finalizeWrite(featureID, s)
		}
	default:
		// 其他 phase 非終態，finalize 不處理
	}
	return nil
}

// finalizeWrite 是 post-loop finalize 的終態寫入 CAS 護欄：加鎖重讀磁碟，若磁碟 Active 已為 false，
// 代表迴圈算完自己的終態後、跑耗時 transition hook 期間，外部 done/abandon/force-done 已搶先寫入
// 終態；此時放棄本迴圈自算的終態、整包尊重磁碟（含其 Phase / StopReason / StopMessage），避免：
//   - 用 pending-review / blocked 覆蓋磁碟上不可逆的 done（缺陷 4）；
//   - 用 StopReason="done" 覆蓋 force-done 設的 StopReason／StopMessage（缺陷 6）。
//
// 迴圈自己走到 done 時，commitLoopState 寫入的磁碟仍為 Active=true，故不會被跳過、正常收尾為 done。
// 只在真正寫入時才 SyncFeatureStatus，且以磁碟現況的 phase 為準（跳過時外部終結者已自行 sync 過）。
func (r *Runner) finalizeWrite(featureID string, s protocol.State) {
	persisted, err := r.Ws.UpdateState(featureID, func(disk *protocol.State) error {
		if !disk.Active {
			return protocol.ErrSkipStateWrite
		}
		*disk = s
		return nil
	})
	if err != nil {
		LogStateWriteErr(err, featureID, s.Phase)
		return
	}
	LogSyncErr(r.Ws.SyncFeatureStatus(featureID, persisted.Phase), featureID, persisted.Phase)
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
	cr := runner.NewRunner(r.Ws, runnerName, rcfg, 120*time.Second, logPath, model, runner.ResolveEnvFilter(r.Cfg))

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
		if err := prompt.GenerateLearningsContext(r.Ws); err != nil {
			slog.Warn("refresh learnings context after consolidation failed", "error", err)
		}
	}
}

// runRoundSummarizer 在進入 accepting phase 且 round ≥ 3 時呼叫 AI 壓縮舊輪次 report。
// 產出 rounds-summary.md，供 Acceptor 取代讀取所有輪次全文。
// 屬 nice-to-have，任何錯誤只 warn 不影響 accepting 執行。
func (r *Runner) runRoundSummarizer(ctx context.Context, s protocol.State) {
	featureID := r.featureID()
	summaryPath := filepath.Join(r.Ws.FeatureDir(featureID), protocol.RoundsSummaryFile)
	if _, err := os.Stat(summaryPath); err == nil {
		return // 已存在，避免重跑
	}

	tmpl, err := prompt.LoadRoleTemplate(r.Ws.DotDir(), protocol.RoleRoundSummarizer)
	if err != nil {
		slog.Warn("load round-summarizer template failed", "error", err)
		return
	}
	locale, localeName := prompt.ResolveLocale()
	var b strings.Builder
	data := prompt.Data{
		Role:       protocol.RoleRoundSummarizer,
		Config:     r.Cfg,
		DotDir:     r.RunnerWs.DotDir(),
		Feature:    r.Feature,
		Round:      s.Round,
		Locale:     locale,
		LocaleName: localeName,
	}
	if err := tmpl.Execute(&b, data); err != nil {
		slog.Warn("render round-summarizer prompt failed", "error", err)
		return
	}

	runnerName := r.Cfg.Default
	rcfg, ok := r.Cfg.Runners[runnerName]
	if !ok {
		slog.Warn("round-summarizer: default runner not found", "runner", runnerName)
		return
	}
	logPath := filepath.Join(r.Ws.FeatureDir(featureID), "round-summarizer.log")
	model := ""
	if m, merr := protocol.ResolveModel(r.Cfg, runnerName, protocol.RoleReviewer); merr == nil {
		model = m
	}
	cr := runner.NewRunner(r.RunnerWs, runnerName, rcfg, 120*time.Second, logPath, model, runner.ResolveEnvFilter(r.Cfg))

	slog.Info("running round summarizer", "feature", featureID, "round", s.Round)
	if _, rerr := cr.Run(ctx, b.String()); rerr != nil {
		slog.Warn("round-summarizer runner failed", "error", rerr)
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

// runEndMetrics 解析 runner log 的 token/cost 統計並累加至 r.totalTokens/totalCostUSD，
// 回傳供 run-end event 填欄的 (tokens, costUSD, codex)。runnerName=="codex" 時額外從對應
// rollout jsonl 擷取即時額度用量：成功則回傳非 nil 的 codex（且 log 未回報 token 時以
// rollout 的累計 total_tokens 補位）；解析失敗只 slog.Warn、回傳 codex==nil，run-end 照常寫。
// runnerName 非 codex 時 codex 恆為 nil，行為等同純 ParseRunStatsFromLog（claude 路徑不受影響）。
//
// 所有 run-end 寫入點皆透過本 helper 收斂 ParseRunStatsFromLog 呼叫與累加，避免 codex 分支散落。
func (r *Runner) runEndMetrics(runnerName, logPath string) (tokens int, costUSD float64, codex *protocol.CodexUsage) {
	stats := ParseRunStatsFromLog(logPath)
	tokens, costUSD = stats.Tokens, stats.CostUSD
	if runnerName == "codex" {
		if cu, cuTokens := ParseCodexUsage(logPath, ResolveCodexHome(os.Getenv("CODEX_HOME"))); cu != nil {
			codex = cu
			if tokens == 0 {
				tokens = cuTokens
			}
		} else {
			slog.Warn("codex usage unavailable, skipping", "feature", r.featureID(), "log", logPath)
		}
	}
	r.totalTokens += tokens
	r.totalCostUSD += costUSD
	return tokens, costUSD, codex
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

// formatPct 把額度百分比格式化為簡潔字串（如 1.0 → "1%"、60.0 → "60%"、0 → "0%"），
// 供 codex 用量進度列與 status/cost 顯示共用。
func formatPct(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64) + "%"
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
	// 檢查+修改+寫回收斂到單一加鎖臨界區（讀最新磁碟值為權威）：只在磁碟仍為 active
	// 時降級，否則回 ErrSkipStateWrite 不寫。wrote 明確標記「本次確實由 active 降為
	// inactive」，供其後只在真正降級時才發 sync/run-end（磁碟本就非 active 代表已有
	// 其他路徑寫過終態，不重複記錄）。
	wrote := false
	cur, err := ws.UpdateState(featureID, func(cur *protocol.State) error {
		if !cur.Active {
			return protocol.ErrSkipStateWrite
		}
		cur.Active = false
		cur.Pid = 0
		if cur.StopReason == "" {
			cur.StopReason = "process-exit"
			cur.StopMessage = fmt.Sprintf("process exited unexpectedly during %s (round %d)", cur.Phase, cur.Round)
			// 只在「原本 StopReason 為空」的兜底情境覆寫 phase：這代表 RunLoop 沒有機會
			// 走到任何終態收尾路徑就中斷（真正的 crash），需要 needs-attention 讓
			// `4x retry` 能接手；已由其他路徑正常寫入終態的情況不會進到這個 if。
			cur.Phase = protocol.PhaseNeedsAttention
			// cur.Role 維持不變：retryTransition（cmd/4x/retry.go）依目標 phase 算
			// toRole，不讀舊 Role；state.Transition 的合法性判斷也只看 Phase，
			// 清空 Role 沒有好處反而丟失資訊。
		}
		wrote = true
		return nil
	})
	if err != nil {
		LogStateWriteErr(err, featureID, cur.Phase)
		return
	}
	if !wrote {
		return
	}
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

// StartBackgroundRun 以 background 方式啟動 run 子程序，將其 stdout/stderr 導向 logPath，
// 讓早期錯誤（config 載入、worktree setup、runner not found 等）可事後檢視。
func StartBackgroundRun(binPath string, args []string, dir, logPath string) (*os.Process, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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
