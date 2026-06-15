package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// AC-4 / S1：所有 feature 自然跑完後，報告 outcome=completed 且統計到完成數。
func TestBatchReport_S1CompletedAllDone(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)
	batchTestFeature(t, ws, "feat-b", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]feature.Status{"feat-a": "not-started", "feat-b": "not-started"}
	progress := &batchProgress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   protocol.BatchOutcomeCompleted,
	}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		return feature.StatusReadyForReview, nil
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
	if completed != 2 {
		t.Fatalf("completed = %d, want 2", completed)
	}

	report := finishBatchReport(ws, plan, "mock", progress, "", "")
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
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}
	progress := &batchProgress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   protocol.BatchOutcomeCompleted,
	}

	// 在主迴圈開跑前放下 stop-file，迴圈第一輪即偵測到 → markStopped → break。
	if err := os.WriteFile(filepath.Join(ws.DotDir(), protocol.BatchStopFile), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stop-file: %v", err)
	}

	executed := 0
	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		executed++
		return feature.StatusReadyForReview, nil
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
	if executed != 0 {
		t.Fatalf("runFeature executed %d times, want 0 (stopped before run)", executed)
	}
	if completed != 0 {
		t.Fatalf("completed = %d, want 0", completed)
	}

	report := finishBatchReport(ws, plan, "mock", progress, "", "")
	if report.Outcome != protocol.BatchOutcomeStopped {
		t.Errorf("outcome = %q, want stopped", report.Outcome)
	}

	got, _ := ws.ReadBatchReport()
	if got == nil || got.Outcome != protocol.BatchOutcomeStopped {
		t.Errorf("persisted outcome = %v, want stopped", got)
	}
}

// AC-4 / S1 回歸測試：snapshot 殘留 runningFeature（末位 feature 走 error-continue 路徑後
// 未清空）時，S1（completed/stopped，outcomeOverride==""）的報告必須清空 RunningFeature——
// completed/stopped 報告語義上不該有「正在跑的 feature」。鎖住 round 3 deep-review 找到的回歸。
func TestBatchReport_S1ClearsStaleRunningFeature(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": feature.StatusBlocked}
	progress := &batchProgress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   protocol.BatchOutcomeCompleted,
	}
	// 模擬 error-continue 路徑：setRunning 後未經 update() 清空就退出迴圈，殘留 runningFeature。
	progress.setRunning("feat-a")

	report := finishBatchReport(ws, plan, "mock", progress, "", "")
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
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)
	batchTestFeature(t, ws, "feat-b", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	// feat-a 已完成、feat-b 正在跑時收到 signal。
	statusMap := map[string]feature.Status{
		"feat-a": feature.StatusDone,
		"feat-b": feature.StatusInProgress,
	}
	progress := &batchProgress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   protocol.BatchOutcomeCompleted,
	}
	progress.update(statusMap)
	progress.setRunning("feat-b")

	// 直接呼叫 production 的 signal goroutine 收尾函式（outcome 覆寫為 interrupted）。
	report := finishBatchReport(ws, plan, "mock", progress, protocol.BatchOutcomeInterrupted, "")
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
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]feature.Status{"feat-a": "not-started"}
	progress := &batchProgress{
		startedAt: time.Now(),
		statusMap: maps.Clone(statusMap),
		outcome:   protocol.BatchOutcomeCompleted,
	}

	runFeature := func(next string, f feature.Feature, s protocol.State) (feature.Status, error) {
		panic("coder subprocess exploded")
	}

	// 走 production 的 recover 收尾路徑：攔截 panic 後呼叫 finishBatchReport 寫 crashed report，
	// 再 re-panic（此處測試僅驗證報告產出，故不轉拋）。
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate from runFeature")
			}
			finishBatchReport(ws, plan, "mock", progress, protocol.BatchOutcomeCrashed, fmt.Sprint(r))
		}()
		runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, nil, progress)
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
