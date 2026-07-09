package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// --- 測試替身 ---

// funcRunner 把一個函式包成 runner.Runner。
type funcRunner func(ctx context.Context) (*runner.Result, error)

func (f funcRunner) Run(ctx context.Context, _ string) (*runner.Result, error) { return f(ctx) }

// fakeConvergeOps 實作 gitops.Ops，只讓 DetectChangedRepos 回傳可控值供 guard scope 檢查，
// 其餘方法皆 no-op（收斂路徑不會用到）。
type fakeConvergeOps struct{ changedRepos []string }

func (f fakeConvergeOps) SetupWorktree(string, []string) (string, error) { return "", nil }
func (f fakeConvergeOps) Commit(string, string, string) error            { return nil }
func (f fakeConvergeOps) Merge(string, string) gitops.MergeResult        { return gitops.MergeResult{} }
func (f fakeConvergeOps) PushAndOpenMR(string, string) gitops.MergeResult {
	return gitops.MergeResult{}
}
func (f fakeConvergeOps) Cleanup(string) error                                 { return nil }
func (f fakeConvergeOps) DetectChangedRepos(string) []string                   { return f.changedRepos }
func (f fakeConvergeOps) DetectChangedFiles(string) []protocol.ChangedFile     { return nil }
func (f fakeConvergeOps) CaptureBaseline(string, []string) error               { return nil }
func (f fakeConvergeOps) IsMultiRepo() bool                                    { return false }
func (f fakeConvergeOps) GenerateReviewPackage(string, string) (string, error) { return "", nil }

// convergeScript 依 log 檔名分辨呼叫的子 role，記錄呼叫次數並在 reviewer 重跑時依序寫入
// 預先腳本化的 review-report 內容；mini-coder 呼叫時可選地觸發 miniAction（模擬 scope 越界）。
type convergeScript struct {
	ws              *protocol.Workspace
	featureID       string
	round           int
	reviewerReports []string
	reviewerIdx     int
	miniCalls       int
	reviewerCalls   int
	miniAction      func()
}

func (cs *convergeScript) newRunner(_, logPath, _ string) runner.Runner {
	return funcRunner(func(_ context.Context) (*runner.Result, error) {
		_ = os.WriteFile(logPath, []byte("exit-0\n"), 0o644)
		base := filepath.Base(logPath)
		switch {
		case strings.Contains(base, "review-fix"):
			cs.miniCalls++
			if cs.miniAction != nil {
				cs.miniAction()
			}
		case strings.Contains(base, "reviewer"):
			cs.reviewerCalls++
			content := ""
			if cs.reviewerIdx < len(cs.reviewerReports) {
				content = cs.reviewerReports[cs.reviewerIdx]
			} else if len(cs.reviewerReports) > 0 {
				content = cs.reviewerReports[len(cs.reviewerReports)-1]
			}
			cs.reviewerIdx++
			_ = os.WriteFile(filepath.Join(cs.ws.RoundDir(cs.featureID, cs.round), protocol.ReviewReport), []byte(content), 0o644)
		}
		return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
	})
}

// --- report fixtures ---

const (
	condPassReport  = "# Review Report\n\n## Issues\n### [WARNING] nit\ndetail\n\n## Verdict\nCONDITIONAL PASS\n"
	cleanPassReport = "# Review Report\n\n## Verdict\nPASS\n"
	failReport      = "# Review Report\n\n## Issues\n### [CRITICAL] boom\ndetail\n\n## Verdict\nFAIL\n"
)

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupConvergeWS(t *testing.T, featureID string, repos []string, initialReport string) (*protocol.Workspace, protocol.Config, feat.Feature) {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "f144"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Tiers: map[string]string{"sonnet": "claude-sonnet", "opus": "claude-opus"}},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws := &protocol.Workspace{Root: root}
	feature := feat.Feature{ID: featureID, Name: featureID + " test", Status: feat.StatusInProgress, Repos: repos}
	if err := ws.SaveFeature(feature); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	fd := ws.FeatureDir(featureID)
	if err := os.MkdirAll(fd, 0o755); err != nil {
		t.Fatalf("mkdir feature dir: %v", err)
	}
	st := protocol.State{FeatureID: featureID, Phase: protocol.PhaseReviewing, Role: protocol.RoleReviewer, Round: 1, Active: true}
	if err := ws.WriteState(featureID, st); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	writeFile(t, filepath.Join(fd, protocol.TaskBrief), "# Task Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(fd, protocol.Criteria), "# Acceptance Criteria\n")
	writeFile(t, filepath.Join(ws.RoundDir(featureID, 1), protocol.ReviewReport), initialReport)
	return ws, cfg, feature
}

