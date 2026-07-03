package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTotalCost_SumsRunEndEvents 驗證 TotalCost 只加總多筆 run-end 事件的 cost_usd，
// 非 run-end 事件（如 phase-start／transition）不計入。
func TestTotalCost_SumsRunEndEvents(t *testing.T) {
	ws := setupWorkspace(t)
	featureID := "feat-cost"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	if err := ws.AppendEvent(featureID, Event{Type: "phase-start", CostUSD: 999}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := ws.AppendEvent(featureID, Event{Type: "run-end", CostUSD: 1.5}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := ws.AppendEvent(featureID, Event{Type: "transition", CostUSD: 999}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := ws.AppendEvent(featureID, Event{Type: "run-end", CostUSD: 2.25}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	got, err := ws.TotalCost(featureID)
	if err != nil {
		t.Fatalf("TotalCost error: %v", err)
	}
	if want := 3.75; got != want {
		t.Errorf("TotalCost = %v, want %v", got, want)
	}
}

// TestTotalCost_NoEventsFile 驗證 events.jsonl 不存在時回傳 (0, nil)，不視為錯誤。
func TestTotalCost_NoEventsFile(t *testing.T) {
	ws := setupWorkspace(t)

	got, err := ws.TotalCost("feat-missing")
	if err != nil {
		t.Fatalf("TotalCost error: %v", err)
	}
	if got != 0 {
		t.Errorf("TotalCost = %v, want 0", got)
	}
}

// TestTotalCost_SkipsMalformedLines 驗證壞掉的 JSON 行被跳過，不中斷其餘合法行的加總。
func TestTotalCost_SkipsMalformedLines(t *testing.T) {
	ws := setupWorkspace(t)
	featureID := "feat-malformed"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ws.AppendEvent(featureID, Event{Type: "run-end", CostUSD: 1}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	eventsPath := filepath.Join(ws.FeatureDir(featureID), EventsFile)
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	if _, err := f.WriteString("{not valid json\n"); err != nil {
		t.Fatalf("write malformed line: %v", err)
	}
	f.Close()

	if err := ws.AppendEvent(featureID, Event{Type: "run-end", CostUSD: 2}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	got, err := ws.TotalCost(featureID)
	if err != nil {
		t.Fatalf("TotalCost error: %v", err)
	}
	if want := 3.0; got != want {
		t.Errorf("TotalCost = %v, want %v", got, want)
	}
}

// TestTotalCost_ReadErrorNotIsNotExist 驗證讀檔遇到非 IsNotExist 的錯誤時回傳非 nil error。
func TestTotalCost_ReadErrorNotIsNotExist(t *testing.T) {
	ws := setupWorkspace(t)
	featureID := "feat-direrr"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	// 讓 events.jsonl 這個路徑實際上是個目錄，os.ReadFile 會回傳非 IsNotExist 的錯誤。
	eventsPath := filepath.Join(ws.FeatureDir(featureID), EventsFile)
	if err := os.Mkdir(eventsPath, 0o755); err != nil {
		t.Fatalf("mkdir events path: %v", err)
	}

	_, err := ws.TotalCost(featureID)
	if err == nil {
		t.Fatal("TotalCost error = nil, want non-nil")
	}
	if os.IsNotExist(err) {
		t.Errorf("TotalCost error = %v, want non-IsNotExist error", err)
	}
}
