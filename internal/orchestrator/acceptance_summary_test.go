package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestDeepReviewCleanPass(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	featureID := "feat-clean-pass"
	roundDir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"clean pass, no issues", "# Deep Review Report\n\n## Verdict\nPASS\n", true},
		{"pass with warning", "### [WARNING] Something\n\n## Verdict\nPASS\n", false},
		{"conditional pass with warning", "### [WARNING] Something\n\n## Verdict\nCONDITIONAL PASS\n", false},
		{"fail", "### [CRITICAL] Broken\n\n## Verdict\nFAIL\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(roundDir, protocol.DeepReviewReport)
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := DeepReviewCleanPass(ws, featureID, 1); got != tt.want {
				t.Errorf("DeepReviewCleanPass() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeepReviewCleanPass_MissingReport(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	if DeepReviewCleanPass(ws, "feat-missing", 1) {
		t.Error("DeepReviewCleanPass() = true, want false when report missing")
	}
}

func TestGenerateAcceptanceSummary_NoData(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	featureID := "feat-empty"
	if err := os.MkdirAll(ws.RoundDir(featureID, 1), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GenerateAcceptanceSummary(ws, featureID, 1); got != "" {
		t.Errorf("GenerateAcceptanceSummary() = %q, want empty when no artifacts present", got)
	}
}

func TestGenerateAcceptanceSummary_AggregatesReports(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	featureID := "feat-summary"
	featureDir := ws.FeatureDir(featureID)
	roundDir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}

	criteria := "# Acceptance Criteria\n| # | Criterion | Verification Method | Verify |\n|---|---|---|---|\n| AC-1 | Login works | run go test | unit-test |\n"
	if err := os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte(criteria), 0o644); err != nil {
		t.Fatal(err)
	}

	verify := `{
		"passed": true,
		"round": 1,
		"role": "tester",
		"commands": [{"command": "go test ./...", "exitCode": 0, "summary": "94 tests passed"}],
		"ac_results": [{"id": "AC-1", "passed": true, "evidence": ["ok"]}]
	}`
	if err := os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), []byte(verify), 0o644); err != nil {
		t.Fatal(err)
	}

	reviewReport := "### [UNVERIFIABLE] Needs runtime check\nCannot verify from diff alone.\n\n## Verdict\nPASS\n"
	if err := os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte(reviewReport), 0o644); err != nil {
		t.Fatal(err)
	}

	got := GenerateAcceptanceSummary(ws, featureID, 1)
	if got == "" {
		t.Fatal("GenerateAcceptanceSummary() returned empty, want aggregated content")
	}
	for _, want := range []string{
		"AC-1", "Login works", "PASS", // AC status table
		protocol.ReviewReport, // round reports table
		"[UNVERIFIABLE]",      // unverifiable items
		"go test ./...",       // verification commands
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GenerateAcceptanceSummary() missing %q in:\n%s", want, got)
		}
	}
}
