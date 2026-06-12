# F029 — Auto-merge on Done Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `4x done` 自動 merge worktree branch 回 main 並清理，衝突時引導 user 用 `4x merge` 手動完成

**Architecture:** 新增 `internal/worktree/` package 封裝 merge/cleanup 邏輯（CLI 和 server 共用）。`done.go` 在 state transition 後呼叫 merge；新增 `merge.go` 處理衝突後的手動完成；server endpoint 同步增強。

**Tech Stack:** Go (os/exec git commands), Cobra CLI, net/http

---

### Task 1: 新增 `internal/worktree/` package

**Files:**
- Create: `internal/worktree/merge.go`
- Create: `internal/worktree/merge_test.go`

- [ ] **Step 1: 寫測試**

```go
package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) (mainDir string, wtDir string, featureID string) {
	t.Helper()
	featureID = "F099-test"
	mainDir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", mainDir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	run("init")
	run("commit", "--allow-empty", "-m", "init")

	wtDir = filepath.Join(mainDir, ".worktrees", "4x", featureID)
	branch := "4x/" + featureID
	os.MkdirAll(filepath.Dir(wtDir), 0o755)
	run("worktree", "add", wtDir, "-b", branch)

	// worktree 上加一個 commit
	os.WriteFile(filepath.Join(wtDir, "new.txt"), []byte("hello"), 0o644)
	wtRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", wtDir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	wtRun("add", "new.txt")
	wtRun("commit", "-m", "feat: add new.txt")

	return mainDir, wtDir, featureID
}

func TestMerge_Success(t *testing.T) {
	mainDir, _, featureID := setupTestRepo(t)

	result := Merge(mainDir, featureID, "Test Feature")
	if result.Conflict {
		t.Fatalf("expected no conflict, got conflicts: %v", result.Files)
	}

	// worktree 和 branch 應該被清理
	wtDir := filepath.Join(mainDir, ".worktrees", "4x", featureID)
	if _, err := os.Stat(wtDir); err == nil {
		t.Error("worktree directory should be removed")
	}

	// new.txt 應該在 main 上
	if _, err := os.Stat(filepath.Join(mainDir, "new.txt")); err != nil {
		t.Error("new.txt should exist on main after merge")
	}
}

func TestMerge_NoWorktree(t *testing.T) {
	mainDir := t.TempDir()
	exec.Command("git", "-C", mainDir, "init").Run()
	exec.Command("git", "-C", mainDir, "commit", "--allow-empty", "-m", "init").Run()

	result := Merge(mainDir, "F099-nonexistent", "Test")
	if result.Conflict {
		t.Error("no worktree should mean no conflict")
	}
	if !result.Skipped {
		t.Error("should be skipped when no worktree exists")
	}
}

func TestMerge_Conflict(t *testing.T) {
	mainDir, _, featureID := setupTestRepo(t)

	// main 上也建同檔，製造衝突
	os.WriteFile(filepath.Join(mainDir, "new.txt"), []byte("conflict"), 0o644)
	exec.Command("git", "-C", mainDir, "add", "new.txt").Run()
	exec.Command("git", "-C", mainDir, "commit", "-m", "conflict on main").Run()

	result := Merge(mainDir, featureID, "Test Feature")
	if !result.Conflict {
		t.Fatal("expected conflict")
	}
	if len(result.Files) == 0 {
		t.Error("should report conflicting files")
	}

	// worktree 應該保留
	wtDir := filepath.Join(mainDir, ".worktrees", "4x", featureID)
	if _, err := os.Stat(wtDir); err != nil {
		t.Error("worktree should be preserved on conflict")
	}
}

func TestCleanup_Success(t *testing.T) {
	mainDir, wtDir, featureID := setupTestRepo(t)

	err := Cleanup(mainDir, featureID)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if _, err := os.Stat(wtDir); err == nil {
		t.Error("worktree directory should be removed")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/worktree/ -v`
Expected: FAIL — package 不存在

- [ ] **Step 3: 實作 merge.go**

