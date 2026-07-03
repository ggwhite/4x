package protocol

import (
	"encoding/json"
	"testing"
)

func TestConfig_IssueTracker_DefaultDisabled(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.IssueTracker.Enabled {
		t.Error("IssueTracker.Enabled zero-value should be false")
	}
}

func TestConfig_IssueTracker_Enabled(t *testing.T) {
	raw := `{"issue_tracker":{"enabled":true}}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.IssueTracker.Enabled {
		t.Error("IssueTracker.Enabled should be true")
	}
}
