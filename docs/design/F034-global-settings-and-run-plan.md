# F034: Global Settings and Run — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將設定拆分為 user-level 和 project-level 兩層，支援 deep merge（project 覆蓋 user），Dashboard 和 CLI 各自只操作對應層級。

**Architecture:** 擴展現有 `UserConfig` struct 加入 runners/roles/default_runner/theme 欄位。新增 `MergeConfig()` 做欄位級 deep merge。`RunnerConfig` 的 bool 欄位改為 `*bool` 以區分「沒設」和「設為 false」。`4x run` 和 `4x prompt` 在讀完兩層設定後呼叫 merge。Server 新增 user-config 和 merged-config API。

**Tech Stack:** Go 1.22+, Cobra CLI, net/http, embedded HTML dashboard

**Spec:** `docs/design/F034-global-settings-and-run-spec.md`

---

## File Structure

| Action | Path | Responsibility |
|---|---|---|
| Create | `internal/protocol/merge.go` | MergeConfig + mergeRunner + mergeRole helpers |
| Create | `internal/protocol/merge_test.go` | MergeConfig 測試（各種 merge 情境） |
| Modify | `internal/protocol/types.go` | UserConfig 擴展、RunnerConfig bool→*bool、BoolVal/BoolPtr helpers |
| Modify | `internal/runner/runner.go` | 更新 .Tty/.Stdin/.Quiet 為 *bool 存取 |
| Modify | `internal/runner/runner_test.go` | 更新測試中 RunnerConfig 建構式（bool→*bool） |
| Modify | `cmd/4x/run.go` | 在 runLoop 前插入 merge 步驟 |
| Modify | `cmd/4x/prompt.go` | 在 generatePrompt 前插入 merge 步驟 |
| Modify | `cmd/4x/config.go` | dot notation get/set 支援所有 UserConfig 欄位 |
| Modify | `cmd/4x/cli_test.go` | config CLI 擴展測試 |
| Modify | `cmd/4x/run_loop_test.go` | 更新 RunnerConfig 建構式 |
| Modify | `internal/server/server.go` | 新增 /api/user-config、/api/merged-config endpoints |
| Modify | `internal/server/server_test.go` | 新 endpoint 測試 |
| Modify | `internal/server/static/index.html` | Global settings editor UI |
| Modify | `docs/guide/configuration.md` | 文件更新 |

---

### Task 1: RunnerConfig bool → *bool 遷移

**Files:**
- Modify: `internal/protocol/types.go:228-237`
- Modify: `internal/runner/runner.go:70,97,106,114`
- Modify: `internal/runner/runner_test.go`
- Modify: `cmd/4x/run_loop_test.go:111,515`

- [ ] **Step 1: 加入 BoolVal / BoolPtr helpers 並改 RunnerConfig**

在 `internal/protocol/types.go` 加入 helper 函式並修改 RunnerConfig：

```go
// BoolPtr 建立 *bool 指標，用於 RunnerConfig 的布林欄位初始化
func BoolPtr(b bool) *bool {
	return &b
}

// BoolVal 安全取值，nil 視為 false
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
```

修改 RunnerConfig 的三個 bool 欄位為 `*bool`：

```go
type RunnerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Model   string            `json:"model,omitempty"`
	Tiers   map[string]string `json:"tiers,omitempty"`
	Stdin   *bool             `json:"stdin,omitempty"`
	Tty     *bool             `json:"tty,omitempty"`
	Quiet   *bool             `json:"quiet,omitempty"`
}
```

- [ ] **Step 2: 更新 runner.go 的 bool 存取**

在 `internal/runner/runner.go` 修改四處直接存取：

```go
// line 70: r.Config.Tty → protocol.BoolVal(r.Config.Tty)
usePty := protocol.BoolVal(r.Config.Tty) && logFile != nil

// line 97: r.Config.Quiet → protocol.BoolVal(r.Config.Quiet)
if protocol.BoolVal(r.Config.Quiet) {

// line 106: r.Config.Quiet → protocol.BoolVal(r.Config.Quiet)
if protocol.BoolVal(r.Config.Quiet) {

// line 114: r.Config.Stdin → protocol.BoolVal(r.Config.Stdin)
if protocol.BoolVal(r.Config.Stdin) {
```

