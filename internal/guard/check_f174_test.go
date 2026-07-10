package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// initRepoWithBaseline 建立一個真實 git repo 並提交一份 baseline，使後續 git worktree add 有 HEAD 可依附。
func initRepoWithBaseline(t *testing.T, dir string) {
	t.Helper()
	mustMkdirAll(t, dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "README.md"), "# "+filepath.Base(dir)+"\n")
	runGit(t, dir, "add", "README.md")
	gitCommit(t, dir, "baseline")
}

// evalSym 對路徑做 EvalSymlinks 正規化，消除 macOS /var→/private/var 差異後再比較。
func evalSym(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

// setupWorktreeWorkspace 建立 multi-repo 的「部分 checkout worktree」情境：
// main/core、main/api 為各自獨立 git repo；container 為 worktree 容器，只 checkout core（linked worktree），
// api 未 provision 進 container。回傳 main root 與 container root。
func setupWorktreeWorkspace(t *testing.T) (mainRoot, container string) {
	t.Helper()
	mainRoot = t.TempDir()
	container = t.TempDir()

	initRepoWithBaseline(t, filepath.Join(mainRoot, "core"))
	initRepoWithBaseline(t, filepath.Join(mainRoot, "api"))

	// 只把 core checkout 進 container（模擬 SetupWorktree 僅 provision feature.repos）。
	runGit(t, filepath.Join(mainRoot, "core"), "worktree", "add", filepath.Join(container, "core"), "-b", "4x/F174-test")
	return mainRoot, container
}

// --- AC-1: originWorkspaceRoot 以真實 git worktree 推導容器根 ---

func TestF174_OriginWorkspaceRoot(t *testing.T) {
	mainRoot, container := setupWorktreeWorkspace(t)

	base := map[string]string{
		"core": filepath.Join(container, "core"), // 已 checkout，存在
		"api":  filepath.Join(container, "api"),  // 未 provision，不存在
	}

	root, ok := originWorkspaceRoot(container, base)
	if !ok {
		t.Fatalf("originWorkspaceRoot should resolve container root via scoped sub-repo, got ok=false")
	}
	if got, want := evalSym(t, root), evalSym(t, mainRoot); got != want {
		t.Errorf("origin root = %q, want main workspace root %q", got, want)
	}

	// 純 temp 目錄（非 git worktree、無可推導的存在路徑）→ ok=false。
	if _, ok := originWorkspaceRoot(t.TempDir(), map[string]string{"x": filepath.Join(t.TempDir(), "nope")}); ok {
		t.Errorf("originWorkspaceRoot on non-git dir should return ok=false")
	}
}

// --- AC-2: resolveFanoutRepoPaths 三種 repo 狀態 ---

func TestF174_ResolveFanoutRepoPaths(t *testing.T) {
	mainRoot, container := setupWorktreeWorkspace(t)

	cfg := protocol.Config{
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core":  {Path: "core"},  // worktree 內存在
				"api":   {Path: "api"},   // 僅 origin(main) 存在
				"ghost": {Path: "ghost"}, // 兩者皆不存在
			},
		},
	}

	r := CheckResult{Pass: true}
	resolved := resolveFanoutRepoPaths(cfg, container, &r)

	// core：worktree 內存在，以 container 路徑納入。
	if got, ok := resolved["core"]; !ok {
		t.Errorf("core should be resolved from worktree")
	} else if evalSym(t, got) != evalSym(t, filepath.Join(container, "core")) {
		t.Errorf("core resolved to %q, want %q", got, filepath.Join(container, "core"))
	}
	// api：僅 origin 存在，以 main 路徑納入。
	if got, ok := resolved["api"]; !ok {
		t.Errorf("api should be resolved via origin root")
	} else if evalSym(t, got) != evalSym(t, filepath.Join(mainRoot, "api")) {
		t.Errorf("api resolved to %q, want origin %q", got, filepath.Join(mainRoot, "api"))
	}
	// ghost：皆不存在，不納入。
	if _, ok := resolved["ghost"]; ok {
		t.Errorf("ghost should NOT be resolved, got %q", resolved["ghost"])
	}

	// 恰一條「無法驗證」Warn，且指向 ghost。
	warn := findWarn(r.Warns, "無法驗證")
	if warn == "" {
		t.Fatalf("expected an 無法驗證 warn for ghost, got %v", r.Warns)
	}
	for _, sub := range []string{"ghost", "無法驗證", "人工覆核"} {
		if !strings.Contains(warn, sub) {
			t.Errorf("warn missing substring %q: %s", sub, warn)
		}
	}
	if n := countWarns(r.Warns, "無法驗證"); n != 1 {
		t.Errorf("expected exactly 1 無法驗證 warn, got %d (%v)", n, r.Warns)
	}
}

