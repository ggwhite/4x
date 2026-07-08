package main

import (
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestTransition_SetsManualPhase 驗證 `4x transition` 寫出的 state 帶有 ManualPhase=true，
// 供後續 4x run 的 RecoverState 尊重人為指定的 phase。
func TestTransition_SetsManualPhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-trans-manual")
	t.Chdir(ws.Root)

	cmd := newTransitionCmd()
	cmd.SetArgs([]string{"feat-trans-manual", "--to", string(protocol.PhaseDesigning)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("transition execute: %v", err)
	}

	got, err := ws.ReadState("feat-trans-manual")
	if err != nil {
		t.Fatalf("ReadState error: %v", err)
	}
	if got.Phase != protocol.PhaseDesigning {
		t.Errorf("Phase = %s, want designing", got.Phase)
	}
	if !got.ManualPhase {
		t.Error("ManualPhase should be true after manual transition")
	}
}

func TestResolveHooks_PreAndPost(t *testing.T) {
	cfg := protocol.Config{
		Hooks: map[string][]feat.HookEntry{
			"pre_coding":  {{Run: "setup", OnFail: "block"}},
			"post_coding": {{Run: "cleanup"}},
		},
	}
	feature := feat.Feature{}
	got := orchestrator.ResolveHooks(cfg, feature, protocol.PhaseCoding)
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
	got := orchestrator.ResolveHooks(cfg, feature, protocol.PhaseCoding)
	if pre := got["pre"]; len(pre) != 1 || pre[0].Run != "feature-setup" {
		t.Errorf("feature hook should override global, got %+v", pre)
	}
}

func TestResolveHooks_NoHooks(t *testing.T) {
	cfg := protocol.Config{}
	feature := feat.Feature{}
	got := orchestrator.ResolveHooks(cfg, feature, protocol.PhaseCoding)
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
	got := orchestrator.ResolveHooks(cfg, feature, protocol.PhaseCoding)
	if len(got) != 0 {
		t.Errorf("should not return hooks for other phase, got %+v", got)
	}
}
