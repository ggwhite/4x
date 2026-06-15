package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

type mockOutcome struct {
	reviewVerdict    string
	criticalIssues   int
	testPassed       bool
	escalation       bool
	escalationReason string
	omitCommitPlan   bool
}

type mockRunner struct {
	ws               *protocol.Workspace
	featureID        string
	outcomes         []mockOutcome
	idx              int
	phases           []protocol.Phase
	roles            []protocol.Role
	prompts          []string
	baselineAtCoding []bool
}

func (m *mockRunner) Run(_ context.Context, prompt string) (*runner.Result, error) {
	s, _ := m.ws.ReadState(m.featureID)
	m.phases = append(m.phases, s.Phase)
	m.roles = append(m.roles, s.Role)
	m.prompts = append(m.prompts, prompt)
	if s.Phase == protocol.PhaseCoding {
		baselinePath := filepath.Join(m.ws.FeatureDir(m.featureID), protocol.BaselineFile)
		_, err := os.Stat(baselinePath)
		m.baselineAtCoding = append(m.baselineAtCoding, err == nil)
	}

	outcome := mockOutcome{testPassed: true, reviewVerdict: "PASS"}
	if m.idx < len(m.outcomes) {
		outcome = m.outcomes[m.idx]
	}
	m.idx++

	roundDir := m.ws.RoundDir(m.featureID, s.Round)
	os.MkdirAll(roundDir, 0o755)
	featureDir := m.ws.FeatureDir(m.featureID)

	switch s.Phase {
	case protocol.PhaseDesigning:
		os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)

	case protocol.PhaseCoding, protocol.PhaseAmending:
		os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Coder Report"), 0o644)
		if outcome.escalation {
			reason := outcome.escalationReason
			if reason == "" {
				reason = "blocker"
			}
			data, _ := json.Marshal(protocol.Escalation{Needed: true, Reason: reason})
			os.WriteFile(filepath.Join(roundDir, protocol.EscalationFile), data, 0o644)
		}

	case protocol.PhaseReviewing:
		verdict := outcome.reviewVerdict
		if verdict == "" {
			verdict = "PASS"
		}
		report := "# Review Report\n\n"
		for i := 0; i < outcome.criticalIssues; i++ {
			report += "### [CRITICAL] Issue — file.go\n\n"
		}
		report += "## Verdict\n" + verdict + "\n"
		os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte(report), 0o644)

	case protocol.PhaseDeepReviewing:
		// deep-reviewing phase 內三個子 role 共用同一 phase，依 role 區分產出物：
		// mini-coder 寫 coder-report（模擬修正）；deep-reviewer 與 re-verifier 寫 / 改 deep-review-report 的 Verdict。
		switch s.Role {
		case protocol.RoleMiniCoder:
			os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Mini Coder Report"), 0o644)
		default:
			verdict := outcome.reviewVerdict
			if verdict == "" {
				verdict = "PASS"
			}
			report := "# Deep Review Report\n\n"
			for i := 0; i < outcome.criticalIssues; i++ {
				report += "### [CRITICAL] Issue — file.go\n\n"
			}
			report += "## Verdict\n" + verdict + "\n"
			os.WriteFile(filepath.Join(roundDir, protocol.DeepReviewReport), []byte(report), 0o644)
		}

	case protocol.PhaseTesting:
		ve := protocol.VerifyEvidence{Passed: outcome.testPassed, Round: s.Round}
		data, _ := json.Marshal(ve)
		os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), data, 0o644)
		os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
		if outcome.testPassed {
			os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final"), 0o644)
			if !outcome.omitCommitPlan {
				os.WriteFile(filepath.Join(featureDir, protocol.CommitPlan), []byte("# Commit Plan"), 0o644)
			}
		}
		if outcome.escalation {
			reason := outcome.escalationReason
			if reason == "" {
				reason = "blocker"
			}
			data, _ := json.Marshal(protocol.Escalation{Needed: true, Reason: reason})
			os.WriteFile(filepath.Join(roundDir, protocol.EscalationFile), data, 0o644)
		}
	}

	return &runner.Result{ExitCode: 0}, nil
}

func setupLoopWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "loop-test"},
		Default: "mock",
		Runners: map[string]protocol.RunnerConfig{"mock": {Command: "echo"}},
		ModelTiers: map[string]map[string]string{
			"opus":   {"mock": "mock-opus"},
			"sonnet": {"mock": "mock-sonnet"},
			"haiku":  {"mock": "mock-haiku"},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	f := feature.Feature{ID: featureID, Name: "Test Feature", Status: "not-started"}
	ws.SaveFeature(f)
	return ws
}

func TestRunLoop_HappyPath(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
	if final.Round != 1 {
		t.Errorf("round = %d, want 1", final.Round)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_TestPassMissingArtifactsStopsBeforeAccepting(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-missing-test-artifacts")
	feature, _ := ws.LoadFeature("feat-missing-test-artifacts")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-missing-test-artifacts", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-missing-test-artifacts", s)

	mock := &mockRunner{ws: ws, featureID: "feat-missing-test-artifacts", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true, omitCommitPlan: true},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-missing-test-artifacts")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("feature should stop when tester artifacts are incomplete")
	}
	if !strings.Contains(final.StopReason, protocol.CommitPlan) {
		t.Errorf("stopReason = %q, want missing commit-plan detail", final.StopReason)
	}
	if len(mock.phases) != 4 {
		t.Fatalf("ran %d phases, want 4 before accepting: %v", len(mock.phases), mock.phases)
	}
	for _, phase := range mock.phases {
		if phase == protocol.PhaseAccepting {
			t.Fatal("accepting runner should not execute when tester artifacts are incomplete")
		}
	}
}

