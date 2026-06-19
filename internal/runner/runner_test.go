package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	args, _, err := r.buildArgs("hello")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
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
	args, _, err := r.buildArgs("hello")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
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
	args, _, err := r.buildArgs("hello")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	for _, a := range args {
		if a == "--model" {
			t.Error("--model should not appear when ModelOverride is empty")
		}
	}
}

func TestSubprocessRunner_StreamJSON(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"test"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello from stream"}]}}'
echo '{"type":"result","subtype":"success","duration_ms":1000,"total_cost_usd":0.05}'
exit 0
`
	writeScript(t, binDir, "test-runner", script)

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	logDir := filepath.Join(root, ".4x", "test-feature", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	logPath := filepath.Join(logDir, "round-1-coder.log")

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command:      filepath.Join(binDir, "test-runner"),
			Args:         []string{"-p", "{prompt}"},
			OutputFormat: "stream-json",
		},
		LogPath: logPath,
	}

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logStr := string(logContent)
	if !strings.Contains(logStr, "[assistant] Hello from stream") {
		t.Errorf("log missing assistant text, got:\n%s", logStr)
	}
	if strings.Contains(logStr, "init") {
		t.Errorf("log should not contain system init event, got:\n%s", logStr)
	}

	rawPath := filepath.Join(logDir, StreamLogFileName(1, "coder"))
	rawContent, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read stream log: %v", err)
	}
	rawLines := strings.Count(strings.TrimSpace(string(rawContent)), "\n") + 1
	if rawLines != 3 {
		t.Errorf("stream.jsonl line count = %d, want 3", rawLines)
	}
}

func TestSubprocessRunner_NoOutputFormat_UsesOldPath(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\necho hello\nexit 0\n")
	defer cleanup()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "test.log")
	r.LogPath = logPath

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	rawPath := strings.TrimSuffix(logPath, ".log") + ".stream.jsonl"
	if _, err := os.Stat(rawPath); err == nil {
		t.Error("stream.jsonl should not exist for non-stream-json runner")
	}
}

func TestSubprocessRunner_SignalKill_TreatedAsHardError(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nkill -9 $$\n")
	defer cleanup()

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run should not error for signal kill: %v", err)
	}
	if !IsHardError(result) {
		t.Errorf("signal kill should be treated as hard error, got exit %d", result.ExitCode)
	}
}

func TestSubprocessRunner_ContextCanceled(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nsleep 10\n")
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := r.Run(ctx, "test prompt")
	if err == nil {
		t.Fatal("expected error for context cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// setupCountingRunner 建立一個會記錄執行次數的測試 runner：
// 子程序每次執行把計數寫入 counterPath；前 failTimes 次以 stderrMsg 輸出到 stderr 並 exit 1，
// 第 failTimes+1 次起 exit 0。回傳 runner、讀取執行次數的 func、與環境還原 func。
// backoffBase 注入極小值讓重試迴圈快速完成。
func setupCountingRunner(t *testing.T, failTimes int, stderrMsg string) (*SubprocessRunner, func() int, func()) {
	t.Helper()
	binDir := t.TempDir()
	counterPath := filepath.Join(t.TempDir(), "count")

	script := fmt.Sprintf(`#!/bin/sh
count=$(cat %q 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > %q
if [ "$count" -le %d ]; then
  echo %q >&2
  exit 1
fi
exit 0
`, counterPath, counterPath, failTimes, stderrMsg)
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
		backoffBase: 1 * time.Millisecond,
	}

	readCount := func() int {
		data, err := os.ReadFile(counterPath)
		if err != nil {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return n
	}

	return r, readCount, func() { os.Setenv("PATH", origPath) }
}

// TestRunTransientRetry 覆蓋暫態重試迴圈的各種情境（AC-5 / AC-6 / AC-7 / AC-8）。
func TestRunTransientRetry(t *testing.T) {
	// (a) 暫態失敗後重試成功 → 最終 ExitCode == 0，執行次數 == K+1
	t.Run("retry then success", func(t *testing.T) {
		r, count, cleanup := setupCountingRunner(t, 2, "Error: socket closed")
		defer cleanup()
		r.MaxTransientRetries = 3

		res, err := r.Run(context.Background(), "p")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.ExitCode != 0 {
			t.Errorf("ExitCode = %d, want 0", res.ExitCode)
		}
		if got := count(); got != 3 {
			t.Errorf("execution count = %d, want 3 (1 original + 2 retries)", got)
		}
	})

	// (b) 持續暫態失敗 → 在 MaxTransientRetries+1 次後停止並回傳非零
	t.Run("persistent transient failure stops at limit", func(t *testing.T) {
		r, count, cleanup := setupCountingRunner(t, 100, "connection reset by peer")
		defer cleanup()
		r.MaxTransientRetries = 3

		res, err := r.Run(context.Background(), "p")
		if err != nil {
			t.Fatalf("Run should not error for exit 1: %v", err)
		}
		if res.ExitCode == 0 {
			t.Errorf("ExitCode = %d, want non-zero", res.ExitCode)
		}
		if got := count(); got != 4 {
			t.Errorf("execution count = %d, want 4 (1 original + 3 retries)", got)
		}
	})

	// (c) 非暫態失敗（exit 1 但 stderr 不含 pattern）→ 不重試、執行 1 次
	t.Run("non-transient failure does not retry", func(t *testing.T) {
		r, count, cleanup := setupCountingRunner(t, 100, "compilation error: undefined foo")
		defer cleanup()
		r.MaxTransientRetries = 3

		res, err := r.Run(context.Background(), "p")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !IsSoftFail(res) {
			t.Errorf("ExitCode = %d, want 1 (soft fail)", res.ExitCode)
		}
		if got := count(); got != 1 {
			t.Errorf("execution count = %d, want 1 (no retry)", got)
		}
	})

	// (d) 透過 NewRunner 以 config TransientRetries=0 停用重試 → 執行 1 次（AC-8）
	t.Run("config disables retry", func(t *testing.T) {
		binDir := t.TempDir()
		counterPath := filepath.Join(t.TempDir(), "count")
		script := fmt.Sprintf(`#!/bin/sh
count=$(cat %q 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > %q
echo "socket closed" >&2
exit 1
`, counterPath, counterPath)
		writeScript(t, binDir, "test-runner", script)

		root := t.TempDir()
		protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
		ws := &protocol.Workspace{Root: root}

		disabled := 0
		cfg := protocol.RunnerConfig{
			Command:          filepath.Join(binDir, "test-runner"),
			Args:             []string{"-p", "{prompt}"},
			TransientRetries: &disabled,
		}
		rn := NewRunner(ws, "test", cfg, 0, "", "")
		sr := rn.(*SubprocessRunner)
		sr.backoffBase = 1 * time.Millisecond

		res, err := sr.Run(context.Background(), "p")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.ExitCode == 0 {
			t.Errorf("ExitCode = %d, want non-zero", res.ExitCode)
		}
		data, _ := os.ReadFile(counterPath)
		if n, _ := strconv.Atoi(strings.TrimSpace(string(data))); n != 1 {
			t.Errorf("execution count = %d, want 1 (retry disabled)", n)
		}
	})

	// (e) ctx 取消 / 逾時期間不重試：即使首次失敗命中暫態 pattern，ctx 結束後也不再重試。
	// 用大 backoffBase 確保進入 backoff 等待，並在首次執行後 cancel ctx，讓重試迴圈在
	// select 收到 ctx.Done 而非 timer.C，最終只執行 1 次。
	t.Run("ctx cancel does not retry", func(t *testing.T) {
		r, count, cleanup := setupCountingRunner(t, 100, "Error: socket closed")
		defer cleanup()
		r.MaxTransientRetries = 3
		r.backoffBase = 10 * time.Second // 夠大，確保不會在 cancel 前就重試

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(300 * time.Millisecond)
			cancel()
		}()

		// 關注點是「不再重試」：在高負載環境下 cancel 可能落在首次執行中或 backoff 期間，
		// 兩者都應只執行 1 次。
		res, err := r.Run(ctx, "p")
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.ExitCode == 0 && !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected success result without context cancellation: res=%+v err=%v", res, err)
		}
		if got := count(); got > 1 {
			t.Errorf("execution count = %d, want <= 1 (no retry after ctx cancel)", got)
		}
	})
}

func TestDeepFixLogFileName(t *testing.T) {
	if got := DeepFixLogFileName(2, 1); got != "round-2-deep-fix-1.log" {
		t.Errorf("got %q, want round-2-deep-fix-1.log", got)
	}
}

func TestDeepReverifyLogFileName(t *testing.T) {
	if got := DeepReverifyLogFileName(3, 2); got != "round-3-deep-reverify-2.log" {
		t.Errorf("got %q, want round-3-deep-reverify-2.log", got)
	}
}

func TestDeepReviewerLogFileName(t *testing.T) {
	if got := DeepReviewerLogFileName(1, 2); got != "round-1-deep-reviewer-2.log" {
		t.Errorf("got %q, want round-1-deep-reviewer-2.log", got)
	}
}

// TestBuildArgs_UnresolvedModel 驗證 W16：arg 含 {model} 但 ModelOverride 為空時
// 回傳 error，不把字面 "{model}" 傳給 CLI。
func TestBuildArgs_UnresolvedModel(t *testing.T) {
	r := &SubprocessRunner{
		Name:   "claude",
		Config: protocol.RunnerConfig{Args: []string{"--model", "{model}"}},
	}
	_, cleanup, err := r.buildArgs("prompt")
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected error for unresolved {model}, got nil")
	}
	if !strings.Contains(err.Error(), "model not resolved") || !strings.Contains(err.Error(), "claude") {
		t.Errorf("error = %q, want mention of model not resolved + runner name", err)
	}
}

// TestBuildArgs_ResolvedModel 驗證 W16 正常路徑：有 ModelOverride 時正確替換，無 error。
func TestBuildArgs_ResolvedModel(t *testing.T) {
	r := &SubprocessRunner{
		Name:          "claude",
		ModelOverride: "opus",
		Config:        protocol.RunnerConfig{Args: []string{"--model", "{model}"}},
	}
	args, cleanup, err := r.buildArgs("prompt")
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[1] != "opus" {
		t.Errorf("args = %v, want [--model opus]", args)
	}
}

// TestBuildArgs_PromptFileCreateFailure 驗證 W17：os.CreateTemp 失敗時回傳
// wrap 過的 error，不 fallback 成字面 placeholder。
func TestBuildArgs_PromptFileCreateFailure(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	r := &SubprocessRunner{
		Name:   "claude",
		Config: protocol.RunnerConfig{Args: []string{"--prompt-file", "{promptFile}"}},
	}
	_, cleanup, err := r.buildArgs("prompt")
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected error when CreateTemp fails, got nil")
	}
	if !strings.Contains(err.Error(), "create prompt temp file") {
		t.Errorf("error = %q, want wrapped create prompt temp file error", err)
	}
}

// TestBuildArgs_NoPlaceholder 驗證無 placeholder 的一般路徑不受 signature 變更影響。
func TestBuildArgs_NoPlaceholder(t *testing.T) {
	r := &SubprocessRunner{
		Name:   "claude",
		Config: protocol.RunnerConfig{Args: []string{"chat", "--json"}},
	}
	args, cleanup, err := r.buildArgs("prompt")
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[0] != "chat" || args[1] != "--json" {
		t.Errorf("args = %v, want [chat --json]", args)
	}
}
