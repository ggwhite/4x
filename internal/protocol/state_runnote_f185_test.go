package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStateRunNoteJSONTag 驗證 AC-1：RunNote 有值時 marshal 含 "runNote" key；
// 零值時因 omitempty 不含該 key。
func TestStateRunNoteJSONTag(t *testing.T) {
	withNote, err := json.Marshal(State{RunNote: "x"})
	if err != nil {
		t.Fatalf("marshal with note: %v", err)
	}
	if !strings.Contains(string(withNote), `"runNote":"x"`) {
		t.Errorf("marshal should contain runNote key: %s", withNote)
	}

	empty, err := json.Marshal(State{})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(empty), "runNote") {
		t.Errorf("empty RunNote should be omitted: %s", empty)
	}
}