func TestRunLoop_ReviewFailLoop(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "FAIL"}, {},
		{reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
	if final.Round != 2 {
		t.Errorf("round = %d, want 2", final.Round)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseAmending, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_TestFailLoop(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: false},
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
	if final.Round != 2 {
		t.Errorf("round = %d, want 2", final.Round)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAmending, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_EscalationFromCoder(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {escalation: true},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive after escalation")
	}
	if final.StopReason != "blocker" {
		t.Errorf("stopReason = %q, want blocker", final.StopReason)
	}
}

func TestRunLoop_EscalationFromTester(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {escalation: true},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive after escalation")
	}
	if final.StopReason != "blocker" {
		t.Errorf("stopReason = %q, want blocker", final.StopReason)
	}
}

func TestRunLoop_MaxRoundsStop(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 3, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "FAIL"}, {}, {reviewVerdict: "FAIL"},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive at max rounds")
	}
	if final.StopReason == "" {
		t.Error("stopReason should be set")
	}
}

func TestRunLoop_BaselineCaptured(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-1")
	feature, _ := ws.LoadFeature("feat-1")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-1", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-1", s)

	mock := &mockRunner{ws: ws, featureID: "feat-1", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never")

	baselinePath := filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile)
	if _, err := os.Stat(baselinePath); os.IsNotExist(err) {
		t.Error("baseline.json should be created before first coding")
	}
	if len(mock.baselineAtCoding) != 1 {
		t.Fatalf("coding invocations = %d, want 1", len(mock.baselineAtCoding))
	}
	if !mock.baselineAtCoding[0] {
		t.Error("baseline.json should exist before Coder runner starts")
	}
}

func TestRunLoop_BaselinePreservesExistingRoundOne(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-existing")
	feature, _ := ws.LoadFeature("feat-existing")
	cfg, _ := ws.ReadConfig()

	baselinePath := filepath.Join(ws.FeatureDir("feat-existing"), protocol.BaselineFile)
	const existingBaseline = `{"repos":[{"name":"sentinel"}]}`
	if err := os.WriteFile(baselinePath, []byte(existingBaseline), 0o644); err != nil {
		t.Fatal(err)
	}

	s := protocol.State{
		FeatureID: "feat-existing", Phase: protocol.PhaseCoding, Role: protocol.RoleCoder,
		Round: 1, MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-existing", s)

	mock := &mockRunner{ws: ws, featureID: "feat-existing", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existingBaseline {
		t.Errorf("baseline overwritten: %s", string(data))
	}
	if len(mock.baselineAtCoding) != 1 || !mock.baselineAtCoding[0] {
		t.Fatalf("baseline should be available to coder, got %v", mock.baselineAtCoding)
	}
}

func TestRunLoop_BaselineNotRepeatedAfterRoundOne(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-round-2")
	feature, _ := ws.LoadFeature("feat-round-2")
	cfg, _ := ws.ReadConfig()

	baselinePath := filepath.Join(ws.FeatureDir("feat-round-2"), protocol.BaselineFile)
	const existingBaseline = `{"repos":[{"name":"round-one"}]}`
	if err := os.WriteFile(baselinePath, []byte(existingBaseline), 0o644); err != nil {
		t.Fatal(err)
	}

	s := protocol.State{
		FeatureID: "feat-round-2", Phase: protocol.PhaseAmending, Role: protocol.RoleCoder,
		Round: 2, MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-round-2", s)

	mock := &mockRunner{ws: ws, featureID: "feat-round-2", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existingBaseline {
		t.Errorf("baseline overwritten after round 1: %s", string(data))
	}
}

func TestRunLoop_BaselineUsesFeatureRepoScope(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "backend")
	os.MkdirAll(repoDir, 0o755)

	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s - %v", args, out, err)
		}
	}

	gitRun(repoDir, "git", "init")
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main"), 0o644)
	gitRun(repoDir, "git", "add", ".")
	gitRun(repoDir, "git", "commit", "-m", "init")

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "baseline-scope-test"},
		Default: "mock",
		Runners: map[string]protocol.RunnerConfig{"mock": {Command: "echo"}},
		ModelTiers: map[string]map[string]string{
			"opus":   {"mock": "mock-opus"},
			"sonnet": {"mock": "mock-sonnet"},
			"haiku":  {"mock": "mock-haiku"},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-scope"); err != nil {
		t.Fatal(err)
	}
	f := feature.Feature{
		ID:     "feat-scope",
		Name:   "Scope Test",
		Status: "not-started",
		Repos:  []string{"backend"},
	}
	ws.SaveFeature(f)

	feature, _ := ws.LoadFeature("feat-scope")

	s := protocol.State{
		FeatureID: "feat-scope", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-scope", s)

	mock := &mockRunner{ws: ws, featureID: "feat-scope", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-scope"), protocol.BaselineFile))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if len(baseline.Repos) != 1 {
		t.Fatalf("repos count = %d, want 1", len(baseline.Repos))
	}
	if baseline.Repos[0].Name != "backend" {
		t.Errorf("repo name = %q, want %q", baseline.Repos[0].Name, "backend")
	}
	if baseline.Repos[0].Head == "" {
		t.Error("repo HEAD should not be empty")
	}
	if baseline.Repos[0].Branch == "" {
		t.Error("repo branch should not be empty")
	}
}

func TestRunLoop_BaselineFailureBlocksCoder(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-baseline-fail")
	feature, _ := ws.LoadFeature("feat-baseline-fail")
	cfg, _ := ws.ReadConfig()

	baselinePath := filepath.Join(ws.FeatureDir("feat-baseline-fail"), protocol.BaselineFile)
	if err := os.Mkdir(baselinePath, 0o755); err != nil {
		t.Fatal(err)
	}

	s := protocol.State{
		FeatureID: "feat-baseline-fail", Phase: protocol.PhaseCoding, Role: protocol.RoleCoder,
		Round: 1, MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-baseline-fail", s)

	mock := &mockRunner{ws: ws, featureID: "feat-baseline-fail"}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err == nil {
		t.Fatal("runLoop should fail when baseline cannot be captured")
	}
	if len(mock.phases) != 0 {
		t.Fatalf("Coder runner should not execute after baseline failure, phases: %v", mock.phases)
	}
}

func TestParseReviewVerdict(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantPassed   bool
		wantCritical int
	}{
		{"pass", "## Verdict\nPASS — looks good\n", true, 0},
		{"fail", "## Verdict\nFAIL — fix bugs\n", false, 0},
		{"conditional pass", "## Verdict\nCONDITIONAL PASS — minor issues\n", true, 0},
		{"no verdict section", "# Review\n\nSome text\n", false, 0},
		{"empty", "", false, 0},
		{
			"pass with critical",
			"# Review\n\n### [CRITICAL] SQL injection — db.go\n\n## Verdict\nPASS — looks good\n",
			true, 1,
		},
		{
			"pass with multiple criticals",
			"### [CRITICAL] Bug 1 — a.go\n\n### [CRITICAL] Bug 2 — b.go\n\n## Verdict\nPASS\n",
			true, 2,
		},
		{
			"fail with critical",
			"### [CRITICAL] Fatal flaw — c.go\n\n## Verdict\nFAIL — fix bugs\n",
			false, 1,
		},
		{
			"conditional pass with critical",
			"### [CRITICAL] Race condition — d.go\n\n## Verdict\nCONDITIONAL PASS — needs fix\n",
			true, 1,
		},
		{
			"critical case insensitive lowercase",
			"### [critical] minor labeled critical — e.go\n\n## Verdict\nPASS\n",
			true, 1,
		},
		{
			"critical case insensitive mixed",
			"### [Critical] mixed case — f.go\n\n## Verdict\nPASS\n",
			true, 1,
		},
		{
			"no verdict with critical",
			"# Review\n\n### [CRITICAL] Issue — g.go\n",
			false, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReviewVerdict(tt.content)
			if got.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", got.Passed, tt.wantPassed)
			}
			if got.CriticalCount != tt.wantCritical {
				t.Errorf("CriticalCount = %d, want %d", got.CriticalCount, tt.wantCritical)
			}
		})
	}
}

