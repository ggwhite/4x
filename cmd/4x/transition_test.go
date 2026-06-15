package main

import (
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveHooks_PreAndPost(t *testing.T) {
	cfg := protocol.Config{
		Hooks: map[string][]feat.HookEntry{
			"pre_coding":  {{Run: "setup", OnFail: "block"}},
			"post_coding": {{Run: "cleanup"}},
		},
	}
	feature := feat.Feature{}
	got := resolveHooks(cfg, feature, protocol.PhaseCoding)
	if pre := got["pre"]; len(pre) != 1 || pre[0].Run != "setup" {
		t.Errorf("expected pre hook 'setup', got %+v", pre)
	}
	if post := got["post"]; len(post) != 1 || post[0].Run != "cleanup" {
		t.Errorf("expected post hook 'cleanup', got %+v", post)
	}
}

func TestResolveHooks_FeatureOverridesGlobal(t *testing.T) {
	cfg := protocol.Config{
		Hooks: map[string][]feat.HookEntry{
			"pre_coding": {{Run: "global-setup"}},
		},
	}
	feature := feat.Feature{
		Hooks: map[string][]feat.HookEntry{
			"pre_coding": {{Run: "feature-setup", OnFail: "warn"}},
		},
	}
	got := resolveHooks(cfg, feature, protocol.PhaseCoding)
	if pre := got["pre"]; len(pre) != 1 || pre[0].Run != "feature-setup" {
		t.Errorf("feature hook should override global, got %+v", pre)
	}
}

func TestResolveHooks_NoHooks(t *testing.T) {
	cfg := protocol.Config{}
	feature := feat.Feature{}
	got := resolveHooks(cfg, feature, protocol.PhaseCoding)
	if got != nil {
		t.Errorf("expected nil when no hooks, got %+v", got)
	}
}

func TestResolveHooks_OtherPhaseNotReturned(t *testing.T) {
	cfg := protocol.Config{
		Hooks: map[string][]feat.HookEntry{
			"pre_testing": {{Run: "test-setup"}},
		},
	}
	feature := feat.Feature{}
	got := resolveHooks(cfg, feature, protocol.PhaseCoding)
	if len(got) != 0 {
		t.Errorf("should not return hooks for other phase, got %+v", got)
	}
}
