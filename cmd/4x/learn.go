package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

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
		newLearnListCmd(),
		newLearnPruneCmd(),
		newLearnPromoteCmd(),
		newLearnRemoveCmd(),
		newLearnContextCmd(),
	)
	return cmd
}

func newLearnListCmd() *cobra.Command {
	var category string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all learnings",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			if jsonOutput {
				entries := []learning.Entry{}
				for _, e := range store.Entries {
					if category != "" && string(e.Category) != category {
						continue
					}
					entries = append(entries, e)
				}
				return printJSON(struct {
					Entries []learning.Entry `json:"entries"`
					Active  int              `json:"active"`
					Total   int              `json:"total"`
				}{entries, len(store.ActiveEntries()), len(store.Entries)})
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCATEGORY\tSTATUS\tUSED\tCONTENT")
			for _, e := range store.Entries {
				if category != "" && string(e.Category) != category {
					continue
				}
				content := e.Content
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					e.ID, e.Category, e.Status, e.UsedCount, content)
			}
			w.Flush()

			active := len(store.ActiveEntries())
			fmt.Printf("\n%d entries (%d active)\n", len(store.Entries), active)
			return nil
		}),
	}
	cmd.Flags().StringVar(&category, "category", "", "filter by category")
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
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			store.MarkStale(learning.DefaultStaleDays)

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

// findLearningsPath 從 cwd 往上找 .4x/，回傳 learnings.json 的完整路徑。
func findLearningsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return "", fmt.Errorf("not in a 4x project: %w", err)
	}
	return filepath.Join(ws.DotDir(), protocol.LearningsFile), nil
}
