package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestEnrichedEnv_ExePathInPath 驗證 4x 執行檔所在目錄會被併入 PATH，
// 不被各 OS case 的 extraPaths 賦值沖掉（回歸測試 env-path-append）。
func TestEnrichedEnv_ExePathInPath(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skipf("unsupported GOOS %s", runtime.GOOS)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	exeDir := filepath.Dir(exe)

	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}

	env := enrichedEnv()

	var pathVal string
	found := false
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			pathVal = e[len("PATH="):]
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no PATH entry in enrichedEnv output")
	}

	for _, dir := range strings.Split(pathVal, sep) {
		if dir == exeDir {
			return
		}
	}
	t.Errorf("exe dir %q not found in PATH %q", exeDir, pathVal)
}

// TestEnrichedEnv_SetsFourxBin 驗證 FOURX_BIN 仍被設定（不應被本次修改影響）。
func TestEnrichedEnv_SetsFourxBin(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	env := enrichedEnv()
	want := "FOURX_BIN=" + exe
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Errorf("FOURX_BIN=%q not found in enrichedEnv output", exe)
}

// TestRun_WorktreeEnvInjection 是 F154 的端到端 wiring 測試：workspace root 為
// linked worktree 時，runner 子程序必須看到 worktree-local 的 GOLANGCI_LINT_CACHE。
func TestRun_WorktreeEnvInjection(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	git("worktree", "add", wt, "-b", "feat-env")

	outFile := filepath.Join(t.TempDir(), "env.out")
	binDir := t.TempDir()
	writeScript(t, binDir, "test-runner", "#!/bin/sh\necho \"$GOLANGCI_LINT_CACHE\" > "+outFile+"\nexit 0\n")

	r := &SubprocessRunner{
		Workspace: &protocol.Workspace{Root: wt},
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command: filepath.Join(binDir, "test-runner"),
			Args:    []string{"-p", "{prompt}"},
		},
	}
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read env.out: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if !strings.HasSuffix(got, filepath.Join(".cache", "golangci-lint")) || got == "" {
		t.Errorf("GOLANGCI_LINT_CACHE in runner subprocess = %q, want worktree-local cache path", got)
	}
}