func TestRunLoop_StaleArtifactsCleanedOnTestingEntry(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-stale")
	feature, _ := ws.LoadFeature("feat-stale")
	cfg, _ := ws.ReadConfig()

	featureDir := ws.FeatureDir("feat-stale")

	// 從 reviewing phase 開始，review 已 pass，下一步是 testing。
	// 預先放入 stale feature-level artifact，模擬上一輪 tester 遺留。
	os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Stale"), 0o644)
	os.WriteFile(filepath.Join(featureDir, protocol.CommitPlan), []byte("# Stale"), 0o644)
	os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
	os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)

	// 預先放 round-1 review report（pass）讓 nextPhaseAfter 進入 testing
	round1Dir := ws.RoundDir("feat-stale", 1)
	os.MkdirAll(round1Dir, 0o755)
	os.WriteFile(filepath.Join(round1Dir, protocol.ReviewReport),
		[]byte("## Verdict\nPASS\n"), 0o644)

	s := protocol.State{
		FeatureID: "feat-stale", Phase: protocol.PhaseReviewing, Role: protocol.RoleReviewer,
		Round: 1, MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-stale", s)

	// mock：reviewing（已有 report）→ testing → accepting
	mock := &mockRunner{ws: ws, featureID: "feat-stale", outcomes: []mockOutcome{
		{reviewVerdict: "PASS"}, // reviewing（mock 會覆寫 report，但結果不變）
		{testPassed: true},      // testing
		{},                      // accepting
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-stale")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	// 驗證 testing 階段入口有清除 stale artifact（新 tester 會重新寫入非 stale 內容）
	data, err := os.ReadFile(filepath.Join(featureDir, protocol.FinalReport))
	if err != nil {
		t.Fatalf("final-report.md should exist after successful run: %v", err)
	}
	if string(data) == "# Stale" {
		t.Error("stale final-report.md was not cleaned before testing phase")
	}
}

func TestRunLoop_SeverityGate_PassWithCritical(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sg")
	feature, _ := ws.LoadFeature("feat-sg")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-sg", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-sg", s)

	mock := &mockRunner{ws: ws, featureID: "feat-sg", outcomes: []mockOutcome{
		{},
		{},
		{reviewVerdict: "PASS", criticalIssues: 1},
		{},
		{reviewVerdict: "PASS"},
		{testPassed: true},
		{},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseAmending, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_SeverityGate_FailWithCritical(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sg2")
	feature, _ := ws.LoadFeature("feat-sg2")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-sg2", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-sg2", s)

	mock := &mockRunner{ws: ws, featureID: "feat-sg2", outcomes: []mockOutcome{
		{},
		{},
		{reviewVerdict: "FAIL", criticalIssues: 2},
		{},
		{reviewVerdict: "PASS"},
		{testPassed: true},
		{},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg2")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseAmending, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_SeverityGate_ConditionalPassWithCritical(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sg3")
	feature, _ := ws.LoadFeature("feat-sg3")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-sg3", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-sg3", s)

	mock := &mockRunner{ws: ws, featureID: "feat-sg3", outcomes: []mockOutcome{
		{},
		{},
		{reviewVerdict: "CONDITIONAL PASS", criticalIssues: 1},
		{},
		{reviewVerdict: "PASS"},
		{testPassed: true},
		{},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg3")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseAmending, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

func TestRunLoop_WorktreeSync(t *testing.T) {
	mainWs := setupLoopWorkspace(t, "feat-wt")
	feature, _ := mainWs.LoadFeature("feat-wt")
	cfg, _ := mainWs.ReadConfig()

	wtRoot := t.TempDir()
	protocol.Init(wtRoot, cfg)
	wtWs := &protocol.Workspace{Root: wtRoot}

	mock := &mockRunner{ws: wtWs, featureID: "feat-wt", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	s := protocol.State{
		FeatureID: "feat-wt", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	mainWs.WriteState("feat-wt", s)

	if err := runLoop(context.Background(), mainWs, wtWs, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	brief := filepath.Join(mainWs.FeatureDir("feat-wt"), protocol.TaskBrief)
	if _, err := os.Stat(brief); err != nil {
		t.Errorf("task-brief.md not synced back to main workspace")
	}
	coderReport := filepath.Join(mainWs.RoundDir("feat-wt", 1), protocol.CoderReport)
	if _, err := os.Stat(coderReport); err != nil {
		t.Errorf("coder-report.md not synced back to main workspace")
	}
	reviewReport := filepath.Join(mainWs.RoundDir("feat-wt", 1), protocol.ReviewReport)
	if _, err := os.Stat(reviewReport); err != nil {
		t.Errorf("review-report.md not synced back to main workspace")
	}
}

func TestCommitWorktree(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, string(out), err)
		}
	}

	run(root, "git", "init")
	run(root, "git", "config", "user.email", "test@test.com")
	run(root, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	run(root, "git", "add", ".")
	run(root, "git", "commit", "-m", "init")

	wtPath := filepath.Join(root, ".worktrees", "F099-test")
	run(root, "git", "worktree", "add", wtPath, "-b", "4x/F099-test")

	os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644)

	if err := gitops.New(root, nil, protocol.Config{}).Commit(wtPath, "F099-test", "wip(F099-test): round 1"); err != nil {
		t.Fatalf("commitWorktree: %v", err)
	}

	out, err := exec.Command("git", "-C", wtPath, "log", "--oneline", "-1").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "wip(F099-test): round 1") {
		t.Errorf("commit message = %q, want wip(F099-test): round 1", strings.TrimSpace(string(out)))
	}
}

func TestCommitWorktree_NoChanges(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, string(out), err)
		}
	}

	run(root, "git", "init")
	run(root, "git", "config", "user.email", "test@test.com")
	run(root, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	run(root, "git", "add", ".")
	run(root, "git", "commit", "-m", "init")

	wtPath := filepath.Join(root, ".worktrees", "F100-empty")
	run(root, "git", "worktree", "add", wtPath, "-b", "4x/F100-empty")

	if err := gitops.New(root, nil, protocol.Config{}).Commit(wtPath, "F100-empty", "feat(F100-empty): Empty"); err != nil {
		t.Fatalf("commitWorktree should succeed with no changes: %v", err)
	}
}

func TestRunLoopEventsCarryRunnerModel(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-evt-rm")

	s := protocol.State{
		FeatureID: "feat-evt-rm",
		Phase:     protocol.PhaseInit,
		MaxRounds: 5,
		Active:    true,
		Runner:    "mock",
		Runners:   []string{"mock"},
	}
	ws.WriteState("feat-evt-rm", s)

	feature, _ := ws.LoadFeature("feat-evt-rm")
	cfg, _ := ws.ReadConfig()
	cfg.Roles = map[string]protocol.RoleConfig{
		"designer": {Model: "opus"},
	}

	mock := &mockRunner{ws: ws, featureID: "feat-evt-rm", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never")

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-evt-rm"), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	hasRunnerEvent := false
	for _, line := range lines {
		var evt protocol.Event
		json.Unmarshal([]byte(line), &evt)
		if evt.Type == "phase-start" && evt.Runner != "" {
			hasRunnerEvent = true
			if evt.Runner != "mock" {
				t.Errorf("event runner = %q, want 'mock'", evt.Runner)
			}
		}
	}
	if !hasRunnerEvent {
		t.Error("no phase-start event with runner field found")
	}
}

func TestCommitWorktree_OnDone(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, string(out), err)
		}
	}

	run(root, "git", "init")
	run(root, "git", "config", "user.email", "test@test.com")
	run(root, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	run(root, "git", "add", ".")
	run(root, "git", "commit", "-m", "init")

	wtPath := filepath.Join(root, ".worktrees", "F101-done")
	run(root, "git", "worktree", "add", wtPath, "-b", "4x/F101-done")

	os.WriteFile(filepath.Join(wtPath, "feat.go"), []byte("package main\n"), 0o644)

	if err := gitops.New(root, nil, protocol.Config{}).Commit(wtPath, "F101-done", "feat(F101-done): Done Feature"); err != nil {
		t.Fatalf("commitWorktree: %v", err)
	}

	out, err := exec.Command("git", "-C", wtPath, "log", "--oneline", "-1").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "feat(F101-done): Done Feature") {
		t.Errorf("commit message = %q, want feat(F101-done): Done Feature", strings.TrimSpace(string(out)))
	}
}

func TestRunLoop_MergedConfig(t *testing.T) {
	root := t.TempDir()

	// project config 沒有 runners，只設 default runner 名稱
	projectCfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "merge-test"},
		Default: "mock",
		ModelTiers: map[string]map[string]string{
			"opus":   {"mock": "mock-opus"},
			"sonnet": {"mock": "mock-sonnet"},
		},
	}
	if err := protocol.Init(root, projectCfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	featureID := "feat-merge"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	ws.SaveFeature(feature.Feature{ID: featureID, Name: "Merge Test", Status: "not-started"})

	// user config 有 runner 定義，project config 沒有
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)
	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	userCfg := protocol.UserConfig{
		Runners: map[string]protocol.RunnerConfig{
			"mock": {Command: "echo"},
		},
	}
	if err := protocol.WriteUserConfig(userCfg); err != nil {
		t.Fatal(err)
	}

	// 模擬 run.go 的 merge 流程
	cfg, _ := ws.ReadConfig()
	uCfg, _ := protocol.ReadUserConfig()
	merged := protocol.MergeConfig(uCfg, cfg)

	// 驗證 user-only runner 被 merge 進來
	rc, ok := merged.Runners["mock"]
	if !ok {
		t.Fatal("merged config should have mock runner from user config")
	}
	if rc.Command != "echo" {
		t.Errorf("Command = %q, want echo", rc.Command)
	}

	// 驗證 runLoop 以 merged config 正常運作
	feature, _ := ws.LoadFeature(featureID)
	s := protocol.State{
		FeatureID: featureID, Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState(featureID, s)

	mock := &mockRunner{ws: ws, featureID: featureID}
	if err := runLoop(context.Background(), ws, ws, feature, merged, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop with merged config should succeed: %v", err)
	}

	final, _ := ws.ReadState(featureID)
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
}

