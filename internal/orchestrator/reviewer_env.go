package orchestrator

import (
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// setReviewerExtraEnv 只對 reviewer/deep-reviewer 角色的 SubprocessRunner 注入 FOURX_ROLE
// 與 FOURX_REVIEW_PACKAGE；其他角色（coder/tester/mini-coder/re-verifier…）與非 SubprocessRunner
// 一律不動。reviewPackagePath 是否存在由 runtime 的 guard-tool 判定，此處不檢查檔案存在。
//
// role gating 內建於此 helper，是四個 runner 建構點統一且可單測的唯一 choke point；
// 所有呼叫點可無條件呼叫、把實際 role 傳進來，非目標角色由此自動略過。
func setReviewerExtraEnv(rn runner.Runner, role protocol.Role, reviewPackagePath string) {
	if role != protocol.RoleReviewer && role != protocol.RoleDeepReviewer {
		return
	}
	if sr, ok := rn.(*runner.SubprocessRunner); ok {
		sr.ExtraEnv = []string{
			"FOURX_ROLE=" + string(role),
			"FOURX_REVIEW_PACKAGE=" + reviewPackagePath,
		}
	}
}
