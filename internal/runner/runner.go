package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

	start := time.Now()
	cmd := exec.CommandContext(ctx, r.Config.Command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = r.Workspace.Root

	err := cmd.Run()
	duration := time.Since(start).Seconds()

	result := &Result{DurationSec: duration}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{ExitCode: ExitSoftFail, DurationSec: duration},
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

func NewRunner(ws *protocol.Workspace, name string, cfg protocol.RunnerConfig, timeout time.Duration) Runner {
	return &SubprocessRunner{
		Workspace: ws,
		Config:    cfg,
		Name:      name,
		Timeout:   timeout,
	}
}
