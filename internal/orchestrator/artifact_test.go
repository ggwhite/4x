package orchestrator

import (
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestParseEscalationVerdict 覆蓋三態解析與安全預設（F179 AC-1）：
// agree-split / disagree / n/a、無 marker、marker 後空白、含裝飾、未知值。
func TestParseEscalationVerdict(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    EscalationVerdict
	}{
		{
			name:    "agree-split",
			content: "# DR\n## Verdict\nFAIL\n## Escalation Verdict\nagree-split\n",
			want:    EscalationAgreeSplit,
		},
		{
			name:    "disagree",
			content: "# DR\n## Escalation Verdict\ndisagree\n",
			want:    EscalationDisagree,
		},
		{
			name:    "n/a explicit",
			content: "# DR\n## Escalation Verdict\nn/a\n",
			want:    EscalationNA,
		},
		{
			name:    "no marker",
			content: "# DR\n## Verdict\nFAIL\n",
			want:    EscalationNA,
		},
		{
			name:    "marker then blank lines before value",
			content: "# DR\n## Escalation Verdict\n\n\nagree-split\n",
			want:    EscalationAgreeSplit,
		},
		{
			name:    "decorated agree-split",
			content: "# DR\n## Escalation Verdict\n**agree-split**\n",
			want:    EscalationAgreeSplit,
		},
		{
			name:    "unknown value falls back to NA",
			content: "# DR\n## Escalation Verdict\nmaybe\n",
			want:    EscalationNA,
		},
		{
			name:    "heading with no following non-empty line",
			content: "# DR\n## Escalation Verdict\n",
			want:    EscalationNA,
		},
		{
			name:    "na alias",
			content: "# DR\n## Escalation Verdict\nNA\n",
			want:    EscalationNA,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseEscalationVerdict(tt.content); got != tt.want {
				t.Errorf("ParseEscalationVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDesignReviewerAgreedSplit 以真實 workspace 驗證讀檔與判斷（F179 AC-2）：
// 只有 agree-split 回 true；disagree / n/a / 無 marker / 檔案不存在皆回 false。
func TestDesignReviewerAgreedSplit(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		content string // 空字串代表不寫報告檔（測檔案不存在）
		want    bool
	}{
		{name: "agree-split true", id: "F999-agree-split", content: "# DR\n## Escalation Verdict\nagree-split\n", want: true},
		{name: "disagree false", id: "F999-agree-disagree", content: "# DR\n## Escalation Verdict\ndisagree\n", want: false},
		{name: "n/a false", id: "F999-agree-na", content: "# DR\n## Escalation Verdict\nn/a\n", want: false},
		{name: "no marker false", id: "F999-agree-nomarker", content: "# DR\n## Verdict\nFAIL\n", want: false},
		{name: "report absent false", id: "F999-agree-absent", content: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.id
			ws := setupPhaseWorkspace(t, id)
			if tt.content != "" {
				writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.DesignReviewReport), tt.content)
			}
			if got := DesignReviewerAgreedSplit(ws, id); got != tt.want {
				t.Errorf("DesignReviewerAgreedSplit() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
