package prompt

import (
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestGenerate_CoderSharedPaths 驗證 F181 AC-4：coder prompt 在 Feature.SharedPaths 非空時
// 渲染「Shared Root-Level Paths (allowed to modify)」區塊並逐項列出宣告路徑；為空時不渲染。
func TestGenerate_CoderSharedPaths(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "f181"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws := &protocol.Workspace{Root: root}

	// 非空 SharedPaths：應渲染區塊與各路徑
	ctxWith := &Context{
		Ws: ws, RunnerWs: ws, Cfg: cfg,
		Feature: feat.Feature{ID: "F181-x", Name: "F181 test", SharedPaths: []string{"Dockerfile", "docker-compose.yml"}},
	}
	out, err := Generate(ctxWith, protocol.RoleCoder, 1, 1, "claude")
	if err != nil {
		t.Fatalf("Generate (with shared paths): %v", err)
	}
	if !strings.Contains(out, "== Shared Root-Level Paths (allowed to modify) ==") {
		t.Error("coder prompt with SharedPaths should render the Shared Root-Level Paths section")
	}
	if !strings.Contains(out, "Dockerfile") || !strings.Contains(out, "docker-compose.yml") {
		t.Errorf("coder prompt should list each declared shared path, got:\n%s", out)
	}

	// 空 SharedPaths：不應渲染區塊
	ctxNone := &Context{
		Ws: ws, RunnerWs: ws, Cfg: cfg,
		Feature: feat.Feature{ID: "F181-y", Name: "F181 empty"},
	}
	outNone, err := Generate(ctxNone, protocol.RoleCoder, 1, 1, "claude")
	if err != nil {
		t.Fatalf("Generate (no shared paths): %v", err)
	}
	if strings.Contains(outNone, "== Shared Root-Level Paths (allowed to modify) ==") {
		t.Error("coder prompt without SharedPaths must NOT render the Shared Root-Level Paths section")
	}
}
