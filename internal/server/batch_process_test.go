package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// fakeBatchCommand 回傳一個忽略參數、輪詢 .4x/batch-stop 直到出現才結束的假 batch 執行檔，
// 讓 BatchManager.Stop 的 graceful 停止語意可被測試（不需依賴真實 4x binary）。
func fakeBatchCommand(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-batch")
	script := "#!/bin/sh\nwhile [ ! -f .4x/batch-stop ]; do sleep 0.02; done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// AC-9：Start 在已有 batch 執行中時回 error；Running 反映狀態，Stop 後回 false。
func TestBatchManager_StartTwiceErrors(t *testing.T) {
	ws := setupPMWorkspace(t)
	bm := NewBatchManager(ws, fakeBatchCommand(t))

	if err := bm.Start("test", 5); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	waitUntil(t, bm.Running)

	if err := bm.Start("test", 5); err == nil {
		t.Error("second Start should error while batch is running")
	}

	if err := bm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitUntil(t, func() bool { return !bm.Running() })
}

// AC-10：Stop 寫出 .4x/batch-stop 信號檔（graceful），不直接 SIGKILL。
func TestBatchManager_StopWritesSignal(t *testing.T) {
	ws := setupPMWorkspace(t)
	bm := NewBatchManager(ws, "echo")

	if err := bm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
	if _, err := os.Stat(stopFile); err != nil {
		t.Errorf("expected %s to exist after Stop: %v", stopFile, err)
	}
}

// F075：Adopt 認領 .4x/batch-pid 指向的存活孤兒程序，標記 running；孤兒結束後 watcher 清掉 running 與 PID 檔。
func TestBatchManager_AdoptAliveOrphan(t *testing.T) {
	ws := setupPMWorkspace(t)

	// 起一個真實存活的程序當作孤兒，寫入其 PID。
	orphan := exec.Command("sleep", "30")
	if err := orphan.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	defer func() { _ = orphan.Process.Kill(); _, _ = orphan.Process.Wait() }()
	if err := ws.WriteBatchPID(orphan.Process.Pid); err != nil {
		t.Fatalf("WriteBatchPID: %v", err)
	}

	bm := NewBatchManager(ws, "echo")
	bm.Adopt()
	if !bm.Running() {
		t.Fatal("Adopt should mark running for an alive orphan")
	}

	// 孤兒結束後，watcher（2s ticker）最終會把 running 清為 false 並刪除 PID 檔。
	_ = orphan.Process.Kill()
	_, _ = orphan.Process.Wait()
	waitUntil(t, func() bool { return !bm.Running() })
	if pid, _ := ws.ReadBatchPID(); pid != 0 {
		t.Errorf("expected batch-pid cleared after orphan exit, got %d", pid)
	}
}

// F075：Adopt 遇到指向已死程序的 stale PID 檔時，清掉檔案且維持非執行狀態。
func TestBatchManager_AdoptStalePID(t *testing.T) {
	ws := setupPMWorkspace(t)

	// 起一個程序立即結束以取得一個已死的 PID。
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatalf("run dead: %v", err)
	}
	if err := ws.WriteBatchPID(dead.Process.Pid); err != nil {
		t.Fatalf("WriteBatchPID: %v", err)
	}

	bm := NewBatchManager(ws, "echo")
	bm.Adopt()
	if bm.Running() {
		t.Error("Adopt should not mark running for a dead PID")
	}
	if pid, _ := ws.ReadBatchPID(); pid != 0 {
		t.Errorf("expected stale batch-pid cleared, got %d", pid)
	}
}

// F075：Shutdown 主動終止 adopted 孤兒並清除 PID 檔，不留孤兒。
func TestBatchManager_ShutdownKillsAdopted(t *testing.T) {
	ws := setupPMWorkspace(t)

	orphan := exec.Command("sleep", "30")
	if err := orphan.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	defer func() { _ = orphan.Process.Kill(); _, _ = orphan.Process.Wait() }()
	pid := orphan.Process.Pid
	if err := ws.WriteBatchPID(pid); err != nil {
		t.Fatalf("WriteBatchPID: %v", err)
	}

	bm := NewBatchManager(ws, "echo")
	bm.Adopt()
	if !bm.Running() {
		t.Fatal("Adopt should mark running")
	}

	if err := bm.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if bm.Running() {
		t.Error("Running should be false after Shutdown")
	}
	if got, _ := ws.ReadBatchPID(); got != 0 {
		t.Errorf("expected batch-pid cleared after Shutdown, got %d", got)
	}
	// orphan 是測試程序的子程序，被 kill 後會成為 zombie 直到被 reap（真實情境由 init 回收）；
	// 這裡主動 Wait 回收，避免 ProcessAlive 因 zombie 仍回 true。
	_, _ = orphan.Process.Wait()
	if protocol.ProcessAlive(pid) {
		t.Error("orphan process should be terminated after Shutdown")
	}
}

// F075：Shutdown 主動終止 managed 子程序（不依賴 batch-stop 信號檔）。
func TestBatchManager_ShutdownKillsManaged(t *testing.T) {
	ws := setupPMWorkspace(t)
	// fake batch 會 busy-wait 直到 batch-stop 出現；Shutdown 應在不寫信號檔的情況下直接送訊號終止。
	bm := NewBatchManager(ws, fakeBatchCommand(t))

	if err := bm.Start("test", 5); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, bm.Running)

	if err := bm.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	waitUntil(t, func() bool { return !bm.Running() })

	// Shutdown 走主動 kill 路徑，不應寫出 graceful 信號檔。
	stopFile := filepath.Join(ws.DotDir(), protocol.BatchStopFile)
	if _, err := os.Stat(stopFile); err == nil {
		t.Error("Shutdown should not write batch-stop signal file")
	}
}
