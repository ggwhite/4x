# 4x clean — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `4x clean` CLI 命令與 dashboard Clean 按鈕，清理已完成 feature 的 workspace artifacts。

**Architecture:** 在 `internal/protocol/workspace.go` 新增 `CleanableFeatures()` 和 `CleanFeature()` 方法，`cmd/4x/clean.go` 接 CLI，`internal/server/server.go` 接 API，dashboard UI 在 settings 旁加按鈕。

**Tech Stack:** Go (Cobra CLI, net/http), JavaScript (vanilla, embedded static)

---

### Task 1: Protocol 層 — CleanableFeatures + CleanFeature

**Files:**
- Modify: `internal/protocol/workspace.go`
- Test: `internal/protocol/workspace_test.go`

- [ ] **Step 1: 寫 CleanCandidate 型別和 CleanableFeatures 的測試**

在 `internal/protocol/workspace_test.go` 末尾新增：

```go
func TestCleanableFeatures(t *testing.T) {
	ws := setupWorkspace(t)

	// done feature（有 state.json + workspace dir）
	doneF := Feature{ID: "F001-done", Name: "Done Feature", Status: StatusDone}
	if err := ws.SaveFeature(doneF); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F001-done"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F001-done", State{
		FeatureID: "F001-done", Phase: PhaseDone, Active: false,
	}); err != nil {
		t.Fatal(err)
	}
	// 寫一些假 log 檔
	logsDir := filepath.Join(ws.FeatureDir("F001-done"), "logs")
	os.MkdirAll(logsDir, 0o755)
	os.WriteFile(filepath.Join(logsDir, "round-1-coder.stream.jsonl"), make([]byte, 1024), 0o644)

	// abandoned feature
	abandonedF := Feature{ID: "F002-abandoned", Name: "Abandoned", Status: StatusAbandoned}
	if err := ws.SaveFeature(abandonedF); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F002-abandoned"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F002-abandoned", State{
		FeatureID: "F002-abandoned", Phase: PhaseAbandoned, Active: false,
	}); err != nil {
		t.Fatal(err)
	}

	// in-progress feature（不可清理）
	activeF := Feature{ID: "F003-active", Name: "Active", Status: StatusInProgress}
	if err := ws.SaveFeature(activeF); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F003-active"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F003-active", State{
		FeatureID: "F003-active", Phase: PhaseCoding, Active: true, Pid: os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	// done 但沒 workspace dir 的 feature（跳過）
	noWorkspaceF := Feature{ID: "F004-no-ws", Name: "No Workspace", Status: StatusDone}
	ws.SaveFeature(noWorkspaceF)

	candidates, err := ws.CleanableFeatures()
	if err != nil {
		t.Fatalf("CleanableFeatures: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(candidates))
	}
	if candidates[0].FeatureID != "F001-done" {
		t.Errorf("candidates[0].FeatureID = %q, want F001-done", candidates[0].FeatureID)
	}
	if candidates[0].Size < 1024 {
		t.Errorf("candidates[0].Size = %d, want >= 1024", candidates[0].Size)
	}
	if candidates[1].FeatureID != "F002-abandoned" {
		t.Errorf("candidates[1].FeatureID = %q, want F002-abandoned", candidates[1].FeatureID)
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./internal/protocol/ -run TestCleanableFeatures -v`
Expected: FAIL — `CleanableFeatures` 未定義

- [ ] **Step 3: 實作 CleanCandidate 型別和 CleanableFeatures**

在 `internal/protocol/workspace.go` 新增（放在 `InitFeatureDir` 後面）：

```go
// CleanCandidate 描述一個可清理的 feature workspace。
type CleanCandidate struct {
	FeatureID string
	Size      int64
}

// CleanableFeatures 列出所有可清理的 feature（done 或 abandoned、非 active、有 workspace 目錄）。
func (w *Workspace) CleanableFeatures() ([]CleanCandidate, error) {
	features, err := w.ListFeatures()
	if err != nil {
		return nil, err
	}

	var candidates []CleanCandidate
	for _, f := range features {
		if f.Status != StatusDone && f.Status != StatusAbandoned {
			continue
		}
		dir := w.FeatureDir(f.ID)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}

		s, err := w.ReadState(f.ID)
		if err == nil {
			w.ReconcileActive(f.ID, &s)
			if s.Active {
				continue
			}
		}

		size := dirSize(dir)
		candidates = append(candidates, CleanCandidate{FeatureID: f.ID, Size: size})
	}
	return candidates, nil
}

func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
```

