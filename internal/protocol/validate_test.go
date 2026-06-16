package protocol

import (
	"strings"
	"testing"
)

func TestValidateConfig_OK(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test-project"},
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"coder": {Model: "opus"},
		},
		Isolation: "worktree",
		Commit:    "per-round",
	}
	if err := ValidateConfig(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_Minimal(t *testing.T) {
	c := Config{Project: ProjectConfig{Name: "minimal"}}
	if err := ValidateConfig(c); err != nil {
		t.Fatalf("minimal config should be valid: %v", err)
	}
}

func TestValidateConfig_MissingProjectName(t *testing.T) {
	c := Config{}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for missing project.name")
	}
	if !strings.Contains(err.Error(), "project.name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_InvalidIsolation(t *testing.T) {
	c := Config{
		Project:   ProjectConfig{Name: "test"},
		Isolation: "docker",
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for invalid isolation")
	}
	if !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("error should mention isolation: %v", err)
	}
}

func TestValidateConfig_InvalidCommit(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Commit:  "always",
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for invalid commit strategy")
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Fatalf("error should mention commit: %v", err)
	}
}

func TestValidateConfig_DefaultRunnerNotInRunners(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Default: "gemini",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for default_runner not in runners")
	}
	if !strings.Contains(err.Error(), "default_runner") {
		t.Fatalf("error should mention default_runner: %v", err)
	}
}

func TestValidateConfig_RunnerMissingCommand(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Runners: map[string]RunnerConfig{
			"bad": {},
		},
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for runner missing command")
	}
	if !strings.Contains(err.Error(), "runners.bad.command") {
		t.Fatalf("error should mention runner name: %v", err)
	}
}

func TestValidateConfig_InvalidRoleName(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Roles: map[string]RoleConfig{
			"hacker": {Model: "gpt-5"},
		},
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for invalid role name")
	}
	if !strings.Contains(err.Error(), "roles.hacker") {
		t.Fatalf("error should mention role name: %v", err)
	}
}

func TestValidateConfig_ProfileMissingCoder(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Profiles: map[string]ProfileConfig{
			"fast": {Roles: []string{"reviewer", "tester"}},
		},
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for profile missing coder")
	}
	if !strings.Contains(err.Error(), "must include \"coder\"") {
		t.Fatalf("error should mention coder requirement: %v", err)
	}
}

func TestValidateConfig_ProfileUnknownRole(t *testing.T) {
	c := Config{
		Project: ProjectConfig{Name: "test"},
		Profiles: map[string]ProfileConfig{
			"bad": {Roles: []string{"coder", "ninja"}},
		},
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error for unknown role in profile")
	}
	if !strings.Contains(err.Error(), "ninja") {
		t.Fatalf("error should mention unknown role: %v", err)
	}
}

func TestValidateConfig_MultipleErrors(t *testing.T) {
	c := Config{
		Isolation: "invalid",
		Commit:    "invalid",
	}
	err := ValidateConfig(c)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "project.name") || !strings.Contains(msg, "isolation") || !strings.Contains(msg, "commit") {
		t.Fatalf("should aggregate errors: %v", err)
	}
}

func TestValidateState_OK(t *testing.T) {
	s := State{
		FeatureID: "f001-test",
		Phase:     PhaseCoding,
		Runner:    "claude",
	}
	if err := ValidateState(s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateState_InvalidPhase(t *testing.T) {
	s := State{
		FeatureID: "f001-test",
		Phase:     "hacking",
		Runner:    "claude",
	}
	err := ValidateState(s)
	if err == nil {
		t.Fatal("expected error for invalid phase")
	}
	if !strings.Contains(err.Error(), "hacking") {
		t.Fatalf("error should mention invalid phase: %v", err)
	}
}

func TestValidateState_MissingFields(t *testing.T) {
	s := State{}
	err := ValidateState(s)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "featureId") || !strings.Contains(msg, "runner") {
		t.Fatalf("should mention missing fields: %v", err)
	}
}
