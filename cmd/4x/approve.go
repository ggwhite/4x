package main

import (
	"fmt"
	"os"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newApproveCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "approve <feature-id>",
		Short: "Approve a draft feature (draft → not-started)",
		Long: `Approve a draft feature created by enriched auto-discover.

The feature must be in draft status; it transitions to not-started so the
meta-loop will pick it up. Use 4x reject to discard a draft instead.`,
		Args: cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			ws, featureID, err := resolveDraftTarget(args[0])
			if err != nil {
				return err
			}
			if err := approveFeature(ws, featureID); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(struct {
					FeatureID string `json:"featureId"`
					Status    string `json:"status"`
				}{featureID, "not-started"})
			}
			fmt.Printf("approved: %s → not-started\n", featureID)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// resolveDraftTarget 找到 workspace 並把使用者輸入的 feature 參照解析成正規 feature ID。
func resolveDraftTarget(arg string) (*protocol.Workspace, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return nil, "", err
	}
	featureID, err := ws.ResolveFeatureID(arg)
	if err != nil {
		return nil, "", err
	}
	return ws, featureID, nil
}

// approveFeature 將 draft feature 轉為 not-started。非 draft 狀態回 error，避免誤改其他流程中的 feature。
func approveFeature(ws *protocol.Workspace, featureID string) error {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return fmt.Errorf("feature %s not found: %w", featureID, err)
	}
	if f.Status != feat.StatusDraft {
		return fmt.Errorf("feature %s is %s, not draft", featureID, f.Status)
	}
	f.Status = feat.StatusNotStarted
	if err := ws.SaveFeature(f); err != nil {
		return fmt.Errorf("failed to save feature: %w", err)
	}
	return nil
}
