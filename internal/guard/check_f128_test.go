package guard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestCheckTestingToAccepting_E2EScreenshot(t *testing.T) {
	passingAC := []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"screenshot captured"}}}
	notPassedAC := []protocol.ACEvidence{{ID: "AC-1", Passed: false, Evidence: []string{"failed"}}}
	defaultShot := ".4x/run/feat-1/screenshot/1-foo.png"

	// writeFilePath 為空字串代表不在磁碟寫入任何檔案（驗證「宣稱有截圖但實際不存在」的情境）。
	cases := []struct {
		name, writeFilePath, wantErrSubstr string
		shots                              []feature.Screenshot
		acResults                          []protocol.ACEvidence
		wantPass                           bool
	}{
		{name: "default .4x dir exists", writeFilePath: defaultShot, shots: []feature.Screenshot{{Path: defaultShot}}, acResults: passingAC, wantPass: true},
		{name: "custom dir exists", writeFilePath: "custom/shots/a.png", shots: []feature.Screenshot{{Path: "custom/shots/a.png"}}, acResults: passingAC, wantPass: true},
		{name: "file missing on disk", shots: []feature.Screenshot{{Path: defaultShot}}, acResults: passingAC, wantPass: false, wantErrSubstr: "no screenshot file found"},
		{name: "empty screenshot list", shots: []feature.Screenshot{}, acResults: passingAC, wantPass: false, wantErrSubstr: "no screenshot file found"},
		{name: "AC missing from ac_results", writeFilePath: defaultShot, shots: []feature.Screenshot{{Path: defaultShot}}, acResults: []protocol.ACEvidence{}, wantPass: false, wantErrSubstr: "missing or not passed"},
		{name: "AC present but not passed", writeFilePath: defaultShot, shots: []feature.Screenshot{{Path: defaultShot}}, acResults: notPassedAC, wantPass: false, wantErrSubstr: "missing or not passed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := setupGuardWorkspace(t, "feat-1")
			roundDir, featureDir := ws.RoundDir("feat-1", 1), ws.FeatureDir("feat-1")
			writeFile(t, filepath.Join(featureDir, protocol.TestStratFile),
				"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: e2e-screenshot\n")
			if tc.writeFilePath != "" {
				writeFile(t, filepath.Join(ws.Root, tc.writeFilePath), "fake png bytes")
			}
			data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1, ACResults: tc.acResults, Screenshots: tc.shots})
			writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
			writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
			writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

			result := CheckTestingToAccepting(ws, "feat-1", 1)
			if result.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, want %v; errors: %v", result.Pass, tc.wantPass, result.Errors)
			}
			if tc.wantErrSubstr == "" {
				return
			}
			for _, e := range result.Errors {
				if strings.Contains(e, "AC-1") && strings.Contains(e, tc.wantErrSubstr) {
					return
				}
			}
			t.Errorf("expected error containing %q, got: %v", tc.wantErrSubstr, result.Errors)
		})
	}
}

func TestCheckTestingToAccepting_NoE2EScreenshot_NoRegression(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir, featureDir := ws.RoundDir("feat-1", 1), ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: inspection\n  AC-3: skip\n")
	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1, ACResults: []protocol.ACEvidence{
		{ID: "AC-1", Passed: true, Evidence: []string{"$ go test -run TestFoo → PASS (0.02s)"}},
		{ID: "AC-2", Passed: true, Evidence: []string{"diff shows no API changes"}},
		{ID: "AC-3", Passed: true, Evidence: []string{}},
	}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("test-strategy without e2e-screenshot should not be affected by new check, got: %v", result.Errors)
	}
}

func TestCheckTestStrategyVerifyTypes_E2EScreenshotIsValid(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseDesigning, Round: 0})
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile),
		"verify_commands:\n  - make test\nac_verify_map:\n  AC-1: e2e-screenshot\n")

	result := Check(ws, "feat-1", nil)
	for _, e := range result.Errors {
		if strings.Contains(e, "invalid verify_type") {
			t.Fatalf("e2e-screenshot should be a valid verify_type, got: %v", result.Errors)
		}
	}
}
