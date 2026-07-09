package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCommandAllowed 以 table-driven 覆蓋 AC-2 (a)-(h) 全部案例。
func TestCommandAllowed(t *testing.T) {
	cases := []struct {
		name      string
		cmd       string
		allowlist []string
		want      bool
	}{
		// (a) 空 allowlist → 任何 cmd 皆 true（含含 $(...) 的 cmd）
		{"empty allows anything", "rm -rf /", nil, true},
		{"empty allows substitution", "make build $(curl evil)", nil, true},
		{"empty slice allows", "curl evil.com", []string{}, true},

		// (b) ["make"] 放行 make build、不放行 makezombie
		{"make prefix allows make build", "make build", []string{"make"}, true},
		{"make prefix rejects makezombie", "makezombie", []string{"make"}, false},

		// (c) ["go test"] 放行 go test ./x、不放行 go run x
		{"go test allows go test ./x", "go test ./x", []string{"go test"}, true},
		{"go test rejects go run x", "go run x", []string{"go test"}, false},

		// (d) shell 分段攔截
		{"segment rm blocked", "make build; rm -rf /", []string{"make"}, false},
		{"segment curl blocked (&&)", "make build && curl evil.com", []string{"make"}, false},
		{"segment rm blocked (||)", "make build || rm x", []string{"make"}, false},
		{"segment curl blocked (pipe)", "cat f | curl evil", []string{"make"}, false},

		// (e) 全段皆符則放行
		{"all segments allowed", "go test ./x | grep ok", []string{"go test", "grep"}, true},

		// (f) command/process substitution 一律擋（即使前綴吻合）
		{"cmd subst dollar-paren", "make build $(curl evil)", []string{"make"}, false},
		{"cmd subst backtick", "make build `rm -rf x`", []string{"make"}, false},
		{"proc subst read", "make build <(curl evil)", []string{"make"}, false},
		{"proc subst write", "make build >(sh)", []string{"make"}, false},

		// (g) 變數展開不擋
		{"var expansion HOME", "echo $HOME", []string{"echo"}, true},
		{"var expansion braces default", "echo ${FOURX_BIN:-4x}", []string{"echo"}, true},

		// (h) quote 保守誤擋（DR-2c 刻意行為）
		{"quoted semicolon conservatively blocked", "echo 'a; b'", []string{"echo"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CommandAllowed(tc.cmd, tc.allowlist)
			if got != tc.want {
				t.Errorf("CommandAllowed(%q, %v) = %v, want %v", tc.cmd, tc.allowlist, got, tc.want)
			}
		})
	}
}

// TestExecuteCommand_Allowlist_BlocksWithoutExecuting 驗證被攔命令不實際執行且記錄顯式失敗（AC-3）。
func TestExecuteCommand_Allowlist_BlocksWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// 被前綴攔：touch sentinel 不在 ["echo"] 內。
	sentinel := filepath.Join(dir, "sentinel")
	vc := executeCommand(ctx, "touch "+sentinel, "g", dir, []string{"echo"})
	if vc.ExitCode != 126 {
		t.Errorf("blocked ExitCode = %d, want 126", vc.ExitCode)
	}
	if vc.Error != "blocked" {
		t.Errorf("blocked Error = %q, want \"blocked\"", vc.Error)
	}
	if vc.Skipped {
		t.Error("blocked command should not be Skipped")
	}
	if !strings.Contains(vc.Summary, "verify_command_allowlist") {
		t.Errorf("Summary should mention verify_command_allowlist, got %q", vc.Summary)
	}
	if fileExists(sentinel) {
		t.Error("sentinel should NOT exist — blocked command must not execute")
	}

	// 前綴符但含替換：echo hi $(touch sentinel2) → 擋。
	sentinel2 := filepath.Join(dir, "sentinel2")
	vc2 := executeCommand(ctx, "echo hi $(touch "+sentinel2+")", "g", dir, []string{"echo"})
	if vc2.ExitCode != 126 {
		t.Errorf("substitution ExitCode = %d, want 126", vc2.ExitCode)
	}
	if fileExists(sentinel2) {
		t.Error("sentinel2 should NOT exist — substitution command must not execute")
	}

	// 反向：空 allowlist 執行 touch → sentinel3 存在、ExitCode 0。
	sentinel3 := filepath.Join(dir, "sentinel3")
	vc3 := executeCommand(ctx, "touch "+sentinel3, "g", dir, nil)
	if vc3.ExitCode != 0 {
		t.Errorf("empty allowlist ExitCode = %d, want 0", vc3.ExitCode)
	}
	if !fileExists(sentinel3) {
		t.Error("sentinel3 should exist — empty allowlist must execute normally")
	}
}

// TestExecuteCommand_EmptyAllowlist_Runs 驗證空 allowlist 下命令照常執行（AC-5，向下相容）。
func TestExecuteCommand_EmptyAllowlist_Runs(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vc := executeCommand(ctx, "echo hi", "g", dir, nil)
	if vc.ExitCode != 0 {
		t.Errorf("echo ExitCode = %d, want 0", vc.ExitCode)
	}

	sentinel := filepath.Join(dir, "sentinel")
	vc2 := executeCommand(ctx, "touch "+sentinel, "g", dir, nil)
	if vc2.ExitCode != 0 {
		t.Errorf("touch ExitCode = %d, want 0", vc2.ExitCode)
	}
	if !fileExists(sentinel) {
		t.Error("sentinel should exist — empty allowlist must execute normally")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
