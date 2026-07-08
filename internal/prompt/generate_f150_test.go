package prompt

import (
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

const profileMarker = "Active profile:"

func renderRole(t *testing.T, ctx *Context, role protocol.Role) string {
	t.Helper()
	p, err := Generate(ctx, role, 1, 0, "")
	if err != nil {
		t.Fatalf("Generate(%s): %v", role, err)
	}
	return p
}

// AC-6：6 個執行角色渲染含 profile-aware 段落標記；designer / design-reviewer 不含。
func TestGenerate_ProfileSectionInjection(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F150-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: featureID, Name: "Test"}, Cfg: protocol.Config{}}

	inject := []protocol.Role{
		protocol.RoleCoder, protocol.RoleReviewer, protocol.RoleTester,
		protocol.RoleDeepReviewer, protocol.RoleFixer, protocol.RoleAcceptor,
	}
	for _, role := range inject {
		if got := renderRole(t, ctx, role); !strings.Contains(got, profileMarker) {
			t.Errorf("role %s output missing %q marker", role, profileMarker)
		}
	}

	for _, role := range []protocol.Role{protocol.RoleDesigner, protocol.RoleDesignReviewer} {
		if got := renderRole(t, ctx, role); strings.Contains(got, profileMarker) {
			t.Errorf("role %s output should NOT contain %q marker", role, profileMarker)
		}
	}
}

// AC-7：Context.Profile 覆寫確實流到 Generate——切換 lean/空兩個值，acceptor 段落 AbsentUpstream 隨之改變。
func TestGenerate_ProfileOverrideConsumed(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F150-override"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	cfg := protocol.Config{Profiles: map[string]protocol.ProfileConfig{
		"lean": profileWith(protocol.PhaseCoding, protocol.PhaseTesting, protocol.PhaseAccepting),
	}}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	leanCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Profile: "lean"}
	leanOut := renderRole(t, leanCtx, protocol.RoleAcceptor)
	if !strings.Contains(leanOut, protocol.ReviewReport) || !strings.Contains(leanOut, "absence is EXPECTED") {
		t.Errorf("lean acceptor output should list absent %s\n---\n%s", protocol.ReviewReport, leanOut)
	}

	// Profile="" → fallback（cfg.Profiles 非空、priority nil → full）→ AbsentUpstream 空。
	fullCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Profile: ""}
	fullOut := renderRole(t, fullCtx, protocol.RoleAcceptor)
	if strings.Contains(fullOut, "absence is EXPECTED") {
		t.Errorf("fallback (full) acceptor output should NOT contain absent-upstream block\n---\n%s", fullOut)
	}
	if leanOut == fullOut {
		t.Errorf("acceptor output did not change when Context.Profile switched (override not consumed)")
	}
}

// AC-10：acceptor 渲染含「不可據此判 FAIL / 拒絕驗收」引導句。
func TestGenerate_AcceptorGuidance(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F150-guidance"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: featureID, Name: "Test"}, Cfg: protocol.Config{}}
	got := renderRole(t, ctx, protocol.RoleAcceptor)
	if !strings.Contains(got, "不可據此判 FAIL 或拒絕驗收") {
		t.Errorf("acceptor output missing profile-absent guidance sentence\n---\n%s", got)
	}
}
