package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/hook"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// ExecutePhaseHooks 執行指定 timing（"pre"/"post"）的 phase hooks，記錄事件，失敗時統一處理狀態
func ExecutePhaseHooks(ctx context.Context, ws *protocol.Workspace, featureID string, s *protocol.State,
	hooks []feat.HookEntry, phase protocol.Phase, timing string, logDir string) error {
	if len(hooks) == 0 {
		return nil
	}
	action := timing + "_" + string(phase)
	results, err := hook.Execute(ctx, hooks, logDir)
	for _, r := range results {
		ws.AppendEvent(featureID, hook.ToEvent(r, phase, action))
	}
	if err != nil {
		naState, naErr := state.Transition(*s, protocol.PhaseNeedsAttention, "")
		if naErr == nil {
			*s = naState
		} else {
			s.Phase = protocol.PhaseNeedsAttention
		}
		s.Active = false
		if s.StopReason == "" {
			s.StopReason = timing + "-hook-fail"
		}
		if werr := ws.WriteState(featureID, *s); werr != nil {
			slog.Warn("failed to write state during hook error recovery", "feature", featureID, "error", werr)
		}
		if serr := ws.SyncFeatureStatus(featureID, s.Phase); serr != nil {
			slog.Warn("failed to sync feature status during hook error recovery", "feature", featureID, "error", serr)
		}
		return fmt.Errorf("%s hook failed: %w", action, err)
	}
	return nil
}

// ResolveHooks 根據 config 和 feature 的 hooks 設定，回傳目標 phase 的 pre/post hooks。
// feature hooks 同名 key 整組取代全域。
func ResolveHooks(cfg protocol.Config, feature feat.Feature, targetPhase protocol.Phase) map[string][]feat.HookEntry {
	merged := protocol.MergeHooks(cfg.Hooks, feature.Hooks)
	if merged == nil {
		return nil
	}
	result := make(map[string][]feat.HookEntry)
	preKey := "pre_" + string(targetPhase)
	postKey := "post_" + string(targetPhase)
	if h, ok := merged[preKey]; ok {
		result["pre"] = h
	}
	if h, ok := merged[postKey]; ok {
		result["post"] = h
	}
	return result
}
