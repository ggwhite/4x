package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var pending bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status [feature-id]",
		Short: "Show feature status",
		Args:  cobra.MaximumNArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				featureID, err := ws.ResolveFeatureID(args[0])
				if err != nil {
					return err
				}
				if jsonOutput {
					return showFeatureDetailJSON(ws, featureID)
				}
				return showFeatureDetail(ws, featureID)
			}
			if jsonOutput {
				return showAllFeaturesJSON(ws)
			}
			return showAllFeatures(ws, pending)
		}),
	}

	cmd.Flags().BoolVar(&pending, "pending", false, "show only non-done features")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

type featureRow struct {
	feature   feature.Feature
	phase     string
	round     string
	active    bool
	category  int // 0=running, 1=review, 2=pending, 3=todo, 4=done
	hasSpec   bool
	hasPlan   bool
	updatedAt time.Time
}

func categorize(f feature.Feature, active bool) int {
	if f.Status == feature.StatusInProgress && active {
		return 0 // running
	}
	if f.Status == feature.StatusReadyForReview {
		return 1 // review
	}
	if f.Status == feature.StatusInProgress || f.Status == feature.StatusNeedsAttention {
		return 2 // pending (in-progress but not actively running)
	}
	if f.Status == feature.StatusDone || f.Status == feature.StatusAbandoned {
		return 4
	}
	return 3 // not-started = todo
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

	var designDocDirs []string
	if cfg, err := ws.LoadMergedConfig(); err == nil {
		designDocDirs = cfg.DesignDocDirs
	}

	var rows []featureRow
	for _, f := range features {
		phase := "-"
		round := "-"
		active := false
		var updatedAt time.Time
		s, err := ws.ReadState(f.ID)
		if err == nil {
			_ = ws.ReconcileActive(f.ID, &s)
			phase = string(s.Phase)
			round = fmt.Sprintf("%d/%d", s.Round, s.MaxRounds)
			active = s.Active
			updatedAt = s.UpdatedAt
		}
		hasSpec := protocol.ResolveDesignDoc(ws.Root, f, "spec", designDocDirs...).Source != ""
		hasPlan := protocol.ResolveDesignDoc(ws.Root, f, "plan", designDocDirs...).Source != ""
		rows = append(rows, featureRow{
			feature:   f,
			phase:     phase,
			round:     round,
			active:    active,
			category:  categorize(f, active),
			hasSpec:   hasSpec,
			hasPlan:   hasPlan,
			updatedAt: updatedAt,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].category != rows[j].category {
			return rows[i].category < rows[j].category
		}
		if rows[i].category == 4 {
			return rows[j].updatedAt.Before(rows[i].updatedAt)
		}
		pi, pj := rows[i].feature.Priority, rows[j].feature.Priority
		piSet, pjSet := pi != nil, pj != nil
		if piSet != pjSet {
			return piSet
		}
		if piSet && pjSet && *pi != *pj {
			return *pi < *pj
		}
		return rows[i].feature.ID < rows[j].feature.ID
	})

	counts := map[int]int{}
	for _, r := range rows {
		counts[r.category]++
	}
	fmt.Printf("Total: %d features — %d running, %d review, %d pending, %d todo, %d done\n\n",
		len(features), counts[0], counts[1], counts[2], counts[3], counts[4])

	categoryLabels := []struct {
		cat   int
		label string
	}{
		{0, "Running"},
		{1, "Review"},
		{2, "Pending"},
		{3, "Todo"},
		{4, "Done"},
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
		if pendingOnly && cl.cat == 4 {
			continue
		}

		truncated := 0
		if cl.cat == 4 && len(group) > maxDone {
			truncated = len(group) - maxDone
			group = group[:maxDone]
		}

		fmt.Printf("── %s (%d) ──\n", cl.label, counts[cl.cat])
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "  PRI\tID\tNAME\tDOCS\tPHASE\tROUND\n")
		fmt.Fprintf(w, "  ───\t──\t────\t────\t─────\t─────\n")
		for _, r := range group {
			pri := "-"
			if r.feature.Priority != nil {
				pri = fmt.Sprintf("P%d", *r.feature.Priority)
			}
			docs := docsLabel(r.hasSpec, r.hasPlan)
			name := r.feature.Name
			if len(r.feature.Depends) > 0 {
				name += " (→ " + strings.Join(r.feature.Depends, ", ") + ")"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n", pri, r.feature.ID, name, docs, r.phase, r.round)
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

func docsLabel(hasSpec, hasPlan bool) string {
	switch {
	case hasSpec && hasPlan:
		return "S+P"
	case hasSpec:
		return "S"
	case hasPlan:
		return "P"
	default:
		return "-"
	}
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

	if f.Priority != nil {
		fmt.Printf("Pri:     P%d\n", *f.Priority)
	}

	if len(f.Depends) > 0 {
		fmt.Printf("Depends: %s\n", strings.Join(f.Depends, ", "))
	}

	if len(f.Repos) > 0 {
		fmt.Println("Repos:")
		for _, repo := range f.Repos {
			fmt.Printf("  - %s\n", repo)
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
			dep := ""
			if len(st.Depends) > 0 {
				dep = fmt.Sprintf(" (→ %s)", strings.Join(st.Depends, ", "))
			}
			fmt.Printf("  %s %s: %s%s\n", icon, st.ID, st.Name, dep)
		}
	}

	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return err
	}
	screenshotDir := protocol.ScreenshotDir(cfg)
	groups, err := ws.DiscoverScreenshots(id, screenshotDir)
	if err != nil {
		return err
	}
	if len(groups) > 0 {
		total := 0
		parts := make([]string, 0, len(groups))
		for _, group := range groups {
			n := len(group.Screenshots)
			if n == 0 {
				continue
			}
			total += n
			parts = append(parts, fmt.Sprintf("round %d: %d", group.Round, n))
		}
		if total > 0 {
			fmt.Printf("\nScreenshots: %d (%s)\n", total, strings.Join(parts, ", "))
		}
	}

	printCodexUsage(latestCodexUsageByRound(ws, id))

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

type statusListJSON struct {
	Features []statusItemJSON `json:"features"`
}

type statusItemJSON struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Priority  *int     `json:"priority,omitempty"`
	Profile   string   `json:"profile,omitempty"`
	Depends   []string `json:"depends,omitempty"`
	Phase     string   `json:"phase,omitempty"`
	Role      string   `json:"role,omitempty"`
	Round     int      `json:"round,omitempty"`
	MaxRounds int      `json:"maxRounds,omitempty"`
	Active    bool     `json:"active"`
	Runner    string   `json:"runner,omitempty"`
	Pid       int      `json:"pid,omitempty"`
}

func showAllFeaturesJSON(ws *protocol.Workspace) error {
	features, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	items := make([]statusItemJSON, 0, len(features))
	for _, f := range features {
		item := statusItemJSON{
			ID:       f.ID,
			Name:     f.Name,
			Status:   string(f.Status),
			Priority: f.Priority,
			Profile:  f.Profile,
			Depends:  f.Depends,
		}
		if s, err := ws.ReadState(f.ID); err == nil {
			item.Phase = string(s.Phase)
			item.Role = string(s.Role)
			item.Round = s.Round
			item.MaxRounds = s.MaxRounds
			item.Active = s.Active
			item.Runner = s.Runner
			item.Pid = s.Pid
		}
		items = append(items, item)
	}
	data, _ := json.MarshalIndent(statusListJSON{Features: items}, "", "  ")
	fmt.Println(string(data))
	return nil
}

type featureDetailJSON struct {
	Feature feature.Feature `json:"feature"`
	State   *protocol.State `json:"state"`
}

func showFeatureDetailJSON(ws *protocol.Workspace, id string) error {
	f, err := ws.LoadFeature(id)
	if err != nil {
		return fmt.Errorf("feature %q not found: %w", id, err)
	}
	result := featureDetailJSON{Feature: f}
	if s, err := ws.ReadState(id); err == nil {
		result.State = &s
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	return nil
}
