package main

import (
	"fmt"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newRejectCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "reject <feature-id>",
		Short: "Reject a draft feature (draft → abandoned)",
		Long: `Reject a draft feature created by enriched auto-discover.

The feature must be in draft status; it transitions to abandoned so it stays
out of the meta-loop. Use 4x approve to accept a draft instead.`,
		Args: cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			ws, featureID, err := resolveDraftTarget(args[0])
			if err != nil {
				return err
			}
			if err := rejectFeature(ws, featureID); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(struct {
					FeatureID string `json:"featureId"`
					Status    string `json:"status"`
				}{featureID, "abandoned"})
			}
			fmt.Printf("rejected: %s → abandoned\n", featureID)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// rejectFeature 將 draft feature 標記為 abandoned（人工否決，不做了）。非 draft 狀態回 error。
func rejectFeature(ws *protocol.Workspace, featureID string) error {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return fmt.Errorf("feature %s not found: %w", featureID, err)
	}
	if f.Status != feat.StatusDraft {
		return fmt.Errorf("feature %s is %s, not draft", featureID, f.Status)
	}
	f.Status = feat.StatusAbandoned
	if err := ws.SaveFeature(f); err != nil {
		return fmt.Errorf("failed to save feature: %w", err)
	}
	return nil
}
