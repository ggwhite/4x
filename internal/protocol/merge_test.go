package protocol

import (
	"testing"
)

func TestMergeConfig_DefaultRunner_ProjectOverrides(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{Default: "codex", Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Default != "codex" {
		t.Errorf("Default = %q, want codex", got.Default)
	}
}

func TestMergeConfig_DefaultRunner_FallbackToUser(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Default != "claude" {
		t.Errorf("Default = %q, want claude", got.Default)
	}
}

func TestMergeConfig_Runners_FieldMerge(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {
				Command: "/usr/local/bin/claude",
				Args:    []string{"--skip", "-p", "{prompt}"},
				Tty:     BoolPtr(true),
			},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Model: "opus"},
		},
	}
	got := MergeConfig(user, proj)
	rc := got.Runners["claude"]
	if rc.Command != "/usr/local/bin/claude" {
		t.Errorf("Command = %q, want /usr/local/bin/claude", rc.Command)
	}
	if len(rc.Args) != 3 {
		t.Errorf("Args len = %d, want 3 (inherited from user)", len(rc.Args))
	}
	if !BoolVal(rc.Tty) {
		t.Error("Tty should be true (inherited from user)")
	}
	if rc.Model != "opus" {
		t.Errorf("Model = %q, want opus (from project)", rc.Model)
	}
}

func TestMergeConfig_Runners_ArgsFullReplace(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Args: []string{"--old", "-p", "{prompt}"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Args: []string{"--new", "{prompt}"}},
		},
	}
	got := MergeConfig(user, proj)
	if len(got.Runners["claude"].Args) != 2 || got.Runners["claude"].Args[0] != "--new" {
		t.Errorf("Args = %v, want [--new {prompt}]", got.Runners["claude"].Args)
	}
}

func TestMergeConfig_Runners_UserOnly(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini", Stdin: BoolPtr(true)},
		},
	}
	proj := Config{Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Runners["gemini"].Command != "gemini" {
		t.Errorf("user-only runner not preserved")
	}
	if !BoolVal(got.Runners["gemini"].Stdin) {
		t.Error("Stdin should be true")
	}
}

func TestMergeConfig_Runners_ProjectOnly(t *testing.T) {
	user := UserConfig{}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"codex": {Command: "codex"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Runners["codex"].Command != "codex" {
		t.Errorf("project-only runner not preserved")
	}
}

func TestMergeConfig_Runners_TiersMerge(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Tiers: map[string]string{"opus": "claude-opus-4-5"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Tiers: map[string]string{"sonnet": "claude-sonnet-4-5"}},
		},
	}
	got := MergeConfig(user, proj)
	tiers := got.Runners["claude"].Tiers
	if tiers["opus"] != "claude-opus-4-5" {
		t.Errorf("user tier not preserved: %v", tiers)
	}
	if tiers["sonnet"] != "claude-sonnet-4-5" {
		t.Errorf("project tier not merged: %v", tiers)
	}
}

func TestMergeConfig_Roles_FieldMerge(t *testing.T) {
	user := UserConfig{
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
			"coder":    {Model: "sonnet"},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Roles: map[string]RoleConfig{
			"coder": {Model: "haiku"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Roles["designer"].Model != "opus" {
		t.Errorf("designer.Model = %q, want opus (from user)", got.Roles["designer"].Model)
	}
	if got.Roles["coder"].Model != "haiku" {
		t.Errorf("coder.Model = %q, want haiku (project overrides)", got.Roles["coder"].Model)
	}
}

func TestMergeConfig_Roles_InstructionsMerge(t *testing.T) {
	user := UserConfig{
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet", Instructions: []string{"user rule"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Roles: map[string]RoleConfig{
			"coder": {Instructions: []string{"project rule"}},
		},
	}
	got := MergeConfig(user, proj)
	if len(got.Roles["coder"].Instructions) != 1 || got.Roles["coder"].Instructions[0] != "project rule" {
		t.Errorf("Instructions = %v, want [project rule]", got.Roles["coder"].Instructions)
	}
	if got.Roles["coder"].Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet (from user)", got.Roles["coder"].Model)
	}
}

func TestMergeConfig_ProjectOnlyFields_Preserved(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{
		Project:           ProjectConfig{Name: "my-proj", Language: "go"},
		Isolation:         "worktree",
		Commit:            "per-round",
		MaxConcurrentRuns: 3,
		HubRepos:          []string{"shared-lib"},
		Rules:             []string{"no LLM in CLI"},
	}
	got := MergeConfig(user, proj)
	if got.Project.Name != "my-proj" {
		t.Errorf("Project.Name = %q", got.Project.Name)
	}
	if got.Isolation != "worktree" {
		t.Errorf("Isolation = %q", got.Isolation)
	}
	if got.Commit != "per-round" {
		t.Errorf("Commit = %q", got.Commit)
	}
	if got.MaxConcurrentRuns != 3 {
		t.Errorf("MaxConcurrentRuns = %d", got.MaxConcurrentRuns)
	}
}

func TestMergeConfig_BoolOverride(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Tty: BoolPtr(true)},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Tty: BoolPtr(false)},
		},
	}
	got := MergeConfig(user, proj)
	if BoolVal(got.Runners["claude"].Tty) {
		t.Error("Tty should be false (project explicitly set false)")
	}
}

func TestMergeConfig_OutputFormat_UserInherited(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {OutputFormat: "stream-json"},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Model: "opus"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Runners["claude"].OutputFormat != "stream-json" {
		t.Errorf("OutputFormat = %q, want stream-json (inherited from user)", got.Runners["claude"].OutputFormat)
	}
}

func TestMergeConfig_OutputFormat_ProjectOverrides(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {OutputFormat: "stream-json"},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {OutputFormat: "text"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Runners["claude"].OutputFormat != "text" {
		t.Errorf("OutputFormat = %q, want text (project overrides)", got.Runners["claude"].OutputFormat)
	}
}
