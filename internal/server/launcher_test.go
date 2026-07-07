package server

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// fakeCmd 是 Cmd 的假實作，讓測試可控制子程序啟動/管線/訊號行為，不必真的產生 process。
type fakeCmd struct {
	dir        string
	stdoutErr  error
	stderrErr  error
	startErr   error
	waitErr    error
	signalErr  error
	killErr    error
	pid        int
	started    bool
	signals    []os.Signal
	killed     bool
	waitCalled bool
}

func (c *fakeCmd) SetDir(dir string) { c.dir = dir }

func (c *fakeCmd) StdoutPipe() (io.ReadCloser, error) {
	if c.stdoutErr != nil {
		return nil, c.stdoutErr
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (c *fakeCmd) StderrPipe() (io.ReadCloser, error) {
	if c.stderrErr != nil {
		return nil, c.stderrErr
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (c *fakeCmd) Start() error {
	c.started = true
	return c.startErr
}

func (c *fakeCmd) Wait() error {
	c.waitCalled = true
	return c.waitErr
}

func (c *fakeCmd) Pid() int { return c.pid }

func (c *fakeCmd) Signal(sig os.Signal) error {
	c.signals = append(c.signals, sig)
	return c.signalErr
}

func (c *fakeCmd) Kill() error {
	c.killed = true
	return c.killErr
}

// fakeLauncher 回傳固定的 fakeCmd，讓測試取得對子程序行為的完全控制權。
type fakeLauncher struct {
	cmd      *fakeCmd
	lastName string
	lastArgs []string
}

func (l *fakeLauncher) Command(name string, args ...string) Cmd {
	l.lastName = name
	l.lastArgs = args
	return l.cmd
}

func TestExecLauncher_CommandRunsRealProcess(t *testing.T) {
	var launcher Launcher = execLauncher{}
	cmd := launcher.Command("echo", "hello")
	cmd.SetDir(t.TempDir())

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if cmd.Pid() <= 0 {
		t.Errorf("Pid() = %d, want > 0 after Start", cmd.Pid())
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestExecCmd_SignalAndKillBeforeStartError(t *testing.T) {
	cmd := execLauncher{}.Command("echo", "hello")

	if cmd.Pid() != 0 {
		t.Errorf("Pid() before Start = %d, want 0", cmd.Pid())
	}
	if err := cmd.Signal(os.Interrupt); err == nil {
		t.Error("Signal before Start should error")
	}
	if err := cmd.Kill(); err == nil {
		t.Error("Kill before Start should error")
	}
}

// TestProcessManager_Start_StdoutPipeError 驗證 launcher 注入後可不啟動真實子程序
// 就測到 StdoutPipe 失敗會讓 Start 直接回錯誤、不留下任何 run 記錄。
func TestProcessManager_Start_StdoutPipeError(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, "unused")
	fake := &fakeLauncher{cmd: &fakeCmd{stdoutErr: errors.New("boom")}}
	pm.launcher = fake

	if _, err := pm.Start("test-feat", "", 5, "", nil); err == nil {
		t.Fatal("expected error when StdoutPipe fails")
	}
	if len(pm.List()) != 0 {
		t.Errorf("List() = %d runs after failed Start, want 0", len(pm.List()))
	}
	if fake.cmd.started {
		t.Error("cmd.Start should not be called when StdoutPipe fails")
	}
}

// TestProcessManager_Start_LauncherStartError 驗證子程序啟動失敗時 Start 回錯誤且不留下 run 記錄。
func TestProcessManager_Start_LauncherStartError(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, "unused")
	fake := &fakeLauncher{cmd: &fakeCmd{startErr: errors.New("start failed")}}
	pm.launcher = fake

	if _, err := pm.Start("test-feat", "", 5, "", nil); err == nil {
		t.Fatal("expected error when cmd.Start fails")
	}
	if len(pm.List()) != 0 {
		t.Errorf("List() = %d runs after failed Start, want 0", len(pm.List()))
	}
}

// TestTerminateRun_SignalSuccessClosesPromptly 驗證 Signal 成功且 done 已關閉時立即回傳 nil、不觸發 Kill。
func TestTerminateRun_SignalSuccessClosesPromptly(t *testing.T) {
	fake := &fakeCmd{}
	done := make(chan struct{})
	close(done)
	info := &RunInfo{Cmd: fake, done: done}

	if err := terminateRun(info); err != nil {
		t.Fatalf("terminateRun: %v", err)
	}
	if fake.killed {
		t.Error("Kill should not be called when done already closed")
	}
}

// TestTerminateRun_SignalErrorPropagates 驗證 Signal 失敗且 done 未關閉時回傳該錯誤。
func TestTerminateRun_SignalErrorPropagates(t *testing.T) {
	wantErr := errors.New("signal failed")
	fake := &fakeCmd{signalErr: wantErr}
	info := &RunInfo{Cmd: fake, done: make(chan struct{})}

	err := terminateRun(info)
	if !errors.Is(err, wantErr) {
		t.Errorf("terminateRun error = %v, want %v", err, wantErr)
	}
}

// TestTerminateRun_NilCmd 驗證 Cmd 為 nil（尚未啟動）時直接回傳 nil，不 panic。
func TestTerminateRun_NilCmd(t *testing.T) {
	info := &RunInfo{done: make(chan struct{})}
	if err := terminateRun(info); err != nil {
		t.Errorf("terminateRun with nil Cmd = %v, want nil", err)
	}
}

