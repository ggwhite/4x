package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
