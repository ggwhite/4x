package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func cfgFor(maxAccept, maxBacklog int) ResolvedEvolution {
	return ResolvedEvolution{ValueFloor: 0.6, MaxAcceptPerRun: maxAccept, MaxBacklogUndone: maxBacklog, DedupThreshold: 0.6}
}

func TestPostVeto_AcceptsValid(t *testing.T) {
	cands := []protocol.Candidate{{Title: "A", Description: "d", Source: protocol.SourceStuck}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.8, WhyNotHack: "real"}}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 1 || acc[0].ValueScore != 0.8 || acc[0].WhyNotHack != "real" {
		t.Errorf("accepted = %+v", acc)
	}
	if len(rej) != 0 {
		t.Errorf("rejected = %+v, want none", rej)
	}
}

func TestPostVeto_RejectsNonAccept(t *testing.T) {
	cands := []protocol.Candidate{{Title: "A", Description: "d"}, {Title: "B", Description: "d"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictReject, ValueScore: 0.9, WhyNotHack: "x"}}
	// A 被 gate reject；B 無對應 verdict。
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 0 || len(rej) != 2 {
		t.Fatalf("acc=%d rej=%d, want 0/2", len(acc), len(rej))
	}
	if rej[0].Reason != "gate did not accept" || rej[1].Reason != "gate did not accept" {
		t.Errorf("reasons = %+v", rej)
	}
}

func TestPostVeto_RejectsNoJustification(t *testing.T) {
	cands := []protocol.Candidate{{Title: "A", Description: "d"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "  "}}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 0 || len(rej) != 1 || rej[0].Reason != "missing why_not_hack justification" {
		t.Fatalf("acc=%d rej=%+v, want 0 / missing justification", len(acc), rej)
	}
}

func TestPostVeto_RejectsBelowFloor(t *testing.T) {
	cands := []protocol.Candidate{{Title: "A", Description: "d"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.4, WhyNotHack: "x"}}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 0 || rej[0].Reason != "value_score below floor" {
		t.Errorf("acc=%+v rej=%+v", acc, rej)
	}
}

func TestPostVeto_RejectsDuplicateExisting(t *testing.T) {
	existing := []feature.Feature{{Name: "Runner retry", Description: "retry transient runner errors", Status: feature.StatusDone}}
	cands := []protocol.Candidate{{Title: "Runner retry", Description: "retry transient runner errors"}}
	verdicts := []Verdict{{Title: "Runner retry", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "x"}}
	acc, rej := PostVeto(cands, verdicts, existing, cfgFor(3, 15))
	if len(acc) != 0 || rej[0].Reason != "duplicates existing feature" {
		t.Errorf("acc=%+v rej=%+v", acc, rej)
	}
}

func TestPostVeto_AcceptCap(t *testing.T) {
	cands := []protocol.Candidate{{Title: "A", Description: "a"}, {Title: "B", Description: "b"}}
	verdicts := []Verdict{
		{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "x"},
		{Title: "B", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "y"},
	}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(1, 15))
	if len(acc) != 1 || len(rej) != 1 || rej[0].Reason != "max_accept_per_run reached" {
		t.Errorf("acc=%d rej=%+v, want 1/1 (cap)", len(acc), rej)
	}
}

func TestPostVeto_BacklogCap(t *testing.T) {
	existing := []feature.Feature{
		{Name: "X", Status: feature.StatusNotStarted},
		{Name: "Y", Status: feature.StatusDone},
		{Name: "Z", Status: feature.StatusDraft},
	}
	cands := []protocol.Candidate{{Title: "A", Description: "a"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "x"}}
	// 未做數 = 1（只有 X；Y=done、Z=draft 不計）；上限 1 → 已滿，A 應被拒。
	acc, rej := PostVeto(cands, verdicts, existing, cfgFor(3, 1))
	if len(acc) != 0 || len(rej) != 1 || rej[0].Reason != "max_backlog_undone reached" {
		t.Errorf("acc=%d rej=%+v, want 0/1 (backlog cap)", len(acc), rej)
	}
}
