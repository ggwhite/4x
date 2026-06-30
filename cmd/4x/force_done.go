package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newForceDoneCmd() *cobra.Command {
	var (
		reason  string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "force-done <feature-id>",
		Short: "Force a feature to done from any phase (needs --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOut, func(cmd *cobra.Command, args []string) error {
			if reason == "" {
				return fmt.Errorf("--reason is required: explain why you're skipping the normal pipeline")
			}
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
			return forceDone(ws, featureID, reason, jsonOut)
		}),
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this feature is being force-completed (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output result as JSON")
	return cmd
}

func forceDone(ws *protocol.Workspace, featureID, reason string, jsonOut bool) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase == protocol.PhaseDone || s.Phase == protocol.PhaseAbandoned {
		return fmt.Errorf("feature %s is already in terminal phase %q", featureID, s.Phase)
	}

	if s.Phase == protocol.PhasePendingReview {
		return markDone(ws, featureID, false, jsonOut)
	}

	if guard.SelfModNeedsApproval(s, false) {
		return fmt.Errorf("feature %s touches protected paths — use 'force-done --approve-self-mod' or resolve via normal pipeline", featureID)
	}

	newState, err := state.Transition(s, protocol.PhaseDone, "force-done")
	if err != nil {
		return fmt.Errorf("cannot transition %s from %s to done: %w", featureID, s.Phase, err)
	}
	newState.Active = false
	newState.StopReason = "force-done"
	newState.StopMessage = reason

	if err := ws.WriteState(featureID, newState); err != nil {
		return err
	}
	if err := ws.SyncFeatureStatus(featureID, protocol.PhaseDone); err != nil {
		slog.Warn("sync feature status failed", "feature", featureID, "error", err)
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type:   "force-done",
		Phase:  protocol.PhaseDone,
		Round:  newState.Round,
		Detail: reason,
		Notify: protocol.NotifyWarning,
	})

	if !jsonOut {
		fmt.Printf("Feature %s force-done (reason: %s)\n", featureID, reason)
	}

	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return fmt.Errorf("cannot load config for %s: %w", featureID, err)
	}

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}

	result := autoMergeFeature(ws, cfg, featureID, name)
	if result.Conflict {
		if jsonOut {
			return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhaseDone), Conflict: true})
		}
		fmt.Println("Merge conflict — resolve manually:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	if result.Error != "" {
		slog.Error("merge failed", "feature", featureID, "error", result.Error)
		if jsonOut {
			return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhaseDone)})
		}
		fmt.Printf("Worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return nil
	}

	if jsonOut {
		return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhaseDone), Merged: !result.Skipped})
	}
	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
}
