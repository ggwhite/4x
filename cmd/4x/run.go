package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run <feature-id>",
		Short: "Run the Design-Code-Review-Test loop for a feature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID := args[0]
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				return err
			}

			if runnerName == "" {
				runnerName = cfg.Default
			}
			runnerCfg, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			if maxRounds <= 0 {
				maxRounds = 5
			}

			if err := ws.InitFeatureDir(featureID); err != nil {
				return err
			}

			s := protocol.State{
				FeatureID: featureID,
				Phase:     protocol.PhaseInit,
				MaxRounds: maxRounds,
				Active:    true,
				Runner:    runnerName,
				CreatedAt: time.Now(),
			}

			if existing, err := ws.ReadState(featureID); err == nil {
				s = existing
				s.Active = true
				s.Runner = runnerName
				if s.Phase == protocol.PhaseDone {
					return fmt.Errorf("feature %s is already done", featureID)
				}
			}

			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:  "run-start",
				Phase: s.Phase,
				Role:  state.PhaseToRole(s.Phase),
			})

			if dryRun {
				return dryRunLoop(ws, feature, cfg, s)
			}

			r := runner.NewRunner(ws, runnerName, runnerCfg, time.Duration(timeout)*time.Second)
			return runLoop(ws, feature, cfg, s, r)
		},
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name (default: config default)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max iteration rounds (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling plugin")
	return cmd
}

// runLoop 驅動完整的 Design→Code→Review→Test loop
func runLoop(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State, r runner.Runner) error {
	featureID := feature.ID
	ctx := context.Background()

	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseAccepting, protocol.RoleDesigner},
	}

	startIdx := 0
	for i, p := range phases {
		if s.Phase == p.phase {
			startIdx = i
			break
		}
	}
	if s.Phase == protocol.PhaseInit {
		startIdx = 0
	}

	for round := s.Round; round <= s.MaxRounds; round++ {
		for i := startIdx; i < len(phases); i++ {
			p := phases[i]

			newState, err := state.Transition(s, p.phase, p.role)
			if err != nil {
				fmt.Printf("  skip %s→%s: %v\n", s.Phase, p.phase, err)
				continue
			}
			s = newState

			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:  "phase-start",
				Phase: p.phase,
				Role:  p.role,
				Round: s.Round,
			})

			fmt.Printf("[round %d] %s (%s)\n", s.Round, p.phase, p.role)

			result, err := r.Run(ctx, featureID, p.role)
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				s.Active = false
				s.StopReason = "plugin-error"
				ws.WriteState(featureID, s)
				ws.AppendEvent(featureID, protocol.Event{
					Type:   "run-end",
					Phase:  p.phase,
					Role:   p.role,
					Round:  s.Round,
					Status: "error",
					Detail: err.Error(),
				})
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:   "run-end",
				Phase:  p.phase,
				Role:   p.role,
				Round:  s.Round,
				Status: fmt.Sprintf("exit-%d", result.ExitCode),
			})

			if runner.IsHardError(result) {
				s.Active = false
				s.StopReason = "hard-error"
				ws.WriteState(featureID, s)
				return fmt.Errorf("plugin returned hard error (exit 2)")
			}

			if runner.IsSoftFail(result) {
				s.Phase = protocol.PhaseBlocked
				s.Active = false
				s.StopReason = "soft-fail"
				ws.WriteState(featureID, s)
				fmt.Printf("  soft fail — feature blocked\n")
				return nil
			}

			check := guard.Check(ws, featureID)
			if !check.Pass {
				fmt.Printf("  guardrails failed:\n")
				for _, e := range check.Errors {
					fmt.Printf("    ERROR: %s\n", e)
				}
			}

			if stop, reason := state.ShouldStop(s); stop {
				s.Active = false
				s.StopReason = reason
				ws.WriteState(featureID, s)
				fmt.Printf("  stopped: %s\n", reason)
				return nil
			}
		}

		startIdx = 1
	}

	s.Phase = protocol.PhaseDone
	s.Active = false
	s.StopReason = "done"
	ws.WriteState(featureID, s)
	syncFeatureStatus(ws, featureID, protocol.PhaseDone)
	ws.AppendEvent(featureID, protocol.Event{
		Type:   "transition",
		Phase:  protocol.PhaseDone,
		Status: "done",
	})

	fmt.Printf("\n✅ Feature %s complete (%d rounds)\n", featureID, s.Round)
	return nil
}

// dryRunLoop 印出每個 phase 的 prompt（不呼叫 plugin）
func dryRunLoop(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State) error {
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)

		data := promptData{
			Feature: feature,
			Project: cfg.Project,
			Role:    p.role,
			Round:   1,
			Config:  cfg,
			DotDir:  ws.DotDir(),
		}

		tmpl, err := loadRoleTemplate(p.role)
		if err != nil {
			fmt.Printf("  (no template for %s)\n\n", p.role)
			continue
		}
		tmpl.Execute(os.Stdout, data)
		fmt.Println()
	}
	return nil
}
