package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/batch"
	feat "github.com/ggwhite/4x/internal/feature"
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

			cfg, _ := ws.LoadMergedConfig()

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []feat.Feature
			for _, f := range features {
				if f.Status != feat.StatusDone {
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
			statusMap := make(map[string]feat.Status)
			featureMap := make(map[string]feat.Feature)
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

			// 留下自己的 PID，讓 server 重啟後可 adopt 仍存活的孤兒；
			// 正常結束（return nil 或一般 error）皆由 defer 清除。SIGKILL 不觸發 defer，
			// 殘留的 stale PID 檔由 server 端 adopt 邏輯偵測清理。
			_ = ws.WriteBatchPID(os.Getpid())
			defer func() { _ = ws.ClearBatchPID() }()

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

			// manualRunner 保存使用者顯式指定的 --runner（覆寫優先序最高層，全 phase 套用）；
			// 未指定時為空，讓 per-phase profile/feature override 與 default_runner 生效。
			manualRunner := runnerName
			if runnerName == "" {
				runnerName = cfg.Default
			}
			if _, ok := cfg.Runners[runnerName]; !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			features, err := ws.ListFeatures()
			if err != nil {
				return err
			}

			var pending []feat.Feature
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

			if planData, je := json.MarshalIndent(plan, "", "  "); je == nil {
				_ = os.WriteFile(filepath.Join(ws.DotDir(), "batch-plan.json"), planData, 0o644)
			}

			statusMap := make(map[string]feat.Status)
			for _, f := range features {
				statusMap[f.ID] = f.Status
			}

			// progress 是 S1/S2/S3 共用的進度快照：runBatchSchedule 在主迴圈更新，
			// signal handler 與 panic recover 讀它建報告。預設 outcome 為 completed，
			// 偵測 stop-file / 衝突暫停時改為 stopped。
			progress := &batchProgress{
				startedAt: time.Now(),
				statusMap: maps.Clone(statusMap),
				outcome:   protocol.BatchOutcomeCompleted,
			}

			// S2：行程層 signal handler。收到 SIGTERM/SIGINT → 以 outcome=interrupted +
			// 當前 runningFeature 寫一份 best-effort 報告後結束行程（130）。doneCh 讓正常結束時
			// goroutine 安靜退出，不會誤寫 interrupted 報告。與 runFeature 內 per-feature signal
			// context 並存：行程層負責寫報告與終止 batch，feature 層負責中止子程序。
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			doneCh := make(chan struct{})
			defer func() {
				signal.Stop(sigCh)
				close(doneCh)
			}()
			go func() {
				select {
				case <-doneCh:
					return
				case <-sigCh:
					finishBatchReport(ws, plan, runnerName, progress, protocol.BatchOutcomeInterrupted, "")
					os.Exit(130)
				}
			}()

			// S3：行程層 panic recover。寫 outcome=crashed + PanicMessage 的 partial report（best-effort，
			// 寫失敗只記 log 不掩蓋原 panic），然後 re-panic 保留原本的堆疊與非零退出。
			defer func() {
				if r := recover(); r != nil {
					finishBatchReport(ws, plan, runnerName, progress, protocol.BatchOutcomeCrashed, fmt.Sprint(r))
					panic(r)
				}
			}()

			// runFeature 執行單一 feature 的完整 runLoop（含 worktree 隔離），回傳跑完後的最新 status。
			// 抽出成 callback 讓 runBatchSchedule 的排程 / gate / 失敗追蹤邏輯可獨立測試。
			runFeature := func(next string, feature feat.Feature, s protocol.State) (feat.Status, error) {
				signal.Ignore(syscall.SIGPIPE)
				batchCtx, batchCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer batchCancel()
				batchOps := gitops.New(ws.Root, ws, cfg)

				batchRunnerWs := ws
				commitStrategy := "never"
				if cfg.Isolation == "worktree" {
					wtPath, wtErr := batchOps.SetupWorktree(next, feature.Repos)
					if wtErr != nil {
						return feat.StatusBlocked, fmt.Errorf("worktree setup failed: %w", wtErr)
					}
					batchRunnerWs = &protocol.Workspace{Root: wtPath}
					commitStrategy = "per-round"
				}

				runnerFactory := func(rn string, logPath string, model string) runner.Runner {
					return runner.NewRunner(batchRunnerWs, rn, cfg.Runners[rn], time.Duration(timeout)*time.Second, logPath, model)
				}
				runErr := runLoop(batchCtx, ws, batchRunnerWs, feature, cfg, s, batchOps, runnerFactory, commitStrategy, manualRunner, nil)

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
				// self-mod guard：觸及受保護路徑的 feature 一律需人工 approve，不可全自動 merge。
				// batch 為非互動流程，無法在此核可，只能 block 並 warn，由人工跑 4x done --approve-self-mod。
				if guard.SelfModNeedsApproval(st, false) {
					slog.Warn("self-mod guard: protected paths touched, auto-merge blocked — use 4x done --approve-self-mod",
						"feature", featureID, "paths", st.SelfModPaths)
					return gitops.MergeResult{Error: "self-mod: protected paths require manual --approve-self-mod"}
				}
				f, _ := ws.LoadFeature(featureID)
				name := featureID
				if f.Name != "" {
					name = f.Name
				}
				return autoMergeFeature(ws, cfg, st, featureID, name)
			}

			slog.Info("batch operation", "action", "run", "features", len(plan.Schedule), "runner", runnerName)
			completed := runBatchSchedule(ws, plan, statusMap, maxRounds, runnerName, runFeature, noAutoMerge, autoMerge, progress)

			// S1：正常停止（自然跑完或 stop-file / 衝突暫停）後寫整體報告。
			// outcome 由 progress 決定：stop-file / 衝突 → stopped，自然跑完 → completed。
			finishBatchReport(ws, plan, runnerName, progress, "", "")

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

// batchProgress 是 batch run 的進度快照，供三種結束觸發點（S1 正常停止、S2 signal 中斷、
// S3 panic）共用同一份資料建報告。主迴圈在 runBatchSchedule 內更新，signal handler 與
// panic recover goroutine 透過 snapshot() 在鎖保護下讀取，避免與主迴圈對 statusMap 的 race。
type batchProgress struct {
	mu             sync.Mutex
	startedAt      time.Time
	statusMap      map[string]feat.Status
	runningFeature string
	outcome        string
}

// setRunning 記錄目前正在跑的 feature id（被選中即將執行時呼叫）。
func (p *batchProgress) setRunning(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = id
}

// update 在一個 feature 收尾後刷新進度：清空 runningFeature、複製最新 statusMap。
// 完成數不在此追蹤——報告的 Completed 由 BuildBatchReport 從 statusMap 重新統計。
func (p *batchProgress) update(statusMap map[string]feat.Status) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = ""
	p.statusMap = maps.Clone(statusMap)
}

// markStopped 把 outcome 標記為 stopped（stop-file / 衝突暫停等 graceful 提前結束）。
func (p *batchProgress) markStopped() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outcome = protocol.BatchOutcomeStopped
}

