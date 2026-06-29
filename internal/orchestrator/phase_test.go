package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func setupPhaseWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "phase-test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID}); err != nil {
		t.Fatal(err)
	}
	return ws
}

func writePhaseFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNextPhaseAfter_TestingGuardRetry(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// test-strategy with manual_checks
	writePhaseFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test\n    steps:\n      - curl localhost\n")

	// verify.json passes automated checks but missing manual_check_results
	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
	})
	writePhaseFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writePhaseFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test\n## Verdict\nPASS")
	writePhaseFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final\n## Verdict\nPASS")

	s := protocol.State{Phase: protocol.PhaseTesting, Round: 1}

	// First call: should retry (return PhaseTesting)
	next, role, stopReason := NextPhaseAfter(ws, "feat-1", s)
	if next != protocol.PhaseTesting {
		t.Fatalf("first guard fail: expected PhaseTesting retry, got %s (reason: %s)", next, stopReason)
	}
	if role != protocol.RoleTester {
		t.Fatalf("expected RoleTester, got %s", role)
	}

	// guard-feedback.json should now exist
	fbPath := filepath.Join(roundDir, protocol.GuardFeedback)
	if _, err := os.Stat(fbPath); err != nil {
		t.Fatalf("guard-feedback.json should exist after retry: %v", err)
	}

	// Second call: guard-feedback exists → needs-attention
	next2, _, stopReason2 := NextPhaseAfter(ws, "feat-1", s)
	if next2 != protocol.PhaseNeedsAttention {
		t.Fatalf("second guard fail: expected NeedsAttention, got %s (reason: %s)", next2, stopReason2)
	}
}

func TestNextPhaseAfter_TestingGuardRetrySucceeds(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// test-strategy with manual_checks
	writePhaseFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test\n    steps:\n      - curl localhost\n")

	// verify.json with manual_check_results present — should pass
	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
		ManualCheckResults: []protocol.ManualCheckResult{
			{ID: "mc-1", Passed: true, Evidence: []string{"curl returned 200"}},
		},
	})
	writePhaseFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writePhaseFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test\n## Verdict\nPASS")
	writePhaseFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final\n## Verdict\nPASS")

	// Even with guard-feedback from previous attempt, should pass if guard is happy
	writePhaseFile(t, filepath.Join(roundDir, protocol.GuardFeedback), `{"errors":["old error"]}`)

	s := protocol.State{Phase: protocol.PhaseTesting, Round: 1}
	next, _, _ := NextPhaseAfter(ws, "feat-1", s)
	if next != protocol.PhaseDeepReviewing {
		t.Fatalf("expected PhaseDeepReviewing when guard passes, got %s", next)
	}
}

func TestNextPhaseAfter_TestingGuardRetryGlobalCap(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writePhaseFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test\n    steps:\n      - curl localhost\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
	})
	writePhaseFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writePhaseFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test\n## Verdict\nPASS")
	writePhaseFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final\n## Verdict\nPASS")

	// GuardRetries already at max → should NOT retry, go straight to needs-attention
	s := protocol.State{Phase: protocol.PhaseTesting, Round: 1, GuardRetries: 2}
	next, _, _ := NextPhaseAfter(ws, "feat-1", s)
	if next != protocol.PhaseNeedsAttention {
		t.Fatalf("expected NeedsAttention when GuardRetries >= MaxGuardRetries, got %s", next)
	}
}

func TestNextPhaseAfter_TestingNonRetryableGoesToNeedsAttention(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// verify.json with W7 mismatch (non-retryable) + missing manual_checks (retryable)
	writePhaseFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test\n    steps:\n      - curl localhost\n")

	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		Commands: []protocol.VerifyCommand{
			{Command: "make test", ExitCode: 1},
		},
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
	}
	data, _ := json.Marshal(ev)
	writePhaseFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writePhaseFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test\n## Verdict\nPASS")
	writePhaseFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final\n## Verdict\nPASS")

	s := protocol.State{Phase: protocol.PhaseTesting, Round: 1}
	next, _, _ := NextPhaseAfter(ws, "feat-1", s)
	// W7 mismatch is non-retryable, so even first fail should go to needs-attention
	if next != protocol.PhaseNeedsAttention {
		t.Fatalf("expected NeedsAttention for non-retryable error, got %s", next)
	}
}
