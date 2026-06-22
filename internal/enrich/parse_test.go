package enrich

import "testing"

func TestParseResponse_ValidJSON(t *testing.T) {
	log := `some preamble
[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "step-a", "name": "Task A", "description": "Do A"},
    {"id": "step-b", "name": "Task B", "description": "Do B"}
  ],
  "repos": ["internal/protocol"],
  "rules": ["no breaking changes"],
  "priority": 2,
  "description": "Enhanced description"
}
[/ENRICHMENT-RESULT]
some epilogue`

	resp, err := parseResponse(log)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if len(resp.Subtasks) != 2 {
		t.Errorf("subtasks count = %d, want 2", len(resp.Subtasks))
	}
	if resp.Priority != 2 {
		t.Errorf("priority = %d, want 2", resp.Priority)
	}
	if resp.Description != "Enhanced description" {
		t.Errorf("description = %q", resp.Description)
	}
}

func TestParseResponse_NoMarker(t *testing.T) {
	_, err := parseResponse("no markers here")
	if err == nil {
		t.Error("expected error for missing marker")
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	log := "[ENRICHMENT-RESULT]\n{invalid json}\n[/ENRICHMENT-RESULT]"
	_, err := parseResponse(log)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidate_Pass(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err != nil {
		t.Errorf("validate() = %v, want nil", err)
	}
}

func TestValidate_InsufficientSubtasks(t *testing.T) {
	resp := &enrichResponse{
		Subtasks:    []enrichSubtask{{ID: "a", Name: "A", Description: "do A"}},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for < 2 subtasks")
	}
}

func TestValidate_EmptySubtaskID(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for empty subtask ID")
	}
}

func TestValidate_EmptyDescription(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Priority: 3,
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for empty description")
	}
}

func TestValidate_ZeroPriority(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for zero priority")
	}
}

func TestValidate_PriorityOutOfRange(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    6,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for priority > 5")
	}
}
