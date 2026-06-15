package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <feature-id>",
		Short: "Complete merge after resolving conflicts",
		Long:  "Use after '4x done' reported a merge conflict and you resolved it in the worktree.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			s, err := ws.ReadState(featureID)
			if err != nil {
				return fmt.Errorf("cannot read state for %s: %w", featureID, err)
			}
			if s.Phase != protocol.PhasePendingReview && s.Phase != protocol.PhaseDone {
				return fmt.Errorf("feature %s is in phase %q, not pending-review or done (run '4x done %s' first)", featureID, s.Phase, featureID)
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				slog.Warn("cannot read config, using defaults", "error", err)
			}

			ops := gitops.New(ws.Root, ws, cfg)

			wtDir := gitops.Dir(ws.Root, featureID)
			if _, err := os.Stat(wtDir); err != nil {
				return fmt.Errorf("no worktree found at %s", wtDir)
			}

			f, _ := ws.LoadFeature(featureID)
			name := featureID
			if f.Name != "" {
				name = f.Name
			}

			msg := fmt.Sprintf("fix(%s): resolve merge conflicts — %s", featureID, name)
			if err := ops.Commit(wtDir, featureID, msg); err != nil {
				return fmt.Errorf("commit in worktree failed: %w", err)
			}

			result := ops.Merge(featureID, name)
			if result.Conflict {
				fmt.Println("Merge still has conflicts:")
				for _, file := range result.Files {
					fmt.Printf("  conflict: %s\n", file)
				}
				if result.ConflictRepo != "" {
					fmt.Printf("  repo: %s\n", result.ConflictRepo)
				}
				return nil
			}
			if result.Error != "" {
				return fmt.Errorf("merge failed: %s", result.Error)
			}

			if s.Phase == protocol.PhasePendingReview {
				if err := finalizeDone(ws, featureID, s); err != nil {
					return err
				}
				fmt.Printf("Feature %s marked as done.\n", featureID)
			}

			fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
			return nil
		},
	}
}