// snapshot 在鎖保護下回傳目前進度（statusMap 為複本），供 finishBatchReport 建報告。
func (p *batchProgress) snapshot() (startedAt time.Time, runningFeature, outcome string, statusMap map[string]feat.Status) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt, p.runningFeature, p.outcome, maps.Clone(p.statusMap)
}

// finishBatchReport 取進度快照、建報告並原子寫出，是 S1/S2/S3 三個結束觸發點與其測試共用的收尾。
// outcomeOverride 非空時覆蓋快照的 outcome（S2 interrupted、S3 crashed）；空字串代表沿用快照 outcome
// （S1：stop-file/衝突 → stopped，自然跑完 → completed）。panicMsg 僅 crashed 帶入。寫檔失敗只記 log
// （best-effort），不掩蓋呼叫端後續的 os.Exit／re-panic。回傳組好的報告供測試斷言。
func finishBatchReport(ws *protocol.Workspace, plan *batch.BatchPlan, runnerName string,
	progress *batchProgress, outcomeOverride, panicMsg string) protocol.BatchReport {
	startedAt, running, outcome, sm := progress.snapshot()
	if outcomeOverride != "" {
		outcome = outcomeOverride
	} else {
		// S1（completed/stopped）：依 BuildBatchReport 契約（report.go:15）無正在跑的
		// feature。snapshot 的 running 可能因末位 feature 走 error-continue 路徑殘留，須清空。
		running = ""
	}
	report := batch.BuildBatchReport(ws, plan, sm, runnerName, startedAt, time.Now(), outcome, running, panicMsg)
	if err := ws.WriteBatchReport(report); err != nil {
		slog.Warn("failed to write batch report", "outcome", outcome, "error", err)
	}
	return report
}

