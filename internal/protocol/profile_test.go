package protocol

import "testing"

func intPtr(i int) *int { return &i }

func TestResolveProfile_NoProfilesSection(t *testing.T) {
	cfg := Config{} // 無 profiles 區段
	cases := []*int{nil, intPtr(0), intPtr(1), intPtr(2), intPtr(3), intPtr(5)}
	for _, prio := range cases {
		name, pc, err := ResolveProfile(cfg, Feature{Priority: prio}, "")
		if err != nil {
			t.Fatalf("priority %v: %v", prio, err)
		}
		if name != "full" {
			t.Errorf("priority %v: name = %q, want full", prio, name)
		}
		if !pc.EnablesRole(RoleDesigner) || !pc.EnablesRole(RoleAcceptor) {
			t.Errorf("priority %v: full profile should enable all roles", prio)
		}
	}
}

func TestResolveProfile_AutoSelectByPriority(t *testing.T) {
	cfg := Config{Profiles: DefaultProfiles()}
	tests := []struct {
		prio *int
		want string
	}{
		{nil, "full"},
		{intPtr(0), "full"},
		{intPtr(1), "full"},
		{intPtr(2), "normal"},
		{intPtr(3), "quick"},
		{intPtr(4), "quick"},
		{intPtr(5), "quick"},
	}
	for _, tt := range tests {
		name, _, err := ResolveProfile(cfg, Feature{Priority: tt.prio}, "")
		if err != nil {
			t.Fatalf("priority %v: %v", tt.prio, err)
		}
		if name != tt.want {
			t.Errorf("priority %v: name = %q, want %q", tt.prio, name, tt.want)
		}
	}
}

func TestResolveProfile_OverrideHit(t *testing.T) {
	cfg := Config{Profiles: DefaultProfiles()}
	name, pc, err := ResolveProfile(cfg, Feature{Priority: intPtr(0)}, "quick")
	if err != nil {
		t.Fatal(err)
	}
	if name != "quick" {
		t.Errorf("name = %q, want quick", name)
	}
	if pc.EnablesRole(RoleTester) {
		t.Error("quick profile should not enable tester")
	}
}

func TestResolveProfile_OverrideMiss(t *testing.T) {
	cfg := Config{Profiles: DefaultProfiles()}
	_, _, err := ResolveProfile(cfg, Feature{}, "nonexistent")
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestResolveProfile_OverrideFallsBackToDefaults(t *testing.T) {
	// cfg.Profiles 不含 normal，但 DefaultProfiles 有 → override "normal" 仍命中。
	cfg := Config{Profiles: map[string]ProfileConfig{
		"custom": {Roles: []string{"coder"}},
	}}
	name, pc, err := ResolveProfile(cfg, Feature{}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if name != "normal" {
		t.Errorf("name = %q, want normal", name)
	}
	if !pc.EnablesRole(RoleTester) {
		t.Error("default normal profile should enable tester")
	}
}

func TestResolveProfile_MissingCoderIsError(t *testing.T) {
	cfg := Config{Profiles: map[string]ProfileConfig{
		"broken": {Roles: []string{"reviewer", "tester"}},
	}}
	_, _, err := ResolveProfile(cfg, Feature{}, "broken")
	if err == nil {
		t.Error("expected error when profile is missing coder")
	}
}

func TestResolveProfile_CustomProfileTakesPrecedenceOverDefault(t *testing.T) {
	// cfg.Profiles 覆寫 full → 取 cfg 版本而非 DefaultProfiles。
	cfg := Config{Profiles: map[string]ProfileConfig{
		"full": {Roles: []string{"coder", "reviewer"}},
	}}
	name, pc, err := ResolveProfile(cfg, Feature{Priority: intPtr(0)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "full" {
		t.Errorf("name = %q, want full", name)
	}
	if pc.EnablesRole(RoleTester) {
		t.Error("custom full profile should not enable tester")
	}
}

func TestProfileConfig_EnablesRole_DeepReviewer(t *testing.T) {
	pc := DefaultProfiles()["full"]
	if !pc.EnablesRole(RoleDeepReviewer) {
		t.Error("full profile should enable deep-reviewer")
	}
	if DefaultProfiles()["normal"].EnablesRole(RoleDeepReviewer) {
		t.Error("normal profile should not enable deep-reviewer")
	}
}

func TestResolveProfileModel_CoderOverride(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus":   {"claude": "opus"},
			"sonnet": {"claude": "sonnet"},
		},
		Runners: map[string]RunnerConfig{"claude": {Command: "claude"}},
		Roles:   map[string]RoleConfig{"coder": {Model: "sonnet"}},
	}
	pc := ProfileConfig{Roles: []string{"coder"}, CoderModel: "opus"}
	got, err := ResolveProfileModel(cfg, "claude", RoleCoder, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want opus (coder_model override)", got)
	}
}

func TestResolveProfileModel_CoderNoOverride(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{"sonnet": {"claude": "sonnet"}},
		Runners:    map[string]RunnerConfig{"claude": {Command: "claude"}},
		Roles:      map[string]RoleConfig{"coder": {Model: "sonnet"}},
	}
	pc := ProfileConfig{Roles: []string{"coder"}}
	got, err := ResolveProfileModel(cfg, "claude", RoleCoder, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want sonnet", got)
	}
}

func TestResolveProfileModel_NonCoderUnaffected(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus":   {"claude": "opus"},
			"sonnet": {"claude": "sonnet"},
		},
		Runners: map[string]RunnerConfig{"claude": {Command: "claude"}},
		Roles:   map[string]RoleConfig{"reviewer": {Model: "sonnet"}},
	}
	// CoderModel 覆蓋只影響 coder，reviewer 不受影響。
	pc := ProfileConfig{Roles: []string{"coder", "reviewer"}, CoderModel: "opus"}
	got, err := ResolveProfileModel(cfg, "claude", RoleReviewer, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want sonnet (reviewer should ignore coder_model)", got)
	}
}
