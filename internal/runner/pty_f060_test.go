//go:build !windows

package runner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// W3（AC-5）：PTY 模式下 ctx 被 cancel 時，child process group 收到訊號被殺，
// Run 在遠短於 child sleep 的時間內返回，而非等到 sleep 自然結束。
func TestSubprocessRunner_PtyContextCancelKills(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nsleep 30\n")
	defer cleanup()

	r.Config.Tty = protocol.BoolPtr(true)
	r.LogPath = filepath.Join(t.TempDir(), "round-1-coder.log")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	start := time.Now()
	go func() {
		_, _ = r.Run(ctx, "test prompt")
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("Run returned after %v, expected fast kill on ctx cancel", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after ctx cancel — child process not killed")
	}
}

// W3：正常結束（ctx 未 cancel）時 Run 正常返回，watcher goroutine 不卡住整體流程。
func TestSubprocessRunner_PtyNormalCompletion(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\nexit 0\n")
	defer cleanup()

	r.Config.Tty = protocol.BoolPtr(true)
	r.LogPath = filepath.Join(t.TempDir(), "round-1-coder.log")
	r.Timeout = 5 * time.Second

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}
