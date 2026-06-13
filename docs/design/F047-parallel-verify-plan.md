# F047: Parallel Verify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `4x verify` CLI subcommand，平行執行 verify commands 分組，產生 verify.json。

**Architecture:** 新增 `internal/verify/` package 封裝分組解析與平行執行邏輯，`cmd/4x/verify.go` 只做 Cobra 參數解析。擴展 `TestStrategy` 和 `VerifyCommand` 型別支援 group。修改 Tester/Designer 模板適配新流程。

**Tech Stack:** Go 1.26+, Cobra CLI, sync.WaitGroup, context.WithTimeout, gopkg.in/yaml.v3

---

## File Structure

| 檔案 | 職責 |
|---|---|
| `internal/protocol/types.go` | 擴展 `TestStrategy`（加 `VerifyGroups`）和 `VerifyCommand`（加 `Group`、`Skipped`） |
| `internal/verify/verify.go` | 核心邏輯：解析 strategy → 分組 → 平行執行 → 產 VerifyEvidence |
| `internal/verify/verify_test.go` | verify package 單元測試 |
| `cmd/4x/verify.go` | Cobra subcommand 定義 |
| `cmd/4x/main.go` | 註冊 newVerifyCmd |
| `cmd/4x/cli_test.go` | CLI 整合測試 |
| `templates/tester.md.tmpl` | 改用 `4x verify`，移除自寫 verify.json |
| `templates/designer.md.tmpl` | 加 `verify_groups` 範例格式 |

---

### Task 1: 擴展 protocol 型別

**Files:**
- Modify: `internal/protocol/types.go:170-186`

- [ ] **Step 1: 在 `VerifyCommand` 加 `Group` 和 `Skipped` 欄位**

在 `internal/protocol/types.go` 的 `VerifyCommand` struct 末尾加兩個欄位：

```go
// VerifyCommand 是單一 verify command 的結果
type VerifyCommand struct {
	Command    string    `json:"command"`
	ExitCode   int       `json:"exitCode"`
	DurationMs int64     `json:"durationMs"`
	Summary    string    `json:"summary"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Group      string    `json:"group,omitempty"`
	Skipped    bool      `json:"skipped,omitempty"`
}
```

- [ ] **Step 2: 在 `TestStrategy` 加 `VerifyGroups` 欄位**

在 `internal/protocol/types.go` 的 `TestStrategy` struct 末尾加一個欄位：

```go
// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web          bool                `yaml:"web" json:"web"`
	API          bool                `yaml:"api" json:"api"`
	Gate         bool                `yaml:"gate" json:"gate"`
	CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
	Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
	VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
}
```

- [ ] **Step 3: 驗證編譯**

Run: `go build ./... && go vet ./...`
Expected: 成功，無 error

- [ ] **Step 4: 跑既有測試確認無破壞**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F047): extend TestStrategy and VerifyCommand types for group support"
```

---

### Task 2: 新增 `internal/verify/` package — 解析邏輯

**Files:**
- Create: `internal/verify/verify.go`
- Create: `internal/verify/verify_test.go`

- [ ] **Step 1: 寫 `ResolveGroups` 的失敗測試**

建立 `internal/verify/verify_test.go`：

```go
package verify

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveGroups_VerifyGroupsOnly(t *testing.T) {
	ts := protocol.TestStrategy{
		VerifyGroups: map[string][]string{
			"core":     {"make build", "make test"},
			"sub-repo": {"cd ../sub && make test"},
		},
	}
	groups, err := ResolveGroups(ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	found := false
	for _, g := range groups {
		if g.Name == "core" {
			found = true
			if len(g.Commands) != 2 {
				t.Errorf("core group: expected 2 commands, got %d", len(g.Commands))
			}
		}
	}
	if !found {
		t.Error("core group not found")
	}
}

func TestResolveGroups_FallbackVerifyCommands(t *testing.T) {
	ts := protocol.TestStrategy{
		Verify: []string{"make build", "make test"},
	}
	groups, err := ResolveGroups(ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "default" {
		t.Errorf("expected group name 'default', got %q", groups[0].Name)
	}
	if len(groups[0].Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(groups[0].Commands))
	}
}

func TestResolveGroups_BothPresent_Error(t *testing.T) {
	ts := protocol.TestStrategy{
		Verify:       []string{"make test"},
		VerifyGroups: map[string][]string{"core": {"make build"}},
	}
	_, err := ResolveGroups(ts)
	if err == nil {
		t.Error("expected error when both verify_commands and verify_groups present")
	}
}

func TestResolveGroups_NeitherPresent_Error(t *testing.T) {
	ts := protocol.TestStrategy{}
	_, err := ResolveGroups(ts)
	if err == nil {
		t.Error("expected error when no verify commands defined")
	}
}
```

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./internal/verify/ -v`
Expected: 編譯失敗（package 不存在）

- [ ] **Step 3: 實作 `ResolveGroups`**

建立 `internal/verify/verify.go`：

```go
package verify

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// Group 表示一組依序執行的 verify commands
type Group struct {
	Name     string
	Commands []string
}

