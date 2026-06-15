package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/health"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

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

func newRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var dryRun bool
	var jsonOutput bool
	var profileFlag string

	cmd := &cobra.Command{
		Use:   "run <feature-id>",
		Short: "Run the Design-Code-Review-Test loop for a feature",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

			if runnerName == "" {
				runnerName = cfg.Default
			}
			_, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			if maxRounds <= 0 {
				maxRounds = 5
			}

			// 提早驗證 --profile（unknown profile / 缺 coder）；空值時回 full/auto 不報錯。
			if _, _, err := protocol.ResolveProfile(cfg, feature, profileFlag); err != nil {
				return err
			}

			if jsonOutput {
				bgArgs := []string{"run", featureID}
				if runnerName != "" {
					bgArgs = append(bgArgs, "--runner", runnerName)
				}
				if profileFlag != "" {
					bgArgs = append(bgArgs, "--profile", profileFlag)
				}
				if maxRounds > 0 {
					bgArgs = append(bgArgs, "--max-rounds", fmt.Sprintf("%d", maxRounds))
				}
				if timeout > 0 {
					bgArgs = append(bgArgs, "--timeout", fmt.Sprintf("%d", timeout))
				}
				if dryRun {
					bgArgs = append(bgArgs, "--dry-run")
				}

				bgCmd := exec.Command(os.Args[0], bgArgs...)
				bgCmd.Dir = cwd
				bgCmd.Stdin = nil
				bgCmd.Stdout = nil
				bgCmd.Stderr = nil

				if err := bgCmd.Start(); err != nil {
					return fmt.Errorf("failed to start run: %w", err)
				}
				go bgCmd.Wait()

				result := struct {
					FeatureID string `json:"featureId"`
					Runner    string `json:"runner"`
					MaxRounds int    `json:"maxRounds"`
					PID       int    `json:"pid"`
				}{
					FeatureID: featureID,
					Runner:    runnerName,
					MaxRounds: maxRounds,
					PID:       bgCmd.Process.Pid,
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			depResult := guard.CheckDependencies(ws, featureID)
			if !depResult.Pass {
				for _, e := range depResult.Errors {
					slog.Warn("dependency blocked", "feature", featureID, "reason", e)
				}
				return fmt.Errorf("feature %s has unmet dependencies", featureID)
			}

			// worktree isolation：runner 在獨立 worktree 內執行
			ops := gitops.New(ws.Root, ws, cfg)
			var runnerWs *protocol.Workspace
			var wtPath string
			if cfg.Isolation == "worktree" {
				var err error
				wtPath, err = ops.SetupWorktree(featureID)
				if err != nil {
					return fmt.Errorf("worktree setup: %w", err)
				}
				runnerWs = &protocol.Workspace{Root: wtPath}
				fmt.Printf("worktree: %s\n", wtPath)
			} else {
				runnerWs = ws
			}

			if err := ws.InitFeatureDir(featureID); err != nil {
				return err
			}

			s := protocol.State{
				FeatureID: featureID,
				Phase:     protocol.PhaseInit,
				MaxRounds: maxRounds,
				Active:    true,
				Runner:    runnerName,
				CreatedAt: time.Now(),
			}

			if existing, err := ws.ReadState(featureID); err == nil {
				if existing.Active && protocol.ProcessAlive(existing.Pid) {
					return fmt.Errorf("feature %s is already running (pid %d)", featureID, existing.Pid)
				}
				s = existing
				s.Active = true
				s.Runner = runnerName
				s.StopReason = ""
				newMax := s.Round + maxRounds
				if newMax > s.MaxRounds {
					s.MaxRounds = newMax
				}
				if s.Phase == protocol.PhaseDone {
					return fmt.Errorf("feature %s is already done", featureID)
				}
				if s.Phase == protocol.PhaseBlocked || s.Phase == protocol.PhaseNeedsAttention {
					resumePhase := roleToResumePhase(s.Role)
					fmt.Printf("  recovering %s → %s (max rounds: %d)\n", s.Phase, resumePhase, s.MaxRounds)
					ns, err := state.Transition(s, resumePhase, s.Role)
					if err != nil {
						return fmt.Errorf("recovery transition %s → %s: %w", s.Phase, resumePhase, err)
					}
					s = ns
				}
			}

			found := false
			for _, r := range s.Runners {
				if r == runnerName {
					found = true
					break
				}
			}
			if !found {
				s.Runners = append(s.Runners, runnerName)
			}

			// 決定本次 run 的 profile：--profile 優先，否則沿用 resume 既有值，
			// 再否則依 priority auto-select（或無 profiles 區段時回 full）。
			profileOverride := profileFlag
			if profileOverride == "" {
				profileOverride = s.Profile
			}
			profileName, _, err := protocol.ResolveProfile(cfg, feature, profileOverride)
			if err != nil {
				return err
			}
			s.Profile = profileName

			s.Pid = os.Getpid()
			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:   "run-start",
				Phase:  s.Phase,
				Role:   state.PhaseToRole(s.Phase),
				Runner: runnerName,
			})

			slog.Info("feature run started", "feature", featureID, "runner", runnerName, "maxRounds", maxRounds, "profile", s.Profile)

			if dryRun {
				return dryRunLoop(ws, feature, cfg, s)
			}

			commitStrategy := cfg.Commit
			if commitStrategy == "" {
				commitStrategy = "per-round"
			}

			signal.Ignore(syscall.SIGPIPE)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			defer func() {
				cur, err := ws.ReadState(featureID)
				if err != nil {
					return
				}
				if cur.Active {
					cur.Active = false
					cur.Pid = 0
					if cur.StopReason == "" {
						cur.StopReason = "process-exit"
					}
					_ = ws.WriteState(featureID, cur)
					_ = ws.SyncFeatureStatus(featureID, cur.Phase)
					ws.AppendEvent(featureID, protocol.Event{
						Type:   "run-end",
						Phase:  cur.Phase,
						Role:   cur.Role,
						Round:  cur.Round,
						Status: "interrupted",
						Detail: cur.StopReason,
						Runner: cur.Runner,
					})
				}
			}()

			runnerCfg := cfg.Runners[runnerName]
			runnerFactory := func(logPath string, model string) runner.Runner {
				return runner.NewRunner(runnerWs, runnerName, runnerCfg, time.Duration(timeout)*time.Second, logPath, model)
			}
			loopErr := runLoop(ctx, ws, runnerWs, feature, cfg, s, ops, runnerFactory, commitStrategy)

			if wtPath != "" && commitStrategy != "never" {
				finalState, _ := ws.ReadState(featureID)
				if finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview {
					if commitStrategy == "on-done" {
						if err := ops.Commit(wtPath, featureID, fmt.Sprintf("feat(%s): %s", featureID, feature.Name)); err != nil {
							slog.Error("auto-commit failed", "feature", featureID, "error", err)
						} else {
							slog.Info("auto-commit", "feature", featureID, "strategy", commitStrategy)
						}
					}
					fmt.Printf("  branch: 4x/%s\n", featureID)
					fmt.Printf("  to merge: git merge 4x/%s && git worktree remove %s && git branch -d 4x/%s\n", featureID, wtPath, featureID)
				}
			}

			return loopErr
		}),
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name (default: config default)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max iteration rounds (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling plugin")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "start run and return JSON immediately")
	// --profile 與 --only 互斥；目前 Go run 指令沒有 --only（屬 skill 編排層），
	// 故此處不註冊互斥規則，未來若新增 --only 再加 MarkFlagsMutuallyExclusive。
	cmd.Flags().StringVar(&profileFlag, "profile", "", "pipeline profile (full/normal/quick or custom); overrides priority-based auto-select")
	return cmd
}

