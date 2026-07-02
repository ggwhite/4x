package main

import (
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestRetry_AfterDeferRunCleanupFallback 驗證 AC-8（Issue 4 Part A 銜接）：
// DeferRunCleanup 在 Active==true 兜底時補上 needs-attention 後，`4x retry`
// 的核心 retryTransition 必須能從該 state 成功接手，不再被
// "retry only works from needs-attention or blocked" 拒絕。
func TestRetry_AfterDeferRunCleanupFallback(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-defer")
	s := protocol.State{
		FeatureID: "feat-retry-defer",
		Phase:     protocol.PhaseFixing,
		Round:     2,
		Active:    true,
		Runner:    "mock",
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState("feat-retry-defer", s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	// 模擬 process 中斷：DeferRunCleanup 的兜底邏輯將 state 標為 needs-attention。
	orchestrator.DeferRunCleanup(ws, "feat-retry-defer")

	newState, from, err := retryTransition(ws, "feat-retry-defer", protocol.PhaseAccepting)
	if err != nil {
		t.Fatalf("retryTransition should succeed after DeferRunCleanup fallback, got error: %v", err)
	}
	if from != protocol.PhaseNeedsAttention {
		t.Errorf("from = %s, want needs-attention", from)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting", newState.Phase)
	}
	if !newState.Active {
		t.Error("Active should be true after retry")
	}
}
