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
				if f.Status != feat.StatusDone && f.Status != feat.StatusAbandoned {
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

// batchRunConfig 收納 batch run 子指令的設定與依賴，供 executeBatchFeature / mergeBatchFeature
// 作為 receiver 使用，取代原本在 RunE closure 內捕獲多個外部變數的 inline closure。
type batchRunConfig struct {
	ws           *protocol.Workspace
	cfg          protocol.Config
	runnerName   string
	manualRunner string
	timeout      int
}

// executeBatchFeature 執行單一 feature 的完整 runLoop（含 worktree 隔離），回傳最新 status。
func (bc *batchRunConfig) executeBatchFeature(next string, feature feat.Feature, s protocol.State) (feat.Status, error) {
	signal.Ignore(syscall.SIGPIPE)
	batchCtx, batchCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer batchCancel()
	batchOps := gitops.New(bc.ws.Root, bc.ws, bc.cfg)

	batchRunnerWs := bc.ws
	commitStrategy := "never"
	if bc.cfg.Isolation == "worktree" {
		wtPath, wtErr := batchOps.SetupWorktree(next, feature.Repos)
		if wtErr != nil {
			return feat.StatusBlocked, fmt.Errorf("worktree setup failed: %w", wtErr)
		}
		batchRunnerWs = &protocol.Workspace{Root: wtPath}
		commitStrategy = "per-round"
	}

	runnerFactory := func(rn string, logPath string, model string) runner.Runner {
		return runner.NewRunner(batchRunnerWs, rn, bc.cfg.Runners[rn], time.Duration(bc.timeout)*time.Second, logPath, model)
	}
	runErr := runLoop(batchCtx, bc.ws, batchRunnerWs, feature, bc.cfg, s, batchOps, runnerFactory, commitStrategy, bc.manualRunner, nil)

	updated, _ := bc.ws.LoadFeature(next)
	return updated.Status, runErr
}

// mergeBatchFeature 對 ready-for-review 的 feature 走 done.go 的共用 helper，
// 回傳 MergeResult 供 handleAutoMerge 決定衝突暫停 / 錯誤續跑 / 成功標 done。
func (bc *batchRunConfig) mergeBatchFeature(featureID string) gitops.MergeResult {
	st, err := bc.ws.ReadState(featureID)
	if err != nil {
		return gitops.MergeResult{Error: fmt.Sprintf("cannot read state for %s: %v", featureID, err)}
	}
	if guard.SelfModNeedsApproval(st, false) {
		slog.Warn("self-mod guard: protected paths touched, auto-merge blocked — use 4x done --approve-self-mod",
			"feature", featureID, "paths", st.SelfModPaths)
		return gitops.MergeResult{Error: "self-mod: protected paths require manual --approve-self-mod"}
	}
	f, _ := bc.ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	return autoMergeFeature(bc.ws, bc.cfg, featureID, name)
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

			_ = ws.WriteBatchPID(os.Getpid())
			defer func() { _ = ws.ClearBatchPID() }()

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

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

			progress := &batchProgress{
				startedAt: time.Now(),
				statusMap: maps.Clone(statusMap),
				outcome:   protocol.BatchOutcomeCompleted,
			}

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

			defer func() {
				if r := recover(); r != nil {
					finishBatchReport(ws, plan, runnerName, progress, protocol.BatchOutcomeCrashed, fmt.Sprint(r))
					panic(r)
				}
			}()

			bc := &batchRunConfig{
				ws:           ws,
				cfg:          cfg,
				runnerName:   runnerName,
				manualRunner: manualRunner,
				timeout:      timeout,
			}

			slog.Info("batch operation", "action", "run", "features", len(plan.Schedule), "runner", runnerName)
			completed := runBatchSchedule(ws, plan, statusMap, maxRounds, runnerName, bc.executeBatchFeature, noAutoMerge, bc.mergeBatchFeature, progress)

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

const maxFeatureFailures = 2

type batchProgress struct {
	mu             sync.Mutex
	startedAt      time.Time
	statusMap      map[string]feat.Status
	failReasons    map[string]string
	runningFeature string
	outcome        string
}

func (p *batchProgress) setRunning(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = id
}

func (p *batchProgress) update(statusMap map[string]feat.Status, failReasons map[string]string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = ""
	p.statusMap = maps.Clone(statusMap)
	p.failReasons = maps.Clone(failReasons)
}

func (p *batchProgress) markStopped() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outcome = protocol.BatchOutcomeStopped
}

func (p *batchProgress) snapshot() (startedAt time.Time, runningFeature, outcome string, statusMap map[string]feat.Status, failReasons map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt, p.runningFeature, p.outcome, maps.Clone(p.statusMap), maps.Clone(p.failReasons)
}

func finishBatchReport(ws *protocol.Workspace, plan *batch.BatchPlan, runnerName string,
	progress *batchProgress, outcomeOverride, panicMsg string) protocol.BatchReport {
	startedAt, running, outcome, sm, fr := progress.snapshot()
	if outcomeOverride != "" {
		outcome = outcomeOverride
	} else {
		running = ""
	}
	report := batch.BuildBatchReport(ws, plan, sm, fr, runnerName, startedAt, time.Now(), outcome, running, panicMsg)
	if err := ws.WriteBatchReport(report); err != nil {
		slog.Warn("failed to write batch report", "outcome", outcome, "error", err)
	}
	return report
}

// batchTracker 追蹤每個 feature 的失敗次數與原因，供 selectNextFeature 跳過達上限的 feature。
type batchTracker struct {
	failedFeatures map[string]int
	failReasons    map[string]string
	loggedSkip     map[string]bool
}

func newBatchTracker() *batchTracker {
	return &batchTracker{
		failedFeatures: map[string]int{},
		failReasons:    map[string]string{},
		loggedSkip:     map[string]bool{},
	}
}

func (t *batchTracker) recordFailure(featureID, reason string) {
	t.failedFeatures[featureID]++
	t.failReasons[featureID] = reason
}

func (t *batchTracker) recordFailureNoReason(featureID string) {
	t.failedFeatures[featureID]++
}

// selectNextFeature 從 plan 的 schedule 中選出下一個可執行的 feature：跳過已完成、已達失敗
// 上限、依賴未滿足的 feature，回傳第一個符合條件的 feature ID；全部不可選時回傳空字串。
func selectNextFeature(plan *batch.BatchPlan, statusMap map[string]feat.Status, tracker *batchTracker) string {
	for _, s := range plan.Schedule {
		if batchCompleted(statusMap[s.FeatureID]) {
			continue
		}
		if tracker.failedFeatures[s.FeatureID] >= maxFeatureFailures {
			if !tracker.loggedSkip[s.FeatureID] {
				fmt.Printf("  skipping %s: failed %d times\n", s.FeatureID, tracker.failedFeatures[s.FeatureID])
				tracker.loggedSkip[s.FeatureID] = true
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
			return s.FeatureID
		}
	}
	return ""
}

// prepareFeatureState 載入 feature、初始化目錄、檢查依賴、建構或恢復 state。
// 回傳 (feature, state, error)：error 非 nil 時呼叫端應跳過並記錄失敗；
// skipCompleted 為 true 時表示 feature 已 done，呼叫端直接計入 completed。
type featurePrep struct {
	feature       feat.Feature
	state         protocol.State
	skipCompleted bool
	skipAlive     bool
}

func prepareFeatureState(ws *protocol.Workspace, featureID string, maxRounds int, runnerName string,
	statusMap map[string]feat.Status, tracker *batchTracker, progress *batchProgress) (*featurePrep, error) {

	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		reason := fmt.Sprintf("error loading feature: %v", err)
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feat.StatusBlocked
		tracker.recordFailure(featureID, reason)
		return nil, fmt.Errorf("skip")
	}

	if err := ws.InitFeatureDir(featureID); err != nil {
		reason := fmt.Sprintf("init feature dir failed: %v", err)
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feat.StatusBlocked
		tracker.recordFailure(featureID, reason)
		return nil, fmt.Errorf("skip")
	}

	depResult := guard.CheckDependencies(ws, featureID)
	if !depResult.Pass {
		reason := "dependency check failed: " + strings.Join(depResult.Errors, "; ")
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feat.StatusBlocked
		tracker.recordFailure(featureID, reason)
		return nil, fmt.Errorf("skip")
	}

	rounds := maxRounds
	if rounds <= 0 {
		rounds = 5
	}

	s := protocol.State{
		FeatureID: featureID,
		Phase:     protocol.PhaseInit,
		MaxRounds: rounds,
		Active:    true,
		Runner:    runnerName,
		CreatedAt: time.Now(),
	}
	if existing, err := ws.ReadState(featureID); err == nil {
		if existing.Active && existing.Pid != os.Getpid() && protocol.ProcessAlive(existing.Pid) {
			fmt.Printf("  skipping %s: already running (pid %d)\n", featureID, existing.Pid)
			statusMap[featureID] = feat.StatusInProgress
			tracker.recordFailureNoReason(featureID)
			progress.update(statusMap, tracker.failReasons)
			return &featurePrep{skipAlive: true}, nil
		}
		s = existing
		s.Active = true
	}

	if s.Phase == protocol.PhaseDone {
		fmt.Printf("  %s already done — skipping\n", featureID)
		statusMap[featureID] = feat.StatusDone
		progress.update(statusMap, tracker.failReasons)
		return &featurePrep{skipCompleted: true}, nil
	}

	s.Pid = os.Getpid()
	_ = ws.WriteState(featureID, s)

	return &featurePrep{feature: feature, state: s}, nil
}

// mergeAction 表示 handleAutoMerge 的決策結果。
type mergeAction int

const (
	mergeActionNone     mergeAction = iota // 不需 merge 或 merge 成功
	mergeActionConflict                    // 衝突，應暫停 batch
	mergeActionError                       // 非衝突錯誤，警告後繼續
)

// handleAutoMerge 處理 feature 完成後的自動 merge，回傳決策讓主迴圈決定暫停或繼續。
func handleAutoMerge(ws *protocol.Workspace, featureID string, feature feat.Feature,
	statusMap map[string]feat.Status, completed int,
	autoMerge func(featureID string) gitops.MergeResult, progress *batchProgress,
	tracker *batchTracker) mergeAction {

	result := autoMerge(featureID)

	switch {
	case result.Conflict:
		slog.Error("auto-merge conflict", "feature", featureID, "files", result.Files, "repo", result.ConflictRepo)
		_ = ws.WriteBatchConflict(protocol.BatchConflict{
			FeatureID:    featureID,
			FeatureName:  feature.Name,
			ConflictRepo: result.ConflictRepo,
			Files:        result.Files,
			DetectedAt:   time.Now().UTC(),
		})
		fmt.Printf("\n⏸ auto-merge conflict on %s — pausing batch (%d done):\n", featureID, completed)
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		if result.ConflictRepo != "" {
			fmt.Printf("  repo: %s\n", result.ConflictRepo)
		}
		fmt.Printf("  worktree: %s\n", gitops.Dir(ws.Root, featureID))
		fmt.Printf("  resolve conflicts, then run '4x merge %s' and re-run '4x batch run' to continue.\n", featureID)
		progress.markStopped()
		progress.update(statusMap, tracker.failReasons)
		return mergeActionConflict

	case result.Error != "":
		slog.Error("auto-merge failed", "feature", featureID, "error", result.Error)
		fmt.Printf("  worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return mergeActionError

	default:
		slog.Info("auto-merge succeeded", "feature", featureID, "skipped", result.Skipped)
		statusMap[featureID] = feat.StatusDone
		return mergeActionNone
	}
}

func runBatchSchedule(ws *protocol.Workspace, plan *batch.BatchPlan, statusMap map[string]feat.Status,
	maxRounds int, runnerName string,
	runFeature func(next string, feature feat.Feature, s protocol.State) (feat.Status, error),
	noAutoMerge bool, autoMerge func(featureID string) gitops.MergeResult, progress *batchProgress) int {

	stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
	if err := ws.ClearBatchConflict(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to clear stale batch conflict: %v\n", err)
	}

	completed := 0
	tracker := newBatchTracker()

	for {
		if _, err := os.Stat(stopFile); err == nil {
			os.Remove(stopFile)
			fmt.Printf("\n⏸ batch-stop detected — stopping gracefully (%d done)\n", completed)
			progress.markStopped()
			break
		}

		next := selectNextFeature(plan, statusMap, tracker)
		if next == "" {
			break
		}

		progress.setRunning(next)

		slog.Info("batch feature", "feature", next, "status", "started", "completed", completed, "total", len(plan.Schedule))
		fmt.Printf("\n══════════════════════════════════════\n")
		fmt.Printf("  BATCH: %s (%d/%d done)\n", next, completed, len(plan.Schedule))
		fmt.Printf("══════════════════════════════════════\n\n")

		prep, err := prepareFeatureState(ws, next, maxRounds, runnerName, statusMap, tracker, progress)
		if err != nil {
			continue
		}
		if prep.skipAlive {
			continue
		}
		if prep.skipCompleted {
			completed++
			continue
		}

		updatedStatus, runErr := runFeature(next, prep.feature, prep.state)
		statusMap[next] = updatedStatus

		slog.Info("batch feature", "feature", next, "status", "completed", "result", string(updatedStatus))

		if updatedStatus == feat.StatusNeedsAttention || updatedStatus == feat.StatusBlocked || updatedStatus == feat.StatusInProgress {
			tracker.recordFailureNoReason(next)
		}

		if runErr != nil {
			fmt.Printf("  feature %s failed: %v\n", next, runErr)
		}

		if !noAutoMerge && updatedStatus == feat.StatusReadyForReview && autoMerge != nil {
			action := handleAutoMerge(ws, next, prep.feature, statusMap, completed, autoMerge, progress, tracker)
			if action == mergeActionConflict {
				return completed
			}
		}

		if batchCompleted(updatedStatus) {
			completed++
		}

		progress.update(statusMap, tracker.failReasons)
	}

	return completed
}

func newBatchStopCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Signal batch to stop after current feature completes",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if err := ws.WriteBatchStop(); err != nil {
				return err
			}
			slog.Info("batch operation", "action", "stop")
			if jsonOutput {
				return printJSON(struct {
					Stopped bool `json:"stopped"`
				}{true})
			}
			fmt.Println("Stop signal sent — batch will finish current feature then exit.")
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func batchCompleted(s feat.Status) bool {
	return feat.BatchCompleted(s)
}
