package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

func renderDeep(t *testing.T, role protocol.Role, opts ...prompt.Option) string {
	t.Helper()
	tmpl, err := prompt.LoadRoleTemplate("", role)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	data := prompt.Data{
		Feature: feature.Feature{ID: "F062", Name: "Parallel Deep Review"},
		Round:   1,
		DotDir:  ".4x",
		Locale:  "zh-TW", LocaleName: "繁體中文",
	}
	for _, opt := range opts {
		opt(&data)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return buf.String()
}

func TestDeepReviewerTemplate_ParallelMode(t *testing.T) {
	out := renderDeep(t, protocol.RoleDeepReviewer,
		prompt.WithParallelDeepReviewer(1, 3, []int{1, 2, 3, 4}, prompt.DeepReviewPartialName(1)))

	for _, a := range []string{"Angle 1 —", "Angle 2 —", "Angle 3 —", "Angle 4 —"} {
		if !strings.Contains(out, a) {
			t.Errorf("missing assigned %q", a)
		}
	}
	for _, a := range []string{"Angle 5 —", "Angle 8 —", "Angle 11 —"} {
		if strings.Contains(out, a) {
			t.Errorf("unexpected unassigned %q in parallel mode", a)
		}
	}
	if !strings.Contains(out, "deep-review-partial-1.md") {
		t.Error("expected partial report filename in output target")
	}
	if !strings.Contains(out, "Deep Review Partial Report") {
		t.Error("expected partial report format header")
	}
	if !strings.Contains(out, "do NOT write deep-review-report.md") {
		t.Error("expected instruction to not write the final report")
	}
}

func TestDeepReviewerTemplate_FallbackMode(t *testing.T) {
	out := renderDeep(t, protocol.RoleDeepReviewer)

	for i := 1; i <= 11; i++ {
		marker := "Angle " + strconv.Itoa(i) + " —"
		if !strings.Contains(out, marker) {
			t.Errorf("fallback mode missing %q", marker)
		}
	}
	if !strings.Contains(out, "deep-review-report.md") {
		t.Error("fallback should target deep-review-report.md")
	}
	if !strings.Contains(out, "## Verdict") {
		t.Error("fallback report format must include a Verdict section")
	}
	if strings.Contains(out, "Deep Review Partial Report") {
		t.Error("fallback must not use the partial report format")
	}
}

func TestSynthesizerTemplate_EmbedsPartials(t *testing.T) {
	partials := []prompt.IncludeContent{
		{Path: "deep-review-partial-1.md", Content: "PARTIAL-ONE-MARKER critical bug in foo.go:10"},
		{Path: "deep-review-partial-2.md", Content: "PARTIAL-TWO-MARKER suspicious lock in bar.go:42"},
	}
	out := renderDeep(t, protocol.RoleSynthesizer, prompt.WithSynthesizerReports(partials))

	if !strings.Contains(out, "PARTIAL-ONE-MARKER") || !strings.Contains(out, "PARTIAL-TWO-MARKER") {
		t.Error("synthesizer prompt must embed full partial report contents")
	}
	if !strings.Contains(out, "deep-review-report.md") {
		t.Error("synthesizer must target deep-review-report.md")
	}
	if !strings.Contains(out, "## Verdict") {
		t.Error("synthesizer report format must include a Verdict section")
	}
}
