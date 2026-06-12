# F023 — MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓外部 LLM agent 透過 MCP tool call 操作 4x CLI（status / new / run / stop / check / transition）。

**Architecture:** 兩階段——先幫 CLI 指令補 `--json` flag 輸出結構化 JSON，再用官方 Go MCP SDK 建 stdio server 包裝這些指令。MCP layer 是 thin wrapper，每個 tool exec `4x <cmd> --json` 取 stdout。

**Tech Stack:** Go 1.26+, `github.com/modelcontextprotocol/go-sdk`, Cobra CLI

---

## File Map

| Action | Path | 職責 |
|---|---|---|
| Modify | `cmd/4x/status.go` | 加 `--json` flag |
| Modify | `cmd/4x/new.go` | 加 `--json` flag |
| Modify | `cmd/4x/transition.go` | 加 `--json` flag |
| Modify | `cmd/4x/run.go` | 加 `--json` flag（啟動後立刻回傳） |
| Modify | `cmd/4x/main.go` | 註冊 `mcp` subcommand |
| Create | `cmd/4x/mcp.go` | `4x mcp` subcommand entry point |
| Create | `internal/mcp/server.go` | MCP server 建立與 tool 註冊 |
| Create | `internal/mcp/exec.go` | exec helper：呼叫 4x CLI + parse JSON |
| Create | `internal/mcp/tools.go` | 6 個 tool handler |
| Create | `internal/mcp/exec_test.go` | exec helper 測試 |
| Create | `internal/mcp/tools_test.go` | tool handler 測試（mock exec） |
| Modify | `cmd/4x/cli_test.go` | `--json` flag 整合測試 |
| Modify | `go.mod` | 加 MCP SDK dependency |

---

### Task 1: `4x status --json`

**Files:**
- Modify: `cmd/4x/status.go`
- Modify: `cmd/4x/cli_test.go`

- [ ] **Step 1: 在 `cli_test.go` 加測試**

在檔案尾部加：

```go
func TestStatus_JSON_ListAll(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "JSON test feature")

	out, err := run4x(dir, "status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v\n%s", err, out)
	}

	var result struct {
		Features []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(result.Features) != 1 {
		t.Fatalf("got %d features, want 1", len(result.Features))
	}
	if result.Features[0].Status != "not-started" {
		t.Errorf("status = %q, want not-started", result.Features[0].Status)
	}
}

func TestStatus_JSON_SingleFeature(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Detail test")

	out, err := run4x(dir, "status", "F001", "--json")
	if err != nil {
		t.Fatalf("status <id> --json failed: %v\n%s", err, out)
	}

	var result struct {
		Feature struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"feature"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(result.Feature.ID, "F001-") {
		t.Errorf("id = %q, want F001-* prefix", result.Feature.ID)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestStatus_JSON -v`
Expected: FAIL — `--json` flag 不存在

- [ ] **Step 3: 在 `status.go` 實作 `--json`**

在 `newStatusCmd()` 加 flag 和 JSON 分支。修改 `showAllFeatures` 和 `showFeatureDetail`：

```go
// newStatusCmd 內加 flag
var jsonOutput bool

// 在 RunE 內，JSON 路徑改為：
if len(args) == 1 {
    featureID, err := ws.ResolveFeatureID(args[0])
    if err != nil {
        return err
    }
    if jsonOutput {
        return showFeatureDetailJSON(ws, featureID)
    }
    return showFeatureDetail(ws, featureID)
}
if jsonOutput {
    return showAllFeaturesJSON(ws)
}
return showAllFeatures(ws, pending)

// flag 註冊
cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
```

加兩個 JSON 輸出函式：

