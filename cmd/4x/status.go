package main

import (
	"fmt"
	"os"
	"sort"
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

type featureRow struct {
	feature  protocol.Feature
	phase    string
	round    string
	active   bool
	category int // 0=running, 1=pending, 2=todo, 3=done
}

func categorize(f protocol.Feature, active bool) int {
	if f.Status == "in-progress" && active {
		return 0 // running
	}
	if f.Status == "in-progress" {
		return 1 // pending (in-progress but not actively running)
	}
	if f.Status == "done" {
		return 3
	}
	return 2 // not-started = todo
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

	var rows []featureRow
	for _, f := range features {
		phase := "-"
		round := "-"
		active := false
		s, err := ws.ReadState(f.ID)
		if err == nil {
			phase = string(s.Phase)
			round = fmt.Sprintf("%d/%d", s.Round, s.MaxRounds)
			active = s.Active
		}
		rows = append(rows, featureRow{
			feature:  f,
			phase:    phase,
			round:    round,
			active:   active,
			category: categorize(f, active),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].category != rows[j].category {
			return rows[i].category < rows[j].category
		}
		pi, pj := rows[i].feature.Priority, rows[j].feature.Priority
		if pi != pj {
			return pi > pj
		}
		return rows[i].feature.ID < rows[j].feature.ID
	})

	counts := map[int]int{}
	for _, r := range rows {
		counts[r.category]++
	}
	fmt.Printf("Total: %d features — %d running, %d pending, %d todo, %d done\n\n",
		len(features), counts[0], counts[1], counts[2], counts[3])

	categoryLabels := []struct {
		cat   int
		label string
	}{
		{0, "Running"},
		{1, "Pending"},
		{2, "Todo"},
		{3, "Done"},
	}

	const maxDone = 5

	for _, cl := range categoryLabels {
		var group []featureRow
		for _, r := range rows {
			if r.category == cl.cat {
				group = append(group, r)
			}
		}
		if len(group) == 0 {
			continue
		}
		if pendingOnly && cl.cat == 3 {
			continue
		}

		truncated := 0
		if cl.cat == 3 && len(group) > maxDone {
			truncated = len(group) - maxDone
			group = group[:maxDone]
		}

		fmt.Printf("── %s (%d) ──\n", cl.label, counts[cl.cat])
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "  PRI\tID\tNAME\tPHASE\tROUND\n")
		fmt.Fprintf(w, "  ───\t──\t────\t─────\t─────\n")
		for _, r := range group {
			pri := "-"
			if r.feature.Priority > 0 {
				pri = fmt.Sprintf("P%d", r.feature.Priority)
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", pri, r.feature.ID, r.feature.Name, r.phase, r.round)
		}
		w.Flush()
		if truncated > 0 {
			fmt.Printf("  ... and %d more\n", truncated)
		}
		fmt.Println()
	}

	printBacklogWarnings(ws, "")
	return nil
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

	printBacklogWarnings(ws, id)
	return nil
}

func printBacklogWarnings(ws *protocol.Workspace, featureID string) {
	drift, err := ws.CompareBacklogMirror()
	if err != nil {
		fmt.Printf("\nWARN: cannot compare %s: %v\n", protocol.BacklogFile, err)
		return
	}
	for _, d := range drift {
		if featureID == "" || d.FeatureID == featureID {
			fmt.Printf("\nWARN: %s\n", d.Message)
		}
	}
}
