package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// deepScript 是 runDeepReviewPhase 的 scripted fake runner：deep-reviewer 寫出乾淨 PASS 的
// deep-review-report.md，讓自癒循環判定放行。
type deepScript struct {
	ws        *protocol.Workspace
	featureID string
	round     int
	report    string
	calls     int
	// codexSessionID 非空時 log 前置 thread.started 事件（F168 codex 接線測試用）。
	codexSessionID string
}

func (ds *deepScript) newRunner(_, logPath, _ string) runner.Runner {
	return funcRunner(func(_ context.Context) (*runner.Result, error) {
		content := "exit-0\n"
		if ds.codexSessionID != "" {
			content = `{"type":"thread.started","thread_id":"` + ds.codexSessionID + `"}` + "\n" + content
			_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		}
		_ = os.WriteFile(logPath, []byte(content), 0o644)
		ds.calls++
		_ = os.MkdirAll(ds.ws.RoundDir(ds.featureID, ds.round), 0o755)
		_ = os.WriteFile(
			filepath.Join(ds.ws.RoundDir(ds.featureID, ds.round), protocol.DeepReviewReport),
			[]byte(ds.report), 0o644)
		return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
	})
}

// setupDeepWS 建立 deep-reviewing 起點 workspace，seed 通過 guard 所需的最小 artifact：
// task-brief / criteria / review-report(PASS) / verify.json(passing)。
func setupDeepWS(t *testing.T, featureID string) (*protocol.Workspace, protocol.Config, feat.Feature) {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "f165-deep"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Tiers: map[string]string{"sonnet": "claude-sonnet", "opus": "claude-opus"}},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws := &protocol.Workspace{Root: root}
	feature := feat.Feature{ID: featureID, Name: featureID + " test", Status: feat.StatusInProgress}
	if err := ws.SaveFeature(feature); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	fd := ws.FeatureDir(featureID)
	if err := os.MkdirAll(fd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st := protocol.State{FeatureID: featureID, Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: 1, Active: true}
	if err := ws.WriteState(featureID, st); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	writeFile(t, filepath.Join(fd, protocol.TaskBrief), "# Task Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(fd, protocol.Criteria), "# Acceptance Criteria\n- AC-1\n")
	rd := ws.RoundDir(featureID, 1)
	writeFile(t, filepath.Join(rd, protocol.ReviewReport), cleanPassReport)
	writeFile(t, filepath.Join(rd, protocol.VerifyFile), string(passingVerify(1)))
	writeFile(t, filepath.Join(rd, protocol.TestReport), "# Test\n## Verdict\nPASS\n")
	return ws, cfg, feature
}

// TestRunDeepReviewPhase_CleanPassTransitions 驅動整條 deep-reviewing 自癒入口：
// deepReviewSetup → deepReviewRun（單 agent runDeepSubRole）→ deepGuardCheck →
// deepReviewClearsToAccepting → deepTransitionAccepting。乾淨 PASS + 無 fixing profile
// 應推進到 accepting，且 deep-review-report.md 與 angle selection 已寫出。
func TestRunDeepReviewPhase_CleanPass(t *testing.T) {
	id := "F165-deepok"
	ws, cfg, feature := setupDeepWS(t, id)
	// profile 未啟用 fixing → deepTransitionAccepting 目標為 accepting。
	feature.Profile = "lite"
	cfg.Profiles = map[string]protocol.ProfileConfig{
		"lite": {Phases: []protocol.PhaseSpec{
			{Phase: string(protocol.PhaseCoding)},
			{Phase: string(protocol.PhaseReviewing)},
			{Phase: string(protocol.PhaseTesting)},
			{Phase: string(protocol.PhaseDeepReviewing)},
			{Phase: string(protocol.PhaseAccepting)},
		}},
	}
	if err := ws.SaveFeature(feature); err != nil {
		t.Fatal(err)
	}

	script := &deepScript{ws: ws, featureID: id, round: 1, report: cleanPassReport}
	r := &Runner{Config: Config{
		Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg,
		Ops: fakeConvergeOps{}, NewRunner: script.newRunner,
	}}

	s, _ := ws.ReadState(id)
	cont, err := r.runDeepReviewPhase(context.Background(), &s)
	if err != nil {
		t.Fatalf("runDeepReviewPhase: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (clean PASS should advance)")
	}
	if script.calls == 0 {
		t.Fatal("deep reviewer runner was never invoked")
	}
	if s.Phase != protocol.PhaseAccepting {
		t.Fatalf("phase = %q, want accepting after clean deep-review PASS (no fixing profile)", s.Phase)
	}
	// deep-review 產物已寫出
	if _, statErr := os.Stat(filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewReport)); statErr != nil {
		t.Fatalf("deep-review-report.md not present: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewAnglesFile)); statErr != nil {
		t.Fatalf("angle selection artifact not written: %v", statErr)
	}
}

// TestRunDeepReviewPhase_FailSelfHealExhausts 驅動 FAIL 分支：deep-review-report FAIL 且
// mini-coder 無法收斂 → 自癒循環耗盡 → 落 needs-attention、回 (false,nil)。
func TestRunDeepReviewPhase_FailSelfHeal(t *testing.T) {
	id := "F165-deepfail"
	ws, cfg, feature := setupDeepWS(t, id)
	// max_fix_rounds 設 1，讓自癒最快耗盡（mini-coder 不改東西，report 持續 FAIL）。
	cfg.Roles = map[string]protocol.RoleConfig{
		string(protocol.RoleDeepReviewer): {MaxFixRounds: 1},
	}

	script := &deepScript{ws: ws, featureID: id, round: 1, report: failReport}
	r := &Runner{Config: Config{
		Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg,
		Ops: fakeConvergeOps{}, NewRunner: script.newRunner,
	}}

	s, _ := ws.ReadState(id)
	cont, err := r.runDeepReviewPhase(context.Background(), &s)
	if err != nil {
		t.Fatalf("runDeepReviewPhase: %v", err)
	}
	if cont {
		t.Fatal("cont = true, want false (self-heal exhausted should stop the loop)")
	}
	if s.Phase != protocol.PhaseNeedsAttention {
		t.Fatalf("phase = %q, want needs-attention after self-heal exhausted", s.Phase)
	}
}
