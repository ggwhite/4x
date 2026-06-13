//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty/v2"
)

func startPty(cmd *exec.Cmd) (*os.File, error) {
	attrs := &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	return pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 120}, attrs)
}
