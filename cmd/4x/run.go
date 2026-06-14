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

			if jsonOutput {
				bgArgs := []string{"run", featureID}
				if runnerName != "" {
					bgArgs = append(bgArgs, "--runner", runnerName)
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
	return cmd
}

func generatePrompt(ws *protocol.Workspace, runnerWs *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, role protocol.Role, round int) (string, error) {
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

	if s.Phase == protocol.PhaseInit {
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
	}

	// resume 時清除當前 phase 可能的半成品 artifact，防止 SIGKILL 後 nextPhaseAfter
	// 讀到不完整的 report 誤以為 phase 完成
	cleanStaleArtifact(ws, featureID, s.Phase, s.Round)

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

		var model string
		if role == protocol.RoleDeepReviewer {
			deepModel, err := protocol.ResolveDeepModel(cfg, s.Runner, protocol.RoleReviewer)
			if err != nil {
				s.Active = false
				s.StopReason = "model-error"
				_ = ws.WriteState(featureID, s)
				return fmt.Errorf("deep model resolution failed: %w", err)
			}
			if deepModel == "" {
				next, nextRole, stopReason := protocol.PhaseAccepting, protocol.RoleAcceptor, ""
				newState, err := state.Transition(s, next, nextRole)
				if err != nil {
					return fmt.Errorf("skip deep-review transition: %w", err)
				}
				s = newState
				if stopReason != "" {
					s.Active = false
					s.StopReason = stopReason
				}
				if err := ws.WriteState(featureID, s); err != nil {
					return fmt.Errorf("write state (skip deep-review): %w", err)
				}
				_ = syncFeatureStatus(ws, featureID, s.Phase)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
					Runner: s.Runner, Detail: "deep_model not configured, skipping deep review",
				})
				fmt.Printf("[round %d] deep-reviewing — skipped (no deep_model configured)\n", s.Round)
				continue
			}
			model = deepModel
		} else {
			var err error
			model, err = protocol.ResolveModel(cfg, s.Runner, role)
			if err != nil {
				s.Active = false
				s.StopReason = "model-error"
				_ = ws.WriteState(featureID, s)
				return fmt.Errorf("model resolution failed: %w", err)
			}
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
			Runner: s.Runner, Model: model,
		})

		prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, s.Round)
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

		// needs-attention/blocked 保留原 role，讓 recovery 能判斷該回哪個 phase
		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
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
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.DeepReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DeepReviewReport
		}
		if reviewPassed(ws, featureID, s.Round, protocol.DeepReviewReport) {
			return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
		}
		return protocol.PhaseAmending, protocol.RoleCoder, ""

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

		if strings.Contains(upper, "[CRITICAL]") {
			result.CriticalCount++
		}
		if strings.Contains(upper, "[WARNING]") {
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
	return reason == "spec-mismatch" || reason == "criteria-wrong"
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
		prompt, err := generatePrompt(ws, ws, feature, cfg, p.role, 1)
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