- [ ] **Step 4: 跑測試確認 pass**

Run: `go test ./internal/protocol/ -run TestCleanableFeatures -v`
Expected: PASS

- [ ] **Step 5: 寫 CleanFeature 的測試**

在 `internal/protocol/workspace_test.go` 末尾新增：

```go
func TestCleanFeature(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{ID: "F010-clean-me", Name: "Clean Me", Status: StatusDone}
	ws.SaveFeature(f)
	ws.InitFeatureDir("F010-clean-me")
	ws.WriteState("F010-clean-me", State{
		FeatureID: "F010-clean-me", Phase: PhaseDone, Active: false,
	})
	logsDir := filepath.Join(ws.FeatureDir("F010-clean-me"), "logs")
	os.MkdirAll(logsDir, 0o755)
	os.WriteFile(filepath.Join(logsDir, "stream.jsonl"), make([]byte, 2048), 0o644)

	freed, err := ws.CleanFeature("F010-clean-me")
	if err != nil {
		t.Fatalf("CleanFeature: %v", err)
	}
	if freed < 2048 {
		t.Errorf("freed = %d, want >= 2048", freed)
	}

	// workspace dir 應該不存在
	if _, err := os.Stat(ws.FeatureDir("F010-clean-me")); !os.IsNotExist(err) {
		t.Error("feature dir still exists after clean")
	}

	// feature YAML 應該仍在
	if _, err := ws.LoadFeature("F010-clean-me"); err != nil {
		t.Errorf("feature YAML should still exist: %v", err)
	}
}

func TestCleanFeature_RejectsActive(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{ID: "F011-active", Name: "Active", Status: StatusInProgress}
	ws.SaveFeature(f)
	ws.InitFeatureDir("F011-active")
	ws.WriteState("F011-active", State{
		FeatureID: "F011-active", Phase: PhaseCoding, Active: true, Pid: os.Getpid(),
	})

	_, err := ws.CleanFeature("F011-active")
	if err == nil {
		t.Fatal("expected error for active feature")
	}
}

func TestCleanFeature_RejectsNonTerminal(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{ID: "F012-blocked", Name: "Blocked", Status: StatusBlocked}
	ws.SaveFeature(f)
	ws.InitFeatureDir("F012-blocked")
	ws.WriteState("F012-blocked", State{
		FeatureID: "F012-blocked", Phase: PhaseBlocked, Active: false,
	})

	_, err := ws.CleanFeature("F012-blocked")
	if err == nil {
		t.Fatal("expected error for non-terminal feature")
	}
}
```

- [ ] **Step 6: 跑測試確認 fail**

Run: `go test ./internal/protocol/ -run TestCleanFeature -v`
Expected: FAIL — `CleanFeature` 未定義

- [ ] **Step 7: 實作 CleanFeature**

在 `internal/protocol/workspace.go`，接在 `CleanableFeatures` 後面新增：

```go
// CleanFeature 刪除指定 feature 的 workspace 目錄，回傳釋放的位元組數。
// 僅允許 done 或 abandoned 且非 active 的 feature。
func (w *Workspace) CleanFeature(featureID string) (int64, error) {
	f, err := w.LoadFeature(featureID)
	if err != nil {
		return 0, fmt.Errorf("load feature %s: %w", featureID, err)
	}
	if f.Status != StatusDone && f.Status != StatusAbandoned {
		return 0, fmt.Errorf("feature %s is %s, only done/abandoned can be cleaned", featureID, f.Status)
	}

	dir := w.FeatureDir(featureID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, fmt.Errorf("no workspace directory for %s", featureID)
	}

	s, err := w.ReadState(featureID)
	if err == nil {
		w.ReconcileActive(featureID, &s)
		if s.Active {
			return 0, fmt.Errorf("feature %s is still active (pid %d)", featureID, s.Pid)
		}
	}

	size := dirSize(dir)
	if err := os.RemoveAll(dir); err != nil {
		return 0, fmt.Errorf("remove %s: %w", dir, err)
	}
	return size, nil
}

// HumanSize 將位元組數轉為人類可讀格式（如 "3.9M"）。
func HumanSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0fK", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
```

