package main

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newSubtaskCmd() *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "subtask <featureId> <subtaskId>",
		Short: "Update subtask status",
		Long: `Update the status of a subtask within a feature.

Examples:
  4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
  4x subtask F043-dashboard-screenshot-gall settings-screenshot-dir --status in-progress`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			featureID := args[0]
			subtaskID := args[1]

			ws := &protocol.Workspace{Root: "."}
			f, err := ws.LoadFeature(featureID)
			if err != nil {
				return fmt.Errorf("feature %s not found: %w", featureID, err)
			}

			found := false
			for i, st := range f.Subtasks {
				if st.ID == subtaskID {
					f.Subtasks[i].Status = status
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("subtask %s not found in feature %s", subtaskID, featureID)
			}

			if err := ws.SaveFeature(f); err != nil {
				return fmt.Errorf("failed to save feature: %w", err)
			}

			fmt.Printf("Subtask %s → %s\n", subtaskID, status)
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "new status (done, in-progress, blocked, not-started)")
	cmd.MarkFlagRequired("status")

	return cmd
}
