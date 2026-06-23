package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newDoneCmd() *cobra.Command {
	var approveSelfMod bool
	cmd := &cobra.Command{
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

			return markDone(ws, featureID, approveSelfMod)
		},
	}
	cmd.Flags().BoolVar(&approveSelfMod, "approve-self-mod", false,
		"approve self-modification of protected paths so the feature can be merged")
	return cmd
}

// markDone 將 pending-review 的 feature 推進到 done。
// approveSelfMod 為 true 時核可受保護路徑變更（self-mod guard），允許繞過人工 approve 關卡完成 merge。
func markDone(ws *protocol.Workspace, featureID string, approveSelfMod bool) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	if guard.SelfModNeedsApproval(s, approveSelfMod) {
		printSelfModApprovalRequired(featureID, s.SelfModPaths)
		return fmt.Errorf("feature %s requires --approve-self-mod to complete", featureID)
	}
	if approveSelfMod && s.SelfModTouched && !s.SelfModApproved {
		if err := approveSelfModState(ws, featureID, &s); err != nil {
			return err
		}
	}

	// config 載入失敗不可降級為零值 Config{} 續跑 merge：零值會選錯 mono/multi
	// 策略而把分支 merge 到錯誤的目標，與 run 路徑一致直接中斷（見 run.go LoadMergedConfig）。
	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return fmt.Errorf("cannot load config for %s: %w", featureID, err)
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
		} else {
			commitLearnings(ws)
		}
	}
	return result
}

// commitLearnings 在 feature done 後自動 commit learnings.json（若有變更）。
func commitLearnings(ws *protocol.Workspace) {
	lpath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if _, err := os.Stat(lpath); err != nil {
		return
	}
	rel, _ := filepath.Rel(ws.Root, lpath)
	cmd := exec.Command("git", "diff", "--quiet", "--", rel)
	cmd.Dir = ws.Root
	if cmd.Run() == nil {
		return
	}
	add := exec.Command("git", "add", rel)
	add.Dir = ws.Root
	if err := add.Run(); err != nil {
		slog.Warn("git add learnings.json failed", "error", err)
		return
	}
	ci := exec.Command("git", "commit", "-m", "chore: update learnings.json")
	ci.Dir = ws.Root
	if err := ci.Run(); err != nil {
		slog.Warn("git commit learnings.json failed", "error", err)
	}
}

// printSelfModApprovalRequired 印出受保護路徑變更需人工 approve 的訊息，列出觸及路徑與重跑指示。
func printSelfModApprovalRequired(featureID string, paths []string) {
	fmt.Printf("Feature %s 觸及受保護路徑（self-mod guard），需人工 approve 才能 merge：\n", featureID)
	for _, p := range paths {
		fmt.Printf("  protected: %s\n", p)
	}
	fmt.Printf("確認無誤後加上 --approve-self-mod 重跑以核可並 merge。\n")
}

// approveSelfModState 設定 SelfModApproved 並持久化，append 一條 self-mod-approved event。
func approveSelfModState(ws *protocol.Workspace, featureID string, s *protocol.State) error {
	s.SelfModApproved = true
	if err := ws.WriteState(featureID, *s); err != nil {
		return fmt.Errorf("cannot persist self-mod approval for %s: %w", featureID, err)
	}
	return ws.AppendEvent(featureID, protocol.Event{
		Type: "self-mod-approved", Phase: s.Phase, Round: s.Round, Notify: protocol.NotifyWarning,
	})
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

	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseDone), featureID, protocol.PhaseDone)

	return ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})
}
