# F114 — Coder Build Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `4x check` 在 coding/amending phase 自動跑 build + lint，失敗擋住 Coder；Orchestrator 加最後防線確保壞程式碼不進 Reviewer。

**Architecture:** 在 `guard.Check()` 新增 `checkBuildGate()` 呼叫，復用 `verify.RunGroups()` 跑指令，結果寫 `build-gate.json`。Orchestrator 的 `NextPhaseAfter(PhaseCoding)` 讀該檔做最後防線。

**Tech Stack:** Go 1.26+, 標準 testing package

## Global Constraints

- CLI 層嚴禁呼叫 LLM
- 復用 `protocol.VerifyEvidence` 結構，不發明新 struct
- 不跑 `project.test`（只跑 `project.build` + `project.lint`）
- 不改 `4x verify` 或 Tester 流程
- 不引入 Coder phase retry 機制

---

### Task 1: 新增常量 + `BuildGateGroups` 函式

**Files:**
- Modify: `internal/protocol/workspace.go:40` — 新增 `BuildGateFile` 常量
- Modify: `internal/verify/verify.go:25-36` — 新增 `BuildGateGroups()` 函式
- Test: `internal/verify/verify_test.go`

**Interfaces:**
- Consumes: `protocol.ProjectConfig` (已存在: `.Build`, `.Lint` 欄位)
- Produces: `verify.BuildGateGroups(cfg) ([]Group, error)` — Task 2 用這個產出 groups

- [ ] **Step 1: 寫 `BuildGateGroups` 的失敗測試**

```go
func TestBuildGateGroups_Basic(t *testing.T) {
	cfg := protocol.ProjectConfig{
		Build: []string{"make build"},
		Lint:  []string{"go vet ./...", "gofmt -l ."},
	}
	groups, err := BuildGateGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "build-gate" {
		t.Fatalf("expected group name 'build-gate', got %q", groups[0].Name)
	}
	if len(groups[0].Commands) != 3 {
		t.Fatalf("expected 3 commands (build first, then lint), got %d", len(groups[0].Commands))
	}
	if groups[0].Commands[0] != "make build" {
		t.Fatalf("expected first command 'make build', got %q", groups[0].Commands[0])
	}
}

func TestBuildGateGroups_Empty(t *testing.T) {
	cfg := protocol.ProjectConfig{}
	_, err := BuildGateGroups(cfg)
	if err == nil {
		t.Fatal("expected error for empty build+lint")
	}
}

func TestBuildGateGroups_BuildOnly(t *testing.T) {
	cfg := protocol.ProjectConfig{Build: []string{"go build ./..."}}
	groups, err := BuildGateGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups[0].Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(groups[0].Commands))
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./internal/verify/ -run TestBuildGateGroups -v`
Expected: FAIL — `BuildGateGroups` undefined

- [ ] **Step 3: 新增 `BuildGateFile` 常量**

在 `internal/protocol/workspace.go` 的常量區塊，`VerifyFile` 之後加：

```go
BuildGateFile  = "build-gate.json"
```

- [ ] **Step 4: 實作 `BuildGateGroups`**

在 `internal/verify/verify.go`，`FallbackGroups` 之後加：

```go
// BuildGateGroups 從 settings.json 的 Build + Lint 指令組合出單一 build-gate group。
// Build 指令在前、Lint 在後，復用 runGroup 的「前一個失敗就 skip 後續」語意。
// 不含 Test 指令——test 是 Tester 的職責。
func BuildGateGroups(cfg protocol.ProjectConfig) ([]Group, error) {
	var commands []string
	commands = append(commands, cfg.Build...)
	commands = append(commands, cfg.Lint...)
	if len(commands) == 0 {
		return nil, fmt.Errorf("settings.json has no build/lint commands for build-gate")
	}
	return []Group{{Name: "build-gate", Commands: commands}}, nil
}
```

- [ ] **Step 5: 跑測試確認 pass**

Run: `go test ./internal/verify/ -run TestBuildGateGroups -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/workspace.go internal/verify/verify.go internal/verify/verify_test.go
git commit -m "feat(F114): add BuildGateFile constant and BuildGateGroups function"
```

---

### Task 2: 實作 `checkBuildGate()` guard 函式

**Files:**
- Modify: `internal/guard/check.go:44-56` — `Check()` 內新增呼叫；新增 `checkBuildGate()` 函式
- Test: `internal/guard/check_test.go`

**Interfaces:**
- Consumes: `verify.BuildGateGroups(cfg)` (Task 1), `verify.RunGroups()` (已存在), `ws.ReadState()`, `ws.ReadConfig()`
- Produces: `checkBuildGate(ws, featureID, r)` — 寫 `build-gate.json` 到 round dir，失敗時設 `r.Pass = false`

