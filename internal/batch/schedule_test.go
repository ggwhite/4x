package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// newScheduleTestWorkspace 建立一個含 .4x/ 的最小 workspace，供排程測試使用。
func newScheduleTestWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "schedule-test"},
		Default: "mock",
		Runners: map[string]protocol.RunnerConfig{"mock": {Command: "echo"}},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: "Test Feature", Status: "not-started"}); err != nil {
		t.Fatal(err)
	}
	return ws
}

// scheduleTestFeature 在 ws 建立一個 feature YAML，供 RunSchedule 測試使用。
func scheduleTestFeature(t *testing.T, ws *protocol.Workspace, id string, depends []string) {
	t.Helper()
	if err := ws.InitFeatureDir(id); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: id, Name: id, Status: "not-started", Depends: depends}); err != nil {
		t.Fatal(err)
	}
}

// AC-4 / S1：所有 feature 自然跑完後，報告 outcome=completed 且統計到完成數。
func TestBatchReport_S1CompletedAllDone(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)
	scheduleTestFeature(t, ws, "feat-b", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}
	progress := NewProgress(statusMap, protocol.BatchOutcomeCompleted)

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
	if completed != 2 {
		t.Fatalf("completed = %d, want 2", completed)
	}

	report := FinishReport(ws, plan, "mock", progress, "", "")
	if report.Outcome != protocol.BatchOutcomeCompleted {
		t.Errorf("outcome = %q, want completed", report.Outcome)
	}

	got, err := ws.ReadBatchReport()
	if err != nil || got == nil {
		t.Fatalf("ReadBatchReport: report=%v err=%v", got, err)
	}
	if got.Outcome != protocol.BatchOutcomeCompleted {
		t.Errorf("persisted outcome = %q, want completed", got.Outcome)
	}
	if got.Total != 2 || got.Completed != 2 {
		t.Errorf("total/completed = %d/%d, want 2/2", got.Total, got.Completed)
	}
}

// AC-4 / S1：使用者按 Stop（stop-file）後中途停下，報告 outcome=stopped 並反映已完成項。
func TestBatchReport_S1StoppedByStopFile(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}
	progress := NewProgress(statusMap, protocol.BatchOutcomeCompleted)

	// 在主迴圈開跑前放下 stop-file，迴圈第一輪即偵測到 → MarkStopped → break。
	if err := os.WriteFile(filepath.Join(ws.DotDir(), protocol.BatchStopFile), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stop-file: %v", err)
	}

	executed := 0
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed++
		return feature.StatusReadyForReview, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
	if executed != 0 {
		t.Fatalf("runFeature executed %d times, want 0 (stopped before run)", executed)
	}
	if completed != 0 {
		t.Fatalf("completed = %d, want 0", completed)
	}

	report := FinishReport(ws, plan, "mock", progress, "", "")
	if report.Outcome != protocol.BatchOutcomeStopped {
		t.Errorf("outcome = %q, want stopped", report.Outcome)
	}

	got, _ := ws.ReadBatchReport()
	if got == nil || got.Outcome != protocol.BatchOutcomeStopped {
		t.Errorf("persisted outcome = %v, want stopped", got)
	}
}

// F082：batch 啟動前的 alive guard — 若 feature 已被存活 process 執行中
// （Active=true 且 PID 存活），跳過不啟動，避免並行跑同一 feature；batch graceful
// skip，不終止整個排程。
func TestRunSchedule_SkipsAliveFeature(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-alive")
	scheduleTestFeature(t, ws, "feat-alive", nil)
	// 以 parent process PID 模擬「另一個存活 process 正在跑該 feature」
	//（須與 batch 自身 PID 不同，否則會被 self-PID 排除）。
	ws.WriteState("feat-alive", protocol.State{
		FeatureID: "feat-alive",
		Phase:     protocol.PhaseCoding,
		Active:    true,
		Pid:       os.Getppid(),
	})

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-alive"}}}
	statusMap := map[string]feature.Status{"feat-alive": "in-progress"}

	executed := 0
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed++
		return feature.StatusReadyForReview, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)
	if executed != 0 {
		t.Fatalf("runFeature executed %d times, want 0 (feature already running)", executed)
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0", completed)
	}
	if statusMap["feat-alive"] != feature.StatusInProgress {
		t.Errorf("statusMap[feat-alive] = %s, want in-progress", statusMap["feat-alive"])
	}
}

