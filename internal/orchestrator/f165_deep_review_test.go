package orchestrator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// TestClassifyOutcomes 驗證 classifyOutcomes 對四類結果（runner error / hard error /
// soft fail / 成功）的分離：runner error 立即回傳且短路；hard/soft 取第一個命中者。
func TestClassifyOutcomes(t *testing.T) {
	ok := &runner.Result{ExitCode: 0}
	hard := &runner.Result{ExitCode: runner.ExitHardError}
	soft := &runner.Result{ExitCode: runner.ExitSoftFail}

	t.Run("all success returns nils", func(t *testing.T) {
		runErr, hardErr, softFail := classifyOutcomes([]parallelOutcome{
			{index: 0, result: ok}, {index: 1, result: ok},
		})
		if runErr != nil || hardErr != nil || softFail != nil {
			t.Fatalf("all success: got (%v,%v,%v), want all nil", runErr, hardErr, softFail)
		}
	})

	t.Run("runner error short-circuits", func(t *testing.T) {
		boom := errors.New("boom")
		runErr, hardErr, softFail := classifyOutcomes([]parallelOutcome{
			{index: 0, result: ok},
			{index: 1, err: boom},
			{index: 2, result: hard}, // 應被短路，不列入 hardErr
		})
		if runErr == nil || runErr.index != 1 {
			t.Fatalf("runErr = %v, want outcome index 1", runErr)
		}
		if hardErr != nil || softFail != nil {
			t.Fatalf("short-circuit should leave hard/soft nil, got (%v,%v)", hardErr, softFail)
		}
	})

	t.Run("first hard and first soft captured", func(t *testing.T) {
		runErr, hardErr, softFail := classifyOutcomes([]parallelOutcome{
			{index: 0, result: soft},
			{index: 1, result: hard},
			{index: 2, result: hard}, // 第二個 hard 不覆寫
			{index: 3, result: soft}, // 第二個 soft 不覆寫
		})
		if runErr != nil {
			t.Fatalf("runErr = %v, want nil", runErr)
		}
		if hardErr == nil || hardErr.index != 1 {
			t.Fatalf("hardErr = %v, want first hard at index 1", hardErr)
		}
		if softFail == nil || softFail.index != 0 {
			t.Fatalf("softFail = %v, want first soft at index 0", softFail)
		}
	})
}

// TestSelectDeepReviewAngles_ForceAll 驗證 ForceAllAngles / DeepReviewAllAngles 為 true 時
// 回傳全部角度且 AngleSelection.ForceAll=true，不依賴 git ops。
func TestSelectDeepReviewAngles_ForceAll(t *testing.T) {
	t.Run("ForceAllAngles flag", func(t *testing.T) {
		r := &Runner{Config: Config{ForceAllAngles: true}}
		angles, sel := r.selectDeepReviewAngles()
		if len(angles) != protocol.DeepReviewAngleCount {
			t.Fatalf("got %d angles, want all %d", len(angles), protocol.DeepReviewAngleCount)
		}
		if !sel.ForceAll || sel.TotalAngles != protocol.DeepReviewAngleCount {
			t.Fatalf("sel = %+v, want ForceAll=true TotalAngles=%d", sel, protocol.DeepReviewAngleCount)
		}
	})

	t.Run("Feature.DeepReviewAllAngles yaml", func(t *testing.T) {
		r := &Runner{Config: Config{Feature: feature.Feature{DeepReviewAllAngles: true}}}
		angles, sel := r.selectDeepReviewAngles()
		if len(angles) != protocol.DeepReviewAngleCount || !sel.ForceAll {
			t.Fatalf("yaml force: angles=%d ForceAll=%v, want %d/true", len(angles), sel.ForceAll, protocol.DeepReviewAngleCount)
		}
	})
}

// TestWriteAngleSelection 驗證 writeAngleSelection 把 AngleSelection 寫成可解析的 JSON artifact。
func TestWriteAngleSelection(t *testing.T) {
	id := "F999-angle"
	ws := setupPhaseWorkspace(t, id)
	if err := os.MkdirAll(ws.RoundDir(id, 1), 0o755); err != nil {
		t.Fatal(err)
	}
	sel := protocol.AngleSelection{
		SelectedAngles: []int{1, 3, 5},
		TotalAngles:    protocol.DeepReviewAngleCount,
		ForceAll:       false,
	}
	writeAngleSelection(ws, id, 1, sel)

	path := filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewAnglesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("angle selection file not written: %v", err)
	}
	var got protocol.AngleSelection
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}
	if len(got.SelectedAngles) != 3 || got.SelectedAngles[0] != 1 || got.SelectedAngles[2] != 5 {
		t.Fatalf("SelectedAngles = %v, want [1 3 5]", got.SelectedAngles)
	}
}
