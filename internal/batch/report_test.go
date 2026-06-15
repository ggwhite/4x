package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// newReportWorkspace 在 t.TempDir 下建立一個含 .4x/ 的最小 workspace，供 report 測試使用。
func newReportWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, protocol.DirName), 0o755); err != nil {
		t.Fatalf("mkdir .4x: %v", err)
	}
	return &protocol.Workspace{Root: root}
}

func TestBuildBatchReportCounts(t *testing.T) {
	ws := newReportWorkspace(t)
	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "F001"},
		{FeatureID: "F002"},
		{FeatureID: "F003"},
		{FeatureID: "F004"},
	}}
	statusMap := map[string]protocol.Status{
		"F001": protocol.StatusDone,
		"F002": protocol.StatusReadyForReview,
		"F003": protocol.StatusBlocked,
		"F004": protocol.StatusInProgress,
	}

	started := time.Unix(1000, 0)
	finished := time.Unix(1005, 0)
	r := BuildBatchReport(ws, plan, statusMap, "claude", started, finished, protocol.BatchOutcomeStopped, "F004", "")

	if r.Total != 4 || r.Completed != 2 || r.Failed != 1 || r.Remaining != 1 {
		t.Fatalf("unexpected counts: total=%d completed=%d failed=%d remaining=%d", r.Total, r.Completed, r.Failed, r.Remaining)
	}
	if r.DurationMs != 5000 {
		t.Fatalf("DurationMs = %d, want 5000", r.DurationMs)
	}
	if r.Outcome != protocol.BatchOutcomeStopped || r.RunningFeature != "F004" {
		t.Fatalf("unexpected outcome/running: %q %q", r.Outcome, r.RunningFeature)
	}
	if len(r.Features) != 4 {
		t.Fatalf("Features len = %d, want 4", len(r.Features))
	}
}

func TestBuildBatchReportFeatureDuration(t *testing.T) {
	ws := newReportWorkspace(t)
	if err := ws.InitFeatureDir("F001"); err != nil {
		t.Fatalf("init feature dir: %v", err)
	}
	created := time.Unix(2000, 0)
	st := protocol.State{FeatureID: "F001", Round: 3, CreatedAt: created, StopReason: "max-rounds"}
	if err := ws.WriteState("F001", st); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan := &BatchPlan{Schedule: []ScheduleEntry{{FeatureID: "F001"}}}
	statusMap := map[string]protocol.Status{"F001": protocol.StatusNeedsAttention}
	r := BuildBatchReport(ws, plan, statusMap, "claude", time.Unix(0, 0), time.Unix(1, 0), protocol.BatchOutcomeCompleted, "", "")

	fr := r.Features[0]
	if fr.Rounds != 3 || fr.StopReason != "max-rounds" {
		t.Fatalf("unexpected feature report: rounds=%d stopReason=%q", fr.Rounds, fr.StopReason)
	}
	// WriteState 會把 UpdatedAt 設為 now，因此耗時應為正值。
	if fr.DurationMs <= 0 {
		t.Fatalf("feature DurationMs = %d, want > 0", fr.DurationMs)
	}
}

func ExampleBuildBatchReport() {
	root, _ := os.MkdirTemp("", "batch-report-example-*")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, protocol.DirName), 0o755)
	ws := &protocol.Workspace{Root: root}

	plan := &BatchPlan{Schedule: []ScheduleEntry{
		{FeatureID: "F001"},
		{FeatureID: "F002"},
	}}
	statusMap := map[string]protocol.Status{
		"F001": protocol.StatusDone,
		"F002": protocol.StatusBlocked,
	}

	started := time.Unix(0, 0)
	finished := time.Unix(12, 0)
	r := BuildBatchReport(ws, plan, statusMap, "claude", started, finished, protocol.BatchOutcomeCompleted, "", "")

	fmt.Printf("outcome=%s total=%d completed=%d failed=%d remaining=%d durationMs=%d\n",
		r.Outcome, r.Total, r.Completed, r.Failed, r.Remaining, r.DurationMs)
	// Output: outcome=completed total=2 completed=1 failed=1 remaining=0 durationMs=12000
}
