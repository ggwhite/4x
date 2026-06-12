package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newTransitionCmd() *cobra.Command {
	var to string
	var role string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "transition <feature-id>",
		Short: "Transition feature to a new phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			s, err := ws.ReadState(featureID)
			if err != nil {
				if err := ws.InitFeatureDir(featureID); err != nil {
					if jsonOutput {
						return jsonError(err.Error())
					}
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
					if jsonOutput {
						return jsonError(err.Error())
					}
					return err
				}
			}

			toPhase := protocol.Phase(to)
			toRole := protocol.Role(role)
			if toRole == "" {
				toRole = state.PhaseToRole(toPhase)
			}

			if s.Phase == protocol.PhaseTesting && toPhase == protocol.PhaseAccepting {
				result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
				if !result.Pass {
					errMsg := fmt.Sprintf("testing → accepting blocked: %s", strings.Join(result.Errors, "; "))
					if jsonOutput {
						return jsonError(errMsg)
					}
					return fmt.Errorf("%s", errMsg)
				}
			}

			newState, err := state.Transition(s, toPhase, toRole)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			if toPhase == protocol.PhaseDone || toPhase == protocol.PhasePendingReview || toPhase == protocol.PhaseBlocked {
				newState.Active = false
			}

			if err := ws.WriteState(featureID, newState); err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			syncFeatureStatus(ws, featureID, toPhase)

			ws.AppendEvent(featureID, protocol.Event{
				Phase: toPhase,
				Type:  "transition",
				Role:  toRole,
				Round: newState.Round,
			})

			if jsonOutput {
				result := struct {
					FeatureID string `json:"featureId"`
					From      string `json:"from"`
					To        string `json:"to"`
				}{
					FeatureID: featureID,
					From:      string(s.Phase),
					To:        string(toPhase),
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Printf("%s → %s (role: %s, round: %d)\n", s.Phase, toPhase, toRole, newState.Round)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "target phase (required)")
	cmd.Flags().StringVar(&role, "role", "", "override role (optional)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.MarkFlagRequired("to")
	return cmd
}

func syncFeatureStatus(ws *protocol.Workspace, featureID string, phase protocol.Phase) {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return
	}

	switch phase {
	case protocol.PhasePendingReview:
		f.Status = "ready-for-review"
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
