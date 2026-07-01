package batch

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
)

// MaxFeatureFailures 是同一 feature 連續失敗達此次數後即跳過，避免無限重試空轉。
const MaxFeatureFailures = 2

// Tracker 追蹤每個 feature 的失敗次數與原因，供 SelectNext 跳過達上限的 feature。
type Tracker struct {
	FailedFeatures map[string]int
	FailReasons    map[string]string
	loggedSkip     map[string]bool
}

// NewTracker 建立空的 Tracker。
func NewTracker() *Tracker {
	return &Tracker{
		FailedFeatures: map[string]int{},
		FailReasons:    map[string]string{},
		loggedSkip:     map[string]bool{},
	}
}

// RecordFailure 記錄一次帶原因的失敗。
func (t *Tracker) RecordFailure(featureID, reason string) {
	t.FailedFeatures[featureID]++
	t.FailReasons[featureID] = reason
}

// RecordFailureNoReason 記錄一次不帶原因的失敗（如 feature 執行後狀態仍需人工處理）。
func (t *Tracker) RecordFailureNoReason(featureID string) {
	t.FailedFeatures[featureID]++
}

// SelectNext 從 plan 的 schedule 中選出下一個可執行的 feature：跳過已完成、已達失敗
// 上限、依賴未滿足的 feature，回傳第一個符合條件的 feature ID；全部不可選時回傳空字串。
func SelectNext(plan *BatchPlan, statusMap map[string]feature.Status, tracker *Tracker) string {
	for _, s := range plan.Schedule {
		if feature.BatchCompleted(statusMap[s.FeatureID]) {
			continue
		}
		if tracker.FailedFeatures[s.FeatureID] >= MaxFeatureFailures {
			if !tracker.loggedSkip[s.FeatureID] {
				fmt.Printf("  skipping %s: failed %d times\n", s.FeatureID, tracker.FailedFeatures[s.FeatureID])
				tracker.loggedSkip[s.FeatureID] = true
			}
			continue
		}
		allDone := true
		for _, dep := range s.CanStartAfter {
			if !feature.BatchCompleted(statusMap[dep]) {
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

// FeaturePrep 是 PrepareFeatureState 的回傳結果：error 非 nil 時呼叫端應跳過並記錄失敗；
// SkipCompleted 為 true 時表示 feature 已 done，呼叫端直接計入 completed；SkipAlive 為 true
// 時表示 feature 正被另一存活 process 執行中，呼叫端應跳過不重跑。
type FeaturePrep struct {
	Feature       feature.Feature
	State         protocol.State
	SkipCompleted bool
	SkipAlive     bool
}

// PrepareFeatureState 載入 feature、初始化目錄、檢查依賴、建構或恢復 state。
func PrepareFeatureState(ws *protocol.Workspace, featureID string, maxRounds int, runnerName string,
	statusMap map[string]feature.Status, tracker *Tracker, progress *Progress) (*FeaturePrep, error) {

	f, err := ws.LoadFeature(featureID)
	if err != nil {
		reason := fmt.Sprintf("error loading feature: %v", err)
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feature.StatusBlocked
		tracker.RecordFailure(featureID, reason)
		return nil, fmt.Errorf("skip")
	}

	if err := ws.InitFeatureDir(featureID); err != nil {
		reason := fmt.Sprintf("init feature dir failed: %v", err)
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feature.StatusBlocked
		tracker.RecordFailure(featureID, reason)
		return nil, fmt.Errorf("skip")
	}

	depResult := guard.CheckDependencies(ws, featureID)
	if !depResult.Pass {
		reason := "dependency check failed: " + strings.Join(depResult.Errors, "; ")
		fmt.Printf("  %s\n", reason)
		statusMap[featureID] = feature.StatusBlocked
		tracker.RecordFailure(featureID, reason)
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
			statusMap[featureID] = feature.StatusInProgress
			tracker.RecordFailureNoReason(featureID)
			progress.Update(statusMap, tracker.FailReasons)
			return &FeaturePrep{SkipAlive: true}, nil
		}
		s = existing
		s.Active = true
	}

	if s.Phase == protocol.PhaseDone {
		fmt.Printf("  %s already done — skipping\n", featureID)
		statusMap[featureID] = feature.StatusDone
		progress.Update(statusMap, tracker.FailReasons)
		return &FeaturePrep{SkipCompleted: true}, nil
	}

	s.Pid = os.Getpid()
	if err := ws.WriteState(featureID, s); err != nil {
		slog.Warn("failed to write state during batch prep", "feature", featureID, "error", err)
	}

	return &FeaturePrep{Feature: f, State: s}, nil
}

// MergeAction 表示 HandleAutoMerge 的決策結果。
type MergeAction int

const (
	MergeActionNone     MergeAction = iota // 不需 merge 或 merge 成功
	MergeActionConflict                    // 衝突，應暫停 batch
	MergeActionError                       // 非衝突錯誤，警告後繼續
)

// HandleAutoMerge 處理 feature 完成後的自動 merge，回傳決策讓主迴圈決定暫停或繼續。
func HandleAutoMerge(ws *protocol.Workspace, featureID string, f feature.Feature,
	statusMap map[string]feature.Status, completed int,
	autoMerge func(featureID string) gitops.MergeResult, progress *Progress,
	tracker *Tracker) MergeAction {

	result := autoMerge(featureID)

	switch {
	case result.Conflict:
		slog.Error("auto-merge conflict", "feature", featureID, "files", result.Files, "repo", result.ConflictRepo)
		if err := ws.WriteBatchConflict(protocol.BatchConflict{
			FeatureID:    featureID,
			FeatureName:  f.Name,
			ConflictRepo: result.ConflictRepo,
			Files:        result.Files,
			DetectedAt:   time.Now().UTC(),
		}); err != nil {
			slog.Warn("failed to write batch conflict", "feature", featureID, "error", err)
		}
		fmt.Printf("\n⏸ auto-merge conflict on %s — pausing batch (%d done):\n", featureID, completed)
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		if result.ConflictRepo != "" {
			fmt.Printf("  repo: %s\n", result.ConflictRepo)
		}
		fmt.Printf("  worktree: %s\n", gitops.Dir(ws.Root, featureID))
		fmt.Printf("  resolve conflicts, then run '4x merge %s' and re-run '4x batch run' to continue.\n", featureID)
		progress.MarkStopped()
		progress.Update(statusMap, tracker.FailReasons)
		return MergeActionConflict

	case result.Error != "":
		slog.Error("auto-merge failed", "feature", featureID, "error", result.Error)
		fmt.Printf("  worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return MergeActionError

	default:
		slog.Info("auto-merge succeeded", "feature", featureID, "skipped", result.Skipped)
		statusMap[featureID] = feature.StatusDone
		return MergeActionNone
	}
}

// RunSchedule 依 plan 的排程逐一執行可執行的 feature，直到全部完成、遇 batch-stop 檔、
// 或因 auto-merge 衝突而暫停。回傳完成的 feature 數。
func RunSchedule(ws *protocol.Workspace, plan *BatchPlan, statusMap map[string]feature.Status,
	maxRounds int, runnerName string,
	runFeature func(next string, f feature.Feature, s protocol.State) (feature.Status, error),
	noAutoMerge bool, autoMerge func(featureID string) gitops.MergeResult, progress *Progress) int {

	stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
	if err := ws.ClearBatchConflict(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to clear stale batch conflict: %v\n", err)
	}

	completed := 0
	tracker := NewTracker()

	for {
		if _, err := os.Stat(stopFile); err == nil {
			os.Remove(stopFile)
			fmt.Printf("\n⏸ batch-stop detected — stopping gracefully (%d done)\n", completed)
			progress.MarkStopped()
			break
		}

		next := SelectNext(plan, statusMap, tracker)
		if next == "" {
			break
		}

		progress.SetRunning(next)

		slog.Info("batch feature", "feature", next, "status", "started", "completed", completed, "total", len(plan.Schedule))
		fmt.Printf("\n══════════════════════════════════════\n")
		fmt.Printf("  BATCH: %s (%d/%d done)\n", next, completed, len(plan.Schedule))
		fmt.Printf("══════════════════════════════════════\n\n")

		prep, err := PrepareFeatureState(ws, next, maxRounds, runnerName, statusMap, tracker, progress)
		if err != nil {
			continue
		}
		if prep.SkipAlive {
			continue
		}
		if prep.SkipCompleted {
			completed++
			continue
		}

		updatedStatus, runErr := runFeature(next, prep.Feature, prep.State)
		statusMap[next] = updatedStatus

		slog.Info("batch feature", "feature", next, "status", "completed", "result", string(updatedStatus))

		if updatedStatus == feature.StatusNeedsAttention || updatedStatus == feature.StatusBlocked || updatedStatus == feature.StatusInProgress {
			tracker.RecordFailureNoReason(next)
		}

		if runErr != nil {
			fmt.Printf("  feature %s failed: %v\n", next, runErr)
		}

		if !noAutoMerge && updatedStatus == feature.StatusReadyForReview && autoMerge != nil {
			action := HandleAutoMerge(ws, next, prep.Feature, statusMap, completed, autoMerge, progress, tracker)
			if action == MergeActionConflict {
				return completed
			}
		}

		if feature.BatchCompleted(updatedStatus) {
			completed++
		}

		progress.Update(statusMap, tracker.FailReasons)
	}

	return completed
}

// Progress 收納 batch run 執行期間的進度快照，供 dashboard/report 讀取。
type Progress struct {
	mu             sync.Mutex
	startedAt      time.Time
	statusMap      map[string]feature.Status
	failReasons    map[string]string
	runningFeature string
	outcome        string
}

// NewProgress 建立一個以現有 statusMap 快照為初始狀態的 Progress。
func NewProgress(statusMap map[string]feature.Status, outcome string) *Progress {
	return &Progress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   outcome,
	}
}

// SetRunning 標記目前正在執行的 feature。
func (p *Progress) SetRunning(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = id
}

// Update 以最新 statusMap/failReasons 更新快照，並清空 runningFeature。
func (p *Progress) Update(statusMap map[string]feature.Status, failReasons map[string]string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runningFeature = ""
	p.statusMap = maps.Clone(statusMap)
	p.failReasons = maps.Clone(failReasons)
}

// MarkStopped 將 outcome 標為 stopped（收到 batch-stop 訊號）。
func (p *Progress) MarkStopped() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outcome = protocol.BatchOutcomeStopped
}

// Snapshot 回傳目前進度的一致性快照。
func (p *Progress) Snapshot() (startedAt time.Time, runningFeature, outcome string, statusMap map[string]feature.Status, failReasons map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt, p.runningFeature, p.outcome, maps.Clone(p.statusMap), maps.Clone(p.failReasons)
}

// FinishReport 從 Progress 快照組出 BatchReport 並寫檔，回傳組好的 report。
func FinishReport(ws *protocol.Workspace, plan *BatchPlan, runnerName string,
	progress *Progress, outcomeOverride, panicMsg string) protocol.BatchReport {
	startedAt, running, outcome, sm, fr := progress.Snapshot()
	if outcomeOverride != "" {
		outcome = outcomeOverride
	} else {
		running = ""
	}
	report := BuildBatchReport(ws, plan, sm, fr, runnerName, startedAt, time.Now(), outcome, running, panicMsg)
	if err := ws.WriteBatchReport(report); err != nil {
		slog.Warn("failed to write batch report", "outcome", outcome, "error", err)
	}
	return report
}
