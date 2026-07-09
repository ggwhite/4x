package orchestrator

import (
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// newF164Runner 建一個帶 Workspace 的 Runner，供 CAS 護欄整合測試使用。
func newF164Runner(t *testing.T, featureID string) (*Runner, *protocol.Workspace) {
	t.Helper()
	ws := setupPhaseWorkspace(t, featureID)
	r := NewRunner(Config{Ws: ws, Feature: feat.Feature{ID: featureID}})
	return r, ws
}

// TestCommitLoopState_YieldsToExternalTermination 驗證 AC-10：run loop 持有 Active=true 的
// 舊快照，其寫回前由外部把同一 feature 設為終態（Active=false）；commitLoopState 不得復活，
// 應回 yielded==true 且磁碟維持外部終態。搭配 -race 執行。
func TestCommitLoopState_YieldsToExternalTermination(t *testing.T) {
	const id = "feat-cas-yield"
	r, ws := newF164Runner(t, id)

	// 初始 Active=true（run loop 進行中）。
	if err := ws.WriteState(id, protocol.State{FeatureID: id, Phase: protocol.PhaseCoding, Round: 1, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	// 外部（模擬 dashboard/CLI）透過 UpdateState 把 feature 設為終態。
	if _, err := ws.UpdateState(id, func(s *protocol.State) error {
		s.Active = false
		s.Phase = protocol.PhaseAbandoned
		s.StopReason = "abandoned"
		return nil
	}); err != nil {
		t.Fatalf("external UpdateState: %v", err)
	}

	// run loop 帶著 Active=true 的 next 快照嘗試 commit。
	next := protocol.State{FeatureID: id, Phase: protocol.PhaseReviewing, Round: 1, Active: true}
	persisted, yielded, err := r.commitLoopState(id, next)
	if err != nil {
		t.Fatalf("commitLoopState: %v", err)
	}
	if !yielded {
		t.Error("yielded = false, want true (外部已終結，應放棄覆寫)")
	}
	if persisted.Active {
		t.Error("persisted.Active = true, want false (不得復活)")
	}
	if persisted.Phase != protocol.PhaseAbandoned {
		t.Errorf("persisted.Phase = %s, want abandoned", persisted.Phase)
	}

	// 磁碟必須維持外部終態。
	disk, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if disk.Active {
		t.Error("disk.Active = true, want false (run loop 復活了外部終態)")
	}
	if disk.Phase != protocol.PhaseAbandoned {
		t.Errorf("disk.Phase = %s, want abandoned", disk.Phase)
	}
}

// TestCommitLoopState_WritesWhenStillActive 驗證 commitLoopState 在磁碟仍 active 時正常寫入
// next 快照、回 yielded==false（護欄不誤傷正常 run-continuing 寫入）。
func TestCommitLoopState_WritesWhenStillActive(t *testing.T) {
	const id = "feat-cas-write"
	r, ws := newF164Runner(t, id)

	if err := ws.WriteState(id, protocol.State{FeatureID: id, Phase: protocol.PhaseCoding, Round: 1, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	next := protocol.State{FeatureID: id, Phase: protocol.PhaseReviewing, Round: 1, Active: true}
	persisted, yielded, err := r.commitLoopState(id, next)
	if err != nil {
		t.Fatalf("commitLoopState: %v", err)
	}
	if yielded {
		t.Error("yielded = true, want false (磁碟仍 active，應正常寫入)")
	}
	if persisted.Phase != protocol.PhaseReviewing {
		t.Errorf("persisted.Phase = %s, want reviewing", persisted.Phase)
	}

	disk, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if disk.Phase != protocol.PhaseReviewing || !disk.Active {
		t.Errorf("disk = {%s, active=%v}, want {reviewing, active=true}", disk.Phase, disk.Active)
	}
}

// TestExternallyTerminated 驗證 AC-12：externallyTerminated 在磁碟 Active==true 時回 (disk, false)；
// 外部把 feature 設為終態後回 (disk, true)、disk.Active==false、disk.Phase 為外部終態。
func TestExternallyTerminated(t *testing.T) {
	const id = "feat-ext-term"
	r, ws := newF164Runner(t, id)

	if err := ws.WriteState(id, protocol.State{FeatureID: id, Phase: protocol.PhaseCoding, Round: 1, Active: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	// (a) Active=true → (_, false)
	if disk, yielded := r.externallyTerminated(); yielded {
		t.Errorf("yielded = true while active, want false (disk=%+v)", disk)
	}

	// (b) 外部設終態後 → (disk, true)
	if _, err := ws.UpdateState(id, func(s *protocol.State) error {
		s.Active = false
		s.Phase = protocol.PhaseDone
		s.StopReason = "done"
		return nil
	}); err != nil {
		t.Fatalf("external UpdateState: %v", err)
	}

	disk, yielded := r.externallyTerminated()
	if !yielded {
		t.Fatal("yielded = false after external termination, want true")
	}
	if disk.Active {
		t.Error("disk.Active = true, want false")
	}
	if disk.Phase != protocol.PhaseDone {
		t.Errorf("disk.Phase = %s, want done", disk.Phase)
	}
}

// TestDeferRunCleanup_SkipsWhenExternallyTerminated 驗證 DeferRunCleanup 收斂到 UpdateState 後，
// 磁碟已非 active（外部已寫終態）時不重複 sync/event、不覆寫既有終態。
func TestDeferRunCleanup_SkipsWhenExternallyTerminated(t *testing.T) {
	const id = "feat-defer-skip"
	ws := setupPhaseWorkspace(t, id)
	if err := ws.WriteState(id, protocol.State{
		FeatureID:  id,
		Phase:      protocol.PhaseDone,
		Round:      2,
		Active:     false,
		StopReason: "done",
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	DeferRunCleanup(ws, id)

	got, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.Phase != protocol.PhaseDone {
		t.Errorf("Phase = %s, want done (unchanged)", got.Phase)
	}
	if got.StopReason != "done" {
		t.Errorf("StopReason = %q, want done (unchanged)", got.StopReason)
	}
}
