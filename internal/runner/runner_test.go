package runner

import (
	"context"
	"os"
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

func setupRunner(t *testing.T, script string) (*SubprocessRunner, func()) {
	t.Helper()
	binDir := t.TempDir()
	writeScript(t, binDir, "test-runner", script)

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command: filepath.Join(binDir, "test-runner"),
			Args:    []string{"-p", "{prompt}"},
		},
	}

	return r, func() { os.Setenv("PATH", origPath) }
}

func TestSubprocessRunner_Success(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nexit 0\n")
	defer cleanup()

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSubprocessRunner_SoftFail(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nexit 1\n")
	defer cleanup()

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run should not error for exit 1: %v", err)
	}
	if !IsSoftFail(result) {
		t.Errorf("expected soft fail, got exit %d", result.ExitCode)
	}
}

func TestSubprocessRunner_HardError(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nexit 2\n")
	defer cleanup()

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run should not error for exit 2: %v", err)
	}
	if !IsHardError(result) {
		t.Errorf("expected hard error, got exit %d", result.ExitCode)
	}
}

func TestSubprocessRunner_Timeout(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nsleep 10\n")
	defer cleanup()
	r.Timeout = 200 * time.Millisecond

	_, err := r.Run(context.Background(), "test prompt")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestSubprocessRunner_PromptSubstitution(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\necho \"$@\"\nexit 0\n")
	defer cleanup()

	result, err := r.Run(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSubprocessRunner_PromptFile(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "test-runner", "#!/bin/sh\ncat \"$2\"\nexit 0\n")

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command: filepath.Join(binDir, "test-runner"),
			Args:    []string{"--prompt-file", "{promptFile}"},
		},
	}

	result, err := r.Run(context.Background(), "file-based prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSubprocessRunner_NotFound(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command: "/nonexistent/binary",
			Args:    []string{"-p", "{prompt}"},
		},
	}

	_, err := r.Run(context.Background(), "test")
	if err == nil {
		t.Error("expected error when binary not found")
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

	cfg := protocol.RunnerConfig{Command: "claude", Args: []string{"-p", "{prompt}"}}
	r := NewRunner(ws, "claude", cfg, 30*time.Second, "", "")
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}

	sr, ok := r.(*SubprocessRunner)
	if !ok {
		t.Fatal("expected *SubprocessRunner")
	}
	if sr.Config.Command != "claude" {
		t.Errorf("Command = %s, want claude", sr.Config.Command)
	}
}

func TestBuildArgs_ModelOverrideAppended(t *testing.T) {
	r := &SubprocessRunner{
		Config:        protocol.RunnerConfig{Args: []string{"-p", "{prompt}"}},
		ModelOverride: "opus",
	}
	args, _ := r.buildArgs("hello")
	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "opus" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --model opus in args, got %v", args)
	}
}

func TestBuildArgs_ModelPlaceholder(t *testing.T) {
	r := &SubprocessRunner{
		Config:        protocol.RunnerConfig{Args: []string{"--model", "{model}", "-p", "{prompt}"}},
		ModelOverride: "sonnet",
	}
	args, _ := r.buildArgs("hello")
	if args[0] != "--model" || args[1] != "sonnet" {
		t.Errorf("expected --model sonnet, got %v", args[:2])
	}
	// {model} 已被替換，不應再 append
	for i, a := range args {
		if i > 1 && a == "--model" {
			t.Error("--model should not be appended when placeholder was used")
		}
	}
}

func TestBuildArgs_NoModelOverride(t *testing.T) {
	r := &SubprocessRunner{
		Config: protocol.RunnerConfig{Args: []string{"-p", "{prompt}"}},
	}
	args, _ := r.buildArgs("hello")
	for _, a := range args {
		if a == "--model" {
			t.Error("--model should not appear when ModelOverride is empty")
		}
	}
}
