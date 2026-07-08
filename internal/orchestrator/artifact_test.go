package orchestrator

import "testing"

// TestReviewConditionalPass 覆蓋三態：純 PASS（無 warning）、FAIL（含 critical）、
// CONDITIONAL PASS（有 warning、無 critical）——只有第三態回 true（F144 AC-1）。
func TestReviewConditionalPass(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "pure PASS no warning",
			content: "# Review Report\n\n## Verdict\nPASS\n",
			want:    false,
		},
		{
			name:    "FAIL with critical",
			content: "# Review Report\n\n## Issues\n### [CRITICAL] boom\ndetail\n\n## Verdict\nFAIL\n",
			want:    false,
		},
		{
			name:    "CONDITIONAL PASS with warning no critical",
			content: "# Review Report\n\n## Issues\n### [WARNING] nit\ndetail\n\n## Verdict\nCONDITIONAL PASS\n",
			want:    true,
		},
		{
			name:    "PASS verdict but warning present is conditional",
			content: "# Review Report\n\n## Issues\n### [WARNING] nit\ndetail\n\n## Verdict\nPASS\n",
			want:    true,
		},
		{
			name:    "PASS with warning AND critical is not conditional",
			content: "# Review Report\n\n## Issues\n### [CRITICAL] boom\n### [WARNING] nit\n\n## Verdict\nPASS\n",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReviewConditionalPass(tt.content); got != tt.want {
				t.Errorf("ReviewConditionalPass() = %v, want %v", got, tt.want)
			}
		})
	}
}
