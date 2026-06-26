package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTokensFromLog(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"claude format", "some output\ntokens used\n73,204\n", 73204},
		{"large number", "output\ntokens used\n1,234,567\n", 1234567},
		{"no commas", "output\ntokens used\n5000\n", 5000},
		{"no token info", "just some log output\n", 0},
		{"empty file", "", 0},
		{"tokens used without number", "tokens used\n", 0},
		{"tokens used with trailing text", "tokens used\n90,648\nsome trailing output\n", 90648},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, tt.name+".log")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got := parseTokensFromLog(p)
			if got != tt.want {
				t.Errorf("parseTokensFromLog() = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		got := parseTokensFromLog(filepath.Join(dir, "nonexistent.log"))
		if got != 0 {
			t.Errorf("parseTokensFromLog(missing) = %d, want 0", got)
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
