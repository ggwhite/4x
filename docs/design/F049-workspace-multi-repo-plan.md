# F049: Workspace Multi-Repo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support multi-repo workspaces where each sub-directory is an independent git repo, enabling per-repo worktree isolation, commit, merge, and scope checking.

**Architecture:** New `internal/gitops` package with `Ops` interface; `monoRepo` wraps existing logic, `multiRepo` adds per-repo git operations. Factory function selects implementation based on `workspace.repos` in settings.json. All CLI callers (`run.go`, `batch.go`, `done.go`, `merge.go`, `check.go`) delegate to `gitops.Ops` instead of calling git directly.

**Tech Stack:** Go 1.26+, Cobra CLI, standard `os/exec` for git commands

---

### Task 1: Config Model — WorkspaceConfig + RepoConfig

**Files:**
- Modify: `internal/protocol/types.go:240-252` (Config struct)
- Test: `internal/protocol/model_test.go` (existing)

- [ ] **Step 1: Write test for WorkspaceConfig JSON round-trip**

In `internal/protocol/model_test.go`, add:

```go
func TestConfig_WorkspaceRoundTrip(t *testing.T) {
	cfg := Config{
		Project: ProjectConfig{Name: "test"},
		Workspace: WorkspaceConfig{
			Repos: map[string]RepoConfig{
				"core": {Path: "core", Hub: true},
				"gate": {Path: "services/gate"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Workspace.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(got.Workspace.Repos))
	}
	if !got.Workspace.Repos["core"].Hub {
		t.Error("core should be hub")
	}
	if got.Workspace.Repos["gate"].Path != "services/gate" {
		t.Errorf("gate path = %q, want services/gate", got.Workspace.Repos["gate"].Path)
	}
}

func TestConfig_WorkspaceEmpty(t *testing.T) {
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "workspace") {
		t.Error("empty workspace should be omitted from JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocol/ -run TestConfig_Workspace -v`
Expected: FAIL — `WorkspaceConfig` and `RepoConfig` types don't exist yet

- [ ] **Step 3: Add WorkspaceConfig and RepoConfig types**

In `internal/protocol/types.go`, add before the `Config` struct (around line 238):

```go
// WorkspaceConfig 描述 multi-repo workspace 的 repo 映射。
// 沒有設定時代表 monorepo 模式。
type WorkspaceConfig struct {
	Repos map[string]RepoConfig `json:"repos,omitempty"`
}

// RepoConfig 描述 workspace 中單一 repo 的設定。
type RepoConfig struct {
	Path string `json:"path"`
	Hub  bool   `json:"hub,omitempty"`
}
```

Add `Workspace` field to the `Config` struct:

```go
type Config struct {
	Project           ProjectConfig                `json:"project"`
	Runners           map[string]RunnerConfig      `json:"runners"`
	Default           string                       `json:"default_runner"`
	Roles             map[string]RoleConfig        `json:"roles,omitempty"`
	Rules             []string                     `json:"rules,omitempty"`
	HubRepos          []string                     `json:"hub_repos,omitempty"`
	Isolation         string                       `json:"isolation,omitempty"`
	MaxConcurrentRuns int                          `json:"max_concurrent_runs,omitempty"`
	Commit            string                       `json:"commit,omitempty"`
	ModelTiers        map[string]map[string]string `json:"model_tiers,omitempty"`
	Workspace         WorkspaceConfig              `json:"workspace,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocol/ -run TestConfig_Workspace -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/types.go internal/protocol/model_test.go
