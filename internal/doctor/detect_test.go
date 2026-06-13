package doctor

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"claude", "2.1.175 (Claude Code)", "2.1.175"},
		{"codex", "codex-cli 0.139.0", "0.139.0"},
		{"gemini", "0.46.0", "0.46.0"},
		{"agy", "1.0.7", "1.0.7"},
		{"copilot", "GitHub Copilot CLI 1.0.61.\nRun 'copilot update' ...", "1.0.61"},
		{"no version", "some random text", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVersion(tt.output)
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestDetectRunners_NoRunners(t *testing.T) {
	runners := map[string]string{}
	result := DetectRunners(runners)
	if len(result) != 0 {
		t.Errorf("expected 0 runners, got %d", len(result))
	}
}
