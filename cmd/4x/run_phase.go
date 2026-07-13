package main

import (
	"fmt"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// firstRunRole 計算本次 run 實際會先跑的 role，與真實 RunLoop 對齊：fresh（Phase==init）
// 時 initPhase 會把 init→designing 交給 designer；resume 時首個 resolvePrompt 對
// PhaseToRole(s.Phase)。供 dry-run 把一次性 note 對準正確角色（按 role 比對，非固定清單第一項）。
func firstRunRole(s protocol.State) protocol.Role {
	p := s.Phase
	if p == protocol.PhaseInit {
		p = protocol.PhaseDesigning
	}
	return state.PhaseToRole(p)
}

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

	// F185：一次性 note 只綁到本次實際先跑的 role（按 role 比對），並在標頭明示 note 會給誰。
	target := firstRunRole(s)
	if s.RunNote != "" {
		fmt.Printf("=== One-Shot Note (will be injected into first role: %s) ===\n%s\n\n", target, s.RunNote)
	}

	for _, p := range phases {
		if p.role == target {
			pctx.ExtraPrompt = s.RunNote
		} else {
			pctx.ExtraPrompt = ""
		}
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
