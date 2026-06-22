package enrich

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestBuildPrompt_ContainsCandidate(t *testing.T) {
	candidate := protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Implement retry for failed API calls",
	}
	ectx := &enrichContext{
		FeatureList:  "- F001: Foo",
		DirTree:      "internal/\ncmd/",
		CodeSnippets: "some code",
	}
	prompt, err := buildPrompt(candidate, ectx)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "Add retry logic") {
		t.Error("prompt missing candidate title")
	}
	if !strings.Contains(prompt, "Implement retry for failed API calls") {
		t.Error("prompt missing candidate description")
	}
	if !strings.Contains(prompt, "F001: Foo") {
		t.Error("prompt missing feature list")
	}
	if !strings.Contains(prompt, "[ENRICHMENT-RESULT]") {
		t.Error("prompt missing enrichment marker instruction")
	}
}