// promptOption 在 promptData 組好、模板 render 前對其做最後調整，
// 供平行 deep review 注入 ReviewerIndex / AssignedAngles / PartialReports 等額外欄位。
type promptOption func(*promptData)

// deepReviewPartialName 回傳平行 deep review 中第 index 個 sub-reviewer 的 partial report
// 檔名（deep-review-partial-<index>.md，index 為 1-based）。
func deepReviewPartialName(index int) string {
	return fmt.Sprintf("deep-review-partial-%d.md", index)
}

// withParallelDeepReviewer 把第 index/count 個 sub-reviewer 的 angle 指派與 partial report
// 檔名注入 promptData，讓 deep-reviewer 模板只 render 被分配的 angle 並輸出到 partial report。
func withParallelDeepReviewer(index, count int, angles []int, partialName string) promptOption {
	return func(d *promptData) {
		d.ReviewerIndex = index
		d.ReviewerCount = count
		d.AssignedAngles = angles
		d.PartialReportName = partialName
	}
}

// withSynthesizerReports 把所有 sub-reviewer 的 partial report 完整內文注入 promptData，
// 供 synthesizer 模板內嵌合併（不是只給路徑）。
func withSynthesizerReports(reports []includeContent) promptOption {
	return func(d *promptData) {
		d.ReviewerCount = len(reports)
		d.PartialReports = reports
	}
}

