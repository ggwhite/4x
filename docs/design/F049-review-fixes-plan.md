# F049 Code Review Fixes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 10 findings from the F049 code review — 4 correctness bugs + 6 cleanup items.

**Architecture:** All changes on branch `4x/F049-workspace-multi-repo` in worktree `.worktrees/4x/F049-workspace-multi-repo/`. Changes span `internal/gitops/`, `internal/guard/`, `cmd/4x/`, `.4x/features/`, `schemas/`.

**Tech Stack:** Go 1.26, git worktree

**Working directory:** `/Users/white/github/4x/.worktrees/4x/F049-workspace-multi-repo`

---

### Task 1: YAML migration — Feature.Repos map → list

24 個既有 feature YAML 的 `repos:` 欄位用 map 格式（`key: value`），但 types.go 已改為 `[]string`。需把所有 YAML 改成 list 格式，只保留 key（repo name），丟掉 value（path hint）。

**Files:**
- Modify: `.4x/features/*.yaml` (24 files with repos)
- Modify: `schemas/feature.schema.json` (repos type)

- [ ] **Step 1: 更新所有 feature YAML 的 repos 為 list 格式**

每個 feature YAML 的 repos 從：
```yaml
repos:
  state: internal/state/
```
改為：
```yaml
repos:
  - state
```

對於 `repos: { self: "." }` 或 `repos: { self: . }`，改為：
```yaml
repos:
  - self
```

用 Python 腳本批量處理：
```bash
python3 -c "
import yaml, glob, os
for f in sorted(glob.glob('.4x/features/*.yaml')):
    with open(f) as fh:
        data = yaml.safe_load(fh)
    if not data or 'repos' not in data or not isinstance(data['repos'], dict):
        continue
    data['repos'] = list(data['repos'].keys())
    with open(f, 'w') as fh:
        yaml.dump(data, fh, default_flow_style=False, allow_unicode=True, sort_keys=False)
    print(f'  updated: {os.path.basename(f)}')
"
```

- [ ] **Step 2: 更新 JSON schema**

`schemas/feature.schema.json` 的 `repos` 改為：
```json
"repos": {
  "type": "array",
  "items": { "type": "string" }
}
```

- [ ] **Step 3: 驗證**

```bash
go test ./internal/protocol/ -v -count=1
go test ./cmd/4x/ -v -count=1 -run TestRunLoop
```

- [ ] **Step 4: Commit**

```bash
git add .4x/features/ schemas/feature.schema.json
git commit -m "fix(F049): migrate feature YAML repos from map to list format"
```

---

### Task 2: Fix ensureGitignore — 還原逐行精確比對

**Files:**
- Modify: `internal/gitops/gitops.go:61-72`

- [ ] **Step 1: 寫測試**

在 `internal/gitops/gitops_test.go` 新增：
```go
func TestEnsureGitignore_ExactMatch(t *testing.T) {
	root := t.TempDir()
	gitignorePath := filepath.Join(root, ".gitignore")

	// comment 包含 entry 子字串不應被視為已存在
	os.WriteFile(gitignorePath, []byte("# .worktrees/ is managed\nnode_modules/\n"), 0o644)
	ensureGitignore(root, ".worktrees/")

	data, _ := os.ReadFile(gitignorePath)
	if !strings.Contains(string(data), "\n.worktrees/\n") && !strings.HasSuffix(string(data), "\n.worktrees/") {
		t.Errorf("should add .worktrees/ entry, got:\n%s", data)
	}
}

func TestEnsureGitignore_AlreadyExists(t *testing.T) {
	root := t.TempDir()
	gitignorePath := filepath.Join(root, ".gitignore")

	os.WriteFile(gitignorePath, []byte("node_modules/\n.worktrees/\n"), 0o644)
	ensureGitignore(root, ".worktrees/")

	data, _ := os.ReadFile(gitignorePath)
	if strings.Count(string(data), ".worktrees/") != 1 {
		t.Errorf("should not duplicate, got:\n%s", data)
	}
}
```

- [ ] **Step 2: 跑測試確認 ExactMatch fails**

```bash
go test ./internal/gitops/ -run TestEnsureGitignore_ExactMatch -v
```

