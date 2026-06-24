package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// newFinalizeWorkspace 建一個帶 .4x/ 的暫時 workspace，並寫入一筆 pending-review state。
// withFeatureYAML 為 false 時刻意不建 feature YAML，使 SyncFeatureStatus 因 LoadFeature 失敗而報錯，
// 用來驗證 FinalizeDone 對 sync 失敗採 non-fatal。
func newFinalizeWorkspace(t *testing.T, featureID string, withFeatureYAML bool) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws, err := protocol.Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	ws.SkipAutoCommit = true
	if withFeatureYAML {
		if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: "Test", Status: feature.StatusInProgress}); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	s := protocol.State{FeatureID: featureID, Phase: protocol.PhasePendingReview, Role: protocol.RoleAcceptor, Round: 2, Active: true}
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	return ws
}

// readEvents 讀回 feature 的 events.jsonl 全文，供測試斷言 append 的事件。
func readEvents(t *testing.T, ws *protocol.Workspace, featureID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return string(data)
}

// TestFinalizeDone_SyncFailureNonFatal 驗證 AC-3：SyncFeatureStatus 失敗時 FinalizeDone 仍回 nil，
// 且 state.json 已寫成 done、transition event 已 append。
func TestFinalizeDone_SyncFailureNonFatal(t *testing.T) {
	const id = "feat-nosync"
	ws := newFinalizeWorkspace(t, id, false) // 無 feature YAML → SyncFeatureStatus 必失敗

	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatal(err)
	}

	newState, err := FinalizeDone(ws, id, s)
	if err != nil {
		t.Fatalf("FinalizeDone returned error %v, want nil (sync 失敗應為 non-fatal)", err)
	}
	if newState.Phase != protocol.PhaseDone {
		t.Errorf("returned phase = %q, want done", newState.Phase)
	}

	persisted, err := ws.ReadState(id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase != protocol.PhaseDone {
		t.Errorf("persisted phase = %q, want done", persisted.Phase)
	}
	if persisted.Active {
		t.Errorf("persisted Active = true, want false")
	}
	if persisted.StopReason != "done" {
		t.Errorf("persisted StopReason = %q, want done", persisted.StopReason)
	}

	events := readEvents(t, ws, id)
	if !strings.Contains(events, `"type":"transition"`) || !strings.Contains(events, `"phase":"done"`) {
		t.Errorf("events missing transition→done: %s", events)
	}
}

// TestFinalizeDone_StateFields 驗證 AC-8：FinalizeDone 寫出的 state 欄位（Phase/Active/StopReason/Role）
// 為 done 終態的標準值。CLI（finalizeDone）與 server（transitionDone）皆委派此函式，故兩端輸出必然一致。
func TestFinalizeDone_StateFields(t *testing.T) {
	const id = "feat-ok"
	ws := newFinalizeWorkspace(t, id, true)

	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatal(err)
	}

	newState, err := FinalizeDone(ws, id, s)
	if err != nil {
		t.Fatalf("FinalizeDone: %v", err)
	}

	persisted, err := ws.ReadState(id)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name      string
		got, want any
	}{
		{"returned.Phase", newState.Phase, protocol.PhaseDone},
		{"returned.Active", newState.Active, false},
		{"returned.StopReason", newState.StopReason, "done"},
		{"returned.Role", string(newState.Role), ""},
		{"persisted.Phase", persisted.Phase, protocol.PhaseDone},
		{"persisted.Active", persisted.Active, false},
		{"persisted.StopReason", persisted.StopReason, "done"},
		{"persisted.Role", string(persisted.Role), ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	// feature YAML 狀態已同步為 done（happy path sync 成功）
	f, err := ws.LoadFeature(id)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != feature.StatusDone {
		t.Errorf("feature status = %q, want done", f.Status)
	}
}
