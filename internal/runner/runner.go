package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	Workspace *protocol.Workspace
	Config    protocol.RunnerConfig
	Name      string
	Timeout   time.Duration
	LogPath   string
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
	if logFile != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
		cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	cmd.Dir = r.Workspace.Root
	if r.Config.Stdin {
		cmd.Stdin = strings.NewReader(prompt)
	}

	err := cmd.Run()
	duration := time.Since(start).Seconds()

	result := &Result{DurationSec: duration}
	if logFile != nil {
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
		default:
			args[i] = arg
		}
	}
	return args, cleanup
}

func IsSoftFail(r *Result) bool {
	return r != nil && r.ExitCode == ExitSoftFail
}

func IsHardError(r *Result) bool {
	return r != nil && r.ExitCode == ExitHardError
}

// NewRunner 建立 SubprocessRunner，logPath 為空字串時不產生 log file
func NewRunner(ws *protocol.Workspace, name string, cfg protocol.RunnerConfig, timeout time.Duration, logPath string) Runner {
	return &SubprocessRunner{
		Workspace: ws,
		Config:    cfg,
		Name:      name,
		Timeout:   timeout,
		LogPath:   logPath,
	}
}

// LogDir 回傳 .4x/<featureId>/logs/ 的路徑
func LogDir(ws *protocol.Workspace, featureID string) string {
	return filepath.Join(ws.FeatureDir(featureID), "logs")
}

// LogFileName 產生 log 檔名：round-<N>-<role>.log
func LogFileName(round int, role string) string {
	return fmt.Sprintf("round-%d-%s.log", round, role)
}
