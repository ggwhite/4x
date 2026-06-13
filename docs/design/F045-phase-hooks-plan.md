# F045: Phase Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓 phase 轉換前後自動執行 shell command，支援全域與 per-feature 設定，失敗可選擇 block 或 warn。

**Architecture:** 新增 `internal/hook/` package 負責 hook 解析與執行。Config struct 加 `Hooks` 欄位，merge 邏輯支援 feature override 全域。CLI 層（`transition.go`、`run.go`）在 `state.Transition()` 前後呼叫 hook executor。State machine 不動。

**Tech Stack:** Go 標準庫（`os/exec`、`encoding/json`）

---

### Task 1: HookEntry 型別與 Config 欄位

**Files:**
- Create: `internal/hook/hook.go`
- Modify: `internal/protocol/types.go:240-252` (Config struct)
- Modify: `internal/protocol/types.go:74-87` (Feature struct)
- Test: `internal/hook/hook_test.go`

- [ ] **Step 1: 寫 failing test — HookEntry JSON 反序列化**

```go
// internal/hook/hook_test.go
package hook

import (
	"encoding/json"
	"testing"
)

func TestHookEntry_Unmarshal(t *testing.T) {
	raw := `{"run": "docker compose up -d", "on_fail": "warn"}`
	var h HookEntry
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	if h.Run != "docker compose up -d" {
		t.Errorf("Run = %q, want %q", h.Run, "docker compose up -d")
	}
	if h.OnFail != "warn" {
		t.Errorf("OnFail = %q, want %q", h.OnFail, "warn")
	}
}

func TestHookEntry_OnFail_DefaultBlock(t *testing.T) {
	raw := `{"run": "echo hello"}`
	var h HookEntry
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	if h.EffectiveOnFail() != "block" {
		t.Errorf("EffectiveOnFail() = %q, want %q", h.EffectiveOnFail(), "block")
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/hook/ -v -run TestHookEntry`
Expected: FAIL — package/type 不存在

- [ ] **Step 3: 實作 HookEntry struct**

```go
// internal/hook/hook.go
package hook

// HookEntry 描述一個 phase hook 的 shell command 與失敗策略
type HookEntry struct {
	Run    string `json:"run" yaml:"run"`
	OnFail string `json:"on_fail,omitempty" yaml:"on_fail,omitempty"`
}

// EffectiveOnFail 回傳實際的失敗策略，未設定時預設 "block"
func (h HookEntry) EffectiveOnFail() string {
	if h.OnFail == "" {
		return "block"
	}
	return h.OnFail
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/hook/ -v -run TestHookEntry`
Expected: PASS

- [ ] **Step 5: 在 Config 和 Feature 加 Hooks 欄位**

修改 `internal/protocol/types.go`：

Config struct 加欄位：
```go
Hooks map[string][]hook.HookEntry `json:"hooks,omitempty"`
```

Feature struct 加欄位：
```go
Hooks map[string][]hook.HookEntry `yaml:"hooks,omitempty" json:"hooks,omitempty"`
```

注意：`hook` package 要 import `github.com/ggwhite/4x/internal/hook`。

- [ ] **Step 6: Commit**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go internal/protocol/types.go
git commit -m "feat(F045): add HookEntry type and Hooks field to Config/Feature"
```

---

### Task 2: Hook 合併邏輯

**Files:**
- Modify: `internal/protocol/merge.go`
- Test: `internal/protocol/merge_test.go`

- [ ] **Step 1: 寫 failing test — feature hooks override 全域**

```go
// 加在 internal/protocol/merge_test.go
func TestMergeHooks_FeatureOverridesGlobal(t *testing.T) {
	global := map[string][]hook.HookEntry{
		"pre_coding":  {{Run: "global-setup", OnFail: "block"}},
		"post_testing": {{Run: "global-cleanup"}},
	}
	feature := map[string][]hook.HookEntry{
		"pre_coding": {{Run: "feature-setup", OnFail: "warn"}},
	}
	got := MergeHooks(global, feature)
	if len(got["pre_coding"]) != 1 || got["pre_coding"][0].Run != "feature-setup" {
		t.Errorf("pre_coding should be overridden by feature, got %+v", got["pre_coding"])
	}
	if len(got["post_testing"]) != 1 || got["post_testing"][0].Run != "global-cleanup" {
		t.Errorf("post_testing should be inherited from global, got %+v", got["post_testing"])
	}
}

