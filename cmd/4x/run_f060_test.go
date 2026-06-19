package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// W1 / AC-2：連續 review 失敗計數持平時 ConsecutiveNoProgress 每輪 +1，
// 累積到 3 後 run loop 轉 needs-attention 並停止（不跑滿 maxRounds）。
func TestRunLoop_NoProgressStopsBeforeMaxRounds(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 10, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	// 每輪 review 都 FAIL 且 critical 數持平（2）→ 無進展。
	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string, string) runner.Runner { return mock }, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive after no-progress stop")
	}
	if final.StopReason != "no-progress" {
		t.Errorf("stopReason = %q, want 'no-progress'", final.StopReason)
	}
	if !strings.Contains(final.StopMessage, "no progress") {
		t.Errorf("stopMessage = %q, want 'no progress'", final.StopMessage)
	}
	if final.ConsecutiveNoProgress != 3 {
		t.Errorf("ConsecutiveNoProgress = %d, want 3", final.ConsecutiveNoProgress)
	}
	if final.Round >= final.MaxRounds {
		t.Errorf("round = %d reached maxRounds = %d, expected earlier no-progress stop", final.Round, final.MaxRounds)
	}
}

// W1 / AC-1：某輪失敗計數下降（改善）時 ConsecutiveNoProgress reset 為 0，
// 使原本會在第 3 輪停止的流程得以存活並最終完成。
func TestRunLoop_ImprovementResetsNoProgress(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 10, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	// r1 FAIL(2) 基準 → r2 FAIL(2) CNP=1 → r3 FAIL(1) 改善 reset CNP=0 →
	// r4 FAIL(1) CNP=1 → r5 FAIL(1) CNP=2 → r6 PASS 完成。
	// 若無 r3 的 reset，CNP 會在 r4 達 3 而提前停止，永遠走不到 pending-review。
	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2}, {},
		{reviewVerdict: "FAIL", criticalIssues: 2}, {},
		{reviewVerdict: "FAIL", criticalIssues: 1}, {},
		{reviewVerdict: "FAIL", criticalIssues: 1}, {},
		{reviewVerdict: "FAIL", criticalIssues: 1}, {},
		{reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string, string) runner.Runner { return mock }, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review (improvement should have reset no-progress)", final.Phase)
	}
	if final.ConsecutiveNoProgress != 2 {
		t.Errorf("ConsecutiveNoProgress = %d, want 2", final.ConsecutiveNoProgress)
	}
	if final.LastFailCount != 1 {
		t.Errorf("LastFailCount = %d, want 1", final.LastFailCount)
	}
}

// W1 邊界：review verdict 為 FAIL 但失敗計數恆為 0（report 格式異常）時，
// 「首輪建立基準」判斷不得因 LastFailCount 一直停在 0 而每輪重複成立，
// ConsecutiveNoProgress 仍須遞增並在累積到 3 後停止（cur > 0 守衛）。
func TestRunLoop_ZeroFailCountStillStops(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 10, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	// 每輪 review 都 FAIL 但 criticalIssues=0 → reviewFailCount 恆為 0。
	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {},
		{reviewVerdict: "FAIL", criticalIssues: 0}, {},
		{reviewVerdict: "FAIL", criticalIssues: 0}, {},
		{reviewVerdict: "FAIL", criticalIssues: 0}, {},
		{reviewVerdict: "FAIL", criticalIssues: 0},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string, string) runner.Runner { return mock }, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.ConsecutiveNoProgress != 3 {
		t.Errorf("ConsecutiveNoProgress = %d, want 3", final.ConsecutiveNoProgress)
	}
	if final.Round >= final.MaxRounds {
		t.Errorf("round = %d reached maxRounds = %d, expected earlier no-progress stop", final.Round, final.MaxRounds)
	}
}

// W2 / AC-4：run loop 主迴圈遇到 phase == abandoned 時立即 break，不呼叫 runner。
func TestRunLoop_AbandonedBreaksWithoutRunner(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseAbandoned,
		Round: 1, MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1"}
	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string, string) runner.Runner { return mock }, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	if len(mock.phases) != 0 {
		t.Errorf("runner should not be invoked for abandoned feature, ran phases: %v", mock.phases)
	}
}

