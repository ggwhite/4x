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

			// ManualSourceFeature 豁免 MaxPerFeatureCategory 桶上限，skipped 在此路徑恆為 0。
			added, _ := store.Harvest(learning.ManualSourceFeature, "user", []learning.RetroLearning{
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
	var ineffectiveReset bool
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

			resetSet := make(map[string]bool, len(store.IneffectiveResetIDs))
			for _, id := range store.IneffectiveResetIDs {
				resetSet[id] = true
			}

			showDefault := statusFilter == "" && !ineffective && !ineffectiveReset
			var filtered []learning.Entry
			for _, e := range store.Entries {
				if category != "" && string(e.Category) != category {
					continue
				}
				if ineffective && !e.Ineffective {
					continue
				}
				if ineffectiveReset && !resetSet[e.ID] {
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
	cmd.Flags().BoolVar(&ineffectiveReset, "ineffective-reset", false, "only show entries whose ineffective flag was reset by the v2 migration")
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

			// 先 demote 久未命中的 active 回 candidate（交由 F147 candidate 老化後續處理）。
			preActive := make(map[string]bool)
			for _, e := range store.Entries {
				if e.Status == learning.StatusActive {
					preActive[e.ID] = true
				}
			}
			store.DemoteInactiveActive(resolved.ActiveDemoteDays)
			demotedIDs := []string{}
			demotedSet := make(map[string]bool)
			for _, e := range store.Entries {
				if preActive[e.ID] && e.Status == learning.StatusCandidate {
					demotedIDs = append(demotedIDs, e.ID)
					demotedSet[e.ID] = true
				}
			}

			// 再老化既有從未使用的 candidate 為 stale；還原本輪剛 demote 的 entry，
			// 避免「新 demote 的 active」被 candidate 老化直接標 stale 而遭刪除（AC-6/AC-7）。
			store.MarkCandidatesStale(resolved.CandidateMaxIdleDays)
			for i := range store.Entries {
				if demotedSet[store.Entries[i].ID] && store.Entries[i].Status == learning.StatusStale {
					store.Entries[i].Status = learning.StatusCandidate
				}
			}

			staleIDs := []string{}
			for _, e := range store.Entries {
				if e.Status == learning.StatusStale {
					staleIDs = append(staleIDs, e.ID)
				}
			}

			shouldPrune := !dryRun && (len(staleIDs) > 0 || len(demotedIDs) > 0)

			if jsonOutput {
				removed := 0
				if shouldPrune {
					removed = store.Prune()
					if err := store.Save(storePath); err != nil {
						return err
					}
				}
				return printJSON(struct {
					Removed    int      `json:"removed"`
					Demoted    int      `json:"demoted"`
					DryRun     bool     `json:"dryRun"`
					StaleIDs   []string `json:"staleIds"`
					DemotedIDs []string `json:"demotedIds"`
				}{removed, len(demotedIDs), dryRun, staleIDs, demotedIDs})
			}

			if len(demotedIDs) == 0 && len(staleIDs) == 0 {
				fmt.Println("No inactive active or stale learnings found.")
				return nil
			}

			if dryRun {
				if len(demotedIDs) > 0 {
					fmt.Printf("%d active entries would be demoted to candidate (not deleted):\n", len(demotedIDs))
					printLearningEntries(store, demotedSet)
				}
				if len(staleIDs) > 0 {
					fmt.Printf("%d stale entries would be removed:\n", len(staleIDs))
					printLearningsByStatus(store, learning.StatusStale)
				}
				return nil
			}

			// 非 dry-run 也要印出明細：必須在 store.Prune() 之前呼叫，
			// 否則 stale entries 已被移出 store.Entries，印不出內容。
			if len(demotedIDs) > 0 {
				printLearningEntries(store, demotedSet)
				fmt.Printf("Demoted %d inactive active entries to candidate.\n", len(demotedIDs))
			}
			if len(staleIDs) > 0 {
				printLearningsByStatus(store, learning.StatusStale)
			}

			removed := 0
			if shouldPrune {
				removed = store.Prune()
				if err := store.Save(storePath); err != nil {
					return err
				}
			}
			fmt.Printf("Removed %d stale entries.\n", removed)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without removing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// printLearningEntries 列印 store 中 ID 落在 idSet 的條目（縮排格式，供 dry-run 預覽）。
func printLearningEntries(store learning.Store, idSet map[string]bool) {
	for _, e := range store.Entries {
		if idSet[e.ID] {
			fmt.Printf("  %s (%s) %s\n", e.ID, e.Category, e.Content)
		}
	}
}

// printLearningsByStatus 列印 store 中指定 status 的條目（縮排格式，供 dry-run 預覽）。
func printLearningsByStatus(store learning.Store, status learning.Status) {
	for _, e := range store.Entries {
		if e.Status == status {
			fmt.Printf("  %s (%s) %s\n", e.ID, e.Category, e.Content)
		}
	}
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
