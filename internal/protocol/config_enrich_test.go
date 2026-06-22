package protocol

import (
	"encoding/json"
	"testing"
)

func TestConfig_EnrichFields_Defaults(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.EnrichDiscoveredFeatures {
		t.Error("EnrichDiscoveredFeatures zero-value should be false")
	}
	if cfg.EnrichAutoApprove {
		t.Error("EnrichAutoApprove zero-value should be false")
	}
}

func TestConfig_EnrichFields_Explicit(t *testing.T) {
	raw := `{"enrich_discovered_features": true, "enrich_auto_approve": true}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.EnrichDiscoveredFeatures {
		t.Error("EnrichDiscoveredFeatures should be true")
	}
	if !cfg.EnrichAutoApprove {
		t.Error("EnrichAutoApprove should be true")
	}
}