```go
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MergeResult 描述 merge 操作的結果
type MergeResult struct {
	Skipped  bool
	Conflict bool
	Files    []string
}

// Merge 嘗試將 worktree branch merge 回 main，成功則清理 worktree 和 branch。
// 衝突時 abort merge 並回傳衝突檔案列表，worktree 保留供手動解決。
// 若沒有 worktree（非 isolation 模式），回傳 Skipped=true。
func Merge(root, featureID, featureName string) MergeResult {
	wtDir := Dir(root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}

	branch := Branch(featureID)

	out, err := exec.Command("git", "-C", root, "merge", "--no-commit", branch).CombinedOutput()
	if err != nil {
		files := conflictFiles(root)
		exec.Command("git", "-C", root, "merge", "--abort").Run()
		return MergeResult{Conflict: true, Files: files}
	}
	_ = out

	msg := fmt.Sprintf("Merge branch '%s' — %s", branch, featureName)
	exec.Command("git", "-C", root, "commit", "--no-edit", "-m", msg).Run()

	Cleanup(root, featureID)
	return MergeResult{}
}

// Cleanup 移除 worktree 目錄和對應 branch
func Cleanup(root, featureID string) error {
	wtDir := Dir(root, featureID)
	branch := Branch(featureID)

	if out, err := exec.Command("git", "-C", root, "worktree", "remove", wtDir).CombinedOutput(); err != nil {
		exec.Command("git", "-C", root, "worktree", "remove", "--force", wtDir).Run()
		if _, statErr := os.Stat(wtDir); statErr == nil {
			return fmt.Errorf("worktree remove failed: %s", string(out))
		}
	}

	exec.Command("git", "-C", root, "branch", "-D", branch).Run()
	return nil
}

// Dir 回傳 worktree 目錄路徑
func Dir(root, featureID string) string {
	return filepath.Join(root, ".worktrees", "4x", featureID)
}

// Branch 回傳 worktree branch 名稱
func Branch(featureID string) string {
	return "4x/" + featureID
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

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/worktree/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/merge.go internal/worktree/merge_test.go
git commit -m "feat(F029): add internal/worktree package with Merge and Cleanup"
```

---

### Task 2: 更新 `4x done` 呼叫 merge

**Files:**
- Modify: `cmd/4x/done.go`

- [ ] **Step 1: 更新 done.go**

在 `import` 加 `"github.com/ggwhite/4x/internal/worktree"`。

把 `markDone` 函式的最後（`fmt.Printf` 之後、`return nil` 之前）加 merge 邏輯：

```go
func markDone(ws *protocol.Workspace, featureID string) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		return err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return err
	}

	syncFeatureStatus(ws, featureID, protocol.PhaseDone)

	ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})

	fmt.Printf("Feature %s marked as done.\n", featureID)

	f, _ := ws.LoadFeature(featureID)
	name := featureID
	if f.Name != "" {
		name = f.Name
	}
	result := worktree.Merge(ws.Root, featureID, name)
	if result.Skipped {
		return nil
	}
	if result.Conflict {
		fmt.Println("Merge conflict — resolve manually:")
		for _, file := range result.Files {
			fmt.Printf("  conflict: %s\n", file)
		}
		fmt.Printf("Worktree: %s\n", worktree.Dir(ws.Root, featureID))
		fmt.Printf("After resolving: 4x merge %s\n", featureID)
		return nil
	}
	fmt.Printf("Merged and cleaned up branch 4x/%s.\n", featureID)
	return nil
}
```

- [ ] **Step 2: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/done.go
git commit -m "feat(F029): 4x done auto-merges worktree branch"
```

---

### Task 3: 新增 `4x merge` subcommand

**Files:**
- Create: `cmd/4x/merge.go`
- Modify: `cmd/4x/main.go:36`

- [ ] **Step 1: 建立 merge.go**

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/worktree"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <feature-id>",
		Short: "Complete merge after resolving conflicts",
		Long:  "Use after '4x done' reported a merge conflict and you resolved it in the worktree.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			s, err := ws.ReadState(featureID)
			if err != nil {
				return fmt.Errorf("cannot read state for %s: %w", featureID, err)
			}
			if s.Phase != protocol.PhaseDone {
				return fmt.Errorf("feature %s is in phase %q, not done (run '4x done %s' first)", featureID, s.Phase, featureID)
			}

			wtDir := worktree.Dir(ws.Root, featureID)
			if _, err := os.Stat(wtDir); err != nil {
				return fmt.Errorf("no worktree found at %s", wtDir)
			}

			// commit 解完衝突的結果
			exec.Command("git", "-C", wtDir, "add", "-A").Run()
			if exec.Command("git", "-C", wtDir, "diff", "--cached", "--quiet").Run() != nil {
				f, _ := ws.LoadFeature(featureID)
				msg := fmt.Sprintf("fix(%s): resolve merge conflicts", featureID)
				if f.Name != "" {
					msg = fmt.Sprintf("fix(%s): resolve merge conflicts — %s", featureID, f.Name)
				}
				exec.Command("git", "-C", wtDir, "commit", "-m", msg).Run()
			}

			// merge 回 main
			branch := worktree.Branch(featureID)
			f, _ := ws.LoadFeature(featureID)
			name := featureID
			if f.Name != "" {
				name = f.Name
			}
			mergeMsg := fmt.Sprintf("Merge branch '%s' — %s", branch, name)

			out, err := exec.Command("git", "-C", ws.Root, "merge", branch, "-m", mergeMsg).CombinedOutput()
			if err != nil {
				return fmt.Errorf("merge still has conflicts: %s", string(out))
			}

			if err := worktree.Cleanup(ws.Root, featureID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: cleanup failed: %v\n", err)
			}

			fmt.Printf("Merged and cleaned up branch %s.\n", branch)
			return nil
		},
	}
}
```

