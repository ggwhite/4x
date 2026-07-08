package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newRetryCmd() *cobra.Command {
	var toPhase string

	cmd := &cobra.Command{
		Use:   "retry <feature-id>",
		Short: "Recover from needs-attention or blocked and re-run",
		Long: `Transition the feature from needs-attention or blocked back to a working phase,
then immediately run the loop. Equivalent to 'transition --to <phase> && run'.

Default target phase is 'accepting' (re-run the Acceptor after fixing issues).
Use --to to target a different phase, e.g. --to amending to go back to coding.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			target := protocol.Phase(toPhase)
			if target == "" {
				target = protocol.PhaseAccepting
			}

			newState, from, err := retryTransition(ws, featureID, target)
			if err != nil {
				return err
			}

			fmt.Printf("%s → %s, launching run...\n", from, target)

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine executable path: %w", err)
			}
			c := exec.Command(exe, "run", featureID)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			_ = newState
			return c.Run()
		},
	}

	cmd.Flags().StringVar(&toPhase, "to", "", "target phase to recover to (default: accepting)")
	return cmd
}

// retryTransition 從 needs-attention / blocked 轉回目標 phase，回傳新 state 與原始 phase。
// 供測試直接呼叫，不含 exec 邏輯。
func retryTransition(ws *protocol.Workspace, featureID string, target protocol.Phase) (protocol.State, protocol.Phase, error) {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return protocol.State{}, "", fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhaseNeedsAttention && s.Phase != protocol.PhaseBlocked {
		return protocol.State{}, "", fmt.Errorf("retry only works from needs-attention or blocked (current phase: %s)", s.Phase)
	}

	toRole := state.PhaseToRole(target)
	newState, err := state.Transition(s, target, toRole)
	if err != nil {
		return protocol.State{}, "", fmt.Errorf("cannot transition %s → %s: %w", s.Phase, target, err)
	}
	newState.Active = true
	// 人為介入旗標：child 的 4x run recovery 要尊重此手動 phase，
	// 不被 SmartResumePhase 依磁碟 artifacts 重推導覆蓋（RecoverState 消費後清除）。
	newState.ManualPhase = true

	if err := ws.WriteState(featureID, newState); err != nil {
		return protocol.State{}, "", err
	}
	if err := ws.SyncFeatureStatus(featureID, target); err != nil {
		_ = err
	}
	ws.AppendEvent(featureID, protocol.Event{
		Phase: target,
		Type:  "transition",
		Role:  toRole,
		Round: newState.Round,
	})

	return newState, s.Phase, nil
}
