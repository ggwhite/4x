//go:build !windows

package runner

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
)

// startPty 以獨立 session/process group 啟動 cmd 並回傳 pty master 與一個 stop function。
// PTY 路徑使用 exec.Command（非 CommandContext），ctx 取消由 watchPtyContext 負責：
// 先對 process group 送 SIGTERM，5 秒後仍存活再送 SIGKILL，實現優雅關機。
// caller 必須在 cmd.Wait() 之後呼叫回傳的 stop（建議 defer），以顯式關閉 watcher，
// 避免在正常結束路徑（ctx 未 cancel）下 goroutine 洩漏。stop 可重複呼叫。
func startPty(ctx context.Context, cmd *exec.Cmd) (*os.File, func(), error) {
	attrs := &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	ptmx, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 120}, attrs)
	if err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go watchPtyContext(ctx, cmd.Process.Pid, done)
	return ptmx, stop, nil
}

// watchPtyContext 監看 ctx 與 done。caller 在 cmd 正常結束後關閉 done，watcher 隨即退出（無洩漏）。
// 若 ctx 先被 cancel，對 process group（-pid，Setsid 使 pgid == pid）送 SIGTERM；
// 5 秒內 child 仍未結束（done 未關閉）再送 SIGKILL，確保整個 process group 被回收。
func watchPtyContext(ctx context.Context, pid int, done <-chan struct{}) {
	select {
	case <-done:
		return
	case <-ctx.Done():
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
}
