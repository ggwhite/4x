package prompt

import (
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestGenerate_MiniCoderConditionalSource 驗證 F144 AC-8：未傳 WithConditionalSource 時
// mini-coder prompt 仍讀 deep-review-report.md（deep-reviewing 路徑不變）；傳
// WithConditionalSource(protocol.ReviewReport) 時改讀 review-report.md。兩者皆保留 scope
// 守衛句 "Fix ONLY the issues"。
func TestGenerate_MiniCoderConditionalSource(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "f144"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws := &protocol.Workspace{Root: root}
	ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: "F144-x", Name: "F144 test"}, Cfg: cfg}

	deep, err := Generate(ctx, protocol.RoleMiniCoder, 1, 1, "claude")
	if err != nil {
		t.Fatalf("Generate (default): %v", err)
	}
	if !strings.Contains(deep, "deep-review-report.md") {
		t.Error("default mini-coder prompt should reference deep-review-report.md")
	}
	if !strings.Contains(deep, "Fix ONLY the issues") {
		t.Error("default mini-coder prompt must keep the scope guard 'Fix ONLY the issues'")
	}

	rev, err := Generate(ctx, protocol.RoleMiniCoder, 1, 1, "claude", WithConditionalSource(protocol.ReviewReport))
	if err != nil {
		t.Fatalf("Generate (review source): %v", err)
	}
	if strings.Contains(rev, "deep-review-report.md") {
		t.Error("reviewing mini-coder prompt must NOT reference deep-review-report.md")
	}
	if !strings.Contains(rev, "review-report.md") {
		t.Error("reviewing mini-coder prompt should reference review-report.md")
	}
	if !strings.Contains(rev, "Fix ONLY the issues") {
		t.Error("reviewing mini-coder prompt must keep the scope guard 'Fix ONLY the issues'")
	}
}
