package prompt

import (
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestGenerate_TesterParallelGate 驗證 AC-4：tester.md.tmpl 的 parallel_review_test 段落只在
// Config.ParallelReviewTest==true 時渲染，且引用 state.json 的 parallelReview 標記為判準。
func TestGenerate_TesterParallelGate(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F151-tester-gate"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	onCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{ParallelReviewTest: true}}
	on := renderRole(t, onCtx, protocol.RoleTester)
	if !strings.Contains(on, "parallel_review_test") {
		t.Errorf("tester (parallel on) output missing parallel_review_test section\n---\n%s", on)
	}
	if !strings.Contains(on, "parallelReview") {
		t.Errorf("tester (parallel on) output should reference the parallelReview state.json marker\n---\n%s", on)
	}

	offCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{ParallelReviewTest: false}}
	off := renderRole(t, offCtx, protocol.RoleTester)
	if strings.Contains(off, "parallel_review_test") {
		t.Errorf("tester (parallel off) output should NOT contain parallel_review_test section\n---\n%s", off)
	}
}

// TestGenerate_ReviewerParallelGate 驗證 AC-5：reviewer.md.tmpl 的 parallel 說明段落只在
// Config.ParallelReviewTest==true 時渲染。
func TestGenerate_ReviewerParallelGate(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F151-reviewer-gate"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	onCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{ParallelReviewTest: true}}
	on := renderRole(t, onCtx, protocol.RoleReviewer)
	if !strings.Contains(on, "parallel_review_test") {
		t.Errorf("reviewer (parallel on) output missing parallel_review_test section\n---\n%s", on)
	}
	if !strings.Contains(on, "tester") {
		t.Errorf("reviewer (parallel on) output should mention the concurrent tester\n---\n%s", on)
	}

	offCtx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{ParallelReviewTest: false}}
	off := renderRole(t, offCtx, protocol.RoleReviewer)
	if strings.Contains(off, "parallel_review_test") {
		t.Errorf("reviewer (parallel off) output should NOT contain parallel_review_test section\n---\n%s", off)
	}
}