// ResolveGroups 從 TestStrategy 解析出 verify groups。
// verify_groups 存在時用它；否則 fallback 到 verify_commands 作為單一 default group。
// 兩者同時存在或都不存在時回傳 error。
func ResolveGroups(ts protocol.TestStrategy) ([]Group, error) {
	hasGroups := len(ts.VerifyGroups) > 0
	hasCommands := len(ts.Verify) > 0

	if hasGroups && hasCommands {
		return nil, fmt.Errorf("test-strategy.yaml has both verify_groups and verify_commands; use only one")
	}
	if !hasGroups && !hasCommands {
		return nil, fmt.Errorf("test-strategy.yaml has no verify_groups or verify_commands")
	}

	if hasGroups {
		names := make([]string, 0, len(ts.VerifyGroups))
		for name := range ts.VerifyGroups {
			names = append(names, name)
		}
		sort.Strings(names)

		groups := make([]Group, 0, len(names))
		for _, name := range names {
			groups = append(groups, Group{Name: name, Commands: ts.VerifyGroups[name]})
		}
		return groups, nil
	}

	return []Group{{Name: "default", Commands: ts.Verify}}, nil
}
```

- [ ] **Step 4: 確認解析測試通過**

Run: `go test ./internal/verify/ -v -run TestResolveGroups`
Expected: 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/verify/verify.go internal/verify/verify_test.go
git commit -m "feat(F047): add verify package with ResolveGroups"
```

---

### Task 3: 新增 `internal/verify/` — 平行執行邏輯

**Files:**
- Modify: `internal/verify/verify.go`
- Modify: `internal/verify/verify_test.go`

- [ ] **Step 1: 寫 `RunGroups` 的測試**

在 `internal/verify/verify_test.go` 尾部加入：

```go
func TestRunGroups_AllPass(t *testing.T) {
	groups := []Group{
		{Name: "a", Commands: []string{"echo hello"}},
		{Name: "b", Commands: []string{"echo world"}},
	}
	ctx := context.Background()
	result := RunGroups(ctx, groups, ".")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %+v", result.Commands)
	}
	if len(result.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(result.Commands))
	}
	for _, cmd := range result.Commands {
		if cmd.ExitCode != 0 {
			t.Errorf("command %q: expected exit 0, got %d", cmd.Command, cmd.ExitCode)
		}
		if cmd.Group == "" {
			t.Errorf("command %q: group should not be empty", cmd.Command)
		}
	}
}

func TestRunGroups_GroupFail_SkipsRemainingInGroup(t *testing.T) {
	groups := []Group{
		{Name: "fail-group", Commands: []string{"false", "echo should-skip"}},
		{Name: "ok-group", Commands: []string{"echo still-runs"}},
	}
	ctx := context.Background()
	result := RunGroups(ctx, groups, ".")
	if result.Passed {
		t.Error("expected overall fail")
	}

	var skippedFound bool
	var okFound bool
	for _, cmd := range result.Commands {
		if cmd.Command == "echo should-skip" && cmd.Skipped {
			skippedFound = true
		}
		if cmd.Command == "echo still-runs" && cmd.ExitCode == 0 {
			okFound = true
		}
	}
	if !skippedFound {
		t.Error("expected 'echo should-skip' to be marked skipped")
	}
	if !okFound {
		t.Error("expected 'echo still-runs' in ok-group to still execute")
	}
}

func TestRunGroups_Parallel(t *testing.T) {
	groups := []Group{
		{Name: "slow", Commands: []string{"sleep 0.3"}},
		{Name: "fast", Commands: []string{"sleep 0.3"}},
	}
	ctx := context.Background()
	start := time.Now()
	result := RunGroups(ctx, groups, ".")
	elapsed := time.Since(start)
	if !result.Passed {
		t.Error("expected pass")
	}
	if elapsed > 1*time.Second {
		t.Errorf("groups should run in parallel, took %v", elapsed)
	}
}

func TestRunGroups_ContextTimeout(t *testing.T) {
	groups := []Group{
		{Name: "slow", Commands: []string{"sleep 10"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result := RunGroups(ctx, groups, ".")
	if result.Passed {
		t.Error("expected fail due to timeout")
	}
}
```

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./internal/verify/ -v -run TestRunGroups`
Expected: 編譯失敗（`RunGroups` 未定義）

- [ ] **Step 3: 實作 `RunGroups`**

在 `internal/verify/verify.go` 尾部加入：

```go
// RunGroups 平行執行所有 group，組內依序。回傳組裝好的 VerifyEvidence（不含 Round 和 Role）。
func RunGroups(ctx context.Context, groups []Group, workDir string) protocol.VerifyEvidence {
	type groupResult struct {
		commands []protocol.VerifyCommand
	}

	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	for i, g := range groups {
		wg.Add(1)
		go func(idx int, grp Group) {
			defer wg.Done()
			results[idx] = runGroup(ctx, grp, workDir)
		}(i, g)
	}
	wg.Wait()

	var allCommands []protocol.VerifyCommand
	allPassed := true
	for _, r := range results {
		for _, cmd := range r.commands {
			allCommands = append(allCommands, cmd)
			if cmd.ExitCode != 0 && !cmd.Skipped {
				allPassed = false
			}
		}
	}

	return protocol.VerifyEvidence{
		Passed:   allPassed,
		Commands: allCommands,
	}
}