- [ ] **Step 3: 更新 runner_test.go 中有明確設 bool 的測試**

搜尋 `runner_test.go` 中所有 `Stdin: true` / `Tty: true` / `Quiet: true`，改為 `Stdin: protocol.BoolPtr(true)` 等。無明確設定的建構式（零值 bool 變 nil *bool）行為不變，不用改。

- [ ] **Step 4: 更新 run_loop_test.go 中的 RunnerConfig 建構式**

`run_loop_test.go:111` 和 `:515` 的 `RunnerConfig{"mock": {Command: "echo"}}` 只設了 Command，沒有 bool 欄位，零值 bool 變 nil *bool 語意不變，無需修改。確認無其他設了 bool 的地方。

- [ ] **Step 5: 驗證編譯和測試通過**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/runner/runner.go internal/runner/runner_test.go cmd/4x/run_loop_test.go
git commit -m "refactor: RunnerConfig bool fields to *bool for merge support"
```

---

### Task 2: UserConfig 擴展

**Files:**
- Modify: `internal/protocol/types.go:246-248`

- [ ] **Step 1: 寫 UserConfig 擴展的測試**

在 `internal/protocol/workspace_test.go` 新增：

```go
func TestUserConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".4x")
	os.MkdirAll(configDir, 0o755)

	// 暫時覆蓋 home dir
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	cfg := UserConfig{
		Locale:        "zh-TW",
		Theme:         "dark",
		DefaultRunner: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "/usr/local/bin/claude", Tty: BoolPtr(true)},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	if err := WriteUserConfig(cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}

	got, err := ReadUserConfig()
	if err != nil {
		t.Fatalf("ReadUserConfig: %v", err)
	}
	if got.Locale != "zh-TW" {
		t.Errorf("Locale = %q, want zh-TW", got.Locale)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", got.Theme)
	}
	if got.DefaultRunner != "claude" {
		t.Errorf("DefaultRunner = %q, want claude", got.DefaultRunner)
	}
	if got.Runners["claude"].Command != "/usr/local/bin/claude" {
		t.Errorf("Runners[claude].Command = %q", got.Runners["claude"].Command)
	}
	if !BoolVal(got.Runners["claude"].Tty) {
		t.Error("Runners[claude].Tty should be true")
	}
	if got.Roles["designer"].Model != "opus" {
		t.Errorf("Roles[designer].Model = %q, want opus", got.Roles["designer"].Model)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestUserConfig_RoundTrip -v`
Expected: FAIL（UserConfig 沒有 Theme/DefaultRunner/Runners/Roles 欄位）

- [ ] **Step 3: 擴展 UserConfig struct**

在 `internal/protocol/types.go` 修改：

```go
// UserConfig 是 ~/.4x/settings.json 的使用者層級設定
type UserConfig struct {
	Locale        string                  `json:"locale,omitempty"`
	Theme         string                  `json:"theme,omitempty"`
	DefaultRunner string                  `json:"default_runner,omitempty"`
	Runners       map[string]RunnerConfig `json:"runners,omitempty"`
	Roles         map[string]RoleConfig   `json:"roles,omitempty"`
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestUserConfig_RoundTrip -v`
Expected: PASS

- [ ] **Step 5: 驗證全部測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/protocol/workspace_test.go
git commit -m "feat: expand UserConfig with runners, roles, theme, default_runner"
```

---

### Task 3: MergeConfig 函式

**Files:**
- Create: `internal/protocol/merge.go`
- Create: `internal/protocol/merge_test.go`

- [ ] **Step 1: 寫 MergeConfig 測試**

建立 `internal/protocol/merge_test.go`：

```go
package protocol

import (
	"testing"
)

func TestMergeConfig_DefaultRunner_ProjectOverrides(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{Default: "codex", Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Default != "codex" {
		t.Errorf("Default = %q, want codex", got.Default)
	}
}

func TestMergeConfig_DefaultRunner_FallbackToUser(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Default != "claude" {
		t.Errorf("Default = %q, want claude", got.Default)
	}
}

func TestMergeConfig_Runners_FieldMerge(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {
				Command: "/usr/local/bin/claude",
				Args:    []string{"--skip", "-p", "{prompt}"},
				Tty:     BoolPtr(true),
			},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Model: "opus"},
		},
	}
	got := MergeConfig(user, proj)
	rc := got.Runners["claude"]
	if rc.Command != "/usr/local/bin/claude" {
		t.Errorf("Command = %q, want /usr/local/bin/claude", rc.Command)
	}
	if len(rc.Args) != 3 {
		t.Errorf("Args len = %d, want 3 (inherited from user)", len(rc.Args))
	}
	if !BoolVal(rc.Tty) {
		t.Error("Tty should be true (inherited from user)")
	}
	if rc.Model != "opus" {
		t.Errorf("Model = %q, want opus (from project)", rc.Model)
	}
}

func TestMergeConfig_Runners_ArgsFullReplace(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Args: []string{"--old", "-p", "{prompt}"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Args: []string{"--new", "{prompt}"}},
		},
	}
	got := MergeConfig(user, proj)
	if len(got.Runners["claude"].Args) != 2 || got.Runners["claude"].Args[0] != "--new" {
		t.Errorf("Args = %v, want [--new {prompt}]", got.Runners["claude"].Args)
	}
}

func TestMergeConfig_Runners_UserOnly(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini", Stdin: BoolPtr(true)},
		},
	}
	proj := Config{Project: ProjectConfig{Name: "x"}}
	got := MergeConfig(user, proj)
	if got.Runners["gemini"].Command != "gemini" {
		t.Errorf("user-only runner not preserved")
	}
	if !BoolVal(got.Runners["gemini"].Stdin) {
		t.Error("Stdin should be true")
	}
}

func TestMergeConfig_Runners_ProjectOnly(t *testing.T) {
	user := UserConfig{}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"codex": {Command: "codex"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Runners["codex"].Command != "codex" {
		t.Errorf("project-only runner not preserved")
	}
}

func TestMergeConfig_Runners_TiersMerge(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Tiers: map[string]string{"opus": "claude-opus-4-5"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Tiers: map[string]string{"sonnet": "claude-sonnet-4-5"}},
		},
	}
	got := MergeConfig(user, proj)
	tiers := got.Runners["claude"].Tiers
	if tiers["opus"] != "claude-opus-4-5" {
		t.Errorf("user tier not preserved: %v", tiers)
	}
	if tiers["sonnet"] != "claude-sonnet-4-5" {
		t.Errorf("project tier not merged: %v", tiers)
	}
}

func TestMergeConfig_Roles_FieldMerge(t *testing.T) {
	user := UserConfig{
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
			"coder":    {Model: "sonnet"},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Roles: map[string]RoleConfig{
			"coder": {Model: "haiku"},
		},
	}
	got := MergeConfig(user, proj)
	if got.Roles["designer"].Model != "opus" {
		t.Errorf("designer.Model = %q, want opus (from user)", got.Roles["designer"].Model)
	}
	if got.Roles["coder"].Model != "haiku" {
		t.Errorf("coder.Model = %q, want haiku (project overrides)", got.Roles["coder"].Model)
	}
}

func TestMergeConfig_Roles_InstructionsMerge(t *testing.T) {
	user := UserConfig{
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet", Instructions: []string{"user rule"}},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Roles: map[string]RoleConfig{
			"coder": {Instructions: []string{"project rule"}},
		},
	}
	got := MergeConfig(user, proj)
	// project Instructions 非 nil → 整組替換
	if len(got.Roles["coder"].Instructions) != 1 || got.Roles["coder"].Instructions[0] != "project rule" {
		t.Errorf("Instructions = %v, want [project rule]", got.Roles["coder"].Instructions)
	}
	// Model 從 user fallback（project 沒設）
	if got.Roles["coder"].Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet (from user)", got.Roles["coder"].Model)
	}
}

func TestMergeConfig_ProjectOnlyFields_Preserved(t *testing.T) {
	user := UserConfig{DefaultRunner: "claude"}
	proj := Config{
		Project:           ProjectConfig{Name: "my-proj", Language: "go"},
		Isolation:         "worktree",
		Commit:            "per-round",
		MaxConcurrentRuns: 3,
		HubRepos:          []string{"shared-lib"},
		Rules:             []string{"no LLM in CLI"},
	}
	got := MergeConfig(user, proj)
	if got.Project.Name != "my-proj" {
		t.Errorf("Project.Name = %q", got.Project.Name)
	}
	if got.Isolation != "worktree" {
		t.Errorf("Isolation = %q", got.Isolation)
	}
	if got.Commit != "per-round" {
		t.Errorf("Commit = %q", got.Commit)
	}
	if got.MaxConcurrentRuns != 3 {
		t.Errorf("MaxConcurrentRuns = %d", got.MaxConcurrentRuns)
	}
}

func TestMergeConfig_BoolOverride(t *testing.T) {
	user := UserConfig{
		Runners: map[string]RunnerConfig{
			"claude": {Tty: BoolPtr(true)},
		},
	}
	proj := Config{
		Project: ProjectConfig{Name: "x"},
		Runners: map[string]RunnerConfig{
			"claude": {Tty: BoolPtr(false)},
		},
	}
	got := MergeConfig(user, proj)
	if BoolVal(got.Runners["claude"].Tty) {
		t.Error("Tty should be false (project explicitly set false)")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestMergeConfig -v`
Expected: FAIL（MergeConfig 未定義）

- [ ] **Step 3: 實作 MergeConfig**

建立 `internal/protocol/merge.go`：

```go
package protocol

// MergeConfig 合併 user-level 和 project-level 設定。
// project 非零值欄位覆蓋 user；project-only 欄位（Project、Isolation 等）直接使用 project 的值。
func MergeConfig(user UserConfig, project Config) Config {
	result := project

	if result.Default == "" && user.DefaultRunner != "" {
		result.Default = user.DefaultRunner
	}

	result.Runners = mergeRunners(user.Runners, project.Runners)
	result.Roles = mergeRoles(user.Roles, project.Roles)

	return result
}

func mergeRunners(user, project map[string]RunnerConfig) map[string]RunnerConfig {
	merged := make(map[string]RunnerConfig)

	for name, rc := range user {
		merged[name] = rc
	}

	for name, prc := range project {
		if urc, ok := merged[name]; ok {
			merged[name] = mergeRunner(urc, prc)
		} else {
			merged[name] = prc
		}
	}

	return merged
}

func mergeRunner(user, project RunnerConfig) RunnerConfig {
	result := user

	if project.Command != "" {
		result.Command = project.Command
	}
	if project.Args != nil {
		result.Args = project.Args
	}
	if project.Model != "" {
		result.Model = project.Model
	}
	if project.Tiers != nil {
		if result.Tiers == nil {
			result.Tiers = make(map[string]string)
		}
		for k, v := range project.Tiers {
			result.Tiers[k] = v
		}
	}
	if project.Stdin != nil {
		result.Stdin = project.Stdin
	}
	if project.Tty != nil {
		result.Tty = project.Tty
	}
	if project.Quiet != nil {
		result.Quiet = project.Quiet
	}

	return result
}

func mergeRoles(user, project map[string]RoleConfig) map[string]RoleConfig {
	merged := make(map[string]RoleConfig)

	for name, rc := range user {
		merged[name] = rc
	}

	for name, prc := range project {
		if urc, ok := merged[name]; ok {
			merged[name] = mergeRole(urc, prc)
		} else {
			merged[name] = prc
		}
	}

	return merged
}

func mergeRole(user, project RoleConfig) RoleConfig {
	result := user

	if project.Model != "" {
		result.Model = project.Model
	}
	if project.DeepModel != "" {
		result.DeepModel = project.DeepModel
	}
	if project.Instructions != nil {
		result.Instructions = project.Instructions
	}
	if project.Includes != nil {
		result.Includes = project.Includes
	}

	return result
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestMergeConfig -v`
Expected: 全部 PASS

- [ ] **Step 5: 驗證全部測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/merge.go internal/protocol/merge_test.go
git commit -m "feat: add MergeConfig for user+project settings deep merge"
```

---

### Task 4: 在 `4x run` 和 `4x prompt` 插入 merge

**Files:**
- Modify: `cmd/4x/run.go:63-72`
- Modify: `cmd/4x/prompt.go:54-55`

- [ ] **Step 1: 寫整合測試**

在 `cmd/4x/run_loop_test.go` 新增：

```go
func TestRunLoop_MergedConfig(t *testing.T) {
	ws := setupTestWorkspace(t)
	featureID := "test-merge"
	setupFeature(t, ws, featureID)

	// project config 沒有 runners
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Default: "mock",
	}
	protocol.WriteConfig(ws.DotDir(), cfg)

	// user config 有 runner 定義
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	userCfg := protocol.UserConfig{
		Runners: map[string]protocol.RunnerConfig{
			"mock": {Command: "echo"},
		},
	}
	protocol.WriteUserConfig(userCfg)

	// merge 後 mock runner 應該可用
	merged := protocol.MergeConfig(userCfg, cfg)
	if _, ok := merged.Runners["mock"]; !ok {
		t.Fatal("merged config should have mock runner from user config")
	}
	if merged.Runners["mock"].Command != "echo" {
		t.Errorf("Command = %q, want echo", merged.Runners["mock"].Command)
	}
}
```

- [ ] **Step 2: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestRunLoop_MergedConfig -v`
Expected: PASS

- [ ] **Step 3: 修改 `4x run` 加入 merge**

在 `cmd/4x/run.go` 的 RunE 裡，`ws.ReadConfig()` 之後加入 merge：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    // ... existing error handling
}