func newConvergeRunner(ws *protocol.Workspace, cfg protocol.Config, feature feat.Feature, ops gitops.Ops, script *convergeScript) *Runner {
	return &Runner{Config: Config{
		Ws:        ws,
		RunnerWs:  ws,
		Feature:   feature,
		Cfg:       cfg,
		Ops:       ops,
		NewRunner: script.newRunner,
	}}
}

func readEvents(t *testing.T, ws *protocol.Workspace, featureID string) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		return nil
	}
	var out []protocol.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e protocol.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func hasEventType(events []protocol.Event, typ string) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func resolvePC(t *testing.T, cfg protocol.Config, feature feat.Feature) protocol.ProfileConfig {
	t.Helper()
	_, pc, err := protocol.ResolveProfile(cfg, feature, "")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	return pc
}

// --- tests ---

// TestRunReviewConvergence_PurePass_NoMiniCoder 驗證 AC-3：純 PASS 不 spawn mini-coder，
// 直接由 NextPhaseAfter 轉 testing。
func TestRunReviewConvergence_PurePass_NoMiniCoder(t *testing.T) {
	ws, cfg, feature := setupConvergeWS(t, "F144-pass", nil, cleanPassReport)
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1}
	r := newConvergeRunner(ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	cont, _, err := r.runReviewConvergence(context.Background(), &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("runReviewConvergence: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (pure PASS should not stop the loop)")
	}
	if script.miniCalls != 0 {
		t.Errorf("mini-coder called %d times, want 0 for pure PASS", script.miniCalls)
	}
	if next, _, _ := NextPhaseAfter(ws, feature.ID, s); next != protocol.PhaseTesting {
		t.Errorf("NextPhaseAfter = %q, want testing", next)
	}
}

// TestRunReviewConvergence_PureFail_NoMiniCoder 驗證 AC-4：純 FAIL 不 spawn mini-coder，
// 由 NextPhaseAfter 轉 amending。
func TestRunReviewConvergence_PureFail_NoMiniCoder(t *testing.T) {
	ws, cfg, feature := setupConvergeWS(t, "F144-fail", nil, failReport)
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1}
	r := newConvergeRunner(ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	cont, _, err := r.runReviewConvergence(context.Background(), &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("runReviewConvergence: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (pure FAIL should not stop the loop)")
	}
	if script.miniCalls != 0 {
		t.Errorf("mini-coder called %d times, want 0 for pure FAIL", script.miniCalls)
	}
	if next, _, _ := NextPhaseAfter(ws, feature.ID, s); next != protocol.PhaseAmending {
		t.Errorf("NextPhaseAfter = %q, want amending", next)
	}
}

// TestRunReviewConvergence_ConditionalThenClean 驗證 AC-2 + AC-5：CONDITIONAL PASS 會 spawn
// mini-coder 並重跑 reviewer；重跑後乾淨 PASS → 收斂結束、round 不變、phase 維持 reviewing、
// 續轉 testing。
func TestRunReviewConvergence_ConditionalThenClean(t *testing.T) {
	ws, cfg, feature := setupConvergeWS(t, "F144-cond", nil, condPassReport)
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport}}
	r := newConvergeRunner(ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	roundBefore := s.Round
	cont, _, err := r.runReviewConvergence(context.Background(), &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("runReviewConvergence: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true")
	}
	if script.miniCalls != 1 {
		t.Errorf("mini-coder called %d times, want 1", script.miniCalls)
	}
	if script.reviewerCalls != 1 {
		t.Errorf("reviewer re-run called %d times, want 1", script.reviewerCalls)
	}
	if s.Round != roundBefore {
		t.Errorf("round changed %d → %d, want unchanged (same-round convergence)", roundBefore, s.Round)
	}
	if s.Phase != protocol.PhaseReviewing {
		t.Errorf("phase = %q, want reviewing throughout convergence", s.Phase)
	}
	if next, _, _ := NextPhaseAfter(ws, feature.ID, s); next != protocol.PhaseTesting {
		t.Errorf("NextPhaseAfter = %q, want testing", next)
	}
}