func TestRunLoop_DeepReviewExecuted(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-deep")
	feature, _ := ws.LoadFeature("feat-deep")
	cfg, _ := ws.ReadConfig()
	cfg.Roles = map[string]protocol.RoleConfig{
		"reviewer": {Model: "sonnet", DeepModel: "opus"},
	}

	s := protocol.State{
		FeatureID: "feat-deep", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-deep", s)

	mock := &mockRunner{ws: ws, featureID: "feat-deep", outcomes: []mockOutcome{
		{},                      // designing
		{},                      // coding
		{reviewVerdict: "PASS"}, // reviewing
		{testPassed: true},      // testing
		{reviewVerdict: "PASS"}, // deep-reviewing
		{},                      // accepting
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-deep")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseDeepReviewing, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

// TestRunLoop_DeepReviewSelfHeal 驗證 F063：deep reviewer FAIL 時不回主迴圈重跑整條流程，
// 而是在 deep-reviewing phase 內 spawn mini-coder + re-verifier 自癒，re-verifier 把 Verdict
// 改 PASS 後直接放行 accepting。reviewer / tester 不重跑。
func TestRunLoop_DeepReviewSelfHeal(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-deep-fail")
	feature, _ := ws.LoadFeature("feat-deep-fail")
	cfg, _ := ws.ReadConfig()
	cfg.Roles = map[string]protocol.RoleConfig{
		"reviewer": {Model: "sonnet", DeepModel: "opus"},
	}

	s := protocol.State{
		FeatureID: "feat-deep-fail", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-deep-fail", s)

	mock := &mockRunner{ws: ws, featureID: "feat-deep-fail", outcomes: []mockOutcome{
		{},                      // designer
		{},                      // coder
		{reviewVerdict: "PASS"}, // reviewer pass
		{testPassed: true},      // tester pass
		{reviewVerdict: "FAIL"}, // deep-reviewer FAIL → self-heal
		{},                      // mini-coder (writes coder-report)
		{reviewVerdict: "PASS"}, // re-verifier → deep-review-report PASS
		{},                      // acceptor
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-deep-fail")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
	if final.Round != 1 {
		t.Errorf("round = %d, want 1 (self-heal must not increment round)", final.Round)
	}

	wantRoles := []protocol.Role{
		protocol.RoleDesigner, protocol.RoleCoder, protocol.RoleReviewer,
		protocol.RoleTester, protocol.RoleDeepReviewer, protocol.RoleMiniCoder,
		protocol.RoleReVerifier, protocol.RoleAcceptor,
	}
	if len(mock.roles) != len(wantRoles) {
		t.Fatalf("ran %d roles, want %d: %v", len(mock.roles), len(wantRoles), mock.roles)
	}
	for i, r := range wantRoles {
		if mock.roles[i] != r {
			t.Errorf("role[%d] = %s, want %s", i, mock.roles[i], r)
		}
	}

	// reviewer 與 tester 各只跑一次（沒有因 deep review 內部修正而重跑）。
	reviewerCount, testerCount := 0, 0
	for _, r := range mock.roles {
		switch r {
		case protocol.RoleReviewer:
			reviewerCount++
		case protocol.RoleTester:
			testerCount++
		}
	}
	if reviewerCount != 1 || testerCount != 1 {
		t.Errorf("reviewer ran %d, tester ran %d; both should run exactly once", reviewerCount, testerCount)
	}
}

// TestRunLoop_DeepReviewSelfHealExhausted 驗證自癒循環跑滿 max_fix_rounds 仍 FAIL 時，
// escalate 到 needs-attention 並維持 FAIL 報告，不會無限重跑。
func TestRunLoop_DeepReviewSelfHealExhausted(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-deep-exhaust")
	feature, _ := ws.LoadFeature("feat-deep-exhaust")
	cfg, _ := ws.ReadConfig()
	cfg.Roles = map[string]protocol.RoleConfig{
		"reviewer":      {Model: "sonnet", DeepModel: "opus"},
		"deep-reviewer": {MaxFixRounds: 2},
	}

	s := protocol.State{
		FeatureID: "feat-deep-exhaust", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-deep-exhaust", s)

	mock := &mockRunner{ws: ws, featureID: "feat-deep-exhaust", outcomes: []mockOutcome{
		{},                      // designer
		{},                      // coder
		{reviewVerdict: "PASS"}, // reviewer pass
		{testPassed: true},      // tester pass
		{reviewVerdict: "FAIL"}, // deep-reviewer FAIL → self-heal iter 1
		{},                      // mini-coder iter 1
		{reviewVerdict: "FAIL"}, // re-verifier iter 1 still FAIL → iter 2
		{},                      // mini-coder iter 2
		{reviewVerdict: "FAIL"}, // re-verifier iter 2 still FAIL → exhausted
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-deep-exhaust")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if !strings.Contains(final.StopReason, "exhausted") {
		t.Errorf("stopReason = %q, want it to mention exhausted", final.StopReason)
	}

	wantRoles := []protocol.Role{
		protocol.RoleDesigner, protocol.RoleCoder, protocol.RoleReviewer,
		protocol.RoleTester, protocol.RoleDeepReviewer,
		protocol.RoleMiniCoder, protocol.RoleReVerifier,
		protocol.RoleMiniCoder, protocol.RoleReVerifier,
	}
	if len(mock.roles) != len(wantRoles) {
		t.Fatalf("ran %d roles, want %d: %v", len(mock.roles), len(wantRoles), mock.roles)
	}
	for i, r := range wantRoles {
		if mock.roles[i] != r {
			t.Errorf("role[%d] = %s, want %s", i, mock.roles[i], r)
		}
	}

	// deep-review-report.md 必須維持 FAIL。
	if reviewPassed(ws, "feat-deep-exhaust", 1, protocol.DeepReviewReport) {
		t.Error("deep-review-report should remain FAIL after exhaustion")
	}
}

// roleAwareOps 只在當前 state.Role 等於 failForRole 時回報 out-of-scope，
// 用於模擬「正常 coder 在 scope 內，但 mini-coder 超出 scope」的情境。
type roleAwareOps struct {
	mockOps
	ws          *protocol.Workspace
	featureID   string
	failForRole protocol.Role
}

func (m *roleAwareOps) DetectChangedRepos() []string {
	s, _ := m.ws.ReadState(m.featureID)
	if s.Role == m.failForRole {
		return []string{"out-of-scope-repo"}
	}
	return nil
}

// TestRunLoop_DeepReviewMiniCoderScopeExceed 驗證：mini-coder 改動超出原始 scope 時，
// 自癒循環立即停下、寫出 FAIL 報告與 escalation，並轉入 needs-attention（不繼續修）。
func TestRunLoop_DeepReviewMiniCoderScopeExceed(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-deep-scope")
	f := feature.Feature{ID: "feat-deep-scope", Name: "Deep Scope Test", Status: "not-started", Repos: []string{"allowed-repo"}}
	ws.SaveFeature(f)
	feature, _ := ws.LoadFeature("feat-deep-scope")
	cfg, _ := ws.ReadConfig()
	cfg.Roles = map[string]protocol.RoleConfig{
		"reviewer": {Model: "sonnet", DeepModel: "opus"},
	}

	s := protocol.State{
		FeatureID: "feat-deep-scope", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-deep-scope", s)

	mock := &mockRunner{ws: ws, featureID: "feat-deep-scope", outcomes: []mockOutcome{
		{},                      // designer
		{},                      // coder
		{reviewVerdict: "PASS"}, // reviewer pass
		{testPassed: true},      // tester pass
		{reviewVerdict: "FAIL"}, // deep-reviewer FAIL → self-heal
		{},                      // mini-coder (will trip scope guard)
	}}
	ops := &roleAwareOps{ws: ws, featureID: "feat-deep-scope", failForRole: protocol.RoleMiniCoder}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, ops, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-deep-scope")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if !strings.Contains(final.StopReason, "scope-exceed") {
		t.Errorf("stopReason = %q, want it to mention scope-exceed", final.StopReason)
	}

	// re-verifier 不應執行（scope-exceed 立刻停止）。
	for _, r := range mock.roles {
		if r == protocol.RoleReVerifier {
			t.Error("re-verifier should not run after mini-coder scope-exceed")
		}
	}

	// deep-review-report 應被改寫為 FAIL，且 escalation.json 應存在。
	if reviewPassed(ws, "feat-deep-scope", 1, protocol.DeepReviewReport) {
		t.Error("deep-review-report should be FAIL after scope-exceed")
	}
	esc := readEscalation(ws, "feat-deep-scope", 1)
	if !esc.Needed || esc.Reason != "scope-change" {
		t.Errorf("escalation = %+v, want needed scope-change", esc)
	}
}

// mockOps 實作 gitops.Ops 介面，讓測試可注入可控的 ScopeDetector。
type mockOps struct {
	changedRepos []string
}

func (m *mockOps) SetupWorktree(_ string) (string, error)     { return "", nil }
func (m *mockOps) Commit(_, _, _ string) error                { return nil }
func (m *mockOps) Merge(_, _ string) gitops.MergeResult       { return gitops.MergeResult{} }
func (m *mockOps) Cleanup(_ string) error                     { return nil }
func (m *mockOps) DetectChangedRepos() []string               { return m.changedRepos }
func (m *mockOps) CaptureBaseline(_ string, _ []string) error { return nil }
func (m *mockOps) IsMultiRepo() bool                          { return false }

// TestRunLoop_GuardFailStopsLoop 驗證：非 designer runner 完成後若 guard.Check 回傳 Pass==false，
// loop 立即停止並轉入 needs-attention，StopReason 包含 guard error 摘要。
func TestRunLoop_GuardFailStopsLoop(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-guard-fail")
	// 覆寫 feature：設 Repos 使 scope check 有作用
	f := feature.Feature{ID: "feat-guard-fail", Name: "Guard Fail Test", Status: "not-started", Repos: []string{"allowed-repo"}}
	ws.SaveFeature(f)
	feature, _ := ws.LoadFeature("feat-guard-fail")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-guard-fail", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-guard-fail", s)

	// designer 和 coder 均成功；注入的 ops 讓 guard 在 coder 後失敗
	mock := &mockRunner{ws: ws, featureID: "feat-guard-fail", outcomes: []mockOutcome{
		{},
		{},
	}}
	ops := &mockOps{changedRepos: []string{"out-of-scope-repo"}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, ops, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-guard-fail")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("feature should be inactive after guard failure")
	}
	if !strings.Contains(final.StopReason, "scope violation") {
		t.Errorf("stopReason = %q, want scope violation detail", final.StopReason)
	}
	// 確認 designer 和 coder 都有執行（guard 跳過 designer、在 coder 後失敗）
	if len(mock.phases) < 2 {
		t.Fatalf("expected designing + coding, got: %v", mock.phases)
	}
	if mock.phases[0] != protocol.PhaseDesigning {
		t.Errorf("phases[0] = %s, want designing", mock.phases[0])
	}
	if mock.phases[1] != protocol.PhaseCoding {
		t.Errorf("phases[1] = %s, want coding", mock.phases[1])
	}
}

// TestRunLoop_DesignerSkipsGuard 驗證：designer phase 完成後不呼叫 guard.Check，
// 即使 guard 一定失敗，loop 仍能繼續到下一個 phase（coder）。
func TestRunLoop_DesignerSkipsGuard(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-designer-skip-guard")
	f := feature.Feature{ID: "feat-designer-skip-guard", Name: "Designer Skip Guard", Status: "not-started", Repos: []string{"allowed-repo"}}
	ws.SaveFeature(f)
	feature, _ := ws.LoadFeature("feat-designer-skip-guard")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-designer-skip-guard", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-designer-skip-guard", s)

	mock := &mockRunner{ws: ws, featureID: "feat-designer-skip-guard", outcomes: []mockOutcome{
		{},
		{},
	}}
	// guard 一定失敗：任何 scope 都被標記為 out-of-scope
	ops := &mockOps{changedRepos: []string{"out-of-scope-repo"}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, ops, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	// designer 跳過 guard → loop 繼續到 coder → coder 後 guard 失敗 → needs-attention
	// 若 designer 誤觸 guard，loop 在 designing 後即停，coding 不會出現在 mock.phases
	final, _ := ws.ReadState("feat-designer-skip-guard")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention (guard failed at coder)", final.Phase)
	}
	sawCoding := false
	for _, p := range mock.phases {
		if p == protocol.PhaseCoding {
			sawCoding = true
			break
		}
	}
	if !sawCoding {
		t.Errorf("coding phase never ran — guard may have incorrectly blocked designer; phases = %v", mock.phases)
	}
}

// readEventTypes 讀取 events.jsonl，回傳指定 type 的所有 event。
func readEventTypes(t *testing.T, ws *protocol.Workspace, featureID, evtType string) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var out []protocol.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var evt protocol.Event
		if json.Unmarshal([]byte(line), &evt) == nil && evt.Type == evtType {
			out = append(out, evt)
		}
	}
	return out
}

// TestRunLoop_QuickProfile 驗證 quick profile（coder+reviewer）：designer/tester/
// deep-reviewer/acceptor 都被 pass-through，最終落 pending-review，並產生 phase-skipped event。
func TestRunLoop_QuickProfile(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-quick")
	feature, _ := ws.LoadFeature("feat-quick")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-quick", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock", Profile: "quick",
	}
	ws.WriteState("feat-quick", s)

	mock := &mockRunner{ws: ws, featureID: "feat-quick", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-quick")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}
	if final.Profile != "quick" {
		t.Errorf("profile = %q, want quick", final.Profile)
	}

	wantPhases := []protocol.Phase{protocol.PhaseCoding, protocol.PhaseReviewing}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}

	skipped := readEventTypes(t, ws, "feat-quick", "phase-skipped")
	skippedRoles := map[protocol.Role]bool{}
	for _, e := range skipped {
		skippedRoles[e.Role] = true
	}
	for _, r := range []protocol.Role{protocol.RoleDesigner, protocol.RoleTester, protocol.RoleDeepReviewer, protocol.RoleAcceptor} {
		if !skippedRoles[r] {
			t.Errorf("expected phase-skipped event for role %s", r)
		}
	}
}

// TestRunLoop_NormalProfile 驗證 normal profile（coder/reviewer/tester/acceptor）：
// designer 被跳過，designing→coding 不因缺 task-brief 而 needs-attention，deep-reviewer 亦跳過。
func TestRunLoop_NormalProfile(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-normal")
	feature, _ := ws.LoadFeature("feat-normal")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-normal", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock", Profile: "normal",
	}
	ws.WriteState("feat-normal", s)

	mock := &mockRunner{ws: ws, featureID: "feat-normal", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-normal")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review (designer skip must not cause needs-attention)", final.Phase)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
}

// TestRunLoop_ProfileReviewFailSkipsTester 驗證 reviewer FAIL 時直接 amending，
// tester 不被執行（無 testing phase 的 run-end，且 amending 緊接在 reviewing 後）。
func TestRunLoop_ProfileReviewFailSkipsTester(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-rf")
	feature, _ := ws.LoadFeature("feat-rf")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-rf", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock", Profile: "normal",
	}
	ws.WriteState("feat-rf", s)

	mock := &mockRunner{ws: ws, featureID: "feat-rf", outcomes: []mockOutcome{
		{}, {reviewVerdict: "FAIL"}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string) runner.Runner { return mock }, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	wantPhases := []protocol.Phase{
		protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseAmending,
		protocol.PhaseReviewing, protocol.PhaseTesting, protocol.PhaseAccepting,
	}
	if len(mock.phases) != len(wantPhases) {
		t.Fatalf("ran %d phases, want %d: %v", len(mock.phases), len(wantPhases), mock.phases)
	}
	for i, p := range wantPhases {
		if mock.phases[i] != p {
			t.Errorf("phase[%d] = %s, want %s", i, mock.phases[i], p)
		}
	}
	// 確認 tester 沒有在第一次 reviewing FAIL 後立即執行（index 2 應為 amending）。
	if mock.phases[2] != protocol.PhaseAmending {
		t.Errorf("after review FAIL expected amending, got %s", mock.phases[2])
	}
}

// roleMockRunner 是 role-aware 的 mock runner：依 logPath 偵測 role 並寫對應 artifact。
// 用於平行 review/test 測試（reviewer 與 tester 同時跑、phase 皆為 reviewing，無法用
// phase 區分），且 concurrency-safe。
type roleMockRunner struct {
	ws        *protocol.Workspace
	featureID string
	role      string
	mu        *sync.Mutex
	started   *[]string
}

func (m *roleMockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	s, _ := m.ws.ReadState(m.featureID)
	round := s.Round
	roundDir := m.ws.RoundDir(m.featureID, round)
	os.MkdirAll(roundDir, 0o755)
	featureDir := m.ws.FeatureDir(m.featureID)

	m.mu.Lock()
	*m.started = append(*m.started, m.role)
	m.mu.Unlock()

	switch m.role {
	case "designer":
		os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)
	case "coder":
		os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Coder Report"), 0o644)
	case "reviewer":
		os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte("## Verdict\nPASS\n"), 0o644)
	case "tester":
		ve := protocol.VerifyEvidence{Passed: true, Round: round}
		data, _ := json.Marshal(ve)
		os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), data, 0o644)
		os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.CommitPlan), []byte("# Commit Plan"), 0o644)
	}
	return &runner.Result{ExitCode: 0}, nil
}