// merge user-level settings
userCfg, _ := protocol.ReadUserConfig()
cfg = protocol.MergeConfig(userCfg, cfg)

if runnerName == "" {
    runnerName = cfg.Default
}
```

- [ ] **Step 4: 修改 `4x prompt` 加入 merge**

在 `cmd/4x/prompt.go` 的 RunE 裡，`ws.ReadConfig()` 之後：

```go
cfg, _ := ws.ReadConfig()
userCfg, _ := protocol.ReadUserConfig()
cfg = protocol.MergeConfig(userCfg, cfg)
```

同樣在 `run.go` 的 `generatePrompt` 中，locale 仍從 `resolveLocale()` 取（它已經直接讀 UserConfig），不需改。

- [ ] **Step 5: 驗證編譯和測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/run.go cmd/4x/prompt.go cmd/4x/run_loop_test.go
git commit -m "feat: wire MergeConfig into 4x run and 4x prompt"
```

---

### Task 5: 擴展 `4x config` CLI

**Files:**
- Modify: `cmd/4x/config.go`
- Modify: `cmd/4x/cli_test.go`

- [ ] **Step 1: 寫 config get/set 擴展測試**

在 `cmd/4x/cli_test.go` 新增：

```go
func TestConfigSet_Theme(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"theme", "dark"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set theme: %v", err)
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", cfg.Theme)
	}
}

func TestConfigSet_DefaultRunner(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"default_runner", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set default_runner: %v", err)
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.DefaultRunner != "codex" {
		t.Errorf("DefaultRunner = %q, want codex", cfg.DefaultRunner)
	}
}

func TestConfigSet_RunnerCommand(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"runner.claude.command", "/usr/local/bin/claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set runner.claude.command: %v", err)
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.Runners["claude"].Command != "/usr/local/bin/claude" {
		t.Errorf("Command = %q", cfg.Runners["claude"].Command)
	}
}

func TestConfigSet_RunnerTty(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"runner.claude.tty", "true"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set runner.claude.tty: %v", err)
	}

	cfg, _ := protocol.ReadUserConfig()
	if !protocol.BoolVal(cfg.Runners["claude"].Tty) {
		t.Error("Tty should be true")
	}
}

func TestConfigSet_RoleModel(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"role.designer.model", "opus"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set role.designer.model: %v", err)
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.Roles["designer"].Model != "opus" {
		t.Errorf("Model = %q, want opus", cfg.Roles["designer"].Model)
	}
}

func TestConfigSet_RunnerArgs_Rejected(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"runner.claude.args", "foo"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for args field")
	}
}

func TestConfigGet_RunnerCommand(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	cfg := protocol.UserConfig{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "/opt/claude"},
		},
	}
	protocol.WriteUserConfig(cfg)

	cmd := newConfigGetCmd()
	cmd.SetArgs([]string{"runner.claude.command"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config get runner.claude.command: %v", err)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestConfigSet_Theme -v`
