package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestMarkDone_SelfModBlocksAutoMerge 驗證 F098：觸及受保護路徑且未核可時，
// markDone（不帶 approve）不可 merge，feature 維持 pending-review，且不被標記 approved。
func TestMarkDone_SelfModBlocksAutoMerge(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sm")
	ws.SaveFeature(feature.Feature{ID: "feat-sm", Name: "Self Mod", Status: "pending-review"})
	ws.WriteState("feat-sm", protocol.State{
		FeatureID:      "feat-sm",
		Phase:          protocol.PhasePendingReview,
		Runner:         "mock",
		SelfModTouched: true,
		SelfModPaths:   []string{"internal/state/machine.go"},
	})

	err := markDone(ws, "feat-sm", false, false)
	if err == nil {
		t.Fatal("markDone should return error when self-mod approval is required")
	}

	final, _ := ws.ReadState("feat-sm")
	if final.Phase != protocol.PhasePendingReview {
		t.Errorf("phase = %s, want pending-review (merge must be blocked)", final.Phase)
	}
	if final.SelfModApproved {
		t.Error("SelfModApproved must remain false when not approved")
	}
}

// TestMarkDone_SelfModApproveSetsApproved 驗證帶 --approve-self-mod 時設定 SelfModApproved，
// 不再被 approve 關卡擋下（此處不驗證實際 merge 結果，僅驗證核可狀態落地）。
func TestMarkDone_SelfModApproveSetsApproved(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-sm2")
	ws.SaveFeature(feature.Feature{ID: "feat-sm2", Name: "Self Mod 2", Status: "pending-review"})
	ws.WriteState("feat-sm2", protocol.State{
		FeatureID:      "feat-sm2",
		Phase:          protocol.PhasePendingReview,
		Runner:         "mock",
		SelfModTouched: true,
		SelfModPaths:   []string{"internal/guard/check.go", "internal/guard/check_test.go"},
	})

	// 帶 approve 旗標：approve 關卡放行並落地 SelfModApproved（後續 merge 由既有邏輯處理）。
	_ = markDone(ws, "feat-sm2", true, false)

	final, _ := ws.ReadState("feat-sm2")
	if !final.SelfModApproved {
		t.Error("SelfModApproved should be set true after --approve-self-mod")
	}
}