// W10 / AC-14：designing 完成後缺 acceptance-criteria.md → needs-attention；兩者皆有才回 coding。
func TestNextPhaseAfter_DesigningRequiresCriteria(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-d")
	featureDir := ws.FeatureDir("feat-d")
	os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# brief"), 0o644)

	s := protocol.State{FeatureID: "feat-d", Phase: protocol.PhaseDesigning}

	next, _, reason := nextPhaseAfter(ws, "feat-d", s)
	if next != protocol.PhaseNeedsAttention {
		t.Errorf("with only task-brief: next = %s, want needs-attention", next)
	}
	if !strings.Contains(reason, protocol.Criteria) {
		t.Errorf("reason = %q, want mention of %s", reason, protocol.Criteria)
	}

	os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# criteria"), 0o644)
	next2, role2, _ := nextPhaseAfter(ws, "feat-d", s)
	if next2 != protocol.PhaseDesignReviewing || role2 != protocol.RoleDesignReviewer {
		t.Errorf("with both artifacts: next=%s role=%s, want design-reviewing/design-reviewer", next2, role2)
	}
}

func TestNextPhaseAfter_DesignReviewingVerdict(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-dr")
	s := protocol.State{FeatureID: "feat-dr", Phase: protocol.PhaseDesignReviewing, Round: 0}

	writeFeatureFile(t, ws, "feat-dr", protocol.DesignReviewReport, "# Design Review\n\n## Verdict\nPASS\n")
	next, role, reason := nextPhaseAfter(ws, "feat-dr", s)
	if next != protocol.PhaseCoding || role != protocol.RoleCoder || reason != "" {
		t.Fatalf("PASS: got next=%s role=%s reason=%q, want coding/coder", next, role, reason)
	}

	writeFeatureFile(t, ws, "feat-dr", protocol.DesignReviewReport, "# Design Review\n\n## Verdict\nFAIL\n")
	next, role, reason = nextPhaseAfter(ws, "feat-dr", s)
	if next != protocol.PhaseDesigning || role != protocol.RoleDesigner || reason != "" {
		t.Fatalf("FAIL: got next=%s role=%s reason=%q, want designing/designer", next, role, reason)
	}
}

// W5 / AC-9：startLiveSync 回傳的 stop 為阻塞式，drain 後 final sync 不與背景 sync 競爭。
// 本測試在 -race 下執行以偵測 data race。
func TestStartLiveSync_StopDrainsThenFinalSync(t *testing.T) {
	mainWs := setupLoopWorkspace(t, "feat-sync")
	wtWs := &protocol.Workspace{Root: t.TempDir()}
	if err := wtWs.InitFeatureDir("feat-sync"); err != nil {
		t.Fatal(err)
	}
	round := 1
	roundDir := wtWs.RoundDir("feat-sync", round)
	os.MkdirAll(roundDir, 0o755)
	os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# coder"), 0o644)

	stop := startLiveSync(wtWs, mainWs, "feat-sync", round)
	os.WriteFile(filepath.Join(wtWs.FeatureDir("feat-sync"), protocol.TaskBrief), []byte("# brief"), 0o644)
	stop() // 必須阻塞到背景 goroutine 完全結束

	if err := syncFeatureFromWorktree(wtWs, mainWs, "feat-sync", round); err != nil {
		t.Fatalf("final sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mainWs.RoundDir("feat-sync", round), protocol.CoderReport)); err != nil {
		t.Errorf("coder report not synced to main: %v", err)
	}
}

// batchTestFeature 在 ws 建立一個 feature YAML，供 runBatchSchedule 測試使用。
func batchTestFeature(t *testing.T, ws *protocol.Workspace, id string, depends []string) {
	t.Helper()
	if err := ws.InitFeatureDir(id); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: id, Name: id, Status: "not-started", Depends: depends}); err != nil {
		t.Fatal(err)
	}
}

// W4 / AC-7：dependency 未完成的 feature 被 gate 擋下、標記 blocked、不進 runFeature。
func TestRunBatchSchedule_DependencyGateBlocks(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", []string{"feat-b"})
	batchTestFeature(t, ws, "feat-b", nil) // B 未 done

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed = append(executed, next)
		return feature.StatusReadyForReview, nil
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)
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
func TestRunBatchSchedule_SkipsDoneAndSetsPid(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-done")
	batchTestFeature(t, ws, "feat-done", nil)
	ws.WriteState("feat-done", protocol.State{FeatureID: "feat-done", Phase: protocol.PhaseDone})

	batchTestFeature(t, ws, "feat-run", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
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

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)

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
func TestRunBatchSchedule_FailureCapStopsRetry(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-fail")
	batchTestFeature(t, ws, "feat-fail", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-fail"}}}
	statusMap := map[string]feature.Status{"feat-fail": "not-started"}

	count := 0
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		count++
		return feature.StatusNeedsAttention, nil
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, nil)
	if count != 2 {
		t.Fatalf("feat-fail ran %d times, want capped at 2", count)
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0", completed)
	}
}
