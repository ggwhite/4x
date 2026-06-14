package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch operations for multiple features",
	}

	cmd.AddCommand(newBatchPlanCmd())
	cmd.AddCommand(newBatchNextCmd())
	cmd.AddCommand(newBatchStopCmd())
	cmd.AddCommand(newBatchRunCmd())
	return cmd
}

func newBatchPlanCmd() *cobra.Command {
	var dryRun bool
	var maxChain int

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Plan batch execution (dependency DAG + Union-Find grouping)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			cfg, _ := ws.ReadConfig()
			if userCfg, err := protocol.ReadUserConfig(); err == nil {
				cfg = protocol.MergeConfig(userCfg, cfg)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []protocol.Feature
			for _, f := range features {
				if f.Status != protocol.StatusDone {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features to batch.")
				return nil
			}

			plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), maxChain)
			if err != nil {
				return err
			}

			if dryRun {
				return printPlan(plan)
			}

			data, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return err
			}
			planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
			if err := os.WriteFile(planPath, data, 0o644); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "plan", "features", len(plan.Schedule), "path", planPath)
			fmt.Printf("Wrote %s\n", planPath)
			return printPlan(plan)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print schedule without writing file")
	cmd.Flags().IntVar(&maxChain, "max-chain", 4, "Maximum chain length per cluster")
	return cmd
}

func printPlan(plan *batch.BatchPlan) error {
	for _, c := range plan.Clusters {
		fmt.Printf("  %s: ", c.ID)
		for i, chain := range c.Chains {
			if i > 0 {
				fmt.Print(" | ")
			}
			for j, fID := range chain {
				if j > 0 {
					fmt.Print(" → ")
				}
				fmt.Print(fID)
			}
		}
		fmt.Println()
	}

	fmt.Printf("\nSchedule (%d features):\n", len(plan.Schedule))
	for _, s := range plan.Schedule {
		after := "—"
		if len(s.CanStartAfter) > 0 {
			after = fmt.Sprintf("after %v", s.CanStartAfter)
		}
		fmt.Printf("  [slot %d] %s %s\n", s.Slot, s.FeatureID, after)
	}
	return nil
}

func newBatchNextCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the next eligible feature to run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
			data, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("no batch-plan.json found, run '4x batch plan' first")
			}

			var plan batch.BatchPlan
			if err := json.Unmarshal(data, &plan); err != nil {
				return fmt.Errorf("invalid batch-plan.json: %w", err)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}
			statusMap := make(map[string]protocol.Status)
			featureMap := make(map[string]protocol.Feature)
			for _, f := range features {
				statusMap[f.ID] = f.Status
				featureMap[f.ID] = f
			}

			for _, s := range plan.Schedule {
				if batchCompleted(statusMap[s.FeatureID]) {
					continue
				}
				allDone := true
				for _, dep := range s.CanStartAfter {
					if !batchCompleted(statusMap[dep]) {
						allDone = false
						break
					}
				}
				if allDone {
					if !jsonOutput {
						fmt.Println(s.FeatureID)
						return nil
					}

					result := struct {
						FeatureID       string   `json:"featureId"`
						Slot            int      `json:"slot"`
						SubtaskFrontier []string `json:"subtaskFrontier"`
					}{
						FeatureID: s.FeatureID,
						Slot:      s.Slot,
					}

					if f, ok := featureMap[s.FeatureID]; ok && len(f.Subtasks) > 0 {
						frontier, err := batch.SubtaskFrontier(f.Subtasks)
						if err != nil {
							return fmt.Errorf("feature %s subtask dependency error: %w", s.FeatureID, err)
						}
						result.SubtaskFrontier = frontier
					}
					if result.SubtaskFrontier == nil {
						result.SubtaskFrontier = []string{}
					}

					out, err := json.MarshalIndent(result, "", "  ")
					if err != nil {
						return err
					}
					fmt.Println(string(out))
					return nil
				}
			}

			if jsonOutput {
				fmt.Println("null")
			} else {
				fmt.Println("No eligible features (all done or blocked by dependencies).")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format with subtask frontier")
	return cmd
}

func newBatchRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var noAutoMerge bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run eligible features in dependency order",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				return err
			}
			if userCfg, err := protocol.ReadUserConfig(); err != nil {
				slog.Warn("failed to read user config", "error", err)
			} else {
				cfg = protocol.MergeConfig(userCfg, cfg)
			}

			if runnerName == "" {
				runnerName = cfg.Default
			}
			runnerCfg, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []protocol.Feature
			for _, f := range features {
				if !batchCompleted(f.Status) {
					pending = append(pending, f)
				}
			}

			if len(pending) == 0 {
				fmt.Println("No pending features.")
				return nil
			}

			plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), 4)
			if err != nil {
				return err
			}

			statusMap := make(map[string]protocol.Status)
			for _, f := range features {
				statusMap[f.ID] = f.Status
			}

			// runFeature 執行單一 feature 的完整 runLoop（含 worktree 隔離），回傳跑完後的最新 status。
			// 抽出成 callback 讓 runBatchSchedule 的排程 / gate / 失敗追蹤邏輯可獨立測試。
			runFeature := func(next string, feature protocol.Feature, s protocol.State) (protocol.Status, error) {
				signal.Ignore(syscall.SIGPIPE)
				batchCtx, batchCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer batchCancel()
				batchOps := gitops.New(ws.Root, ws, cfg)

				batchRunnerWs := ws
				commitStrategy := "never"
				if cfg.Isolation == "worktree" {
					wtPath, wtErr := batchOps.SetupWorktree(next)
					if wtErr != nil {
						return protocol.StatusBlocked, fmt.Errorf("worktree setup failed: %w", wtErr)
					}
					batchRunnerWs = &protocol.Workspace{Root: wtPath}
					commitStrategy = "per-round"
				}

				runnerFactory := func(logPath string, model string) runner.Runner {
					return runner.NewRunner(batchRunnerWs, runnerName, runnerCfg, time.Duration(timeout)*time.Second, logPath, model)
				}
				runErr := runLoop(batchCtx, ws, batchRunnerWs, feature, cfg, s, batchOps, runnerFactory, commitStrategy)

				updated, _ := ws.LoadFeature(next)
				return updated.Status, runErr
			}

			// autoMerge 對 ready-for-review 的 feature 走 done.go 的共用 helper（ops.Merge + finalizeDone），
			// 回傳 MergeResult 供 runBatchSchedule 決定衝突暫停 / 錯誤續跑 / 成功標 done。
			autoMerge := func(featureID string) gitops.MergeResult {
				st, err := ws.ReadState(featureID)
				if err != nil {
					return gitops.MergeResult{Error: fmt.Sprintf("cannot read state for %s: %v", featureID, err)}
				}
				f, _ := ws.LoadFeature(featureID)
				name := featureID
				if f.Name != "" {
					name = f.Name
				}
				return autoMergeFeature(ws, cfg, st, featureID, name)
			}

			slog.Info("batch operation", "action", "run", "features", len(plan.Schedule), "runner", runnerName)
			completed := runBatchSchedule(ws, plan, statusMap, maxRounds, runnerName, runFeature, noAutoMerge, autoMerge)

			slog.Info("batch operation", "action", "complete", "completed", completed, "total", len(plan.Schedule))
			fmt.Printf("\n══════════════════════════════════════\n")
			fmt.Printf("  BATCH COMPLETE: %d/%d features done\n", completed, len(plan.Schedule))
			fmt.Printf("══════════════════════════════════════\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max rounds per feature (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&noAutoMerge, "no-auto-merge", false, "feature 完成後停在 pending-review，不自動 merge 回 main")
	return cmd
}

// maxFeatureFailures 是 batch 對單一 feature 連續跑出失敗狀態的容忍上限，達標後跳過避免無限重跑。
const maxFeatureFailures = 2

