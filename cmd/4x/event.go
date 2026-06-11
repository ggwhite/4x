package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newEventCmd() *cobra.Command {
	var eventType string
	var role string
	var round int
	var action string
	var detail string

	cmd := &cobra.Command{
		Use:   "event <feature-id>",
		Short: "Append an event to the timeline",
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

			evt := protocol.Event{
				Phase:  protocol.Phase(""),
				Type:   eventType,
				Role:   protocol.Role(role),
				Round:  round,
				Action: action,
				Detail: detail,
			}

			if s, err := ws.ReadState(featureID); err == nil {
				evt.Phase = s.Phase
			}

			if err := ws.AppendEvent(featureID, evt); err != nil {
				return err
			}

			fmt.Printf("Event appended: %s\n", eventType)
			return nil
		},
	}

	cmd.Flags().StringVar(&eventType, "type", "", "event type (required)")
	cmd.Flags().StringVar(&role, "role", "", "role")
	cmd.Flags().IntVar(&round, "round", 0, "round number")
	cmd.Flags().StringVar(&action, "action", "", "action")
	cmd.Flags().StringVar(&detail, "detail", "", "detail")
	cmd.MarkFlagRequired("type")
	return cmd
}
