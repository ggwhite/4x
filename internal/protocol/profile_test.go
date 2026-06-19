package protocol

import (
	"encoding/json"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func intPtr(i int) *int { return &i }

func TestResolveProfile_NoProfilesSection(t *testing.T) {
	cfg := Config{} // 無 profiles 區段
	cases := []*int{nil, intPtr(0), intPtr(1), intPtr(2), intPtr(3), intPtr(5)}
	for _, prio := range cases {
		name, pc, err := ResolveProfile(cfg, feature.Feature{Priority: prio}, "")
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
		name, _, err := ResolveProfile(cfg, feature.Feature{Priority: tt.prio}, "")
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
	name, pc, err := ResolveProfile(cfg, feature.Feature{Priority: intPtr(0)}, "quick")
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
	_, _, err := ResolveProfile(cfg, feature.Feature{}, "nonexistent")
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestResolveProfile_OverrideFallsBackToDefaults(t *testing.T) {
	// cfg.Profiles 不含 normal，但 DefaultProfiles 有 → override "normal" 仍命中。
	cfg := Config{Profiles: map[string]ProfileConfig{
		"custom": {Roles: []string{"coder"}},
	}}
	name, pc, err := ResolveProfile(cfg, feature.Feature{}, "normal")
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
	_, _, err := ResolveProfile(cfg, feature.Feature{}, "broken")
	if err == nil {
		t.Error("expected error when profile is missing coder")
	}
}

func TestResolveProfile_CustomProfileTakesPrecedenceOverDefault(t *testing.T) {
	// cfg.Profiles 覆寫 full → 取 cfg 版本而非 DefaultProfiles。
	cfg := Config{Profiles: map[string]ProfileConfig{
		"full": {Roles: []string{"coder", "reviewer"}},
	}}
	name, pc, err := ResolveProfile(cfg, feature.Feature{Priority: intPtr(0)}, "")
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

func TestResolveProfile_FeatureProfileHitsDefaults(t *testing.T) {
	// feature YAML profile 命中 DefaultProfiles（cfg 無對應自訂 profile）。
	cfg := Config{Profiles: DefaultProfiles()}
	name, pc, err := ResolveProfile(cfg, feature.Feature{Priority: intPtr(0), Profile: "quick"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "quick" {
		t.Errorf("name = %q, want quick (feature profile over priority auto-select)", name)
	}
	if pc.EnablesPhase(PhaseTesting) {
		t.Error("quick should not enable testing")
	}
}

func TestResolveProfile_FeatureProfileHitsCustom(t *testing.T) {
	// feature YAML profile 命中 cfg.Profiles 自訂 profile。
	cfg := Config{Profiles: map[string]ProfileConfig{
		"custom": {Roles: []string{"coder", "reviewer"}},
	}}
	name, pc, err := ResolveProfile(cfg, feature.Feature{Profile: "custom"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "custom" {
		t.Errorf("name = %q, want custom", name)
	}
	if pc.EnablesRole(RoleTester) {
		t.Error("custom profile should not enable tester")
	}
}

func TestResolveProfile_FeatureProfileUnknownIsError(t *testing.T) {
	// feature YAML profile 不存在於 cfg.Profiles 也不在 DefaultProfiles → 報錯（落實 rule）。
	cfg := Config{Profiles: DefaultProfiles()}
	_, _, err := ResolveProfile(cfg, feature.Feature{ID: "F999-x", Profile: "nonexistent"}, "")
	if err == nil {
		t.Error("expected error for unknown feature profile")
	}
}

func TestResolveProfile_OverrideWinsOverFeatureProfile(t *testing.T) {
	// override（--profile）優先於 feature YAML profile。
	cfg := Config{Profiles: DefaultProfiles()}
	name, _, err := ResolveProfile(cfg, feature.Feature{Profile: "quick"}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if name != "normal" {
		t.Errorf("override should win over feature profile, got %q", name)
	}
}

func TestResolveProfile_EmptyFeatureProfileFallsBackUnchanged(t *testing.T) {
	// feature profile 為空時行為與現況一致：default_profile 與 auto-select 相對順序不變。
	// (a) default_profile 仍優先於 auto-select。
	cfg := Config{Profiles: DefaultProfiles(), DefaultProfile: "quick"}
	name, _, err := ResolveProfile(cfg, feature.Feature{Priority: intPtr(0)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "quick" {
		t.Errorf("empty feature profile should still honor default_profile, got %q", name)
	}
	// (b) 無 default_profile 時退回 priority auto-select。
	cfg2 := Config{Profiles: DefaultProfiles()}
	name2, _, err := ResolveProfile(cfg2, feature.Feature{Priority: intPtr(2)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != "normal" {
		t.Errorf("empty feature profile should fall back to auto-select, got %q", name2)
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

func TestDefaultProfiles_DesignReviewPhase(t *testing.T) {
	full := DefaultProfiles()["full"]
	want := []Phase{
		PhaseDesigning,
		PhaseDesignReviewing,
		PhaseCoding,
		PhaseReviewing,
		PhaseDeepReviewing,
		PhaseTesting,
		PhaseAccepting,
	}
	if len(full.Phases) != len(want) {
		t.Fatalf("full phases = %v, want %v", full.Phases, want)
	}
	for i, phase := range want {
		if got := Phase(full.Phases[i].Phase); got != phase {
			t.Fatalf("full phase[%d] = %s, want %s", i, got, phase)
		}
	}
	if !full.EnablesRole(RoleDesignReviewer) {
		t.Fatal("full profile should enable design-reviewer")
	}
	if DefaultProfiles()["normal"].EnablesPhase(PhaseDesignReviewing) {
		t.Fatal("normal profile should not enable design-reviewing")
	}
	if DefaultProfiles()["quick"].EnablesPhase(PhaseDesignReviewing) {
		t.Fatal("quick profile should not enable design-reviewing")
	}
}

func TestProfileConfig_Normalize_RolesToPhases(t *testing.T) {
	// 舊格式 Roles[]string + CoderModel 應 normalize 成等價的 Phases。
	pc := ProfileConfig{Roles: []string{"designer", "design-reviewer", "coder", "reviewer"}, CoderModel: "opus"}
	pc.normalize()

	want := map[Phase]string{
		PhaseDesigning:       "",
		PhaseDesignReviewing: "",
		PhaseCoding:          "opus",
		PhaseReviewing:       "",
	}
	if len(pc.Phases) != len(want) {
		t.Fatalf("got %d phases, want %d: %+v", len(pc.Phases), len(want), pc.Phases)
	}
	for _, ps := range pc.Phases {
		wantModel, ok := want[Phase(ps.Phase)]
		if !ok {
			t.Errorf("unexpected phase %q", ps.Phase)
			continue
		}
		if ps.Model != wantModel {
			t.Errorf("phase %q model = %q, want %q", ps.Phase, ps.Model, wantModel)
		}
	}
}

func TestProfileConfig_Normalize_Idempotent(t *testing.T) {
	// 已是新格式（Phases 非空）時不應再被 Roles 改寫。
	pc := ProfileConfig{
		Phases: []PhaseSpec{{Phase: string(PhaseCoding), Runner: "codex"}},
		Roles:  []string{"designer", "coder", "reviewer", "tester", "deep-reviewer", "acceptor"},
	}
	pc.normalize()
	if len(pc.Phases) != 1 || pc.Phases[0].Runner != "codex" {
		t.Errorf("normalize mutated explicit Phases: %+v", pc.Phases)
	}
}

func TestProfileConfig_UnmarshalJSON_BackwardCompat(t *testing.T) {
	// 舊格式 JSON 載入後應自動轉成 Phases，coder_model 落到 coding phase。
	var pc ProfileConfig
	if err := json.Unmarshal([]byte(`{"roles":["coder","reviewer"],"coder_model":"opus"}`), &pc); err != nil {
		t.Fatal(err)
	}
	if !pc.EnablesPhase(PhaseCoding) || !pc.EnablesPhase(PhaseReviewing) {
		t.Errorf("expected coding+reviewing enabled, got %+v", pc.Phases)
	}
	spec, ok := pc.phaseSpec(PhaseCoding)
	if !ok || spec.Model != "opus" {
		t.Errorf("coding phase model = %q (ok=%v), want opus", spec.Model, ok)
	}
}

func TestProfileConfig_EnablesPhase(t *testing.T) {
	pc := DefaultProfiles()["quick"]
	if !pc.EnablesPhase(PhaseCoding) || !pc.EnablesPhase(PhaseReviewing) {
		t.Error("quick should enable coding + reviewing phases")
	}
	if pc.EnablesPhase(PhaseTesting) || pc.EnablesPhase(PhaseAccepting) {
		t.Error("quick should not enable testing/accepting phases")
	}
}

func TestResolveProfile_DefaultProfile(t *testing.T) {
	cfg := Config{Profiles: DefaultProfiles(), DefaultProfile: "quick"}
	// 即便 priority=0（平常會 auto-select full），default_profile 應優先。
	name, pc, err := ResolveProfile(cfg, feature.Feature{Priority: intPtr(0)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "quick" {
		t.Errorf("name = %q, want quick (default_profile)", name)
	}
	if pc.EnablesPhase(PhaseTesting) {
		t.Error("quick should not enable testing")
	}
	// 明確 --profile 仍應覆寫 default_profile。
	name, _, err = ResolveProfile(cfg, feature.Feature{Priority: intPtr(0)}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if name != "normal" {
		t.Errorf("override should win over default_profile, got %q", name)
	}
}

func TestResolveProfile_DefaultProfileUnknownFallsBackToFull(t *testing.T) {
	cfg := Config{Profiles: DefaultProfiles(), DefaultProfile: "nonexistent"}
	name, _, err := ResolveProfile(cfg, feature.Feature{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "full" {
		t.Errorf("name = %q, want full (unknown default_profile fallback)", name)
	}
}
