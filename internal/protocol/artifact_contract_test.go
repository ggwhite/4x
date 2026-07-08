package protocol

import (
	"reflect"
	"testing"
)

// profileWith 以指定 phase 建構一個 ProfileConfig（Go literal 路徑，測試用）。
func profileWith(phases ...Phase) ProfileConfig {
	specs := make([]PhaseSpec, 0, len(phases))
	for _, p := range phases {
		specs = append(specs, PhaseSpec{Phase: string(p)})
	}
	return ProfileConfig{Phases: specs}
}

func fullProfile() ProfileConfig {
	return profileWith(SelectablePhases()...)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// AC-1：ArtifactContract 對 8 個角色回 (contract, true)，Produces 與權威表一致；子角色回 false。
func TestArtifactContract_Produces(t *testing.T) {
	cases := map[Role][]string{
		RoleDesigner:       {TaskBrief, Criteria, TestStratFile},
		RoleDesignReviewer: {DesignReviewReport},
		RoleCoder:          {CoderReport},
		RoleReviewer:       {ReviewReport},
		RoleTester:         {TestReport, FinalReport},
		RoleDeepReviewer:   {DeepReviewReport},
		RoleFixer:          {FixerReport},
		RoleAcceptor:       {FinalReport, RetroLearningsFile},
	}
	for _, role := range AllRoles() {
		rc, ok := ArtifactContract(role)
		if !ok {
			t.Fatalf("ArtifactContract(%s) = false, want true", role)
		}
		want := sortedCopy(cases[role])
		if !reflect.DeepEqual(rc.Produces, want) {
			t.Errorf("ArtifactContract(%s).Produces = %v, want %v", role, rc.Produces, want)
		}
	}
	if _, ok := ArtifactContract(RoleMiniCoder); ok {
		t.Errorf("ArtifactContract(RoleMiniCoder) = true, want false (sub-role)")
	}
}

// AC-2：lean profile [coding,testing,accepting] 下 acceptor 的 AbsentUpstream 含 review/deep-review report；
// full profile 下為空。
func TestRoleArtifactView_AcceptorAbsentUpstream(t *testing.T) {
	lean := profileWith(PhaseCoding, PhaseTesting, PhaseAccepting)
	view := lean.RoleArtifactView(RoleAcceptor)
	if !contains(view.AbsentUpstream, ReviewReport) {
		t.Errorf("lean acceptor AbsentUpstream = %v, want to contain %s", view.AbsentUpstream, ReviewReport)
	}
	if !contains(view.AbsentUpstream, DeepReviewReport) {
		t.Errorf("lean acceptor AbsentUpstream = %v, want to contain %s", view.AbsentUpstream, DeepReviewReport)
	}

	full := fullProfile().RoleArtifactView(RoleAcceptor)
	if len(full.AbsentUpstream) != 0 {
		t.Errorf("full acceptor AbsentUpstream = %v, want empty", full.AbsentUpstream)
	}
}

// AC-3：full profile 下 tester 的 Forbidden 不含 final-report.md（共產），含 review-report.md。
func TestRoleArtifactView_TesterForbidden(t *testing.T) {
	view := fullProfile().RoleArtifactView(RoleTester)
	if contains(view.Forbidden, FinalReport) {
		t.Errorf("tester Forbidden = %v, must NOT contain %s (tester+acceptor co-produce)", view.Forbidden, FinalReport)
	}
	if !contains(view.Forbidden, ReviewReport) {
		t.Errorf("tester Forbidden = %v, want to contain %s", view.Forbidden, ReviewReport)
	}
}

// AC-4：EnabledPhaseNames 以 canonical 順序回傳已啟用 phase 名稱。
func TestEnabledPhaseNames(t *testing.T) {
	full := fullProfile().EnabledPhaseNames()
	if len(full) != 8 {
		t.Errorf("full EnabledPhaseNames len = %d, want 8: %v", len(full), full)
	}

	lean := profileWith(PhaseCoding, PhaseTesting, PhaseAccepting).EnabledPhaseNames()
	want := []string{string(PhaseCoding), string(PhaseTesting), string(PhaseAccepting)}
	if !reflect.DeepEqual(lean, want) {
		t.Errorf("lean EnabledPhaseNames = %v, want %v", lean, want)
	}
}
