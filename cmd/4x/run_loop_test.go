package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	baselineAtCoding []bool
}

func (m *mockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	s, _ := m.ws.ReadState(m.featureID)
	m.phases = append(m.phases, s.Phase)
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
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	f := protocol.Feature{ID: featureID, Name: "Test Feature", Status: "not-started"}
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock })

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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-scope"); err != nil {
		t.Fatal(err)
	}
	f := protocol.Feature{
		ID:     "feat-scope",
		Name:   "Scope Test",
		Status: "not-started",
		Repos:  map[string]string{"backend": "backend"},
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err == nil {
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
		{testPassed: true},     // testing
		{},                     // accepting
	}}

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-stale")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg2")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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

	if err := runLoop(ws, feature, cfg, s, func(_ string) runner.Runner { return mock }); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-sg3")
	if final.Phase != protocol.PhaseDone {
		t.Errorf("phase = %s, want done", final.Phase)
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