git commit -m "feat(F049): add WorkspaceConfig and RepoConfig types"
```

---

### Task 2: Feature.Repos schema change — map[string]string → []string

**Files:**
- Modify: `internal/protocol/types.go:74-87` (Feature struct)
- Modify: `internal/protocol/feature.go` (if any Repos-specific logic)
- Modify: `internal/protocol/feature_test.go`
- Modify: `internal/batch/group.go:302-321` (mergeBySharedRepos)
- Modify: `internal/batch/group_test.go` (all test fixtures)
- Modify: `.4x/features/*.yaml` (all feature files with repos field)

- [ ] **Step 1: Write test for Feature YAML with []string repos**

In `internal/protocol/feature_test.go`, add:

```go
func TestFeature_ReposSlice(t *testing.T) {
	yamlData := `
id: test-feat
name: Test
repos:
  - repo-a
  - repo-b
`
	var f Feature
	if err := yaml.Unmarshal([]byte(yamlData), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(f.Repos))
	}
	if f.Repos[0] != "repo-a" || f.Repos[1] != "repo-b" {
		t.Errorf("repos = %v, want [repo-a repo-b]", f.Repos)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocol/ -run TestFeature_ReposSlice -v`
Expected: FAIL — `Repos` is still `map[string]string`, can't unmarshal `[]string`

- [ ] **Step 3: Change Feature.Repos type**

In `internal/protocol/types.go`, change line 81:

```go
// Before
Repos       map[string]string `yaml:"repos,omitempty" json:"repos,omitempty"`

// After
Repos       []string          `yaml:"repos,omitempty" json:"repos,omitempty"`
```

- [ ] **Step 4: Fix compilation errors**

The type change will break these files. Fix each one:

**`internal/batch/group.go:302-321`** — `mergeBySharedRepos`:

```go
func mergeBySharedRepos(features []protocol.Feature, hubRepos []string, uf *unionFind) {
	hubSet := make(map[string]bool)
	for _, r := range hubRepos {
		hubSet[r] = true
	}
	for i := 0; i < len(features); i++ {
		for j := i + 1; j < len(features); j++ {
			for _, r := range features[i].Repos {
				if hubSet[r] {
					continue
				}
				for _, r2 := range features[j].Repos {
					if r == r2 {
						uf.union(i, j)
						goto nextPair
					}
				}
			}
		nextPair:
		}
	}
}
```

**`internal/guard/check.go:200-224`** — `checkScope`:

```go
func checkScope(ws *protocol.Workspace, featureID string, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot load feature YAML: %v", err))
		return
	}

	if len(feature.Repos) == 0 {
		return
	}

	allowedRepos := make(map[string]bool)
	for _, repo := range feature.Repos {
		allowedRepos[repo] = true
	}

	changedRepos := detectChangedRepos(ws.Root)
	for _, repo := range changedRepos {
		if !allowedRepos[repo] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("scope violation: repo %q not in feature repos", repo))
		}
	}
}
```

**`cmd/4x/run.go:736-744`** — `repoPathsFromFeature` (temporary, will be replaced by gitops later):

```go
func repoPathsFromFeature(f protocol.Feature) []string {
	if len(f.Repos) == 0 {
		return []string{"."}
	}
	return f.Repos
}
```

- [ ] **Step 5: Update batch test fixtures**

In `internal/batch/group_test.go`, change all `Repos: map[string]string{...}` to `Repos: []string{...}`:

```go
// TestPlanBatch_IndependentFeatures (line 30-32)
{ID: "a", Repos: []string{"repo-1"}},
{ID: "b", Repos: []string{"repo-2"}},
{ID: "c", Repos: []string{"repo-3"}},

// TestPlanBatch_SharedRepoMergesClusters (line 44-46)
{ID: "a", Repos: []string{"shared"}},
{ID: "b", Repos: []string{"shared"}},
{ID: "c", Repos: []string{"other"}},

// TestPlanBatch_HubRepoNotMerged (line 59-60)
{ID: "a", Repos: []string{"hub-repo"}},
{ID: "b", Repos: []string{"hub-repo"}},

// TestPlanBatch_DependencyMergesClusters (line 73-74)
{ID: "auth", Repos: []string{"repo-1"}},
{ID: "api", Repos: []string{"repo-2"}, Depends: []string{"auth"}},
```

- [ ] **Step 6: Update existing feature YAML files**

Scan `.4x/features/*.yaml` for any file containing `repos:` as a map and convert to list format. Most features in this repo don't use repos (it's a monorepo), so this may be a no-op.

Run: `grep -l "repos:" .4x/features/*.yaml` to find affected files.

- [ ] **Step 7: Run all tests**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/protocol/types.go internal/protocol/feature_test.go internal/batch/group.go internal/batch/group_test.go internal/guard/check.go cmd/4x/run.go
git commit -m "feat(F049): change Feature.Repos from map[string]string to []string"
```

---

### Task 3: Path resolution helpers

**Files:**
- Modify: `internal/protocol/types.go` (add helper functions)
- Test: `internal/protocol/model_test.go`

- [ ] **Step 1: Write tests for path resolution**

In `internal/protocol/model_test.go`, add:

```go
func TestResolveRepoPaths_MultiRepo(t *testing.T) {
	cfg := Config{
		Workspace: WorkspaceConfig{
			Repos: map[string]RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "services/gate"},
			},
		},
	}
	paths := ResolveRepoPaths(cfg, "/workspace")
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	if paths["core"] != "/workspace/core" {
		t.Errorf("core = %q, want /workspace/core", paths["core"])
	}
	if paths["gate"] != "/workspace/services/gate" {
		t.Errorf("gate = %q, want /workspace/services/gate", paths["gate"])
	}
}

func TestResolveRepoPaths_Monorepo(t *testing.T) {
	cfg := Config{}
	paths := ResolveRepoPaths(cfg, "/workspace")
	if len(paths) != 1 {
		t.Fatalf("paths = %d, want 1", len(paths))
	}
	if paths["."] != "/workspace" {
		t.Errorf(". = %q, want /workspace", paths["."])
	}
}

func TestResolveFeatureRepoPaths_Scoped(t *testing.T) {
	cfg := Config{
		Workspace: WorkspaceConfig{
			Repos: map[string]RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
				"admin": {Path: "admin"},
			},
		},
	}
	f := Feature{Repos: []string{"core", "gate"}}
	paths := ResolveFeatureRepoPaths(f, cfg, "/ws")
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	if _, ok := paths["admin"]; ok {
		t.Error("admin should not be included")
	}
}

func TestResolveFeatureRepoPaths_Empty(t *testing.T) {
	cfg := Config{
		Workspace: WorkspaceConfig{
			Repos: map[string]RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
	}
	f := Feature{}
	paths := ResolveFeatureRepoPaths(f, cfg, "/ws")
	if len(paths) != 2 {
		t.Fatalf("empty repos should return all workspace repos, got %d", len(paths))
	}
}

func TestEffectiveHubRepos(t *testing.T) {
	cfg := Config{
		HubRepos: []string{"shared-lib"},
		Workspace: WorkspaceConfig{
			Repos: map[string]RepoConfig{
				"core": {Path: "core", Hub: true},
				"gate": {Path: "gate"},
			},
		},
	}
	hubs := EffectiveHubRepos(cfg)
	hubSet := make(map[string]bool)
	for _, h := range hubs {
		hubSet[h] = true
	}
	if !hubSet["shared-lib"] {
		t.Error("shared-lib from HubRepos should be included")
	}
	if !hubSet["core"] {
		t.Error("core with Hub:true should be included")
	}
	if hubSet["gate"] {
		t.Error("gate should not be hub")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/protocol/ -run "TestResolve|TestEffective" -v`
Expected: FAIL — functions don't exist

- [ ] **Step 3: Implement path resolution helpers**

In `internal/protocol/types.go`, add after `BoolVal`:

```go
// ResolveRepoPaths 從 workspace config 解析 repo name → absolute path。
// monorepo 模式回傳 {"." : root}。
func ResolveRepoPaths(cfg Config, root string) map[string]string {
	if len(cfg.Workspace.Repos) == 0 {
		return map[string]string{".": root}
	}
	paths := make(map[string]string, len(cfg.Workspace.Repos))
	for name, rc := range cfg.Workspace.Repos {
		paths[name] = filepath.Join(root, rc.Path)
	}
	return paths
}

// ResolveFeatureRepoPaths 解析 feature 涉及的 repo name → absolute path。
// feature.Repos 為空時：multi-repo 回傳所有 workspace repos，monorepo 回傳 {".": root}。
func ResolveFeatureRepoPaths(f Feature, cfg Config, root string) map[string]string {
	all := ResolveRepoPaths(cfg, root)
	if len(f.Repos) == 0 {
		return all
	}
	result := make(map[string]string, len(f.Repos))
	for _, name := range f.Repos {
		if p, ok := all[name]; ok {
			result[name] = p
		}
	}
	return result
}

// EffectiveHubRepos 合併 Config.HubRepos 與 workspace config 中 Hub: true 的 repo。
func EffectiveHubRepos(cfg Config) []string {
	seen := make(map[string]bool)
	var hubs []string
	for _, h := range cfg.HubRepos {
		if !seen[h] {
			seen[h] = true
			hubs = append(hubs, h)
		}
	}
	for name, rc := range cfg.Workspace.Repos {
		if rc.Hub && !seen[name] {
			seen[name] = true
			hubs = append(hubs, name)
		}
	}
	return hubs
}
```

Add `"path/filepath"` to the import block if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/protocol/ -run "TestResolve|TestEffective" -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/protocol/model_test.go
git commit -m "feat(F049): add ResolveRepoPaths, ResolveFeatureRepoPaths, EffectiveHubRepos"
```

---

### Task 4: gitops interface + MergeResult + shared helpers

**Files:**
- Create: `internal/gitops/gitops.go`

- [ ] **Step 1: Create the gitops package with interface and shared types**

```go
package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// Ops 封裝所有 git 操作，根據 workspace config 決定 monorepo 或 multi-repo 模式。
type Ops interface {
	SetupWorktree(featureID string) (wtRoot string, err error)
	Commit(wtRoot, featureID, featureName string, round int) error
	Merge(featureID, featureName string) MergeResult
	Cleanup(featureID string) error
	DetectChangedRepos() []string
	CaptureBaseline(featureID string, featureRepos []string) error
	IsMultiRepo() bool
}

// MergeResult 描述 Merge 操作的結果。
type MergeResult struct {
	Skipped      bool
	Conflict     bool
	Error        string
	Files        []string
	ConflictRepo string
}

// New 根據 workspace config 建立對應的 Ops 實作。
func New(root string, ws *protocol.Workspace, cfg protocol.Config) Ops {
	if len(cfg.Workspace.Repos) > 0 {
		return &multiRepo{root: root, ws: ws, cfg: cfg}
	}
	return &monoRepo{root: root, ws: ws}
}

// Dir 回傳 worktree 組合目錄的路徑（兩種模式共用）。
func Dir(root, featureID string) string {
	return filepath.Join(root, ".worktrees", "4x", featureID)
}

// Branch 回傳 feature 對應的 branch 名稱（兩種模式共用）。
func Branch(featureID string) string {
	return "4x/" + featureID
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func ensureGitignore(root, entry string) {
	path := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), entry) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}

func copyFileIfExists(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, data, 0o644)
}

func conflictFiles(root string) []string {
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/gitops/`
Expected: PASS (multiRepo and monoRepo structs referenced but not defined yet — add stubs)

Create stub files so it compiles:

`internal/gitops/monorepo.go`:
```go
package gitops

import "github.com/ggwhite/4x/internal/protocol"

type monoRepo struct {
	root string
	ws   *protocol.Workspace
}
```

`internal/gitops/multirepo.go`:
```go
package gitops

import "github.com/ggwhite/4x/internal/protocol"

type multiRepo struct {
	root string
	ws   *protocol.Workspace
	cfg  protocol.Config
}
```

- [ ] **Step 3: Run build**

Run: `go build ./internal/gitops/`
Expected: PASS (stubs don't implement Ops yet, but New() references are forward-declared)

- [ ] **Step 4: Commit**

```bash
git add internal/gitops/
git commit -m "feat(F049): add gitops package with Ops interface and shared helpers"
```

---

### Task 5: monoRepo implementation

**Files:**
- Modify: `internal/gitops/monorepo.go`
- Create: `internal/gitops/monorepo_test.go`

This task moves existing logic from `run.go`, `worktree/merge.go`, and `guard/check.go` into the monoRepo struct. The code is already tested indirectly; here we add direct unit tests for the interface methods.

- [ ] **Step 1: Write tests for monoRepo**

Create `internal/gitops/monorepo_test.go`:

```go
package gitops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
}

func setupMonoWorkspace(t *testing.T) (*protocol.Workspace, Ops) {
	t.Helper()
	root := t.TempDir()
	initGitRepo(t, root)

	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	ops := New(root, ws, cfg)
	return ws, ops
}

func TestMonoRepo_IsMultiRepo(t *testing.T) {
	_, ops := setupMonoWorkspace(t)
	if ops.IsMultiRepo() {
		t.Error("monoRepo should not be multi-repo")
	}
}

func TestMonoRepo_SetupWorktree(t *testing.T) {
	ws, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	expected := Dir(ws.Root, "feat-1")
	if wtPath != expected {
		t.Errorf("wtPath = %q, want %q", wtPath, expected)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, protocol.DirName, protocol.ConfigFile)); err != nil {
		t.Error(".4x/settings.json should be copied to worktree")
	}
}

func TestMonoRepo_CommitAndMerge(t *testing.T) {
	_, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package new"), 0o644)

	if err := ops.Commit(wtPath, "feat-1", "Test Feature", 1); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge("feat-1", "Test Feature")
	if result.Conflict || result.Error != "" {
		t.Fatalf("Merge failed: conflict=%v error=%q", result.Conflict, result.Error)
	}
}

func TestMonoRepo_CaptureBaseline(t *testing.T) {
	ws, ops := setupMonoWorkspace(t)
	ws.InitFeatureDir("feat-1")

	if err := ops.CaptureBaseline("feat-1", nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(baseline.Repos))
	}
	if baseline.Repos[0].Head == "" {
		t.Error("HEAD should not be empty")
	}
}

func TestMonoRepo_Cleanup(t *testing.T) {
	ws, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.Cleanup("feat-1"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed")
	}
	_ = ws
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gitops/ -run TestMonoRepo -v`
Expected: FAIL — monoRepo methods not implemented

- [ ] **Step 3: Implement monoRepo**

Replace `internal/gitops/monorepo.go` with the full implementation. Move code from:
- `cmd/4x/run.go:setupWorktree` (lines 775-800)
- `cmd/4x/run.go:ensureWorktreeDotDir` (lines 804-834)
- `cmd/4x/run.go:commitWorktree` (lines 916-919)
- `internal/worktree/merge.go:Merge` (lines 25-55)
- `internal/worktree/merge.go:Cleanup` (lines 59-72)
- `internal/guard/check.go:detectChangedRepos` (lines 227-251)
- `internal/guard/check.go:CaptureBaseline` (lines 253-289)

```go
package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

type monoRepo struct {
	root string
	ws   *protocol.Workspace
}

func (m *monoRepo) IsMultiRepo() bool { return false }

func (m *monoRepo) SetupWorktree(featureID string) (string, error) {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	ensureGitignore(m.root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		m.ensureDotDir(wtDir)
		return wtDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", err
	}

	out, err := exec.Command("git", "-C", m.root, "worktree", "add", wtDir, "-b", branch).CombinedOutput()
	if err != nil {
		out2, err2 := exec.Command("git", "-C", m.root, "worktree", "add", wtDir, branch).CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("git worktree add: %s\n%s", string(out), string(out2))
		}
	}

	m.ensureDotDir(wtDir)
	return wtDir, nil
}

func (m *monoRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)

	if info, err := os.Lstat(dotDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			os.Remove(dotDir)
		}
	}

	os.MkdirAll(dotDir, 0o755)

	src := filepath.Join(m.root, protocol.DirName, protocol.ConfigFile)
	dst := filepath.Join(dotDir, protocol.ConfigFile)
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(dst, data, 0o644)
	}

	srcPlugins := filepath.Join(m.root, protocol.DirName, "plugins")
	dstPlugins := filepath.Join(dotDir, "plugins")
	if entries, err := os.ReadDir(srcPlugins); err == nil {
		os.MkdirAll(dstPlugins, 0o755)
		for _, e := range entries {
			if !e.IsDir() {
				copyFileIfExists(filepath.Join(srcPlugins, e.Name()), filepath.Join(dstPlugins, e.Name()))
			}
		}
	}
}

func (m *monoRepo) Commit(wtPath, featureID, featureName string, round int) error {
	if out, err := exec.Command("git", "-C", wtPath, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", string(out), err)
	}

	if exec.Command("git", "-C", wtPath, "diff", "--cached", "--quiet").Run() == nil {
		return nil
	}

	var msg string
	if round > 0 {
		msg = fmt.Sprintf("wip(%s): round %d", featureID, round)
	} else {
		msg = fmt.Sprintf("feat(%s): %s", featureID, featureName)
	}
	if out, err := exec.Command("git", "-C", wtPath, "commit", "-m", msg).CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", string(out), err)
	}

	fmt.Printf("  committed: %s\n", msg)
	return nil
}

func (m *monoRepo) Merge(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)

	curBranch, err := exec.Command("git", "-C", m.root, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return MergeResult{Error: "cannot determine current branch (detached HEAD?)"}
	}
	if strings.TrimSpace(string(curBranch)) == branch {
		return MergeResult{Error: fmt.Sprintf("current branch is %s — switch to main/master first", branch)}
	}

	msg := fmt.Sprintf("Merge branch '%s' — %s", branch, featureName)
	out, err := exec.Command("git", "-C", m.root, "merge", "--no-ff", "-m", msg, branch).CombinedOutput()
	if err != nil {
		files := conflictFiles(m.root)
		exec.Command("git", "-C", m.root, "merge", "--abort").Run()
		if len(files) > 0 {
			return MergeResult{Conflict: true, Files: files}
		}
		return MergeResult{Error: strings.TrimSpace(string(out))}
	}
	_ = out

	m.Cleanup(featureID)
	return MergeResult{}
}

func (m *monoRepo) Cleanup(featureID string) error {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	if out, err := exec.Command("git", "-C", m.root, "worktree", "remove", wtDir).CombinedOutput(); err != nil {
		exec.Command("git", "-C", m.root, "worktree", "remove", "--force", wtDir).Run()
		if _, statErr := os.Stat(wtDir); statErr == nil {
			return fmt.Errorf("worktree remove failed: %s", string(out))
		}
	}

	exec.Command("git", "-C", m.root, "branch", "-D", branch).Run()
	return nil
}

func (m *monoRepo) DetectChangedRepos() []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = m.root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	repoSet := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "/", 2)
		if len(parts) > 0 {
			repoSet[parts[0]] = true
		}
	}

	var repos []string
	for r := range repoSet {
		repos = append(repos, r)
	}
	return repos
}

func (m *monoRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	if len(featureRepos) == 0 {
		featureRepos = []string{"."}
	}

	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for _, repoPath := range featureRepos {
		fullPath := filepath.Join(m.root, repoPath)
		if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
			continue
		}

		head := gitOutput(fullPath, "rev-parse", "HEAD")
		branch := gitOutput(fullPath, "rev-parse", "--abbrev-ref", "HEAD")
		statusOut := gitOutput(fullPath, "status", "--short")

		var dirty []string
		for _, line := range strings.Split(statusOut, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				dirty = append(dirty, line)
			}
		}

		baseline.Repos = append(baseline.Repos, protocol.BaselineRepo{
			Name:       repoPath,
			Path:       repoPath,
			Branch:     branch,
			Head:       head,
			DirtyFiles: dirty,
		})
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitops/ -run TestMonoRepo -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitops/monorepo.go internal/gitops/monorepo_test.go
git commit -m "feat(F049): implement monoRepo gitops — move existing git logic into gitops package"
```

---

### Task 6: multiRepo implementation

**Files:**
- Modify: `internal/gitops/multirepo.go`
- Create: `internal/gitops/multirepo_test.go`

- [ ] **Step 1: Write tests for multiRepo**

Create `internal/gitops/multirepo_test.go`:

```go
package gitops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupMultiWorkspace(t *testing.T) (string, *protocol.Workspace, Ops) {
	t.Helper()
	root := t.TempDir()

	for _, name := range []string{"core", "gate"} {
		dir := filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)
		initGitRepo(t, dir)
	}

	os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\nuse ./core\nuse ./gate\n"), 0o644)

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	ops := New(root, ws, cfg)
	return root, ws, ops
}

func TestMultiRepo_IsMultiRepo(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	if !ops.IsMultiRepo() {
		t.Error("should be multi-repo")
	}
}

func TestMultiRepo_SetupWorktree(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	expected := Dir(root, "feat-1")
	if wtPath != expected {
		t.Errorf("wtPath = %q, want %q", wtPath, expected)
	}

	for _, name := range []string{"core", "gate"} {
		repoDir := filepath.Join(wtPath, name)
		if _, err := os.Stat(repoDir); err != nil {
			t.Errorf("repo %s should exist in worktree: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(wtPath, "go.work")); err != nil {
		t.Error("go.work should be copied to worktree")
	}

	if _, err := os.Stat(filepath.Join(wtPath, protocol.DirName, protocol.ConfigFile)); err != nil {
		t.Error(".4x/settings.json should be copied to worktree")
	}
}

func TestMultiRepo_CommitOnlyFeatureRepos(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(wtPath, "core", "new.go"), []byte("package core"), 0o644)

	if err := ops.Commit(wtPath, "feat-1", "Test", 1); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	log := gitOutput(filepath.Join(wtPath, "core"), "log", "--oneline", "-1")
	if log == "" {
		t.Error("core should have a new commit")
	}
}

func TestMultiRepo_MergeAllOrNothing(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(wtPath, "core", "feature.go"), []byte("package core"), 0o644)
	os.WriteFile(filepath.Join(wtPath, "gate", "feature.go"), []byte("package gate"), 0o644)

	if err := ops.Commit(wtPath, "feat-1", "Test", 1); err != nil {
		t.Fatal(err)
	}

	result := ops.Merge("feat-1", "Test Feature")
	if result.Conflict || result.Error != "" {
		t.Fatalf("Merge failed: conflict=%v error=%q", result.Conflict, result.Error)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "core")); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up after merge")
	}
}

func TestMultiRepo_CaptureBaseline(t *testing.T) {
	_, ws, ops := setupMultiWorkspace(t)
	ws.InitFeatureDir("feat-1")

	if err := ops.CaptureBaseline("feat-1", []string{"core", "gate"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(baseline.Repos))
	}
}

func TestMultiRepo_DetectChangedRepos(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	_ = root

	changed := ops.DetectChangedRepos()
	if len(changed) != 0 {
		t.Errorf("no changes expected, got %v", changed)
	}
}

func TestMultiRepo_Cleanup(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.Cleanup("feat-1"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed")
	}
	_ = root
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gitops/ -run TestMultiRepo -v`
Expected: FAIL — multiRepo methods not implemented

- [ ] **Step 3: Implement multiRepo**

Replace `internal/gitops/multirepo.go`:

```go
package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

type multiRepo struct {
	root string
	ws   *protocol.Workspace
	cfg  protocol.Config
}

func (m *multiRepo) IsMultiRepo() bool { return true }

func (m *multiRepo) SetupWorktree(featureID string) (string, error) {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	ensureGitignore(m.root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		m.ensureDotDir(wtDir)
		return wtDir, nil
	}

	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return "", err
	}

	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)

		out, err := exec.Command("git", "-C", repoPath, "worktree", "add", wtRepoDir, "-b", branch).CombinedOutput()
		if err != nil {
			out2, err2 := exec.Command("git", "-C", repoPath, "worktree", "add", wtRepoDir, branch).CombinedOutput()
			if err2 != nil {
				m.cleanupPartial(wtDir, featureID)
				return "", fmt.Errorf("git worktree add %s: %s\n%s", name, string(out), string(out2))
			}
		}
	}

	m.copyWorkspaceFiles(wtDir)
	m.ensureDotDir(wtDir)
	return wtDir, nil
}

func (m *multiRepo) cleanupPartial(wtDir, featureID string) {
	branch := Branch(featureID)
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)
		exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtRepoDir).Run()
		exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run()
	}
	os.RemoveAll(wtDir)
}

func (m *multiRepo) copyWorkspaceFiles(wtDir string) {
	repoDirs := make(map[string]bool)
	for _, rc := range m.cfg.Workspace.Repos {
		parts := strings.SplitN(rc.Path, "/", 2)
		repoDirs[parts[0]] = true
	}
	repoDirs[protocol.DirName] = true
	repoDirs[".worktrees"] = true
	repoDirs[".git"] = true

	entries, err := os.ReadDir(m.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if repoDirs[name] {
			continue
		}
		if e.IsDir() {
			continue
		}
		copyFileIfExists(filepath.Join(m.root, name), filepath.Join(wtDir, name))
	}
}

func (m *multiRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)
	os.MkdirAll(dotDir, 0o755)

	src := filepath.Join(m.root, protocol.DirName, protocol.ConfigFile)
	dst := filepath.Join(dotDir, protocol.ConfigFile)
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(dst, data, 0o644)
	}

	srcPlugins := filepath.Join(m.root, protocol.DirName, "plugins")
	dstPlugins := filepath.Join(dotDir, "plugins")
	if entries, err := os.ReadDir(srcPlugins); err == nil {
		os.MkdirAll(dstPlugins, 0o755)
		for _, e := range entries {
			if !e.IsDir() {
				copyFileIfExists(filepath.Join(srcPlugins, e.Name()), filepath.Join(dstPlugins, e.Name()))
			}
		}
	}
}

func (m *multiRepo) Commit(wtRoot, featureID, featureName string, round int) error {
	var msg string
	if round > 0 {
		msg = fmt.Sprintf("wip(%s): round %d", featureID, round)
	} else {
		msg = fmt.Sprintf("feat(%s): %s", featureID, featureName)
	}

	for name := range m.cfg.Workspace.Repos {
		repoDir := filepath.Join(wtRoot, name)
		if _, err := os.Stat(repoDir); err != nil {
			continue
		}

		if out, err := exec.Command("git", "-C", repoDir, "add", "-A").CombinedOutput(); err != nil {
			return fmt.Errorf("git add %s: %s: %w", name, string(out), err)
		}

		if exec.Command("git", "-C", repoDir, "diff", "--cached", "--quiet").Run() == nil {
			continue
		}

		if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", msg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit %s: %s: %w", name, string(out), err)
		}
		fmt.Printf("  committed [%s]: %s\n", name, msg)
	}
	return nil
}

func (m *multiRepo) Merge(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)
	msg := fmt.Sprintf("Merge branch '%s' — %s", branch, featureName)

	type repoHead struct {
		name     string
		repoPath string
		head     string
	}

	var preHeads []repoHead
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)

		curBranch, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD").Output()
		if err != nil {
			return MergeResult{Error: fmt.Sprintf("%s: cannot determine current branch", name)}
		}
		if strings.TrimSpace(string(curBranch)) == branch {
			return MergeResult{Error: fmt.Sprintf("%s: current branch is %s — switch to main/master first", name, branch)}
		}

		head := gitOutput(repoPath, "rev-parse", "HEAD")
		preHeads = append(preHeads, repoHead{name: name, repoPath: repoPath, head: head})
	}

	var merged []repoHead
	for _, rh := range preHeads {
		out, err := exec.Command("git", "-C", rh.repoPath, "merge", "--no-ff", "-m", msg, branch).CombinedOutput()
		if err != nil {
			files := conflictFiles(rh.repoPath)
			exec.Command("git", "-C", rh.repoPath, "merge", "--abort").Run()

			for _, done := range merged {
				exec.Command("git", "-C", done.repoPath, "reset", "--hard", done.head).Run()
			}

			if len(files) > 0 {
				return MergeResult{Conflict: true, ConflictRepo: rh.name, Files: files}
			}
			return MergeResult{Error: fmt.Sprintf("%s: %s", rh.name, strings.TrimSpace(string(out)))}
		}
		_ = out
		merged = append(merged, rh)
	}

	m.Cleanup(featureID)
	return MergeResult{}
}

func (m *multiRepo) Cleanup(featureID string) error {
	wtDir := Dir(m.root, featureID)
	branch := Branch(featureID)

	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		wtRepoDir := filepath.Join(wtDir, name)

		if out, err := exec.Command("git", "-C", repoPath, "worktree", "remove", wtRepoDir).CombinedOutput(); err != nil {
			exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtRepoDir).Run()
			if _, statErr := os.Stat(wtRepoDir); statErr == nil {
				return fmt.Errorf("worktree remove %s failed: %s", name, string(out))
			}
		}

		exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run()
	}

	os.RemoveAll(wtDir)
	return nil
}

func (m *multiRepo) DetectChangedRepos() []string {
	var changed []string
	for name, rc := range m.cfg.Workspace.Repos {
		repoPath := filepath.Join(m.root, rc.Path)
		out := gitOutput(repoPath, "diff", "--name-only", "HEAD")
		if out != "" {
			changed = append(changed, name)
		}
	}
	return changed
}

func (m *multiRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	repoPaths := protocol.ResolveFeatureRepoPaths(
		protocol.Feature{Repos: featureRepos}, m.cfg, m.root,
	)

	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for name, fullPath := range repoPaths {
		if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
			continue
		}

		head := gitOutput(fullPath, "rev-parse", "HEAD")
		branch := gitOutput(fullPath, "rev-parse", "--abbrev-ref", "HEAD")
		statusOut := gitOutput(fullPath, "status", "--short")

		var dirty []string
		for _, line := range strings.Split(statusOut, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				dirty = append(dirty, line)
			}
		}

		baseline.Repos = append(baseline.Repos, protocol.BaselineRepo{
			Name:       name,
			Path:       m.cfg.Workspace.Repos[name].Path,
			Branch:     branch,
			Head:       head,
			DirtyFiles: dirty,
		})
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitops/ -run TestMultiRepo -v`
Expected: ALL PASS

- [ ] **Step 5: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/gitops/multirepo.go internal/gitops/multirepo_test.go
git commit -m "feat(F049): implement multiRepo gitops — per-repo worktree, commit, merge, cleanup"
```

---

### Task 7: Guard integration — checkScope uses gitops.Ops

**Files:**
- Modify: `internal/guard/check.go:22-32` (Check function signature)
- Modify: `internal/guard/check_test.go`
- Modify: `cmd/4x/check.go:35` (pass ops to Check)

- [ ] **Step 1: Add ScopeChecker interface to guard package**

To avoid circular dependency (guard importing gitops, while gitops is independent), define a narrow interface in the guard package:

In `internal/guard/check.go`, add after the imports:

```go
// ScopeDetector 偵測哪些 repo 有 uncommitted changes，由 gitops.Ops 實作。
type ScopeDetector interface {
	DetectChangedRepos() []string
}
```

Change `Check` signature:

```go
func Check(ws *protocol.Workspace, featureID string, detector ScopeDetector) CheckResult {
	r := CheckResult{Pass: true}

	checkRequiredFiles(ws, featureID, &r)
	checkBaseline(ws, featureID, &r)
	checkScope(ws, featureID, detector, &r)
	checkDependencies(ws, featureID, &r)
	checkBacklogDrift(ws, featureID, &r)

	return r
}
```

Change `checkScope`:

```go
func checkScope(ws *protocol.Workspace, featureID string, detector ScopeDetector, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("cannot load feature YAML: %v", err))
		return
	}

	if len(feature.Repos) == 0 {
		return
	}

	allowedRepos := make(map[string]bool)
	for _, repo := range feature.Repos {
		allowedRepos[repo] = true
	}

	var changedRepos []string
	if detector != nil {
		changedRepos = detector.DetectChangedRepos()
	} else {
		changedRepos = detectChangedRepos(ws.Root)
	}
	for _, repo := range changedRepos {
		if !allowedRepos[repo] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("scope violation: repo %q not in feature repos", repo))
		}
	}
}
```

Keep `detectChangedRepos` and `CaptureBaseline` as exported functions for backward compatibility (they're used in tests), but the primary path now goes through gitops.

- [ ] **Step 2: Update guard tests**

In `internal/guard/check_test.go`, update all `Check(ws, featureID)` calls to `Check(ws, featureID, nil)`:

Search and replace all occurrences of `Check(ws,` with `Check(ws,` ensuring the third argument `nil` is added. The `nil` detector falls back to the existing `detectChangedRepos` behavior.

Lines affected: 50, 91, 169, 189, 215, 228, 247, 282, 311, 445.

- [ ] **Step 3: Update cmd/4x/check.go**

In `cmd/4x/check.go:35`, change:

```go
// Before
result := guard.Check(ws, featureID)

// After
result := guard.Check(ws, featureID, nil)
```

- [ ] **Step 4: Run all tests**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/guard/check.go internal/guard/check_test.go cmd/4x/check.go
git commit -m "feat(F049): guard.Check accepts ScopeDetector interface for multi-repo scope checking"
```

---

### Task 8: CLI callers — run.go, batch.go, done.go, merge.go

**Files:**
- Modify: `cmd/4x/run.go`
- Modify: `cmd/4x/batch.go`
- Modify: `cmd/4x/done.go`
- Modify: `cmd/4x/merge.go`

- [ ] **Step 1: Update run.go — replace direct git calls with gitops.Ops**

Add import `"github.com/ggwhite/4x/internal/gitops"` to run.go.

In `newRunCmd` (around line 144-157), replace worktree setup:

```go
// Before
var runnerWs *protocol.Workspace
var wtPath string
if cfg.Isolation == "worktree" {
    var err error
    wtPath, err = setupWorktree(ws.Root, featureID)
    if err != nil {
        return fmt.Errorf("worktree setup: %w", err)
    }
    runnerWs = &protocol.Workspace{Root: wtPath}
    fmt.Printf("worktree: %s\n", wtPath)
} else {
    runnerWs = ws
}

// After
ops := gitops.New(ws.Root, ws, cfg)
var runnerWs *protocol.Workspace
var wtPath string
if cfg.Isolation == "worktree" {
    var err error
    wtPath, err = ops.SetupWorktree(featureID)
    if err != nil {
        return fmt.Errorf("worktree setup: %w", err)
    }
    runnerWs = &protocol.Workspace{Root: wtPath}
    fmt.Printf("worktree: %s\n", wtPath)
} else {
    runnerWs = ws
}
```

In `runLoop`, replace `captureBaselineOnce` call (line 367):

```go
// Before
if err := captureBaselineOnce(ws, featureID, repoPathsFromFeature(feature)); err != nil {
    return err
}

// After
if err := captureBaselineOnce(ws, ops, featureID, feature.Repos); err != nil {
    return err
}
```

Pass `ops` into `runLoop` (add parameter) and update `captureBaselineOnce`:

```go
func captureBaselineOnce(ws *protocol.Workspace, ops gitops.Ops, featureID string, featureRepos []string) error {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("baseline path is a directory: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check baseline: %w", err)
	}
	if err := ops.CaptureBaseline(featureID, featureRepos); err != nil {
		return fmt.Errorf("capture baseline: %w", err)
	}
	return nil
}
```

Replace `commitWorktree` calls (line 487) with `ops.Commit`:

```go
// Before
if err := commitWorktree(runnerWs.Root, featureID, feature.Name, s.Round); err != nil {

// After
if err := ops.Commit(runnerWs.Root, featureID, feature.Name, s.Round); err != nil {
```

Replace post-loop commit (line 263):

```go
// Before
if err := commitWorktree(wtPath, featureID, feature.Name, 0); err != nil {

// After
if err := ops.Commit(wtPath, featureID, feature.Name, 0); err != nil {
```

Pass `ops` through to `guard.Check` calls in `runLoop`:

```go
// Wherever guard.Check is called in the run loop, pass ops as detector
```

Remove the now-unused functions: `setupWorktree`, `ensureWorktreeDotDir`, `commitWorktree`, `repoPathsFromFeature`, `ensureGitignore`, `copyFileIfExists` (the last two are now in gitops). Keep `syncFeatureToWorktree`, `syncFeatureFromWorktree`, `startLiveSync` — they stay in run.go as they deal with protocol file sync, not git.

- [ ] **Step 2: Update done.go**

Add import `"github.com/ggwhite/4x/internal/gitops"`.

In `markDone` (line 39-79), replace `worktree.Merge` with `gitops`:

```go
func markDone(ws *protocol.Workspace, featureID string) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	cfg, _ := ws.ReadConfig()
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}

	ops := gitops.New(ws.Root, ws, cfg)

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	result := ops.Merge(featureID, name)
	if result.Conflict {
		fmt.Println("Merge conflict — feature remains pending-review:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		if result.ConflictRepo != "" {
			fmt.Printf("  repo: %s\n", result.ConflictRepo)
		}
		fmt.Printf("Worktree: %s\n", gitops.Dir(ws.Root, featureID))
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "warning: merge failed; feature remains pending-review: %s\n", result.Error)
		fmt.Printf("Worktree preserved at: %s\n", gitops.Dir(ws.Root, featureID))
		return nil
	}

	if err := finalizeDone(ws, featureID, s); err != nil {
		return err
	}

	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
}
```

Remove import of `"github.com/ggwhite/4x/internal/worktree"`.

- [ ] **Step 3: Update merge.go**

Similar pattern — replace `worktree.Merge` / `worktree.Dir` / `worktree.Branch` with `gitops` equivalents. Load config, create `ops`, use `ops.Merge`.

- [ ] **Step 4: Update batch.go — add worktree isolation**

In batch.go, around line 315, add worktree support:

```go
// Before
err = runLoop(batchCtx, ws, ws, feature, cfg, s, runnerFactory, "never")

// After
ops := gitops.New(ws.Root, ws, cfg)
var batchRunnerWs *protocol.Workspace
var batchWtPath string
commitStrategy := "never"
if cfg.Isolation == "worktree" {
    var wtErr error
    batchWtPath, wtErr = ops.SetupWorktree(next)
    if wtErr != nil {
        fmt.Printf("  worktree setup failed: %v\n", wtErr)
        statusMap[next] = protocol.StatusBlocked
        batchCancel()
        continue
    }
    batchRunnerWs = &protocol.Workspace{Root: batchWtPath}
    commitStrategy = "per-round"
    runnerFactory = func(logPath string, model string) runner.Runner {
        return runner.NewRunner(batchRunnerWs, runnerName, runnerCfg, time.Duration(timeout)*time.Second, logPath, model)
    }
} else {
    batchRunnerWs = ws
}
err = runLoop(batchCtx, ws, batchRunnerWs, feature, cfg, s, runnerFactory, commitStrategy)
```

- [ ] **Step 5: Update batch.go to pass ops through runLoop**

The `runLoop` function signature needs `ops gitops.Ops` added as a parameter. Update all callers.

- [ ] **Step 6: Run all tests**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/run.go cmd/4x/batch.go cmd/4x/done.go cmd/4x/merge.go
git commit -m "feat(F049): CLI callers delegate to gitops.Ops — run, batch, done, merge"
```

---

### Task 9: Batch clustering — use EffectiveHubRepos

**Files:**
- Modify: `cmd/4x/batch.go:235` (PlanBatch call)

- [ ] **Step 1: Update PlanBatch call to use EffectiveHubRepos**

In `cmd/4x/batch.go`, line 235:

```go
// Before
plan, err := batch.PlanBatch(pending, cfg.HubRepos, 4)

// After
plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), 4)
```

- [ ] **Step 2: Run all tests**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/batch.go
git commit -m "feat(F049): batch clustering uses EffectiveHubRepos — merges HubRepos config with workspace hub flags"
```

---

### Task 10: Templates — designer and coder

**Files:**
- Modify: `templates/designer.md.tmpl:16-21`
- Modify: `templates/coder.md.tmpl` (add RepoMap section)
- Modify: `cmd/4x/run.go` (add RepoMap to promptData)

- [ ] **Step 1: Update designer template**

In `templates/designer.md.tmpl`, replace lines 16-21:

```tmpl
{{- if .Feature.Repos}}
Repos:
{{- range .Feature.Repos}}
  - {{.}}
{{- end}}
{{- end}}
```

- [ ] **Step 2: Add RepoMap to promptData and coder template**

In `cmd/4x/run.go`, in the `promptData` struct (find it near `generatePrompt`), add:

```go
RepoMap          map[string]string
```

In `generatePrompt`, populate it:

```go
var repoMap map[string]string
if len(cfg.Workspace.Repos) > 0 {
    repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
}
data := promptData{
    // ... existing fields ...
    RepoMap:          repoMap,
}
```

In `templates/coder.md.tmpl`, add before `== Workflow ==`:

```tmpl
{{- if .RepoMap}}

== Workspace Repos ==
{{- range $name, $path := .RepoMap}}
- {{$name}} → {{$path}}/
{{- end}}
{{- end}}
```

- [ ] **Step 3: Run dry-run to verify templates render**

Run: `go build ./cmd/4x && ./bin/4x run --dry-run <any-feature-id>` (pick an existing feature)
Expected: template renders without errors

- [ ] **Step 4: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add templates/designer.md.tmpl templates/coder.md.tmpl cmd/4x/run.go
git commit -m "feat(F049): templates — designer shows repo list, coder gets workspace repo map"
```

---

### Task 11: Remove old worktree package

**Files:**
- Remove: `internal/worktree/merge.go`
- Remove: `internal/worktree/merge_test.go`
- Modify: any remaining imports of `internal/worktree`

- [ ] **Step 1: Verify no remaining imports**

Run: `grep -rn '"github.com/ggwhite/4x/internal/worktree"' --include='*.go'`
Expected: no results (all callers should have been migrated in Task 8)

- [ ] **Step 2: Remove worktree package**

```bash
rm internal/worktree/merge.go internal/worktree/merge_test.go
rmdir internal/worktree/
```

- [ ] **Step 3: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(F049): remove internal/worktree package — logic moved to internal/gitops"
```

---

### Task 12: Update F049 feature YAML + docs sync

**Files:**
- Modify: `.4x/features/F049-workspace-multi-repo.yaml` (mark subtasks done)

- [ ] **Step 1: Run docs sync check**

Run: `make check-docs-sync`

If any docs need updating, update them.

- [ ] **Step 2: Run i18n check**

Run: `make check-i18n`

If any keys are missing, add them.

- [ ] **Step 3: Run final full verification**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Commit any doc/i18n fixes**

```bash
git add -A
git commit -m "docs(F049): update docs and i18n for workspace multi-repo support"
```
