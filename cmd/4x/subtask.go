package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newSubtaskCmd() *cobra.Command {
	var status string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "subtask <featureId> <subtaskId>",
		Short: "Update subtask status",
		Long: `Update the status of a subtask within a feature.

Examples:
  4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
  4x subtask F043-dashboard-screenshot-gall settings-screenshot-dir --status in-progress`,
		Args: cobra.ExactArgs(2),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			subtaskID := args[1]

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
			f, err := ws.LoadFeature(featureID)
			if err != nil {
				return fmt.Errorf("feature %s not found: %w", featureID, err)
			}

			validStatuses := make(map[string]bool, len(feature.AllSubtaskStatuses()))
			for _, s := range feature.AllSubtaskStatuses() {
				validStatuses[s] = true
			}
			if !validStatuses[status] {
				return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(feature.AllSubtaskStatuses(), ", "))
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

			if jsonOutput {
				return printJSON(struct {
					FeatureID string `json:"featureId"`
					SubtaskID string `json:"subtaskId"`
					Status    string `json:"status"`
				}{featureID, subtaskID, status})
			}
			fmt.Printf("Subtask %s → %s\n", subtaskID, status)
			return nil
		}),
	}

	cmd.Flags().StringVar(&status, "status", "", fmt.Sprintf("new status (%s)", strings.Join(feature.AllSubtaskStatuses(), ", ")))
	cmd.MarkFlagRequired("status")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}
