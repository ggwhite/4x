package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/ggwhite/4x/internal/worktree"
	"github.com/spf13/cobra"
)

func newDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <feature-id>",
		Short: "Mark a pending-review feature as done",
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

			return markDone(ws, featureID)
		},
	}
}

// markDone 將 pending-review 的 feature 推進到 done
func markDone(ws *protocol.Workspace, featureID string) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		return err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return err
	}

	syncFeatureStatus(ws, featureID, protocol.PhaseDone)

	ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})

	fmt.Printf("Feature %s marked as done.\n", featureID)

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	result := worktree.Merge(ws.Root, featureID, name)
	if result.Skipped {
		return nil
	}
	if result.Conflict {
		fmt.Println("Merge conflict — resolve manually:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		fmt.Printf("Worktree: %s\n", worktree.Dir(ws.Root, featureID))
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "warning: merge failed: %s\n", result.Error)
		fmt.Printf("Worktree preserved at: %s\n", worktree.Dir(ws.Root, featureID))
		return nil
	}
	fmt.Printf("Merged and cleaned up branch 4x/%s.\n", featureID)
	return nil
}
