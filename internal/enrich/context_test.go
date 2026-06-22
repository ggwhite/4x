package enrich

import "testing"

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  int // 期待 keyword 數量：>0 表示應非空，0 表示應為空
	}{
		{"normal", "Add batch retry logic for failed features", 5},
		{"short", "Fix bug logic", 2},
		{"all stop words or short", "the are for and is of to in", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kw := extractKeywords(tt.title)
			if len(kw) > maxKeywords {
				t.Errorf("extractKeywords(%q) returned %d keywords, max %d", tt.title, len(kw), maxKeywords)
			}
			if tt.want > 0 && len(kw) == 0 {
				t.Errorf("extractKeywords(%q) returned 0 keywords, want > 0", tt.title)
			}
			if tt.want == 0 && len(kw) != 0 {
				t.Errorf("extractKeywords(%q) returned %d keywords, want 0", tt.title, len(kw))
			}
		})
	}
}

func TestFormatFeatureList(t *testing.T) {
	list := formatFeatureList([]featureSummary{
		{ID: "F001-foo", Name: "F001: Foo", Description: "Foo desc"},
		{ID: "F002-bar", Name: "F002: Bar", Description: "Bar desc"},
	})
	if list == "" {
		t.Error("formatFeatureList returned empty string")
	}
}

func TestFormatFeatureList_Empty(t *testing.T) {
	if got := formatFeatureList(nil); got != "(no existing features)" {
		t.Errorf("formatFeatureList(nil) = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long = %q", got)
	}
}
