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

// AC-10：claude runner 且 ExtraEnv 有值時注入 --settings（含 guard-tool/PreToolUse）；
// ExtraEnv 為 nil 或非 claude runner 時不注入。
func TestBuildArgs_GuardToolSettings(t *testing.T) {
	baseArgs := []string{"-p", "{prompt}"}

	t.Run("claude with ExtraEnv injects settings", func(t *testing.T) {
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
		path := argsSettingsPath(args)
		if path == "" {
			t.Fatalf("expected --settings in args, got %v", args)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read settings temp file: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "guard-tool") || !strings.Contains(content, "PreToolUse") {
			t.Errorf("settings should contain guard-tool + PreToolUse, got: %s", content)
		}
		// cleanup 後 temp 檔應被移除。
		cleanup()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("settings temp file should be removed by cleanup, stat err = %v", err)
		}
	})

	t.Run("claude without ExtraEnv does not inject settings", func(t *testing.T) {
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
		if p := argsSettingsPath(args); p != "" {
			t.Errorf("nil ExtraEnv should not inject --settings, got %v", args)
		}
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
