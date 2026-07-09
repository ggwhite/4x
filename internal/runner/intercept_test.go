package runner

import (
	"os"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// argsContainSettings 回傳 args 中 "--settings" 的下一個值（temp settings 檔路徑），找不到回空字串。
func argsSettingsPath(args []string) string {
	for i, a := range args {
		if a == "--settings" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// assertGuardToolSettings 讀出 --settings temp 檔並斷言內容含 guard-tool 與 Bash /
// Edit|Write|MultiEdit 兩個 PreToolUse matcher（AC-8）。
func assertGuardToolSettings(t *testing.T, args []string) {
	t.Helper()
	path := argsSettingsPath(args)
	if path == "" {
		t.Fatalf("expected --settings in args, got %v", args)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings temp file: %v", err)
	}
	content := string(data)
	for _, want := range []string{"guard-tool", "PreToolUse", `"matcher":"Bash"`, `"matcher":"Edit|Write|MultiEdit"`, `\"$FOURX_BIN\" guard-tool`} {
		if !strings.Contains(content, want) {
			t.Errorf("settings should contain %q, got: %s", want, content)
		}
	}
}

// AC-8：所有 claude role（不論 ExtraEnv 是否為空）都注入 --settings，內容含 Bash 與
// Edit|Write|MultiEdit 兩個 matcher，command 皆指向 "$FOURX_BIN" guard-tool；非 claude runner 不注入。
func TestBuildArgs_GuardToolSettings(t *testing.T) {
	baseArgs := []string{"-p", "{prompt}"}

	t.Run("claude with ExtraEnv (reviewer) injects settings", func(t *testing.T) {
		r := &SubprocessRunner{
			Name:     "claude",
			Config:   protocol.RunnerConfig{Command: "claude", Args: baseArgs},
			ExtraEnv: []string{"FOURX_ROLE=reviewer", "FOURX_REVIEW_PACKAGE=/tmp/rp.md"},
		}
		args, cleanup, err := r.buildArgs("hello")
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("buildArgs: %v", err)
		}
		assertGuardToolSettings(t, args)
		path := argsSettingsPath(args)
		// cleanup 後 temp 檔應被移除。
		cleanup()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("settings temp file should be removed by cleanup, stat err = %v", err)
		}
	})

	t.Run("claude without ExtraEnv (coder) also injects settings", func(t *testing.T) {
		r := &SubprocessRunner{
			Name:   "claude",
			Config: protocol.RunnerConfig{Command: "claude", Args: baseArgs},
		}
		args, cleanup, err := r.buildArgs("hello")
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("buildArgs: %v", err)
		}
		assertGuardToolSettings(t, args)
	})

	t.Run("non-claude runner with ExtraEnv does not inject settings", func(t *testing.T) {
		r := &SubprocessRunner{
			Name:     "gemini",
			Config:   protocol.RunnerConfig{Command: "gemini", Args: baseArgs},
			ExtraEnv: []string{"FOURX_ROLE=reviewer", "FOURX_REVIEW_PACKAGE=/tmp/rp.md"},
		}
		args, cleanup, err := r.buildArgs("hello")
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("buildArgs: %v", err)
		}
		if p := argsSettingsPath(args); p != "" {
			t.Errorf("non-claude runner should not inject --settings, got %v", args)
		}
	})
}
