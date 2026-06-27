package guard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestCheckACEvidence_MissingACResults(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{Passed: true, Round: 1}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("missing ac_results should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "missing ac_results") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'missing ac_results' error, got %v", result.Errors)
	}
}

func TestCheckACEvidence_AllACsHaveEvidence(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"build passed"}},
			{ID: "AC-2", Passed: true, Evidence: []string{"test passed", "output verified"}},
		},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("all ACs have evidence, should pass, got errors: %v", result.Errors)
	}
}

func TestCheckACEvidence_ACMissingEvidence(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"build passed"}},
			{ID: "AC-2", Passed: true, Evidence: []string{}},
			{ID: "AC-3", Passed: false, Evidence: nil},
		},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("AC with empty evidence should fail")
	}

	foundAC2, foundAC3 := false, false
	for _, e := range result.Errors {
		if strings.Contains(e, "AC-2") && strings.Contains(e, "no evidence") {
			foundAC2 = true
		}
		if strings.Contains(e, "AC-3") && strings.Contains(e, "no evidence") {
			foundAC3 = true
		}
	}
	if !foundAC2 {
		t.Errorf("expected error for AC-2, got %v", result.Errors)
	}
	if !foundAC3 {
		t.Errorf("expected error for AC-3, got %v", result.Errors)
	}
}

func TestCheckACEvidence_EmptyACResultsArray(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("empty ac_results array should fail")
	}
}
