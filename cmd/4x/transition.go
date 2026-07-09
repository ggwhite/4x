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
	"github.com/ggwhite/4x/internal/orchestrator"
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

			cfg, cfgErr := ws.LoadMergedConfig()
			if cfgErr != nil {
				cfg = protocol.Config{}
			}
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				slog.Warn("failed to load feature for hooks", "feature", featureID, "error", err)
			}
			hooksMap := orchestrator.ResolveHooks(cfg, feature, toPhase)
			hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")

			if err := orchestrator.ExecutePhaseHooks(context.Background(), ws, featureID, &s, hooksMap["pre"], toPhase, "pre", hookLogDir); err != nil {
				return err
			}

			// Transition→設欄位→寫回收斂到單一加鎖臨界區，讀最新磁碟值為權威，
			// 避免與進行中的 run loop／dashboard done 競寫時用過時快照覆蓋。
			newState, err := ws.UpdateState(featureID, func(cur *protocol.State) error {
				transitioned, terr := state.Transition(*cur, toPhase, toRole)
				if terr != nil {
					return terr
				}
				if toPhase == protocol.PhaseDone || toPhase == protocol.PhasePendingReview || toPhase == protocol.PhaseBlocked || toPhase == protocol.PhaseAbandoned {
					transitioned.Active = false
				}
				// 人為介入旗標：後續 4x run 的 resume recovery 要尊重此手動 phase，
				// 不被 SmartResumePhase 依磁碟 artifacts 重推導覆蓋（RecoverState 消費後清除）。
				transitioned.ManualPhase = true
				*cur = transitioned
				return nil
			})
			if err != nil {
				return err
			}

			if err := ws.SyncFeatureStatus(featureID, toPhase); err != nil {
				slog.Warn("sync feature status failed", "feature", featureID, "phase", toPhase, "error", err)
			}

			ws.AppendEvent(featureID, protocol.Event{
				Phase: toPhase,
				Type:  "transition",
				Role:  toRole,
				Round: newState.Round,
			})

			if err := orchestrator.ExecutePhaseHooks(context.Background(), ws, featureID, &newState, hooksMap["post"], toPhase, "post", hookLogDir); err != nil {
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
