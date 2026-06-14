package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

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
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			if userCfg, err := protocol.ReadUserConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
			} else {
				cfg = protocol.MergeConfig(userCfg, cfg)
			}

			if runnerName == "" {
				runnerName = cfg.Default
			}
			_, ok := cfg.Runners[runnerName]
			if !ok {
				errMsg := fmt.Sprintf("runner %q not found in config", runnerName)
				if jsonOutput {
					return jsonError(errMsg)
				}
				return fmt.Errorf("%s", errMsg)
			}

			if maxRounds <= 0 {
				maxRounds = 5
			}

			// 提早驗證 --profile（unknown profile / 缺 coder）；空值時回 full/auto 不報錯。
			if _, _, err := protocol.ResolveProfile(cfg, feature, profileFlag); err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
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
					return jsonError(fmt.Sprintf("failed to start run: %v", err))
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
					fmt.Fprintf(os.Stderr, "  blocked: %s\n", e)
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
					_ = syncFeatureStatus(ws, featureID, cur.Phase)
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
							fmt.Fprintf(os.Stderr, "  auto-commit failed: %v\n", err)
						}
					}
					fmt.Printf("  branch: 4x/%s\n", featureID)
					fmt.Printf("  to merge: git merge 4x/%s && git worktree remove %s && git branch -d 4x/%s\n", featureID, wtPath, featureID)
				}
			}

			return loopErr
		},
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

func generatePrompt(ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, role protocol.Role, round, iteration int) (string, error) {
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
		Feature:          feature,
		Project:          cfg.Project,
		Role:             role,
		Round:            round,
		Iteration:        iteration,
		Config:           cfg,
		DotDir:           runnerWs.DotDir(),
		Locale:           locale,
		LocaleName:       localeName,
		RoleInstructions: roleInstructions(cfg, role),
		ProjectIncludes:  append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, cfg.Project.Includes)...),
		RoleIncludes:     loadIncludes(ws.Root, roleInc),
		PlanningDoc:      loadPlanningDocs(ws.Root, feature.ID),
		RepoMap:          repoMap,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runLoop(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner, commitStrategy string) error {
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
		if err := syncFeatureStatus(ws, featureID, s.Phase); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
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

	for s.Active {
		if ctx.Err() != nil {
			s.Active = false
			s.StopReason = "interrupted"
			if err := ws.WriteState(featureID, s); err != nil {
				fmt.Fprintf(os.Stderr, "warning: write state: %v\n", err)
			}
			return ctx.Err()
		}

		phase := s.Phase
		role := state.PhaseToRole(phase)

		if phase == protocol.PhaseDone || phase == protocol.PhasePendingReview || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention {
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
			_ = syncFeatureStatus(ws, featureID, s.Phase)
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
				fmt.Fprintf(os.Stderr, "warning: write state: %v\n", err)
			}
			if err := syncFeatureStatus(ws, featureID, s.Phase); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner})
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

		prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, s.Round, 0)
		if err != nil {
			prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
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

		result, err := r.Run(ctx, prompt)

		if stopSync != nil {
			stopSync()
		}
		if runnerWs.Root != ws.Root {
			syncFeatureFromWorktree(runnerWs, ws, featureID, s.Round)
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
			_ = syncFeatureStatus(ws, featureID, protocol.PhaseBlocked)
			return nil
		}

		if commitStrategy == "per-round" && runnerWs.Root != ws.Root &&
			(phase == protocol.PhaseCoding || phase == protocol.PhaseAmending) {
			if err := ops.Commit(runnerWs.Root, featureID, fmt.Sprintf("wip(%s): round %d", featureID, s.Round)); err != nil {
				fmt.Fprintf(os.Stderr, "  auto-commit round %d failed: %v\n", s.Round, err)
			}
		}

		// designer 不改 source code，略過 scope/baseline 檢查
		if role != protocol.RoleDesigner {
			guardResult := guard.Check(ws, featureID, ops)
			if !guardResult.Pass {
				s.Phase = protocol.PhaseNeedsAttention
				s.Active = false
				s.StopReason = strings.Join(guardResult.Errors, "; ")
				_ = ws.WriteState(featureID, s)
				_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "guard-fail", Phase: phase, Role: role, Round: s.Round,
					Detail: s.StopReason, Runner: s.Runner,
				})
				return nil
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
		s = newState
		if stopReason != "" {
			s.Active = false
			s.StopReason = stopReason
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		_ = syncFeatureStatus(ws, featureID, s.Phase)

		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner,
		})

		if err := executePhaseHooks(ctx, ws, featureID, &s, loopHooks["post"], next, "post", hookLogDir); err != nil {
			return err
		}
	}

	switch s.Phase {
	case protocol.PhasePendingReview:
		s.Active = false
		s.StopReason = "pending-review"
		_ = ws.WriteState(featureID, s)
		_ = syncFeatureStatus(ws, featureID, protocol.PhasePendingReview)
		fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, s.Round, featureID)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		_ = ws.WriteState(featureID, s)
		_ = syncFeatureStatus(ws, featureID, protocol.PhaseDone)
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
func runReviewTestParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner) (bool, error) {
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
		syncFeatureFromWorktree(runnerWs, ws, featureID, round)
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
			_ = syncFeatureStatus(ws, featureID, protocol.PhaseBlocked)
			return false, nil
		}
	}

	guardResult := guard.Check(ws, featureID, ops)
	if !guardResult.Pass {
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = strings.Join(guardResult.Errors, "; ")
		_ = ws.WriteState(featureID, *s)
		_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
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
	_ = syncFeatureStatus(ws, featureID, s.Phase)
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
	_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
	return false, nil
}

