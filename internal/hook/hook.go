package hook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// Result 記錄單一 hook 的執行結果。
type Result struct {
	Command  string  `json:"command"`
	ExitCode int     `json:"exitCode"`
	Status   string  `json:"status"`
	Duration float64 `json:"duration"`
	LogFile  string  `json:"logFile,omitempty"`
}

// Execute 依序執行 hooks，logDir 用來存放 stdout/stderr output 檔。
// 遇到 on_fail=block 的 hook 失敗時立即停止並回傳 error；
// on_fail=warn 的 hook 失敗只記錄 result，不中止後續執行。
func Execute(ctx context.Context, hooks []feature.HookEntry, logDir string) ([]Result, error) {
	if len(hooks) == 0 {
		return nil, nil
	}

	logDirReady := false
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "hook: failed to create log dir %q: %v\n", logDir, err)
		} else {
			logDirReady = true
		}
	}

	var results []Result

	for i, h := range hooks {
		start := time.Now()
		cmd := exec.CommandContext(ctx, "sh", "-c", h.Run)

		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output

		err := cmd.Run()
		dur := time.Since(start).Seconds()

		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		status := "pass"
		if exitCode != 0 {
			status = "fail"
		}

		logFile := ""
		if logDirReady {
			ts := time.Now().Format("20060102-150405.000")
			candidate := filepath.Join(logDir, fmt.Sprintf("%s-hook-%d.log", ts, i))
			if werr := os.WriteFile(candidate, output.Bytes(), 0o644); werr != nil {
				fmt.Fprintf(os.Stderr, "hook: failed to write log file %q: %v\n", candidate, werr)
			} else {
				logFile = candidate
			}
		}

		r := Result{
			Command:  h.Run,
			ExitCode: exitCode,
			Status:   status,
			Duration: dur,
			LogFile:  logFile,
		}
		results = append(results, r)

		if exitCode != 0 && h.EffectiveOnFail() == "block" {
			return results, fmt.Errorf("hook %q failed (exit %d)", h.Run, exitCode)
		}
	}

	return results, nil
}

// ToEvent 將 hook Result 轉成 protocol.Event，供寫入 events.jsonl。
// Timestamp 留空，AppendEvent 會自動補 RFC3339。
func ToEvent(r Result, phase protocol.Phase, action string) protocol.Event {
	detail := fmt.Sprintf("exit %d, %.1fs", r.ExitCode, r.Duration)
	return protocol.Event{
		Type:    "hook",
		Phase:   phase,
		Action:  action,
		Command: r.Command,
		Status:  r.Status,
		Detail:  detail,
	}
}