func generatePrompt(ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, role protocol.Role, round, iteration int, opts ...promptOption) (string, error) {
	tmpl, err := loadRoleTemplate(role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := resolveLocale()
	var roleInc []string
	if rc, ok := cfg.Roles[string(role)]; ok {
		roleInc = rc.Includes
	}
	var repoMap map[string]string
	if len(cfg.Workspace.Repos) > 0 {
		if runnerWs.Root != ws.Root {
			// worktree 模式：組合目錄下 repo 子目錄以 name 命名，使用相對路徑讓 coder 在正確邊界內作業
			featureRepos := make(map[string]bool, len(feature.Repos))
			for _, r := range feature.Repos {
				featureRepos[r] = true
			}
			repoMap = make(map[string]string, len(cfg.Workspace.Repos))
			for name := range cfg.Workspace.Repos {
				if len(feature.Repos) > 0 && !featureRepos[name] {
					continue
				}
				repoMap[name] = name
			}
		} else {
			repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
		}
	}
	data := promptData{
		Feature:             feature,
		Project:             cfg.Project,
		Role:                role,
		Round:               round,
		Iteration:           iteration,
		Config:              cfg,
		DotDir:              runnerWs.DotDir(),
		Locale:              locale,
		LocaleName:          localeName,
		RoleInstructions:    roleInstructions(cfg, role),
		ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, cfg.Project.Includes)...),
		RoleIncludes:        loadIncludes(ws.Root, roleInc),
		PlanningDoc:         loadPlanningDocs(ws.Root, feature.ID),
		RepoMap:             repoMap,
		ProfileInstructions: loadProfiles(ws, feature.ID, cfg),
	}
	for _, opt := range opts {
		opt(&data)
	}
	for _, opt := range opts {
		opt(&data)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// promptResult 是 prefetch goroutine 透過 channel 回傳的生成結果。
type promptResult struct {
	prompt string
	err    error
}

// promptPrefetch 保存一個已在背景啟動的 prompt 預生成任務；以 role+round 為 key，
// 消費端 mismatch 時丟棄並退回同步生成，確保不會用錯 prompt。
type promptPrefetch struct {
	role  protocol.Role
	round int
	ch    chan promptResult
}

// prefetchablePhase 回報某 phase 是否會在主迴圈頂端走同步 generatePrompt（line 530），
// 只有這些 phase 值得預生成 prompt。reviewing 僅在非平行模式下才走頂端路徑
// （平行 runReviewTestParallel 自行呼叫 generatePrompt，不經頂端）。
func prefetchablePhase(phase protocol.Phase, cfg protocol.Config) bool {
	switch phase {
	case protocol.PhaseCoding, protocol.PhaseAmending, protocol.PhaseTesting, protocol.PhaseAccepting:
		return true
	case protocol.PhaseReviewing:
		return !cfg.ParallelReviewTest
	default:
		return false
	}
}

func runLoop(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner, commitStrategy string) error {
	if ops == nil {
		ops = gitops.New(ws.Root, ws, cfg)
	}
	featureID := feature.ID

	// 解析本次 run 的 active profile：用 s.Profile 當 override 確保與啟動時一致。
	// 無 profiles 區段時回 full（所有 role 啟用），既有行為不變。
	profileName, pc, err := protocol.ResolveProfile(cfg, feature, s.Profile)
	if err != nil {
		return err
	}
	s.Profile = profileName

	if s.Phase == protocol.PhaseInit {
		hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
		initHooks := resolveHooks(cfg, feature, protocol.PhaseDesigning)
		if err := executePhaseHooks(ctx, ws, featureID, &s, initHooks["pre"], protocol.PhaseDesigning, "pre", hookLogDir); err != nil {
			return err
		}

		var err error
		s, err = state.Transition(s, protocol.PhaseDesigning, protocol.RoleDesigner)
		if err != nil {
			return err
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (init→designing): %w", err)
		}
		if err := ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
			slog.Warn("sync feature status failed", "feature", featureID, "phase", s.Phase, "error", err)
		}

		if err := executePhaseHooks(ctx, ws, featureID, &s, initHooks["post"], protocol.PhaseDesigning, "post", hookLogDir); err != nil {
			return err
		}
	}

	// resume 時清除當前 phase 可能的半成品 artifact，防止 SIGKILL 後 nextPhaseAfter
	// 讀到不完整的 report 誤以為 phase 完成
	cleanStaleArtifact(ws, featureID, s.Phase, s.Round)

	designerEscalations := 0
	const maxDesignerEscalations = 2

	// P3：per-round auto-commit 改為背景執行，不阻塞下一個 phase 的 prompt 生成與 runner 啟動。
	// defer Wait 一次涵蓋 runLoop 所有 return 路徑，確保 process exit / batch 結束前背景 commit 都完成。
	var commitWG sync.WaitGroup
	defer commitWG.Wait()

	// P1：保存上一輪 transition 後背景啟動的 prompt 預生成；消費端以 role+round 比對。
	var pending *promptPrefetch

	for s.Active {
		if ctx.Err() != nil {
			s.Active = false
			s.StopReason = "interrupted"
			if err := ws.WriteState(featureID, s); err != nil {
				slog.Warn("write state failed", "feature", featureID, "error", err)
			}
			return ctx.Err()
		}

		phase := s.Phase
		role := state.PhaseToRole(phase)

		if phase == protocol.PhaseDone || phase == protocol.PhasePendingReview || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention || phase == protocol.PhaseAbandoned {
			break
		}

		// pass-through：role 不在 active profile 時，沿成功路徑的下一個合法邊跳過，
		// 不呼叫 runner、不檢查 artifact、不跑 guard。coder 永遠啟用故不會被跳過。
		if role != "" && !pc.EnablesRole(role) {
			next, nextRole := successorPhase(phase)
			newState, err := state.Transition(s, next, nextRole)
			if err != nil {
				return fmt.Errorf("pass-through transition %s→%s: %w", phase, next, err)
			}
			s = newState
			if err := ws.WriteState(featureID, s); err != nil {
				return fmt.Errorf("write state (skip %s): %w", phase, err)
			}
			_ = ws.SyncFeatureStatus(featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "phase-skipped", Phase: phase, Role: role, Round: s.Round,
				Runner: s.Runner, Detail: "role not in profile " + profileName,
			})
			fmt.Printf("[round %d] %s — skipped (not in profile %s)\n", s.Round, phase, profileName)
			continue
		}

		// S6：reviewing phase 啟用平行 reviewer + tester（兩者皆 read-only、共用 worktree）。
		if phase == protocol.PhaseReviewing && cfg.ParallelReviewTest &&
			pc.EnablesRole(protocol.RoleReviewer) && pc.EnablesRole(protocol.RoleTester) {
			cont, err := runReviewTestParallel(ctx, ws, runnerWs, feature, cfg, &s, ops, newRunner)
			if err != nil {
				return err
			}
			if !cont {
				break
			}
			continue
		}

		// F063：deep-reviewing phase 由自癒循環接管 — deep reviewer FAIL 時在同一 phase
		// 內 spawn mini-coder + re-verifier 修正，通過才放行 accepting，不回主迴圈重跑整條流程。
		if phase == protocol.PhaseDeepReviewing {
			cont, err := runDeepReviewPhase(ctx, ws, runnerWs, feature, cfg, &s, ops, newRunner, commitStrategy)
			if err != nil {
				return err
			}
			if !cont {
				break
			}
			continue
		}

		if stop, reason := state.ShouldStop(s); stop {
			s.Active = false
			s.StopReason = reason
			s.Phase = protocol.PhaseNeedsAttention
			if err := ws.WriteState(featureID, s); err != nil {
				slog.Warn("write state failed", "feature", featureID, "error", err)
			}
			if err := ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
				slog.Warn("sync feature status failed", "feature", featureID, "error", err)
			}
			ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner})
			slog.Info("run stopped", "feature", featureID, "reason", reason, "round", s.Round)
			fmt.Printf("  stopped: %s\n", reason)
			return nil
		}

		// 清除上一輪遺留的 escalation，避免 designer escalation 後 coder 重跑又讀到舊的
		if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
			os.Remove(filepath.Join(ws.RoundDir(featureID, s.Round), protocol.EscalationFile))
		}

		// 清除上一輪遺留的 feature-level 產出物，避免舊文件通過新一輪的 guard 檢查
		if phase == protocol.PhaseTesting || phase == protocol.PhaseAmending {
			os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport))
			os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.CommitPlan))
		}

		// F046：testing phase 啟動 Tester 前，先跑環境 health check（F045 pre-testing
		// hooks 已於上一輪迴圈底部執行完畢）。失敗且 recovery 無法救回則 escalate。
		if phase == protocol.PhaseTesting {
			testStrat, tsErr := ws.ReadTestStrategy(featureID)
			if tsErr != nil {
				slog.Warn("read test-strategy failed", "feature", featureID, "error", tsErr)
			}
			hc := health.ResolveHealthCheck(cfg.HealthCheck, testStrat.HealthCheck)
			if hc != nil {
				fmt.Printf("[round %d] testing — running health check\n", s.Round)
				if err := health.RunHealthCheck(*hc, healthCheckExecutor(ctx, hc.Timeout)); err != nil {
					ws.AppendEvent(featureID, protocol.Event{
						Type: "health-check-failed", Phase: s.Phase, Role: protocol.RoleTester,
						Round: s.Round, Detail: err.Error(), Runner: s.Runner,
					})
					newState, transErr := state.Transition(s, protocol.PhaseNeedsAttention, "")
					if transErr != nil {
						return fmt.Errorf("health check transition: %w", transErr)
					}
					s = newState
					s.Active = false
					s.StopReason = "health-check-failed"
					_ = ws.WriteState(featureID, s)
					_ = ws.SyncFeatureStatus(featureID, s.Phase)
					slog.Info("run stopped", "feature", featureID, "reason", "health-check-failed", "round", s.Round)
					fmt.Printf("  health check failed, escalated to needs-attention\n")
					return nil
				}
				fmt.Printf("[round %d] testing — health check passed\n", s.Round)
			}
		}

		if phase == protocol.PhaseCoding && s.Round == 1 {
			if err := captureBaselineOnce(ws, ops, featureID, feature.Repos); err != nil {
				return err
			}
		}

		// deep-reviewing phase 已由 runDeepReviewPhase 接管（含 deep_model 解析與跳過邏輯），
		// 故此處 role 必不為 deep-reviewer，直接走 profile-aware 解析。
		model, err := protocol.ResolveProfileModel(cfg, s.Runner, role, pc)
		if err != nil {
			s.Active = false
			s.StopReason = "model-error"
			_ = ws.WriteState(featureID, s)
			return fmt.Errorf("model resolution failed: %w", err)
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
			Runner: s.Runner, Model: model,
		})

		slog.Info("phase transition", "feature", featureID, "phase", phase, "role", role, "round", s.Round, "model", model)

		// P1：優先取用上一輪背景預生成的 prompt（role+round 比對）；prefetch 失敗或無
		// matching prefetch 則退回同步 generatePrompt，同步亦失敗才用 minimal prompt。
		var prompt string
		gotPrefetch := false
		if pending != nil && pending.role == role && pending.round == s.Round {
			res := <-pending.ch
			if res.err == nil {
				prompt = res.prompt
				gotPrefetch = true
			}
		}
		pending = nil // 已消費或不匹配，一律清掉避免下一輪誤用
		if !gotPrefetch {
			p, gerr := generatePrompt(ws, runnerWs, feature, cfg, role, s.Round, 0)
			if gerr != nil {
				p = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
			}
			prompt = p
		}

		logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(s.Round, string(role)))
		r := newRunner(logPath, model)

		if runnerWs.Root != ws.Root {
			syncFeatureToWorktree(ws, runnerWs, featureID, s.Round)
		}

		// 背景即時 sync：runner 執行期間每 2 秒把 worktree 的 protocol 檔案同步回 main
		var stopSync func()
		if runnerWs.Root != ws.Root {
			stopSync = startLiveSync(runnerWs, ws, featureID, s.Round)
		}

		if model != "" {
			fmt.Printf("[round %d] %s (%s) — invoking %s (model: %s)\n", s.Round, phase, role, s.Runner, model)
		} else {
			fmt.Printf("[round %d] %s (%s) — invoking %s\n", s.Round, phase, role, s.Runner)
		}

		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", s.Runner, "model", model, "round", s.Round, "status", "started")
		invokeStart := time.Now()
		result, err := r.Run(ctx, prompt)
		invokeDur := time.Since(invokeStart)
		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", s.Runner, "model", model, "round", s.Round, "status", "completed", "duration_ms", invokeDur.Milliseconds())

		if stopSync != nil {
			stopSync()
		}
		if runnerWs.Root != ws.Root {
			if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, s.Round); serr != nil {
				slog.Warn("sync from worktree failed", "feature", featureID, "round", s.Round, "error", serr)
			}
		}
		if err != nil {
			if ctx.Err() == context.Canceled {
				s.Active = false
				s.StopReason = "interrupted"
				_ = ws.WriteState(featureID, s)
				return ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			_ = ws.WriteState(featureID, s)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: phase, Role: role, Round: s.Round,
				Status: "error", Detail: err.Error(),
				Runner: s.Runner, Model: model,
			})
			return err
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status: fmt.Sprintf("exit-%d", result.ExitCode),
			Runner: s.Runner, Model: model,
		})

		if runner.IsHardError(result) {
			s.Active = false
			s.StopReason = "hard-error"
			_ = ws.WriteState(featureID, s)
			return fmt.Errorf("runner returned hard error (exit 2)")
		}

		if runner.IsSoftFail(result) {
			s.Phase = protocol.PhaseBlocked
			s.Active = false
			s.StopReason = "soft-fail"
			_ = ws.WriteState(featureID, s)
			_ = ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked)
			return nil
		}

		if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
			guardResult := guard.Check(ws, featureID, ops)
			if !guardResult.Pass {
				s.Phase = protocol.PhaseNeedsAttention
				s.Active = false
				s.StopReason = strings.Join(guardResult.Errors, "; ")
				_ = ws.WriteState(featureID, s)
				_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "guard-fail", Phase: phase, Role: role, Round: s.Round,
					Detail: s.StopReason, Runner: s.Runner,
				})
				return nil
			}

			// P3：guard 通過後才在背景啟動 per-round auto-commit。guard.Check 已先觀察乾淨的
			// working tree，背景 commit 不再與 guard 競爭，也不阻塞下一個 phase 的啟動。
			// 行為變更：guard 失敗（→ needs-attention）那輪不再自動 wip-commit，未提交 diff 便於人工檢視。
			if commitStrategy == "per-round" && runnerWs.Root != ws.Root {
				commitWG.Add(1)
				go func(wtRoot string, round int) {
					defer commitWG.Done()
					if err := ops.Commit(wtRoot, featureID, fmt.Sprintf("wip(%s): round %d", featureID, round)); err != nil {
						slog.Error("auto-commit failed", "feature", featureID, "round", round, "error", err)
					} else {
						slog.Info("auto-commit", "feature", featureID, "round", round, "strategy", "per-round")
					}
				}(runnerWs.Root, s.Round)
			}
		}

		next, nextRole, stopReason := nextPhaseAfter(ws, featureID, s)

		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
		}

		loopHooks := resolveHooks(cfg, feature, next)
		hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")

		if err := executePhaseHooks(ctx, ws, featureID, &s, loopHooks["pre"], next, "pre", hookLogDir); err != nil {
			return err
		}

		// 防止 coder ↔ designer 無限 escalation 循環
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

		// W1：reviewer FAIL（→ amending）時追蹤連續無進展輪次。
		// 用轉換前的 s.Round 讀本輪 review-report.md 的失敗計數，與上輪基準比較：
		// 持平或更差 → ConsecutiveNoProgress++；改善 → reset。首輪（尚無基準）只建立 LastFailCount。
		if next == protocol.PhaseAmending {
			cur := reviewFailCount(ws, featureID, s.Round)
			// 首輪 amending 僅建立基準、不 increment。額外要求 cur > 0：
			// 否則 review-report 缺失/格式異常使 cur 恆為 0 時，LastFailCount 會一直停在 0，
			// 「首輪」條件每輪都成立，ConsecutiveNoProgress 永遠無法遞增而漏掉 no-progress 停止。
			if newState.LastFailCount == 0 && newState.ConsecutiveNoProgress == 0 && cur > 0 {
				// 首輪 amending：僅建立基準，不 increment
			} else if cur >= newState.LastFailCount {
				newState.ConsecutiveNoProgress++
			} else {
				newState.ConsecutiveNoProgress = 0
			}
			newState.LastFailCount = cur
		}

		s = newState
		if stopReason != "" {
			s.Active = false
			s.StopReason = stopReason
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		_ = ws.SyncFeatureStatus(featureID, s.Phase)

		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner,
		})

		if err := executePhaseHooks(ctx, ws, featureID, &s, loopHooks["post"], next, "post", hookLogDir); err != nil {
			return err
		}

		// P1：post-hooks 跑完後，若下一輪會走頂端同步 generatePrompt，背景預生成其 prompt，
		// 與下一輪頂端的 syncFeatureToWorktree/startLiveSync 並行。放在 post-hooks 之後啟動，
		// 完全避開「hook 改寫 prompt 輸入檔（planning docs / includes）→ prefetch 讀到舊內容」的競爭。
		// key 用 PhaseToRole(s.Phase) 與 s.Round，確保與消費端的 role/round 比對一致。
		if s.Active {
			nextRole := state.PhaseToRole(s.Phase)
			if prefetchablePhase(s.Phase, cfg) && nextRole != "" && pc.EnablesRole(nextRole) {
				ch := make(chan promptResult, 1)
				pending = &promptPrefetch{role: nextRole, round: s.Round, ch: ch}
				go func(role protocol.Role, round int) {
					p, gerr := generatePrompt(ws, runnerWs, feature, cfg, role, round, 0)
					ch <- promptResult{prompt: p, err: gerr}
				}(nextRole, s.Round)
			}
		}
	}

	switch s.Phase {
	case protocol.PhasePendingReview:
		s.Active = false
		s.StopReason = "pending-review"
		_ = ws.WriteState(featureID, s)
		_ = ws.SyncFeatureStatus(featureID, protocol.PhasePendingReview)
		fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, s.Round, featureID)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		_ = ws.WriteState(featureID, s)
		_ = ws.SyncFeatureStatus(featureID, protocol.PhaseDone)
		fmt.Printf("\nFeature %s complete (%d rounds)\n", featureID, s.Round)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			_ = ws.WriteState(featureID, s)
		}
	}
	return nil
}

