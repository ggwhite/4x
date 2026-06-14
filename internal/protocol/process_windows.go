//go:build windows

package protocol

import "os"

// ProcessAlive 檢查 PID 是否仍在執行中
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Windows 的 FindProcess 永遠成功，用 Signal(0) 探測
	return p.Signal(os.Signal(nil)) == nil
}
