package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestConfig_RunnerEnv 驗證 runner_env.{denylist,allowlist} 與 runners.claude.env_allowlist
// 能解析進 Config/RunnerConfig（AC-6）。
func TestConfig_RunnerEnv(t *testing.T) {
	raw := `{
		"project": {"name": "x"},
		"runner_env": {
			"denylist": ["CUSTOM_*"],
			"allowlist": ["AWS_*"]
		},
		"runners": {
			"claude": {
				"command": "claude",
				"args": ["-p", "{prompt}"],
				"env_allowlist": ["MY_EXTRA_*"]
			}
		}
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(cfg.RunnerEnv.Denylist, []string{"CUSTOM_*"}) {
		t.Errorf("RunnerEnv.Denylist = %v", cfg.RunnerEnv.Denylist)
	}
	if !reflect.DeepEqual(cfg.RunnerEnv.Allowlist, []string{"AWS_*"}) {
		t.Errorf("RunnerEnv.Allowlist = %v", cfg.RunnerEnv.Allowlist)
	}
	if !reflect.DeepEqual(cfg.Runners["claude"].EnvAllowlist, []string{"MY_EXTRA_*"}) {
		t.Errorf("claude.EnvAllowlist = %v", cfg.Runners["claude"].EnvAllowlist)
	}
}

// TestMergeRunner_EnvAllowlist 驗證 mergeRunner 讓 project 的 EnvAllowlist 覆寫 user 的（AC-6）。
func TestMergeRunner_EnvAllowlist(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", EnvAllowlist: []string{"USER_*"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {EnvAllowlist: []string{"PROJ_*"}},
		},
	}
	got := MergeConfig(user, proj)
	if !reflect.DeepEqual(got.Runners["claude"].EnvAllowlist, []string{"PROJ_*"}) {
		t.Errorf("EnvAllowlist = %v, want [PROJ_*] (project overrides user)", got.Runners["claude"].EnvAllowlist)
	}
}

// TestMergeRunner_EnvAllowlist_FallbackToUser 驗證 project 未設 EnvAllowlist 時沿用 user 的。
func TestMergeRunner_EnvAllowlist_FallbackToUser(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", EnvAllowlist: []string{"USER_*"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Model: "opus"},
		},
	}
	got := MergeConfig(user, proj)
	if !reflect.DeepEqual(got.Runners["claude"].EnvAllowlist, []string{"USER_*"}) {
		t.Errorf("EnvAllowlist = %v, want [USER_*] (inherited from user)", got.Runners["claude"].EnvAllowlist)
	}
}