// nextPhaseAfter 根據目前 phase 和 artifacts 決定下一個 phase，第三個回傳值為 escalation 停止原因
func nextPhaseAfter(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.Phase, protocol.Role, string) {
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
		return protocol.PhaseCoding, protocol.RoleCoder, ""

	case protocol.PhaseCoding, protocol.PhaseAmending:
		if esc := readEscalation(ws, featureID, s.Round); esc.Needed {
			if isDesignerEscalation(esc.Reason) {
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
		if reviewPassed(ws, featureID, s.Round, protocol.ReviewReport) {
			return protocol.PhaseTesting, protocol.RoleTester, ""
		}
		return protocol.PhaseAmending, protocol.RoleCoder, ""

	case protocol.PhaseTesting:
		if esc := readEscalation(ws, featureID, s.Round); esc.Needed {
			if isDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
		if result.Pass {
			return protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer, ""
		}
		if !verifyPassed(ws, featureID, s.Round) {
			return protocol.PhaseAmending, protocol.RoleCoder, ""
		}
		return protocol.PhaseNeedsAttention, "", strings.Join(result.Errors, "; ")

	case protocol.PhaseDeepReviewing:
		// deep-reviewing 由 runDeepReviewPhase 完整接管（F063）：在正常執行路徑上，
		// 主迴圈一遇到此 phase 即呼叫 runDeepReviewPhase 並 continue/break，不會落到這裡。
		// 此 case 僅保留供 dry-run 診斷等間接查詢使用，回傳值需符合 F063 設計意圖：
		// deep-review FAIL 後走 needs-attention（自癒已在 phase 內完成），不回 amending。
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.DeepReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DeepReviewReport
		}
		if reviewPassed(ws, featureID, s.Round, protocol.DeepReviewReport) {
			return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
		}
		return protocol.PhaseNeedsAttention, "", "deep-review self-heal exhausted"

	case protocol.PhaseAccepting:
		report := filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.FinalReport
		}
		return protocol.PhasePendingReview, "", ""

	default:
		return protocol.PhaseDone, "", ""
	}
}

