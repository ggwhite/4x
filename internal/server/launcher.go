package server

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// Cmd 抽出子程序需要的最小操作介面（設定工作目錄、輸出管線、啟動、等待、訊號），
// 讓 ProcessManager/BatchManager 依賴介面而非具體 *exec.Cmd，測試可注入假實作而不需真的產生子程序。
type Cmd interface {
	SetDir(dir string)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	Start() error
	Wait() error
	Pid() int
	Signal(sig os.Signal) error
	Kill() error
}

// Launcher 依 name/args 建立一個尚未啟動的 Cmd。
type Launcher interface {
	Command(name string, args ...string) Cmd
}

// execLauncher 是 Launcher 的預設實作，透過 os/exec 啟動真實子程序。
type execLauncher struct{}

// Command 建立一個包裝 *exec.Cmd 的 Cmd。
func (execLauncher) Command(name string, args ...string) Cmd {
	return &execCmd{cmd: exec.Command(name, args...)}
}

// execCmd 是 Cmd 的預設實作，包裝 *exec.Cmd。
type execCmd struct {
	cmd *exec.Cmd
}

func (c *execCmd) SetDir(dir string) { c.cmd.Dir = dir }

func (c *execCmd) StdoutPipe() (io.ReadCloser, error) { return c.cmd.StdoutPipe() }

func (c *execCmd) StderrPipe() (io.ReadCloser, error) { return c.cmd.StderrPipe() }

func (c *execCmd) Start() error { return c.cmd.Start() }

func (c *execCmd) Wait() error { return c.cmd.Wait() }

func (c *execCmd) Pid() int {
	if c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *execCmd) Signal(sig os.Signal) error {
	if c.cmd.Process == nil {
		return errors.New("process not started")
	}
	return c.cmd.Process.Signal(sig)
}

func (c *execCmd) Kill() error {
	if c.cmd.Process == nil {
		return errors.New("process not started")
	}
	return c.cmd.Process.Kill()
}
