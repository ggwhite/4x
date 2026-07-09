package orchestrator

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// AC-15（F157 post-merge 缺陷 5 修訂）：setReviewerExtraEnv 對所有角色無條件注入
// FOURX_FEATURE_ID；只有 reviewer/deep-reviewer 額外注入 FOURX_ROLE/FOURX_REVIEW_PACKAGE，
// 其他角色的 ExtraEnv 只含 FOURX_FEATURE_ID 一個元素。
func TestSetReviewerExtraEnv(t *testing.T) {
	const pkg = "/abs/round-1/review-package.md"
	const featureID = "F157-x"

	tests := []struct {
		role          protocol.Role
		wantReviewEnv bool
	}{
		{protocol.RoleReviewer, true},
		{protocol.RoleDeepReviewer, true},
		{protocol.RoleCoder, false},
		{protocol.RoleTester, false},
		{protocol.RoleDesigner, false},
		{protocol.RoleFixer, false},
		{protocol.RoleMiniCoder, false},
		{protocol.Role(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			sr := &runner.SubprocessRunner{}
			setReviewerExtraEnv(sr, tt.role, featureID, pkg)

			want := []string{"FOURX_FEATURE_ID=" + featureID}
			if tt.wantReviewEnv {
				want = append(want, "FOURX_ROLE="+string(tt.role), "FOURX_REVIEW_PACKAGE="+pkg)
			}
			if len(sr.ExtraEnv) != len(want) {
				t.Fatalf("role %q ExtraEnv = %v, want %v", tt.role, sr.ExtraEnv, want)
			}
			for i := range want {
				if sr.ExtraEnv[i] != want[i] {
					t.Errorf("role %q ExtraEnv[%d] = %q, want %q", tt.role, i, sr.ExtraEnv[i], want[i])
				}
			}
		})
	}
}

func TestSetReviewerExtraEnv_NilRunner(t *testing.T) {
	// 傳入 nil Runner 介面值（type-assert 失敗）不應 panic。
	var rn runner.Runner
	setReviewerExtraEnv(rn, protocol.RoleReviewer, "F157-x", "/x/review-package.md")
}
