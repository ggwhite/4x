package main

import (
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newDoneCmd() *cobra.Command {
	var approveSelfMod bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "done <feature-id>",
		Short: "Mark a pending-review feature as done",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
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

			return markDone(ws, featureID, approveSelfMod, jsonOutput)
		}),
	}
	cmd.Flags().BoolVar(&approveSelfMod, "approve-self-mod", false,
		"approve self-modification of protected paths so the feature can be merged")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// markDone 將 pending-review 的 feature 推進到 done。
// approveSelfMod 為 true 時核可受保護路徑變更（self-mod guard），允許繞過人工 approve 關卡完成 merge。
// jsonOutput 為 true 時，成功／衝突路徑只印單一 JSON object（不夾雜文字），供 MCP 端解析。
func markDone(ws *protocol.Workspace, featureID string, approveSelfMod, jsonOutput bool) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	if guard.SelfModNeedsApproval(s, approveSelfMod) {
		if !jsonOutput {
			printSelfModApprovalRequired(featureID, s.SelfModPaths)
		}
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

	result := autoMergeFeature(ws, cfg, featureID, name)
	if result.Conflict {
		if jsonOutput {
			return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhasePendingReview), Conflict: true})
		}
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
		if jsonOutput {
			return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhasePendingReview)})
		}
		fmt.Printf("Worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return nil
	}

	if jsonOutput {
		return printJSON(doneResult{
			FeatureID: featureID, Phase: string(protocol.PhaseDone), Merged: !result.Skipped, MRUrls: result.MRUrls,
		})
	}
	fmt.Printf("Feature %s marked as done.\n", featureID)
	if len(result.MRUrls) > 0 {
		printMRUrls(result.MRUrls)
	} else if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
}

// printMRUrls 依 repo 名稱排序列印每個開出的 MR/PR URL；monorepo 固定 key "." 省略 repo 標註。
func printMRUrls(mrUrls map[string]string) {
	names := make([]string, 0, len(mrUrls))
	for name := range mrUrls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "." {
			fmt.Printf("MR opened: %s\n", mrUrls[name])
		} else {
			fmt.Printf("MR opened(%s): %s\n", name, mrUrls[name])
		}
	}
}

// doneResult 是 `4x done` 與 `4x merge` 在 --json 下的成功／衝突輸出結構。
type doneResult struct {
	FeatureID string            `json:"featureId"`
	Phase     string            `json:"phase,omitempty"`
	Merged    bool              `json:"merged"`
	Conflict  bool              `json:"conflict"`
	MRUrls    map[string]string `json:"mrUrls,omitempty"`
}

// autoMergeFeature 對 pending-review 的 feature 執行 merge 並標記 done，委派共用編排
// gitops.MergeAndFinalize（ops.Merge → re-read state → state.FinalizeDone → commitSelfManaged）。
// batch 與 done 共用此 helper，不重寫第二份 merge+finalize 流程。回傳 MergeResult 供呼叫端決定後續
// （衝突→暫停、錯誤→警告續跑、成功→done）。不印任何訊息。
//
// 真正 fatal 的 finalize/re-read 失敗（MergeAndFinalize 回傳的 error）與 merge 期間 state 已被改動
// （StateChanged）皆編入 result.Error，讓呼叫端走錯誤分支（保持 pending-review、保留 worktree）
// 而非靜默成功；CLI 單程序下 StateChanged 理論上不會發生。
func autoMergeFeature(ws *protocol.Workspace, cfg protocol.Config, featureID, featureName string) gitops.MergeResult {
	result, err := gitops.MergeAndFinalize(ws.Root, ws, cfg, featureID, featureName)
	if err != nil {
		result.Error = fmt.Sprintf("finalize state failed: %v", err)
	} else if result.StateChanged {
		result.Error = fmt.Sprintf("state changed during merge (phase=%s)", result.FinalState.Phase)
	}
	return result
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

// finalizeDone 委派共用的 state.FinalizeDone 收尾序列。merge.go 在解決衝突後續跑時共用此 wrapper，
// 不再各自重寫 Transition+WriteState+AppendEvent。
func finalizeDone(ws *protocol.Workspace, featureID string, s protocol.State) error {
	_, err := state.FinalizeDone(ws, featureID, s)
	return err
}
