package main

import (
	"fmt"
	"os"

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
				return fmt.Errorf("read state: %w", err)
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

			if err := ws.WriteState(featureID, newState); err != nil {
				return err
			}

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