// AC-4 / S1 回歸測試：snapshot 殘留 runningFeature（末位 feature 走 error-continue 路徑後
// 未清空）時，S1（completed/stopped，outcomeOverride==""）的報告必須清空 RunningFeature——
// completed/stopped 報告語義上不該有「正在跑的 feature」。鎖住 round 3 deep-review 找到的回歸。
func TestBatchReport_S1ClearsStaleRunningFeature(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": feature.StatusBlocked}
	progress := NewProgress(statusMap, protocol.BatchOutcomeCompleted)
	// 模擬 error-continue 路徑：SetRunning 後未經 Update() 清空就退出迴圈，殘留 runningFeature。
	progress.SetRunning("feat-a")

	report := FinishReport(ws, plan, "mock", progress, "", "")
	if report.RunningFeature != "" {
		t.Fatalf("report running = %q, want empty (S1 completed must not carry running feature)", report.RunningFeature)
	}

	got, err := ws.ReadBatchReport()
	if err != nil || got == nil {
		t.Fatalf("ReadBatchReport: report=%v err=%v", got, err)
	}
	if got.RunningFeature != "" {
		t.Errorf("persisted runningFeature = %q, want empty", got.RunningFeature)
	}
}

// AC-5 / S2：SIGTERM/SIGINT 行程層 handler 從進度快照組出 outcome=interrupted 的報告，
// 記錄當下正在跑的 feature 與已完成項。此處測 builder + 快照組裝，不真送 signal。
func TestBatchReport_S2InterruptedSnapshot(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)
	scheduleTestFeature(t, ws, "feat-b", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	// feat-a 已完成、feat-b 正在跑時收到 signal。
	statusMap := map[string]feature.Status{
		"feat-a": feature.StatusDone,
		"feat-b": feature.StatusInProgress,
	}
	progress := NewProgress(statusMap, protocol.BatchOutcomeCompleted)
	progress.Update(statusMap, nil)
	progress.SetRunning("feat-b")

	// 直接呼叫 production 的 signal goroutine 收尾函式（outcome 覆寫為 interrupted）。
	report := FinishReport(ws, plan, "mock", progress, protocol.BatchOutcomeInterrupted, "")
	if report.RunningFeature != "feat-b" {
		t.Fatalf("report running = %q, want feat-b", report.RunningFeature)
	}

	got, err := ws.ReadBatchReport()
	if err != nil || got == nil {
		t.Fatalf("ReadBatchReport: report=%v err=%v", got, err)
	}
	if got.Outcome != protocol.BatchOutcomeInterrupted {
		t.Errorf("outcome = %q, want interrupted", got.Outcome)
	}
	if got.RunningFeature != "feat-b" {
		t.Errorf("runningFeature = %q, want feat-b", got.RunningFeature)
	}
	if got.Completed != 1 {
		t.Errorf("completed = %d, want 1 (feat-a done)", got.Completed)
	}
}

// AC-6 / S3：runFeature panic 時，行程層 defer recover 以進度快照寫出 outcome=crashed 的
// partial report，panicMessage 含 panic 文字，並保留當下正在跑的 feature。
func TestBatchReport_S3CrashedWritesPartial(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}
	progress := NewProgress(statusMap, protocol.BatchOutcomeCompleted)

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		panic("coder subprocess exploded")
	}

	// 走 production 的 recover 收尾路徑：攔截 panic 後呼叫 FinishReport 寫 crashed report，
	// 再 re-panic（此處測試僅驗證報告產出，故不轉拋）。
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate from runFeature")
			}
			FinishReport(ws, plan, "mock", progress, protocol.BatchOutcomeCrashed, fmt.Sprint(r))
		}()
		RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
	}()

	got, err := ws.ReadBatchReport()
	if err != nil || got == nil {
		t.Fatalf("ReadBatchReport: report=%v err=%v", got, err)
	}
	if got.Outcome != protocol.BatchOutcomeCrashed {
		t.Errorf("outcome = %q, want crashed", got.Outcome)
	}
	if !strings.Contains(got.PanicMessage, "coder subprocess exploded") {
		t.Errorf("panicMessage = %q, want it to contain panic text", got.PanicMessage)
	}
	if got.RunningFeature != "feat-a" {
		t.Errorf("runningFeature = %q, want feat-a (captured at crash)", got.RunningFeature)
	}
}