- [ ] **Step 1: 寫 `checkBuildGate` 跳過非 coding phase 的測試**

```go
func TestCheckBuildGate_SkipsNonCodingPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-test")
	writeState(t, ws, "F999-test", protocol.State{
		Phase: protocol.PhaseReviewing,
		Round: 1,
	})
	r := guard.Check(ws, "F999-test", nil)
	// build-gate.json should NOT be created for non-coding phases
	bgPath := filepath.Join(ws.RoundDir("F999-test", 1), protocol.BuildGateFile)
	if _, err := os.Stat(bgPath); err == nil {
		t.Fatal("build-gate.json should not exist for reviewing phase")
	}
}
```

- [ ] **Step 2: 寫 coding phase 無 build/lint 指令時 warn 不 fail 的測試**

```go
func TestCheckBuildGate_NoBuildLintCommands(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-nobuild")
	writeState(t, ws, "F999-nobuild", protocol.State{
		Phase: protocol.PhaseCoding,
		Round: 1,
	})
	// settings.json has no build/lint — should warn, not fail
	r := guard.Check(ws, "F999-nobuild", nil)
	if !r.Pass {
		t.Fatalf("expected pass (no build/lint is a warn, not error), got errors: %v", r.Errors)
	}
}
```

- [ ] **Step 3: 寫 coding phase build 成功的測試**

這個測試需要 build/lint 指令能在 CI 跑成功。用 `echo ok` 作為 test command：

```go
func TestCheckBuildGate_CodingPhaseSuccess(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-bgpass")
	// Write settings with simple build+lint commands
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Lint:  []string{"echo lint-ok"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F999-bgpass", protocol.State{
		Phase: protocol.PhaseCoding,
		Round: 1,
	})
	os.MkdirAll(ws.RoundDir("F999-bgpass", 1), 0o755)

	r := guard.Check(ws, "F999-bgpass", nil)
	if !r.Pass {
		t.Fatalf("expected pass, got errors: %v", r.Errors)
	}
	// build-gate.json should exist
	bgPath := filepath.Join(ws.RoundDir("F999-bgpass", 1), protocol.BuildGateFile)
	data, err := os.ReadFile(bgPath)
	if err != nil {
		t.Fatalf("build-gate.json not written: %v", err)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("invalid build-gate.json: %v", err)
	}
	if !ev.Passed {
		t.Fatal("expected build-gate passed=true")
	}
}
```

- [ ] **Step 4: 寫 coding phase build 失敗的測試**

```go
func TestCheckBuildGate_CodingPhaseFail(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-bgfail")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"false"},
			Lint:  []string{"echo lint-ok"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F999-bgfail", protocol.State{
		Phase: protocol.PhaseCoding,
		Round: 1,
	})
	os.MkdirAll(ws.RoundDir("F999-bgfail", 1), 0o755)

	r := guard.Check(ws, "F999-bgfail", nil)
	if r.Pass {
		t.Fatal("expected fail when build command fails")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "build-gate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error mentioning build-gate, got: %v", r.Errors)
	}
	// lint should be skipped (same group, build failed first)
	bgPath := filepath.Join(ws.RoundDir("F999-bgfail", 1), protocol.BuildGateFile)
	data, _ := os.ReadFile(bgPath)
	var ev protocol.VerifyEvidence
	json.Unmarshal(data, &ev)
	if ev.Passed {
		t.Fatal("expected build-gate passed=false")
	}
	for _, cmd := range ev.Commands {
		if cmd.Command == "echo lint-ok" && !cmd.Skipped {
			t.Fatal("lint should be skipped when build fails")
		}
	}
}
```

- [ ] **Step 5: 跑測試確認 fail**

Run: `go test ./internal/guard/ -run TestCheckBuildGate -v`
Expected: FAIL — `checkBuildGate` not called yet

- [ ] **Step 6: 實作 `checkBuildGate`**

在 `internal/guard/check.go` 新增：

