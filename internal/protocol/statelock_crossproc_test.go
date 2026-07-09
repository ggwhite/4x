package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestCrossProcLock 驗證 AC-5：由獨立 OS 子程序（re-exec 測試 binary 的 TestHelperProcess
// 模式）持有 .state.lock 時，本程序取鎖逾時回 ErrStateLockTimeout；子程序退出釋放後取鎖成功。
// 這是真實跨程序互斥，非同程序 goroutine 假冒。
func TestCrossProcLock(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		return // 子程序由 TestHelperProcess 處理
	}

	const id = "feat-xproc"
	root := t.TempDir()
	ws := &Workspace{Root: root}
	if err := ws.InitFeatureDir(id); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ws.WriteState(id, State{FeatureID: id, Phase: PhaseCoding, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	lockPath := ws.stateLockPath(id)

	// 啟動真實子程序持鎖 1.5s。
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"STATELOCK_PATH="+lockPath,
		"STATELOCK_HOLD_MS=1500",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	// 等子程序確認已取得鎖。
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.Contains(line, "locked") {
		t.Fatalf("child did not acquire lock (line=%q err=%v)", line, err)
	}

	// 子程序持鎖中：本程序以短逾時取鎖必須回 ErrStateLockTimeout，且不 hang。
	start := time.Now()
	lock, aerr := acquireFileLock(lockPath, 200*time.Millisecond)
	elapsed := time.Since(start)
	if aerr == nil {
		lock.release()
		t.Fatal("expected timeout while child holds lock, got success")
	}
	if !errors.Is(aerr, ErrStateLockTimeout) {
		t.Errorf("err = %v, want errors.Is ErrStateLockTimeout", aerr)
	}
	if elapsed > 3*time.Second {
		t.Errorf("acquire hung for %v", elapsed)
	}

	// 等子程序退出（釋放鎖）。
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child exit: %v", err)
	}

	// 子程序退出後，走同一把鎖的 UpdateState 應成功。
	got, err := ws.UpdateState(id, func(s *State) error {
		s.GuardRetries++
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateState after child release: %v", err)
	}
	if got.GuardRetries != 1 {
		t.Errorf("GuardRetries = %d, want 1", got.GuardRetries)
	}
}

// TestHelperProcess 為 TestCrossProcLock re-exec 的子程序：取得 STATELOCK_PATH 上的鎖，
// 對 stdout 印出 "locked" 通知父程序，持鎖 STATELOCK_HOLD_MS 毫秒後釋放退出。
// 非子程序情境（未設 GO_WANT_HELPER_PROCESS）直接返回，不影響一般測試。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	lockPath := os.Getenv("STATELOCK_PATH")
	holdMs, _ := strconv.Atoi(os.Getenv("STATELOCK_HOLD_MS"))

	lock, err := acquireFileLock(lockPath, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper acquire failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("locked")
	os.Stdout.Sync()
	time.Sleep(time.Duration(holdMs) * time.Millisecond)
	_ = lock.release()
	os.Exit(0)
}
