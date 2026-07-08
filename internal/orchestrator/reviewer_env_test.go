package orchestrator

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// AC-15：setReviewerExtraEnv 的 role gating——只有 reviewer/deep-reviewer 被注入 ExtraEnv，
// 其他角色維持 nil；注入值為 FOURX_ROLE/FOURX_REVIEW_PACKAGE 兩元素。
func TestSetReviewerExtraEnv(t *testing.T) {
	const pkg = "/abs/round-1/review-package.md"

	tests := []struct {
		role       protocol.Role
		wantInject bool
	}{
		{protocol.RoleReviewer, true},
		{protocol.RoleDeepReviewer, true},
		{protocol.RoleCoder, false},
		{protocol.RoleTester, false},
		{protocol.RoleDesigner, false},
		{protocol.RoleFixer, false},
		{protocol.Role(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			sr := &runner.SubprocessRunner{}
			setReviewerExtraEnv(sr, tt.role, pkg)

			if !tt.wantInject {
				if sr.ExtraEnv != nil {
					t.Errorf("role %q should not inject ExtraEnv, got %v", tt.role, sr.ExtraEnv)
				}
				return
			}
			want := []string{"FOURX_ROLE=" + string(tt.role), "FOURX_REVIEW_PACKAGE=" + pkg}
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
	setReviewerExtraEnv(rn, protocol.RoleReviewer, "/x/review-package.md")
}
