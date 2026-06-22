package feature

import "testing"

func TestStatusDraft_IsValidStatus(t *testing.T) {
	if StatusDraft != "draft" {
		t.Errorf("StatusDraft = %q, want %q", StatusDraft, "draft")
	}
}

func TestBatchCompleted_DraftIsFalse(t *testing.T) {
	if BatchCompleted(StatusDraft) {
		t.Error("BatchCompleted(StatusDraft) = true, want false")
	}
}

// draft feature 必須能通過 Validate，否則 SaveFeature/LoadFeature 無法 round-trip，draft 模式失效。
func TestValidate_DraftStatusIsValid(t *testing.T) {
	f := Feature{ID: "F099-draft", Name: "F099: Draft", Status: StatusDraft}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() with draft status = %v, want nil", err)
	}
}
