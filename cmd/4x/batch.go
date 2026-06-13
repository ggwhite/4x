package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/gitops"
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

			cfg, _ := ws.ReadConfig()
			if userCfg, err := protocol.ReadUserConfig(); err == nil {
				cfg = protocol.MergeConfig(userCfg, cfg)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []protocol.Feature
			for _, f := range features {
				if f.Status != protocol.StatusDone {
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
	return &cobra.Command{
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
			statusMap := make(map[string]protocol.Status)
			featureMap := make(map[string]protocol.Feature)
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

			fmt.Println("No eligible features (all done or blocked by dependencies).")
			return nil
		},
	}
}

func newBatchRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int

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

			cfg, err := ws.ReadConfig()
			if err != nil {
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
			runnerCfg, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []protocol.Feature
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

			statusMap := make(map[string]protocol.Status)
			for _, f := range features {
				statusMap[f.ID] = f.Status
			}

			stopFile := filepath.Join(ws.DotDir(), "batch-stop")

			completed := 0
			for {
				if _, err := os.Stat(stopFile); err == nil {
					os.Remove(stopFile)
					fmt.Printf("\n⏸ batch-stop detected — stopping gracefully (%d done)\n", completed)
					break
				}

				next := ""
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
						next = s.FeatureID
						break
					}
				}

				if next == "" {
					break
				}

				fmt.Printf("\n══════════════════════════════════════\n")
				fmt.Printf("  BATCH: %s (%d/%d done)\n", next, completed, len(plan.Schedule))
				fmt.Printf("══════════════════════════════════════\n\n")

				feature, err := ws.LoadFeature(next)
				if err != nil {
					fmt.Printf("  error loading feature: %v\n", err)
					statusMap[next] = protocol.StatusBlocked
					continue
				}

				if err := ws.InitFeatureDir(next); err != nil {
					return err
				}

				rounds := maxRounds
				if rounds <= 0 {
					rounds = 5
				}

				s := protocol.State{
					FeatureID: next,
					Phase:     protocol.PhaseInit,
					MaxRounds: rounds,
					Active:    true,
					Runner:    runnerName,
					CreatedAt: time.Now(),
				}
				if existing, err := ws.ReadState(next); err == nil {
					s = existing
					s.Active = true
				}
				_ = ws.WriteState(next, s)

				signal.Ignore(syscall.SIGPIPE)
				batchCtx, batchCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				batchOps := gitops.New(ws.Root, ws, cfg)

				batchRunnerWs := ws
				commitStrategy := "never"
				if cfg.Isolation == "worktree" {
					wtPath, wtErr := batchOps.SetupWorktree(next)
					if wtErr != nil {
						fmt.Printf("  worktree setup failed: %v\n", wtErr)
						statusMap[next] = protocol.StatusBlocked
						batchCancel()
						continue
					}
					batchRunnerWs = &protocol.Workspace{Root: wtPath}
					commitStrategy = "per-round"
				}

				runnerFactory := func(logPath string, model string) runner.Runner {
					return runner.NewRunner(batchRunnerWs, runnerName, runnerCfg, time.Duration(timeout)*time.Second, logPath, model)
				}
				err = runLoop(batchCtx, ws, batchRunnerWs, feature, cfg, s, batchOps, runnerFactory, commitStrategy)
				batchCancel()

				updated, _ := ws.LoadFeature(next)
				statusMap[next] = updated.Status

				if err != nil {
					fmt.Printf("  feature %s failed: %v\n", next, err)
				}

				if batchCompleted(updated.Status) {
					completed++
				}

				}

			fmt.Printf("\n══════════════════════════════════════\n")
			fmt.Printf("  BATCH COMPLETE: %d/%d features done\n", completed, len(plan.Schedule))
			fmt.Printf("══════════════════════════════════════\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max rounds per feature (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	return cmd
}

func newBatchStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Signal batch to stop after current feature completes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			stopFile := filepath.Join(ws.DotDir(), "batch-stop")
			if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
				return err
			}
			fmt.Println("Stop signal sent — batch will finish current feature then exit.")
			return nil
		},
	}
}

func batchCompleted(s protocol.Status) bool {
	return s == protocol.StatusDone || s == protocol.StatusReadyForReview
}