```go
type statusListJSON struct {
	Features []statusItemJSON `json:"features"`
}

type statusItemJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Role      string `json:"role"`
	Round     int    `json:"round"`
	MaxRounds int    `json:"maxRounds"`
	Active    bool   `json:"active"`
	Runner    string `json:"runner,omitempty"`
}

func showAllFeaturesJSON(ws *protocol.Workspace) error {
	features, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	var items []statusItemJSON
	for _, f := range features {
		item := statusItemJSON{
			ID:     f.ID,
			Name:   f.Name,
			Status: f.Status,
		}
		if s, err := ws.ReadState(f.ID); err == nil {
			item.Phase = string(s.Phase)
			item.Role = string(s.Role)
			item.Round = s.Round
			item.MaxRounds = s.MaxRounds
			item.Active = s.Active
			item.Runner = s.Runner
		}
		items = append(items, item)
	}
	data, _ := json.MarshalIndent(statusListJSON{Features: items}, "", "  ")
	fmt.Println(string(data))
	return nil
}

type featureDetailJSON struct {
	Feature protocol.Feature `json:"feature"`
	State   *protocol.State  `json:"state"`
}

func showFeatureDetailJSON(ws *protocol.Workspace, id string) error {
	f, err := ws.LoadFeature(id)
	if err != nil {
		return fmt.Errorf("feature %q not found: %w", id, err)
	}
	result := featureDetailJSON{Feature: f}
	if s, err := ws.ReadState(id); err == nil {
		result.State = &s
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	return nil
}
```

