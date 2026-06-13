# F046: Health Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Testing phase 開始前自動跑 health check commands，失敗嘗試 recovery，再失敗 escalate 到 needs-attention。

**Architecture:** 新增 `internal/health/health.go` 封裝 check → recovery → retry 邏輯，透過 `Executor` 介面抽象 command 執行以利測試。在 `cmd/4x/run.go` 的 testing phase 啟動前呼叫。Config 兩層 merge：settings.json 全域 + test-strategy.yaml per-feature。

**Tech Stack:** Go 1.26+, 標準 testing package

**前置依賴：** F045（phase-hooks）必須先完成。本 plan 假設 F045 已實作。

---

### Task 1: HealthCheck 型別定義

**Files:**
- Modify: `internal/protocol/types.go:210-216` (TestStrategy struct)
- Modify: `internal/protocol/types.go:240-252` (Config struct)

- [ ] **Step 1: 在 TestStrategy 和 Config 加入 HealthCheck 欄位**

在 `internal/protocol/types.go`，先在 `TestStrategy` struct 之前加入 `HealthCheck` struct：

```go
// HealthCheck 是 testing phase 前的環境檢查設定
type HealthCheck struct {
	Commands []string `yaml:"commands" json:"commands"`
	Recovery []string `yaml:"recovery,omitempty" json:"recovery,omitempty"`
	Timeout  int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}
```

在 `TestStrategy` struct 加入欄位：

```go
type TestStrategy struct {
	Web         bool         `yaml:"web" json:"web"`
	API         bool         `yaml:"api" json:"api"`
	Gate        bool         `yaml:"gate" json:"gate"`
	CoderOnly   bool         `yaml:"coder_only" json:"coder_only"`
	Verify      []string     `yaml:"verify_commands" json:"verify_commands"`
	HealthCheck *HealthCheck `yaml:"health_check,omitempty" json:"health_check,omitempty"`
}
```

在 `Config` struct 加入欄位：

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
	HealthCheck       *HealthCheck                 `json:"health_check,omitempty"`
}
```

- [ ] **Step 2: 編譯確認無錯誤**

Run: `go build ./... && go vet ./...`
Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F046): add HealthCheck type to Config and TestStrategy"
```

---

### Task 2: RunHealthCheck 核心邏輯 — TDD

**Files:**
- Create: `internal/health/health.go`
- Create: `internal/health/health_test.go`

- [ ] **Step 1: 寫全部通過的 failing test**

在 `internal/health/health_test.go`：

```go
package health

import (
	"fmt"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestRunHealthCheck_AllPass(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db", "check-redis"},
	}
	executor := func(cmd string) error { return nil }

	err := RunHealthCheck(cfg, executor)
	if err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/health/ -run TestRunHealthCheck_AllPass -v`
Expected: FAIL — package 不存在

- [ ] **Step 3: 寫最小實作讓測試通過**

在 `internal/health/health.go`：

```go
package health

import (
	"github.com/ggwhite/4x/internal/protocol"
)

// RunHealthCheck 執行 health check commands，失敗時嘗試 recovery，再失敗回傳 error
func RunHealthCheck(cfg protocol.HealthCheck, executor func(cmd string) error) error {
	for _, cmd := range cfg.Commands {
		if err := executor(cmd); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/health/ -run TestRunHealthCheck_AllPass -v`
Expected: PASS

- [ ] **Step 5: 寫 check 失敗 + 無 recovery 的 failing test**

在 `internal/health/health_test.go` 加入：

```go
func TestRunHealthCheck_FailNoRecovery(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
	}
	executor := func(cmd string) error {
		return fmt.Errorf("exit 1")
	}

	err := RunHealthCheck(cfg, executor)
	if err == nil {
		t.Error("expected error")
	}
}
```

- [ ] **Step 6: 執行測試確認通過**

Run: `go test ./internal/health/ -run TestRunHealthCheck_FailNoRecovery -v`
Expected: PASS（現有實作已回傳 error）

- [ ] **Step 7: 寫 check 失敗 + recovery 成功的 failing test**

