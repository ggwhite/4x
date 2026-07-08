package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestVerifyACChecksEndToEnd 以真實 spawned subprocess 驗證：test-strategy 含 ac_checks
// （一條 exit 0、一條 exit 1）時，`4x verify` 把每條 AC 的執行結果寫入 verify.json 的 ac_results
// （含真實 exitCode 與由 exit code 決定的 passed），且任一失敗時 verify 回非 0 exit code。（AC-4）
func TestVerifyACChecksEndToEnd(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
		},
		Default: "claude",
	}
	featureID := "F156-e2e"
	dir := setupVerifyWorkspace(t, cfg, featureID)

	ws := &protocol.Workspace{Root: dir}
	tsPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	tsContent := "verify_commands:\n  - echo strategy-ok\nac_verify_map:\n  AC-1: unit-test\n  AC-2: unit-test\nac_checks:\n  AC-1: [\"sh -c 'exit 0'\"]\n  AC-2: [\"sh -c 'exit 1'\"]\n"
	if err := os.WriteFile(tsPath, []byte(tsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// AC-2 exit 1 → verify 整體失敗 → run4x 回 error（非 0 exit code）。
	out, err := run4x(dir, "verify", featureID, "--json")
	if err == nil {
		t.Fatalf("expected non-zero exit code when an ac_check fails, got success\n%s", out)
	}

	verifyPath := filepath.Join(ws.RoundDir(featureID, 1), protocol.VerifyFile)
	data, rerr := os.ReadFile(verifyPath)
	if rerr != nil {
		t.Fatalf("verify.json not created: %v", rerr)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse verify.json: %v", err)
	}

	if ev.Passed {
		t.Error("expected verify.json passed=false when an ac_check fails")
	}

	byID := map[string]protocol.ACEvidence{}
	for _, ac := range ev.ACResults {
		byID[ac.ID] = ac
	}

	ac1, ok := byID["AC-1"]
	if !ok {
		t.Fatal("verify.json ac_results missing AC-1")
	}
	if !ac1.Passed {
		t.Errorf("AC-1 should pass (exit 0), got passed=%v", ac1.Passed)
	}
	if len(ac1.Checks) == 0 {
		t.Fatal("AC-1 should have non-empty checks")
	}
	if ac1.Checks[0].ExitCode != 0 {
		t.Errorf("AC-1 check exit code = %d, want 0", ac1.Checks[0].ExitCode)
	}

	ac2, ok := byID["AC-2"]
	if !ok {
		t.Fatal("verify.json ac_results missing AC-2")
	}
	if ac2.Passed {
		t.Errorf("AC-2 should fail (exit 1), got passed=%v", ac2.Passed)
	}
	if len(ac2.Checks) == 0 {
		t.Fatal("AC-2 should have non-empty checks")
	}
	if ac2.Checks[0].ExitCode != 1 {
		t.Errorf("AC-2 check exit code = %d, want 1", ac2.Checks[0].ExitCode)
	}
}
