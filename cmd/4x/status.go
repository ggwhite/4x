package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var pending bool

	cmd := &cobra.Command{
		Use:   "status [feature-id]",
		Short: "Show feature status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				return showFeatureDetail(ws, args[0])
			}
			return showAllFeatures(ws, pending)
		},
	}

	cmd.Flags().BoolVar(&pending, "pending", false, "show only non-done features")
	return cmd
}

func showAllFeatures(ws *protocol.Workspace, pendingOnly bool) error {
	features, err := ws.ListFeatures()
	if err != nil {
		return err
	}

	if len(features) == 0 {
		fmt.Println("No features found. Create one with: 4x new \"feature name\"")
		return nil
	}

	var done, inProgress, notStarted int
	for _, f := range features {
		switch f.Status {
		case "done":
			done++
		case "in-progress":
			inProgress++
		default:
			notStarted++
		}
	}
	fmt.Printf("Total: %d features — %d done, %d in-progress, %d pending\n\n",
		len(features), done, inProgress, notStarted)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tPHASE\tROUND")
	fmt.Fprintln(w, "──\t────\t──────\t─────\t─────")

	for _, f := range features {
		if pendingOnly && f.Status == "done" {
			continue
		}
		phase := "-"
		round := "-"
		s, err := ws.ReadState(f.ID)
		if err == nil {
			phase = string(s.Phase)
			round = fmt.Sprintf("%d/%d", s.Round, s.MaxRounds)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", f.ID, f.Name, f.Status, phase, round)
	}
	return w.Flush()
}

func showFeatureDetail(ws *protocol.Workspace, id string) error {
	f, err := ws.LoadFeature(id)
	if err != nil {
		return fmt.Errorf("feature %q not found: %w", id, err)
	}

	fmt.Printf("Feature: %s\n", f.ID)
	fmt.Printf("Name:    %s\n", f.Name)
	fmt.Printf("Status:  %s\n", f.Status)

	if f.Description != "" && f.Description != f.Name {
		fmt.Printf("Desc:    %s\n", f.Description)
	}

	if len(f.Repos) > 0 {
		fmt.Println("Repos:")
		for repo, desc := range f.Repos {
			if desc != "" {
				fmt.Printf("  - %s: %s\n", repo, desc)
			} else {
				fmt.Printf("  - %s\n", repo)
			}
		}
	}

	state, err := ws.ReadState(id)
	if err == nil {
		fmt.Printf("\nPhase:   %s\n", state.Phase)
		fmt.Printf("Role:    %s\n", state.Role)
		fmt.Printf("Round:   %d/%d\n", state.Round, state.MaxRounds)
		fmt.Printf("Active:  %v\n", state.Active)
		if state.Runner != "" {
			fmt.Printf("Runner:  %s\n", state.Runner)
		}
	}

	if len(f.Subtasks) > 0 {
		fmt.Println("\nSubtasks:")
		for _, st := range f.Subtasks {
			icon := "⏳"
			switch st.Status {
			case "done":
				icon = "✅"
			case "in-progress":
				icon = "🔄"
			case "blocked":
				icon = "🚫"
			}
			fmt.Printf("  %s %s: %s\n", icon, st.ID, st.Name)
		}
	}

	return nil
}