import 需加 `"encoding/json"`。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestStatus_JSON -v`
Expected: PASS

- [ ] **Step 5: 跑完整測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./cmd/4x/ -v`
Expected: build + vet clean，新測試 PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/status.go cmd/4x/cli_test.go
git commit -m "feat(status): add --json flag for structured output"
```

---

### Task 2: `4x new --json`

**Files:**
- Modify: `cmd/4x/new.go`
- Modify: `cmd/4x/cli_test.go`

- [ ] **Step 1: 在 `cli_test.go` 加測試**

```go
func TestNew_JSON(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")

	out, err := run4x(dir, "new", "--json", "JSON new test")
	if err != nil {
		t.Fatalf("new --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		Name      string `json:"name"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(result.FeatureID, "F") {
		t.Errorf("featureId = %q, want F-prefix", result.FeatureID)
	}
	if result.Path == "" {
		t.Error("path is empty")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestNew_JSON -v`
Expected: FAIL

- [ ] **Step 3: 在 `new.go` 實作 `--json`**

加 `jsonOutput` flag，在 feature 建立成功後輸出 JSON：

```go
var jsonOutput bool

// 在 SaveFeature 成功後：
if jsonOutput {
    result := struct {
        FeatureID string `json:"featureId"`
        Name      string `json:"name"`
        Path      string `json:"path"`
    }{
        FeatureID: id,
        Name:      name,
        Path:      fmt.Sprintf(".4x/features/%s.yaml", id),
    }
    data, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(data))
    return nil
}
// ... 原有 fmt.Printf 輸出 ...

cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
```

import 加 `"encoding/json"`。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestNew_JSON -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/new.go cmd/4x/cli_test.go
git commit -m "feat(new): add --json flag for structured output"
```

---

### Task 3: `4x transition --json`

**Files:**
- Modify: `cmd/4x/transition.go`
- Modify: `cmd/4x/cli_test.go`

- [ ] **Step 1: 在 `cli_test.go` 加測試**

```go
func TestTransition_JSON(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Trans JSON")

	featureID := "F001-trans-json"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	os.MkdirAll(featureDir, 0o755)
	os.WriteFile(filepath.Join(featureDir, protocol.StateFile),
		[]byte(`{"featureId":"F001-trans-json","phase":"init","role":"","round":0,"maxRounds":5,"active":true,"runner":"mock","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`), 0o644)

	out, err := run4x(dir, "transition", featureID, "--to", "designing", "--json")
	if err != nil {
		t.Fatalf("transition --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		From      string `json:"from"`
		To        string `json:"to"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.From != "init" || result.To != "designing" {
		t.Errorf("got from=%q to=%q, want init→designing", result.From, result.To)
	}
}

func TestTransition_JSON_Error(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Trans err")

	featureID := "F001-trans-err"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	os.MkdirAll(featureDir, 0o755)
	os.WriteFile(filepath.Join(featureDir, protocol.StateFile),
		[]byte(`{"featureId":"F001-trans-err","phase":"init","role":"","round":0,"maxRounds":5,"active":true,"runner":"mock","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`), 0o644)

	out, err := run4x(dir, "transition", featureID, "--to", "done", "--json")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON on error: %v\n%s", err, out)
	}
	if result.Error == "" {
		t.Error("error field is empty")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestTransition_JSON -v`
Expected: FAIL

- [ ] **Step 3: 在 `transition.go` 實作 `--json`**

加 `jsonOutput` flag。成功時輸出 `{featureId, from, to}`，失敗時輸出 `{error}`：

```go
var jsonOutput bool

// RunE 內，成功路徑改為：
from := s.Phase
// ... 既有的 transition 邏輯 ...
if jsonOutput {
    result := struct {
        FeatureID string `json:"featureId"`
        From      string `json:"from"`
        To        string `json:"to"`
    }{featureID, string(from), string(toPhase)}
    data, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(data))
    return nil
}
fmt.Printf("%s → %s (role: %s, round: %d)\n", from, toPhase, toRole, newState.Round)

// 錯誤路徑（transition error、guard error）需要在 jsonOutput 時輸出 JSON 到 stdout 再回傳 error：
if jsonOutput {
    errResult, _ := json.MarshalIndent(struct {
        Error string `json:"error"`
    }{err.Error()}, "", "  ")
    fmt.Println(string(errResult))
}
return err

cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
```

import 加 `"encoding/json"`。

注意：jsonOutput 時 error 路徑要先印 JSON 再 return error（讓 exit code 非零但 stdout 有結構化資料）。具體做法是把 RunE 裡的 error return 點都包一層 helper：

```go
func jsonError(msg string) error {
	data, _ := json.MarshalIndent(struct {
		Error string `json:"error"`
	}{msg}, "", "  ")
	fmt.Println(string(data))
	return fmt.Errorf("%s", msg)
}
```

放在 `transition.go` 內即可（private helper）。在 `jsonOutput` 為 true 時，所有 error return 改用 `return jsonError(err.Error())`。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestTransition_JSON -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/transition.go cmd/4x/cli_test.go
git commit -m "feat(transition): add --json flag for structured output"
```

---

### Task 4: `4x run --json`

**Files:**
- Modify: `cmd/4x/run.go`

- [ ] **Step 1: 在 `run.go` 加 `--json` flag**

`run --json` 的語意是：啟動 run、印 JSON、立刻 return（不等 loop 結束）。用 `os/exec` 啟動自己的 non-json 版本為 background process：

```go
var jsonOutput bool

// RunE 內，在 dryRun 檢查之前加：
if jsonOutput {
    bgArgs := []string{"run", featureID, "--runner", runnerName,
        "--max-rounds", fmt.Sprintf("%d", maxRounds),
        "--timeout", fmt.Sprintf("%d", timeout)}
    bgCmd := exec.Command(os.Args[0], bgArgs...)
    bgCmd.Dir = cwd
    bgCmd.Stdout = os.NewFile(0, os.DevNull)
    bgCmd.Stderr = os.NewFile(0, os.DevNull)
    if err := bgCmd.Start(); err != nil {
        return jsonError(fmt.Sprintf("failed to start run: %v", err))
    }
    result := struct {
        FeatureID string `json:"featureId"`
        Runner    string `json:"runner"`
        MaxRounds int    `json:"maxRounds"`
        PID       int    `json:"pid"`
    }{featureID, runnerName, maxRounds, bgCmd.Process.Pid}
    data, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(data))
    return nil
}

cmd.Flags().BoolVar(&jsonOutput, "json", false, "start run and return JSON immediately")
```

同時把 Task 3 的 `jsonError` helper 搬到一個共用位置。在 `cmd/4x/` 下新增 `json_helpers.go`（不需 export）：

```go
package main

import (
	"encoding/json"
	"fmt"
)

func jsonError(msg string) error {
	data, _ := json.MarshalIndent(struct {
		Error string `json:"error"`
	}{msg}, "", "  ")
	fmt.Println(string(data))
	return fmt.Errorf("%s", msg)
}
```

然後從 `transition.go` 移除 local 版。

- [ ] **Step 2: build + vet 確認編譯通過**

Run: `go build ./cmd/4x && go vet ./cmd/4x/`
Expected: clean

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/run.go cmd/4x/json_helpers.go cmd/4x/transition.go
git commit -m "feat(run): add --json flag for async start with PID"
```

---

### Task 5: 加 MCP SDK dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: 加 dependency**

```bash
go get github.com/modelcontextprotocol/go-sdk@latest
```

- [ ] **Step 2: tidy**

```bash
go mod tidy
```

- [ ] **Step 3: 確認 build 正常**

Run: `go build ./cmd/4x`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add modelcontextprotocol/go-sdk"
```

---

### Task 6: exec helper — `internal/mcp/exec.go`

**Files:**
- Create: `internal/mcp/exec.go`
- Create: `internal/mcp/exec_test.go`

- [ ] **Step 1: 寫測試**

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRun_ParsesJSON(t *testing.T) {
	// mock 不了外部 binary，用 integration 風格：
	// 跳過測試 if 4x binary 不在 PATH
	if _, err := lookPath(); err != nil {
		t.Skip("4x binary not in PATH")
	}
	// 這個 test 在 Task 9 整合時用，先建骨架
}

func TestBuildArgs_AddsJSON(t *testing.T) {
	args := buildArgs("status", "--json")
	if len(args) < 2 || args[0] != "status" || args[1] != "--json" {
		t.Errorf("args = %v, want [status --json ...]", args)
	}
}

func TestParseOutput_ValidJSON(t *testing.T) {
	raw := `{"features": []}` + "\nsome warning on stderr\n"
	result, err := parseJSONOutput([]byte(raw))
	if err != nil {
		t.Fatalf("parseJSONOutput failed: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	raw := `not json at all`
	_, err := parseJSONOutput([]byte(raw))
	if err == nil {
		t.Fatal("expected error for non-JSON output")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/mcp/ -v`
Expected: FAIL — 函式不存在

- [ ] **Step 3: 實作 exec.go**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ExecFunc 是可被 mock 的 exec 函式簽名
type ExecFunc func(ctx context.Context, args ...string) (json.RawMessage, error)

// DefaultExec 透過呼叫 4x binary 取得 JSON output
func DefaultExec(ctx context.Context, args ...string) (json.RawMessage, error) {
	binPath, err := lookPath()
	if err != nil {
		return nil, fmt.Errorf("4x binary not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, binPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			parsed, parseErr := parseJSONOutput(out)
			if parseErr == nil {
				return parsed, fmt.Errorf("exit %d", exitErr.ExitCode())
			}
			return nil, fmt.Errorf("exit %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, err
	}
	return parseJSONOutput(out)
}

func lookPath() (string, error) {
	return exec.LookPath("4x")
}

func buildArgs(args ...string) []string {
	return args
}

func parseJSONOutput(out []byte) (json.RawMessage, error) {
	out = trimToJSON(out)
	if !json.Valid(out) {
		return nil, fmt.Errorf("invalid JSON output: %s", string(out))
	}
	return json.RawMessage(out), nil
}

// trimToJSON 找到第一個 { 或 [ 開頭，截到對應的結尾
func trimToJSON(data []byte) []byte {
	for i, b := range data {
		if b == '{' || b == '[' {
			return data[i:]
		}
	}
	return data
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/mcp/ -v`
Expected: PASS（除了跳過的 integration test）

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/exec.go internal/mcp/exec_test.go
git commit -m "feat(mcp): exec helper for calling 4x CLI with JSON output"
```

---

### Task 7: MCP tools — `internal/mcp/tools.go`

**Files:**
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/tools_test.go`

- [ ] **Step 1: 寫測試**

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func mockExec(responses map[string]string) ExecFunc {
	return func(ctx context.Context, args ...string) (json.RawMessage, error) {
		key := args[0]
		if resp, ok := responses[key]; ok {
			return json.RawMessage(resp), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
}

func TestStatusTool_NoArgs(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"features":[{"id":"F001","name":"test","status":"not-started"}]}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestStatusTool_WithFeatureID(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"feature":{"id":"F001","name":"test"},"state":null}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{FeatureID: "F001"}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestNewTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"new": `{"featureId":"F002-hello","name":"hello","path":".4x/features/F002-hello.yaml"}`,
	})
	h := &Handlers{Exec: exec}

	input := NewInput{Name: "hello"}
	result, err := h.New(context.Background(), input)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestCheckTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"check": `{"pass":true,"errors":[],"warnings":[]}`,
	})
	h := &Handlers{Exec: exec}

	input := CheckInput{FeatureID: "F001"}
	result, err := h.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/mcp/ -run TestStatusTool -v`
Expected: FAIL

- [ ] **Step 3: 實作 tools.go**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Handlers 持有 exec func，方便測試時 mock
type Handlers struct {
	Exec ExecFunc
}

// StatusInput 是 4x_status tool 的輸入參數
type StatusInput struct {
	FeatureID string `json:"featureId,omitempty" jsonschema:"description=Feature ID (optional — omit to list all)"`
}

// NewInput 是 4x_new tool 的輸入參數
type NewInput struct {
	Name        string `json:"name" jsonschema:"description=Feature name,required"`
	Description string `json:"description,omitempty" jsonschema:"description=Feature description"`
}

// RunInput 是 4x_run tool 的輸入參數
type RunInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	Runner    string `json:"runner,omitempty" jsonschema:"description=Runner plugin name"`
	MaxRounds int    `json:"maxRounds,omitempty" jsonschema:"description=Max iteration rounds"`
}

// StopInput 是 4x_stop tool 的輸入參數
type StopInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// CheckInput 是 4x_check tool 的輸入參數
type CheckInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// TransitionInput 是 4x_transition tool 的輸入參數
type TransitionInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	To        string `json:"to" jsonschema:"description=Target phase (designing/coding/reviewing/testing/accepting/done/blocked),required"`
}

// Status 列出 features 狀態
func (h *Handlers) Status(ctx context.Context, input StatusInput) (json.RawMessage, error) {
	args := []string{"status", "--json"}
	if input.FeatureID != "" {
		args = []string{"status", input.FeatureID, "--json"}
	}
	return h.Exec(ctx, args...)
}

// New 建立新 feature
func (h *Handlers) New(ctx context.Context, input NewInput) (json.RawMessage, error) {
	args := []string{"new", "--json", input.Name}
	return h.Exec(ctx, args...)
}

// Run 啟動 Design-Code-Review-Test loop
func (h *Handlers) Run(ctx context.Context, input RunInput) (json.RawMessage, error) {
	args := []string{"run", input.FeatureID, "--json"}
	if input.Runner != "" {
		args = append(args, "--runner", input.Runner)
	}
	if input.MaxRounds > 0 {
		args = append(args, "--max-rounds", fmt.Sprintf("%d", input.MaxRounds))
	}
	return h.Exec(ctx, args...)
}

// Stop 終止執行中的 run（寫 state.Active = false）
func (h *Handlers) Stop(ctx context.Context, input StopInput) (json.RawMessage, error) {
	// 不走 exec — 直接讀寫 state.json（CLI 沒有 stop 指令）
	// 此處回傳 placeholder，實際實作在 Task 8
	return nil, fmt.Errorf("not implemented")
}

// Check 執行 guardrail 檢查
func (h *Handlers) Check(ctx context.Context, input CheckInput) (json.RawMessage, error) {
	return h.Exec(ctx, "check", input.FeatureID, "--json")
}

// Transition 手動推進 phase
func (h *Handlers) Transition(ctx context.Context, input TransitionInput) (json.RawMessage, error) {
	return h.Exec(ctx, "transition", input.FeatureID, "--to", input.To, "--json")
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/mcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): tool handlers with mock-friendly exec interface"
```

---

### Task 8: Stop tool — 直接讀寫 state.json

**Files:**
- Modify: `internal/mcp/tools.go`
- Modify: `internal/mcp/tools_test.go`

- [ ] **Step 1: 加測試**

在 `tools_test.go` 加：

```go
func TestStopTool(t *testing.T) {
	dir := t.TempDir()
	dotDir := filepath.Join(dir, ".4x")
	featuresDir := filepath.Join(dotDir, "features")
	featureDir := filepath.Join(dotDir, "F001-test")
	os.MkdirAll(featuresDir, 0o755)
	os.MkdirAll(featureDir, 0o755)

	state := `{"featureId":"F001-test","phase":"coding","role":"coder","round":1,"maxRounds":5,"active":true,"runner":"claude","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`
	os.WriteFile(filepath.Join(featureDir, "state.json"), []byte(state), 0o644)
	os.WriteFile(filepath.Join(featuresDir, "F001-test.yaml"), []byte("id: F001-test\nname: test\nstatus: in-progress\n"), 0o644)

	h := &Handlers{WorkspaceRoot: dir}
	input := StopInput{FeatureID: "F001-test"}
	result, err := h.Stop(context.Background(), input)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	var parsed struct {
		FeatureID string `json:"featureId"`
		Stopped   bool   `json:"stopped"`
	}
	json.Unmarshal(result, &parsed)
	if !parsed.Stopped {
		t.Error("stopped should be true")
	}

	// 確認 state.json 被改了
	data, _ := os.ReadFile(filepath.Join(featureDir, "state.json"))
	var s protocol.State
	json.Unmarshal(data, &s)
	if s.Active {
		t.Error("state should be inactive")
	}
	if s.StopReason != "mcp-stop" {
		t.Errorf("stopReason = %q, want mcp-stop", s.StopReason)
	}
}
```

import 需加 `"os"`, `"path/filepath"`, `"github.com/ggwhite/4x/internal/protocol"`。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/mcp/ -run TestStopTool -v`
Expected: FAIL

- [ ] **Step 3: 修改 Handlers struct 和 Stop 實作**

在 `tools.go` 的 `Handlers` 加 `WorkspaceRoot` 欄位：

```go
type Handlers struct {
	Exec          ExecFunc
	WorkspaceRoot string
}
```

實作 Stop：

```go
func (h *Handlers) Stop(ctx context.Context, input StopInput) (json.RawMessage, error) {
	ws := &protocol.Workspace{Root: h.WorkspaceRoot}
	s, err := ws.ReadState(input.FeatureID)
	if err != nil {
		return nil, fmt.Errorf("read state for %s: %w", input.FeatureID, err)
	}
	s.Active = false
	s.StopReason = "mcp-stop"
	if err := ws.WriteState(input.FeatureID, s); err != nil {
		return nil, fmt.Errorf("write state for %s: %w", input.FeatureID, err)
	}
	result, _ := json.Marshal(struct {
		FeatureID string `json:"featureId"`
		Stopped   bool   `json:"stopped"`
	}{input.FeatureID, true})
	return result, nil
}
```

import 加 `"github.com/ggwhite/4x/internal/protocol"`。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/mcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): stop tool reads/writes state.json directly"
```

---

### Task 9: MCP server — `internal/mcp/server.go` + `cmd/4x/mcp.go`

**Files:**
- Create: `internal/mcp/server.go`
- Create: `cmd/4x/mcp.go`
- Modify: `cmd/4x/main.go`

- [ ] **Step 1: 實作 server.go**

```go
package mcp

import (
	"context"
	"encoding/json"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/server"
)

// NewServer 建立 MCP server 並註冊所有 tools
func NewServer(version, workspaceRoot string) *server.MCPServer {
	s := server.NewMCPServer(
		"4x",
		version,
	)

	h := &Handlers{
		Exec:          DefaultExec,
		WorkspaceRoot: workspaceRoot,
	}

	s.AddTool(gomcp.Tool{
		Name:        "4x_status",
		Description: "List all features and their status, or get detail for a single feature",
	}, statusHandler(h))

	s.AddTool(gomcp.Tool{
		Name:        "4x_new",
		Description: "Create a new feature",
	}, newHandler(h))

	s.AddTool(gomcp.Tool{
		Name:        "4x_run",
		Description: "Start the Design-Code-Review-Test loop for a feature",
	}, runHandler(h))

	s.AddTool(gomcp.Tool{
		Name:        "4x_stop",
		Description: "Stop a running feature loop",
	}, stopHandler(h))

	s.AddTool(gomcp.Tool{
		Name:        "4x_check",
		Description: "Run guardrail checks on a feature",
	}, checkHandler(h))

	s.AddTool(gomcp.Tool{
		Name:        "4x_transition",
		Description: "Manually transition a feature to a new phase",
	}, transitionHandler(h))

	return s
}

func toolResult(data json.RawMessage) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			gomcp.TextContent{Text: string(data)},
		},
	}
}

func toolError(err error) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		IsError: true,
		Content: []gomcp.Content{
			gomcp.TextContent{Text: err.Error()},
		},
	}
}

func statusHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input StatusInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.Status(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}

func newHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input NewInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.New(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}

func runHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input RunInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.Run(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}

func stopHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input StopInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.Stop(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}

func checkHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input CheckInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.Check(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}

func transitionHandler(h *Handlers) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var input TransitionInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(err), nil
		}
		result, err := h.Transition(ctx, input)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(result), nil
	}
}
```

注意：以上 server.go 的 import path 和 API 依據 research 結果，實作時需對照 SDK 的實際 API。如果 SDK API 不同（例如 `AddTool` 用泛型 `mcp.AddTool(s, tool, handler)`），需適配。

- [ ] **Step 2: 實作 `cmd/4x/mcp.go`**

```go
package main

