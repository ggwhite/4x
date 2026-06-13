package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
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

	cfg, err := ws.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot read config, using defaults: %v\n", err)
	}
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}

	ops := gitops.New(ws.Root, ws, cfg)

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	result := ops.Merge(featureID, name)
	if result.Conflict {
		fmt.Println("Merge conflict — feature remains pending-review:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		if result.ConflictRepo != "" {
			fmt.Printf("  repo: %s\n", result.ConflictRepo)
		}
		fmt.Printf("Worktree: %s\n", gitops.Dir(ws.Root, featureID))
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "warning: merge failed; feature remains pending-review: %s\n", result.Error)
		fmt.Printf("Worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return nil
	}

	if err := finalizeDone(ws, featureID, s); err != nil {
		return err
	}

	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
}

func finalizeDone(ws *protocol.Workspace, featureID string, s protocol.State) error {
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

	return ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})
}