// runBatchSchedule 依 plan 順序挑選並執行 feature，套用 4x run 啟動前的三道 gate（W4：
// dependency 檢查、已 done 跳過、PID 記錄）與失敗重跑上限（W12）。runFeature 注入實際執行
// （worktree + runLoop）並回傳跑完後的最新 status，測試可替換為模擬。回傳完成的 feature 數。
//
// F068：feature 完成（ready-for-review）後若 !noAutoMerge 且 autoMerge != nil，呼叫注入的
// autoMerge 把 worktree 改動合回 main——衝突則 graceful pause（保留 worktree、回傳目前 completed），
// 非衝突錯誤則警告後續跑，成功（含 skipped）則標記 done。autoMerge 注入式（mirror runFeature）讓測試可替換。
func runBatchSchedule(ws *protocol.Workspace, plan *batch.BatchPlan, statusMap map[string]protocol.Status,
	maxRounds int, runnerName string,
	runFeature func(next string, feature protocol.Feature, s protocol.State) (protocol.Status, error),
	noAutoMerge bool, autoMerge func(featureID string) gitops.MergeResult) int {

	stopFile := filepath.Join(ws.DotDir(), "batch-stop")
	completed := 0
	// W12：追蹤每個 feature 跑出失敗狀態（needs-attention/blocked）的次數，
	// 達 maxFeatureFailures 後從 selection 跳過。loggedSkip 確保 skip 訊息只印一次。
	failedFeatures := map[string]int{}
	loggedSkip := map[string]bool{}

	for {
		if _, err := os.Stat(stopFile); err == nil {
			os.Remove(stopFile)
			fmt.Printf("\n⏸ batch-stop detected — stopping gracefully (%d done)\n", completed)
			break
		}

		next := ""
		for _, s := range plan.Schedule {
			if batchCompleted(statusMap[s.FeatureID]) {
				continue
			}
			if failedFeatures[s.FeatureID] >= maxFeatureFailures {
				if !loggedSkip[s.FeatureID] {
					fmt.Printf("  skipping %s: failed %d times\n", s.FeatureID, failedFeatures[s.FeatureID])
					loggedSkip[s.FeatureID] = true
				}
				continue
			}
			allDone := true
			for _, dep := range s.CanStartAfter {
				if !batchCompleted(statusMap[dep]) {
					allDone = false
					break
				}
			}
			if allDone {
				next = s.FeatureID
				break
			}
		}

		if next == "" {
			break
		}

		slog.Info("batch feature", "feature", next, "status", "started", "completed", completed, "total", len(plan.Schedule))
		fmt.Printf("\n══════════════════════════════════════\n")
		fmt.Printf("  BATCH: %s (%d/%d done)\n", next, completed, len(plan.Schedule))
		fmt.Printf("══════════════════════════════════════\n\n")

		feature, err := ws.LoadFeature(next)
		if err != nil {
			fmt.Printf("  error loading feature: %v\n", err)
			statusMap[next] = protocol.StatusBlocked
			failedFeatures[next]++
			continue
		}

		if err := ws.InitFeatureDir(next); err != nil {
			fmt.Printf("  init feature dir failed: %v\n", err)
			statusMap[next] = protocol.StatusBlocked
			failedFeatures[next]++
			continue
		}

		// W4：套用 4x run 啟動前的 dependency gate；未完成則跳過並標記 blocked。
		depResult := guard.CheckDependencies(ws, next)
		if !depResult.Pass {
			fmt.Printf("  dependency check failed: %s\n", strings.Join(depResult.Errors, "; "))
			statusMap[next] = protocol.StatusBlocked
			failedFeatures[next]++
			continue
		}

		rounds := maxRounds
		if rounds <= 0 {
			rounds = 5
		}

		s := protocol.State{
			FeatureID: next,
			Phase:     protocol.PhaseInit,
			MaxRounds: rounds,
			Active:    true,
			Runner:    runnerName,
			CreatedAt: time.Now(),
		}
		if existing, err := ws.ReadState(next); err == nil {
			s = existing
			s.Active = true
		}

		// W4：resume 既有 state 時，若已 done 則跳過不重跑。
		if s.Phase == protocol.PhaseDone {
			fmt.Printf("  %s already done — skipping\n", next)
			statusMap[next] = protocol.StatusDone
			completed++
			continue
		}

		// W4：記錄本 process PID，與 4x run 一致。
		s.Pid = os.Getpid()
		_ = ws.WriteState(next, s)

		updatedStatus, runErr := runFeature(next, feature, s)
		statusMap[next] = updatedStatus

		slog.Info("batch feature", "feature", next, "status", "completed", "result", string(updatedStatus))

		// W12：跑出失敗狀態時累計，達上限後於 selection 跳過避免無限重跑。
		if updatedStatus == protocol.StatusNeedsAttention || updatedStatus == protocol.StatusBlocked {
			failedFeatures[next]++
		}

		if runErr != nil {
			fmt.Printf("  feature %s failed: %v\n", next, runErr)
		}

		// F068：feature 完成（ready-for-review/pending-review）後自動 merge 回 main，
		// 使下一個 feature 的 worktree 從含本輪改動的最新 main 開出 branch。
		if !noAutoMerge && updatedStatus == protocol.StatusReadyForReview && autoMerge != nil {
			result := autoMerge(next)
			switch {
			case result.Conflict:
				// M2：衝突 → graceful pause。保留 pending-review 與 worktree、不計入 completed、停止主迴圈。
				slog.Error("auto-merge conflict", "feature", next, "files", result.Files, "repo", result.ConflictRepo)
				fmt.Printf("\n⏸ auto-merge conflict on %s — pausing batch (%d done):\n", next, completed)
				for _, file := range result.Files {
					fmt.Printf("  conflict: %s\n", file)
				}
				if result.ConflictRepo != "" {
					fmt.Printf("  repo: %s\n", result.ConflictRepo)
				}
				fmt.Printf("  worktree: %s\n", gitops.Dir(ws.Root, next))
				fmt.Printf("  resolve conflicts, then run '4x merge %s' and re-run '4x batch run' to continue.\n", next)
				return completed
			case result.Error != "":
				// M3：非衝突錯誤 → 警告後嘗試下一個 feature；feature 仍算 ready-for-review（batchCompleted）。
				slog.Error("auto-merge failed", "feature", next, "error", result.Error)
				fmt.Printf("  worktree preserved at: %s\n", gitops.Dir(ws.Root, next))
			default:
				// M1/M4：成功或 skipped（非 worktree 模式）→ 標記 done。
				slog.Info("auto-merge succeeded", "feature", next, "skipped", result.Skipped)
				statusMap[next] = protocol.StatusDone
			}
		}

		if batchCompleted(updatedStatus) {
			completed++
		}
	}

	return completed
}

func newBatchStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Signal batch to stop after current feature completes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			stopFile := filepath.Join(ws.DotDir(), "batch-stop")
			if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "stop")
			fmt.Println("Stop signal sent — batch will finish current feature then exit.")
			return nil
		},
	}
}

func batchCompleted(s protocol.Status) bool {
	return s == protocol.StatusDone || s == protocol.StatusAbandoned || s == protocol.StatusReadyForReview
}
