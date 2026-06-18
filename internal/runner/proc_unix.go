//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
	"time"
)

// gracefulShutdownDelay 是 context 取消後，子程序在被強制 SIGKILL 前
// 可用於 graceful shutdown（flush log、刪 temp、回收子 session）的寬限期。
const gracefulShutdownDelay = 5 * time.Second

// setupProcGroup 設定 non-PTY 子程序使用獨立 process group，
// 使 context 取消時能連同所有子程序一併終止，避免孤兒程序。
//
// 取消時採兩段式終止：先對整個 process group 送 SIGTERM 讓子程序
// graceful shutdown；若逾 gracefulShutdownDelay 仍未結束，os/exec 會
// 依 cmd.WaitDelay 自動升級為 SIGKILL 強制終止。
func setupProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = gracefulShutdownDelay
}
