package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestCandidateToDiscovered(t *testing.T) {
	c := protocol.Candidate{Title: "T", Description: "D", Source: protocol.SourceStuck}
	got := CandidateToDiscovered(c)
	if got.Title != "T" || got.Description != "D" {
		t.Errorf("CandidateToDiscovered = %+v", got)
	}
}

func TestBareFeature(t *testing.T) {
	c := protocol.Candidate{Title: "T", Description: "原始描述"}
	f := BareFeature(c, "F101-t", "F101: T")
	if f.ID != "F101-t" || f.Name != "F101: T" {
		t.Errorf("id/name not set: %+v", f)
	}
	if f.Description != "原始描述" {
		t.Errorf("description should be candidate text, got %q", f.Description)
	}
	if f.Status != feature.StatusNotStarted {
		t.Errorf("status = %q, want not-started", f.Status)
	}
}