// --- AC-3 / AC-5: 無法定位 origin 時輸出「無法驗證」Warn，取代靜默零命中 ---

func TestF174_UnverifiableWarnReplacesSilentSkip(t *testing.T) {
	root := t.TempDir() // 非 git worktree
	featureID := "F174-unverifiable"

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "f174"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"shared": {Path: "shared"},
				"api":    {Path: "api"}, // 未 provision（目錄不存在）、無法定位 origin
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
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID, Repos: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}

	// shared：真實 git repo + 未提交的 proto 變更，觸發 fan-out 掃描。api 目錄刻意不存在。
	sharedDir := filepath.Join(root, "shared")
	initRepoWithBaseline(t, sharedDir)
	writeFile(t, filepath.Join(sharedDir, "proto", "user.proto"),
		"syntax = \"proto3\";\npackage user;\nmessage User { string id = 1; }\n")

	r := CheckResult{Pass: true}
	checkProtoFanoutScope(ws, featureID, &r)

	// F171 不變式：不阻斷。
	if !r.Pass {
		t.Errorf("gate must not set Pass=false, errors=%v", r.Errors)
	}
	if len(r.Errors) != 0 {
		t.Errorf("gate must not append Errors, got %v", r.Errors)
	}
	// 「無法驗證」Warn 取代原本靜默略過，含 sibling 名稱與人工覆核語意。
	warn := findWarn(r.Warns, "無法驗證")
	if warn == "" {
		t.Fatalf("expected 無法驗證 warn for unprovisioned api, got %v", r.Warns)
	}
	for _, sub := range []string{"api", "無法驗證", "人工覆核"} {
		if !strings.Contains(warn, sub) {
			t.Errorf("warn missing substring %q: %s", sub, warn)
		}
	}
	// 不再對 sibling 產生誤導性的 scope-violation 零命中結果。
	if w := findWarn(r.Warns, "scope violation 建議覆核"); w != "" {
		t.Errorf("should not emit scope-violation warn for unresolvable sibling, got %q", w)
	}
}

// --- AC-4 / AC-5: origin root 定位成功、真實掃到未 provision 的 sibling ---

func TestF174_OriginResolutionEndToEnd(t *testing.T) {
	mainRoot, container := setupWorktreeWorkspace(t)
	featureID := "F174-e2e"

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "f174"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"api":  {Path: "api"},
			},
		},
	}
	if err := protocol.Init(container, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: container}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID, Repos: []string{"core"}}); err != nil {
		t.Fatal(err)
	}

	// worktree 內的 core：新增未提交的 proto 變更（觸發 fan-out）。
	writeFile(t, filepath.Join(container, "core", "proto", "user.proto"),
		"syntax = \"proto3\";\npackage user;\nmessage User { string id = 1; }\n")

	// 未 provision 的 sibling api（只存在於 main working tree）：import 該 proto（去前綴後的 path）。
	writeFile(t, filepath.Join(mainRoot, "api", "client.go"),
		"package api\n\n// generated from proto/user.proto\nfunc New() {}\n")

	r := CheckResult{Pass: true}
	checkProtoFanoutScope(ws, featureID, &r)

	// 經 origin root 解析後掃到 sibling api，產生 F171 的 scope-violation 覆核 Warn。
	warn := findWarn(r.Warns, "scope violation 建議覆核")
	if warn == "" {
		t.Fatalf("expected scope-violation warn for sibling api resolved via origin, warns=%v", r.Warns)
	}
	for _, sub := range []string{"api", "core/proto/user.proto"} {
		if !strings.Contains(warn, sub) {
			t.Errorf("warn missing substring %q: %s", sub, warn)
		}
	}
	// 全部 repo 皆可定位，不應出現「無法驗證」Warn。
	if w := findWarn(r.Warns, "無法驗證"); w != "" {
		t.Errorf("all repos resolvable, should not emit 無法驗證 warn, got %q", w)
	}
	// F171 不變式。
	if !r.Pass {
		t.Errorf("gate must not set Pass=false, errors=%v", r.Errors)
	}
	if len(r.Errors) != 0 {
		t.Errorf("gate must not append Errors, got %v", r.Errors)
	}
}

// countWarns 回傳 warns 中含 sub 子字串的條數。
func countWarns(warns []string, sub string) int {
	n := 0
	for _, w := range warns {
		if strings.Contains(w, sub) {
			n++
		}
	}
	return n
}
