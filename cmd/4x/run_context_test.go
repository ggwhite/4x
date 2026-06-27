package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRunStatsFromLog(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		wantTok  int
		wantCost float64
	}{
		{"claude format", "some output\ntokens used\n73,204\n", 73204, 0},
		{"large number", "output\ntokens used\n1,234,567\n", 1234567, 0},
		{"no commas", "output\ntokens used\n5000\n", 5000, 0},
		{"no token info", "just some log output\n", 0, 0},
		{"empty file", "", 0, 0},
		{"tokens used without number", "tokens used\n", 0, 0},
		{"tokens used with trailing text", "tokens used\n90,648\nsome trailing output\n", 90648, 0},
		{"stream-json result", "[tool_use] Bash: go test\n[result] success (325.5s, $2.2826)\n", 0, 2.2826},
		{"stream-json cost only", "[result] success (10.2s, $0.1500)\n", 0, 0.15},
		{"stream-json no cost", "[result] success (10.2s, $0.0000)\n", 0, 0},
		{"both formats", "tokens used\n50000\n[result] success (100.0s, $1.5000)\n", 50000, 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, tt.name+".log")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got := parseRunStatsFromLog(p)
			if got.Tokens != tt.wantTok {
				t.Errorf("Tokens = %d, want %d", got.Tokens, tt.wantTok)
			}
			if got.CostUSD != tt.wantCost {
				t.Errorf("CostUSD = %f, want %f", got.CostUSD, tt.wantCost)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		got := parseRunStatsFromLog(filepath.Join(dir, "nonexistent.log"))
		if got.Tokens != 0 || got.CostUSD != 0 {
			t.Errorf("parseRunStatsFromLog(missing) = %+v, want zero", got)
		}
	})
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{73204, "73,204"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
