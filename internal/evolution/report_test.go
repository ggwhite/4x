package evolution

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestBuildEvolveReport_Sections 驗證 AC-11：各分段標題與關鍵內容皆出現。
func TestBuildEvolveReport_Sections(t *testing.T) {
	r := EvolveRoundResult{
		Round:    2,
		Mined:    []protocol.Candidate{{Title: "Cand A"}},
		Deduped:  1,
		Accepted: []protocol.Candidate{{Title: "Cand A", ValueScore: 0.9}},
		Rejected: []Rejection{{Title: "Cand B", Reason: "value_score below floor"}},
		Enqueued: []EnqueuedFeature{{ID: "F101", Name: "F101: Cand A", Enriched: false}},
		AutoRan:  []AutoRunResult{{ID: "F101", FinalStatus: "pending-review", SelfModBlocked: true}},
	}
	out := BuildEvolveReport(r)

	for _, want := range []string{
		"# Evolve Report — Round 2",
		"## Mined", "Cand A",
		"## Accepted", "value 0.90",
		"## Rejected", "value_score below floor",
		"## Enqueued", "F101", "enriched=false",
		"## Auto-Run", "pending-review", "self-mod blocked=true",
		"## Halted", "false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

func TestBuildEvolveReport_Halted(t *testing.T) {
	out := BuildEvolveReport(EvolveRoundResult{Round: 5, Halted: true, ConsecutiveNoAccept: 3})
	if !strings.Contains(out, "## Halted") || !strings.Contains(out, "true") {
		t.Errorf("halted report should mark Halted true:\n%s", out)
	}
	if !strings.Contains(out, "## Accepted") || !strings.Contains(out, "None") {
		t.Errorf("halted report should still render empty sections:\n%s", out)
	}
}
