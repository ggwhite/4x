package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
)

// TestRunLoop_SoftFailMessageExitCode 驗證 soft-fail 訊息含正確的 exit code 文字（F086 task 1）：
// 不再寫死 "(exit 3)"，而是與 runner.ExitSoftFail 一致的 "(exit 1)"。
func TestRunLoop_SoftFailMessageExitCode(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sf")
	feat0, _ := ws.LoadFeature("feat-sf")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-sf", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-sf", s)

	// designer 正常寫產出物後，coding phase runner 回傳 soft-fail（exit 1）。
	factory := func(_, logPath, _ string) runner.Runner {
		return &softFailRunner{ws: ws, featureID: "feat-sf", failPhase: protocol.PhaseCoding}
	}

	if err := runLoop(context.Background(), ws, ws, feat0, cfg, s, nil, factory, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sf")
	if final.Phase != protocol.PhaseBlocked {
		t.Fatalf("phase = %s, want blocked", final.Phase)
	}
	if !strings.Contains(final.StopMessage, "(exit 1)") {
		t.Errorf("StopMessage = %q, want it to contain \"(exit 1)\"", final.StopMessage)
	}
	if strings.Contains(final.StopMessage, "(exit 3)") {
		t.Errorf("StopMessage should not contain stale \"(exit 3)\": %q", final.StopMessage)
	}
}

// softFailRunner 在指定 phase 回傳 soft-fail（ExitSoftFail），其餘 phase 正常寫產出物。
type softFailRunner struct {
	ws        *protocol.Workspace
	featureID string
	failPhase protocol.Phase
}

func (r *softFailRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	st, _ := r.ws.ReadState(r.featureID)
	roundDir := r.ws.RoundDir(r.featureID, st.Round)
	os.MkdirAll(roundDir, 0o755)
	featureDir := r.ws.FeatureDir(r.featureID)
	switch st.Phase {
	case protocol.PhaseDesigning:
		os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)
	}
	if st.Phase == r.failPhase {
		return &runner.Result{ExitCode: runner.ExitSoftFail}, nil
	}
	return &runner.Result{ExitCode: 0}, nil
}

// TestRunLoop_ParallelNoProgressStops 驗證平行 review/test 路徑（F086 task 6）：
// 連續 FAIL 時 ConsecutiveNoProgress 隨輪次累加，達門檻（>=3）時 ShouldStop 生效，
// loop 落入 needs-attention 而非跑滿 maxRounds。
func TestRunLoop_ParallelNoProgressStops(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-np")
	feat0, _ := ws.LoadFeature("feat-np")
	cfg, _ := ws.ReadConfig()
	cfg.ParallelReviewTest = true

	s := protocol.State{
		FeatureID: "feat-np", Phase: protocol.PhaseInit,
		MaxRounds: 20, Active: true, Runner: "mock", Profile: "full",
	}
	ws.WriteState("feat-np", s)

	var mu sync.Mutex
	factory := func(_, logPath, _ string) runner.Runner {
		return &failingParallelRunner{ws: ws, featureID: "feat-np", role: roleFromLogPath(logPath), mu: &mu}
	}

	if err := runLoop(context.Background(), ws, ws, feat0, cfg, s, nil, factory, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-np")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.ConsecutiveNoProgress < 3 {
		t.Errorf("ConsecutiveNoProgress = %d, want >= 3", final.ConsecutiveNoProgress)
	}
	if stop, _ := state.ShouldStop(final); !stop {
		t.Errorf("ShouldStop should be true for final state, got false")
	}
	// 應在達 no-progress 門檻時停下，遠未跑滿 maxRounds(20)。
	if final.Round >= 20 {
		t.Errorf("round = %d, should stop well before maxRounds(20) due to no-progress", final.Round)
	}
}

// failingParallelRunner 模擬平行 reviewing：reviewer 永遠 FAIL（固定 1 個 critical），
// tester verify 永遠不過，coder/designer 正常產出，使每輪 reviewing→amending 且無進展。
type failingParallelRunner struct {
	ws        *protocol.Workspace
	featureID string
	role      string
	mu        *sync.Mutex
}

