package protocol

import (
	"errors"
	"os"
	"time"
)

// ErrStateLockTimeout 為取鎖逾時的哨兵錯誤。取鎖端可用 errors.Is 判定逾時，
// 呼叫端據此回報明確訊息而非無限阻塞。
var ErrStateLockTimeout = errors.New("acquire state lock: timed out")

// ErrSkipStateWrite 為 UpdateState 的 mutate 回呼專用哨兵：mutate 回傳它代表
// 「在臨界區內、拿到最新磁碟值後決定不寫入」。UpdateState 收到後釋放鎖、不寫入、
// 回傳 (現況磁碟 State, nil)——屬正常流程而非錯誤。
var ErrSkipStateWrite = errors.New("skip state write")

const (
	// stateLockTimeout 為取 state.json 排他鎖的預設逾時。逾時回 ErrStateLockTimeout，
	// 不再無限等待（避免 batch/parallel 或 dashboard 競寫時互相 hang）。
	stateLockTimeout = 5 * time.Second

	// stateLockPollInterval 為阻塞取鎖時的重試間隔：flock 的非阻塞版命中 EWOULDBLOCK
	// 後 sleep 此間隔再試，直到取得或超過 deadline。
	stateLockPollInterval = 20 * time.Millisecond
)

// fileLock 持有一個已取得 advisory 排他鎖的 lock 檔 fd。
//
// 採 flock 系語意：advisory（僅序列化同樣走此鎖的 writer，不影響 ReadState）、
// kernel 級（程序死亡時由 OS 自動釋放，無 stale lock 清理負擔）、只鎖寫入。
type fileLock struct {
	f *os.File
}

// release 釋放鎖並關閉 lock 檔 fd。重複呼叫安全（第二次為 no-op）。
// lock 檔本身永不刪除（flock + unlink 有經典 race，且檔案位於已被 gitignore 的 run/ 下）。
func (l *fileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := unlockFile(l.f.Fd())
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// acquireFileLock 開啟（或建立）lockPath 並在其上取得 advisory 排他鎖。
//
// 以 poll 非阻塞取鎖 + backoff 實作逾時：flock 阻塞版無原生 timeout，故每次命中
// 「已被他人持有」就 sleep stateLockPollInterval 再試，超過 timeout 回 ErrStateLockTimeout
// 並關閉 fd。程序死亡時 kernel 自動釋放，無需清 stale lock。
func acquireFileLock(lockPath string, timeout time.Duration) (*fileLock, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, err := tryLockEx(f.Fd())
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			return &fileLock{f: f}, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrStateLockTimeout
		}
		time.Sleep(stateLockPollInterval)
	}
}
