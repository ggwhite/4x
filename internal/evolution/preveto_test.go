package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestPreVeto_DropsSimilarToExisting(t *testing.T) {
	existing := []feature.Feature{
		{Name: "Runner transient retry", Description: "retry transient runner errors"},
	}
	cands := []protocol.Candidate{
		{Title: "Runner transient retry", Description: "retry transient runner errors", Source: protocol.SourceFailPattern},
		{Title: "Dashboard dark mode", Description: "add dark theme toggle", Source: protocol.SourceStuck},
	}
	kept, dropped := PreVeto(cands, existing, 0.6)
	if len(kept) != 1 || kept[0].Title != "Dashboard dark mode" {
		t.Errorf("kept = %+v, want only dark mode", kept)
	}
	if len(dropped) != 1 || dropped[0].Title != "Runner transient retry" {
		t.Errorf("dropped = %+v, want retry", dropped)
	}
}

func TestPreVeto_DropsIntraBatchDuplicate(t *testing.T) {
	cands := []protocol.Candidate{
		{Title: "Add caching layer", Description: "cache feature id lookups", Source: protocol.SourceFailPattern},
		{Title: "Add caching layer", Description: "cache feature id lookups", Source: protocol.SourceStuck},
	}
	kept, dropped := PreVeto(cands, nil, 0.6)
	if len(kept) != 1 {
		t.Errorf("kept = %d, want 1 (intra-batch dedup)", len(kept))
	}
	if len(dropped) != 1 {
		t.Errorf("dropped = %d, want 1", len(dropped))
	}
}

func TestPreVeto_KeepsOrder(t *testing.T) {
	cands := []protocol.Candidate{
		{Title: "Alpha feature", Description: "first"},
		{Title: "Beta feature", Description: "second"},
		{Title: "Gamma feature", Description: "third"},
	}
	kept, _ := PreVeto(cands, nil, 0.6)
	if len(kept) != 3 || kept[0].Title != "Alpha feature" || kept[2].Title != "Gamma feature" {
		t.Errorf("order not preserved: %+v", kept)
	}
}
