package protocol

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

// baseOverrideConfig 提供兩個 runner（claude/codex）與各 tier 的 model 對應，供覆寫優先序測試共用。
func baseOverrideConfig() Config {
	return Config{
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus", "sonnet": "sonnet"}},
			"codex":  {Command: "codex", Tiers: map[string]string{"opus": "gpt-5.5", "sonnet": "gpt-5.5"}},
		},
		Roles: map[string]RoleConfig{"coder": {Model: "sonnet"}},
	}
}

func TestResolvePhaseRunner_Hierarchy(t *testing.T) {
	cfg := baseOverrideConfig()
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding), Runner: "codex"}}}

	tests := []struct {
		name    string
		feature feature.Feature
		manual  string
		want    string
	}{
		{"default_runner only", feature.Feature{}, "", "codex"}, // profile coding.runner=codex 勝過 default
		{"manual wins all", feature.Feature{
			PhaseOverrides: map[string]feature.PhaseOverride{"coding": {Runner: "claude"}},
		}, "claude", "claude"},
		{"feature override beats profile", feature.Feature{
			PhaseOverrides: map[string]feature.PhaseOverride{"coding": {Runner: "claude"}},
		}, "", "claude"},
	}
	for _, tt := range tests {
		got, err := ResolvePhaseRunner(cfg, tt.feature, pc, PhaseCoding, tt.manual)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestResolvePhaseRunner_FallsBackToDefault(t *testing.T) {
	cfg := baseOverrideConfig()
	// profile 對 reviewing 沒有 runner 覆寫 → 退 default_runner。
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseReviewing)}}}
	got, err := ResolvePhaseRunner(cfg, feature.Feature{}, pc, PhaseReviewing, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "claude" {
		t.Errorf("got %q, want claude (default_runner)", got)
	}
}

func TestResolvePhaseRunner_UnknownRunnerErrors(t *testing.T) {
	cfg := baseOverrideConfig()
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding), Runner: "ghost"}}}
	if _, err := ResolvePhaseRunner(cfg, feature.Feature{}, pc, PhaseCoding, ""); err == nil {
		t.Error("expected error for unknown runner")
	}
}

func TestResolvePhaseModel_Hierarchy(t *testing.T) {
	cfg := baseOverrideConfig()
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding), Model: "opus"}}}

	tests := []struct {
		name    string
		feature feature.Feature
		manual  string
		want    string
	}{
		// profile coding.model=opus 勝過 roles.coder.model=sonnet。
		{"profile beats roles", feature.Feature{}, "", "opus"},
		// feature override 勝過 profile。
		{"feature override beats profile", feature.Feature{
			PhaseOverrides: map[string]feature.PhaseOverride{"coding": {Model: "sonnet"}},
		}, "", "sonnet"},
		// manual 勝過全部。
		{"manual wins all", feature.Feature{
			PhaseOverrides: map[string]feature.PhaseOverride{"coding": {Model: "sonnet"}},
		}, "opus", "opus"},
	}
	for _, tt := range tests {
		got, err := ResolvePhaseModel(cfg, tt.feature, pc, PhaseCoding, RoleCoder, "claude", tt.manual)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestResolvePhaseModel_FallsBackToRolesModel(t *testing.T) {
	cfg := baseOverrideConfig()
	// profile/feature/manual 皆無 → fallback ResolveModel（roles.coder.model=sonnet）。
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding)}}}
	got, err := ResolvePhaseModel(cfg, feature.Feature{}, pc, PhaseCoding, RoleCoder, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want sonnet (roles fallback)", got)
	}
}

// TestResolvePhaseRunner_AmendingUsesCoderOverride 驗證 amend 輪的 role 覆寫解析（post-merge
// 缺陷 #1）：PhaseAmending 的 role 是 coder（同 state.PhaseToRole），roles.coder.runner 覆寫
// 必須對 amending phase 生效，不能因 protocol 自己的 phaseToRoleMap 漏列 amending 而 fallback
// 到 cfg.Default。
func TestResolvePhaseRunner_AmendingUsesCoderOverride(t *testing.T) {
	cfg := baseOverrideConfig()
	cfg.Roles["coder"] = RoleConfig{Runner: "codex"}

	got, err := ResolvePhaseRunner(cfg, feature.Feature{}, ProfileConfig{}, PhaseAmending, "")
	if err != nil {
		t.Fatalf("ResolvePhaseRunner(PhaseAmending): %v", err)
	}
	if got != "codex" {
		t.Errorf("amending phase runner: got %q, want %q (roles.coder.runner override)", got, "codex")
	}
}

// TestPhaseRole_Amending 直接驗證 PhaseRole(PhaseAmending) 回傳 RoleCoder，
// 而不是空字串（空字串會讓 ResolvePhaseRunner 的 roles 層解析整組失效）。
func TestPhaseRole_Amending(t *testing.T) {
	if got := PhaseRole(PhaseAmending); got != RoleCoder {
		t.Errorf("PhaseRole(PhaseAmending): got %q, want %q", got, RoleCoder)
	}
}

func TestResolvePhaseModel_ResolvesTierPerRunner(t *testing.T) {
	cfg := baseOverrideConfig()
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding), Model: "opus"}}}
	// 同一 opus tier 對 codex runner 應解析成 gpt-5.5。
	got, err := ResolvePhaseModel(cfg, feature.Feature{}, pc, PhaseCoding, RoleCoder, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt-5.5" {
		t.Errorf("got %q, want gpt-5.5", got)
	}
}

func TestPhaseOverrideLegacyAlias(t *testing.T) {
	// canonical claude runner（只含 strong/fast key）下，既有 --phase-override :opus/:sonnet
	// 呼叫方式解析不變，證明舊呼叫慣例未被破壞。
	cfg := Config{
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{TierStrong: "opus", TierFast: "sonnet"}},
		},
		Roles: map[string]RoleConfig{"coder": {Model: TierFast}},
	}
	pc := ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding)}}}
	for manual, want := range map[string]string{"opus": "opus", "sonnet": "sonnet"} {
		got, err := ResolvePhaseModel(cfg, feature.Feature{}, pc, PhaseCoding, RoleCoder, "claude", manual)
		if err != nil {
			t.Fatalf("manual=%q: %v", manual, err)
		}
		if got != want {
			t.Errorf("manual=%q: got %q, want %q", manual, got, want)
		}
	}
}