// TestRunReviewConvergence_ScopeExceed 驗證 AC-6：mini-coder 觸發 guard scope 越界時
// state 轉 needs-attention、回 (false, nil)、寫 guard-fail event。
func TestRunReviewConvergence_ScopeExceed(t *testing.T) {
	ws, cfg, feature := setupConvergeWS(t, "F144-scope", []string{"internal/foo"}, condPassReport)
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport}}
	// mini-coder「改到」scope 外的 repo：讓 DetectChangedRepos 回報 internal/bar（不在 feature.Repos）。
	ops := fakeConvergeOps{changedRepos: []string{"internal/bar"}}
	r := newConvergeRunner(ws, cfg, feature, ops, script)

	s, _ := ws.ReadState(feature.ID)
	cont, _, err := r.runReviewConvergence(context.Background(), &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("runReviewConvergence: %v", err)
	}
	if cont {
		t.Fatal("cont = true, want false (scope-exceed should stop the loop)")
	}
	if s.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %q, want needs-attention", s.Phase)
	}
	if s.Active {
		t.Error("s.Active = true, want false")
	}
	if script.miniCalls != 1 {
		t.Errorf("mini-coder called %d times, want 1 (guard checked after first fix)", script.miniCalls)
	}
	if !hasEventType(readEvents(t, ws, feature.ID), "guard-fail") {
		t.Error("event log should contain a guard-fail event")
	}
}

// TestRunReviewConvergence_ResidualPassthrough 驗證 AC-7：跑滿上限仍 CONDITIONAL PASS 時
// 放行 testing（不打回 amending），mini-coder 呼叫次數 == 上限，並寫 conditional-pass-residual event。
func TestRunReviewConvergence_ResidualPassthrough(t *testing.T) {
	ws, cfg, feature := setupConvergeWS(t, "F144-residual", nil, condPassReport)
	// 每次重跑 reviewer 都回 CONDITIONAL PASS。
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{condPassReport}}
	r := newConvergeRunner(ws, cfg, feature, fakeConvergeOps{}, script)

	maxFix := protocol.ResolveMaxFixRounds(cfg, protocol.RoleReviewer)

	s, _ := ws.ReadState(feature.ID)
	cont, _, err := r.runReviewConvergence(context.Background(), &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("runReviewConvergence: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (residual warnings pass through, not blocked)")
	}
	if script.miniCalls != maxFix {
		t.Errorf("mini-coder called %d times, want %d (max fix rounds)", script.miniCalls, maxFix)
	}
	if next, _, _ := NextPhaseAfter(ws, feature.ID, s); next != protocol.PhaseTesting {
		t.Errorf("NextPhaseAfter = %q, want testing (non-blocking downgrade)", next)
	}
	if !hasEventType(readEvents(t, ws, feature.ID), "conditional-pass-residual") {
		t.Error("event log should contain a conditional-pass-residual event")
	}
}

// TestDeepReviewClearsToAccepting 驗證 AC-10：deep-review 放行判定三情境。
func TestDeepReviewClearsToAccepting(t *testing.T) {
	fixingPC := protocol.ProfileConfig{Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseFixing)}}}
	noFixingPC := protocol.ProfileConfig{Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseAccepting)}}}

	tests := []struct {
		name   string
		report string
		pc     protocol.ProfileConfig
		want   bool
	}{
		{"clean PASS clears (F132 regression)", cleanPassReport, fixingPC, true},
		{"clean PASS clears without fixing", cleanPassReport, noFixingPC, true},
		{"conditional with fixing clears", condPassReport, fixingPC, true},
		{"conditional without fixing goes self-heal", condPassReport, noFixingPC, false},
		{"FAIL goes self-heal", failReport, fixingPC, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "f144"}}); err != nil {
				t.Fatal(err)
			}
			ws := &protocol.Workspace{Root: root}
			feature := feat.Feature{ID: "F144-deep", Name: "deep"}
			writeFile(t, filepath.Join(ws.RoundDir(feature.ID, 1), protocol.DeepReviewReport), tt.report)
			r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature}}
			if got := r.deepReviewClearsToAccepting(1, tt.pc); got != tt.want {
				t.Errorf("deepReviewClearsToAccepting = %v, want %v", got, tt.want)
			}
		})
	}
}