```go
// checkBuildGate 在 coding/amending phase 時執行 settings.json 的 build + lint 指令，
// 結果寫入 build-gate.json。非 coding/amending phase 時不執行。
func checkBuildGate(ws *protocol.Workspace, featureID string, r *CheckResult) {
	state, err := ws.ReadState(featureID)
	if err != nil {
		return
	}
	if state.Phase != protocol.PhaseCoding && state.Phase != protocol.PhaseAmending {
		return
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: cannot read settings.json: %v", err))
		return
	}
	groups, err := verify.BuildGateGroups(cfg.Project)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: %v", err))
		return
	}

	roundDir := ws.RoundDir(featureID, state.Round)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: cannot create round dir: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	evidence := verify.RunGroups(ctx, groups, ws.Root)
	evidence.Round = state.Round
	evidence.Role = protocol.RoleCoder

	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: marshal error: %v", err))
		return
	}
	outPath := filepath.Join(roundDir, protocol.BuildGateFile)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("build-gate: write error: %v", err))
		return
	}

	if !evidence.Passed {
		r.Pass = false
		var failedCmds []string
		for _, cmd := range evidence.Commands {
			if cmd.ExitCode != 0 && !cmd.Skipped {
				failedCmds = append(failedCmds, fmt.Sprintf("%s (exit %d): %s", cmd.Command, cmd.ExitCode, cmd.Summary))
			}
		}
		r.Errors = append(r.Errors, fmt.Sprintf("build-gate failed: %s", strings.Join(failedCmds, "; ")))
	}
}
```

在 `Check()` 函式的 `checkSymlinks` 之後加呼叫：

```go
checkBuildGate(ws, featureID, &r)
```

新增 import：`"context"`, `"time"`, `"github.com/ggwhite/4x/internal/verify"`

- [ ] **Step 7: 跑測試確認 pass**

Run: `go test ./internal/guard/ -run TestCheckBuildGate -v`
Expected: PASS

- [ ] **Step 8: 跑全部 guard 測試確認無 regression**

Run: `go test ./internal/guard/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/guard/check.go internal/guard/check_test.go
git commit -m "feat(F114): add checkBuildGate guard for coding/amending phase"
```

---

### Task 3: Orchestrator 防線 + Coder prompt 更新

**Files:**
- Modify: `internal/orchestrator/phase.go:38-49` — `PhaseCoding/PhaseAmending` case 加 build-gate 檢查
- Modify: `templates/coder.md.tmpl:143-145` — 加 `4x check` 自修迴圈指示
- Test: `internal/orchestrator/phase_test.go`

**Interfaces:**
- Consumes: `protocol.BuildGateFile` (Task 1), `protocol.VerifyEvidence` (已存在)
- Produces: `NextPhaseAfter` 在 build-gate 失敗時回傳 `PhaseNeedsAttention`

- [ ] **Step 1: 寫 orchestrator 防線測試 — build-gate.json 不存在**

```go
func TestNextPhaseAfter_CodingNoBuildGate(t *testing.T) {
	ws := setupPhaseWorkspace(t, "F999-nobg")
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	roundDir := ws.RoundDir("F999-nobg", 1)
	os.MkdirAll(roundDir, 0o755)
	// coder-report exists but no build-gate.json
	writePhaseFile(t, filepath.Join(roundDir, protocol.CoderReport), "# Report")

	next, _, reason := NextPhaseAfter(ws, "F999-nobg", s)
	if next != protocol.PhaseNeedsAttention {
		t.Fatalf("expected NeedsAttention, got %s", next)
	}
	if !strings.Contains(reason, "build-gate") {
		t.Fatalf("expected reason about build-gate, got: %s", reason)
	}
}
```

- [ ] **Step 2: 寫 orchestrator 防線測試 — build-gate failed**

```go
func TestNextPhaseAfter_CodingBuildGateFailed(t *testing.T) {
	ws := setupPhaseWorkspace(t, "F999-bgfail")
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	roundDir := ws.RoundDir("F999-bgfail", 1)
	os.MkdirAll(roundDir, 0o755)
	writePhaseFile(t, filepath.Join(roundDir, protocol.CoderReport), "# Report")
	ev := protocol.VerifyEvidence{Passed: false, Round: 1, Role: protocol.RoleCoder}
	data, _ := json.MarshalIndent(ev, "", "  ")
	writePhaseFile(t, filepath.Join(roundDir, protocol.BuildGateFile), string(data))

	next, _, reason := NextPhaseAfter(ws, "F999-bgfail", s)
	if next != protocol.PhaseNeedsAttention {
		t.Fatalf("expected NeedsAttention, got %s", next)
	}
	if !strings.Contains(reason, "build-gate") {
		t.Fatalf("expected reason about build-gate, got: %s", reason)
	}
}
```

- [ ] **Step 3: 寫 orchestrator 防線測試 — build-gate passed**

```go
func TestNextPhaseAfter_CodingBuildGatePassed(t *testing.T) {
	ws := setupPhaseWorkspace(t, "F999-bgok")
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	roundDir := ws.RoundDir("F999-bgok", 1)
	os.MkdirAll(roundDir, 0o755)
	writePhaseFile(t, filepath.Join(roundDir, protocol.CoderReport), "# Report")
	ev := protocol.VerifyEvidence{Passed: true, Round: 1, Role: protocol.RoleCoder}
	data, _ := json.MarshalIndent(ev, "", "  ")
	writePhaseFile(t, filepath.Join(roundDir, protocol.BuildGateFile), string(data))

	next, _, _ := NextPhaseAfter(ws, "F999-bgok", s)
	if next != protocol.PhaseReviewing {
		t.Fatalf("expected PhaseReviewing, got %s", next)
	}
}
```

