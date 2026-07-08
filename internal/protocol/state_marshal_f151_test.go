package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStateParallelReviewOmitempty 驗證 AC-1：State.ParallelReview 使用 parallelReview,omitempty
// tag——值為 false 時 JSON 省略該欄位（序列路徑不外漏），值為 true 時輸出 "parallelReview":true。
func TestStateParallelReviewOmitempty(t *testing.T) {
	off := State{FeatureID: "F151", Phase: PhaseReviewing, Round: 1, ParallelReview: false}
	data, err := json.Marshal(off)
	if err != nil {
		t.Fatalf("marshal (false): %v", err)
	}
	if strings.Contains(string(data), "parallelReview") {
		t.Errorf("ParallelReview:false should be omitted, got: %s", data)
	}

	on := State{FeatureID: "F151", Phase: PhaseReviewing, Round: 1, ParallelReview: true}
	data, err = json.Marshal(on)
	if err != nil {
		t.Fatalf("marshal (true): %v", err)
	}
	if !strings.Contains(string(data), `"parallelReview":true`) {
		t.Errorf("ParallelReview:true should serialize to \"parallelReview\":true, got: %s", data)
	}
}