// successorPhase 回傳成功路徑上的下一個 phase 與其 role，皆為合法 state 邊。
// 用於 pass-through 跳過未啟用的 role；pending-review 的 role 為空字串。
func successorPhase(p protocol.Phase) (protocol.Phase, protocol.Role) {
	switch p {
	case protocol.PhaseDesigning:
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

// runReviewTestParallel 在 reviewing phase 同時跑 reviewer 與 tester（皆 read-only、共用
// worktree），兩者完成後合併判定。回傳 (cont, err)：cont 為 true 表示主迴圈應 continue
// 接手後續 phase（deep-reviewing 或 amending）；cont 為 false 且 err 為 nil 表示已落入
// 終止狀態（blocked / needs-attention），主迴圈應 break；err 非 nil 表示 hard error 直接中止。
func runReviewTestParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner) (bool, error) {
	featureID := feature.ID
	round := s.Round

	reviewModel, err := protocol.ResolveModel(cfg, s.Runner, protocol.RoleReviewer)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("model resolution failed: %w", err)
	}
	testModel, err := protocol.ResolveModel(cfg, s.Runner, protocol.RoleTester)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("model resolution failed: %w", err)
	}

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}

	type runOutcome struct {
		role   protocol.Role
		model  string
		result *runner.Result
		err    error
	}

	runRole := func(role protocol.Role, model string) runOutcome {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: protocol.PhaseReviewing, Role: role, Round: round,
			Runner: s.Runner, Model: model,
		})
		prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, round, 0)
		if err != nil {
			prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
		}
		logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(role)))
		r := newRunner(logPath, model)
		res, runErr := r.Run(ctx, prompt)
		return runOutcome{role: role, model: model, result: res, err: runErr}
	}

	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	fmt.Printf("[round %d] reviewing — running reviewer + tester in parallel (%s)\n", round, s.Runner)

	var wg sync.WaitGroup
	outcomes := make([]runOutcome, 2)
	wg.Add(2)
	go func() { defer wg.Done(); outcomes[0] = runRole(protocol.RoleReviewer, reviewModel) }()
	go func() { defer wg.Done(); outcomes[1] = runRole(protocol.RoleTester, testModel) }()
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
				s.Active = false
				s.StopReason = "interrupted"
				_ = ws.WriteState(featureID, *s)
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			_ = ws.WriteState(featureID, *s)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: s.Runner, Model: o.model,
			})
			return false, o.err
		}
	}

	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: s.Runner, Model: o.model,
		})
	}

	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			s.Active = false
			s.StopReason = "hard-error"
			_ = ws.WriteState(featureID, *s)
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			s.Phase = protocol.PhaseBlocked
			s.Active = false
			s.StopReason = "soft-fail"
			_ = ws.WriteState(featureID, *s)
			_ = ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked)
			return false, nil
		}
	}

	guardResult := guard.Check(ws, featureID, ops)
	if !guardResult.Pass {
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = strings.Join(guardResult.Errors, "; ")
		_ = ws.WriteState(featureID, *s)
		_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "guard-fail", Phase: protocol.PhaseReviewing, Round: round,
			Detail: s.StopReason, Runner: s.Runner,
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
	verifyOK := verifyPassed(ws, featureID, round)

	// reviewer FAIL 或 tester verify 未過 → amending（合法邊 reviewing→amending）。
	if !reviewOK || !verifyOK {
		return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
	}

	// 兩者皆 PASS：tester 必須備齊 final-report / commit-plan 等抵達 accepting 的 artifact。
	if testGuard := guard.CheckTestingToAccepting(ws, featureID, round); !testGuard.Pass {
		return parallelNeedsAttention(ws, featureID, s, strings.Join(testGuard.Errors, "; "))
	}

	// 沿合法邊兩跳：reviewing→testing→deep-reviewing，由主迴圈在 deep-reviewing 接手。
	if cont, err := parallelTransition(ws, featureID, s, protocol.PhaseTesting, protocol.RoleTester); !cont || err != nil {
		return cont, err
	}
	return parallelTransition(ws, featureID, s, protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer)
}

