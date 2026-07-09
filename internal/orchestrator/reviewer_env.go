package orchestrator

import (
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// setReviewerExtraEnv 對每個 SubprocessRunner 無條件注入 FOURX_FEATURE_ID（F157 post-merge
// 缺陷 5 的修復：cmd/4x/check.go 的 resolvePathFeatureID 優先讀 FOURX_FEATURE_ID 明確消解
// feature id 的邏輯本已正確，但若沒有任何呼叫端設定此環境變數，該邏輯永遠走不到——當同一
// workspace 有 >1 個 active feature 並行跑（如多 session 各自 `4x run` 不同 feature）時，
// PreToolUse 即時 gate 會因無法唯一決定 feature 而整體 fail-open，形同關閉）。
// 另外只對 reviewer/deep-reviewer 角色額外注入 FOURX_ROLE 與 FOURX_REVIEW_PACKAGE；
// 其他角色（coder/tester/mini-coder/re-verifier…）不注入這兩者。reviewPackagePath
// 是否存在由 runtime 的 guard-tool 判定，此處不檢查檔案存在。
//
// role gating（FOURX_ROLE/FOURX_REVIEW_PACKAGE 部分）與 featureID 注入（全角色）皆內建於
// 此 helper，是四個 runner 建構點統一且可單測的唯一 choke point；所有呼叫點可無條件呼叫、
// 把實際 role 與 featureID 傳進來，非目標角色的額外 env 由此自動略過。
func setReviewerExtraEnv(rn runner.Runner, role protocol.Role, featureID, reviewPackagePath string) {
	sr, ok := rn.(*runner.SubprocessRunner)
	if !ok {
		return
	}
	env := []string{"FOURX_FEATURE_ID=" + featureID}
	if role == protocol.RoleReviewer || role == protocol.RoleDeepReviewer {
		env = append(env,
			"FOURX_ROLE="+string(role),
			"FOURX_REVIEW_PACKAGE="+reviewPackagePath,
		)
	}
	sr.ExtraEnv = env
}
