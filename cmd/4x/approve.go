package main

import (
	"fmt"
	"os"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <feature-id>",
		Short: "Approve a draft feature (draft → not-started)",
		Long: `Approve a draft feature created by enriched auto-discover.

The feature must be in draft status; it transitions to not-started so the
meta-loop will pick it up. Use 4x reject to discard a draft instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, featureID, err := resolveDraftTarget(args[0])
			if err != nil {
				return err
			}
			return approveFeature(ws, featureID)
		},
	}
}

func newRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <feature-id>",
		Short: "Reject a draft feature (draft → abandoned)",
		Long: `Reject a draft feature created by enriched auto-discover.

The feature must be in draft status; it transitions to abandoned so it stays
out of the meta-loop. Use 4x approve to accept a draft instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, featureID, err := resolveDraftTarget(args[0])
			if err != nil {
				return err
			}
			return rejectFeature(ws, featureID)
		},
	}
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
	fmt.Printf("approved: %s → not-started\n", featureID)
	return nil
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
	fmt.Printf("rejected: %s → abandoned\n", featureID)
	return nil
}