func roleFromLogPath(logPath string) string {
	// deep-reviewer 須在 reviewer 之前比對（前者含後者子字串）。
	for _, r := range []string{"designer", "coder", "deep-reviewer", "reviewer", "tester", "acceptor"} {
		if strings.Contains(logPath, "-"+r+".") {
			return r
		}
	}
	return "unknown"
}

// TestRunLoop_ParallelReviewTest 驗證 parallel_review_test：reviewing phase 同時跑
// reviewer + tester，兩者皆 PASS 後沿合法邊兩跳抵達 deep-reviewing（此處 deep 跳過）→ accepting。
func TestRunLoop_ParallelReviewTest(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-par")
	feature, _ := ws.LoadFeature("feat-par")
	cfg, _ := ws.ReadConfig()
	cfg.ParallelReviewTest = true

	s := protocol.State{
		FeatureID: "feat-par", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock", Profile: "full",
	}
	ws.WriteState("feat-par", s)

	var mu sync.Mutex
	var started []string
	factory := func(logPath, _ string) runner.Runner {
		return &roleMockRunner{ws: ws, featureID: "feat-par", role: roleFromLogPath(logPath), mu: &mu, started: &started}
	}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, factory, "never"); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-par")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review", final.Phase)
	}

	mu.Lock()
	defer mu.Unlock()
	sawReviewer, sawTester := false, false
	for _, r := range started {
		if r == "reviewer" {
			sawReviewer = true
		}
		if r == "tester" {
			sawTester = true
		}
	}
	if !sawReviewer || !sawTester {
		t.Errorf("parallel reviewing should run both reviewer and tester; started=%v", started)
	}

	// reviewing phase 應同時有 reviewer 與 tester 的 phase-start event。
	starts := readEventTypes(t, ws, "feat-par", "phase-start")
	startReviewer, startTester := false, false
	for _, e := range starts {
		if e.Phase == protocol.PhaseReviewing && e.Role == protocol.RoleReviewer {
			startReviewer = true
		}
		if e.Phase == protocol.PhaseReviewing && e.Role == protocol.RoleTester {
			startTester = true
		}
	}
	if !startReviewer || !startTester {
		t.Errorf("parallel reviewing should emit phase-start for both roles (reviewer=%v tester=%v)", startReviewer, startTester)
	}
}

