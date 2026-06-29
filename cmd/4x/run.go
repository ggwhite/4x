package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/spf13/cobra"
)

// runParams 收納 newRunCmd 的 RunE 解析後的參數
type runParams struct {
	ws           *protocol.Workspace
	feature      feat.Feature
	cfg          protocol.Config
	runnerName   string
	manualRunner string
	maxRounds    int
	timeout      int
	runOverrides map[protocol.Phase]protocol.PhaseSpec
}

// resolveRunParams 解析 workspace / feature / config / runner / phase-override，
// 供 RunE 後續流程使用。
func resolveRunParams(featureID string, runnerFlag string, maxRounds int, timeout int, phaseOverrides []string) (*runParams, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return nil, err
	}
	featureID, err = ws.ResolveFeatureID(featureID)
	if err != nil {
		return nil, err
	}
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		return nil, err
	}
	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return nil, err
	}
	syncPlugins(ws.Root, cfg)

	manualRunner := runnerFlag
	runnerName := runnerFlag
	if runnerName == "" {
		runnerName = cfg.Default
	}
	if _, ok := cfg.Runners[runnerName]; !ok {
		return nil, fmt.Errorf("runner %q not found in config", runnerName)
	}
	if maxRounds <= 0 {
		maxRounds = 5
	}
	if _, _, err := protocol.ResolveProfile(cfg, feature, ""); err != nil {
		return nil, err
	}
	overrides, err := orchestrator.ParsePhaseOverrides(phaseOverrides)
	if err != nil {
		return nil, err
	}
	return &runParams{
		ws: ws, feature: feature, cfg: cfg,
		runnerName: runnerName, manualRunner: manualRunner,
		maxRounds: maxRounds, timeout: timeout,
		runOverrides: overrides,
	}, nil
}

