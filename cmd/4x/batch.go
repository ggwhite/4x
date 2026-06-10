package main

import (
	"fmt"
	"os"

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
	return &cobra.Command{
		Use:   "plan",
		Short: "Plan batch execution (Union-Find grouping)",
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
				if f.Status == "not-started" {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features to batch.")
				return nil
			}

			plan := batch.PlanBatch(pending, cfg.HubRepos)

			if len(plan.Bridges) > 0 {
				fmt.Println("Phase 0 — Bridge Wave (run first, merge before continuing):")
				for _, g := range plan.Bridges {
					for _, f := range g.Features {
						fmt.Printf("  %s [%s] (bridge)\n", f.ID, f.Name)
					}
				}
				fmt.Println()
			}

			fmt.Println("Phase 1 — Normal Batch:")
			for _, g := range plan.Groups {
				if len(g.Features) == 1 {
					f := g.Features[0]
					fmt.Printf("  Group %s (independent): %s [%s]\n", g.ID, f.ID, f.Name)
				} else {
					fmt.Printf("  Group %s (chain):\n", g.ID)
					for i, f := range g.Features {
						arrow := "→"
						if i == len(g.Features)-1 {
							arrow = " "
						}
						fmt.Printf("    %s %s [%s]\n", f.ID, arrow, f.Name)
					}
				}
			}

			return nil
		},
	}
}

func newBatchNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Show the next feature to run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			for _, f := range features {
				if f.Status == "not-started" {
					fmt.Println(f.ID)
					return nil
				}
			}

			fmt.Println("No pending features.")
			return nil
		},
	}
}