func TestParseReviewVerdict_Warning(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantPassed   bool
		wantCritical int
		wantWarning  int
	}{
		{
			"pass with warning",
			"### [WARNING] Missing edge case — handler.go\n\n## Verdict\nPASS\n",
			true, 0, 1,
		},
		{
			"pass with warning and critical",
			"### [CRITICAL] Bug — a.go\n### [WARNING] Edge case — b.go\n\n## Verdict\nPASS\n",
			true, 1, 1,
		},
		{
			"warning case insensitive",
			"### [warning] minor — c.go\n\n## Verdict\nPASS\n",
			true, 0, 1,
		},
		{
			"inline mention not counted",
			"### [INFO] round-1 [WARNING] 已完整修正\nround-1 [WARNING]（邊界檢查）本輪已無此問題\n\n## Verdict\nPASS\n",
			true, 0, 0,
		},
		{
			"bold markdown verdict",
			"## Verdict\n**PASS**\n",
			true, 0, 0,
		},
		{
			"italic markdown verdict",
			"## Verdict\n*PASS*\n",
			true, 0, 0,
		},
		{
			"bold markdown fail",
			"### [CRITICAL] Bug\n## Verdict\n**FAIL**\n",
			false, 1, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReviewVerdict(tt.content)
			if got.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", got.Passed, tt.wantPassed)
			}
			if got.CriticalCount != tt.wantCritical {
				t.Errorf("CriticalCount = %d, want %d", got.CriticalCount, tt.wantCritical)
			}
			if got.WarningCount != tt.wantWarning {
				t.Errorf("WarningCount = %d, want %d", got.WarningCount, tt.wantWarning)
			}
		})
	}
}