// W4 / AC-7：dependency 未完成的 feature 被 gate 擋下、標記 blocked、不進 runFeature。
func TestRunSchedule_DependencyGateBlocks(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", []string{"feat-b"})
	scheduleTestFeature(t, ws, "feat-b", nil) // B 未 done

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed = append(executed, next)
		return feature.StatusReadyForReview, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)
	if len(executed) != 0 {
		t.Fatalf("feat-a should not run with unmet dependency, executed: %v", executed)
	}
	if statusMap["feat-a"] != feature.StatusBlocked {
		t.Errorf("statusMap[feat-a] = %s, want blocked", statusMap["feat-a"])
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0", completed)
	}
}

// W4 / AC-8：resume 已 done 的 feature 時跳過不重跑；執行的 feature state 帶有本 process PID。
func TestRunSchedule_SkipsDoneAndSetsPid(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-done")
	scheduleTestFeature(t, ws, "feat-done", nil)
	ws.WriteState("feat-done", protocol.State{FeatureID: "feat-done", Phase: protocol.PhaseDone})

	scheduleTestFeature(t, ws, "feat-run", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-done"},
		{FeatureID: "feat-run"},
	}}
	statusMap := map[string]feature.Status{"feat-done": "not-started", "feat-run": "not-started"}

	var executed []string
	var gotPid int
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed = append(executed, next)
		if next == "feat-run" {
			gotPid = s.Pid
		}
		return feature.StatusReadyForReview, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)

	for _, e := range executed {
		if e == "feat-done" {
			t.Error("already-done feature should not be executed")
		}
	}
	if statusMap["feat-done"] != feature.StatusDone {
		t.Errorf("statusMap[feat-done] = %s, want done", statusMap["feat-done"])
	}
	if gotPid != os.Getpid() {
		t.Errorf("feat-run state Pid = %d, want %d", gotPid, os.Getpid())
	}
	st, _ := ws.ReadState("feat-run")
	if st.Pid != os.Getpid() {
		t.Errorf("persisted feat-run Pid = %d, want %d", st.Pid, os.Getpid())
	}
	if completed != 2 {
		t.Errorf("completed = %d, want 2", completed)
	}
}

// W12 / AC-16：對恆失敗的 feature，連續 2 次後跳過不再選中，batch 正常結束（不無限迴圈）。
func TestRunSchedule_FailureCapStopsRetry(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-fail")
	scheduleTestFeature(t, ws, "feat-fail", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-fail"}}}
	statusMap := map[string]feature.Status{"feat-fail": "not-started"}

	count := 0
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		count++
		return feature.StatusNeedsAttention, nil
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)
	if count != 2 {
		t.Fatalf("feat-fail ran %d times, want capped at 2", count)
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0", completed)
	}
}

// F068 / M1：feature 完成（ready-for-review）後 auto-merge 成功 → 標記 done 並計入 completed。
func TestRunSchedule_AutoMergeSuccessMarksDone(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}

	var merged []string
	autoMerge := func(featureID string) gitops.MergeResult {
		merged = append(merged, featureID)
		return gitops.MergeResult{} // 成功（無衝突、無錯誤）
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	if len(merged) != 1 || merged[0] != "feat-a" {
		t.Fatalf("autoMerge called with %v, want [feat-a]", merged)
	}
	if statusMap["feat-a"] != feature.StatusDone {
		t.Errorf("statusMap[feat-a] = %s, want done", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M4：非 worktree 模式 merge 被 skipped 時，仍視為成功標記 done。
func TestRunSchedule_AutoMergeSkippedMarksDone(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Skipped: true}
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	if statusMap["feat-a"] != feature.StatusDone {
		t.Errorf("statusMap[feat-a] = %s, want done", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M2：auto-merge 衝突 → graceful pause。feature 保持 ready-for-review、不計入 completed、
// 後續 feature 不執行（主迴圈停止）。
func TestRunSchedule_AutoMergeConflictPauses(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)
	scheduleTestFeature(t, ws, "feat-b", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed = append(executed, next)
		return feature.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}}
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	if len(executed) != 1 || executed[0] != "feat-a" {
		t.Fatalf("executed = %v, want only [feat-a] (paused before feat-b)", executed)
	}
	if statusMap["feat-a"] != feature.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (preserved)", statusMap["feat-a"])
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0 (conflict not counted)", completed)
	}
}

// F068 / M3：auto-merge 非衝突錯誤 → 警告後續跑下一個 feature；錯誤的 feature 仍算 completed。
func TestRunSchedule_AutoMergeErrorContinues(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)
	scheduleTestFeature(t, ws, "feat-b", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed = append(executed, next)
		return feature.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		if featureID == "feat-a" {
			return gitops.MergeResult{Error: "git push failed"}
		}
		return gitops.MergeResult{}
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	if len(executed) != 2 {
		t.Fatalf("executed = %v, want both features (error does not pause)", executed)
	}
	if statusMap["feat-a"] != feature.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (merge failed, preserved)", statusMap["feat-a"])
	}
	if statusMap["feat-b"] != feature.StatusDone {
		t.Errorf("statusMap[feat-b] = %s, want done", statusMap["feat-b"])
	}
	if completed != 2 {
		t.Errorf("completed = %d, want 2 (both count: ready-for-review + done)", completed)
	}
}

// F068：--no-auto-merge → 完全不呼叫 autoMerge，feature 停在 ready-for-review（舊行為）但仍計入 completed。
func TestRunSchedule_NoAutoMergeSkipsMerge(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}
	called := false
	autoMerge := func(featureID string) gitops.MergeResult {
		called = true
		return gitops.MergeResult{}
	}

	completed := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, autoMerge, nil)

	if called {
		t.Error("autoMerge should not be called when noAutoMerge is true")
	}
	if statusMap["feat-a"] != feature.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (no auto-merge)", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M6：衝突暫停後 resume——人工解完衝突跑 4x merge（feat-a 標 done）後重跑 batch，
