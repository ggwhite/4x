package feature

import (
	"testing"

	"gopkg.in/yaml.v3"
)

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

// TestFeature_Issues_RoundTrip 涵蓋 AC-2：IssueRef 可設定並通過 Validate，
// yaml/json marshal/unmarshal 後欄位不遺失。
func TestFeature_Issues_RoundTrip(t *testing.T) {
	f := Feature{
		ID:     "F127-issue-first",
		Name:   "F127: Issue-first",
		Status: StatusNotStarted,
		Issues: []IssueRef{
			{Repo: ".", ID: "42", URL: "https://github.com/acme/widget/issues/42"},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	data, err := yaml.Marshal(f)
	if err != nil {
		t.Fatalf("yaml.Marshal() = %v", err)
	}
	var loaded Feature
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("yaml.Unmarshal() = %v", err)
	}
	if len(loaded.Issues) != 1 || loaded.Issues[0] != f.Issues[0] {
		t.Errorf("Issues round-trip = %+v, want %+v", loaded.Issues, f.Issues)
	}
}

// TestFeature_Warnings_YAMLRoundTrip 涵蓋 AC-3（D1）：Warnings 改為
// yaml:"warnings,omitempty" 後不再是 no-op，marshal/unmarshal 能取回寫入的內容。
func TestFeature_Warnings_YAMLRoundTrip(t *testing.T) {
	f := Feature{
		ID:       "F127-issue-first",
		Name:     "F127: Issue-first",
		Status:   StatusNotStarted,
		Warnings: []string{"repo old-game-server: preflight failed: gh not authenticated"},
	}

	data, err := yaml.Marshal(f)
	if err != nil {
		t.Fatalf("yaml.Marshal() = %v", err)
	}
	var loaded Feature
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("yaml.Unmarshal() = %v", err)
	}
	if len(loaded.Warnings) != 1 || loaded.Warnings[0] != f.Warnings[0] {
		t.Errorf("Warnings round-trip = %+v, want %+v", loaded.Warnings, f.Warnings)
	}
}
