package protocol

import (
	"encoding/json"
	"testing"
)

func TestHookEntry_Unmarshal(t *testing.T) {
	raw := `{"run": "docker compose up -d", "on_fail": "warn"}`
	var h HookEntry
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	if h.Run != "docker compose up -d" {
		t.Errorf("Run = %q, want %q", h.Run, "docker compose up -d")
	}
	if h.OnFail != "warn" {
		t.Errorf("OnFail = %q, want %q", h.OnFail, "warn")
	}
}

func TestHookEntry_OnFail_DefaultBlock(t *testing.T) {
	raw := `{"run": "echo hello"}`
	var h HookEntry
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	if h.EffectiveOnFail() != "block" {
		t.Errorf("EffectiveOnFail() = %q, want %q", h.EffectiveOnFail(), "block")
	}
}

func TestHookEntry_OnFail_Explicit(t *testing.T) {
	h := HookEntry{Run: "echo hi", OnFail: "warn"}
	if h.EffectiveOnFail() != "warn" {
		t.Errorf("EffectiveOnFail() = %q, want warn", h.EffectiveOnFail())
	}
}

func TestMergeHooks_FeatureOverridesGlobal(t *testing.T) {
	global := map[string][]HookEntry{
		"pre_coding":   {{Run: "global-setup", OnFail: "block"}},
		"post_testing": {{Run: "global-cleanup"}},
	}
	feature := map[string][]HookEntry{
		"pre_coding": {{Run: "feature-setup", OnFail: "warn"}},
	}
	got := MergeHooks(global, feature)
	if len(got["pre_coding"]) != 1 || got["pre_coding"][0].Run != "feature-setup" {
		t.Errorf("pre_coding should be overridden by feature, got %+v", got["pre_coding"])
	}
	if len(got["post_testing"]) != 1 || got["post_testing"][0].Run != "global-cleanup" {
		t.Errorf("post_testing should be inherited from global, got %+v", got["post_testing"])
	}
}

func TestMergeHooks_BothNil(t *testing.T) {
	got := MergeHooks(nil, nil)
	if got != nil {
		t.Errorf("both nil should return nil, got %+v", got)
	}
}

func TestMergeHooks_GlobalNilFeatureOnly(t *testing.T) {
	feature := map[string][]HookEntry{
		"pre_coding": {{Run: "setup"}},
	}
	got := MergeHooks(nil, feature)
	if len(got["pre_coding"]) != 1 {
		t.Errorf("expected 1 entry, got %+v", got)
	}
}
