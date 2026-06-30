package guard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestCheckTestingToAccepting_ExecutionEvidenceLacksOutput(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"code looks correct at main.go:42"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("execution-type AC with code-only evidence should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "AC-1") && strings.Contains(e, "no execution output") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about AC-1 lacking execution output, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_InspectionEvidencePass(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: inspection\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"git diff shows no API signature changes"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("inspection AC with non-empty evidence should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_SkipVerifyType(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: skip\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("skip AC should pass without evidence, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_NoMapBasicChecksOnly(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"code looks correct at main.go:42"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("no ac_verify_map should pass with basic checks only, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_ExecutionEvidenceWithOutput(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ go test -run TestFoo → PASS (0.02s)"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("execution AC with real output should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_InvalidVerifyType(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: bogus\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("invalid verify_type should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "invalid verify_type") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invalid verify_type error, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_IntegrationVerifyType(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: integration\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ curl localhost:4567/api → 200 OK"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("integration AC with command output should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MixedVerifyTypes(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: inspection\n  AC-3: skip\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ go test -run TestFoo → PASS (0.02s)"}},
			{ID: "AC-2", Passed: true, Evidence: []string{"diff shows no API changes"}},
			{ID: "AC-3", Passed: true, Evidence: []string{}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("mixed verify types with correct evidence should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_UnmappedACDefaultsExecution(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: inspection\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"diff checked"}},
			{ID: "AC-2", Passed: true, Evidence: []string{"code looks correct"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("AC-2 not in ac_verify_map should default to execution and fail on code-only evidence")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "AC-2") && strings.Contains(e, "no execution output") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected AC-2 execution output error, got: %v", result.Errors)
	}
}
