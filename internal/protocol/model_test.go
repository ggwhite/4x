package protocol

import "testing"

func TestResolveModel_RoleTier(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus", "gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro")
	}
}

func TestResolveModel_FallbackRunnerDefault(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Model: "opus"},
		},
		Roles: map[string]RoleConfig{
			"coder": {},
		},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}

func TestResolveModel_FallbackSonnet(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"sonnet": {"claude": "sonnet"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want %q", got, "sonnet")
	}
}

func TestResolveModel_RunnerTiersOverride(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-2.5-pro-preview"}},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro-preview" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro-preview")
	}
}

func TestResolveModel_NotFound(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	_, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err == nil {
		t.Error("expected error when tier mapping not found")
	}
}

func TestResolveDeepModel_Found(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"reviewer": {Model: "sonnet", DeepModel: "opus"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}

func TestResolveDeepModel_NoDeepModel(t *testing.T) {
	cfg := Config{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestResolveMaxFixRounds_Default(t *testing.T) {
	cfg := Config{}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != defaultMaxFixRounds {
		t.Errorf("got %d, want default %d", got, defaultMaxFixRounds)
	}
}

func TestResolveMaxFixRounds_Configured(t *testing.T) {
	cfg := Config{
		Roles: map[string]RoleConfig{
			"deep-reviewer": {MaxFixRounds: 5},
		},
	}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestResolveMaxFixRounds_NonPositiveFallsBackToDefault(t *testing.T) {
	cfg := Config{
		Roles: map[string]RoleConfig{
			"deep-reviewer": {MaxFixRounds: 0},
		},
	}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != defaultMaxFixRounds {
		t.Errorf("got %d, want default %d", got, defaultMaxFixRounds)
	}
}