// parallelTransition 執行一次合法 state 轉換並寫回，供平行 review/test 合併後推進 phase。
func parallelTransition(ws *protocol.Workspace, featureID string, s *protocol.State, to protocol.Phase, role protocol.Role) (bool, error) {
	newState, err := state.Transition(*s, to, role)
	if err != nil {
		return false, fmt.Errorf("parallel transition %s→%s: %w", s.Phase, to, err)
	}
	*s = newState
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (%s): %w", s.Phase, err)
	}
	_ = ws.SyncFeatureStatus(featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// parallelNeedsAttention 把 state 落入 needs-attention 並寫回，回傳 (false, nil) 讓主迴圈 break。
func parallelNeedsAttention(ws *protocol.Workspace, featureID string, s *protocol.State, reason string) (bool, error) {
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	s.StopReason = reason
	_ = ws.WriteState(featureID, *s)
	_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
	return false, nil
}

// runDeepReviewParallel 在 deep-reviewing phase 內平行 spawn len(groups) 個 sub-reviewer，
// 各自只跑分配到的 review angle 並寫出 deep-review-partial-<i>.md，全部完成後再 spawn 一個
// synthesizer 把所有 partial report 合併成單一 deep-review-report.md（格式與單 agent 完全相同）。
// 全程維持 deep-reviewing phase。sub-reviewer 與 synthesizer 皆 read-only，共用同一 worktree。
//
// 回傳 (ok, err)：語意同 runDeepSubRole；ok 為 true 時 deep-review-report.md 已產出，
// caller 接續走 reviewPassed → accepting / self-heal 分支。
func runDeepReviewParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner, deepModel string, groups [][]int, round int) (bool, error) {
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

	fmt.Printf("[round %d] deep-reviewing — running %d parallel sub-reviewers (%s, model: %s)\n", round, len(groups), s.Runner, deepModel)

	outcomes := make([]runOutcome, len(groups))
	var wg sync.WaitGroup
	for i, angles := range groups {
		wg.Add(1)
		go func(i int, angles []int) {
			defer wg.Done()
			idx := i + 1
			partialName := deepReviewPartialName(idx)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
				Runner: s.Runner, Model: deepModel,
			})
			prompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleDeepReviewer, round, 0,
				withParallelDeepReviewer(idx, len(groups), angles, partialName))
			if perr != nil {
				prompt = fmt.Sprintf("You are deep sub-reviewer %d for feature %s, round %d. Read .4x/%s/ for context.", idx, featureID, round, featureID)
			}
			logPath := filepath.Join(runner.LogDir(ws, featureID), runner.DeepReviewerLogFileName(round, idx))
			r := newRunner(logPath, deepModel)
			res, runErr := r.Run(ctx, prompt)
			outcomes[i] = runOutcome{index: idx, result: res, err: runErr}
		}(i, angles)
	}
	wg.Wait()

	// runner 執行錯誤分類：context cancel → interrupted；其餘 → runner-error needs-attention。
	for _, o := range outcomes {
		if o.err != nil {
			cleanup()
			if ctx.Err() == context.Canceled {
				s.Active = false
				s.StopReason = "interrupted"
				_ = ws.WriteState(featureID, *s)
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			_ = ws.WriteState(featureID, *s)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: s.Runner, Model: deepModel,
			})
			return false, o.err
		}
	}
	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: s.Runner, Model: deepModel,
		})
	}
	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			cleanup()
			s.Active = false
			s.StopReason = "hard-error"
			_ = ws.WriteState(featureID, *s)
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			cleanup()
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "deep-reviewer-soft-fail"
			_ = ws.WriteState(featureID, *s)
			_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
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
	if err := ws.WriteState(featureID, *s); err != nil {
		cleanup()
		return false, fmt.Errorf("write state (synthesizer): %w", err)
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Runner: s.Runner, Model: deepModel,
	})
	synthPrompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleSynthesizer, round, 0,
		withSynthesizerReports(partials))
	if perr != nil {
		synthPrompt = fmt.Sprintf("You are the deep review synthesizer for feature %s, round %d. Read .4x/%s/ for context.", featureID, round, featureID)
	}
	synthLog := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(protocol.RoleSynthesizer)))
	synthRunner := newRunner(synthLog, deepModel)
	fmt.Printf("[round %d] deep-reviewing (synthesizer) — invoking %s (model: %s)\n", round, s.Runner, deepModel)
	synthRes, synthErr := synthRunner.Run(ctx, synthPrompt)
	if synthErr != nil {
		cleanup()
		if ctx.Err() == context.Canceled {
			s.Active = false
			s.StopReason = "interrupted"
			_ = ws.WriteState(featureID, *s)
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "runner-error"
		_ = ws.WriteState(featureID, *s)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
			Status: "error", Detail: synthErr.Error(), Runner: s.Runner, Model: deepModel,
		})
		return false, synthErr
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Status: fmt.Sprintf("exit-%d", synthRes.ExitCode), Runner: s.Runner, Model: deepModel,
	})
	if runner.IsHardError(synthRes) {
		cleanup()
		s.Active = false
		s.StopReason = "hard-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(synthRes) {
		cleanup()
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "synthesizer-soft-fail"
		_ = ws.WriteState(featureID, *s)
		_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
		return false, nil
	}

	cleanup()
	// sub-reviewer 與 synthesizer 皆 read-only：跑一次 guardrail 確認沒越界改檔。
	if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
		return ok, err
	}
	return true, nil
}

