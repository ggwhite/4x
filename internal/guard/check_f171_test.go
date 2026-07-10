package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// --- AC-2: isProtoInterfaceDefinition 正反例 ---

func TestF171_IsProtoInterfaceDefinition(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"shared/proto/user.proto", true},  // .proto 一律命中
		{"api.proto", true},                // 根層 .proto
		{"pkg/interface/user.go", true},    // segment == interface
		{"pkg/interfaces/user.ts", true},   // segment == interfaces
		{"src/UserInterface.tsx", true},    // segment 含 interface（子字串）
		{"model/interface.java", true},     // 檔名即 interface
		{"cfg/interfaces.yaml", true},      // interfaces + .yaml
		{"internal/foo/foo.go", false},     // 一般 .go 不命中
		{"cmd/main.go", false},             // 一般 .go 不命中
		{"pkg/interface/readme.md", false}, // 有 interface segment 但副檔名不在白名單
	}

	for _, c := range cases {
		if got := isProtoInterfaceDefinition(c.path); got != c.want {
			t.Errorf("isProtoInterfaceDefinition(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// --- AC-3: importSearchTerms 產生完整 path 與去 repo 前綴後的 path ---

func TestF171_ImportSearchTerms(t *testing.T) {
	terms := importSearchTerms("shared/proto/user.proto")
	want := map[string]bool{
		"shared/proto/user.proto": false,
		"proto/user.proto":        false,
	}
	for _, term := range terms {
		if _, ok := want[term]; !ok {
			t.Errorf("unexpected term %q (terms=%v)", term, terms)
			continue
		}
		want[term] = true
	}
	for term, seen := range want {
		if !seen {
			t.Errorf("missing expected term %q (terms=%v)", term, terms)
		}
	}

	// 無 repo 前綴（單段 path）只產生原始 term。
	if got := importSearchTerms("user.proto"); len(got) != 1 || got[0] != "user.proto" {
		t.Errorf("importSearchTerms(user.proto) = %v, want [user.proto]", got)
	}
}

// --- AC-3: 用 temp 檔案內容證明去前綴後的 term 可命中下游 repo ---

func TestF171_ScanImportingReposMatchesStrippedTerm(t *testing.T) {
	root := t.TempDir()
	sharedDir := filepath.Join(root, "shared")
	apiDir := filepath.Join(root, "api")
	mustMkdirAll(t, filepath.Join(apiDir, "client"))
	mustMkdirAll(t, sharedDir)

	// api 只 import 去前綴後的 path（proto/user.proto），不含完整 shared/ 前綴。
	writeFile(t, filepath.Join(apiDir, "client", "user.go"), "package client\n// import proto/user.proto\n")

	repoPaths := map[string]string{"shared": sharedDir, "api": apiDir}
	terms := importSearchTerms("shared/proto/user.proto")

	got := scanImportingRepos(repoPaths, terms, "shared")
	if _, ok := got["api"]; !ok {
		t.Fatalf("expected api to match stripped term, got %v", got)
	}
	// changedRepo 本身（shared）必須被排除，即使含相同字串。
	writeFile(t, filepath.Join(sharedDir, "self.go"), "// proto/user.proto\n")
	got = scanImportingRepos(repoPaths, terms, "shared")
	if _, ok := got["shared"]; ok {
		t.Errorf("changedRepo shared should be excluded from scan, got %v", got)
	}
}

// --- AC-1 / AC-6: fan-out 缺 repo → 只 Warn、不 Error、Pass 仍 true ---

func TestF171_FanoutWarningNotError(t *testing.T) {
	ws, featureID := setupFanoutWorkspace(t, []string{"shared"})

	r := CheckResult{Pass: true}
	checkProtoFanoutScope(ws, featureID, &r)

	if !r.Pass {
		t.Errorf("fan-out gate must not set Pass=false, got Pass=%v errors=%v", r.Pass, r.Errors)
	}
	if len(r.Errors) != 0 {
		t.Errorf("fan-out gate must not append Errors, got %v", r.Errors)
	}
	warn := findWarn(r.Warns, "scope violation 建議覆核")
	if warn == "" {
		t.Fatalf("expected fan-out warning containing 'scope violation 建議覆核', got %v", r.Warns)
	}
	// AC-6：訊息需含 missing repo 名稱、觸發 path、grep-based 誤判提示。
	for _, sub := range []string{"api", "shared/proto/user.proto", "grep-based", "feature.repos"} {
		if !strings.Contains(warn, sub) {
			t.Errorf("warning missing substring %q: %s", sub, warn)
		}
	}
}

// --- AC-4: feature.Repos 為空時不警告 ---

func TestF171_EmptyReposNoWarn(t *testing.T) {
	ws, featureID := setupFanoutWorkspace(t, nil)

	r := CheckResult{Pass: true}
	checkProtoFanoutScope(ws, featureID, &r)

	if w := findWarn(r.Warns, "scope violation 建議覆核"); w != "" {
		t.Errorf("empty feature.repos must not produce fan-out warning, got %q", w)
	}
}

// --- AC-4: api 已在 feature.Repos 時不警告 ---

func TestF171_ApiInReposNoWarn(t *testing.T) {
	ws, featureID := setupFanoutWorkspace(t, []string{"shared", "api"})

	r := CheckResult{Pass: true}
	checkProtoFanoutScope(ws, featureID, &r)

	if w := findWarn(r.Warns, "scope violation 建議覆核"); w != "" {
		t.Errorf("api already in feature.repos must not warn, got %q", w)
	}
}

// --- AC-5: 真實 temp git repos + protocol.Init + guard.Check 正式路徑整合測試 ---

func TestF171_GuardCheckIntegration(t *testing.T) {
	ws, featureID := setupFanoutWorkspace(t, []string{"shared"})
	writeState(t, ws, featureID, protocol.State{Phase: protocol.PhaseInit})

	// 走 guard.Check 正式呼叫路徑（detector=nil，fan-out gate 不依賴 detector，直接對 repo 跑 git）。
	result := Check(ws, featureID, nil)

	warn := findWarn(result.Warns, "scope violation 建議覆核")
	if warn == "" {
		t.Fatalf("guard.Check should surface fan-out warning, warns=%v", result.Warns)
	}
	if !strings.Contains(warn, "api") || !strings.Contains(warn, "shared/proto/user.proto") {
		t.Errorf("integration warning missing repo/path: %s", warn)
	}
	// fan-out warning 不得阻斷 check：不能有任何 proto-fanout 相關 error。
	for _, e := range result.Errors {
		if strings.Contains(e, "proto-fanout") || strings.Contains(e, "scope violation 建議覆核") {
			t.Errorf("fan-out gate must not produce blocking error: %s", e)
		}
	}
	if !result.Pass {
		t.Errorf("PhaseInit workspace with only fan-out warning should still Pass, errors=%v", result.Errors)
	}
}

// --- helpers ---

// setupFanoutWorkspace 建立一個 multi-repo workspace：shared 為真實 git repo（已 commit
// 基準檔，並有 uncommitted 的 shared/proto/user.proto 變更），api repo 有一個 import
// `proto/user.proto` 字串的檔案。feature YAML 的 repos 由 featureRepos 決定。
func setupFanoutWorkspace(t *testing.T, featureRepos []string) (*protocol.Workspace, string) {
	t.Helper()
	root := t.TempDir()
	featureID := "F171-test"

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "fanout-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"shared": {Path: "shared"},
				"api":    {Path: "api"},
			},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID, Repos: featureRepos}); err != nil {
		t.Fatal(err)
	}

	// shared repo：真實 git init + baseline commit，再新增 uncommitted 的 .proto 變更。
	sharedDir := filepath.Join(root, "shared")
	mustMkdirAll(t, sharedDir)
	runGit(t, sharedDir, "init")
	writeFile(t, filepath.Join(sharedDir, "README.md"), "# shared\n")
	runGit(t, sharedDir, "add", "README.md")
	gitCommit(t, sharedDir, "baseline")
	writeFile(t, filepath.Join(sharedDir, "proto", "user.proto"),
		"syntax = \"proto3\";\npackage user;\nmessage User { string id = 1; }\n")

	// api repo：內含 import 該 proto path 的檔案（用去前綴後的 proto/user.proto）。
	apiDir := filepath.Join(root, "api")
	mustMkdirAll(t, apiDir)
	writeFile(t, filepath.Join(apiDir, "client.go"),
		"package api\n\n// generated from proto/user.proto\nfunc New() {}\n")

	return ws, featureID
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// gitCommit 以顯式 identity 提交，避免 CI 環境未設 user.name/user.email 時失敗。
func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	cmd := exec.Command("git",
		"-c", "user.email=test@4x.local", "-c", "user.name=4x-test",
		"commit", "-m", msg)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in %s: %v\n%s", dir, err, out)
	}
}

func findWarn(warns []string, sub string) string {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return w
		}
	}
	return ""
}
