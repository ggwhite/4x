package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/hook"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newTransitionCmd() *cobra.Command {
	var to string
	var role string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "transition <feature-id>",
		Short: "Transition feature to a new phase",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			s, err := ws.ReadState(featureID)
			if err != nil {
				if err := ws.InitFeatureDir(featureID); err != nil {
					return err
				}
				s = protocol.State{
					FeatureID: featureID,
					Phase:     protocol.PhaseInit,
					MaxRounds: 5,
					Active:    true,
					Runner:    "claude",
					CreatedAt: time.Now(),
				}
				if err := ws.WriteState(featureID, s); err != nil {
					return err
				}
			}

			toPhase := protocol.Phase(to)
			toRole := protocol.Role(role)
			if toRole == "" {
				toRole = state.PhaseToRole(toPhase)
			}

			if s.Phase == protocol.PhaseTesting && toPhase == protocol.PhaseAccepting {
				result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
				if !result.Pass {
					return fmt.Errorf("testing → accepting blocked: %s", strings.Join(result.Errors, "; "))
				}
			}

			cfg, cfgErr := ws.ReadConfig()
			if cfgErr != nil {
				cfg = protocol.Config{}
			}
			feature, _ := ws.LoadFeature(featureID)
			hooksMap := resolveHooks(cfg, feature, toPhase)
			hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")

			if err := executePhaseHooks(context.Background(), ws, featureID, &s, hooksMap["pre"], toPhase, "pre", hookLogDir); err != nil {
				return err
			}

			newState, err := state.Transition(s, toPhase, toRole)
			if err != nil {
				return err
			}

			if toPhase == protocol.PhaseDone || toPhase == protocol.PhasePendingReview || toPhase == protocol.PhaseBlocked || toPhase == protocol.PhaseAbandoned {
				newState.Active = false
			}

			if err := ws.WriteState(featureID, newState); err != nil {
				return err
			}

			if err := syncFeatureStatus(ws, featureID, toPhase); err != nil {
				slog.Warn("sync feature status failed", "feature", featureID, "phase", toPhase, "error", err)
			}

			ws.AppendEvent(featureID, protocol.Event{
				Phase: toPhase,
				Type:  "transition",
				Role:  toRole,
				Round: newState.Round,
			})

			if err := executePhaseHooks(context.Background(), ws, featureID, &newState, hooksMap["post"], toPhase, "post", hookLogDir); err != nil {
				return err
			}

			if jsonOutput {
				result := struct {
					FeatureID string `json:"featureId"`
					From      string `json:"from"`
					To        string `json:"to"`
				}{
					FeatureID: featureID,
					From:      string(s.Phase),
					To:        string(toPhase),
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Printf("%s → %s (role: %s, round: %d)\n", s.Phase, toPhase, toRole, newState.Round)
			return nil
		}),
	}

	cmd.Flags().StringVar(&to, "to", "", "target phase (required)")
	cmd.Flags().StringVar(&role, "role", "", "override role (optional)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.MarkFlagRequired("to")
	return cmd
}

// executePhaseHooks 執行指定 timing（"pre"/"post"）的 phase hooks，記錄事件，失敗時統一處理狀態。
func executePhaseHooks(ctx context.Context, ws *protocol.Workspace, featureID string, s *protocol.State,
	hooks []protocol.HookEntry, phase protocol.Phase, timing string, logDir string) error {
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
		_ = ws.WriteState(featureID, *s)
		_ = syncFeatureStatus(ws, featureID, s.Phase)
		return fmt.Errorf("%s hook failed: %w", action, err)
	}
	return nil
}

// resolveHooks 根據 config 和 feature 的 hooks 設定，回傳目標 phase 的 pre/post hooks。
// feature hooks 同名 key 整組取代全域。
func resolveHooks(cfg protocol.Config, feature protocol.Feature, targetPhase protocol.Phase) map[string][]protocol.HookEntry {
	merged := protocol.MergeHooks(cfg.Hooks, feature.Hooks)
	if merged == nil {
		return nil
	}
	result := make(map[string][]protocol.HookEntry)
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

// syncFeatureStatus 將 feature YAML 的 Status 欄位同步為對應 phase 的狀態
func syncFeatureStatus(ws *protocol.Workspace, featureID string, phase protocol.Phase) error {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return fmt.Errorf("sync feature status: load: %w", err)
	}
	f.Status = protocol.PhaseToStatus(phase)
	if err := ws.SaveFeature(f); err != nil {
		return fmt.Errorf("sync feature status: save: %w", err)
	}
	return nil
}
