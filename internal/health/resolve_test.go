package health

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveHealthCheck_FeatureOverride(t *testing.T) {
	global := &protocol.HealthCheck{Commands: []string{"make build"}, Timeout: 30}
	feature := &protocol.HealthCheck{Commands: []string{"curl localhost"}, Timeout: 60}

	result := ResolveHealthCheck(global, feature)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if len(result.Commands) != 1 || result.Commands[0] != "curl localhost" {
		t.Errorf("commands = %v, want [curl localhost]", result.Commands)
	}
	if result.Timeout != 60 {
		t.Errorf("timeout = %d, want 60", result.Timeout)
	}
}

func TestResolveHealthCheck_GlobalOnly(t *testing.T) {
	global := &protocol.HealthCheck{Commands: []string{"make build"}, Timeout: 30}

	result := ResolveHealthCheck(global, nil)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Commands[0] != "make build" {
		t.Errorf("commands = %v, want [make build]", result.Commands)
	}
}

func TestResolveHealthCheck_NoneConfigured(t *testing.T) {
	if result := ResolveHealthCheck(nil, nil); result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestResolveHealthCheck_FeatureOnly(t *testing.T) {
	feature := &protocol.HealthCheck{Commands: []string{"check-api"}}

	result := ResolveHealthCheck(nil, feature)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Commands[0] != "check-api" {
		t.Errorf("commands = %v, want [check-api]", result.Commands)
	}
}