- [ ] **Step 4: 跑測試確認 fail**

Run: `go test ./internal/orchestrator/ -run TestNextPhaseAfter_CodingBuildGate -v`
Expected: FAIL — 目前 `NextPhaseAfter` 不檢查 build-gate

- [ ] **Step 5: 修改 `NextPhaseAfter` 加 build-gate 防線**

在 `internal/orchestrator/phase.go` 的 `case protocol.PhaseCoding, protocol.PhaseAmending:` 區段，`coder-report.md` 檢查通過之後、`return protocol.PhaseReviewing` 之前，加：

```go
		// build-gate 防線：build/lint 未通過不放行
		bgPath := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.BuildGateFile)
		bgData, err := os.ReadFile(bgPath)
		if err != nil {
			return protocol.PhaseNeedsAttention, "", "build-gate.json missing: coder did not run 4x check with build/lint"
		}
		var bgEvidence protocol.VerifyEvidence
		if err := json.Unmarshal(bgData, &bgEvidence); err != nil {
			return protocol.PhaseNeedsAttention, "", "build-gate.json invalid: " + err.Error()
		}
		if !bgEvidence.Passed {
			return protocol.PhaseNeedsAttention, "", "build-gate failed: build/lint did not pass"
		}
```

注意：`phase.go` 已有 `"encoding/json"`, `"os"`, `"path/filepath"` import，不需額外加。用 string concatenation 取代 `fmt.Sprintf` 避免新增 `"fmt"` import。

- [ ] **Step 6: 跑測試確認 pass**

Run: `go test ./internal/orchestrator/ -run TestNextPhaseAfter_CodingBuildGate -v`
Expected: PASS

- [ ] **Step 7: 跑全部 orchestrator 測試確認無 regression**

Run: `go test ./internal/orchestrator/ -v`
Expected: PASS（既有的 PhaseCoding 測試可能需要補寫 build-gate.json，如果有的話修復）

- [ ] **Step 8: 更新 Coder prompt template**

在 `templates/coder.md.tmpl` 的 workflow section（`== Workflow ==` 區段），step 4 `Run verify commands` 前面加：

```
4. Run guardrail check: ${FOURX_BIN:-4x} check {{.Feature.ID}}
   - `4x check` now runs build + lint automatically in coding phase
   - If it fails, read the error output, fix the issues, and re-run until it passes
   - Do NOT write coder-report.md until `4x check` passes
```

原本的 step 4、5 改為 5、6。

- [ ] **Step 9: Commit**

```bash
git add internal/orchestrator/phase.go internal/orchestrator/phase_test.go templates/coder.md.tmpl
git commit -m "feat(F114): add build-gate orchestrator guard and update coder prompt"
```

---

### Task 4: 修復 regression + 全套驗證

**Files:**
- Possibly modify: `internal/orchestrator/phase_test.go` — 既有測試可能因為新增 build-gate 檢查而 fail
- Possibly modify: `internal/guard/check_test.go` — 同理

**Interfaces:**
- Consumes: Task 1-3 的全部產出
- Produces: 全綠的測試套件

- [ ] **Step 1: 跑全部測試**

Run: `make test`
Expected: PASS。若有 regression，是因為既有的 `PhaseCoding` 測試沒有寫 `build-gate.json`。

- [ ] **Step 2: 修復 regression（如果有）**

對每個 fail 的既有測試，在 setup 裡補寫 `build-gate.json`（`passed: true`）。例如：

```go
// 在需要通過 coding → reviewing 的既有測試中補上
ev := protocol.VerifyEvidence{Passed: true, Round: 1, Role: protocol.RoleCoder}
data, _ := json.MarshalIndent(ev, "", "  ")
writePhaseFile(t, filepath.Join(roundDir, protocol.BuildGateFile), string(data))
```

- [ ] **Step 3: 跑 build + lint**

Run: `make build && make lint`
Expected: PASS

- [ ] **Step 4: 跑 check-docs-sync**

Run: `make check-docs-sync`
Expected: 若 `docs/guide/cli.md` 需更新則更新

- [ ] **Step 5: Commit（如果有修復）**

```bash
git add -p  # 只 add 修復的檔案
git commit -m "fix(F114): fix regressions from build-gate guard"
```
