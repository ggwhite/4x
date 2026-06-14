package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/ggwhite/4x/internal/protocol"
)

// BatchManager 管理單一 project 的 `4x batch run` 子程序。
// 一個 project 同時只允許一個 batch run，避免兩個排程互相干擾；
// 與 ProcessManager（管 `4x run <feature>`）分離，兩者生命週期互不影響。
type BatchManager struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	ws      *protocol.Workspace
	binName string
	running bool
	done    chan struct{}
}

// NewBatchManager 建立 BatchManager，binName 通常是當前 4x 執行檔路徑，測試可替換成假 command。
func NewBatchManager(ws *protocol.Workspace, binName string) *BatchManager {
	if binName == "" {
		binName = "4x"
	}
	return &BatchManager{ws: ws, binName: binName}
}

// Start 啟動 `4x batch run` 子程序；若已有 batch 執行中則回 error（同 project 不可並行兩個 batch）。
func (bm *BatchManager) Start(runner string, maxRounds int) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.running {
		return fmt.Errorf("a batch run is already in progress")
	}

	cmd := exec.Command(bm.binName, buildBatchArgs(runner, maxRounds)...)
	cmd.Dir = bm.ws.Root

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
		_ = cmd.Wait()
	}
	bm.mu.Lock()
	bm.running = false
	bm.cmd = nil
	bm.mu.Unlock()
	close(done)
}

// Stop 寫出 .4x/batch-stop 信號檔（graceful）：batch 跑完當前 feature 後自行 break，不直接 SIGKILL。
func (bm *BatchManager) Stop() error {
	stopFile := filepath.Join(bm.ws.DotDir(), protocol.BatchStopFile)
	return os.WriteFile(stopFile, []byte("stop"), 0o644)
}

// Running 回報目前是否有 batch run 執行中。
func (bm *BatchManager) Running() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.running
}
