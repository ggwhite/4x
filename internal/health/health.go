// Package health 在 testing phase 啟動 Tester role 前驗證環境，
// 失敗時嘗試 recovery 並重跑一次，避免浪費整輪 test cycle。
package health

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// RunHealthCheck 依序執行 cfg.Commands，任一失敗即停。
// 失敗時：無 Recovery → 回傳 error；有 Recovery → 逐一執行 Recovery，
// 成功則重跑一次 Commands（最多重試一次，避免無限循環），仍失敗回傳 error。
// cfg.Commands 為空時直接回傳 nil，executor 不會被呼叫。
// executor 由呼叫端注入以利測試（真實執行時包含 timeout 與 stderr 輸出）。
func RunHealthCheck(cfg protocol.HealthCheck, executor func(cmd string) error) error {
	err := runCommands(cfg.Commands, executor)
	if err == nil {
		return nil
	}

	if len(cfg.Recovery) == 0 {
		return fmt.Errorf("health check failed: %w", err)
	}

	if recErr := runCommands(cfg.Recovery, executor); recErr != nil {
		return fmt.Errorf("health check failed: %w; recovery also failed: %v", err, recErr)
	}

	if retryErr := runCommands(cfg.Commands, executor); retryErr != nil {
		return fmt.Errorf("health check failed after recovery: %w", retryErr)
	}
	return nil
}

// runCommands 逐一執行 cmds，任一失敗即停並回傳含 command 名稱的 error。
func runCommands(cmds []string, executor func(cmd string) error) error {
	for _, cmd := range cmds {
		if err := executor(cmd); err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return nil
}
