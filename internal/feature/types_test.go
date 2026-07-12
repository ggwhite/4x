package feature

import (
	"strings"
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

// TestFeature_SharedPaths_YAMLRoundTrip 涵蓋 AC-1：YAML 欄位 shared_paths 能
// unmarshal 進 SharedPaths，marshal 回來仍以 shared_paths 為 key（非 sharedPaths）。
func TestFeature_SharedPaths_YAMLRoundTrip(t *testing.T) {
	src := "id: F181-shared\nname: 'F181: Shared'\nstatus: not-started\nshared_paths:\n  - Dockerfile\n  - docker-compose.yml\n"
	var loaded Feature
	if err := yaml.Unmarshal([]byte(src), &loaded); err != nil {
		t.Fatalf("yaml.Unmarshal() = %v", err)
	}
	if len(loaded.SharedPaths) != 2 || loaded.SharedPaths[0] != "Dockerfile" || loaded.SharedPaths[1] != "docker-compose.yml" {
		t.Fatalf("SharedPaths unmarshal = %+v, want [Dockerfile docker-compose.yml]", loaded.SharedPaths)
	}

	data, err := yaml.Marshal(loaded)
	if err != nil {
		t.Fatalf("yaml.Marshal() = %v", err)
	}
	if !strings.Contains(string(data), "shared_paths:") {
		t.Errorf("marshalled YAML missing shared_paths key:\n%s", data)
	}
	if strings.Contains(string(data), "sharedPaths") {
		t.Errorf("marshalled YAML should use shared_paths, not sharedPaths:\n%s", data)
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
