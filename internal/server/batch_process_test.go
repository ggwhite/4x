package server

import (
	"os"
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
