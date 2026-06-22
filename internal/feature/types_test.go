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