func runGroup(ctx context.Context, g Group, workDir string) groupResult {
	var commands []protocol.VerifyCommand
	failed := false

	for _, cmdStr := range g.Commands {
		if failed {
			commands = append(commands, protocol.VerifyCommand{
				Command: cmdStr,
				Group:   g.Name,
				Skipped: true,
			})
			continue
		}

		vc := executeCommand(ctx, cmdStr, g.Name, workDir)
		commands = append(commands, vc)
		if vc.ExitCode != 0 {
			failed = true
		}
	}

	return groupResult{commands: commands}
}

func executeCommand(ctx context.Context, cmdStr, group, workDir string) protocol.VerifyCommand {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir

	out, err := cmd.CombinedOutput()
	finished := time.Now()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	summary := strings.TrimSpace(string(out))
	if len(summary) > 500 {
		summary = summary[:250] + "\n...\n" + summary[len(summary)-250:]
	}

	return protocol.VerifyCommand{
		Command:    cmdStr,
		ExitCode:   exitCode,
		DurationMs: finished.Sub(start).Milliseconds(),
		Summary:    summary,
		StartedAt:  start,
		FinishedAt: finished,
		Group:      group,
	}
}

type groupResult struct {
	commands []protocol.VerifyCommand
}
```

注意：`groupResult` 定義在 package level，`RunGroups` 內直接使用它，不要在 `RunGroups` 內再定義匿名 struct。

- [ ] **Step 4: 確認測試通過**

Run: `go test ./internal/verify/ -v -run TestRunGroups`
Expected: 4 tests PASS

- [ ] **Step 5: 跑全部 verify 測試確認**

Run: `go test ./internal/verify/ -v`
Expected: 全部 8 tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/verify/
git commit -m "feat(F047): add RunGroups for parallel verify execution"
```

---

### Task 4: 新增 `4x verify` CLI subcommand

**Files:**
- Create: `cmd/4x/verify.go`
- Modify: `cmd/4x/main.go:23-40`

