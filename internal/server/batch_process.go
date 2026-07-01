package server

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// BatchManager 管理單一 project 的 `4x batch run` 子程序。
// 一個 project 同時只允許一個 batch run，避免兩個排程互相干擾；
// 與 ProcessManager（管 `4x run <feature>`）分離，兩者生命週期互不影響。
type BatchManager struct {
	mu         sync.Mutex
	cmd        Cmd
	ws         BatchWorkspace
	root       string
	binName    string
	launcher   Launcher
	running    bool
	done       chan struct{}
	adoptedPid int // 被 adopt 的孤兒子程序 PID，供 Shutdown 用 PID kill；managed 子程序時維持 0
}

// NewBatchManager 建立 BatchManager，binName 通常是當前 4x 執行檔路徑，測試可替換成假 command。
func NewBatchManager(ws *protocol.Workspace, binName string) *BatchManager {
	if binName == "" {
		binName = "4x"
	}
	return &BatchManager{ws: ws, root: ws.Root, binName: binName, launcher: execLauncher{}}
}

// Start 啟動 `4x batch run` 子程序；若已有 batch 執行中則回 error（同 project 不可並行兩個 batch）。
func (bm *BatchManager) Start(runner string, maxRounds int) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.running {
		return fmt.Errorf("a batch run is already in progress")
	}

	cmd := bm.launcher.Command(bm.binName, buildBatchArgs(runner, maxRounds)...)
	cmd.SetDir(bm.root)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start batch subprocess: %w", err)
	}

	bm.cmd = cmd
	bm.running = true
	bm.done = make(chan struct{})

	go bm.pipeOutput(stdout)
	go bm.pipeOutput(stderr)
	go bm.wait(bm.done)

	return nil
}

func buildBatchArgs(runner string, maxRounds int) []string {
	args := []string{"batch", "run"}
	if runner != "" {
		args = append(args, "--runner", runner)
	}
	if maxRounds > 0 {
		args = append(args, "--max-rounds", strconv.Itoa(maxRounds))
	}
	return args
}

// pipeOutput 將 batch 子程序輸出逐行 append 到 .4x/batch.log，供 dashboard 與除錯查看。
func (bm *BatchManager) pipeOutput(r io.Reader) {
	logPath := filepath.Join(bm.ws.DotDir(), "batch.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		// 無法寫 log 時仍須排空 pipe，避免子程序因 buffer 滿而 block。
		_, _ = io.Copy(io.Discard, r)
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			fmt.Fprintln(f, line)
		}
	}
}

func (bm *BatchManager) wait(done chan struct{}) {
	cmd := bm.cmd
	if cmd != nil {
		if err := cmd.Wait(); err != nil {
			slog.Warn("batch subprocess exited with error", "error", err)
		}
	}
	bm.mu.Lock()
	bm.running = false
	bm.cmd = nil
	bm.mu.Unlock()
	close(done)
}

// Stop 寫出 .4x/batch-stop 信號檔（graceful）：batch 跑完當前 feature 後自行 break，不直接 SIGKILL。
func (bm *BatchManager) Stop() error {
	return bm.ws.WriteBatchStop()
}

// Adopt 在 server 啟動時呼叫，據 .4x/batch-pid 認領前一個 server 啟動、現以孤兒身份存活的
// batch run 子程序：若該 PID 仍存活則標記為 running 並啟動 watcher 監控其結束；
// 若無 PID 檔或程序已死，則清掉 stale PID 檔、維持非執行狀態。
func (bm *BatchManager) Adopt() {
	pid, err := bm.ws.ReadBatchPID()
	if err != nil || pid <= 0 || !protocol.ProcessAlive(pid) {
		if cerr := bm.ws.ClearBatchPID(); cerr != nil {
			slog.Warn("failed to clear stale batch PID", "error", cerr)
		}
		return
	}

	bm.mu.Lock()
	bm.running = true
	bm.adoptedPid = pid
	bm.mu.Unlock()

	go bm.watchAdopted(pid)
}

// watchAdopted 輪詢被 adopt 的孤兒 PID，一旦該程序結束即清除 running/adoptedPid 與 stale PID 檔。
func (bm *BatchManager) watchAdopted(pid int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if protocol.ProcessAlive(pid) {
			continue
		}
		bm.mu.Lock()
		bm.running = false
		bm.adoptedPid = 0
		bm.mu.Unlock()
		if err := bm.ws.ClearBatchPID(); err != nil {
			slog.Warn("failed to clear batch PID after adopted process exit", "error", err)
		}
		return
	}
}

// Shutdown 在 server 關閉時主動終止 batch 子程序（SIGTERM → 最多 5 秒 → SIGKILL），
// 涵蓋 managed（本 server 起的子程序）與 adopted（重啟後認領的孤兒）兩種情況，避免關閉時殘留孤兒。
// 與 Stop（graceful 寫信號檔）不同，Shutdown 直接送訊號終止程序。
func (bm *BatchManager) Shutdown() error {
	bm.mu.Lock()
	cmd := bm.cmd
	done := bm.done
	adoptedPid := bm.adoptedPid
	bm.mu.Unlock()

	var err error
	switch {
	case cmd != nil:
		err = terminateProcess(cmd, done)
	case adoptedPid != 0:
		err = terminateAdopted(adoptedPid)
	}

	bm.mu.Lock()
	bm.running = false
	bm.adoptedPid = 0
	bm.mu.Unlock()
	if cerr := bm.ws.ClearBatchPID(); cerr != nil {
		slog.Warn("failed to clear batch PID during shutdown", "error", cerr)
	}
	return err
}

// terminateProcess 對 managed 子程序送 SIGTERM，等其結束（done channel）最多 5 秒，逾時則 Kill。
func terminateProcess(cmd Cmd, done chan struct{}) error {
	if err := cmd.Signal(syscall.SIGTERM); err != nil {
		// 程序可能已結束；若 done 已關閉視為正常。
		if done != nil {
			select {
			case <-done:
				return nil
			default:
			}
		}
		return err
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = cmd.Kill()
		<-done
		return nil
	}
}

// terminateAdopted 對 adopted 孤兒 PID 送 SIGTERM，輪詢存活狀態最多 5 秒，仍存活則送 SIGKILL。
// 用 os.FindProcess + Process.Signal/Kill 而非 syscall.Kill，以維持跨平台（Windows 無 syscall.Kill）。
func terminateAdopted(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !protocol.ProcessAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if protocol.ProcessAlive(pid) {
		return proc.Kill()
	}
	return nil
}

// Running 回報目前是否有 batch run 執行中。
func (bm *BatchManager) Running() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.running
}
