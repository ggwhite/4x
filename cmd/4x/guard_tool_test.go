package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runGuardTool 以 stdin/env 執行 4x guard-tool，回傳 stdout 內容。
func runGuardTool(t *testing.T, stdin, role, pkg string) string {
	t.Helper()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	origStdin, origStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Setenv("FOURX_ROLE", role)
	t.Setenv("FOURX_REVIEW_PACKAGE", pkg)

	go func() {
		io.WriteString(inW, stdin)
		inW.Close()
	}()

	cmd := newGuardToolCmd()
	runErr := cmd.RunE(cmd, nil)

	outW.Close()
	data, _ := io.ReadAll(outR)
	os.Stdin, os.Stdout = origStdin, origStdout

	if runErr != nil {
		t.Fatalf("guard-tool RunE returned error (should always exit 0): %v", runErr)
	}
	return string(data)
}

// AC-9：guard-tool 端到端行為。
func TestGuardToolCmd(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "review-package.md")
	if err := os.WriteFile(pkg, []byte("pkg"), 0o644); err != nil {
		t.Fatalf("write pkg: %v", err)
	}

	t.Run("reviewer git diff with existing pkg denies", func(t *testing.T) {
		out := runGuardTool(t, `{"tool_name":"Bash","tool_input":{"command":"git diff HEAD"}}`, "reviewer", pkg)
		if !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("expected deny decision, got: %s", out)
		}
	})

	t.Run("reviewer make test is allowed", func(t *testing.T) {
		out := runGuardTool(t, `{"tool_name":"Bash","tool_input":{"command":"make test"}}`, "reviewer", pkg)
		if strings.Contains(out, "deny") {
			t.Errorf("make test should be allowed, got: %s", out)
		}
	})

	t.Run("coder git diff is allowed", func(t *testing.T) {
		out := runGuardTool(t, `{"tool_name":"Bash","tool_input":{"command":"git diff"}}`, "coder", pkg)
		if strings.Contains(out, "deny") {
			t.Errorf("coder should be allowed, got: %s", out)
		}
	})

	t.Run("reviewer git diff with missing pkg is allowed (fallback)", func(t *testing.T) {
		out := runGuardTool(t, `{"tool_name":"Bash","tool_input":{"command":"git diff"}}`, "reviewer", filepath.Join(dir, "nope.md"))
		if strings.Contains(out, "deny") {
			t.Errorf("missing pkg should fall back to allow, got: %s", out)
		}
	})

	t.Run("parse failure is allowed", func(t *testing.T) {
		out := runGuardTool(t, `not-json`, "reviewer", pkg)
		if strings.Contains(out, "deny") {
			t.Errorf("parse failure should be allowed, got: %s", out)
		}
	})
}