- [ ] **Step 1: 建立 `cmd/4x/verify.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/verify"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newVerifyCmd() *cobra.Command {
	var (
		round   int
		timeout time.Duration
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "verify <feature-id>",
		Short: "Run verify commands from test-strategy.yaml (groups run in parallel)",
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

			if round <= 0 {
				state, err := ws.ReadState(featureID)
				if err != nil {
					return fmt.Errorf("cannot read state.json (use --round to specify): %w", err)
				}
				round = state.Round
			}

			stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
			data, err := os.ReadFile(stratPath)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", protocol.TestStratFile, err)
			}
			var ts protocol.TestStrategy
			if err := yaml.Unmarshal(data, &ts); err != nil {
				return fmt.Errorf("invalid %s: %w", protocol.TestStratFile, err)
			}

			groups, err := verify.ResolveGroups(ts)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			fmt.Fprintf(os.Stderr, "Running %d verify group(s)...\n", len(groups))
			evidence := verify.RunGroups(ctx, groups, ws.Root)
			evidence.Round = round
			evidence.Role = protocol.RoleTester

			roundDir := ws.RoundDir(featureID, round)
			if err := os.MkdirAll(roundDir, 0o755); err != nil {
				return fmt.Errorf("create round dir: %w", err)
			}
			outData, err := json.MarshalIndent(evidence, "", "  ")
			if err != nil {
				return err
			}
			outPath := filepath.Join(roundDir, protocol.VerifyFile)
			if err := os.WriteFile(outPath, outData, 0o644); err != nil {
				return err
			}

			if jsonOut {
				fmt.Println(string(outData))
			} else {
				printVerifySummary(evidence)
			}

			if !evidence.Passed {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&round, "round", 0, "round number (default: current round from state.json)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "overall timeout for all groups")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func printVerifySummary(ev protocol.VerifyEvidence) {
	fmt.Println()
	fmt.Printf("%-12s %-40s %6s %10s\n", "GROUP", "COMMAND", "EXIT", "DURATION")
	fmt.Println(fmt.Sprintf("%s %s %s %s",
		pad("-", 12), pad("-", 40), pad("-", 6), pad("-", 10)))

	for _, c := range ev.Commands {
		status := fmt.Sprintf("%d", c.ExitCode)
		dur := fmt.Sprintf("%dms", c.DurationMs)
		if c.Skipped {
			status = "SKIP"
			dur = "-"
		}
		cmdDisplay := c.Command
		if len(cmdDisplay) > 40 {
			cmdDisplay = cmdDisplay[:37] + "..."
		}
		fmt.Printf("%-12s %-40s %6s %10s\n", c.Group, cmdDisplay, status, dur)
	}

	fmt.Println()
	if ev.Passed {
		fmt.Println("PASSED")
	} else {
		fmt.Println("FAILED")
	}
}

func pad(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
```

- [ ] **Step 2: 在 `cmd/4x/main.go` 註冊 subcommand**

在 `root.AddCommand(` 區塊內加入 `newVerifyCmd()`：

```go
root.AddCommand(
	newInitCmd(),
	newSyncCmd(),
	newNewCmd(),
	newRunCmd(),
	newStatusCmd(),
	newCheckCmd(),
	newTransitionCmd(),
	newEventCmd(),
	newPromptCmd(),
	newBatchCmd(),
	newLiveCmd(),
	newConfigCmd(),
	newDoneCmd(),
	newMergeCmd(),
	newMCPCmd(),
	newSubtaskCmd(),
	newVerifyCmd(),
)
```

- [ ] **Step 3: 驗證編譯**

Run: `go build ./cmd/4x && ./bin/4x verify --help`
Expected: 顯示 verify subcommand 的 usage

- [ ] **Step 4: 跑全部測試確認無破壞**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/verify.go cmd/4x/main.go
git commit -m "feat(F047): add 4x verify CLI subcommand"
```

---

### Task 5: CLI 整合測試

**Files:**
- Modify: `cmd/4x/cli_test.go`

- [ ] **Step 1: 寫 `4x verify` 整合測試**

在 `cmd/4x/cli_test.go` 尾部加入：

```go
func TestVerify_VerifyCommands_AllPass(t *testing.T) {
	dir := t.TempDir()
	out, err := run4x(dir, "init")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	out, err = run4x(dir, "new", "verify-test")
	if err != nil {
		t.Fatalf("new: %v\n%s", err, out)
	}

	featureID := findFeatureID(t, dir, "verify-test")

	// write state.json with round=1
	stateDir := filepath.Join(dir, ".4x", featureID)
	stateJSON := fmt.Sprintf(`{"featureId":"%s","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":false}`, featureID)
	writeTestFile(t, filepath.Join(stateDir, "state.json"), stateJSON)

	// write test-strategy.yaml with verify_commands
	writeTestFile(t, filepath.Join(stateDir, "test-strategy.yaml"), "verify_commands:\n  - \"echo ok\"\n")

	out, err = run4x(dir, "verify", featureID, "--json")
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}

	var ev protocol.VerifyEvidence
	if err := json.Unmarshal([]byte(out), &ev); err != nil {
		t.Fatalf("parse verify output: %v\n%s", err, out)
	}
	if !ev.Passed {
		t.Error("expected pass")
	}
	if ev.Round != 1 {
		t.Errorf("expected round 1, got %d", ev.Round)
	}
	if len(ev.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(ev.Commands))
	}

	// verify.json should exist on disk
	verifyPath := filepath.Join(dir, ".4x", featureID, "rounds", "round-1", "verify.json")
	if _, err := os.Stat(verifyPath); err != nil {
		t.Errorf("verify.json not written: %v", err)
	}
}