- [ ] **Step 8: 跑所有 protocol 測試確認 pass**

Run: `go test ./internal/protocol/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add internal/protocol/workspace.go internal/protocol/workspace_test.go
git commit -m "feat(protocol): add CleanableFeatures, CleanFeature, HumanSize"
```

---

### Task 2: CLI — `4x clean` 命令

**Files:**
- Create: `cmd/4x/clean.go`
- Modify: `cmd/4x/main.go` (加 `newCleanCmd()`)
- Test: `cmd/4x/cli_test.go`

- [ ] **Step 1: 寫 CLI 整合測試**

在 `cmd/4x/cli_test.go` 末尾新增：

```go
func TestClean_DryRun(t *testing.T) {
	ws := setupCLIWorkspace(t)

	// 建立 done feature 含 workspace dir
	f := protocol.Feature{ID: "F001-test-done", Name: "Test Done", Status: protocol.StatusDone}
	ws.SaveFeature(f)
	ws.InitFeatureDir("F001-test-done")
	ws.WriteState("F001-test-done", protocol.State{
		FeatureID: "F001-test-done", Phase: protocol.PhaseDone, Active: false,
	})
	logsDir := filepath.Join(ws.FeatureDir("F001-test-done"), "logs")
	os.MkdirAll(logsDir, 0o755)
	os.WriteFile(filepath.Join(logsDir, "test.log"), make([]byte, 512), 0o644)

	out := runCLI(t, ws.Root, "clean", "--dry-run")
	if !strings.Contains(out, "F001-test-done") {
		t.Errorf("dry-run output should list feature, got: %s", out)
	}

	// workspace 應該仍然存在（dry-run 不刪）
	if _, err := os.Stat(ws.FeatureDir("F001-test-done")); os.IsNotExist(err) {
		t.Error("dry-run should not delete workspace dir")
	}
}

func TestClean_Force(t *testing.T) {
	ws := setupCLIWorkspace(t)

	f := protocol.Feature{ID: "F002-test-done", Name: "Force Clean", Status: protocol.StatusDone}
	ws.SaveFeature(f)
	ws.InitFeatureDir("F002-test-done")
	ws.WriteState("F002-test-done", protocol.State{
		FeatureID: "F002-test-done", Phase: protocol.PhaseDone, Active: false,
	})

	out := runCLI(t, ws.Root, "clean", "--force")
	if !strings.Contains(out, "Cleaned") {
		t.Errorf("force output should contain 'Cleaned', got: %s", out)
	}

	if _, err := os.Stat(ws.FeatureDir("F002-test-done")); !os.IsNotExist(err) {
		t.Error("force clean should delete workspace dir")
	}
}

func TestClean_NothingToClean(t *testing.T) {
	ws := setupCLIWorkspace(t)

	out := runCLI(t, ws.Root, "clean", "--dry-run")
	if !strings.Contains(out, "Nothing to clean") {
		t.Errorf("should say nothing to clean, got: %s", out)
	}
}
```

先確認 `setupCLIWorkspace` 和 `runCLI` 這兩個 test helper 存在。若不存在，參考 `cli_test.go` 頂部的 pattern 自行確認。

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./cmd/4x/ -run TestClean -v`
Expected: FAIL — `newCleanCmd` 未定義

- [ ] **Step 3: 建立 `cmd/4x/clean.go`**

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newCleanCmd() *cobra.Command {
	var dryRun, force bool

	cmd := &cobra.Command{
		Use:   "clean [feature-id]",
		Short: "Remove workspace artifacts for completed features",
		Long: `Clean up .4x/{feature-id}/ directories for done or abandoned features.