func TestMergeHooks_BothNil(t *testing.T) {
	got := MergeHooks(nil, nil)
	if got != nil {
		t.Errorf("both nil should return nil, got %+v", got)
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestMergeHooks`
Expected: FAIL — `MergeHooks` 未定義

- [ ] **Step 3: 實作 MergeHooks**

在 `internal/protocol/merge.go` 加：

```go
// MergeHooks 合併全域和 feature 的 hooks，feature 同名 key 整組替換全域
func MergeHooks(global, feature map[string][]hook.HookEntry) map[string][]hook.HookEntry {
	if global == nil && feature == nil {
		return nil
	}
	merged := make(map[string][]hook.HookEntry)
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range feature {
		merged[k] = v
	}
	return merged
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestMergeHooks`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/merge.go internal/protocol/merge_test.go
git commit -m "feat(F045): add MergeHooks for feature-level hook override"
```

---

### Task 3: Hook 執行引擎

**Files:**
- Modify: `internal/hook/hook.go`
- Test: `internal/hook/hook_test.go`

- [ ] **Step 1: 寫 failing test — Execute 成功的 hook**

```go
func TestExecute_Success(t *testing.T) {
	hooks := []HookEntry{{Run: "echo hello", OnFail: "block"}}
	results, err := Execute(hooks, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", results[0].ExitCode)
	}
	if results[0].Status != "pass" {
		t.Errorf("Status = %q, want pass", results[0].Status)
	}
}
```

- [ ] **Step 2: 寫 failing test — block hook 失敗回傳 error**

```go
func TestExecute_BlockFail_ReturnsError(t *testing.T) {
	hooks := []HookEntry{{Run: "exit 1", OnFail: "block"}}
	_, err := Execute(hooks, t.TempDir())
	if err == nil {
		t.Fatal("expected error for block hook failure")
	}
}
```

- [ ] **Step 3: 寫 failing test — warn hook 失敗不回傳 error**

```go
func TestExecute_WarnFail_NoError(t *testing.T) {
	hooks := []HookEntry{{Run: "exit 1", OnFail: "warn"}}
	results, err := Execute(hooks, t.TempDir())
	if err != nil {
		t.Fatalf("warn hook should not return error, got: %v", err)
	}
	if results[0].Status != "fail" {
		t.Errorf("Status = %q, want fail", results[0].Status)
	}
}
```

- [ ] **Step 4: 寫 failing test — 多個 hook，第一個 block 失敗後不繼續**

```go
func TestExecute_BlockFail_StopsEarly(t *testing.T) {
	hooks := []HookEntry{
		{Run: "exit 1", OnFail: "block"},
		{Run: "echo second"},
	}
	results, err := Execute(hooks, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (stopped early), got %d", len(results))
	}
}
```

- [ ] **Step 5: 跑 test 確認都失敗**

Run: `go test ./internal/hook/ -v -run TestExecute`
Expected: FAIL — `Execute` 未定義

- [ ] **Step 6: 實作 Execute 函式**

```go
// Result 記錄單一 hook 的執行結果
type Result struct {
	Command  string  `json:"command"`
	ExitCode int     `json:"exitCode"`
	Status   string  `json:"status"`
	Duration float64 `json:"duration"`
	LogFile  string  `json:"logFile,omitempty"`
}

// Execute 依序執行 hooks，logDir 用來存 stdout/stderr output 檔。
// 遇到 on_fail=block 的 hook 失敗時立即停止並回傳 error。
// on_fail=warn 的 hook 失敗只記錄，不影響流程。
func Execute(hooks []HookEntry, logDir string) ([]Result, error) {
	os.MkdirAll(logDir, 0o755)
	var results []Result

	for i, h := range hooks {
		start := time.Now()
		cmd := exec.Command("sh", "-c", h.Run)

		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output

		err := cmd.Run()
		dur := time.Since(start).Seconds()

		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		status := "pass"
		if exitCode != 0 {
			status = "fail"
		}

		logFile := ""
		if logDir != "" {
			ts := time.Now().Format("20060102-150405")
			logFile = filepath.Join(logDir, fmt.Sprintf("%s-hook-%d.log", ts, i))
			os.WriteFile(logFile, output.Bytes(), 0o644)
		}

		r := Result{
			Command:  h.Run,
			ExitCode: exitCode,
			Status:   status,
			Duration: dur,
			LogFile:  logFile,
		}
		results = append(results, r)

		if exitCode != 0 && h.EffectiveOnFail() == "block" {
			return results, fmt.Errorf("hook %q failed (exit %d)", h.Run, exitCode)
		}
	}

	return results, nil
}
```

需要的 import：`bytes`、`errors`、`fmt`、`os`、`os/exec`、`path/filepath`、`time`

- [ ] **Step 7: 跑 test 確認通過**

Run: `go test ./internal/hook/ -v -run TestExecute`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat(F045): implement hook Execute with block/warn failure modes"
```

---

### Task 4: Hook output 寫入 log 檔

**Files:**
- Modify: `internal/hook/hook.go`
- Test: `internal/hook/hook_test.go`

- [ ] **Step 1: 寫 failing test — log 檔包含 stdout/stderr**

```go
func TestExecute_LogFile_ContainsOutput(t *testing.T) {
	hooks := []HookEntry{{Run: "echo hello && echo err >&2"}}
	logDir := t.TempDir()
	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].LogFile == "" {
		t.Fatal("expected LogFile to be set")
	}
	data, err := os.ReadFile(results[0].LogFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "hello") {
		t.Errorf("log should contain stdout, got: %q", content)
	}
	if !strings.Contains(content, "err") {
		t.Errorf("log should contain stderr, got: %q", content)
	}
}
```

- [ ] **Step 2: 跑 test 確認通過**

這個 test 應該已經被 Task 3 的實作覆蓋。如果通過就直接進下一步。

Run: `go test ./internal/hook/ -v -run TestExecute_LogFile`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/hook/hook_test.go
git commit -m "test(F045): add hook log file content test"
```

---

### Task 5: CLI transition.go 加 hook 呼叫

**Files:**
- Modify: `cmd/4x/transition.go`
- Test: `cmd/4x/transition_test.go`（若存在則修改，不存在則新增）

- [ ] **Step 1: 寫 failing test — transition 執行 pre/post hooks**

先檢查 transition_test.go 是否存在。若不存在就建立。這個 test 需要在 temp dir 建立完整 workspace。

```go
// cmd/4x/transition_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/hook"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestTransitionHooks_PreAndPost(t *testing.T) {
	ws := setupTestWorkspace(t)
	featureID := "test-feat"
	setupTestFeature(t, ws, featureID)

	marker := filepath.Join(t.TempDir(), "hook-marker")
	cfg := protocol.Config{
		Hooks: map[string][]hook.HookEntry{
			"pre_coding":  {{Run: "touch " + marker + "-pre"}},
			"post_coding": {{Run: "touch " + marker + "-post"}},
		},
	}

	hooks := resolveHooks(cfg, protocol.Feature{}, protocol.PhaseCoding)
	if pre := hooks["pre"]; len(pre) != 1 {
		t.Errorf("expected 1 pre hook, got %d", len(pre))
	}
	if post := hooks["post"]; len(post) != 1 {
		t.Errorf("expected 1 post hook, got %d", len(post))
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./cmd/4x/ -v -run TestTransitionHooks`
Expected: FAIL — `resolveHooks` 未定義

- [ ] **Step 3: 實作 resolveHooks 與 transition hook 呼叫邏輯**

在 `cmd/4x/transition.go` 加 helper：

```go
// resolveHooks 根據 config 和 feature 的 hooks 設定，回傳目標 phase 的 pre/post hooks。
// feature hooks override 全域同名 key。
func resolveHooks(cfg protocol.Config, feature protocol.Feature, targetPhase protocol.Phase) map[string][]hook.HookEntry {
	merged := protocol.MergeHooks(cfg.Hooks, feature.Hooks)
	if merged == nil {
		return nil
	}
	result := make(map[string][]hook.HookEntry)
	preKey := "pre_" + string(targetPhase)
	postKey := "post_" + string(targetPhase)
	if h, ok := merged[preKey]; ok {
		result["pre"] = h
	}
	if h, ok := merged[postKey]; ok {
		result["post"] = h
	}
	return result
}
```

在 `transition` command 的 `RunE` 裡，`state.Transition()` 前後加 hook 呼叫：

```go
// 在 state.Transition() 之前
hooks := resolveHooks(cfg, feature, toPhase)
if preHooks := hooks["pre"]; len(preHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, err := hook.Execute(preHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, toPhase, "pre_"+string(toPhase)))
    }
    if err != nil {
        return fmt.Errorf("pre_%s hook failed: %w", toPhase, err)
    }
}

// state.Transition() 呼叫...

// 在 state.Transition() 之後、寫 state 之後
if postHooks := hooks["post"]; len(postHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, err := hook.Execute(postHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, toPhase, "post_"+string(toPhase)))
    }
    if err != nil {
        // post hook 失敗：轉 needs-attention
        naState, naErr := state.Transition(newState, protocol.PhaseNeedsAttention, "")
        if naErr == nil {
            naState.Active = false
            ws.WriteState(featureID, naState)
            syncFeatureStatus(ws, featureID, protocol.PhaseNeedsAttention)
        }
        return fmt.Errorf("post_%s hook failed: %w", toPhase, err)
    }
}
```

注意：`transition.go` 目前沒有載 config 和 feature。需要在 pre-hook 前加：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    cfg = protocol.Config{}
}
feature, _ := ws.LoadFeature(featureID)
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./cmd/4x/ -v -run TestTransitionHooks`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/transition.go cmd/4x/transition_test.go
git commit -m "feat(F045): add pre/post hook execution to transition command"
```

---

### Task 6: Hook event 記錄到 events.jsonl

**Files:**
- Modify: `internal/hook/hook.go`
- Test: `internal/hook/hook_test.go`

- [ ] **Step 1: 寫 failing test — ToEvent 轉換**

```go
func TestToEvent(t *testing.T) {
	r := Result{
		Command:  "docker compose up",
		ExitCode: 0,
		Status:   "pass",
		Duration: 1.23,
	}
	evt := ToEvent(r, "coding", "pre_coding")
	if evt.Type != "hook" {
		t.Errorf("Type = %q, want hook", evt.Type)
	}
	if evt.Phase != "coding" {
		t.Errorf("Phase = %q, want coding", evt.Phase)
	}
	if evt.Action != "pre_coding" {
		t.Errorf("Action = %q, want pre_coding", evt.Action)
	}
	if evt.Command != "docker compose up" {
		t.Errorf("Command = %q, want docker compose up", evt.Command)
	}
	if evt.Status != "pass" {
		t.Errorf("Status = %q, want pass", evt.Status)
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/hook/ -v -run TestToEvent`
Expected: FAIL — `ToEvent` 未定義

- [ ] **Step 3: 實作 ToEvent**

```go
// ToEvent 將 hook Result 轉成 protocol.Event，用於寫入 events.jsonl
func ToEvent(r Result, phase protocol.Phase, action string) protocol.Event {
	detail := fmt.Sprintf("exit %d, %.1fs", r.ExitCode, r.Duration)
	return protocol.Event{
		Type:    "hook",
		Phase:   phase,
		Action:  action,
		Command: r.Command,
		Status:  r.Status,
		Detail:  detail,
	}
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/hook/ -v -run TestToEvent`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat(F045): add ToEvent for hook result to event log conversion"
```

---

### Task 7: run.go runLoop 加 hook 呼叫

**Files:**
- Modify: `cmd/4x/run.go`

- [ ] **Step 1: 在 runLoop 的 transition 前後加 hook**

在 `runLoop()` 函式裡，找到呼叫 `state.Transition()` 的兩個位置：

**位置 1：init → designing（第 322-333 行）**

在 `state.Transition(s, protocol.PhaseDesigning, ...)` 之前加 pre hook：

```go
hooks := resolveHooks(cfg, feature, protocol.PhaseDesigning)
if preHooks := hooks["pre"]; len(preHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, err := hook.Execute(preHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, protocol.PhaseDesigning, "pre_designing"))
    }
    if err != nil {
        return fmt.Errorf("pre_designing hook failed: %w", err)
    }
}
```

transition 之後加 post hook：

```go
if postHooks := hooks["post"]; len(postHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, hookErr := hook.Execute(postHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, protocol.PhaseDesigning, "post_designing"))
    }
    if hookErr != nil {
        s.Phase = protocol.PhaseNeedsAttention
        s.Active = false
        s.StopReason = "post-hook-fail"
        _ = ws.WriteState(featureID, s)
        _ = syncFeatureStatus(ws, featureID, s.Phase)
        return fmt.Errorf("post_designing hook failed: %w", hookErr)
    }
}
```

**位置 2：主迴圈裡的 `state.Transition(s, next, nextRole)`（第 507 行）**

同樣在 transition 前後加 pre/post hook，`next` 是目標 phase：

```go
nextHooks := resolveHooks(cfg, feature, next)
if preHooks := nextHooks["pre"]; len(preHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, err := hook.Execute(preHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, next, "pre_"+string(next)))
    }
    if err != nil {
        s.Active = false
        s.StopReason = "pre-hook-fail"
        _ = ws.WriteState(featureID, s)
        return fmt.Errorf("pre_%s hook failed: %w", next, err)
    }
}

