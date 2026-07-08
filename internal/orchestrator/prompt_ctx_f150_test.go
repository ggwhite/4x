package orchestrator

import (
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// AC-8：promptCtx() 把 state.json 的 profile 帶入 Context.Profile；讀不到時為空且不 panic。
func TestPromptCtx_CarriesProfile(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-promptctx")
	s := protocol.State{
		FeatureID: "feat-promptctx",
		Phase:     protocol.PhaseCoding,
		Round:     1,
		Profile:   "lean",
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState("feat-promptctx", s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	r := NewRunner(Config{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: "feat-promptctx"}})
	ctx := r.promptCtx()
	if ctx.Profile != "lean" {
		t.Errorf("promptCtx().Profile = %q, want %q", ctx.Profile, "lean")
	}
}

// AC-8：無 state 檔時 promptCtx() 不 panic 且 Profile 為空。
func TestPromptCtx_NoStateNoPanic(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-nostate")
	// 不寫 state.json。
	r := NewRunner(Config{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: "feat-nostate"}})
	ctx := r.promptCtx()
	if ctx.Profile != "" {
		t.Errorf("promptCtx().Profile = %q, want empty when no state", ctx.Profile)
	}
}
