package main

import (
	"fmt"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

func dryRunLoop(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State) error {
	pctx := &prompt.Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg}
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)
		promptText, err := prompt.Generate(pctx, p.role, 1, 0, "")
		if err != nil {
			fmt.Printf("  (error: %v)\n\n", err)
			continue
		}
		fmt.Println(promptText)
		fmt.Println()
	}
	return nil
}
