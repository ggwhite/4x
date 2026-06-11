package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

type mockOutcome struct {
	reviewVerdict  string
	criticalIssues int
	testPassed     bool
	escalation     bool
}

type mockRunner struct {
	ws        *protocol.Workspace
	featureID string
	outcomes  []mockOutcome
	idx       int
	phases    []protocol.Phase
}

func (m *mockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	s, _ := m.ws.ReadState(m.featureID)
	m.phases = append(m.phases, s.Phase)

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
			data, _ := json.Marshal(protocol.Escalation{Needed: true, Reason: "spec-mismatch"})
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
		if outcome.testPassed {
			os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final"), 0o644)
		}
		if outcome.escalation {
			data, _ := json.Marshal(protocol.Escalation{Needed: true, Reason: "criteria-wrong"})
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive after escalation")
	}
	if final.StopReason != "spec-mismatch" {
		t.Errorf("stopReason = %q, want spec-mismatch", final.StopReason)
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := ws.ReadState("feat-1")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %s, want needs-attention", final.Phase)
	}
	if final.Active {
		t.Error("should be inactive after escalation")
	}
	if final.StopReason != "criteria-wrong" {
		t.Errorf("stopReason = %q, want criteria-wrong", final.StopReason)
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	runLoop(ws, feature, cfg, s, mock)

	baselinePath := filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile)
	if _, err := os.Stat(baselinePath); os.IsNotExist(err) {
		t.Error("baseline.json should be created before first coding")
	}
}

func TestParseReviewVerdict(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantPassed    bool
		wantCritical  int
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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

	if err := runLoop(ws, feature, cfg, s, mock); err != nil {
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
