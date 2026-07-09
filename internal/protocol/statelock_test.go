package protocol

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newLockTestWorkspace 建一個帶 run/{featureID}/ 目錄的暫時 workspace，供加鎖測試取鎖。
func newLockTestWorkspace(t *testing.T, featureID string) *Workspace {
	t.Helper()
	root := t.TempDir()
	ws := &Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	return ws
}

// TestAcquireFileLock_SameProcessMutex 驗證 AC-1：同程序另一 open fd 在前者 release 前
// 無法取得同一 lock 檔，release 後才成功。
func TestAcquireFileLock_SameProcessMutex(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".state.lock")

	lockA, err := acquireFileLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}

	acquired := make(chan time.Time, 1)
	go func() {
		lockB, err := acquireFileLock(lockPath, 5*time.Second)
		if err != nil {
			acquired <- time.Time{}
			return
		}
		acquired <- time.Now()
		lockB.release()
	}()

	// 給 B 足夠時間嘗試並確定被 A 擋住。
	time.Sleep(150 * time.Millisecond)
	releaseTime := time.Now()
	if err := lockA.release(); err != nil {
		t.Fatalf("release A: %v", err)
	}

	acqTime := <-acquired
	if acqTime.IsZero() {
		t.Fatal("B failed to acquire lock")
	}
	if acqTime.Before(releaseTime) {
		t.Errorf("B acquired at %v before A released at %v", acqTime, releaseTime)
	}
}

// TestAcquireFileLock_Timeout 驗證 AC-2：鎖被持有時第二次取鎖在約 T 內回
// errors.Is(err, ErrStateLockTimeout) 的錯誤，不 hang、不回 nil。
func TestAcquireFileLock_Timeout(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".state.lock")

	lockA, err := acquireFileLock(lockPath, time.Second)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer lockA.release()

	start := time.Now()
	lockB, err := acquireFileLock(lockPath, 100*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		lockB.release()
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrStateLockTimeout) {
		t.Errorf("err = %v, want errors.Is ErrStateLockTimeout", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("acquire took %v, want < 2s (should not hang)", elapsed)
	}
}

// TestUpdateState_ConcurrentNoLostUpdate 驗證 AC-3：N 個 goroutine 併發以 UpdateState
// 遞增 GuardRetries，最終值必為 N（無 lost update）。搭配 -race 執行。
func TestUpdateState_ConcurrentNoLostUpdate(t *testing.T) {
	const id = "feat-conc"
	ws := newLockTestWorkspace(t, id)
	if err := ws.WriteState(id, State{FeatureID: id, Phase: PhaseCoding, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ws.UpdateState(id, func(s *State) error {
				s.GuardRetries++
				return nil
			}); err != nil {
				t.Errorf("UpdateState: %v", err)
			}
		}()
	}
	wg.Wait()

	final, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if final.GuardRetries != n {
		t.Errorf("GuardRetries = %d, want %d (lost update)", final.GuardRetries, n)
	}
}

// TestReadState_NotBlockedByWriteLock 驗證 AC-4：writer 持有 state lock 期間，
// ReadState 仍即時返回完整可解析的 State，不阻塞、不回 parse error。
func TestReadState_NotBlockedByWriteLock(t *testing.T) {
	const id = "feat-read"
	ws := newLockTestWorkspace(t, id)
	if err := ws.WriteState(id, State{FeatureID: id, Phase: PhaseCoding, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	lock, err := acquireFileLock(ws.stateLockPath(id), 2*time.Second)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	done := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		lock.release()
		close(done)
	}()

	start := time.Now()
	s, err := ws.ReadState(id)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ReadState during held lock: %v", err)
	}
	if s.FeatureID != id {
		t.Errorf("FeatureID = %q, want %q", s.FeatureID, id)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("ReadState took %v, want < 100ms (should not block on write lock)", elapsed)
	}
	<-done
}

// TestUpdateState_SkipStateWrite 驗證 AC-11：mutate 回 ErrSkipStateWrite 時 UpdateState
// 回 err==nil、回傳磁碟現況、不改動 UpdatedAt；對照組回 nil 時 UpdatedAt 前進。
func TestUpdateState_SkipStateWrite(t *testing.T) {
	const id = "feat-skip"
	ws := newLockTestWorkspace(t, id)
	if err := ws.WriteState(id, State{FeatureID: id, Phase: PhaseCoding, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	before, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}

	got, err := ws.UpdateState(id, func(s *State) error {
		s.GuardRetries = 99 // 修改應被丟棄
		return ErrSkipStateWrite
	})
	if err != nil {
		t.Fatalf("UpdateState(skip) err = %v, want nil", err)
	}
	if got.GuardRetries != before.GuardRetries {
		t.Errorf("returned GuardRetries = %d, want %d (磁碟現況)", got.GuardRetries, before.GuardRetries)
	}

	afterSkip, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if !afterSkip.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("UpdatedAt changed on skip: before=%v after=%v", before.UpdatedAt, afterSkip.UpdatedAt)
	}

	// 對照組：回 nil 確實寫入，UpdatedAt 前進。
	time.Sleep(2 * time.Millisecond)
	if _, err := ws.UpdateState(id, func(s *State) error {
		s.GuardRetries = 1
		return nil
	}); err != nil {
		t.Fatalf("UpdateState(write) err = %v", err)
	}
	afterWrite, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if !afterWrite.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("UpdatedAt not advanced on write: before=%v after=%v", before.UpdatedAt, afterWrite.UpdatedAt)
	}
	if afterWrite.GuardRetries != 1 {
		t.Errorf("GuardRetries = %d, want 1", afterWrite.GuardRetries)
	}
}
