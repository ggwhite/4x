//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// setupProcGroup 設定 non-PTY 子程序使用獨立 process group，
// 使 context 取消時能連同所有子程序一併終止，避免孤兒程序。
func setupProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
