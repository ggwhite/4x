package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubprocessRunner_Success(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "4x-plugin-test", "#!/bin/sh\nexit 0\n")

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}
	ws.InitFeatureDir("f1")

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	result, err := r.Run(context.Background(), "f1", protocol.RoleCoder)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSubprocessRunner_SoftFail(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "4x-plugin-test", "#!/bin/sh\nexit 1\n")

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}
	ws.InitFeatureDir("f1")

	r := &SubprocessRunner{Workspace: ws, Name: "test"}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	result, err := r.Run(context.Background(), "f1", protocol.RoleCoder)
	if err != nil {
		t.Fatalf("Run should not error for exit 1: %v", err)
	}
	if !IsSoftFail(result) {
		t.Errorf("expected soft fail, got exit %d", result.ExitCode)
	}
}

func TestSubprocessRunner_HardError(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "4x-plugin-test", "#!/bin/sh\nexit 2\n")

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}
	ws.InitFeatureDir("f1")

	r := &SubprocessRunner{Workspace: ws, Name: "test"}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	result, err := r.Run(context.Background(), "f1", protocol.RoleCoder)
	if err != nil {
		t.Fatalf("Run should not error for exit 2: %v", err)
	}
	if !IsHardError(result) {
		t.Errorf("expected hard error, got exit %d", result.ExitCode)
	}
}

func TestSubprocessRunner_Timeout(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "4x-plugin-test", "#!/bin/sh\nsleep 10\n")

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}
	ws.InitFeatureDir("f1")

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Timeout:   200 * time.Millisecond,
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	_, err := r.Run(context.Background(), "f1", protocol.RoleCoder)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestSubprocessRunner_NotFound(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}
	ws.InitFeatureDir("f1")

	r := &SubprocessRunner{Workspace: ws, Name: "nonexistent"}

	_, err := r.Run(context.Background(), "f1", protocol.RoleCoder)
	if err == nil {
		t.Error("expected error when plugin not found")
	}
}

func TestIsSoftFail(t *testing.T) {
	if IsSoftFail(nil) {
		t.Error("nil should not be soft fail")
	}
	if IsSoftFail(&Result{ExitCode: 0}) {
		t.Error("exit 0 should not be soft fail")
	}
	if !IsSoftFail(&Result{ExitCode: 1}) {
		t.Error("exit 1 should be soft fail")
	}
}

func TestIsHardError(t *testing.T) {
	if IsHardError(nil) {
		t.Error("nil should not be hard error")
	}
	if !IsHardError(&Result{ExitCode: 2}) {
		t.Error("exit 2 should be hard error")
	}
}

func TestNewRunner(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	cfg := protocol.RunnerConfig{Command: "claude", Model: "opus"}
	r := NewRunner(ws, "claude", cfg, 30*time.Second)
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}

	sr, ok := r.(*SubprocessRunner)
	if !ok {
		t.Fatal("expected *SubprocessRunner")
	}
	if sr.Name != "claude" {
		t.Errorf("Name = %s, want claude", sr.Name)
	}
	if sr.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", sr.Timeout)
	}
}

func init() {
	// exec.LookPath 的環境在每個 test 裡設
	_ = exec.LookPath
}
