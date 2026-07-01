package protocol

import (
	"time"

	"github.com/ggwhite/4x/internal/feature"
)

// BatchConflict 是 batch auto-merge 遇衝突暫停時寫入的信號（.4x/batch-conflict.json）。
// dashboard 讀此檔得知是哪個 feature、哪個 repo、哪些檔案發生衝突，供使用者解完後 Continue Batch。
type BatchConflict struct {
	FeatureID    string    `json:"featureId"`
	FeatureName  string    `json:"featureName"`
	ConflictRepo string    `json:"conflictRepo"`
	Files        []string  `json:"files"`
	DetectedAt   time.Time `json:"detectedAt"`
}

// Batch run 的結束結果（BatchReport.Outcome）列舉值，描述「批次如何終止」而非「是否全數成功」。
// 即使 outcome=completed，仍可能有 feature 失敗被跳過（看 Failed/Remaining 計數判斷實際結果）。
const (
	BatchOutcomeCompleted   = "completed"   // 主迴圈自然跑完排程（含失敗達上限被跳過），非被停止/中斷/crash
	BatchOutcomeStopped     = "stopped"     // 使用者按 Stop / 衝突暫停等 graceful 提前結束
	BatchOutcomeInterrupted = "interrupted" // 收到 SIGTERM/SIGINT 中斷
	BatchOutcomeCrashed     = "crashed"     // 行程 panic
)

// BatchReport 是一次 batch run 結束後（正常 / stop / interrupt / crash）寫入 .4x/batch-report.json
// 的整體報告。dashboard 在 batch 沒在跑時讀此檔顯示「上次 batch 報告」摘要與每個 feature 的最終狀態。
type BatchReport struct {
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	DurationMs     int64                `json:"durationMs"`
	Outcome        string               `json:"outcome"`
	Total          int                  `json:"total"`
	Completed      int                  `json:"completed"`
	Failed         int                  `json:"failed"`
	Remaining      int                  `json:"remaining"`
	Runner         string               `json:"runner"`
	Features       []BatchFeatureReport `json:"features"`
	PanicMessage   string               `json:"panicMessage,omitempty"`   // 僅 crashed
	RunningFeature string               `json:"runningFeature,omitempty"` // interrupted/crashed 時正在跑的 feature id
}

// BatchFeatureReport 是 BatchReport 內單一 feature 的最終狀態快照。
type BatchFeatureReport struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	FinalStatus feature.Status `json:"finalStatus"`
	DurationMs  int64          `json:"durationMs"`
	Rounds      int            `json:"rounds"`
	StopReason  string         `json:"stopReason,omitempty"`
}
