package guard

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/verify"
)

// PrecheckTestStrategy 只跑 test-strategy.yaml 的靜態 precheck，供 designing 出口閘門呼叫。
// 刻意不呼叫 guard.Check——後者含 build-gate / scope / baseline 等只適用 coding 之後的檢查。
//
// 三種結果：YAML 解析失敗 → Pass=false 且訊息含 "parse failed"（壞掉的 test-strategy.yaml
// 會讓 checkTestStrategyVerifyTypes / checkACChecksSchema 同時靜默失效，這裡把它升成阻擋級）；
// 檔案不存在或無任何 verify 命令 → Pass=true（向下相容，不因缺檔而擋）；
// 其餘 → 對每條命令跑 verify.PrecheckStrategy，SeverityError 進 Errors 並累計 RetryableErrors。
func PrecheckTestStrategy(ws *protocol.Workspace, featureID string) CheckResult {
	r := CheckResult{Pass: true}

	ts, err := ws.ReadTestStrategy(featureID)
	if err != nil {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("test-strategy.yaml: parse failed: %v", err))
		r.RetryableErrors++
		return r
	}
	if len(ts.Verify) == 0 && len(ts.VerifyGroups) == 0 && len(ts.ACChecks) == 0 {
		return r
	}

	for _, f := range verify.PrecheckStrategy(ts, ws.Root) {
		if f.Severity == verify.SeverityWarn {
			r.Warns = append(r.Warns, f.Error())
			continue
		}
		r.Pass = false
		r.Errors = append(r.Errors, f.Error())
		r.RetryableErrors++
	}
	return r
}

// checkVerifyPrecheck 把 PrecheckTestStrategy 的結果併進 `4x check` 的整體結果，
// 讓 Designer 手跑 4x check 時看得到與 designing 出口閘門同一批錯誤。
//
// 只在 phase == designing 時套用（比照 checkRequiredFiles 的 phase-aware 慣例）。
// 理由：guard.Check 有五個 orchestrator 呼叫點把 !Pass 當硬停止（handlePostCoder、
// parallel review 收尾、deep_review 兩處、review_converge），而 test-strategy.yaml
// 不在 Coder 的可改範圍內——一條與本輪 diff 無關的舊命令若在 coding 收尾被判違規，
// 會停掉整個 run 且無法自癒。既有 feature 的 test-strategy.yaml 亦不做回溯修正。
func checkVerifyPrecheck(ws *protocol.Workspace, featureID string, r *CheckResult) {
	st, err := ws.ReadState(featureID)
	if err != nil || st.Phase != protocol.PhaseDesigning {
		return
	}
	res := PrecheckTestStrategy(ws, featureID)
	r.Warns = append(r.Warns, res.Warns...)
	if res.Pass {
		return
	}
	r.Pass = false
	r.Errors = append(r.Errors, res.Errors...)
	r.RetryableErrors += res.RetryableErrors
}