- [ ] **Step 2: 註冊到 main.go**

在 `cmd/4x/main.go:37`（`newDoneCmd()` 後面）加：

```go
newMergeCmd(),
```

- [ ] **Step 3: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/merge.go cmd/4x/main.go
git commit -m "feat(F029): add 4x merge subcommand for conflict resolution"
```

---

### Task 4: 更新 Server API

**Files:**
- Modify: `internal/server/server.go:594-597`

- [ ] **Step 1: 新增 import**

在 `internal/server/server.go` 的 import 加：

```go
"github.com/ggwhite/4x/internal/worktree"
```

- [ ] **Step 2: 更新 handlePostDone response**

把 `handlePostDone` 結尾的：

```go
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"done"}`)
```

改成：

```go
	f, _ = ws.LoadFeature(req.ID)
	name := req.ID
	if f.Name != "" {
		name = f.Name
	}
	result := worktree.Merge(ws.Root, req.ID, name)

	w.Header().Set("Content-Type", "application/json")
	if result.Conflict {
		filesJSON, _ := json.Marshal(result.Files)
		fmt.Fprintf(w, `{"status":"done","merge_conflict":true,"conflicts":%s}`, filesJSON)
	} else if result.Skipped {
		fmt.Fprint(w, `{"status":"done","merged":false}`)
	} else {
		fmt.Fprint(w, `{"status":"done","merged":true}`)
	}
```

注意：此處重複 `ws.LoadFeature`（上面已 load 過一次設 `f.Status`），用同一個 `f` 變數即可，但 `f` 的 scope 在上面的 block 裡。把上面的 `f, err := ws.LoadFeature(req.ID)` 拿到的 `f` 留在外層 scope，或者在這裡重新 load。最簡單：直接用 `req.ID` 做 name fallback，不需要再 load。

修正版（不重複 load）：

```go
	name := req.ID
	if f.Name != "" {
		name = f.Name
	}
	result := worktree.Merge(ws.Root, req.ID, name)

	w.Header().Set("Content-Type", "application/json")
	if result.Conflict {
		filesJSON, _ := json.Marshal(result.Files)
		fmt.Fprintf(w, `{"status":"done","merge_conflict":true,"conflicts":%s}`, filesJSON)
	} else if result.Skipped {
		fmt.Fprint(w, `{"status":"done","merged":false}`)
	} else {
		fmt.Fprint(w, `{"status":"done","merged":true}`)
	}
```

這裡 `f` 就是上面 `ws.LoadFeature` 回來的，已在 scope 內。

- [ ] **Step 3: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(F029): POST /api/done auto-merges worktree"
```

---

### Task 5: 更新 Dashboard 前端

**Files:**
- Modify: `internal/server/static/index.html:492-497`

- [ ] **Step 1: 更新 markDone 函式**

把：

```js
async function markDone(fid) {
  if (!confirm('Mark '+fid+' as done?')) return;
  const res = await fetch(apiBase()+'/api/done', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id:fid})});
  if (!res.ok) { alert('Failed: '+(await res.text())); return; }
  load(); if (!current) renderDashboard(lastTasks);
}
```

改成：

```js
async function markDone(fid) {
  if (!confirm('Mark '+fid+' as done?')) return;
  const res = await fetch(apiBase()+'/api/done', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id:fid})});
  if (!res.ok) { alert('Failed: '+(await res.text())); return; }
  const data = await res.json();
  if (data.merge_conflict) {
    alert('Done, but merge has conflicts:\n\n'+data.conflicts.join('\n')+'\n\nResolve in worktree, then run: 4x merge '+fid);
  } else if (data.merged) {
    // merged + cleaned up
  }
  load(); if (!current) renderDashboard(lastTasks);
}
```

- [ ] **Step 2: 驗證編譯（embedded HTML）**

Run: `go build ./cmd/4x`
Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F029): dashboard shows merge conflict details"
```

---

### Task 6: 全量驗證

**Files:** 無新變更

- [ ] **Step 1: 跑全部 Go 測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 驗證 CLI help**

Run: `go run ./cmd/4x done --help`
Expected: 顯示 "Mark a pending-review feature as done"

Run: `go run ./cmd/4x merge --help`
Expected: 顯示 "Complete merge after resolving conflicts"

- [ ] **Step 3: 手動驗證（可選）**

建立一個 test feature 用 worktree isolation 跑一輪，確認 `4x done` 會自動 merge + cleanup。
