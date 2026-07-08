package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/ggwhite/4x/internal/evolution"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// newLearnCmd 組裝 `4x learn` 命令樹，用於管理跨 feature 累積的 retro learnings。
func newLearnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learn",
		Short: "Manage retro learnings",
	}
	cmd.AddCommand(
		newLearnAddCmd(),
		newLearnListCmd(),
		newLearnPruneCmd(),
		newLearnPromoteCmd(),
		newLearnRemoveCmd(),
		newLearnContextCmd(),
	)
	return cmd
}

// newLearnAddCmd 建立 `4x learn add` 子命令，讓 standalone session 直接寫入 learning。
func newLearnAddCmd() *cobra.Command {
	var category string
	var content string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new learning entry",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			if content == "" {
				return fmt.Errorf("--content is required")
			}
			cat := learning.Category(category)
			if !learning.IsValidCategory(cat) {
				valid := learning.ValidCategories()
				names := make([]string, len(valid))
				for i, c := range valid {
					names[i] = string(c)
				}
				return fmt.Errorf("invalid category %q, valid categories: %s", category, strings.Join(names, ", "))
			}

			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			if existing := store.FindSimilar(content); existing != nil {
				if jsonOutput {
					return printJSON(struct {
						Error string `json:"error"`
						ID    string `json:"id"`
						Added bool   `json:"added"`
					}{
						Error: fmt.Sprintf("similar learning already exists: %s", existing.ID),
						ID:    existing.ID,
						Added: false,
					})
				}
				return fmt.Errorf("similar learning already exists: %s", existing.ID)
			}

			added := store.Harvest("manual", "user", []learning.RetroLearning{
				{Category: cat, Content: content},
			})
			if added == 0 {
				return fmt.Errorf("failed to add learning")
			}
			if err := store.Save(storePath); err != nil {
				return err
			}

			newEntry := store.Entries[len(store.Entries)-1]
			if jsonOutput {
				return printJSON(struct {
					ID    string `json:"id"`
					Added bool   `json:"added"`
				}{newEntry.ID, true})
			}
			fmt.Printf("Added %s\n", newEntry.ID)
			return nil
		}),
	}
	cmd.Flags().StringVar(&category, "category", "", "learning category (required)")
	cmd.Flags().StringVar(&content, "content", "", "learning content (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	_ = cmd.MarkFlagRequired("category")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func newLearnListCmd() *cobra.Command {
	var category string
	var statusFilter string
	var ineffective bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List learnings (default: active + candidate)",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			showDefault := statusFilter == "" && !ineffective
			var filtered []learning.Entry
			for _, e := range store.Entries {
				if category != "" && string(e.Category) != category {
					continue
				}
				if ineffective && !e.Ineffective {
					continue
				}
				if statusFilter != "" && string(e.Status) != statusFilter {
					continue
				}
				if showDefault {
					if e.Status != learning.StatusActive && e.Status != learning.StatusCandidate {
						continue
					}
				}
				filtered = append(filtered, e)
			}

			if jsonOutput {
				if filtered == nil {
					filtered = []learning.Entry{}
				}
				return printJSON(struct {
					Entries []learning.Entry `json:"entries"`
					Active  int              `json:"active"`
					Total   int              `json:"total"`
				}{filtered, len(store.ActiveEntries()), len(store.Entries)})
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCATEGORY\tSTATUS\tUSED\tCONTENT")
			for _, e := range filtered {
				content := e.Content
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				id := e.ID
				if e.Status == learning.StatusCandidate {
					id += "*"
				}
				status := string(e.Status)
				if e.Ineffective && e.Status == learning.StatusActive {
					status = "active!"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					id, e.Category, status, e.UsedCount, content)
			}
			w.Flush()

			active := len(store.ActiveEntries())
			fmt.Printf("\n%d entries (%d active)\n", len(store.Entries), active)
			return nil
		}),
	}
	cmd.Flags().StringVar(&category, "category", "", "filter by category")
	cmd.Flags().StringVar(&statusFilter, "status", "", "filter by status (active, candidate, stale, promoted)")
	cmd.Flags().BoolVar(&ineffective, "ineffective", false, "only show ineffective entries")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newLearnPruneCmd() *cobra.Command {
	var dryRun bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove all stale learnings",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			ws, err := findWorkspace()
			if err != nil {
				return err
			}
			storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}
			resolved := evolution.ResolveEvolution(cfg)

			store.MarkStale(learning.DefaultStaleDays)
			store.MarkCandidatesStale(resolved.CandidateMaxIdleDays)

			staleIDs := []string{}
			for _, e := range store.Entries {
				if e.Status == learning.StatusStale {
					staleIDs = append(staleIDs, e.ID)
					if !jsonOutput {
						fmt.Printf("  %s (%s) %s\n", e.ID, e.Category, e.Content)
					}
				}
			}

			removed := 0
			if !dryRun && len(staleIDs) > 0 {
				removed = store.Prune()
				if err := store.Save(storePath); err != nil {
					return err
				}
			}

			if jsonOutput {
				return printJSON(struct {
					Removed  int      `json:"removed"`
					DryRun   bool     `json:"dryRun"`
					StaleIDs []string `json:"staleIds"`
				}{removed, dryRun, staleIDs})
			}

			if len(staleIDs) == 0 {
				fmt.Println("No stale learnings found.")
				return nil
			}
			if dryRun {
				fmt.Printf("\n%d stale entries would be removed (dry-run)\n", len(staleIDs))
				return nil
			}
			fmt.Printf("\nRemoved %d stale entries.\n", removed)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without removing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newLearnPromoteCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "Mark a learning as promoted (upgraded to template/instructions)",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}
			if err := store.Promote(args[0]); err != nil {
				return err
			}
			if err := store.Save(storePath); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(struct {
					ID       string `json:"id"`
					Promoted bool   `json:"promoted"`
				}{args[0], true})
			}
			fmt.Printf("Promoted %s\n", args[0])
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newLearnRemoveCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a learning entry",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			if err := store.Save(storePath); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(struct {
					ID      string `json:"id"`
					Removed bool   `json:"removed"`
				}{args[0], true})
			}
			fmt.Printf("Removed %s\n", args[0])
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newLearnContextCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Generate learnings context snapshot (.4x/learnings-context.md)",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return fmt.Errorf("not in a 4x project: %w", err)
			}

			if err := prompt.GenerateLearningsContext(ws); err != nil {
				return err
			}

			outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
			if jsonOutput {
				return printJSON(struct {
					Path string `json:"path"`
				}{outPath})
			}
			fmt.Printf("Written: %s\n", outPath)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// findWorkspace 從 cwd 往上找 .4x/，回傳對應的 workspace。
func findWorkspace() (*protocol.Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return nil, fmt.Errorf("not in a 4x project: %w", err)
	}
	return ws, nil
}

// findLearningsPath 從 cwd 往上找 .4x/，回傳 learnings.json 的完整路徑。
func findLearningsPath() (string, error) {
	ws, err := findWorkspace()
	if err != nil {
		return "", err
	}
	return filepath.Join(ws.DotDir(), protocol.LearningsFile), nil
}