Expected: FAIL（config set 不認得 theme）

- [ ] **Step 3: 重寫 config set 支援 dot notation**

替換 `cmd/4x/config.go` 的 `newConfigSetCmd` 實作：

```go
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}

			key, value := args[0], args[1]
			parts := strings.Split(key, ".")

			switch {
			case len(parts) == 1:
				switch key {
				case "locale":
					cfg.Locale = value
				case "theme":
					cfg.Theme = value
				case "default_runner":
					cfg.DefaultRunner = value
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

			case len(parts) == 3 && parts[0] == "runner":
				runnerName, field := parts[1], parts[2]
				if cfg.Runners == nil {
					cfg.Runners = make(map[string]protocol.RunnerConfig)
				}
				rc := cfg.Runners[runnerName]
				switch field {
				case "command":
					rc.Command = value
				case "model":
					rc.Model = value
				case "tty":
					b := value == "true"
					rc.Tty = protocol.BoolPtr(b)
				case "stdin":
					b := value == "true"
					rc.Stdin = protocol.BoolPtr(b)
				case "quiet":
					b := value == "true"
					rc.Quiet = protocol.BoolPtr(b)
				case "args":
					return fmt.Errorf("args is an array field — edit ~/.4x/settings.json directly")
				default:
					return fmt.Errorf("unknown runner field: %s", field)
				}
				cfg.Runners[runnerName] = rc

			case len(parts) == 3 && parts[0] == "role":
				roleName, field := parts[1], parts[2]
				if cfg.Roles == nil {
					cfg.Roles = make(map[string]protocol.RoleConfig)
				}
				rc := cfg.Roles[roleName]
				switch field {
				case "model":
					rc.Model = value
				case "deep_model":
					rc.DeepModel = value
				default:
					return fmt.Errorf("unknown role field: %s", field)
				}
				cfg.Roles[roleName] = rc

			default:
				return fmt.Errorf("unknown config key: %s", key)
			}

			if err := protocol.WriteUserConfig(cfg); err != nil {
				return err
			}
			path, _ := protocol.UserConfigPath()
			fmt.Printf("Set %s = %s in %s\n", key, value, path)
			return nil
		},
	}
}
```

