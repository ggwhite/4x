package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestCheckScope_RootLevelFileNotFlagged 驗證 F081 Task 3：
// 根目錄檔案（go.mod、README.md 等，路徑無 "/"）不是 repo，
// 不可被 checkScope 誤判為 "repo not in feature repos"（false positive）。
func TestCheckScope_RootLevelFileNotFlagged(t *testing.T) {
	ws, ops := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	wtPath, err := ops.SetupWorktree("feat-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree：scope 內變更（core）+ 根目錄新檔（untracked，路徑無 "/"）。
	writeFile(t, filepath.Join(wtPath, "core", "a.go"), "package core\n// scoped\n")
	writeFile(t, filepath.Join(wtPath, "README.md"), "# root file\n")

	result := Check(ws, "feat-scope", ops)
	for _, e := range result.Errors {
		if strings.Contains(e, "scope violation") {
			t.Errorf("root-level file must not trigger scope violation: %q", e)
		}
	}
	if !result.Pass {
		t.Errorf("guard should pass, got errors: %v", result.Errors)
	}
}

// TestCheckScope_UntrackedOutOfScopeCaught 驗證 F081 Task 1 透過 detector 生效：
// worktree 內在 scope 外 repo 新增整個 untracked 檔案，必須被攔下，不可靜默繞過。
func TestCheckScope_UntrackedOutOfScopeCaught(t *testing.T) {
	ws, ops := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	wtPath, err := ops.SetupWorktree("feat-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// scope 外 repo（lib）的全新檔案，尚未 git add（untracked）。
	writeFile(t, filepath.Join(wtPath, "lib", "sneaky.go"), "package lib\n// out of scope, untracked\n")

	result := Check(ws, "feat-scope", ops)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "lib") {
			found = true
		}
	}
	if !found {
		t.Errorf("untracked out-of-scope file should be flagged, got: %v", result.Errors)
	}
}

// TestCheckScope_YamlLoadFailFailsClosed 驗證 F081 Task 4：
// feature YAML 無法載入時必須 fail-closed（Pass=false 並回報錯誤），
// 不可只 warn 然後跳過 scope 驗證（等於關閉 scope guard）。
func TestCheckScope_YamlLoadFailFailsClosed(t *testing.T) {
	ws, _ := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	// 寫入壞掉的 YAML 讓 LoadFeature 失敗。
	yamlPath := filepath.Join(ws.DotDir(), protocol.FeaturesDir, "feat-scope.yaml")
	if err := os.WriteFile(yamlPath, []byte(":\n  not: [valid yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := CheckResult{Pass: true}
	checkScope(ws, "feat-scope", nil, &r)

	if r.Pass {
		t.Error("checkScope must fail-closed when feature YAML cannot be loaded")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "cannot load feature YAML for scope check") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope-check load error, got: %v", r.Errors)
	}
}

// TestNeedsDesignOutputs_DeepReviewing 驗證 F081 Task 5：
// deep-reviewing 階段須要求 task-brief / acceptance-criteria 存在，
// 否則必要產出物檢查在此階段出現缺口。
func TestNeedsDesignOutputs_DeepReviewing(t *testing.T) {
	ws, _ := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	// 切到 deep-reviewing 階段並移除 designer 產出物。
	writeState(t, ws, "feat-scope", protocol.State{
		FeatureID: "feat-scope", Phase: protocol.PhaseDeepReviewing, Round: 1,
	})
	featureDir := ws.FeatureDir("feat-scope")
	os.Remove(filepath.Join(featureDir, protocol.TaskBrief))
	os.Remove(filepath.Join(featureDir, protocol.Criteria))

	r := CheckResult{Pass: true}
	checkRequiredFiles(ws, "feat-scope", &r)

	if r.Pass {
		t.Error("deep-reviewing must require designer outputs, got Pass=true")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, protocol.TaskBrief) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing task-brief error in deep-reviewing, got: %v", r.Errors)
	}
}