func (r *failingParallelRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, _ := r.ws.ReadState(r.featureID)
	roundDir := r.ws.RoundDir(r.featureID, st.Round)
	os.MkdirAll(roundDir, 0o755)
	featureDir := r.ws.FeatureDir(r.featureID)
	switch r.role {
	case "designer":
		os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)
	case "coder":
		os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Coder Report"), 0o644)
	case "reviewer":
		report := "# Review Report\n\n### [CRITICAL] Issue — file.go\n\n## Verdict\nFAIL\n"
		os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte(report), 0o644)
	case "tester":
		ve := protocol.VerifyEvidence{Passed: false, Round: st.Round}
		data, _ := json.Marshal(ve)
		os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), data, 0o644)
		os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
	}
	return &runner.Result{ExitCode: 0}, nil
}

// TestWorktreeExitHints 驗證 worktree 結束提示（F086 task 3）：done/pending-review 印 merge 指令，
// 非完成狀態印 worktree 保留與清理提示；非 worktree（空路徑）回 nil。
func TestWorktreeExitHints(t *testing.T) {
	const wt = "/tmp/wt/feat-x"

	// 非完成狀態 → worktree 保留提示。
	for _, ph := range []protocol.Phase{protocol.PhaseNeedsAttention, protocol.PhaseBlocked} {
		lines := worktreeExitHints(wt, "feat-x", ph, "per-round")
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, wt) {
			t.Errorf("phase %s: hints should contain worktree path %q, got: %q", ph, wt, joined)
		}
		if !strings.Contains(joined, "git worktree remove") {
			t.Errorf("phase %s: hints should contain cleanup command, got: %q", ph, joined)
		}
	}

	// done → merge 指令。
	done := strings.Join(worktreeExitHints(wt, "feat-x", protocol.PhaseDone, "per-round"), "\n")
	if !strings.Contains(done, "to merge") || !strings.Contains(done, wt) {
		t.Errorf("done hints should contain merge instructions with path, got: %q", done)
	}

	// 非 worktree 模式（空路徑）→ nil。
	if got := worktreeExitHints("", "feat-x", protocol.PhaseNeedsAttention, "per-round"); got != nil {
		t.Errorf("empty wtPath should yield nil hints, got: %v", got)
	}
}

// TestStartBackgroundRun 驗證 background 子程序的 stderr/stdout 被導向 log 檔（F086 task 4）：
// 子程序早期失敗時其輸出仍寫進 logPath，不進 /dev/null。
func TestStartBackgroundRun(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.log")

	// 用 sh 模擬會早期失敗並對 stderr 輸出的子程序。
	proc, err := startBackgroundRun("/bin/sh", []string{"-c", "echo early-failure >&2; exit 1"}, dir, logPath)
	if err != nil {
		t.Fatalf("startBackgroundRun: %v", err)
	}
	if proc == nil || proc.Pid <= 0 {
		t.Fatalf("expected a valid process, got %+v", proc)
	}
	// 等子程序結束（背景 Wait 已啟動，這裡用 FindProcess+Wait 的替代：輪詢 log）。
	if _, err := proc.Wait(); err != nil {
		// 子程序以 exit 1 結束，Wait 可能回 *ExitError，屬預期，不視為測試失敗。
		_ = err
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "early-failure") {
		t.Errorf("log should capture child stderr, got: %q", string(data))
	}
}

// TestMarkDone_ConfigLoadFailsAborts 驗證 done 在 config 載入失敗時中斷回 error（F086 task 8），
// 不再用零值 Config{} 繼續 merge；feature 維持 pending-review。
func TestMarkDone_ConfigLoadFailsAborts(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-cfg")

	// feature 置於 pending-review，讓 markDone 通過 phase 檢查後抵達 config 載入。
	s := protocol.State{
		FeatureID: "feat-cfg", Phase: protocol.PhasePendingReview,
		Active: false, Runner: "mock",
	}
	ws.WriteState("feat-cfg", s)
	ws.SaveFeature(feature.Feature{ID: "feat-cfg", Name: "Cfg Feature", Status: "pending-review"})

	// 損毀 settings.json 讓 LoadMergedConfig 失敗。
	cfgPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	if err := os.WriteFile(cfgPath, []byte("{ this is not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := markDone(ws, "feat-cfg")
	if err == nil {
		t.Fatal("markDone should return error when config load fails")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "config") {
		t.Errorf("error should mention config, got: %v", err)
	}

	// 未降級 merge，feature 仍為 pending-review。
	final, _ := ws.ReadState("feat-cfg")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review (merge must not have run)", final.Phase)
	}
}
