package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <feature-id>",
		Short: "Mark a pending-review feature as done",
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

			return markDone(ws, featureID)
		},
	}
}

// markDone 將 pending-review 的 feature 推進到 done
func markDone(ws *protocol.Workspace, featureID string) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		slog.Warn("cannot read config, using defaults", "error", err)
	}
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}

	result := autoMergeFeature(ws, cfg, s, featureID, name)
	if result.Conflict {
		fmt.Println("Merge conflict — feature remains pending-review:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		if result.ConflictRepo != "" {
			fmt.Printf("  repo: %s\n", result.ConflictRepo)
		}
		fmt.Printf("Worktree: %s\n", gitops.Dir(ws.Root, featureID))
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	if result.Error != "" {
		slog.Error("merge failed", "feature", featureID, "error", result.Error)
		fmt.Printf("Worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return nil
	}

	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
}

// autoMergeFeature 對 pending-review 的 feature 執行 merge，成功（含 skipped）時 finalizeDone 標記 done，
// 回傳 MergeResult 供呼叫端決定後續（衝突→暫停、錯誤→警告續跑、成功→done）。不印任何訊息。
//
// 衝突（result.Conflict）與非衝突錯誤（result.Error != ""）時保持 pending-review、不 finalize；
// 其餘情況（含非 worktree 模式的 Skipped）視為成功並 finalizeDone。若 finalizeDone 失敗，
// 將錯誤編入 result.Error（不另改 MergeResult 結構），讓呼叫端走錯誤分支處理而非靜默忽略。
// merge 邏輯只走 ops.Merge + finalizeDone 一處，batch 與 done 共用此 helper，不重寫第二份流程。
func autoMergeFeature(ws *protocol.Workspace, cfg protocol.Config, s protocol.State, featureID, featureName string) gitops.MergeResult {
	ops := gitops.New(ws.Root, ws, cfg)
	result := ops.Merge(featureID, featureName)
	if !result.Conflict && result.Error == "" {
		if err := finalizeDone(ws, featureID, s); err != nil {
			result.Error = fmt.Sprintf("finalize state failed: %v", err)
		}
	}
	return result
}

func finalizeDone(ws *protocol.Workspace, featureID string, s protocol.State) error {
	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		return err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return err
	}

	ws.SyncFeatureStatus(featureID, protocol.PhaseDone)

	return ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})
}
