package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
	"github.com/ggwhite/4x/internal/protocol"
)

const ExitSoftFail = 1
const ExitHardError = 2

// Result 是 plugin 執行的結果
type Result struct {
	ExitCode    int
	DurationSec float64
	LogFile     string
}

// Runner 定義 plugin 的呼叫介面
type Runner interface {
	Run(ctx context.Context, prompt string) (*Result, error)
}

// SubprocessRunner 透過 config 定義的 command + args 呼叫 LLM CLI
type SubprocessRunner struct {
	Workspace     *protocol.Workspace
	Config        protocol.RunnerConfig
	Name          string
	Timeout       time.Duration
	LogPath       string
	ModelOverride string
}

// Run 用 config 的 command/args 執行，替換 {prompt} 和 {promptFile}
func (r *SubprocessRunner) Run(ctx context.Context, prompt string) (*Result, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	args, cleanup := r.buildArgs(prompt)
	if cleanup != nil {
		defer cleanup()
	}

	var logFile *os.File
	if r.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(r.LogPath), 0o755); err == nil {
			if f, err := os.Create(r.LogPath); err == nil {
				logFile = f
				defer logFile.Close()
			}
		}
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, r.Config.Command, args...)
	cmd.Dir = r.Workspace.Root

	usePty := r.Config.Tty && logFile != nil
	var ptmx *os.File

	if usePty {
		attrs := &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		var err error
		ptmx, err = pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 120}, attrs)
		if err != nil {
			return nil, fmt.Errorf("runner %s failed to start (pty): %w", r.Name, err)
		}

		stripW := newAnsiStripper(logFile)
		copyDone := make(chan struct{})
		go func() {
			io.Copy(io.MultiWriter(os.Stdout, stripW), ptmx)
			close(copyDone)
		}()

		err = cmd.Wait()
		ptmx.Close()
		<-copyDone

		duration := time.Since(start).Seconds()
		return r.buildResult(ctx, err, duration)
	}

	if logFile != nil {
		if r.Config.Quiet {
			cmd.Stdout = logFile
			cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
		} else {
			cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
			cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
		}
	} else {
		if r.Config.Quiet {
			cmd.Stdout = io.Discard
		} else {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
	}
	if r.Config.Stdin {
		cmd.Stdin = strings.NewReader(prompt)
	}

	err := cmd.Run()
	duration := time.Since(start).Seconds()
	return r.buildResult(ctx, err, duration)
}

func (r *SubprocessRunner) buildResult(ctx context.Context, err error, duration float64) (*Result, error) {
	result := &Result{DurationSec: duration}
	if r.LogPath != "" {
		result.LogFile = r.LogPath
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{ExitCode: ExitSoftFail, DurationSec: duration, LogFile: r.LogPath},
				fmt.Errorf("runner %s timed out after %v", r.Name, r.Timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("runner %s failed to start: %w", r.Name, err)
		}
	}

	return result, nil
}

func (r *SubprocessRunner) buildArgs(prompt string) ([]string, func()) {
	args := make([]string, len(r.Config.Args))
	var cleanup func()
	modelHandled := false

	for i, arg := range r.Config.Args {
		switch {
		case strings.Contains(arg, "{prompt}"):
			args[i] = strings.ReplaceAll(arg, "{prompt}", prompt)
		case strings.Contains(arg, "{promptFile}"):
			f, err := os.CreateTemp("", "4x-prompt-*.md")
			if err == nil {
				f.WriteString(prompt)
				f.Close()
				args[i] = strings.ReplaceAll(arg, "{promptFile}", f.Name())
				cleanup = func() { os.Remove(f.Name()) }
			} else {
				args[i] = arg
			}
		case strings.Contains(arg, "{model}"):
			if r.ModelOverride != "" {
				args[i] = strings.ReplaceAll(arg, "{model}", r.ModelOverride)
				modelHandled = true
			} else {
				args[i] = arg
			}
		default:
			args[i] = arg
		}
	}

	if r.ModelOverride != "" && !modelHandled {
		args = append(args, "--model", r.ModelOverride)
	}

	return args, cleanup
}

func IsSoftFail(r *Result) bool {
	return r != nil && r.ExitCode == ExitSoftFail
}

func IsHardError(r *Result) bool {
	return r != nil && r.ExitCode == ExitHardError
}

// NewRunner 建立 SubprocessRunner，logPath 為空字串時不產生 log file，model 為空字串時不帶 --model flag
func NewRunner(ws *protocol.Workspace, name string, cfg protocol.RunnerConfig, timeout time.Duration, logPath string, model string) Runner {
	return &SubprocessRunner{
		Workspace:     ws,
		Config:        cfg,
		Name:          name,
		Timeout:       timeout,
		LogPath:       logPath,
		ModelOverride: model,
	}
}

// ansiStripper 以狀態機跨 Write 呼叫正確剝除 ANSI escape sequence，
// 涵蓋 CSI（含 private mode ?）、OSC（BEL 或 ST 結尾）、單字元 ESC 序列。
type ansiStripper struct {
	w     io.Writer
	state stripState
}

type stripState int

const (
	stGround stripState = iota
	stEscape
	stCSI
	stOSC
	stOscEsc // OSC 裡遇到 ESC，等 backslash 組成 ST
)

func newAnsiStripper(w io.Writer) *ansiStripper {
	return &ansiStripper{w: w}
}

func (a *ansiStripper) Write(p []byte) (int, error) {
	start := 0
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch a.state {
		case stGround:
			if b == 0x1b {
				if i > start {
					a.w.Write(p[start:i])
				}
				a.state = stEscape
				start = i
			}
		case stEscape:
			switch {
			case b == '[':
				a.state = stCSI
			case b == ']':
				a.state = stOSC
			case b == '(' || b == ')':
				// charset designation: skip one more byte
				if i+1 < len(p) {
					i++
				}
				a.state = stGround
				start = i + 1
			default:
				// single-char ESC sequence (e.g. \x1b7, \x1bM)
				a.state = stGround
				start = i + 1
			}
		case stCSI:
			// CSI 參數與中間位元組：0x20-0x3F（含 ?;digits space 等）
			// 結束位元組：0x40-0x7E
			if b >= 0x40 && b <= 0x7E {
				a.state = stGround
				start = i + 1
			}
		case stOSC:
			if b == 0x07 {
				a.state = stGround
				start = i + 1
			} else if b == 0x1b {
				a.state = stOscEsc
			}
		case stOscEsc:
			// ST = ESC + backslash
			a.state = stGround
			start = i + 1
		}
	}

	if a.state == stGround && start < len(p) {
		a.w.Write(p[start:])
	}
	// state != stGround 時，未完成的 escape 序列暫存到下次 Write
	return len(p), nil
}

// LogDir 回傳 .4x/<featureId>/logs/ 的路徑
func LogDir(ws *protocol.Workspace, featureID string) string {
	return filepath.Join(ws.FeatureDir(featureID), "logs")
}

// LogFileName 產生 log 檔名：round-<N>-<role>.log
func LogFileName(round int, role string) string {
	return fmt.Sprintf("round-%d-%s.log", round, role)
}