// runDeepReviewPhase 在 deep-reviewing phase 內執行自癒循環：先跑 deep reviewer，FAIL 時
// 不回主迴圈，而是在同一 phase 內反覆 spawn mini-coder（只修被點名的 issue）與 re-verifier
// （只驗舊 issue + 掃本輪新 diff），通過才推進 accepting；最多跑 max_fix_rounds 輪，超過則
// 維持 FAIL 報告並 escalate 到 needs-attention。
//
// 回傳 (cont, err)：cont 為 true 表示主迴圈應 continue（已推進 accepting 或跳過 deep review）；
// cont 為 false 且 err 為 nil 表示已落入終止狀態（needs-attention / blocked），主迴圈應 break；
// err 非 nil 表示 hard error 或 context cancel，直接中止。
func runDeepReviewPhase(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner, commitStrategy string) (bool, error) {
	featureID := feature.ID
	round := s.Round

	// active profile 用於解析 mini-coder 的 coder model（含 profile 的 coder_model 覆蓋）。
	_, pc, err := protocol.ResolveProfile(cfg, feature, s.Profile)
	if err != nil {
		s.Active = false
		s.StopReason = "profile-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("resolve profile: %w", err)
	}

	// 1. 解析 deep_model（deep_model 掛在 reviewer role 上）；未設定時跳過 deep review 直接 accepting。
	deepModel, err := protocol.ResolveDeepModel(cfg, s.Runner, protocol.RoleReviewer)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("deep model resolution failed: %w", err)
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
		_ = ws.SyncFeatureStatus(featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: round,
			Runner: s.Runner, Detail: "deep_model not configured, skipping deep review",
		})
		fmt.Printf("[round %d] deep-reviewing — skipped (no deep_model configured)\n", round)
		return true, nil
	}

	// 2. 跑 deep reviewer：依設定走平行 N sub-reviewer + synthesizer，或 fallback 單 agent。
	s.Role = protocol.RoleDeepReviewer
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (deep-reviewer): %w", err)
	}
	groups := protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount)
	if len(groups) > 1 {
		// 平行模式：N sub-reviewer 各寫 partial report，synthesizer 合併成 deep-review-report.md。
		if ok, err := runDeepReviewParallel(ctx, ws, runnerWs, feature, cfg, s, ops, newRunner, deepModel, groups, round); !ok || err != nil {
			return ok, err
		}
	} else {
		// fallback 單 agent：deep reviewer 直接輸出 deep-review-report.md（現行行為）。
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleDeepReviewer, deepModel, runner.LogFileName(round, string(protocol.RoleDeepReviewer)), round, 0); !ok || err != nil {
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
		autoDiscoverFeatures(ws, feature, cfg, round)
		return deepTransitionAccepting(ws, featureID, s)
	}

	// 4. FAIL → 內部自癒循環。
	maxFix := protocol.ResolveMaxFixRounds(cfg, protocol.RoleDeepReviewer)
	coderModel, err := protocol.ResolveProfileModel(cfg, s.Runner, protocol.RoleCoder, pc)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("coder model resolution failed: %w", err)
	}
	reviewModel, err := protocol.ResolveModel(cfg, s.Runner, protocol.RoleReviewer)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("reviewer model resolution failed: %w", err)
	}

	for iter := 1; iter <= maxFix; iter++ {
		fmt.Printf("[round %d] deep-reviewing — self-heal iteration %d/%d\n", round, iter, maxFix)

		// 4a. mini-coder（model = coder model，不用昂貴 deep_model），phase 維持 deep-reviewing。
		s.Role = protocol.RoleMiniCoder
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (mini-coder): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleMiniCoder, coderModel, runner.DeepFixLogFileName(round, iter), round, iter); !ok || err != nil {
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
			s.Active = false
			s.StopReason = "deep-fix scope-exceed: " + reason
			_ = ws.WriteState(featureID, *s)
			_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleMiniCoder,
				Round: round, Detail: s.StopReason, Runner: s.Runner,
			})
			return false, nil
		}

		// 4c. re-verifier（model = reviewer model，scoped 驗證，不用昂貴 opus），read-only。
		s.Role = protocol.RoleReVerifier
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (re-verifier): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleReVerifier, reviewModel, runner.DeepReverifyLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleReVerifier); !ok || err != nil {
			return ok, err
		}

		// 4d. re-verifier 已把 deep-review-report.md 的 Verdict 改 PASS → accepting。
		if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
			autoDiscoverFeatures(ws, feature, cfg, round)
			return deepTransitionAccepting(ws, featureID, s)
		}
	}

	// 5. 跑滿 maxFix 仍 FAIL → 維持 FAIL 報告 + escalate 到 needs-attention。
	writeDeepEscalation(ws, featureID, round, "blocker",
		fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	s.StopReason = fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix)
	_ = ws.WriteState(featureID, *s)
	_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "escalation", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer,
		Round: round, Detail: s.StopReason, Runner: s.Runner,
	})
	fmt.Printf("[round %d] deep-reviewing — self-heal exhausted (%d iterations), escalating\n", round, maxFix)
	return false, nil
}

// runDeepSubRole 在 deep-reviewing phase 內 spawn 一個子 role（deep-reviewer / mini-coder /
// re-verifier），處理 phase-start/run-end event、prompt 產生、runner 執行與 worktree 同步，
// 並分類 context cancel / runner error / hard error / soft fail。phase 全程維持 deep-reviewing。
//
// 回傳 (ok, err)：ok 為 true 表示 runner 正常結束，caller 可繼續；ok 為 false 且 err 為 nil
// 表示已寫入終止狀態（needs-attention / blocked）；err 非 nil 表示 hard error 或 cancel。
func runDeepSubRole(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, newRunner func(logPath string, model string) runner.Runner, role protocol.Role, model, logName string, round, iteration int) (bool, error) {
	featureID := feature.ID

	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Runner: s.Runner, Model: model,
	})

	prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, round, iteration)
	if err != nil {
		prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
	}
	logPath := filepath.Join(runner.LogDir(ws, featureID), logName)
	r := newRunner(logPath, model)

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}
	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	if model != "" {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s (model: %s)\n", round, role, s.Runner, model)
	} else {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s\n", round, role, s.Runner)
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
			s.Active = false
			s.StopReason = "interrupted"
			_ = ws.WriteState(featureID, *s)
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "runner-error"
		_ = ws.WriteState(featureID, *s)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
			Status: "error", Detail: runErr.Error(), Runner: s.Runner, Model: model,
		})
		return false, runErr
	}

	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Status: fmt.Sprintf("exit-%d", result.ExitCode), Runner: s.Runner, Model: model,
	})

	if runner.IsHardError(result) {
		s.Active = false
		s.StopReason = "hard-error"
		_ = ws.WriteState(featureID, *s)
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		s.Active = false
		s.StopReason = "soft-fail"
		_ = ws.WriteState(featureID, *s)
		_ = ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked)
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
	s.Active = false
	s.StopReason = strings.Join(guardResult.Errors, "; ")
	_ = ws.WriteState(featureID, *s)
	_ = ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: role,
		Round: s.Round, Detail: s.StopReason, Runner: s.Runner,
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
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (accepting): %w", err)
	}
	_ = ws.SyncFeatureStatus(featureID, s.Phase)
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
func autoDiscoverFeatures(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, round int) {
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

	var createdList []discoveredCreated
	for _, d := range kept {
		next, nerr := feat.NextNumber(ws)
		if nerr != nil {
			slog.Warn("auto-discover: next feature number failed", "feature", feature.ID, "title", d.Title, "error", nerr)
			continue
		}
		id := feat.GenerateFeatureID(next, d.Title)
		f := feat.Feature{
			ID:          id,
			Name:        fmt.Sprintf("F%03d: %s", next, d.Title),
			Description: d.Description,
			Status:      feat.StatusNotStarted,
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

	writeDiscoveredFeaturesReport(ws, feature.ID, createdList, skipped, capped)

	fmt.Printf("[round %d] auto-discovered %d feature(s)\n", round, len(createdList))
}

// discoveredCreated 記錄一筆已建立的 feature（id 與 title），供摘要報告列出。
type discoveredCreated struct{ id, title string }

// writeDiscoveredFeaturesReport 寫出 .4x/{featureID}/discovered-features.md 摘要：
// 列出已建立、因重複略過、因超過上限略過的候選 feature。
func writeDiscoveredFeaturesReport(ws *protocol.Workspace, featureID string, created []discoveredCreated, skipped, capped []protocol.DiscoveredFeature) {
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

func reviewPassed(ws *protocol.Workspace, featureID string, round int, reportFile string) bool {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, reportFile))
	if err != nil {
		return false
	}
	result := parseReviewVerdict(string(data))
	return result.Passed && result.CriticalCount == 0 && result.WarningCount == 0
}

// parseReviewVerdict 從 review-report.md 擷取 verdict 與 critical/warning issue 計數
func parseReviewVerdict(content string) protocol.ReviewResult {
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
			strings.HasPrefix(upper, "####") && strings.Contains(upper, "[CRITICAL]") {
			result.CriticalCount++
		}
		if strings.HasPrefix(upper, "[WARNING]") || strings.HasPrefix(upper, "### [WARNING]") ||
			strings.HasPrefix(upper, "####") && strings.Contains(upper, "[WARNING]") {
			result.WarningCount++
		}

		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && !verdictFound && trimmed != "" {
			// strip markdown bold/italic（**PASS** → PASS）
			clean := strings.ToUpper(strings.Trim(trimmed, "*_"))
			if strings.HasPrefix(clean, "PASS") || strings.HasPrefix(clean, "CONDITIONAL PASS") {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
}

// reviewFailCount 回傳指定 round 的 review-report.md 失敗計數（critical + warning）。
// 供 run loop 判斷連續輪次是否有進展（失敗數下降）使用；report 不存在時回 0。
func reviewFailCount(ws *protocol.Workspace, featureID string, round int) int {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.ReviewReport))
	if err != nil {
		return 0
	}
	result := parseReviewVerdict(string(data))
	return result.CriticalCount + result.WarningCount
}

// verifyPassed 檢查 verify.json 的 passed 欄位，用於 guard 失敗時判斷是測試未通過還是 artifact 缺失
func verifyPassed(ws *protocol.Workspace, featureID string, round int) bool {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return false
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil {
		return false
	}
	return ve.Passed
}

// cleanStaleArtifact 清除當前 phase 的 output artifact。
// resume 場景下（SIGKILL、runner-error），runner 可能寫了半成品 report，
// 若不清除，nextPhaseAfter 會誤認為 phase 完成而跳到下一步。
func cleanStaleArtifact(ws *protocol.Workspace, featureID string, phase protocol.Phase, round int) {
	roundDir := ws.RoundDir(featureID, round)
	switch phase {
	case protocol.PhaseCoding, protocol.PhaseAmending:
		os.Remove(filepath.Join(roundDir, protocol.CoderReport))
	case protocol.PhaseReviewing:
		os.Remove(filepath.Join(roundDir, protocol.ReviewReport))
	case protocol.PhaseTesting:
		os.Remove(filepath.Join(roundDir, protocol.TestReport))
		os.Remove(filepath.Join(roundDir, protocol.VerifyFile))
	case protocol.PhaseDeepReviewing:
		os.Remove(filepath.Join(roundDir, protocol.DeepReviewReport))
	case protocol.PhaseAccepting:
		os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport))
		os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.CommitPlan))
	}
}

