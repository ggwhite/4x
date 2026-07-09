//go:build windows

package protocol

import "golang.org/x/sys/windows"

// tryLockEx 以非阻塞方式嘗試取得 handle 上的排他鎖（LockFileEx，鎖第一個 byte）。
// LOCKFILE_FAIL_IMMEDIATELY 讓已被他人持有時立即回 ERROR_LOCK_VIOLATION 而不阻塞。
// 回傳語意與 unix 版一致：(true, nil) 取得、(false, nil) 可重試、(false, err) 其他錯誤。
// Windows 的檔案鎖在 handle 關閉（含程序結束）時由 OS 自動釋放。
func tryLockEx(fd uintptr) (bool, error) {
	handle := windows.Handle(fd)
	ol := &windows.Overlapped{}
	err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_LOCK_VIOLATION || err == windows.ERROR_IO_PENDING {
		return false, nil
	}
	return false, err
}

// unlockFile 釋放 handle 上第一個 byte 的鎖（UnlockFileEx），與 tryLockEx 的鎖範圍對應。
func unlockFile(fd uintptr) error {
	handle := windows.Handle(fd)
	ol := &windows.Overlapped{}
	return windows.UnlockFileEx(handle, 0, 1, 0, ol)
}
