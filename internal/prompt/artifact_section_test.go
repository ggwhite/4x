package prompt

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func profileWith(phases ...protocol.Phase) protocol.ProfileConfig {
	specs := make([]protocol.PhaseSpec, 0, len(phases))
	for _, p := range phases {
		specs = append(specs, protocol.PhaseSpec{Phase: string(p)})
	}
	return protocol.ProfileConfig{Phases: specs}
}

func fullProfile() protocol.ProfileConfig {
	return profileWith(protocol.SelectablePhases()...)
}

// AC-5：lean profile 下 acceptor 段落含 profile 名稱、phase 清單、absent-upstream 段與缺席 report；
// full profile 段落不含 absent-upstream 段。
func TestFormatProfileArtifactSection_AbsentUpstream(t *testing.T) {
	lean := profileWith(protocol.PhaseCoding, protocol.PhaseTesting, protocol.PhaseAccepting)
	got := FormatProfileArtifactSection("lean", lean, protocol.RoleAcceptor)

	for _, want := range []string{
		"lean",
		string(protocol.PhaseCoding),
		"absence is EXPECTED",
		protocol.ReviewReport,
		protocol.DeepReviewReport,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lean acceptor section missing %q\n---\n%s", want, got)
		}
	}

	// AC-9：full profile → AbsentUpstream 空 → 不含 absent-upstream 段。
	full := FormatProfileArtifactSection("full", fullProfile(), protocol.RoleAcceptor)
	if strings.Contains(full, "absence is EXPECTED") {
		t.Errorf("full acceptor section should NOT contain absent-upstream block\n---\n%s", full)
	}
}

// AC-9：full profile 下 tester 段落亦不含 absent-upstream 段。
func TestFormatProfileArtifactSection_FullTesterNoAbsent(t *testing.T) {
	got := FormatProfileArtifactSection("full", fullProfile(), protocol.RoleTester)
	if strings.Contains(got, "absence is EXPECTED") {
		t.Errorf("full tester section should NOT contain absent-upstream block\n---\n%s", got)
	}
	if !strings.Contains(got, "You MUST produce") {
		t.Errorf("tester section missing Required line\n---\n%s", got)
	}
}