- [ ] **Step 4: 重寫 config get 支援 dot notation**

替換 `cmd/4x/config.go` 的 `newConfigGetCmd` 實作：

```go
func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}

			key := args[0]
			parts := strings.Split(key, ".")

			switch {
			case len(parts) == 1:
				switch key {
				case "locale":
					printOrDefault(cfg.Locale, "en")
				case "theme":
					printOrDefault(cfg.Theme, "")
				case "default_runner":
					printOrDefault(cfg.DefaultRunner, "")
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

			case len(parts) == 3 && parts[0] == "runner":
				runnerName, field := parts[1], parts[2]
				rc, ok := cfg.Runners[runnerName]
				if !ok {
					fmt.Println("(not set)")
					return nil
				}
				switch field {
				case "command":
					printOrDefault(rc.Command, "")
				case "model":
					printOrDefault(rc.Model, "")
				case "tty":
					fmt.Println(protocol.BoolVal(rc.Tty))
				case "stdin":
					fmt.Println(protocol.BoolVal(rc.Stdin))
				case "quiet":
					fmt.Println(protocol.BoolVal(rc.Quiet))
				default:
					return fmt.Errorf("unknown runner field: %s", field)
				}

			case len(parts) == 3 && parts[0] == "role":
				roleName, field := parts[1], parts[2]
				rc, ok := cfg.Roles[roleName]
				if !ok {
					fmt.Println("(not set)")
					return nil
				}
				switch field {
				case "model":
					printOrDefault(rc.Model, "")
				case "deep_model":
					printOrDefault(rc.DeepModel, "")
				default:
					return fmt.Errorf("unknown role field: %s", field)
				}

			default:
				return fmt.Errorf("unknown config key: %s", key)
			}
			return nil
		},
	}
}

func printOrDefault(val, def string) {
	if val == "" {
		if def != "" {
			fmt.Printf("(not set, default: %s)\n", def)
		} else {
			fmt.Println("(not set)")
		}
	} else {
		fmt.Println(val)
	}
}
```