Removes logs, rounds, reports, and state files.
Feature definitions (.4x/features/*.yaml) are always preserved.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				return cleanSingle(ws, args[0], dryRun, force)
			}
			return cleanAll(ws, dryRun, force)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "List cleanable features without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func cleanSingle(ws *protocol.Workspace, prefix string, dryRun, force bool) error {
	featureID, err := ws.ResolveFeatureID(prefix)
	if err != nil {
		return err
	}

	candidates, err := ws.CleanableFeatures()
	if err != nil {
		return err
	}

	var target *protocol.CleanCandidate
	for i := range candidates {
		if candidates[i].FeatureID == featureID {
			target = &candidates[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("feature %s is not cleanable (must be done/abandoned with workspace)", featureID)
	}

	if dryRun {
		fmt.Printf("  %-30s %s\n", target.FeatureID, protocol.HumanSize(target.Size))
		return nil
	}

	if !force {
		printCleanWarning()
		fmt.Printf("  %-30s %s\n", target.FeatureID, protocol.HumanSize(target.Size))
		fmt.Println()
		if !confirmPrompt("Clean this feature?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	freed, err := ws.CleanFeature(featureID)
	if err != nil {
		return err
	}
	fmt.Printf("Cleaned %s, freed %s\n", featureID, protocol.HumanSize(freed))
	return nil
}

func cleanAll(ws *protocol.Workspace, dryRun, force bool) error {
	candidates, err := ws.CleanableFeatures()
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		fmt.Println("Nothing to clean.")
		return nil
	}

	var total int64
	for _, c := range candidates {
		total += c.Size
	}

	if dryRun {
		printCleanWarning()
		printCandidates(candidates, total)
		return nil
	}

	if !force {
		printCleanWarning()
		printCandidates(candidates, total)
		fmt.Println()
		if !confirmPrompt("Clean all?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var freed int64
	var cleaned int
	for _, c := range candidates {
		f, err := ws.CleanFeature(c.FeatureID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skip %s: %v\n", c.FeatureID, err)
			continue
		}
		freed += f
		cleaned++
	}
	fmt.Printf("Cleaned %d features, freed %s\n", cleaned, protocol.HumanSize(freed))
	return nil
}

func printCleanWarning() {
	fmt.Println("⚠ Warning: Cleaned features will lose detailed logs, reports, and round")
	fmt.Println("  history in the dashboard. Feature definitions and status are preserved.")
	fmt.Println()
}

func printCandidates(candidates []protocol.CleanCandidate, total int64) {
	fmt.Printf("Found %d cleanable features (done/abandoned):\n", len(candidates))
	for _, c := range candidates {
		fmt.Printf("  %-30s %s\n", c.FeatureID, protocol.HumanSize(c.Size))
	}
	fmt.Printf("  %-30s %s\n", "Total:", protocol.HumanSize(total))
}

func confirmPrompt(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "y"
}
```

- [ ] **Step 4: 在 `cmd/4x/main.go` 註冊新命令**

在 `root.AddCommand(` 區塊中加入 `newCleanCmd()`：

```go
root.AddCommand(
	// ...existing commands...
	newSubtaskCmd(),
	newCleanCmd(),
)
```

- [ ] **Step 5: 確認 build 通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 6: 跑 CLI 測試確認 pass**

Run: `go test ./cmd/4x/ -run TestClean -v`
Expected: ALL PASS

注意：`setupCLIWorkspace` 和 `runCLI` helper 若不存在或簽名不同，需根據 `cli_test.go` 已有 pattern 調整測試寫法。實作者先讀 `cli_test.go` 頂部找到正確 helper。

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/clean.go cmd/4x/main.go cmd/4x/cli_test.go
git commit -m "feat(cli): add 4x clean command"
```

---

### Task 3: Server API — POST /api/clean

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: 寫 server 測試**

在 `internal/server/server_test.go` 末尾新增：

```go
func TestPostClean(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 建立 done feature 含 workspace
	doneF := protocol.Feature{ID: "clean-done", Name: "Clean Done", Status: protocol.StatusDone}
	ws.SaveFeature(doneF)
	ws.InitFeatureDir("clean-done")
	ws.WriteState("clean-done", protocol.State{
		FeatureID: "clean-done", Phase: protocol.PhaseDone, Active: false,
	})
	logsDir := filepath.Join(ws.FeatureDir("clean-done"), "logs")
	os.MkdirAll(logsDir, 0o755)
	os.WriteFile(filepath.Join(logsDir, "test.log"), make([]byte, 2048), 0o644)

	handler := NewMux(ws, nil)
	rec := serveRequest(t, handler, http.MethodPost, "/api/clean", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var resp cleanResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Cleaned != 1 {
		t.Errorf("cleaned = %d, want 1", resp.Cleaned)
	}
	if resp.Freed < 2048 {
		t.Errorf("freed = %d, want >= 2048", resp.Freed)
	}
	if len(resp.Features) != 1 || resp.Features[0] != "clean-done" {
		t.Errorf("features = %v, want [clean-done]", resp.Features)
	}

	// workspace 應該被刪除
	if _, err := os.Stat(ws.FeatureDir("clean-done")); !os.IsNotExist(err) {
		t.Error("workspace dir should be removed after clean")
	}
}

func TestPostClean_NothingToClean(t *testing.T) {
	ws := setupServerWorkspace(t)

	handler := NewMux(ws, nil)
	rec := serveRequest(t, handler, http.MethodPost, "/api/clean", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp cleanResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Cleaned != 0 {
		t.Errorf("cleaned = %d, want 0", resp.Cleaned)
	}
}

func TestPostClean_MethodNotAllowed(t *testing.T) {
	ws := setupServerWorkspace(t)
	handler := NewMux(ws, nil)
	rec := serveRequest(t, handler, http.MethodGet, "/api/clean", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/clean status = %d, want 405", rec.Code)
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./internal/server/ -run TestPostClean -v`
Expected: FAIL — route 不存在（404）

- [ ] **Step 3: 在 server.go 加 handler 和路由**

在 `internal/server/server.go` 的 `NewMux` 函數中，在 `mux.HandleFunc("/api/done", ...)` 後面加入：

```go
mux.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handlePostClean(ws, w)
})
```

在檔案末尾（或 `handlePostDone` 附近）新增 handler：

```go
type cleanResponse struct {
	Cleaned   int      `json:"cleaned"`
	Freed     int64    `json:"freed"`
	FreedText string   `json:"freed_human"`
	Features  []string `json:"features"`
}

func handlePostClean(ws *protocol.Workspace, w http.ResponseWriter) {
	candidates, err := ws.CleanableFeatures()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := cleanResponse{Features: []string{}}
	for _, c := range candidates {
		freed, err := ws.CleanFeature(c.FeatureID)
		if err != nil {
			continue
		}
		resp.Cleaned++
		resp.Freed += freed
		resp.Features = append(resp.Features, c.FeatureID)
	}
	resp.FreedText = protocol.HumanSize(resp.Freed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: 跑測試確認 pass**

Run: `go test ./internal/server/ -run TestPostClean -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): add POST /api/clean endpoint"
```

---

### Task 4: Dashboard UI — 佈局調整 + Clean 按鈕

**Files:**
- Modify: `internal/server/static/index.html`
- Modify: `internal/server/static/ui.js`
- Modify: `internal/server/static/locales/en.json`
- Modify: `internal/server/static/locales/zh-TW.json`
- Modify: `internal/server/static/locales/zh-CN.json`
- Modify: `internal/server/static/locales/ja.json`
- Modify: `internal/server/static/locales/ko.json`
- Modify: `internal/server/static/locales/es.json`

本 task 除了加 Clean 按鈕，同時修正既有 UI 佈局：

**佈局變更：**
1. **移除 main content dashboard header 的齒輪 icon**（`ui.js` 約第 253 行）— Project Settings 已在 sidebar 有入口
2. **Global Settings icon 移到 tab bar 右側**（`index.html` 第 18 行 `#add-tab-btn` 旁邊）— 加一個齒輪 svg icon 按鈕 `onclick="openGlobalSettings()"`
3. **放大 `+`（Add Project）按鈕**（`index.html` 第 18 行）— 從 `text-sm` / plain `+` 改為 svg folder-plus icon，width/height 20
4. **Sidebar 齒輪維持不變** — 有 active project 時開 Project Settings，無時開 Global Settings
5. **Sidebar 齒輪旁加 Clean 按鈕**（`index.html` 第 30-32 行區域）— 垃圾桶 svg icon

- [ ] **Step 1: 在所有 locale 檔加 clean 相關 i18n key**

在 `en.json` 加入：

```json
"clean.button": "Clean",
"clean.title": "Clean Project Artifacts",
"clean.warning": "Cleaned features will lose detailed logs, reports, and round history in the dashboard. Feature definitions and status are preserved.",
"clean.nothingToClean": "Nothing to clean — no completed features with workspace artifacts.",
"clean.confirm": "Clean all?",
"clean.success": "Cleaned {count} features, freed {size}",
```

`zh-TW.json`：
```json
"clean.button": "清理",
"clean.title": "清理專案 Artifacts",
"clean.warning": "清理後的 feature 將失去 dashboard 中的詳細 logs、reports 和 round 歷程。Feature 定義與狀態會保留。",
"clean.nothingToClean": "沒有需要清理的 — 沒有已完成且有 workspace artifacts 的 feature。",
"clean.confirm": "全部清理？",
"clean.success": "已清理 {count} 個 feature，釋放 {size}",
```

`zh-CN.json`：
```json
"clean.button": "清理",
"clean.title": "清理项目工件",
"clean.warning": "清理后的 feature 将丢失面板中的详细日志、报告和轮次记录。Feature 定义和状态保留。",
"clean.nothingToClean": "没有需要清理的 — 没有已完成且有工件的 feature。",
"clean.confirm": "全部清理？",
"clean.success": "已清理 {count} 个 feature，释放 {size}",
```

`ja.json`：
```json
"clean.button": "クリーン",
"clean.title": "プロジェクト成果物のクリーン",
"clean.warning": "クリーンされた feature はダッシュボードでの詳細なログ、レポート、ラウンド履歴を失います。Feature 定義とステータスは保持されます。",
"clean.nothingToClean": "クリーン対象なし — ワークスペース成果物を持つ完了済み feature がありません。",
"clean.confirm": "すべてクリーンしますか？",
"clean.success": "{count} 件の feature をクリーンし、{size} を解放しました",
```

`ko.json`：
```json
"clean.button": "정리",
"clean.title": "프로젝트 아티팩트 정리",
"clean.warning": "정리된 feature는 대시보드에서 상세 로그, 보고서, 라운드 기록을 잃게 됩니다. Feature 정의와 상태는 유지됩니다.",
"clean.nothingToClean": "정리할 항목 없음 — 워크스페이스 아티팩트가 있는 완료된 feature가 없습니다.",
"clean.confirm": "모두 정리하시겠습니까?",
"clean.success": "{count}개 feature 정리, {size} 해제",
```

`es.json`：
```json
"clean.button": "Limpiar",
"clean.title": "Limpiar artefactos del proyecto",
"clean.warning": "Las features limpiadas perderán los registros detallados, informes e historial de rondas en el panel. Las definiciones y estados de features se conservan.",
"clean.nothingToClean": "Nada que limpiar — no hay features completadas con artefactos de workspace.",
"clean.confirm": "¿Limpiar todo?",
"clean.success": "{count} features limpiadas, {size} liberados",
```

- [ ] **Step 2: 修改 `index.html` — tab bar 佈局**

在 `index.html` 第 18 行，將 `#add-tab-btn` 和新的 Global Settings 按鈕改為：

```html
<button id="add-tab-btn" onclick="showProjectPicker()" class="px-2 py-1.5 transition-colors" style="color:var(--text-4)" title="Add project" data-i18n-title="picker.addProject">
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/><line x1="12" y1="11" x2="12" y2="17"/><line x1="9" y1="14" x2="15" y2="14"/></svg>
</button>
<button onclick="openGlobalSettings()" class="px-2 py-1.5 transition-colors" style="color:var(--text-4)" title="Global Settings (⌘⇧,)" data-i18n-title="settings.titleShortcut">
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12.22 2h-.44a2 2 0 00-2 2v.18a2 2 0 01-1 1.73l-.43.25a2 2 0 01-2 0l-.15-.08a2 2 0 00-2.73.73l-.22.38a2 2 0 00.73 2.73l.15.1a2 2 0 011 1.72v.51a2 2 0 01-1 1.74l-.15.09a2 2 0 00-.73 2.73l.22.38a2 2 0 002.73.73l.15-.08a2 2 0 012 0l.43.25a2 2 0 011 1.73V20a2 2 0 002 2h.44a2 2 0 002-2v-.18a2 2 0 011-1.73l.43-.25a2 2 0 012 0l.15.08a2 2 0 002.73-.73l.22-.39a2 2 0 00-.73-2.73l-.15-.08a2 2 0 01-1-1.74v-.5a2 2 0 011-1.74l.15-.09a2 2 0 00.73-2.73l-.22-.38a2 2 0 00-2.73-.73l-.15.08a2 2 0 01-2 0l-.43-.25a2 2 0 01-1-1.73V4a2 2 0 00-2-2z"/><circle cx="12" cy="12" r="3"/></svg>
</button>
```

- [ ] **Step 3: 修改 `index.html` — sidebar header 加 Clean 按鈕**

在 sidebar header（第 30-32 行）的 settings gear `</button>` 之後加：

```html
<button onclick="event.stopPropagation();openCleanDialog()" title="Clean" data-i18n-title="clean.button" style="color:var(--text-4)" class="ml-1 hover:text-white transition-colors">
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>
</button>
```

- [ ] **Step 4: 修改 `ui.js` — 移除 main content dashboard header 的齒輪 icon**

在 `ui.js` 約第 253 行，找到 `el.innerHTML = ...` 中的 settings gear 按鈕 HTML：

```
<button onclick="activeProjectId?openProjectSettings():openGlobalSettings()" title="${t('settings.titleShortcut')}" class="ml-2 transition-colors" style="color:var(--text-3)"><svg width="16" height="16" ...></svg></button>
```

整段刪除（從 `<button onclick="activeProjectId?openProjectSettings()` 到對應的 `</button>`）。

- [ ] **Step 5: 在 `ui.js` 末尾加 Clean dialog 函數**

```javascript
async function openCleanDialog() {
  const base = apiBase();
  const title = t('clean.title');
  const msg = t('clean.warning');

  const overlay = document.createElement('div');
  overlay.className = 'modal-backdrop open';
  const dialog = document.createElement('div');
  dialog.className = 'modal-panel fade-in';
  dialog.style.cssText = 'width:420px';
  dialog.innerHTML = `
    <div style="padding:20px 24px 12px">
      <div style="font-size:15px;font-weight:700;margin-bottom:8px">${esc(title)}</div>
      <div style="font-size:13px;color:var(--text-2);line-height:1.5">⚠ ${esc(msg)}</div>
    </div>
    <div style="padding:12px 24px 20px;display:flex;justify-content:flex-end;gap:8px">
      <button id="clean-cancel-btn" style="padding:8px 16px;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;font-size:13px;color:var(--text-2);cursor:pointer">${t('common.cancel')}</button>
      <button id="clean-confirm-btn" style="padding:8px 16px;background:#dc2626;color:#fff;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer">${t('common.confirm')}</button>
    </div>`;
  overlay.appendChild(dialog);
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
  dialog.querySelector('#clean-cancel-btn').onclick = close;
  dialog.querySelector('#clean-confirm-btn').onclick = async function() {
    this.disabled = true;
    this.textContent = '...';
    try {
      const resp = await fetch(base + '/api/clean', { method: 'POST' });
      const data = await resp.json();
      close();
      if (data.cleaned > 0) {
        showToast(t('clean.success').replace('{count}', data.cleaned).replace('{size}', data.freed_human));
      } else {
        showToast(t('clean.nothingToClean'));
      }
      load();
    } catch (e) {
      showToast(t('toast.failed').replace('{error}', e.message));
      close();
    }
  };
}
```

- [ ] **Step 6: 確認 build 通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 7: 手動測試 dashboard**

1. `bin/4x live` 啟動 dashboard
2. 確認 tab bar 右側有放大的 folder+ icon（Add Project）和齒輪 icon（Global Settings）
3. 確認 sidebar header 的齒輪旁有垃圾桶 icon（Clean）
4. 確認 main content dashboard overview 區域**沒有**齒輪 icon
5. 點 Clean → 出現確認 dialog + 警告文字
6. 點 Confirm → 呼叫 API → toast 顯示結果
7. 切換語言確認 i18n 正確

- [ ] **Step 8: Commit**

```bash
git add internal/server/static/index.html internal/server/static/ui.js internal/server/static/locales/*.json
git commit -m "feat(dashboard): add Clean button, move Global Settings to tab bar, enlarge Add Project icon"
```

---

### Task 5: 全域測試 + docs 更新

**Files:**
- Check: all test suites
- Modify: `docs/guide/cli.md` (如果存在，加 clean 命令說明)

- [ ] **Step 1: 跑全部測試**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: ALL PASS

- [ ] **Step 2: 跑 check-docs-sync**

Run: `make check-docs-sync`

若輸出 `NEEDS_UPDATE` 指向 cli.md，在該文件加入 `clean` 命令說明。

- [ ] **Step 3: 跑 check-i18n**

Run: `make check-i18n`

若輸出 `ERROR: missing keys`，補齊缺漏的 locale key。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: add clean command to CLI reference"
```