func TestVerify_VerifyGroups_Parallel(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "parallel-test")

	featureID := findFeatureID(t, dir, "parallel-test")
	stateDir := filepath.Join(dir, ".4x", featureID)
	stateJSON := fmt.Sprintf(`{"featureId":"%s","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":false}`, featureID)
	writeTestFile(t, filepath.Join(stateDir, "state.json"), stateJSON)

	strategy := "verify_groups:\n  group-a:\n    - \"echo a\"\n  group-b:\n    - \"echo b\"\n"
	writeTestFile(t, filepath.Join(stateDir, "test-strategy.yaml"), strategy)

	out, err := run4x(dir, "verify", featureID, "--json")
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}

	var ev protocol.VerifyEvidence
	if err := json.Unmarshal([]byte(out), &ev); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if !ev.Passed {
		t.Error("expected pass")
	}
	if len(ev.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(ev.Commands))
	}

	groups := map[string]bool{}
	for _, c := range ev.Commands {
		groups[c.Group] = true
	}
	if !groups["group-a"] || !groups["group-b"] {
		t.Errorf("expected both groups, got %v", groups)
	}
}

func TestVerify_BothFormats_Error(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "both-test")

	featureID := findFeatureID(t, dir, "both-test")
	stateDir := filepath.Join(dir, ".4x", featureID)
	stateJSON := fmt.Sprintf(`{"featureId":"%s","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":false}`, featureID)
	writeTestFile(t, filepath.Join(stateDir, "state.json"), stateJSON)

	strategy := "verify_commands:\n  - \"echo x\"\nverify_groups:\n  a:\n    - \"echo y\"\n"
	writeTestFile(t, filepath.Join(stateDir, "test-strategy.yaml"), strategy)

	_, err := run4x(dir, "verify", featureID)
	if err == nil {
		t.Error("expected error when both verify_commands and verify_groups present")
	}
}

func TestVerify_FailedCommand_ExitCode1(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "fail-test")

	featureID := findFeatureID(t, dir, "fail-test")
	stateDir := filepath.Join(dir, ".4x", featureID)
	stateJSON := fmt.Sprintf(`{"featureId":"%s","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":false}`, featureID)
	writeTestFile(t, filepath.Join(stateDir, "state.json"), stateJSON)

	writeTestFile(t, filepath.Join(stateDir, "test-strategy.yaml"), "verify_commands:\n  - \"false\"\n")

	_, err := run4x(dir, "verify", featureID)
	if err == nil {
		t.Error("expected non-zero exit when verify fails")
	}
}

func findFeatureID(t *testing.T, dir, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".4x", "features"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) && strings.HasSuffix(name, ".yaml") {
			return name[:len(name)-5]
		}
	}
	t.Fatalf("feature with prefix %q not found", prefix)
	return ""
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: 跑整合測試**

Run: `go test ./cmd/4x/ -v -run TestVerify`
Expected: 4 tests PASS

- [ ] **Step 3: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/cli_test.go
git commit -m "test(F047): add CLI integration tests for 4x verify"
```

---

### Task 6: 修改 Tester 模板

**Files:**
- Modify: `templates/tester.md.tmpl`

- [ ] **Step 1: 修改 tester.md.tmpl**

將 `templates/tester.md.tmpl` 替換為：

```
You are the Tester for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