// launchBackgroundJSON 在 --json 模式下背景啟動 run 子程序並回傳 JSON 結果
func launchBackgroundJSON(ws *protocol.Workspace, featureID, runnerName, profileFlag string, phaseOverrides []string, maxRounds, timeout int, dryRun, allAngles bool) error {
	bgArgs := []string{"run", featureID}
	if runnerName != "" {
		bgArgs = append(bgArgs, "--runner", runnerName)
	}
	if profileFlag != "" {
		bgArgs = append(bgArgs, "--profile", profileFlag)
	}
	for _, po := range phaseOverrides {
		bgArgs = append(bgArgs, "--phase-override", po)
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
	if allAngles {
		bgArgs = append(bgArgs, "--all-angles")
	}
	if err := ws.InitFeatureDir(featureID); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	logPath := filepath.Join(ws.FeatureDir(featureID), "run.log")
	proc, err := orchestrator.StartBackgroundRun(os.Args[0], bgArgs, cwd, logPath)
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(struct {
		FeatureID string `json:"featureId"`
		Runner    string `json:"runner"`
		MaxRounds int    `json:"maxRounds"`
		PID       int    `json:"pid"`
		LogPath   string `json:"logPath"`
	}{featureID, runnerName, maxRounds, proc.Pid, logPath}, "", "  ")
	fmt.Println(string(data))
	return nil
}

func newRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var dryRun bool
	var jsonOutput bool
	var profileFlag string
	var phaseOverrides []string
	var noNotify bool
	var allAngles bool

	cmd := &cobra.Command{
		Use:   "run <feature-id>",
		Short: "Run the Design-Code-Review-Test loop for a feature",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			p, err := resolveRunParams(args[0], runnerName, maxRounds, timeout, phaseOverrides)
			if err != nil {
				return err
			}
			ws, feature, cfg, featureID := p.ws, p.feature, p.cfg, p.feature.ID

			if jsonOutput {
				return launchBackgroundJSON(ws, featureID, p.runnerName, profileFlag, phaseOverrides, p.maxRounds, p.timeout, dryRun, allAngles)
			}

			if err := orchestrator.CheckDependencies(ws, featureID); err != nil {
				return err
			}

			ops := gitops.New(ws.Root, ws, cfg)
			runnerWs, wtPath, err := setupWorktree(ws, ops, cfg, feature)
			if err != nil {
				return err
			}
			if err := ws.InitFeatureDir(featureID); err != nil {
				return err
			}
			s, err := initOrResumeState(ws, featureID, p.runnerName, p.maxRounds, cfg)
			if err != nil {
				return err
			}

			profileFlag, err = resolveProfileFlag(profileFlag, s, cfg, feature, dryRun)
			if err != nil {
				return err
			}
			profileName, _, err := protocol.ResolveProfile(cfg, feature, profileFlag)
			if err != nil {
				return err
			}
			s.Profile = profileName
			s.Pid = os.Getpid()
			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-start", Phase: s.Phase,
				Role: orchestrator.PhaseToRole(s.Phase), Runner: p.runnerName,
			})
			slog.Info("feature run started", "feature", featureID, "runner", p.runnerName, "maxRounds", p.maxRounds, "profile", s.Profile)

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
			defer orchestrator.DeferRunCleanup(ws, featureID)

			r := orchestrator.NewRunner(orchestrator.Config{
				Ws: ws, RunnerWs: runnerWs, Feature: feature, Cfg: cfg, Ops: ops,
				NewRunner: func(rn, logPath, model string) runner.Runner {
					return runner.NewRunner(runnerWs, rn, cfg.Runners[rn], time.Duration(p.timeout)*time.Second, logPath, model)
				},
				CommitStrategy: commitStrategy, ManualRunner: p.manualRunner, RunOverrides: p.runOverrides,
				ForceAllAngles: allAngles,
			})
			result, loopErr := r.RunLoop(ctx, s)

			return handlePostLoop(ws, featureID, feature, cfg, ops, result, loopErr, wtPath, commitStrategy, p.runnerName, noNotify)
		}),
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name (default: config default)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max iteration rounds (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling plugin")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "start run and return JSON immediately")
	cmd.Flags().StringVar(&profileFlag, "profile", "", "pipeline profile (full/normal/quick or custom); overrides priority-based auto-select")
	cmd.Flags().StringArrayVar(&phaseOverrides, "phase-override", nil, "temporary per-phase runner/model override for this run only, format <phase>:<runner>:<model> (repeatable)")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable OS notification on run completion (overrides config)")
	cmd.Flags().BoolVar(&allAngles, "all-angles", false, "force deep review to run all 11 angles (ignore angle mapping)")
	return cmd
}

// setupWorktree 依 config 設定 worktree isolation，回傳 runner 使用的 workspace 與 worktree 路徑
func setupWorktree(ws *protocol.Workspace, ops gitops.Ops, cfg protocol.Config, feature feat.Feature) (*protocol.Workspace, string, error) {
	if cfg.Isolation != "worktree" {
		return ws, "", nil
	}
	wtPath, err := ops.SetupWorktree(feature.ID, feature.Repos)
	if err != nil {
		return nil, "", fmt.Errorf("worktree setup: %w", err)
	}
	ws.SkipAutoCommit = true
	fmt.Printf("worktree: %s\n", wtPath)
	return &protocol.Workspace{Root: wtPath}, wtPath, nil
}

