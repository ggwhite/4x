package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newPromptCmd() *cobra.Command {
	var role string
	var round int
	var runner string

	cmd := &cobra.Command{
		Use:   "prompt <feature-id>",
		Short: "Generate a role prompt for the current phase",
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

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			// best-effort 讀 state：供 profile 注入（F150）以及 role="" 時回填 role/round。
			// 讀不到時，只有在未指定 --role 的情況下才是致命錯誤。
			s, stateErr := ws.ReadState(featureID)
			r := protocol.Role(role)
			if r == "" {
				if stateErr != nil {
					return fmt.Errorf("no --role specified and cannot read state: %w", stateErr)
				}
				r = state.PhaseToRole(s.Phase)
				if round == 0 {
					round = s.Round
				}
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			pctx := &prompt.Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg}
			if stateErr == nil {
				pctx.Profile = s.Profile
			}
			p, err := prompt.Generate(pctx, r, round, 0, runner)
			if err != nil {
				return err
			}
			_, err = os.Stdout.WriteString(p)
			return err
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "role (designer/coder/reviewer/tester)")
	cmd.Flags().IntVar(&round, "round", 0, "round number")
	cmd.Flags().StringVar(&runner, "runner", "", "runner name (skip auto-read convention files)")
	return cmd
}
