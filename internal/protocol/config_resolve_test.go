package protocol

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func TestSupportedRunnersCanonicalTiers(t *testing.T) {
	// 每個含 tiers 的 runner，其 key 僅為 canonical strong/fast，value 與變更前一致；cursor 無 tiers。
	wantTiers := map[string]map[string]string{
		"claude":   {TierStrong: "opus", TierFast: "sonnet"},
		"codex":    {TierStrong: "gpt-5.5", TierFast: "gpt-5.5"},
		"gemini":   {TierStrong: "gemini-2.5-flash", TierFast: "gemini-2.5-flash"},
		"agy":      {TierStrong: "claude-opus-4-6-thinking", TierFast: "claude-sonnet-4-6-thinking"},
		"copilot":  {TierStrong: "auto", TierFast: "auto"},
		"opencode": {TierStrong: "anthropic/claude-opus-4-6", TierFast: "anthropic/claude-sonnet-4-6"},
		"cursor":   nil,
	}
	for _, p := range SupportedRunners() {
		want, ok := wantTiers[p.Name]
		if !ok {
			t.Errorf("unexpected runner %q in registry", p.Name)
			continue
		}
		if len(want) == 0 {
			if len(p.Config.Tiers) != 0 {
				t.Errorf("%s: expected no tiers, got %v", p.Name, p.Config.Tiers)
			}
			continue
		}
		for _, k := range []string{"opus", "sonnet"} {
			if _, bad := p.Config.Tiers[k]; bad {
				t.Errorf("%s: tiers must not contain legacy key %q", p.Name, k)
			}
		}
		if len(p.Config.Tiers) != len(want) {
			t.Errorf("%s: tier count = %d, want %d (%v)", p.Name, len(p.Config.Tiers), len(want), p.Config.Tiers)
		}
		for k, v := range want {
			if got := p.Config.Tiers[k]; got != v {
				t.Errorf("%s: tiers[%q] = %q, want %q", p.Name, k, got, v)
			}
		}
	}
}

func TestDefaultRoleModels(t *testing.T) {
	// 有 tiers 的 runner → canonical tier 預設；無 tiers 或未知 runner → Model/DeepModel 留空。
	withTiers := []string{"claude", "codex"}
	for _, name := range withTiers {
		roles := DefaultRoleModels(name)
		assertRole(t, name, roles, "designer", TierStrong, "")
		assertRole(t, name, roles, "coder", TierFast, "")
		assertRole(t, name, roles, "reviewer", TierFast, TierStrong)
		assertRole(t, name, roles, "tester", TierFast, "")
		if roles["tester"].ScreenshotDir != feature.DefaultScreenshotDir {
			t.Errorf("%s: tester.ScreenshotDir = %q, want %q", name, roles["tester"].ScreenshotDir, feature.DefaultScreenshotDir)
		}
	}
	for _, name := range []string{"cursor", "bogus-runner"} {
		roles := DefaultRoleModels(name)
		for _, r := range []string{"designer", "coder", "reviewer", "tester"} {
			if roles[r].Model != "" || roles[r].DeepModel != "" {
				t.Errorf("%s: role %q must have empty Model/DeepModel, got %+v", name, r, roles[r])
			}
		}
		if roles["tester"].ScreenshotDir != feature.DefaultScreenshotDir {
			t.Errorf("%s: tester.ScreenshotDir = %q, want %q", name, roles["tester"].ScreenshotDir, feature.DefaultScreenshotDir)
		}
	}
}

func assertRole(t *testing.T, runner string, roles map[string]RoleConfig, role, wantModel, wantDeep string) {
	t.Helper()
	rc := roles[role]
	if rc.Model != wantModel {
		t.Errorf("%s: %s.Model = %q, want %q", runner, role, rc.Model, wantModel)
	}
	if rc.DeepModel != wantDeep {
		t.Errorf("%s: %s.DeepModel = %q, want %q", runner, role, rc.DeepModel, wantDeep)
	}
}
