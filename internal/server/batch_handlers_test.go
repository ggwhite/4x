package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func setupBatchStatusWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "batch-test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	mk := func(id string, status feature.Status) {
		if err := ws.SaveFeature(feature.Feature{ID: id, Name: id, Status: status}); err != nil {
			t.Fatal(err)
		}
		if err := ws.InitFeatureDir(id); err != nil {
			t.Fatal(err)
		}
	}
	mk("feat-a", feature.StatusDone)
	mk("feat-b", feature.StatusInProgress)
	mk("feat-c", feature.StatusNotStarted)

	// feat-b 設為 active 且 pid 為當前 process（必活著），模擬 running 狀態。
	if err := ws.WriteState("feat-b", protocol.State{
		FeatureID: "feat-b",
		Phase:     protocol.PhaseCoding,
		Active:    true,
		Pid:       os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}
	return ws
}

// AC-11：GET /api/batch/status 回 {running, queue[], currentFeature, conflict}，
// queue 依 schedule 排序，含 position 與 state（running/waiting）。
// done/abandoned feature 不進 PlanBatch（見 commit 8a471e5），故 feat-a 不在 queue。
func TestBatchStatus_QueueStatesAndOrder(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, NewBatchManager(ws, "echo"))

	rec := serveRequest(t, mux, http.MethodGet, "/api/batch/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp batchStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Batch 沒在跑時只顯示 pending features（done 不出現）
	if len(resp.Queue) != 2 {
		t.Fatalf("queue len = %d, want 2: %+v", len(resp.Queue), resp.Queue)
	}
	// 順序：feat-b, feat-c（無依賴、無 priority → 依 ID；feat-a done 不在 queue 裡）。
	wantOrder := []string{"feat-b", "feat-c"}
	for i, id := range wantOrder {
		if resp.Queue[i].FeatureID != id {
			t.Errorf("queue[%d] = %s, want %s", i, resp.Queue[i].FeatureID, id)
		}
	}
	if resp.Queue[0].State != "running" || resp.Queue[0].Position != 1 {
		t.Errorf("feat-b: state=%s pos=%d, want running/1", resp.Queue[0].State, resp.Queue[0].Position)
	}
	if resp.Queue[1].State != "waiting" || resp.Queue[1].Position != 2 {
		t.Errorf("feat-c: state=%s pos=%d, want waiting/2", resp.Queue[1].State, resp.Queue[1].Position)
	}
	if resp.CurrentFeature != "feat-b" {
		t.Errorf("currentFeature = %s, want feat-b", resp.CurrentFeature)
	}
}

// AC-12：存在 batch-conflict.json 時 conflict 欄回該內容，否則 null。
func TestBatchStatus_ReportsConflict(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, NewBatchManager(ws, "echo"))

	rec := serveRequest(t, mux, http.MethodGet, "/api/batch/status", "")
	var before batchStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &before)
	if before.Conflict != nil {
		t.Errorf("conflict should be nil before writing signal, got %+v", before.Conflict)
	}

	if err := ws.WriteBatchConflict(protocol.BatchConflict{FeatureID: "feat-b", Files: []string{"x.go"}}); err != nil {
		t.Fatal(err)
	}
	rec = serveRequest(t, mux, http.MethodGet, "/api/batch/status", "")
	var after batchStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &after)
	if after.Conflict == nil {
		t.Fatal("conflict should be non-nil after writing signal")
	}
	if after.Conflict.FeatureID != "feat-b" || len(after.Conflict.Files) != 1 {
		t.Errorf("conflict = %+v, want feat-b with 1 file", *after.Conflict)
	}
}

// AC-7：存在 .4x/batch-report.json 時，GET /api/batch/status 帶 lastReport 物件；無報告時為 null。
func TestBatchStatus_ReportsLastReport(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, NewBatchManager(ws, "echo"))

	rec := serveRequest(t, mux, http.MethodGet, "/api/batch/status", "")
	var before batchStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &before)
	if before.LastReport != nil {
		t.Errorf("lastReport should be nil before writing report, got %+v", before.LastReport)
	}

	if err := ws.WriteBatchReport(protocol.BatchReport{
		Outcome:   protocol.BatchOutcomeStopped,
		Total:     3,
		Completed: 1,
		Failed:    1,
		Remaining: 1,
		Runner:    "claude",
		Features: []protocol.BatchFeatureReport{
			{ID: "feat-a", Name: "feat-a", FinalStatus: feature.StatusDone, Rounds: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec = serveRequest(t, mux, http.MethodGet, "/api/batch/status", "")
	var after batchStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &after)
	if after.LastReport == nil {
		t.Fatal("lastReport should be non-nil after writing report")
	}
	if after.LastReport.Outcome != protocol.BatchOutcomeStopped {
		t.Errorf("lastReport.outcome = %q, want stopped", after.LastReport.Outcome)
	}
	if after.LastReport.Completed != 1 || after.LastReport.Failed != 1 || after.LastReport.Remaining != 1 {
		t.Errorf("lastReport counts = %d/%d/%d, want 1/1/1",
			after.LastReport.Completed, after.LastReport.Failed, after.LastReport.Remaining)
	}
	if len(after.LastReport.Features) != 1 || after.LastReport.Features[0].ID != "feat-a" {
		t.Errorf("lastReport.features = %+v, want 1 entry feat-a", after.LastReport.Features)
	}
}

// AC-13：POST /api/batch/start 在存在未解決 conflict 時回 409。
func TestBatchStart_ConflictReturns409(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, NewBatchManager(ws, "echo"))

	if err := ws.WriteBatchConflict(protocol.BatchConflict{FeatureID: "feat-b"}); err != nil {
		t.Fatal(err)
	}
	rec := serveRequest(t, mux, http.MethodPost, "/api/batch/start", `{}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

// AC-13：POST /api/batch/start 無 conflict 時啟動 batch。
func TestBatchStart_StartsWhenNoConflict(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	bm := NewBatchManager(ws, fakeBatchCommand(t))
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, bm)

	rec := serveRequest(t, mux, http.MethodPost, "/api/batch/start", `{"runner":"claude","maxRounds":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	waitUntil(t, bm.Running)
	if err := bm.Stop(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return !bm.Running() })
}

// AC-14：POST /api/batch/stop 寫出 batch-stop 信號檔。
func TestBatchStop_WritesSignal(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, NewBatchManager(ws, "echo"))

	rec := serveRequest(t, mux, http.MethodPost, "/api/batch/stop", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(ws.DotDir(), protocol.BatchStopFile)); err != nil {
		t.Errorf("batch-stop should exist after stop: %v", err)
	}
}

// AC-14：POST /api/batch/continue 先 ClearBatchConflict 再 Start。
func TestBatchContinue_ClearsConflictThenStarts(t *testing.T) {
	ws := setupBatchStatusWorkspace(t)
	bm := NewBatchManager(ws, fakeBatchCommand(t))
	mux := newMux(protocol.NewCachedWorkspace(ws), nil, bm)

	if err := ws.WriteBatchConflict(protocol.BatchConflict{FeatureID: "feat-b"}); err != nil {
		t.Fatal(err)
	}
	rec := serveRequest(t, mux, http.MethodPost, "/api/batch/continue", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if conflict, _ := ws.ReadBatchConflict(); conflict != nil {
		t.Errorf("conflict should be cleared by continue, got %+v", *conflict)
	}
	waitUntil(t, bm.Running)
	if err := bm.Stop(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return !bm.Running() })
}