// runDeepReviewPhase 在 deep-reviewing phase 內執行自癒循環：先跑 deep reviewer，FAIL 時
// 不回主迴圈，而是在同一 phase 內反覆 spawn mini-coder（只修被點名的 issue）與 re-verifier
// （只驗舊 issue + 掃本輪新 diff），通過才推進 accepting；最多跑 max_fix_rounds 輪，超過則
// 維持 FAIL 報告並 escalate 到 needs-attention。
//
// 回傳 (cont, err)：cont 為 true 表示主迴圈應 continue（已推進 accepting 或跳過 deep review）；
// cont 為 false 且 err 為 nil 表示已落入終止狀態（needs-attention / blocked），主迴圈應 break；
// err 非 nil 表示 hard error 或 context cancel，直接中止。
func runDeepReviewPhase(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(logPath string, model string) runner.Runner, commitStrategy string) (bool, error) {
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
		_ = syncFeatureStatus(ws, featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: round,
			Runner: s.Runner, Detail: "deep_model not configured, skipping deep review",
		})
		fmt.Printf("[round %d] deep-reviewing — skipped (no deep_model configured)\n", round)
		return true, nil
	}

	// 2. 跑 deep reviewer。
	s.Role = protocol.RoleDeepReviewer
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (deep-reviewer): %w", err)
	}
	if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
		protocol.RoleDeepReviewer, deepModel, runner.LogFileName(round, string(protocol.RoleDeepReviewer)), round, 0); !ok || err != nil {
		return ok, err
	}
	if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
		return ok, err
	}
	reportPath := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	if _, statErr := os.Stat(reportPath); statErr != nil {
		return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+protocol.DeepReviewReport)
	}

	// 3. PASS → accepting。
	if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
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
				fmt.Fprintf(os.Stderr, "  auto-commit deep-fix %d failed: %v\n", iter, err)
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
			_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
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
	_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
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
func runDeepSubRole(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s *protocol.State, newRunner func(logPath string, model string) runner.Runner, role protocol.Role, model, logName string, round, iteration int) (bool, error) {
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
		syncFeatureFromWorktree(runnerWs, ws, featureID, round)
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
		_ = syncFeatureStatus(ws, featureID, protocol.PhaseBlocked)
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
	_ = syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
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
	_ = syncFeatureStatus(ws, featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// writeDeepReviewFailReport 由 CLI 在 deep-reviewing 終止場景（如 mini-coder scope-exceed）
// 直接寫出 FAIL 的 deep-review-report.md，標注原因供 dashboard 與 acceptor 辨識。
func writeDeepReviewFailReport(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	content := fmt.Sprintf("# Deep Review Report — Round %d\n\n## Summary\nFAIL — %s\n\n## Issues\n### [CRITICAL] %s\n%s\n\n## Verdict\nFAIL\n",
		round, reason, reason, detail)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  write deep-review FAIL report failed: %v\n", err)
	}
}

// writeDeepEscalation 由 CLI 在 deep-reviewing 終止場景寫出 escalation.json，讓 resume 與
// dashboard 能辨識升級原因（scope-change / blocker）。
func writeDeepEscalation(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	esc := protocol.Escalation{Needed: true, Reason: reason, Detail: detail}
	data, _ := json.Marshal(esc)
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.EscalationFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  write deep-review escalation failed: %v\n", err)
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
			if strings.HasPrefix(upper, "PASS") || strings.HasPrefix(upper, "CONDITIONAL PASS") {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
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

func dryRunLoop(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State) error {
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

// syncFeatureFromWorktree 將 worktree 裡 runner 寫的 protocol 檔案複製回主 workspace
func syncFeatureFromWorktree(wt, main *protocol.Workspace, featureID string, round int) {
	srcDir := wt.FeatureDir(featureID)
	dstDir := main.FeatureDir(featureID)
	os.MkdirAll(dstDir, 0o755)

	// feature-level 檔案
	for _, name := range []string{
		protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile,
		protocol.FinalReport, protocol.CommitPlan,
	} {
		gitops.CopyFileIfExists(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
	}

	// round 目錄
	srcRound := wt.RoundDir(featureID, round)
	dstRound := main.RoundDir(featureID, round)
	os.MkdirAll(dstRound, 0o755)
	entries, _ := os.ReadDir(srcRound)
	for _, e := range entries {
		if !e.IsDir() {
			gitops.CopyFileIfExists(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name()))
		}
	}
}

// startLiveSync 啟動背景 goroutine，每 2 秒將 worktree 的 protocol 檔案同步回 main workspace。
// 回傳 stop function，呼叫後停止同步。
func startLiveSync(wt, main *protocol.Workspace, featureID string, round int) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				syncFeatureFromWorktree(wt, main, featureID, round)
			}
		}
	}()
	return func() { close(done) }
}