// state.Transition() 呼叫...

if postHooks := nextHooks["post"]; len(postHooks) > 0 {
    hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
    results, hookErr := hook.Execute(postHooks, hookLogDir)
    for _, r := range results {
        ws.AppendEvent(featureID, hook.ToEvent(r, next, "post_"+string(next)))
    }
    if hookErr != nil {
        s.Phase = protocol.PhaseNeedsAttention
        s.Active = false
        s.StopReason = "post-hook-fail"
        _ = ws.WriteState(featureID, s)
        _ = syncFeatureStatus(ws, featureID, s.Phase)
        return fmt.Errorf("post_%s hook failed: %w", next, hookErr)
    }
}
```

- [ ] **Step 2: 建置與測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/run.go
git commit -m "feat(F045): add pre/post hook execution to runLoop"
```

---

### Task 8: 整合測試

**Files:**
- Create: `internal/hook/integration_test.go`

- [ ] **Step 1: 寫整合測試 — 完整 hook 流程**

```go
package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestIntegration_FullHookCycle(t *testing.T) {
	logDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "marker")

	hooks := []HookEntry{
		{Run: "touch " + marker, OnFail: "block"},
		{Run: "echo done", OnFail: "warn"},
	}

	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if _, err := os.Stat(marker); err != nil {
		t.Error("marker file should exist")
	}

	for _, r := range results {
		evt := ToEvent(r, protocol.PhaseCoding, "pre_coding")
		if evt.Type != "hook" {
			t.Errorf("event type = %q, want hook", evt.Type)
		}
		if !strings.Contains(evt.Detail, "exit 0") {
			t.Errorf("detail should contain exit code, got %q", evt.Detail)
		}
	}

	entries, _ := os.ReadDir(logDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 log files, got %d", len(entries))
	}
}

func TestIntegration_MixedBlockWarn(t *testing.T) {
	logDir := t.TempDir()

	hooks := []HookEntry{
		{Run: "echo ok", OnFail: "warn"},
		{Run: "exit 42", OnFail: "warn"},
		{Run: "echo after-warn"},
	}

	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatalf("warn hooks should not stop execution: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[1].ExitCode != 42 {
		t.Errorf("second hook exit = %d, want 42", results[1].ExitCode)
	}
}
```

- [ ] **Step 2: 跑整合測試**

Run: `go test ./internal/hook/ -v -run TestIntegration`
Expected: PASS

- [ ] **Step 3: 跑全部測試確認無 regression**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部通過

- [ ] **Step 4: Commit**

```bash
git add internal/hook/integration_test.go
git commit -m "test(F045): add hook integration tests"
```

---

### Task 9: 更新文件

**Files:**
- Modify: `docs/guide/cli.md`（如果有 transition command 文件）

- [ ] **Step 1: 跑 check-docs-sync 確認哪些文件需更新**

Run: `make check-docs-sync`

- [ ] **Step 2: 依腳本輸出更新對應文件**

若 `NEEDS_UPDATE` 指出需要更新特定文件，更新被點名的文件。若沒有需要更新的則跳過。

- [ ] **Step 3: 跑 check-i18n 確認語系同步**

Run: `make check-i18n`

- [ ] **Step 4: Commit**

```bash
git add docs/
git commit -m "docs(F045): update docs for phase hooks"
```
