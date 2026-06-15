package batch

import (
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// BuildBatchReport 從一次 batch run 的進度快照組出 protocol.BatchReport。
//
// 遍歷 plan.Schedule 的每個 feature：用 ws.ReadState 取 round / 耗時（UpdatedAt-CreatedAt）/ stopReason，
// ws.LoadFeature 取 name，statusMap 取最終 status；缺 state 的 feature 耗時與 round 記 0。
// 統計 Completed（feature.BatchCompleted 為真）/ Failed（blocked、needs-attention）/ Remaining（Total 扣除前兩者）。
// outcome 由呼叫端依結束原因傳入（completed / stopped / interrupted / crashed），
// runningFeature 與 panicMsg 僅 interrupted / crashed 時有意義，其餘傳空字串即可。
func BuildBatchReport(ws *protocol.Workspace, plan *BatchPlan, statusMap map[string]feature.Status,
	runner string, startedAt, finishedAt time.Time, outcome, runningFeature, panicMsg string) protocol.BatchReport {

	report := protocol.BatchReport{
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		DurationMs:     finishedAt.Sub(startedAt).Milliseconds(),
		Outcome:        outcome,
		Runner:         runner,
		RunningFeature: runningFeature,
		PanicMessage:   panicMsg,
	}

	for _, entry := range plan.Schedule {
		id := entry.FeatureID
		status := statusMap[id]

		fr := protocol.BatchFeatureReport{
			ID:          id,
			FinalStatus: status,
		}
		if f, err := ws.LoadFeature(id); err == nil {
			fr.Name = f.Name
		}
		if st, err := ws.ReadState(id); err == nil {
			fr.Rounds = st.Round
			fr.StopReason = st.StopReason
			if !st.CreatedAt.IsZero() && !st.UpdatedAt.IsZero() {
				fr.DurationMs = st.UpdatedAt.Sub(st.CreatedAt).Milliseconds()
			}
		}
		report.Features = append(report.Features, fr)

		report.Total++
		switch {
		case feature.BatchCompleted(status):
			report.Completed++
		case status == feature.StatusBlocked || status == feature.StatusNeedsAttention:
			report.Failed++
		}
	}
	report.Remaining = report.Total - report.Completed - report.Failed
	return report
}
