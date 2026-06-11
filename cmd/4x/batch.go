package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch operations for multiple features",
	}

	cmd.AddCommand(newBatchPlanCmd())
	cmd.AddCommand(newBatchNextCmd())
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

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []protocol.Feature
			for _, f := range features {
				if f.Status != "done" {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features to batch.")
				return nil
			}

			plan, err := batch.PlanBatch(pending, cfg.HubRepos, maxChain)
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
			statusMap := make(map[string]string)
			for _, f := range features {
				statusMap[f.ID] = f.Status
			}

			for _, s := range plan.Schedule {
				if statusMap[s.FeatureID] == "done" {
					continue
				}
				allDone := true
				for _, dep := range s.CanStartAfter {
					if statusMap[dep] != "done" {
						allDone = false
						break
					}
				}
				if allDone {
					fmt.Println(s.FeatureID)
					return nil
				}
			}

			fmt.Println("No eligible features (all done or blocked by dependencies).")
			return nil
		},
	}
}
