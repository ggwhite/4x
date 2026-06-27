package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/orchestrator"
)

func TestParseReviewVerdict_Pass(t *testing.T) {
	tests := []struct {
		name    string
		content string
		passed  bool
	}{
		{
			name:    "PASS verdict",
			content: "## Verdict\nPASS\n",
			passed:  true,
		},
		{
			name:    "CONDITIONAL PASS verdict",
			content: "## Verdict\nCONDITIONAL PASS\n",
			passed:  true,
		},
		{
			name:    "FAIL verdict",
			content: "## Verdict\nFAIL\n",
			passed:  false,
		},
		{
			name:    "PASS after blank line",
			content: "## Verdict\n\nPASS\n",
			passed:  true,
		},
		{
			name:    "only blank lines after verdict",
			content: "## Verdict\n\n\n",
			passed:  false,
		},
		{
			name:    "no verdict section",
			content: "## Summary\nsome text\n",
			passed:  false,
		},
		{
			name:    "empty content",
			content: "",
			passed:  false,
		},
		{
			name:    "TODO verdict",
			content: "## Verdict\nTODO\n",
			passed:  false,
		},
		{
			name:    "ERROR verdict",
			content: "## Verdict\nERROR\n",
			passed:  false,
		},
		{
			name:    "PENDING verdict",
			content: "## Verdict\nPENDING\n",
			passed:  false,
		},
		{
			name:    "garbled verdict",
			content: "## Verdict\n\xf0\xf1\xf2\n",
			passed:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := orchestrator.ParseReviewVerdict(tt.content)
			if result.Passed != tt.passed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.passed)
			}
		})
	}
}

func TestParseReviewVerdict_CriticalWarningCount(t *testing.T) {
	content := `## Issues
[CRITICAL] missing test coverage
[WARNING] inefficient loop
[WARNING] unused variable
## Verdict
FAIL
`
	result := orchestrator.ParseReviewVerdict(content)
	if result.CriticalCount != 1 {
		t.Errorf("CriticalCount = %d, want 1", result.CriticalCount)
	}
	if result.WarningCount != 2 {
		t.Errorf("WarningCount = %d, want 2", result.WarningCount)
	}
	if result.Passed {
		t.Error("Passed = true, want false")
	}
}

func TestParseReviewVerdict_PassWithWarnings(t *testing.T) {
	content := `## Issues
[WARNING] minor style issue
## Verdict
PASS
`
	result := orchestrator.ParseReviewVerdict(content)
	if !result.Passed {
		t.Error("Passed = false, want true")
	}
	if result.WarningCount != 1 {
		t.Errorf("WarningCount = %d, want 1", result.WarningCount)
	}
}
