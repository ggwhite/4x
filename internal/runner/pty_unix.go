//go:build !windows

package runner

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
)

// startPty 以獨立 session/process group 啟動 cmd 並回傳 pty master。
// 因 pty.StartWithAttrs 繞過 exec.CommandContext，ctx cancel 無法自動殺 child，
// 故另起一個 watcher goroutine：ctx 被 cancel 時對整個 process group 送訊號回收。
func startPty(ctx context.Context, cmd *exec.Cmd) (*os.File, error) {
	attrs := &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	ptmx, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 120}, attrs)
	if err != nil {
		return nil, err
	}
	go watchPtyContext(ctx, cmd.Process.Pid)
	return ptmx, nil
}

// watchPtyContext 監看 ctx。ctx 被 cancel 時對 process group（-pid，Setsid 使 pgid == pid）
// 送 SIGTERM，5 秒內仍未結束再送 SIGKILL。process 正常結束時以 signal 0 輪詢偵測 pgid 消失
// 後退出，避免 goroutine 洩漏。
func watchPtyContext(ctx context.Context, pid int) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			deadline := time.NewTimer(5 * time.Second)
			for {
				select {
				case <-deadline.C:
					_ = syscall.Kill(-pid, syscall.SIGKILL)
					return
				case <-ticker.C:
					if syscall.Kill(-pid, 0) != nil {
						deadline.Stop()
						return
					}
				}
			}
		case <-ticker.C:
			if syscall.Kill(-pid, 0) != nil {
				return
			}
		}
	}
}
