package verify

import (
	"reflect"
	"testing"
)

// TestF180ExtractScopePaths 驗證 ExtractScopePaths 對證據/命令字串的路徑抽取與去重。
func TestF180ExtractScopePaths(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"go-test-relative", "$ go test ./internal/verify/accheck.go", []string{"internal/verify/accheck.go"}},
		{"cmd-path", "check cmd/4x/main.go compiles", []string{"cmd/4x/main.go"}},
		{"line-number-stripped", "internal/guard/check.go:42: error", []string{"internal/guard/check.go"}},
		{"prose-only", "all checks passed", nil},
		{"dedup", "internal/x.go and internal/x.go again", []string{"internal/x.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractScopePaths(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractScopePaths(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestF180IsAllowedArtifactPath 驗證合法 build 產物/generated 檔回 true、原始碼路徑回 false。
func TestF180IsAllowedArtifactPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"bin/4x", true},
		{"coverage.out", true},
		{".4x/run/F180-ac-untracked-gitignore/verify.json", true},
		{"dashboard/macos/.build/x", true},
		{".4x/run/x/e2e/screenshots/round-1/a.png", true},
		{"internal/foo/zz_generated.go", true},
		{"internal/foo/thing_gen.go", true},
		{"internal/verify/accheck.go", false},
		{"cmd/4x/main.go", false},
	}
	for _, tt := range tests {
		if got := IsAllowedArtifactPath(tt.path); got != tt.want {
			t.Errorf("IsAllowedArtifactPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