確保 `config.go` 頂部 import 包含 `"strings"`。

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run 'TestConfigSet|TestConfigGet' -v`
Expected: 全部 PASS

- [ ] **Step 6: 驗證全部測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/config.go cmd/4x/cli_test.go
git commit -m "feat: expand 4x config CLI with dot notation for all user settings"
```

---

### Task 6: Server API endpoints

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫 endpoint 測試**

在 `internal/server/server_test.go` 新增：

```go
func TestGetUserConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	userCfg := protocol.UserConfig{Locale: "zh-TW", Theme: "dark"}
	protocol.WriteUserConfig(userCfg)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/user-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got protocol.UserConfig
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Locale != "zh-TW" {
		t.Errorf("Locale = %q", got.Locale)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme = %q", got.Theme)
	}
}

func TestGetUserConfig_NotExists(t *testing.T) {
	ws := setupServerWorkspace(t)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/user-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (empty config)", rec.Code)
	}
}

func TestPutUserConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)
	protocol.WriteUserConfig(protocol.UserConfig{Locale: "en"})

	body := `{"locale":"ja","theme":"light"}`
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/user-config", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.Locale != "ja" {
		t.Errorf("Locale = %q, want ja", cfg.Locale)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light", cfg.Theme)
	}
}

func TestPutUserConfig_BackupCreated(t *testing.T) {
	ws := setupServerWorkspace(t)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".4x"), 0o755)
	protocol.WriteUserConfig(protocol.UserConfig{Locale: "en"})

	body := `{"locale":"ja"}`
	serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/user-config", body)

	bakPath := filepath.Join(tmpHome, ".4x", "settings.json.bak")
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestGetMergedConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	userCfg := protocol.UserConfig{
		DefaultRunner: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "/opt/claude"},
		},
	}
	protocol.WriteUserConfig(userCfg)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/merged-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Runners["claude"].Command != "/opt/claude" {
		t.Errorf("merged runner command = %q", got.Runners["claude"].Command)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run 'TestGetUserConfig|TestPutUserConfig|TestGetMergedConfig' -v`