// roleToResumePhase 根據 role 推斷 blocked/needs-attention 後應恢復到哪個 phase。
// 除了 designer 之外都回 coding，讓 coder 根據上一輪的 report 修正後重走流程。
// tester 失敗也回 coding 而非 testing，避免 tester 反覆重跑相同的失敗。
func roleToResumePhase(role protocol.Role) protocol.Phase {
	switch role {
	case protocol.RoleCoder:
		return protocol.PhaseCoding
	case protocol.RoleReviewer:
		return protocol.PhaseCoding
	case protocol.RoleDeepReviewer:
		return protocol.PhaseCoding
	case protocol.RoleMiniCoder:
		return protocol.PhaseCoding
	case protocol.RoleReVerifier:
		return protocol.PhaseCoding
	case protocol.RoleTester:
		return protocol.PhaseCoding
	case protocol.RoleAcceptor:
		return protocol.PhaseCoding
	default:
		return protocol.PhaseDesigning
	}
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

func dryRunLoop(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State) error {
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)
		prompt, err := generatePrompt(ws, ws, feature, cfg, p.role, 1, 0)
		if err != nil {
			fmt.Printf("  (error: %v)\n\n", err)
			continue
		}
		fmt.Println(prompt)
		fmt.Println()
	}
	return nil
}

// syncFeatureToWorktree 將主 workspace 的 feature 目錄複製到 worktree，
// 確保 runner 能讀到最新的 protocol 檔案（task-brief、上一輪 report 等）
func syncFeatureToWorktree(main, wt *protocol.Workspace, featureID string, round int) {
	srcDir := main.FeatureDir(featureID)
	dstDir := wt.FeatureDir(featureID)
	os.MkdirAll(dstDir, 0o755)

	// state + feature-level 檔案
	for _, name := range []string{protocol.StateFile, protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile} {
		gitops.CopyFileIfExists(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
	}

	// 當前 round 目錄
	if round > 0 {
		srcRound := main.RoundDir(featureID, round)
		dstRound := wt.RoundDir(featureID, round)
		os.MkdirAll(dstRound, 0o755)
		entries, _ := os.ReadDir(srcRound)
		for _, e := range entries {
			if !e.IsDir() {
				gitops.CopyFileIfExists(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name()))
			}
		}
	}
}

// syncFeatureFromWorktree 將 worktree 裡 runner 寫的 protocol 檔案複製回主 workspace。
// 回傳彙整後的 error（任一 MkdirAll / ReadDir / CopyFileIfExists 失敗），讓 caller 能在
// disk full 等情況印出真因，而非只看到下游的 missing-artifact。來源檔不存在不算 error。
func syncFeatureFromWorktree(wt, main *protocol.Workspace, featureID string, round int) error {
	srcDir := wt.FeatureDir(featureID)
	dstDir := main.FeatureDir(featureID)
	var errs []string
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		errs = append(errs, err.Error())
	}

	// feature-level 檔案
	for _, name := range []string{
		protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile,
		protocol.FinalReport, protocol.CommitPlan,
	} {
		if _, err := gitops.CopyFileIfNewer(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	// round 目錄
	srcRound := wt.RoundDir(featureID, round)
	dstRound := main.RoundDir(featureID, round)
	if err := os.MkdirAll(dstRound, 0o755); err != nil {
		errs = append(errs, err.Error())
	}
	entries, err := os.ReadDir(srcRound)
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Sprintf("read round dir: %v", err))
	}
	for _, e := range entries {
		if !e.IsDir() {
			if _, err := gitops.CopyFileIfNewer(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name())); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("sync from worktree: %s", strings.Join(errs, "; "))
	}
	return nil
}

// startLiveSync 啟動背景 goroutine，每 2 秒將 worktree 的 protocol 檔案同步回 main workspace。
// 回傳的 stop function 為阻塞式：close(done) 後 wg.Wait() 確保 in-flight 的 sync 完成才返回，
// 避免 caller 隨即執行的 final sync 與背景 sync 競爭寫同一批檔案。
func startLiveSync(wt, main *protocol.Workspace, featureID string, round int) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := syncFeatureFromWorktree(wt, main, featureID, round); err != nil {
					slog.Warn("live sync failed", "feature", featureID, "round", round, "error", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}
