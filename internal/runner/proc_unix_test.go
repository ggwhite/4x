//go:build !windows

package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestSetupProcGroup_Configures 驗證 setupProcGroup 設好 Setpgid、Cancel、WaitDelay。
func TestSetupProcGroup_Configures(t *testing.T) {
	cmd := exec.Command("true")
	setupProcGroup(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid 未設定")
	}
	if cmd.Cancel == nil {
		t.Error("Cancel callback 未設定")
	}
	if cmd.WaitDelay != gracefulShutdownDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, gracefulShutdownDelay)
	}
}

// TestSetupProcGroup_SigtermFirst 驗證 context 取消時子程序先收到 SIGTERM，
// 有機會 graceful shutdown，而非直接被 SIGKILL 硬殺。
func TestSetupProcGroup_SigtermFirst(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "got-term")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 子程序裝上 SIGTERM trap：收到時寫 marker 並 graceful exit。
	// SIGKILL 無法被 trap，若被直接硬殺則 marker 不會出現。
	script := `trap 'echo term > "$0"; exit 0' TERM; sleep 30`
	cmd := exec.CommandContext(ctx, "sh", "-c", script, marker)
	setupProcGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 等子程序裝好 trap 再取消
	time.Sleep(300 * time.Millisecond)
	cancel()
	_ = cmd.Wait()

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("子程序未收到 SIGTERM（marker 不存在），可能被直接 SIGKILL: %v", err)
	}
}
