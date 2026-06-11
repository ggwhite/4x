package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newTransitionCmd() *cobra.Command {
	var to string
	var role string

	cmd := &cobra.Command{
		Use:   "transition <feature-id>",
		Short: "Transition feature to a new phase",
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

			featureID := args[0]

			s, err := ws.ReadState(featureID)
			if err != nil {
				if err := ws.InitFeatureDir(featureID); err != nil {
					return err
				}
				s = protocol.State{
					FeatureID: featureID,
					Phase:     protocol.PhaseInit,
					MaxRounds: 5,
					Active:    true,
					Runner:    "claude",
					CreatedAt: time.Now(),
				}
				if err := ws.WriteState(featureID, s); err != nil {
					return err
				}
			}

			toPhase := protocol.Phase(to)
			toRole := protocol.Role(role)
			if toRole == "" {
				toRole = state.PhaseToRole(toPhase)
			}

			newState, err := state.Transition(s, toPhase, toRole)
			if err != nil {
				return err
			}

			if toPhase == protocol.PhaseDone || toPhase == protocol.PhaseBlocked {
				newState.Active = false
			}

			if err := ws.WriteState(featureID, newState); err != nil {
				return err
			}

			syncFeatureStatus(ws, featureID, toPhase)

			ws.AppendEvent(featureID, protocol.Event{
				Phase: toPhase,
				Type:  "transition",
				Role:  toRole,
				Round: newState.Round,
			})

			fmt.Printf("%s → %s (role: %s, round: %d)\n", s.Phase, toPhase, toRole, newState.Round)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "target phase (required)")
	cmd.Flags().StringVar(&role, "role", "", "override role (optional)")
	cmd.MarkFlagRequired("to")
	return cmd
}

func syncFeatureStatus(ws *protocol.Workspace, featureID string, phase protocol.Phase) {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return
	}

	switch phase {
	case protocol.PhaseDone:
		f.Status = "done"
	case protocol.PhaseBlocked:
		f.Status = "blocked"
	case protocol.PhaseNeedsAttention:
		f.Status = "needs-attention"
	case protocol.PhaseInit:
		f.Status = "not-started"
	default:
		f.Status = "in-progress"
	}

	ws.SaveFeature(f)
}