- [ ] **Step 3: 修正 ensureGitignore**

`internal/gitops/gitops.go` — 將 `strings.Contains` 改回逐行比對：
```go
func ensureGitignore(root, entry string) {
	path := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
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
```

- [ ] **Step 4: 跑測試確認全 pass**

```bash
go test ./internal/gitops/ -run TestEnsureGitignore -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/gitops/
git commit -m "fix(F049): restore line-by-line exact match in ensureGitignore"
```

---

### Task 3: Fix nil ScopeDetector in check command

**Files:**
- Modify: `cmd/4x/check.go`

- [ ] **Step 1: 修正 check.go — 建 gitops.Ops 並傳入**

```go
// 在 RunE 裡，guard.Check 之前加：
cfg, _ := ws.ReadConfig()
if userCfg, err := protocol.ReadUserConfig(); err == nil {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
ops := gitops.New(ws.Root, ws, cfg)

result := guard.Check(ws, featureID, ops)
```

需要 import `"github.com/ggwhite/4x/internal/gitops"`。

- [ ] **Step 2: 驗證編譯和測試**

```bash
go build ./cmd/4x && go test ./internal/guard/ -v -count=1
```

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/check.go
git commit -m "fix(F049): pass gitops.Ops as ScopeDetector to guard.Check in check command"
```

---

### Task 4: Add monoRepo merge conflict and dirty tests

**Files:**
- Modify: `internal/gitops/monorepo_test.go`

- [ ] **Step 1: 新增 TestMonoRepo_MergeConflict**

```go
func TestMonoRepo_MergeConflict(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	_, err := ops.SetupWorktree("feat-conflict")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	wtDir := Dir(root, "feat-conflict")
	os.WriteFile(filepath.Join(wtDir, "conflict.txt"), []byte("from-branch"), 0o644)
	runGit(t, wtDir, "add", "conflict.txt")
	runGit(t, wtDir, "commit", "-m", "branch change")

	os.WriteFile(filepath.Join(root, "conflict.txt"), []byte("from-main"), 0o644)
	runGit(t, root, "add", "conflict.txt")
	runGit(t, root, "commit", "-m", "main change")

	result := ops.Merge("feat-conflict", "Conflict Feature")
	if !result.Conflict {
		t.Fatal("expected conflict")
	}
	if len(result.Files) == 0 {
		t.Error("should report conflicting files")
	}
	if _, err := os.Stat(wtDir); err != nil {
		t.Error("worktree should be preserved on conflict")
	}
}