== MANDATORY — write these files or the task fails ==
You MUST create these files before finishing:

  {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/test-report.md

verify.json is created by `4x verify` — do NOT write it yourself.

If verify.json passed is true, you MUST also create:

  {{.DotDir}}/{{.Feature.ID}}/final-report.md
  {{.DotDir}}/{{.Feature.ID}}/commit-plan.md

Use the Write tool. Do NOT just print the content — write to disk.

== Inputs ==
1. Read: {{.DotDir}}/{{.Feature.ID}}/acceptance-criteria.md
2. Read: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md
3. Read: {{.DotDir}}/{{.Feature.ID}}/test-strategy.yaml (for verify_commands or verify_groups)
{{- if .ProjectIncludes}}
{{range .ProjectIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if .Project.Test}}

== Project Test Commands ==
{{- range .Project.Test}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleInstructions}}

== Role Instructions ==
{{- range .RoleInstructions}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleIncludes}}
{{range .RoleIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}

== Workflow (strict order) ==
1. Read acceptance criteria — list every AC item
2. Run: 4x verify {{.Feature.ID}}
3. Read the generated verify.json for command results
4. For each AC item, collect evidence (command output, verify.json results, file check, etc.)
5. Write test-report.md
6. If verify.json passed is true, write final-report.md and commit-plan.md

== test-report.md format ==
# Test Report — Round {{.Round}}
## Summary
PASS / FAIL — N/N criteria met
## Results
| # | Criterion | Status | Evidence |
|---|---|---|---|
| AC-1 | ... | PASS/FAIL/SKIP | actual output |
## Verdict
PASS / FAIL

== Constraints ==
- Do NOT modify source code — only run tests and report
- Do NOT write verify.json — it is produced by `4x verify`
- Each AC item must have: status + evidence
- SKIP > 30% of items blocks acceptance
- Do NOT fabricate results — mark SKIP if you cannot test
- final-report.md and commit-plan.md are REQUIRED when verify.json passed is true
```

- [ ] **Step 2: 驗證模板語法**

Run: `go build ./cmd/4x && go test ./...`
Expected: 全部 PASS（模板在 build 時嵌入，語法錯會導致 test 失敗）

- [ ] **Step 3: Commit**

```bash
git add templates/tester.md.tmpl
git commit -m "feat(F047): update tester template to use 4x verify"
```

---

### Task 7: 修改 Designer 模板

**Files:**
- Modify: `templates/designer.md.tmpl:96-101`

- [ ] **Step 1: 修改 designer.md.tmpl 的 test-strategy.yaml format 區塊**

將 `templates/designer.md.tmpl` 中的：

```
== test-strategy.yaml format ==
web: false
api: false
coder_only: true
verify_commands:
  - "command here"
```

替換為：

```
== test-strategy.yaml format ==
web: false
api: false
coder_only: true
verify_commands:
  - "command here"

For multi-repo or parallel verification, use verify_groups instead:
verify_groups:
  core:
    - "make build"
    - "make test"
  other-repo:
    - "cd ../other && make test"

Rules: use verify_groups OR verify_commands, not both. Groups run in parallel; commands within each group run sequentially.
```

- [ ] **Step 2: 驗證編譯**

Run: `go build ./cmd/4x && go test ./...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add templates/designer.md.tmpl
git commit -m "feat(F047): add verify_groups format to designer template"
```

---

### Task 8: check-docs-sync 與最終驗證

**Files:**
- Possibly modify: `docs/guide/cli.md`

- [ ] **Step 1: 跑 check-docs-sync**

Run: `make check-docs-sync`
Expected: 檢查是否有 docs 需要更新（新增了 `verify` subcommand，可能需要更新 `docs/guide/cli.md`）

- [ ] **Step 2: 如有需要，更新 cli.md**

若 `check-docs-sync` 報 `NEEDS_UPDATE`，在 `docs/guide/cli.md` 加入 `4x verify` 的說明：

```markdown
### verify

Run verify commands from test-strategy.yaml. Groups run in parallel, commands within each group run sequentially.

```bash
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```
```

- [ ] **Step 3: 跑 check-docs**

Run: `make check-docs`
Expected: PASS（verify subcommand 出現在 cli.md）

- [ ] **Step 4: 跑 check-i18n**

Run: `make check-i18n`
Expected: 無缺漏 key（此 feature 不涉及 i18n）

- [ ] **Step 5: 全部測試最終確認**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit（如有 docs 更新）**

```bash
git add docs/
git commit -m "docs(F047): add verify subcommand to CLI guide"
```