import (
	"context"
	"fmt"
	"os"

	mcpPkg "github.com/ggwhite/4x/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/server"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (stdio mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			s := mcpPkg.NewServer(version, cwd)
			transport := server.NewStdioTransport()
			return s.Run(context.Background(), transport)
		},
	}
}
```

注意：transport API 需對照 SDK 實際 API 調整。

- [ ] **Step 3: 在 `main.go` 註冊**

```go
root.AddCommand(
    // ... 既有指令 ...
    newMCPCmd(),
)
```

- [ ] **Step 4: build + vet**

Run: `go build ./cmd/4x && go vet ./...`
Expected: clean

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go cmd/4x/mcp.go cmd/4x/main.go
git commit -m "feat(mcp): stdio MCP server with 6 tools"
```

---

### Task 10: 整合驗證 + 手動測試

**Files:**
- 無新檔案

- [ ] **Step 1: 全量 build + test**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: build clean，新測試全部 PASS

- [ ] **Step 2: 手動測試 `4x mcp`**

在本 repo 目錄下執行：

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' | ./bin/4x mcp
```

Expected: 收到 JSON-RPC response 包含 server info 和 tool list。

- [ ] **Step 3: 測試 tools/list**

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n' | ./bin/4x mcp
```

Expected: 回傳 6 個 tool 定義（4x_status, 4x_new, 4x_run, 4x_stop, 4x_check, 4x_transition）。

- [ ] **Step 4: 測試 tool call**

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"4x_status","arguments":{}}}\n' | ./bin/4x mcp
```

Expected: 回傳 features 列表 JSON。

- [ ] **Step 5: Commit 最終狀態**

```bash
git add -A
git commit -m "feat(F023): MCP server — expose 4x commands as MCP tools"
```