// initOrResumeState 初始化或恢復 feature 的 state，含 resume recovery
func initOrResumeState(ws *protocol.Workspace, featureID, runnerName string, maxRounds int, cfg protocol.Config) (protocol.State, error) {
	s := protocol.State{
		FeatureID: featureID, Phase: protocol.PhaseInit,
		MaxRounds: maxRounds, Active: true, Runner: runnerName,
		CreatedAt: time.Now(),
	}
	existing, err := ws.ReadState(featureID)
	if err != nil {
		s.Runners = []string{runnerName}
		return s, nil
	}
	if existing.Active && protocol.ProcessAlive(existing.Pid) {
		return s, fmt.Errorf("feature %s is already running (pid %d)", featureID, existing.Pid)
	}
	s = existing
	s.Active = true
	s.Runner = runnerName
	s.StopReason = ""
	s.StopMessage = ""
	if newMax := s.Round + maxRounds; newMax > s.MaxRounds {
		s.MaxRounds = newMax
	}
	if s.Phase == protocol.PhaseDone {
		return s, fmt.Errorf("feature %s is already done", featureID)
	}
	s, err = orchestrator.RecoverState(ws, featureID, s, cfg)
	if err != nil {
		return s, err
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
	return s, nil
}

// resolveProfileFlag 解析 profile flag，必要時彈出互動選單；空值時 fallback 到既有 state
func resolveProfileFlag(profileFlag string, s protocol.State, cfg protocol.Config, feature feat.Feature, dryRun bool) (string, error) {
	if profileFlag == "" && s.Profile == "" && feature.Profile == "" && !dryRun && isInteractiveTerminal() {
		sel, err := selectProfileInteractive(os.Stdin, os.Stdout, cfg, feature)
		if err != nil {
			return "", err
		}
		return sel, nil
	}
	if profileFlag == "" {
		return s.Profile, nil
	}
	return profileFlag, nil
}

// handlePostLoop 處理 run loop 結束後的通知、worktree commit、摘要輸出
func handlePostLoop(ws *protocol.Workspace, featureID string, feature feat.Feature, cfg protocol.Config, ops gitops.Ops, result *orchestrator.Result, loopErr error, wtPath, commitStrategy, runnerName string, noNotify bool) error {
	finalState, err := ws.ReadState(featureID)
	if err != nil {
		slog.Warn("failed to read final state for notification", "feature", featureID, "error", err)
		finalState = protocol.State{Phase: protocol.PhaseBlocked}
	}

	if finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: finalState.Phase, Role: finalState.Role,
			Round: finalState.Round, Status: string(finalState.Phase),
			Runner: finalState.Runner, Notify: protocol.NotifySuccess,
		})
	}

	if !noNotify && protocol.NotificationsEnabled(cfg) {
		level, title, body := runOutcome(featureID, finalState, loopErr)
		sendSystemNotification(level, title, body)
	}

	if wtPath != "" {
		doneOrPending := finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview
		if doneOrPending && commitStrategy == "on-done" {
			if err := ops.Commit(wtPath, featureID, fmt.Sprintf("feat(%s): %s", featureID, feature.Name)); err != nil {
				slog.Error("auto-commit failed", "feature", featureID, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "strategy", commitStrategy)
			}
		}
		for _, line := range orchestrator.WorktreeExitHints(wtPath, featureID, finalState.Phase, commitStrategy) {
			fmt.Println(line)
		}
	}

	printRunSummary(featureID, finalState, result)
	return loopErr
}

// runLoop 保持原簽名供 batch.go / evolve.go 呼叫，內部委託給 orchestrator.Runner.RunLoop
func runLoop(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State, ops gitops.Ops, newRunner func(string, string, string) runner.Runner, commitStrategy string, manualRunner string, runOverrides map[protocol.Phase]protocol.PhaseSpec) error {
	r := orchestrator.NewRunner(orchestrator.Config{
		Ws: ws, RunnerWs: runnerWs, Feature: feature, Cfg: cfg, Ops: ops,
		NewRunner: newRunner, CommitStrategy: commitStrategy,
		ManualRunner: manualRunner, RunOverrides: runOverrides,
	})
	_, err := r.RunLoop(ctx, s)
	return err
}

// printRunSummary 在 run loop 結束後印出摘要與 feature 狀態提示
func printRunSummary(featureID string, finalState protocol.State, result *orchestrator.Result) {
	if result == nil {
		return
	}
	switch {
	case result.TotalCostUSD > 0:
		fmt.Printf("\n── %s: %d rounds, $%.4f total ──\n", featureID, finalState.Round, result.TotalCostUSD)
	case result.TotalTokens > 0:
		fmt.Printf("\n── %s: %d rounds, %s tokens total ──\n", featureID, finalState.Round, orchestrator.FormatTokens(result.TotalTokens))
	}
	switch finalState.Phase {
	case protocol.PhasePendingReview:
		fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, finalState.Round, featureID)
	case protocol.PhaseDone:
		fmt.Printf("\nFeature %s complete (%d rounds)\n", featureID, finalState.Round)
	default:
		// 其他 phase 不印額外提示
	}
}