Expected: FAIL（404 for /api/user-config）

- [ ] **Step 3: 在 server.go 註冊新 endpoint 和實作 handler**

在 `NewMux` 的 `mux.HandleFunc("/api/settings", ...)` 之後加入：

```go
mux.HandleFunc("/api/user-config", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        handleGetUserConfig(w)
        return
    }
    if r.Method == http.MethodPut {
        handlePutUserConfig(w, r)
        return
    }
    http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
})
mux.HandleFunc("/api/merged-config", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        handleGetMergedConfig(ws, w)
        return
    }
    http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
})
```

在 `server.go` 底部加入 handler 函式：

```go
var userConfigMu sync.Mutex

// handleGetUserConfig 讀取 ~/.4x/settings.json 回傳 user config
func handleGetUserConfig(w http.ResponseWriter) {
	cfg, err := protocol.ReadUserConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutUserConfig 接受 user config JSON，驗證後備份並寫入
func handlePutUserConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read error: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	var cfg protocol.UserConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	userConfigMu.Lock()
	defer userConfigMu.Unlock()

	path, err := protocol.UserConfigPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if oldData, err := os.ReadFile(path); err == nil {
		os.WriteFile(path+".bak", oldData, 0o644)
	}

	if err := protocol.WriteUserConfig(cfg); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result, _ := json.MarshalIndent(cfg, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

// handleGetMergedConfig 回傳 user + project merge 後的最終 config
func handleGetMergedConfig(ws *protocol.Workspace, w http.ResponseWriter) {
	projectCfg, err := ws.ReadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userCfg, _ := protocol.ReadUserConfig()
	merged := protocol.MergeConfig(userCfg, projectCfg)

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -run 'TestGetUserConfig|TestPutUserConfig|TestGetMergedConfig' -v`
Expected: 全部 PASS

