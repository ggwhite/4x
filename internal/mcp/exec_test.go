package mcp

import (
	"encoding/json"
	"testing"
)

func TestBuildArgs_AddsJSON(t *testing.T) {
	args := buildArgs("status")
	if len(args) != 2 || args[0] != "status" || args[1] != "--json" {
		t.Errorf("args = %v, want [status --json]", args)
	}
}

func TestBuildArgs_AlreadyHasJSON(t *testing.T) {
	args := buildArgs("status", "--json")
	if len(args) != 2 || args[0] != "status" || args[1] != "--json" {
		t.Errorf("args = %v, want [status --json]", args)
	}
}

func TestParseOutput_ValidJSON(t *testing.T) {
	raw := `{"features": []}` + "\nsome warning on stderr\n"
	result, err := parseJSONOutput([]byte(raw))
	if err != nil {
		t.Fatalf("parseJSONOutput failed: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	raw := `not json at all`
	_, err := parseJSONOutput([]byte(raw))
	if err == nil {
		t.Fatal("expected error for non-JSON output")
	}
}
