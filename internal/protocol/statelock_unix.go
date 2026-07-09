//go:build unix

package protocol

import "golang.org/x/sys/unix"

// tryLockEx 以非阻塞方式嘗試取得 fd 上的 advisory 排他鎖（flock LOCK_EX|LOCK_NB）。
// 回傳 (true, nil) 表示已取得；(false, nil) 表示已被他人持有、可重試（EWOULDBLOCK/EAGAIN）；
// (false, err) 表示其他系統錯誤。flock 為 kernel 級鎖，持鎖程序死亡時由 OS 自動釋放。
func tryLockEx(fd uintptr) (bool, error) {
	err := unix.Flock(int(fd), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
		return false, nil
	}
	return false, err
}

// unlockFile 釋放 fd 上的 flock 鎖（LOCK_UN）。
func unlockFile(fd uintptr) error {
	return unix.Flock(int(fd), unix.LOCK_UN)
}
