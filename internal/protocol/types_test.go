package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestState_SubPhaseRoundTrip 驗證 State.SubPhase 的 JSON marshal/unmarshal round-trip：
// 有值時保留、空字串因 omitempty 不出現在輸出。
func TestState_SubPhaseRoundTrip(t *testing.T) {
	s := State{
		FeatureID: "F079",
		Phase:     PhaseDeepReviewing,
		Role:      RoleMiniCoder,
		SubPhase:  SubPhaseFixing,
		Round:     2,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"subPhase":"fixing"`) {
		t.Errorf("marshal output missing subPhase: %s", data)
	}

	var got State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SubPhase != SubPhaseFixing {
		t.Errorf("SubPhase = %q, want %q", got.SubPhase, SubPhaseFixing)
	}
}

// TestState_SubPhaseOmitEmpty 驗證空 SubPhase 因 omitempty 不出現在 JSON，避免非 deep-review
// 的 state.json 多帶一個空欄位。
func TestState_SubPhaseOmitEmpty(t *testing.T) {
	s := State{FeatureID: "F079", Phase: PhaseCoding, Role: RoleCoder}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "subPhase") {
		t.Errorf("empty SubPhase should be omitted, got: %s", data)
	}
}
