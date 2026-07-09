package main

import (
	"os"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestF158_ConfigSetGetRoleRunner 驗證 user-level config CLI 能 set/get
// role.<name>.runner 欄位（F158 per-role runner 路由的 user-level 設定入口）。
func TestF158_ConfigSetGetRoleRunner(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// set role.deep-reviewer.runner codex
	setCmd := newConfigSetCmd()
	setCmd.SetArgs([]string{"role.deep-reviewer.runner", "codex"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("config set role.deep-reviewer.runner: %v", err)
	}

	// 直接讀回 user config 確認落地
	cfg, err := protocol.ReadUserConfig()
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if got := cfg.Roles["deep-reviewer"].Runner; got != "codex" {
		t.Errorf("Roles[deep-reviewer].Runner = %q, want codex", got)
	}

	// get 路徑（configValue）也要能取回同值
	val, err := configValue(cfg, "role.deep-reviewer.runner")
	if err != nil {
		t.Fatalf("configValue role.deep-reviewer.runner: %v", err)
	}
	if val != "codex" {
		t.Errorf("configValue = %q, want codex", val)
	}
}

// TestF158_ConfigGetRoleRunner_NotSet 驗證未設定 runner 時 get 回傳 "(not set)"，
// 不誤報 unknown field。
func TestF158_ConfigGetRoleRunner_NotSet(t *testing.T) {
	cfg := protocol.UserConfig{
		Roles: map[string]protocol.RoleConfig{
			"deep-reviewer": {Model: "opus"},
		},
	}
	val, err := configValue(cfg, "role.deep-reviewer.runner")
	if err != nil {
		t.Fatalf("configValue role.deep-reviewer.runner: %v", err)
	}
	if val != "(not set)" {
		t.Errorf("configValue = %q, want (not set)", val)
	}
}
