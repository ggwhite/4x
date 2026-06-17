package feature

import (
	"strings"
	"testing"
)

func TestFeatureValidate_OK(t *testing.T) {
	f := Feature{
		ID:     "f001-hello",
		Name:   "Hello Feature",
		Status: StatusInProgress,
		Subtasks: []Subtask{
			{ID: "s1", Name: "Sub 1", Status: "done"},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFeatureValidate_EmptyStatus(t *testing.T) {
	f := Feature{ID: "f001-ok", Name: "OK"}
	if err := f.Validate(); err != nil {
		t.Fatalf("empty status should be valid: %v", err)
	}
}

func TestFeatureValidate_MissingID(t *testing.T) {
	f := Feature{Name: "No ID"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFeatureValidate_ValidIDs(t *testing.T) {
	for _, id := range []string{"F001-hello", "F001-Hello", "abc-123"} {
		f := Feature{ID: id, Name: "test"}
		if err := f.Validate(); err != nil {
			t.Errorf("id %q should be valid: %v", id, err)
		}
	}
}

func TestFeatureValidate_InvalidID(t *testing.T) {
	tests := []struct {
		id   string
		desc string
	}{
		{"-leading", "leading dash"},
		{"trailing-", "trailing dash"},
		{"has space", "space"},
	}
	for _, tt := range tests {
		f := Feature{ID: tt.id, Name: "test"}
		if err := f.Validate(); err == nil {
			t.Errorf("id %q (%s) should be invalid", tt.id, tt.desc)
		}
	}
}

func TestFeatureValidate_SingleCharID(t *testing.T) {
	f := Feature{ID: "x", Name: "test"}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("single char id should fail regex (need 2+ chars with no leading/trailing dash): %v", err)
	}
}

func TestFeatureValidate_MissingName(t *testing.T) {
	f := Feature{ID: "f001-ok"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFeatureValidate_InvalidStatus(t *testing.T) {
	f := Feature{ID: "f001-ok", Name: "OK", Status: "wip"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	if !strings.Contains(err.Error(), "wip") {
		t.Fatalf("error should mention invalid value: %v", err)
	}
}

func TestFeatureValidate_SubtaskErrors(t *testing.T) {
	f := Feature{
		ID:   "f001-ok",
		Name: "OK",
		Subtasks: []Subtask{
			{Name: "Missing ID"},
			{ID: "s2", Status: "invalid-status"},
		},
	}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for subtask issues")
	}
	msg := err.Error()
	if !strings.Contains(msg, "subtasks[0].id is required") {
		t.Errorf("should report missing subtask id: %v", err)
	}
	if !strings.Contains(msg, "subtasks[1].name is required") {
		t.Errorf("should report missing subtask name: %v", err)
	}
	if !strings.Contains(msg, "subtasks[1].status") {
		t.Errorf("should report invalid subtask status: %v", err)
	}
}

func TestFeatureValidate_MultipleErrors(t *testing.T) {
	f := Feature{}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "id is required") || !strings.Contains(msg, "name is required") {
		t.Fatalf("should aggregate errors: %v", err)
	}
}

func TestValidateLoose_OK(t *testing.T) {
	f := Feature{
		ID: "f001-ok", Name: "OK", Status: StatusInProgress,
		Subtasks: []Subtask{{ID: "s1", Name: "Sub", Status: "done"}},
	}
	warnings, err := f.ValidateLoose()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestValidateLoose_MissingID_Fatal(t *testing.T) {
	f := Feature{Name: "No ID"}
	_, err := f.ValidateLoose()
	if err == nil {
		t.Fatal("missing id should be fatal")
	}
}

func TestValidateLoose_InvalidSubtaskStatus_Warning(t *testing.T) {
	f := Feature{
		ID: "f001-ok", Name: "OK",
		Subtasks: []Subtask{
			{ID: "s1", Name: "Sub", Status: "pending"},
			{ID: "s2", Name: "Sub2", Status: "deferred-to-other"},
		},
	}
	warnings, err := f.ValidateLoose()
	if err != nil {
		t.Fatalf("invalid subtask status should not be fatal: %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
	for _, w := range warnings {
		if !strings.Contains(w, "not recognized") {
			t.Errorf("warning should mention 'not recognized': %s", w)
		}
	}
}

func TestValidateLoose_InvalidFeatureStatus_Warning(t *testing.T) {
	f := Feature{ID: "f001-ok", Name: "OK", Status: "wip"}
	warnings, err := f.ValidateLoose()
	if err != nil {
		t.Fatalf("invalid feature status should not be fatal: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "wip") {
		t.Fatalf("expected warning about 'wip', got: %v", warnings)
	}
}

func TestValidateLoose_MissingName_Warning(t *testing.T) {
	f := Feature{ID: "f001-ok"}
	warnings, err := f.ValidateLoose()
	if err != nil {
		t.Fatalf("missing name should not be fatal: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "name") {
		t.Fatalf("expected warning about missing name, got: %v", warnings)
	}
}
