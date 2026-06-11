package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// ExitSoftFail 表示 plugin 可恢復的失敗
const ExitSoftFail = 1

// ExitHardError 表示 plugin 不可恢復的錯誤
const ExitHardError = 2

// Result 是 plugin 執行的結果
type Result struct {
	ExitCode    int
	DurationSec float64
}

// Runner 定義 plugin 的呼叫介面
type Runner interface {
	// Run 執行指定 role 的 plugin
	Run(ctx context.Context, featureID string, role protocol.Role) (*Result, error)
	// DryRun 印出 prompt 但不執行
	DryRun(featureID string, role protocol.Role) (string, error)
}

// SubprocessRunner 透過 os/exec 呼叫 plugin binary
type SubprocessRunner struct {
	Workspace *protocol.Workspace
	Config    protocol.RunnerConfig
	Name      string
	Timeout   time.Duration
}

// Run 執行 plugin subprocess
func (r *SubprocessRunner) Run(ctx context.Context, featureID string, role protocol.Role) (*Result, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmdName := fmt.Sprintf("4x-plugin-%s", r.Name)
	args := []string{"run", "--feature-id", featureID, "--role", string(role), "--workspace", r.Workspace.Root}

	start := time.Now()
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = r.Workspace.Root

	err := cmd.Run()
	duration := time.Since(start).Seconds()

	result := &Result{DurationSec: duration}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{ExitCode: ExitSoftFail, DurationSec: duration},
				fmt.Errorf("plugin %s timed out after %v", r.Name, r.Timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("plugin %s failed to start: %w", r.Name, err)
		}
	}

	return result, nil
}

// DryRun 用 --dry-run flag 呼叫 plugin，回傳 prompt 內容
func (r *SubprocessRunner) DryRun(featureID string, role protocol.Role) (string, error) {
	cmdName := fmt.Sprintf("4x-plugin-%s", r.Name)
	args := []string{"run", "--feature-id", featureID, "--role", string(role), "--workspace", r.Workspace.Root, "--dry-run"}

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = r.Workspace.Root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("dry-run failed: %w", err)
	}
	return string(out), nil
}

// IsSoftFail 判斷是否為可恢復的失敗
func IsSoftFail(r *Result) bool {
	return r != nil && r.ExitCode == ExitSoftFail
}

// IsHardError 判斷是否為不可恢復的錯誤
func IsHardError(r *Result) bool {
	return r != nil && r.ExitCode == ExitHardError
}

// NewRunner 從 config 建立 Runner
func NewRunner(ws *protocol.Workspace, name string, cfg protocol.RunnerConfig, timeout time.Duration) Runner {
	return &SubprocessRunner{
		Workspace: ws,
		Config:    cfg,
		Name:      name,
		Timeout:   timeout,
	}
}