- [ ] **Step 5: 驗證全部測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add /api/user-config and /api/merged-config endpoints"
```

---

### Task 7: Dashboard global settings UI

**Files:**
- Modify: `internal/server/static/index.html`

此 task 需要在瀏覽器中測試。先啟動 `4x live`，然後在瀏覽器確認。

- [ ] **Step 1: 了解現有 settings editor 結構**

讀取 `internal/server/static/index.html`，找到現有 project settings editor 的 HTML 和 JavaScript 區塊。重點了解：
- settings modal 的 open/close 機制
- form view vs JSON view 切換邏輯
- save 時呼叫的 API

- [ ] **Step 2: 在 header 加入 Global Settings 按鈕**

在現有 project settings gear icon 旁邊加一個 "Global" 按鈕：

```html
<button onclick="openGlobalSettings()" title="Global Settings" style="...">🌐</button>
```

- [ ] **Step 3: 加入 global settings modal**

複製現有 project settings modal 的結構，建立 `globalSettingsModal`。form view 只顯示 UserConfig 的欄位：
- Locale (text input)
- Theme (select: light/dark)
- Default Runner (text input)
- Runners (可展開的巢狀 form，每個 runner 有 command、model、tty、stdin、quiet)
- Roles (可展開的巢狀 form，每個 role 有 model、deep_model)

JSON view 顯示 `~/.4x/settings.json` 的原始 JSON，可直接編輯。

- [ ] **Step 4: 實作 load/save 邏輯**

```javascript
async function openGlobalSettings() {
  const res = await fetch('/api/user-config');
  const cfg = await res.json();
  // populate form or JSON editor
  globalSettingsModal.style.display = 'flex';
}

async function saveGlobalSettings() {
  // collect from form or JSON editor
  const res = await fetch('/api/user-config', {
    method: 'PUT',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(cfg)
  });
  if (res.ok) {
    globalSettingsModal.style.display = 'none';
  }
}
```

- [ ] **Step 5: 在 project settings editor 加入 merged view tab**

在 project settings modal 底部加一個 "Merged Config" tab，載入 `/api/merged-config` 並以唯讀 JSON 顯示。

- [ ] **Step 6: 啟動 4x live 測試**

Run: `4x live`

在瀏覽器打開 dashboard：
1. 點 Global Settings 按鈕 → modal 開啟，顯示 user config
2. 修改 locale → save → 重開確認值保存
3. 新增 runner → save → 確認 JSON 正確
4. 開啟 project settings → 切到 Merged Config tab → 確認顯示合併結果
5. 確認 project settings form 不顯示 user-level 繼承的值

- [ ] **Step 7: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat: add global settings editor to dashboard"
```

---

### Task 8: 文件更新

**Files:**
- Modify: `docs/guide/configuration.md`

- [ ] **Step 1: 更新 User Config 段落**

在 `docs/guide/configuration.md` 的 `## User Config` 段落，補充新欄位：

```markdown
## User Config (`~/.4x/settings.json`)

Global user preferences and runner defaults. Managed via `4x config` or the dashboard's Global Settings editor.

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### User Config Fields

| Field | Description |
|---|---|
| `locale` | Language for role prompt instructions |
| `theme` | Dashboard theme |
| `default_runner` | Default runner name (overridden by project) |
| `runners` | Runner definitions (command, args, tty, etc.) |
| `roles` | Role model defaults |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```
```

- [ ] **Step 2: 加入 Merge 段落**

在 User Config 段落之後加入：

```markdown
## Settings Merge

When `4x run` or `4x prompt` executes, user-level and project-level settings are merged:

- **Priority:** project > user > defaults
- **Runner merge:** per-field — project's non-zero fields override user's. `args` replaces entirely (not appended). `tiers` merges at key level.
- **Role merge:** per-field — same as runner.
- **Project-only fields** (`project`, `isolation`, `commit`, `max_concurrent_runs`, `hub_repos`, `rules`): always from project config.

The dashboard's project settings editor shows the **raw** project config, not the merged result. A separate "Merged Config" view shows the final effective settings.
```

- [ ] **Step 3: 驗證 docs 完整性**

Run: `make check-docs 2>/dev/null || echo "no check-docs target"`

確認文件與 CLI 指令一致。

- [ ] **Step 4: Commit**

```bash
git add docs/guide/configuration.md
git commit -m "docs: update configuration guide for global settings and merge"
```