// runBatchSchedule 依 plan 順序挑選並執行 feature，套用 4x run 啟動前的三道 gate（W4：
// dependency 檢查、已 done 跳過、PID 記錄）與失敗重跑上限（W12）。runFeature 注入實際執行
// （worktree + runLoop）並回傳跑完後的最新 status，測試可替換為模擬。回傳完成的 feature 數。
//
// F068：feature 完成（ready-for-review）後若 !noAutoMerge 且 autoMerge != nil，呼叫注入的
// autoMerge 把 worktree 改動合回 main——衝突則 graceful pause（保留 worktree、回傳目前 completed），
// 非衝突錯誤則警告後續跑，成功（含 skipped）則標記 done。autoMerge 注入式（mirror runFeature）讓測試可替換。
// progress（可為 nil，測試傳 nil）讓 batch 行程層的 signal/panic handler 讀取目前進度建報告。
func runBatchSchedule(ws *protocol.Workspace, plan *batch.BatchPlan, statusMap map[string]feat.Status,
	maxRounds int, runnerName string,
	runFeature func(next string, feature feat.Feature, s protocol.State) (feat.Status, error),
	noAutoMerge bool, autoMerge func(featureID string) gitops.MergeResult, progress *batchProgress) int {

	stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
	// 進入主迴圈前清掉上一輪殘留的衝突信號，避免 dashboard 顯示過時的 conflict。
	if err := ws.ClearBatchConflict(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to clear stale batch conflict: %v\n", err)
	}
	completed := 0
	// W12：追蹤每個 feature 跑出失敗狀態（needs-attention/blocked）的次數，
	// 達 maxFeatureFailures 後從 selection 跳過。loggedSkip 確保 skip 訊息只印一次。
	failedFeatures := map[string]int{}
	loggedSkip := map[string]bool{}

	for {
		if _, err := os.Stat(stopFile); err == nil {
			os.Remove(stopFile)
			fmt.Printf("\n⏸ batch-stop detected — stopping gracefully (%d done)\n", completed)
			progress.markStopped()
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

		progress.setRunning(next)

		slog.Info("batch feature", "feature", next, "status", "started", "completed", completed, "total", len(plan.Schedule))
		fmt.Printf("\n══════════════════════════════════════\n")
		fmt.Printf("  BATCH: %s (%d/%d done)\n", next, completed, len(plan.Schedule))
		fmt.Printf("══════════════════════════════════════\n\n")

		feature, err := ws.LoadFeature(next)
		if err != nil {
			fmt.Printf("  error loading feature: %v\n", err)
			statusMap[next] = feat.StatusBlocked
			failedFeatures[next]++
			continue
		}

		if err := ws.InitFeatureDir(next); err != nil {
			fmt.Printf("  init feature dir failed: %v\n", err)
			statusMap[next] = feat.StatusBlocked
			failedFeatures[next]++
			continue
		}

		// W4：套用 4x run 啟動前的 dependency gate；未完成則跳過並標記 blocked。
		depResult := guard.CheckDependencies(ws, next)
		if !depResult.Pass {
			fmt.Printf("  dependency check failed: %s\n", strings.Join(depResult.Errors, "; "))
			statusMap[next] = feat.StatusBlocked
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
			// F082：啟動前的 alive guard（對齊 4x run 的樣板）。若該 feature 已被
			// 另一個存活 process 執行中，跳過不啟動，避免兩個 process 並行跑同一
			// feature、互相覆蓋 state.json。batch 情境下以 graceful skip 處理，
			// 不終止整個 batch，讓排程繼續排其他 feature。
			// 排除本 process 自己的 PID：batch 在跑 feature 前會把自身 PID 寫入
			// state，重選同一 feature 時不能把自己誤判為「外部並行的 process」。
			if existing.Active && existing.Pid != os.Getpid() && protocol.ProcessAlive(existing.Pid) {
				fmt.Printf("  skipping %s: already running (pid %d)\n", next, existing.Pid)
				statusMap[next] = feat.StatusInProgress
				failedFeatures[next]++
				progress.update(statusMap)
				continue
			}
			s = existing
			s.Active = true
		}

		// W4：resume 既有 state 時，若已 done 則跳過不重跑。
		if s.Phase == protocol.PhaseDone {
			fmt.Printf("  %s already done — skipping\n", next)
			statusMap[next] = feat.StatusDone
			completed++
			progress.update(statusMap)
			continue
		}

		// W4：記錄本 process PID，與 4x run 一致。
		s.Pid = os.Getpid()
		_ = ws.WriteState(next, s)

		updatedStatus, runErr := runFeature(next, feature, s)
		statusMap[next] = updatedStatus

		slog.Info("batch feature", "feature", next, "status", "completed", "result", string(updatedStatus))

		// W12：跑出失敗狀態時累計，達上限後於 selection 跳過避免無限重跑。
		if updatedStatus == feat.StatusNeedsAttention || updatedStatus == feat.StatusBlocked || updatedStatus == feat.StatusInProgress {
			failedFeatures[next]++
		}

		if runErr != nil {
			fmt.Printf("  feature %s failed: %v\n", next, runErr)
		}

		// F068：feature 完成（ready-for-review/pending-review）後自動 merge 回 main，
		// 使下一個 feature 的 worktree 從含本輪改動的最新 main 開出 branch。
		if !noAutoMerge && updatedStatus == feat.StatusReadyForReview && autoMerge != nil {
			result := autoMerge(next)
			switch {
			case result.Conflict:
				// M2：衝突 → graceful pause。保留 pending-review 與 worktree、不計入 completed、停止主迴圈。
				slog.Error("auto-merge conflict", "feature", next, "files", result.Files, "repo", result.ConflictRepo)
				// S5：持久化衝突信號讓 dashboard 得知細節並提供 Continue Batch。
				if err := ws.WriteBatchConflict(protocol.BatchConflict{
					FeatureID:    next,
					FeatureName:  feature.Name,
					ConflictRepo: result.ConflictRepo,
					Files:        result.Files,
					DetectedAt:   time.Now().UTC(),
				}); err != nil {
					slog.Warn("failed to persist batch conflict", "feature", next, "error", err)
				}
				fmt.Printf("\n⏸ auto-merge conflict on %s — pausing batch (%d done):\n", next, completed)
				for _, file := range result.Files {
					fmt.Printf("  conflict: %s\n", file)
				}
				if result.ConflictRepo != "" {
					fmt.Printf("  repo: %s\n", result.ConflictRepo)
				}
				fmt.Printf("  worktree: %s\n", gitops.Dir(ws.Root, next))
				fmt.Printf("  resolve conflicts, then run '4x merge %s' and re-run '4x batch run' to continue.\n", next)
				progress.markStopped()
				progress.update(statusMap)
				return completed
			case result.Error != "":
				// M3：非衝突錯誤 → 警告後嘗試下一個 feature；feature 仍算 ready-for-review（batchCompleted）。
				slog.Error("auto-merge failed", "feature", next, "error", result.Error)
				fmt.Printf("  worktree preserved at: %s\n", gitops.Dir(ws.Root, next))
			default:
				// M1/M4：成功或 skipped（非 worktree 模式）→ 標記 done。
				slog.Info("auto-merge succeeded", "feature", next, "skipped", result.Skipped)
				statusMap[next] = feat.StatusDone
			}
		}

		if batchCompleted(updatedStatus) {
			completed++
		}

		progress.update(statusMap)
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

			stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
			if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "stop")
			fmt.Println("Stop signal sent — batch will finish current feature then exit.")
			return nil
		},
	}
}

func batchCompleted(s feat.Status) bool {
	return feat.BatchCompleted(s)
}
