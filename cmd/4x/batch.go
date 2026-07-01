package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/ggwhite/4x/internal/batch"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch operations for multiple features",
	}

	cmd.AddCommand(newBatchPlanCmd())
	cmd.AddCommand(newBatchNextCmd())
	cmd.AddCommand(newBatchStopCmd())
	cmd.AddCommand(newBatchRunCmd())
	return cmd
}

func newBatchPlanCmd() *cobra.Command {
	var dryRun bool
	var maxChain int

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Plan batch execution (dependency DAG + Union-Find grouping)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			cfg, _ := ws.LoadMergedConfig()

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []feat.Feature
			for _, f := range features {
				if f.Status != feat.StatusDone && f.Status != feat.StatusAbandoned {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features to batch.")
				return nil
			}

			plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), maxChain)
			if err != nil {
				return err
			}

			if dryRun {
				return printPlan(plan)
			}

			data, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return err
			}
			planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
			if err := os.WriteFile(planPath, data, 0o644); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "plan", "features", len(plan.Schedule), "path", planPath)
			fmt.Printf("Wrote %s\n", planPath)
			return printPlan(plan)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print schedule without writing file")
	cmd.Flags().IntVar(&maxChain, "max-chain", 4, "Maximum chain length per cluster")
	return cmd
}

func printPlan(plan *batch.BatchPlan) error {
	for _, c := range plan.Clusters {
		fmt.Printf("  %s: ", c.ID)
		for i, chain := range c.Chains {
			if i > 0 {
				fmt.Print(" | ")
			}
			for j, fID := range chain {
				if j > 0 {
					fmt.Print(" → ")
				}
				fmt.Print(fID)
			}
		}
		fmt.Println()
	}

	fmt.Printf("\nSchedule (%d features):\n", len(plan.Schedule))
	for _, s := range plan.Schedule {
		after := "—"
		if len(s.CanStartAfter) > 0 {
			after = fmt.Sprintf("after %v", s.CanStartAfter)
		}
		fmt.Printf("  [slot %d] %s %s\n", s.Slot, s.FeatureID, after)
	}
	return nil
}

func newBatchNextCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the next eligible feature to run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
			data, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("no batch-plan.json found, run '4x batch plan' first")
			}

			var plan batch.BatchPlan
			if err := json.Unmarshal(data, &plan); err != nil {
				return fmt.Errorf("invalid batch-plan.json: %w", err)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}
			statusMap := make(map[string]feat.Status)
			featureMap := make(map[string]feat.Feature)
			for _, f := range features {
				statusMap[f.ID] = f.Status
				featureMap[f.ID] = f
			}

			for _, s := range plan.Schedule {
				if batchCompleted(statusMap[s.FeatureID]) {
					continue
				}
				allDone := true
				for _, dep := range s.CanStartAfter {
					if !batchCompleted(statusMap[dep]) {
						allDone = false
						break
					}
				}
				if allDone {
					if !jsonOutput {
						fmt.Println(s.FeatureID)
						return nil
					}

					result := struct {
						FeatureID       string   `json:"featureId"`
						Slot            int      `json:"slot"`
						SubtaskFrontier []string `json:"subtaskFrontier"`
					}{
						FeatureID: s.FeatureID,
						Slot:      s.Slot,
					}

					if f, ok := featureMap[s.FeatureID]; ok && len(f.Subtasks) > 0 {
						frontier, err := batch.SubtaskFrontier(f.Subtasks)
						if err != nil {
							return fmt.Errorf("feature %s subtask dependency error: %w", s.FeatureID, err)
						}
						result.SubtaskFrontier = frontier
					}
					if result.SubtaskFrontier == nil {
						result.SubtaskFrontier = []string{}
					}

					out, err := json.MarshalIndent(result, "", "  ")
					if err != nil {
						return err
					}
					fmt.Println(string(out))
					return nil
				}
			}

			if jsonOutput {
				fmt.Println("null")
			} else {
				fmt.Println("No eligible features (all done or blocked by dependencies).")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format with subtask frontier")
	return cmd
}

// batchRunConfig 收納 batch run 子指令的設定與依賴，供 executeBatchFeature / mergeBatchFeature
// 作為 receiver 使用，取代原本在 RunE closure 內捕獲多個外部變數的 inline closure。
type batchRunConfig struct {
	ws           *protocol.Workspace
	cfg          protocol.Config
	runnerName   string
	manualRunner string
	timeout      int
}

