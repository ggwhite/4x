package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// renderDeep 用 loadRoleTemplate 直接 render deep-reviewer 模板，套用給定的 promptOption。
func renderDeep(t *testing.T, role protocol.Role, opts ...promptOption) string {
	t.Helper()
	tmpl, err := loadRoleTemplate(role)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	data := promptData{
		Feature: protocol.Feature{ID: "F062", Name: "Parallel Deep Review"},
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
		withParallelDeepReviewer(1, 3, []int{1, 2, 3, 4}, deepReviewPartialName(1)))

	// 只應出現被指派的 angle 1-4，不應出現 angle 5+。
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
	// 輸出目標應為 partial report，且明確指示不要寫 deep-review-report.md / Verdict。
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
	// 不套用任何 option（AssignedAngles 為空）→ 全 11 angle + 完整 report 格式。
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
	partials := []includeContent{
		{Path: "deep-review-partial-1.md", Content: "PARTIAL-ONE-MARKER critical bug in foo.go:10"},
		{Path: "deep-review-partial-2.md", Content: "PARTIAL-TWO-MARKER suspicious lock in bar.go:42"},
	}
	out := renderDeep(t, protocol.RoleSynthesizer, withSynthesizerReports(partials))

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
