package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// RunInfo 描述一個執行中的 4x run subprocess。
type RunInfo struct {
	ID        string    `json:"id"`
	FeatureID string    `json:"featureId"`
	Runner    string    `json:"runner"`
	Pid       int       `json:"pid,omitempty"`
	Cmd       *exec.Cmd `json:"-"`
	StartTime time.Time `json:"startTime"`
	done      chan struct{}
}

// ProcessManager 管理 4x run subprocess 的生命週期。
type ProcessManager struct {
	mu          sync.Mutex
	runs        map[string]*RunInfo
	maxParallel int
	ws          *protocol.Workspace
	binName     string
}

// NewProcessManager 建立 ProcessManager，binName 通常是 "4x"，測試可替換成假 command。
func NewProcessManager(ws *protocol.Workspace, maxParallel int, binName string) *ProcessManager {
	if maxParallel <= 0 {
		maxParallel = 1
	}
	if binName == "" {
		binName = "4x"
	}
	return &ProcessManager{
		runs:        make(map[string]*RunInfo),
		maxParallel: maxParallel,
		ws:          ws,
		binName:     binName,
	}
}

// SetMaxParallel 更新最大併發數，供設定變更後即時生效。
func (pm *ProcessManager) SetMaxParallel(n int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if n <= 0 {
		n = 1
	}
	pm.maxParallel = n
}

// Start 啟動一個 4x run subprocess。
func (pm *ProcessManager) Start(featureID, runner string, maxRounds int) (*RunInfo, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, r := range pm.runs {
		if r.FeatureID == featureID {
			return nil, fmt.Errorf("feature %s already has a running process (run %s, pid %d)", featureID, r.ID, r.Pid)
		}
	}

	if len(pm.runs) >= pm.maxParallel {
		return nil, fmt.Errorf("max concurrent runs reached (%d)", pm.maxParallel)
	}

	cmd := exec.Command(pm.binName, buildRunArgs(featureID, runner, maxRounds)...)
	cmd.Dir = pm.ws.Root

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start subprocess: %w", err)
	}

	id, err := newRunID()
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	info := &RunInfo{
		ID:        id,
		FeatureID: featureID,
		Runner:    runner,
		Pid:       cmd.Process.Pid,
		Cmd:       cmd,
		StartTime: time.Now().UTC(),
		done:      make(chan struct{}),
	}
	pm.runs[id] = info

	go pm.pipeOutput(featureID, stdout, "run-output")
	go pm.pipeOutput(featureID, stderr, "run-error")
	go pm.wait(id, info)

	return cloneRunInfo(info), nil
}

func buildRunArgs(featureID, runner string, maxRounds int) []string {
	args := []string{"run", featureID}
	if runner != "" {
		args = append(args, "--runner", runner)
	}
	if maxRounds > 0 {
		args = append(args, "--max-rounds", strconv.Itoa(maxRounds))
	}
	return args
}

func newRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	return "run-" + hex.EncodeToString(b[:]), nil
}

func (pm *ProcessManager) pipeOutput(featureID string, r io.Reader, eventType string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		_ = pm.ws.AppendEvent(featureID, protocol.Event{
			Type:   eventType,
			Detail: line,
			Round:  0,
		})
	}
}

func (pm *ProcessManager) wait(id string, info *RunInfo) {
	_ = info.Cmd.Wait()

	pm.mu.Lock()
	delete(pm.runs, id)
	pm.mu.Unlock()

	// 以 process 結束當下時間作為比較門檻：若 runner 衍生的 child process
	// （如 `4x transition`）在 main process 退出後才寫 final state，其 UpdatedAt
	// 會 >= 此時間，ensureInactive 便不會用較舊的 in-memory 值蓋掉它。
	pm.ensureInactive(info.FeatureID, time.Now().UTC())

	close(info.done)
}

// ensureInactive 確保 subprocess 結束後 state.json 的 active 被設為 false，
// 防止 process crash 導致 dashboard 永遠顯示 running。
//
// endTime 為 process 結束的時間點。若 ReadState 讀到的 state 其 UpdatedAt 在
// endTime 之後（含同時），代表 runner 已在 process 結束時或之後自行寫過 final state
// （如 pending-review），此時直接跳過，避免用較舊的狀態把 phase 倒退或蓋掉 StopReason。
func (pm *ProcessManager) ensureInactive(featureID string, endTime time.Time) {
	s, err := pm.ws.ReadState(featureID)
	if err != nil {
		return
	}
	if !s.Active {
		return
	}
	if !s.UpdatedAt.Before(endTime) {
		return
	}
	s.Active = false
	s.Pid = 0
	if s.StopReason == "" {
		s.StopReason = "process-exit"
	}
	if err := pm.ws.WriteState(featureID, s); err != nil {
		fmt.Fprintf(os.Stderr, "[4x] ensureInactive: failed to write state for %s: %v\n", featureID, err)
	}
}

// Stop 終止指定的 run（SIGTERM → 5 秒 → SIGKILL）。
func (pm *ProcessManager) Stop(id string) error {
	pm.mu.Lock()
	info, ok := pm.runs[id]
	pm.mu.Unlock()
	if !ok {
		return fmt.Errorf("run %q not found", id)
	}

	return terminateRun(info)
}

// List 回傳所有執行中的 run。
func (pm *ProcessManager) List() []RunInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	runs := make([]RunInfo, 0, len(pm.runs))
	for _, info := range pm.runs {
		runs = append(runs, *cloneRunInfo(info))
	}
	return runs
}

// Shutdown 終止所有仍在執行中的 subprocess。
func (pm *ProcessManager) Shutdown() {
	pm.mu.Lock()
	runs := make([]*RunInfo, 0, len(pm.runs))
	for _, info := range pm.runs {
		runs = append(runs, info)
	}
	pm.mu.Unlock()

	for _, info := range runs {
		_ = terminateRun(info)
	}
}

func terminateRun(info *RunInfo) error {
	if info.Cmd.Process == nil {
		return nil
	}
	select {
	case <-info.done:
		return nil
	default:
	}

	if err := info.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		select {
		case <-info.done:
			return nil
		default:
			return err
		}
	}

	select {
	case <-info.done:
		return nil
	case <-time.After(5 * time.Second):
		if err := info.Cmd.Process.Kill(); err != nil {
			select {
			case <-info.done:
				return nil
			default:
				return err
			}
		}
		<-info.done
		return nil
	}
}

func cloneRunInfo(info *RunInfo) *RunInfo {
	return &RunInfo{
		ID:        info.ID,
		FeatureID: info.FeatureID,
		Runner:    info.Runner,
		Pid:       info.Pid,
		StartTime: info.StartTime,
	}
}