// executeBatchFeature 執行單一 feature 的完整 runLoop（含 worktree 隔離），回傳最新 status。
func (bc *batchRunConfig) executeBatchFeature(next string, feature feat.Feature, s protocol.State) (feat.Status, error) {
	signal.Ignore(syscall.SIGPIPE)
	batchCtx, batchCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer batchCancel()
	batchOps := gitops.New(bc.ws.Root, bc.ws, bc.cfg)

	batchRunnerWs := bc.ws
	commitStrategy := "never"
	if bc.cfg.Isolation == "worktree" {
		wtPath, wtErr := batchOps.SetupWorktree(next, feature.Repos)
		if wtErr != nil {
			return feat.StatusBlocked, fmt.Errorf("worktree setup failed: %w", wtErr)
		}
		batchRunnerWs = &protocol.Workspace{Root: wtPath}
		commitStrategy = "per-round"
	}

	runnerFactory := runner.NewFactory(batchRunnerWs, bc.cfg, bc.timeout)
	runErr := runLoop(batchCtx, bc.ws, batchRunnerWs, feature, bc.cfg, s, batchOps, runnerFactory, commitStrategy, bc.manualRunner, nil)

	updated, _ := bc.ws.LoadFeature(next)
	return updated.Status, runErr
}

// mergeBatchFeature 對 ready-for-review 的 feature 走 done.go 的共用 helper，
// 回傳 MergeResult 供 handleAutoMerge 決定衝突暫停 / 錯誤續跑 / 成功標 done。
func (bc *batchRunConfig) mergeBatchFeature(featureID string) gitops.MergeResult {
	st, err := bc.ws.ReadState(featureID)
	if err != nil {
		return gitops.MergeResult{Error: fmt.Sprintf("cannot read state for %s: %v", featureID, err)}
	}
	if guard.SelfModNeedsApproval(st, false) {
		slog.Warn("self-mod guard: protected paths touched, auto-merge blocked — use 4x done --approve-self-mod",
			"feature", featureID, "paths", st.SelfModPaths)
		return gitops.MergeResult{Error: "self-mod: protected paths require manual --approve-self-mod"}
	}
	f, _ := bc.ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	return autoMergeFeature(bc.ws, bc.cfg, featureID, name)
}

func newBatchRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var noAutoMerge bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run eligible features in dependency order",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if err := ws.WriteBatchPID(os.Getpid()); err != nil {
				slog.Warn("failed to write batch PID", "error", err)
			}
			defer func() { _ = ws.ClearBatchPID() }()

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

			manualRunner := runnerName
			if runnerName == "" {
				runnerName = cfg.Default
			}
			if _, ok := cfg.Runners[runnerName]; !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []feat.Feature
			for _, f := range features {
				if !batchCompleted(f.Status) {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features.")
				return nil
			}

			plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), 4)
			if err != nil {
				return err
			}

			if planData, je := json.MarshalIndent(plan, "", "  "); je == nil {
				if we := os.WriteFile(filepath.Join(ws.DotDir(), "batch-plan.json"), planData, 0o644); we != nil {
					slog.Warn("failed to write batch plan", "error", we)
				}
			}

			statusMap := make(map[string]feat.Status)
			for _, f := range features {
				statusMap[f.ID] = f.Status
			}

			progress := batch.NewProgress(statusMap, protocol.BatchOutcomeCompleted)

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			doneCh := make(chan struct{})
			defer func() {
				signal.Stop(sigCh)
				close(doneCh)
			}()
			go func() {
				select {
				case <-doneCh:
					return
				case <-sigCh:
					batch.FinishReport(ws, plan, runnerName, progress, protocol.BatchOutcomeInterrupted, "")
					os.Exit(130)
				}
			}()

			defer func() {
				if r := recover(); r != nil {
					batch.FinishReport(ws, plan, runnerName, progress, protocol.BatchOutcomeCrashed, fmt.Sprint(r))
					slog.Error("batch panic", "error", r, "stack", string(debug.Stack()))
					panic(r)
				}
			}()

			bc := &batchRunConfig{
				ws:           ws,
				cfg:          cfg,
				runnerName:   runnerName,
				manualRunner: manualRunner,
				timeout:      timeout,
			}

			slog.Info("batch operation", "action", "run", "features", len(plan.Schedule), "runner", runnerName)
			completed := batch.RunSchedule(ws, plan, statusMap, maxRounds, runnerName, bc.executeBatchFeature, noAutoMerge, bc.mergeBatchFeature, progress)

			batch.FinishReport(ws, plan, runnerName, progress, "", "")

			slog.Info("batch operation", "action", "complete", "completed", completed, "total", len(plan.Schedule))
			fmt.Printf("\n══════════════════════════════════════\n")
			fmt.Printf("  BATCH COMPLETE: %d/%d features done\n", completed, len(plan.Schedule))
			fmt.Printf("══════════════════════════════════════\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max rounds per feature (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&noAutoMerge, "no-auto-merge", false, "feature 完成後停在 pending-review，不自動 merge 回 main")
	return cmd
}

func newBatchStopCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Signal batch to stop after current feature completes",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if err := ws.WriteBatchStop(); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "stop")
			if jsonOutput {
				return printJSON(struct {
					Stopped bool `json:"stopped"`
				}{true})
			}
			fmt.Println("Stop signal sent — batch will finish current feature then exit.")
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func batchCompleted(s feat.Status) bool {
	return feat.BatchCompleted(s)
}