// 應跳過已完成的 feat-a、繼續執行並 merge feat-b。
func TestRunSchedule_ResumeAfterConflict(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)
	scheduleTestFeature(t, ws, "feat-b", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}

	// 第一輪：feat-a auto-merge 衝突 → pause，feat-b 未跑。
	conflictMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}}
	}
	completed1 := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, conflictMerge, nil)
	if completed1 != 0 {
		t.Fatalf("first run completed = %d, want 0 (paused at conflict)", completed1)
	}
	if statusMap["feat-b"] != "not-started" {
		t.Fatalf("feat-b should not have run in first batch, status = %s", statusMap["feat-b"])
	}

	// 人工解衝突 + 4x merge feat-a → 標記 done（模擬 resume 前置）。
	statusMap["feat-a"] = feature.StatusDone

	// 第二輪：重跑 batch，feat-a 已 done 跳過，feat-b 正常 auto-merge 成功。
	var merged []string
	successMerge := func(featureID string) gitops.MergeResult {
		merged = append(merged, featureID)
		return gitops.MergeResult{}
	}
	completed2 := RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, successMerge, nil)

	if len(merged) != 1 || merged[0] != "feat-b" {
		t.Fatalf("second run merged %v, want [feat-b] (feat-a already done)", merged)
	}
	if statusMap["feat-b"] != feature.StatusDone {
		t.Errorf("statusMap[feat-b] = %s, want done", statusMap["feat-b"])
	}
	if completed2 != 1 {
		t.Errorf("second run completed = %d, want 1", completed2)
	}
}

// AC-7：auto-merge 衝突 graceful pause 時寫出 .4x/batch-conflict.json（含 featureId 與 files）。
func TestRunSchedule_ConflictWritesSignal(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}, ConflictRepo: "core"}
	}

	RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	conflict, err := ws.ReadBatchConflict()
	if err != nil {
		t.Fatalf("ReadBatchConflict: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected batch-conflict.json after conflict pause, got nil")
	}
	if conflict.FeatureID != "feat-a" {
		t.Errorf("conflict.FeatureID = %s, want feat-a", conflict.FeatureID)
	}
	if len(conflict.Files) != 1 || conflict.Files[0] != "main.go" {
		t.Errorf("conflict.Files = %v, want [main.go]", conflict.Files)
	}
	if conflict.ConflictRepo != "core" {
		t.Errorf("conflict.ConflictRepo = %s, want core", conflict.ConflictRepo)
	}
}

// AC-8：成功 merge（無衝突）時清掉殘留的衝突信號，不留下 batch-conflict.json。
func TestRunSchedule_SuccessClearsStaleSignal(t *testing.T) {
	ws := newScheduleTestWorkspace(t, "feat-a")
	scheduleTestFeature(t, ws, "feat-a", nil)

	// 模擬上一輪殘留的衝突信號。
	if err := ws.WriteBatchConflict(protocol.BatchConflict{FeatureID: "old"}); err != nil {
		t.Fatalf("WriteBatchConflict: %v", err)
	}

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{}
	}

	RunSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge, nil)

	if conflict, _ := ws.ReadBatchConflict(); conflict != nil {
		t.Errorf("stale conflict signal should be cleared, got %+v", *conflict)
	}
}