```go
func TestRunHealthCheck_FailRecoverySuccess(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
		Recovery: []string{"restart-db"},
	}
	attempt := 0
	executor := func(cmd string) error {
		if cmd == "check-db" {
			attempt++
			if attempt == 1 {
				return fmt.Errorf("exit 1")
			}
			return nil
		}
		return nil // recovery succeeds
	}

	err := RunHealthCheck(cfg, executor)
	if err != nil {
		t.Errorf("expected pass after recovery, got %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected 2 check attempts, got %d", attempt)
	}
}
```

- [ ] **Step 8: 執行測試確認失敗**

Run: `go test ./internal/health/ -run TestRunHealthCheck_FailRecoverySuccess -v`
Expected: FAIL — 現有實作沒有 recovery 邏輯

- [ ] **Step 9: 實作完整 check → recovery → retry 邏輯**

更新 `internal/health/health.go`：

```go
package health

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// RunHealthCheck 執行 health check commands，失敗時嘗試 recovery，再失敗回傳 error
func RunHealthCheck(cfg protocol.HealthCheck, executor func(cmd string) error) error {
	err := runCommands(cfg.Commands, executor)
	if err == nil {
		return nil
	}

	if len(cfg.Recovery) == 0 {
		return fmt.Errorf("health check failed: %w", err)
	}

	if recErr := runCommands(cfg.Recovery, executor); recErr != nil {
		return fmt.Errorf("health check failed: %w; recovery also failed: %v", err, recErr)
	}

	if retryErr := runCommands(cfg.Commands, executor); retryErr != nil {
		return fmt.Errorf("health check failed after recovery: %w", retryErr)
	}

	return nil
}

func runCommands(cmds []string, executor func(cmd string) error) error {
	for _, cmd := range cmds {
		if err := executor(cmd); err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return nil
}
```

- [ ] **Step 10: 執行測試確認通過**

Run: `go test ./internal/health/ -run TestRunHealthCheck -v`
Expected: 全部 PASS

- [ ] **Step 11: 寫 recovery 也失敗的 test**

```go
func TestRunHealthCheck_FailRecoveryFail(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
		Recovery: []string{"restart-db"},
	}
	executor := func(cmd string) error {
		return fmt.Errorf("exit 1")
	}

	err := RunHealthCheck(cfg, executor)
	if err == nil {
		t.Error("expected error")
	}
}
```

- [ ] **Step 12: 執行測試確認通過**

Run: `go test ./internal/health/ -run TestRunHealthCheck_FailRecoveryFail -v`
Expected: PASS

- [ ] **Step 13: 寫空 commands 的 test（直接通過）**

```go
func TestRunHealthCheck_NoCommands(t *testing.T) {
	cfg := protocol.HealthCheck{}
	executor := func(cmd string) error {
		t.Error("executor should not be called")
		return nil
	}

	err := RunHealthCheck(cfg, executor)
	if err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}
```

- [ ] **Step 14: 執行全部 health test 確認通過**

Run: `go test ./internal/health/ -v`
Expected: 全部 PASS

- [ ] **Step 15: Commit**

```bash
git add internal/health/health.go internal/health/health_test.go
git commit -m "feat(F046): implement RunHealthCheck with recovery and retry"
```

---

### Task 3: Config merge 邏輯

**Files:**
- Create: `internal/health/resolve.go`
- Create: `internal/health/resolve_test.go`

- [ ] **Step 1: 寫 ResolveHealthCheck 的 failing tests**

在 `internal/health/resolve_test.go`：

```go
package health

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveHealthCheck_FeatureOverride(t *testing.T) {
	global := &protocol.HealthCheck{Commands: []string{"make build"}, Timeout: 30}
	feature := &protocol.HealthCheck{Commands: []string{"curl localhost"}, Timeout: 60}

	result := ResolveHealthCheck(global, feature)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if len(result.Commands) != 1 || result.Commands[0] != "curl localhost" {
		t.Errorf("commands = %v, want [curl localhost]", result.Commands)
	}
	if result.Timeout != 60 {
		t.Errorf("timeout = %d, want 60", result.Timeout)
	}
}

func TestResolveHealthCheck_GlobalOnly(t *testing.T) {
	global := &protocol.HealthCheck{Commands: []string{"make build"}, Timeout: 30}

	result := ResolveHealthCheck(global, nil)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Commands[0] != "make build" {
		t.Errorf("commands = %v, want [make build]", result.Commands)
	}
}

func TestResolveHealthCheck_NoneConfigured(t *testing.T) {
	result := ResolveHealthCheck(nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestResolveHealthCheck_FeatureOnly(t *testing.T) {
	feature := &protocol.HealthCheck{Commands: []string{"check-api"}}

	result := ResolveHealthCheck(nil, feature)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Commands[0] != "check-api" {
		t.Errorf("commands = %v, want [check-api]", result.Commands)
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/health/ -run TestResolveHealthCheck -v`
Expected: FAIL — `ResolveHealthCheck` 不存在

