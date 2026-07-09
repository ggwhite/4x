package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestDeriveAllowlist_MultiSubcommandTools 驗證 bundle/poetry/pip/composer/gradle/pdm/uv
// 這些多子命令工具被 pin 到「工具+子命令」兩 token，而非僅工具名（Finding 5）。
func TestDeriveAllowlist_MultiSubcommandTools(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"bundle exec", "bundle exec rspec", "bundle exec"},
		{"poetry run", "poetry run pytest", "poetry run"},
		{"pip install", "pip install -e .", "pip install"},
		{"composer run", "composer run test", "composer run"},
		{"gradle test", "gradle test", "gradle test"},
		{"pdm run", "pdm run pytest", "pdm run"},
		{"uv run", "uv run pytest", "uv run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAllowlist(protocol.ProjectConfig{Test: []string{tc.cmd}})
			want := []string{tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("deriveAllowlist(%q) = %#v, want %#v", tc.cmd, got, want)
			}
		})
	}
}

func TestDetectProjectProfile_DefaultAllowlist(t *testing.T) {
	t.Run("go with Makefile", func(t *testing.T) {
		dir := t.TempDir()
		writeInitFixture(t, dir, "go.mod", "module example.com/demo\n")
		writeInitFixture(t, dir, "Makefile", "build:\n\tgo build ./...\n")

		got := detectProjectProfile(dir).VerifyCommandAllowlist
		want := []string{"make"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("allowlist = %#v, want %#v", got, want)
		}
	})

	t.Run("go without Makefile", func(t *testing.T) {
		dir := t.TempDir()
		writeInitFixture(t, dir, "go.mod", "module example.com/demo\n")

		got := detectProjectProfile(dir).VerifyCommandAllowlist
		want := []string{"go build", "go test", "go vet"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("allowlist = %#v, want %#v", got, want)
		}
		for _, entry := range got {
			if entry == "go" {
				t.Fatalf("allowlist must not contain bare go: %#v", got)
			}
		}
	})
}

func writeInitFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}