func TestMonoRepo_MergeDirtyWorkingTree(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	_, err := ops.SetupWorktree("feat-dirty")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	wtDir := Dir(root, "feat-dirty")
	os.WriteFile(filepath.Join(wtDir, "new.txt"), []byte("branch"), 0o644)
	runGit(t, wtDir, "add", "new.txt")
	runGit(t, wtDir, "commit", "-m", "feat")

	// dirty main working tree
	os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("uncommitted"), 0o644)

	result := ops.Merge("feat-dirty", "Dirty Feature")
	if result.Conflict {
		t.Error("dirty tree should not be reported as conflict")
	}
	if result.Error == "" {
		t.Error("dirty working tree should produce an error")
	}
}
```

- [ ] **Step 2: 跑測試**

```bash
go test ./internal/gitops/ -run 'TestMonoRepo_Merge' -v
```

- [ ] **Step 3: Commit**

```bash
git add internal/gitops/monorepo_test.go
git commit -m "test(F049): add monoRepo merge conflict and dirty working tree tests"
```

---

### Task 5: Extract shared gitops helpers — dedup ensureDotDir, CaptureBaseline, copyFileIfExists

**Files:**
- Modify: `internal/gitops/gitops.go` (add shared helpers)
- Modify: `internal/gitops/monorepo.go` (call shared helpers)
- Modify: `internal/gitops/multirepo.go` (call shared helpers)
- Modify: `cmd/4x/run.go` (remove duplicate copyFileIfExists, import from gitops)

- [ ] **Step 1: 抽出 syncDotDirContents 到 gitops.go**

```go
// syncDotDirContents 將 mainRoot 的 .4x/settings.json 和 plugins/ 複製到 dotDir。
func syncDotDirContents(mainRoot, dotDir string) {
	os.MkdirAll(dotDir, 0o755)

	src := filepath.Join(mainRoot, protocol.DirName, protocol.ConfigFile)
	dst := filepath.Join(dotDir, protocol.ConfigFile)
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(dst, data, 0o644)
	}

	srcPlugins := filepath.Join(mainRoot, protocol.DirName, "plugins")
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
```

- [ ] **Step 2: 簡化 monoRepo.ensureDotDir 和 multiRepo.ensureDotDir**

monoRepo.ensureDotDir：
```go
func (m *monoRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)
	// 移除舊的 symlink（向下相容）
	if info, err := os.Lstat(dotDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			os.Remove(dotDir)
		}
	}
	syncDotDirContents(m.root, dotDir)
}
```

multiRepo.ensureDotDir：
```go
func (m *multiRepo) ensureDotDir(wtDir string) {
	dotDir := filepath.Join(wtDir, protocol.DirName)
	syncDotDirContents(m.root, dotDir)
}
```

- [ ] **Step 3: 抽出 captureRepoBaseline 到 gitops.go**

```go
func captureRepoBaseline(fullPath, name, repoPath string) *protocol.BaselineRepo {
	if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
		return nil
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

	return &protocol.BaselineRepo{
		Name:       name,
		Path:       repoPath,
		Branch:     branch,
		Head:       head,
		DirtyFiles: dirty,
	}
}
```

- [ ] **Step 4: 簡化 monoRepo.CaptureBaseline 和 multiRepo.CaptureBaseline**

monoRepo 版：
```go
func (m *monoRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	if len(featureRepos) == 0 {
		featureRepos = []string{"."}
	}
	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for _, repoPath := range featureRepos {
		fullPath := filepath.Join(m.root, repoPath)
		if repoPath == "." {
			fullPath = m.root
		}
		if br := captureRepoBaseline(fullPath, repoPath, repoPath); br != nil {
			baseline.Repos = append(baseline.Repos, *br)
		}
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
```

multiRepo 版：
```go
func (m *multiRepo) CaptureBaseline(featureID string, featureRepos []string) error {
	repoPaths := protocol.ResolveFeatureRepoPaths(
		protocol.Feature{Repos: featureRepos}, m.cfg, m.root,
	)
	baseline := protocol.Baseline{CreatedAt: time.Now()}
	for name, fullPath := range repoPaths {
		repoPath := ""
		if rc, ok := m.cfg.Workspace.Repos[name]; ok {
			repoPath = rc.Path
		}
		if br := captureRepoBaseline(fullPath, name, repoPath); br != nil {
			baseline.Repos = append(baseline.Repos, *br)
		}
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.ws.FeatureDir(featureID), protocol.BaselineFile), data, 0o644)
}
```

- [ ] **Step 5: Export copyFileIfExists，刪除 run.go 的重複版本**

`internal/gitops/gitops.go` — 將 `copyFileIfExists` 改名為 `CopyFileIfExists`（exported）。
`cmd/4x/run.go` — 刪除 `func copyFileIfExists`，syncFeatureToWorktree 改用 `gitops.CopyFileIfExists`。

- [ ] **Step 6: 跑測試**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/gitops/ cmd/4x/run.go
git commit -m "refactor(F049): extract shared gitops helpers — syncDotDirContents, captureRepoBaseline, CopyFileIfExists"
```

---

### Task 6: Add message param to Commit, unify merge.go

**Files:**
- Modify: `internal/gitops/gitops.go` (Ops interface)
- Modify: `internal/gitops/monorepo.go`
- Modify: `internal/gitops/multirepo.go`
- Modify: `cmd/4x/merge.go`
- Modify: `cmd/4x/run.go` (callers)
- Modify: `cmd/4x/batch.go` (callers)

- [ ] **Step 1: 改 Ops interface — Commit 加 msg 參數**

```go
type Ops interface {
	SetupWorktree(featureID string) (wtRoot string, err error)
	Commit(wtRoot, featureID, msg string) error
	Merge(featureID, featureName string) MergeResult
	Cleanup(featureID string) error
	DetectChangedRepos() []string
	CaptureBaseline(featureID string, featureRepos []string) error
	IsMultiRepo() bool
}
```

- [ ] **Step 2: 更新 monoRepo.Commit 和 multiRepo.Commit**

monoRepo.Commit：
```go
func (m *monoRepo) Commit(wtPath, featureID, msg string) error {
	if out, err := exec.Command("git", "-C", wtPath, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", string(out), err)
	}
	if exec.Command("git", "-C", wtPath, "diff", "--cached", "--quiet").Run() == nil {
		return nil
	}
	if out, err := exec.Command("git", "-C", wtPath, "commit", "-m", msg).CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", string(out), err)
	}
	fmt.Printf("  committed: %s\n", msg)
	return nil
}
```

multiRepo.Commit：
```go
func (m *multiRepo) Commit(wtRoot, featureID, msg string) error {
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
```

- [ ] **Step 3: 更新所有 Commit 呼叫端**

`cmd/4x/run.go` — 產生 msg 後呼叫：
```go
// on-done commit
msg := fmt.Sprintf("feat(%s): %s", featureID, feature.Name)
if err := ops.Commit(wtPath, featureID, msg); err != nil {

// per-round commit
msg := fmt.Sprintf("wip(%s): round %d", featureID, s.Round)
if err := ops.Commit(runnerWs.Root, featureID, msg); err != nil {
```

`cmd/4x/merge.go` — 移除 IsMultiRepo 分支，統一呼叫：
```go
msg := fmt.Sprintf("fix(%s): resolve merge conflicts — %s", featureID, name)
if err := ops.Commit(wtDir, featureID, msg); err != nil {
    return fmt.Errorf("commit in worktree failed: %w", err)
}
```

- [ ] **Step 4: 更新測試**

`cmd/4x/run_loop_test.go` 和 `internal/gitops/monorepo_test.go`、`multirepo_test.go` 中的 Commit 呼叫也要更新。

- [ ] **Step 5: 跑測試**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/gitops/ cmd/4x/
git commit -m "refactor(F049): simplify Commit interface — accept message string, eliminate IsMultiRepo branch in merge.go"
```

---

### Task 7: Fix pointer comparison in generatePrompt

**Files:**
- Modify: `cmd/4x/run.go` (generatePrompt)

- [ ] **Step 1: 改用明確條件**

將 `if runnerWs != ws` 改為：
```go
isWorktree := cfg.Isolation == "worktree" && runnerWs.Root != ws.Root
```

在 generatePrompt 裡：
```go
if len(cfg.Workspace.Repos) > 0 {
    if isWorktree {
        // worktree 模式...
    } else {
        repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
    }
}
```

注意：runLoop 裡的 `runnerWs != ws` 也要一起改（syncFeatureToWorktree 呼叫處）。

- [ ] **Step 2: 跑測試**

```bash
go build ./cmd/4x && go test ./cmd/4x/ -v -count=1
```

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/run.go
git commit -m "fix(F049): replace pointer comparison with explicit worktree detection in generatePrompt"
```

---

### Task 8: Remove dead guard.CaptureBaseline

**Files:**
- Modify: `internal/guard/check.go` (remove function)
- Modify: `internal/guard/check_test.go` (remove tests for it)

- [ ] **Step 1: 刪除 guard.CaptureBaseline 函式和測試**

`internal/guard/check.go` — 刪除 `func CaptureBaseline(...)` 及其所有程式碼。
`internal/guard/check_test.go` — 刪除 `TestCaptureBaseline*` 相關測試。

- [ ] **Step 2: 確認無其他 caller**

```bash
grep -rn 'guard\.CaptureBaseline' --include='*.go'
```
應只剩測試檔（會被刪除）。

- [ ] **Step 3: 跑測試**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/guard/
git commit -m "refactor(F049): remove dead guard.CaptureBaseline — replaced by gitops.Ops"
```

---

### Task 9: Final verification

- [ ] **Step 1: 全面驗證**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
make check-docs-sync BASE=HEAD~8
make check-i18n
```

- [ ] **Step 2: 確認所有 feature YAML 可讀取**

```bash
./bin/4x status F008-backlog-source-of-truth
./bin/4x status F001-state-tests
./bin/4x status F025-worktree-isolation-auto-c
```
