package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadTestStrategyACChecks 驗證 test-strategy.yaml 的 ac_checks 區塊能被解析進
// TestStrategy.ACChecks（key=AC ID、value=命令列）。（AC-1）
func TestReadTestStrategyACChecks(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-ts"); err != nil {
		t.Fatal(err)
	}
	content := "verify_commands:\n  - go test ./...\nac_checks:\n  AC-1: [\"echo x\"]\n  AC-2:\n    - go test ./internal/foo -run TestBar\n    - bin/app --help\n"
	path := filepath.Join(ws.FeatureDir("feat-ts"), TestStratFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ts, err := ws.ReadTestStrategy("feat-ts")
	if err != nil {
		t.Fatalf("ReadTestStrategy: %v", err)
	}
	if len(ts.ACChecks) != 2 {
		t.Fatalf("expected 2 ac_checks entries, got %d", len(ts.ACChecks))
	}
	if got := ts.ACChecks["AC-1"]; len(got) != 1 || got[0] != "echo x" {
		t.Errorf("ACChecks[AC-1] = %v, want [echo x]", got)
	}
	if got := ts.ACChecks["AC-2"]; len(got) != 2 || got[0] != "go test ./internal/foo -run TestBar" || got[1] != "bin/app --help" {
		t.Errorf("ACChecks[AC-2] = %v, want the two commands", got)
	}
}

// TestACEvidenceChecksJSON 驗證 ACEvidence.Checks 的 json tag 為 checks,omitempty：
// 含 Checks 時 JSON 出現 "checks"，為空時省略。（AC-2）
func TestACEvidenceChecksJSON(t *testing.T) {
	withChecks := ACEvidence{
		ID:     "AC-1",
		Passed: true,
		Checks: []VerifyCommand{{Command: "go test ./...", ExitCode: 0}},
	}
	data, err := json.Marshal(withChecks)
	if err != nil {
		t.Fatalf("marshal withChecks: %v", err)
	}
	if !strings.Contains(string(data), "\"checks\"") {
		t.Errorf("expected JSON to contain \"checks\", got %s", data)
	}

	noChecks := ACEvidence{ID: "AC-2", Passed: true}
	data2, err := json.Marshal(noChecks)
	if err != nil {
		t.Fatalf("marshal noChecks: %v", err)
	}
	if strings.Contains(string(data2), "\"checks\"") {
		t.Errorf("expected empty Checks to be omitted, got %s", data2)
	}
}