- [ ] **Step 3: 實作 ResolveHealthCheck**

在 `internal/health/resolve.go`：

```go
package health

import "github.com/ggwhite/4x/internal/protocol"

// ResolveHealthCheck 合併全域與 per-feature 的 health check 設定
// per-feature 有設就整組覆蓋全域，沒設就用全域。兩者都沒有回傳 nil。
func ResolveHealthCheck(global, feature *protocol.HealthCheck) *protocol.HealthCheck {
	if feature != nil {
		return feature
	}
	return global
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/health/ -run TestResolveHealthCheck -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/health/resolve.go internal/health/resolve_test.go
git commit -m "feat(F046): add ResolveHealthCheck for two-layer config merge"
```

---

### Task 4: 整合進 run loop

**Files:**
- Modify: `cmd/4x/run.go:373-376`

- [ ] **Step 1: 在 run.go import health package**

在 `cmd/4x/run.go` 的 import 區塊加入：

```go
"github.com/ggwhite/4x/internal/health"
```

- [ ] **Step 2: 在 testing phase 清理邏輯之後插入 health check**

在 `cmd/4x/run.go` 的 `runLoop` 函式中，找到這段（約行 373-376）：

```go
if phase == protocol.PhaseTesting {
    os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport))
    os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.CommitPlan))
}
```

在這段之後、`var model string` 之前，插入：

```go
if phase == protocol.PhaseTesting {
    var testStrat protocol.TestStrategy
    stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
    if data, err := os.ReadFile(stratPath); err == nil {
        _ = yaml.Unmarshal(data, &testStrat)
    }

    hc := health.ResolveHealthCheck(cfg.HealthCheck, testStrat.HealthCheck)
    if hc != nil {
        fmt.Printf("[round %d] testing — running health check\n", s.Round)
        executor := func(cmd string) error {
            out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
            if err != nil {
                fmt.Fprintf(os.Stderr, "  health check failed: %s\n%s\n", cmd, string(out))
            }
            return err
        }
        if err := health.RunHealthCheck(*hc, executor); err != nil {
            ws.AppendEvent(featureID, protocol.Event{
                Type:   "health-check-failed",
                Phase:  s.Phase,
                Role:   protocol.RoleTester,
                Round:  s.Round,
                Detail: err.Error(),
                Runner: s.Runner,
            })
            newState, transErr := state.Transition(s, protocol.PhaseNeedsAttention, "")
            if transErr != nil {
                return fmt.Errorf("health check transition: %w", transErr)
            }
            s = newState
            s.Active = false
            s.StopReason = "health-check-failed"
            _ = ws.WriteState(featureID, s)
            _ = syncFeatureStatus(ws, featureID, s.Phase)
            fmt.Printf("  health check failed, escalated to needs-attention\n")
            return nil
        }
        fmt.Printf("[round %d] testing — health check passed\n", s.Round)
    }
}
```

- [ ] **Step 3: 確認 import 包含 `os/exec` 和 `gopkg.in/yaml.v3`**

檢查 `cmd/4x/run.go` 的 import，如果沒有 `os/exec` 和 `gopkg.in/yaml.v3` 就加上。

- [ ] **Step 4: 編譯確認無錯誤**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 5: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/run.go
git commit -m "feat(F046): integrate health check into testing phase of run loop"
```

---

### Task 5: 文件更新與收尾

**Files:**
- Modify: `.4x/features/F046-health-check.yaml`

- [ ] **Step 1: 跑 doc sync 檢查**

Run: `make check-docs-sync`

如果輸出 `NEEDS_UPDATE`，更新被點名的檔案。

- [ ] **Step 2: 最終驗證**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS
