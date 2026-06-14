//go:build !windows

package protocol

import "syscall"

// ProcessAlive 檢查 PID 是否仍在執行中
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
