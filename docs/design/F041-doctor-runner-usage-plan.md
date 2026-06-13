# F041: Doctor Per-Runner Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每個 runner 顯示 rate limit 百分比（5h/7d），Claude 透過 statusLine chain wrapper，Codex 透過 sqlite

**Architecture:** `4x _statusline` 作為 chain wrapper 攔截 Claude Code 的 stdin JSON 並存 rate_limits 到 `~/.4x/usage/claude.json`；Codex 直接讀 `~/.codex/logs_2.sqlite`；Doctor 合併所有來源產出 per-runner 卡片

**Tech Stack:** Go 1.26+, Cobra, SQLite (database/sql + modernc.org/sqlite), ccusage CLI

---

### Task 1: RateLimit 型別與 claude.json reader

**Files:**
- Modify: `internal/doctor/types.go`
- Create: `internal/doctor/claude.go`
- Create: `internal/doctor/claude_test.go`

- [ ] **Step 1: 寫 claude.json reader 的測試**

```go
// internal/doctor/claude_test.go
package doctor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadClaudeRateLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	os.WriteFile(path, []byte(`{
		"updated_at": "`+time.Now().Format(time.RFC3339)+`",
		"five_hour": {"used_percentage": 12, "resets_at": 1781370818},
		"seven_day": {"used_percentage": 7, "resets_at": 1781763687}
	}`), 0644)

	rl, err := readClaudeRateLimits(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.FiveHour == nil || rl.FiveHour.UsedPercentage != 12 {
		t.Errorf("five_hour.used_percentage = %v, want 12", rl.FiveHour)
	}
	if rl.SevenDay == nil || rl.SevenDay.UsedPercentage != 7 {
		t.Errorf("seven_day.used_percentage = %v, want 7", rl.SevenDay)
	}
}

func TestReadClaudeRateLimits_Expired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	old := time.Now().Add(-15 * time.Minute).Format(time.RFC3339)
	os.WriteFile(path, []byte(`{
		"updated_at": "`+old+`",
		"five_hour": {"used_percentage": 50, "resets_at": 0},
		"seven_day": {"used_percentage": 20, "resets_at": 0}
	}`), 0644)

	rl, err := readClaudeRateLimits(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.FiveHour != nil {
		t.Error("expected nil for expired data")
	}
}

func TestReadClaudeRateLimits_NotExist(t *testing.T) {
	rl, err := readClaudeRateLimits("/nonexistent/claude.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.FiveHour != nil || rl.SevenDay != nil {
		t.Error("expected nil for missing file")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/doctor/ -run TestReadClaude -v`
Expected: FAIL — `readClaudeRateLimits` 未定義

- [ ] **Step 3: 新增 RateLimit 型別到 types.go**

在 `internal/doctor/types.go` 的 `RunnerUsage` 之前加入：

```go
// RateLimit 是單一時間窗口的 rate limit 狀態
type RateLimit struct {
	UsedPercentage int   `json:"used_percentage"`
	ResetsAt       int64 `json:"resets_at"`
}
```

在 `RunnerUsage` struct 加入兩個欄位：

```go
type RunnerUsage struct {
	RunnerHealth
	Block5h    *UsageBlock   `json:"block5h,omitempty"`
	Block7d    *UsageBlock   `json:"block7d,omitempty"`
	Recent7d   *UsageSummary `json:"recent7d,omitempty"`
	RateLimit5h *RateLimit   `json:"rateLimit5h,omitempty"`
	RateLimit7d *RateLimit   `json:"rateLimit7d,omitempty"`
}
```

- [ ] **Step 4: 實作 claude.go**

```go
// internal/doctor/claude.go
package doctor

import (
	"encoding/json"
	"os"
	"time"
)

type claudeUsageFile struct {
	UpdatedAt string     `json:"updated_at"`
	FiveHour  *RateLimit `json:"five_hour"`
	SevenDay  *RateLimit `json:"seven_day"`
}

type claudeRateLimits struct {
	FiveHour *RateLimit
	SevenDay *RateLimit
}

// readClaudeRateLimits 讀取 ~/.4x/usage/claude.json，有效期 10 分鐘。
// 檔案不存在或過期時回傳空值（不視為 error）。
func readClaudeRateLimits(path string) (claudeRateLimits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return claudeRateLimits{}, nil
		}
		return claudeRateLimits{}, err
	}

	var f claudeUsageFile
	if err := json.Unmarshal(data, &f); err != nil {
		return claudeRateLimits{}, nil
	}

	t, err := time.Parse(time.RFC3339, f.UpdatedAt)
	if err != nil || time.Since(t) > 10*time.Minute {
		return claudeRateLimits{}, nil
	}

	return claudeRateLimits{FiveHour: f.FiveHour, SevenDay: f.SevenDay}, nil
}

// claudeUsagePath 回傳 ~/.4x/usage/claude.json 的路徑
func claudeUsagePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.4x/usage/claude.json"
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/doctor/ -run TestReadClaude -v`
Expected: 3 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/doctor/types.go internal/doctor/claude.go internal/doctor/claude_test.go
git commit -m "feat(F041): add RateLimit type and claude.json reader"
```

---

### Task 2: `4x _statusline` 隱藏子指令

**Files:**
- Create: `cmd/4x/statusline.go`
- Modify: `cmd/4x/main.go`

- [ ] **Step 1: 實作 statusline.go**

```go
// cmd/4x/statusline.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newStatuslineCmd() *cobra.Command {
	var chain string

	cmd := &cobra.Command{
		Use:    "_statusline",
		Hidden: true,
		Short:  "StatusLine chain wrapper for Claude Code",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, _ := io.ReadAll(os.Stdin)

			saveRateLimits(input)

			if chain == "" {
				return nil
			}

			parts := strings.Fields(chain)
			if len(parts) == 0 {
				return nil
			}
			c := exec.Command(parts[0], parts[1:]...)
			c.Stdin = strings.NewReader(string(input))
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	cmd.Flags().StringVar(&chain, "chain", "", "original statusLine command to chain")
	return cmd
}

func saveRateLimits(input []byte) {
	var raw struct {
		RateLimits *struct {
			FiveHour *struct {
				UsedPercentage float64 `json:"used_percentage"`
				ResetsAt       int64   `json:"resets_at"`
			} `json:"five_hour"`
			SevenDay *struct {
				UsedPercentage float64 `json:"used_percentage"`
				ResetsAt       int64   `json:"resets_at"`
			} `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(input, &raw); err != nil || raw.RateLimits == nil {
		return
	}

	out := map[string]any{
		"updated_at": time.Now().Format(time.RFC3339),
	}
	if raw.RateLimits.FiveHour != nil {
		out["five_hour"] = map[string]any{
			"used_percentage": int(raw.RateLimits.FiveHour.UsedPercentage),
			"resets_at":       raw.RateLimits.FiveHour.ResetsAt,
		}
	}
	if raw.RateLimits.SevenDay != nil {
		out["seven_day"] = map[string]any{
			"used_percentage": int(raw.RateLimits.SevenDay.UsedPercentage),
			"resets_at":       raw.RateLimits.SevenDay.ResetsAt,
		}
	}

	data, err := json.Marshal(out)
	if err != nil {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".4x", "usage")
	os.MkdirAll(dir, 0755)

	tmp := filepath.Join(dir, ".claude.json.tmp")
	target := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, target)
}
```

- [ ] **Step 2: 註冊到 main.go**

在 `cmd/4x/main.go` 的 `root.AddCommand(...)` 裡加入 `newStatuslineCmd()`。

- [ ] **Step 3: 測試 build 和手動驗證**

Run: `go build ./cmd/4x && echo '{"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":1781370818},"seven_day":{"used_percentage":7,"resets_at":1781763687}}}' | ./bin/4x _statusline`
Expected: 無 stdout，`~/.4x/usage/claude.json` 被建立

Run: `cat ~/.4x/usage/claude.json | python3 -m json.tool`
Expected: 包含 `five_hour.used_percentage: 12`

- [ ] **Step 4: 測試 chain 功能**

Run: `echo '{"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":1781370818}}}' | ./bin/4x _statusline --chain "cat"`
Expected: stdin JSON 被原樣輸出到 stdout

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/statusline.go cmd/4x/main.go
git commit -m "feat(F041): add _statusline hidden subcommand with chain wrapper"
```

---

### Task 3: Hook 安裝/移除

**Files:**
- Create: `internal/doctor/hook.go`
- Create: `internal/doctor/hook_test.go`
- Modify: `cmd/4x/doctor.go`

- [ ] **Step 1: 寫 hook 安裝/移除測試**

```go
// internal/doctor/hook_test.go
package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHook_NoExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{}`), 0644)

	if err := InstallHook(path); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	sl, ok := cfg["statusLine"].(map[string]any)
	if !ok {
		t.Fatal("statusLine not found")
	}
	cmd, _ := sl["command"].(string)
	if cmd != "4x _statusline" {
		t.Errorf("command = %q, want %q", cmd, "4x _statusline")
	}
}

func TestInstallHook_WithExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"node ~/.claude/statusline.js","padding":0}}`), 0644)

	if err := InstallHook(path); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	sl := cfg["statusLine"].(map[string]any)
	cmd := sl["command"].(string)
	expected := `4x _statusline --chain "node ~/.claude/statusline.js"`
	if cmd != expected {
		t.Errorf("command = %q, want %q", cmd, expected)
	}
}

func TestUninstallHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"4x _statusline --chain \"node ~/.claude/statusline.js\""}}`), 0644)

	if err := UninstallHook(path); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	sl := cfg["statusLine"].(map[string]any)
	cmd := sl["command"].(string)
	if cmd != "node ~/.claude/statusline.js" {
		t.Errorf("command = %q, want %q", cmd, "node ~/.claude/statusline.js")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/doctor/ -run TestInstallHook -v`

- [ ] **Step 3: 實作 hook.go**

```go
// internal/doctor/hook.go
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const hookPrefix = "4x _statusline"

// InstallHook 在 Claude Code settings.json 安裝 statusLine chain wrapper
func InstallHook(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}

	newCmd := hookPrefix
	if sl, ok := cfg["statusLine"].(map[string]any); ok {
		if existing, ok := sl["command"].(string); ok && existing != "" && !strings.HasPrefix(existing, hookPrefix) {
			newCmd = fmt.Sprintf(`%s --chain "%s"`, hookPrefix, existing)
		}
	}

	if cfg["statusLine"] == nil {
		cfg["statusLine"] = map[string]any{}
	}
	sl := cfg["statusLine"].(map[string]any)
	sl["type"] = "command"
	sl["command"] = newCmd

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

// UninstallHook 從 Claude Code settings.json 移除 statusLine chain wrapper，還原原始 command
func UninstallHook(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}

	sl, ok := cfg["statusLine"].(map[string]any)
	if !ok {
		return nil
	}

	cmd, _ := sl["command"].(string)
	if !strings.HasPrefix(cmd, hookPrefix) {
		return nil
	}

	original := extractChainTarget(cmd)
	if original == "" {
		delete(cfg, "statusLine")
	} else {
		sl["command"] = original
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

func extractChainTarget(cmd string) string {
	idx := strings.Index(cmd, `--chain "`)
	if idx < 0 {
		return ""
	}
	rest := cmd[idx+len(`--chain "`):]
	end := strings.LastIndex(rest, `"`)
	if end < 0 {
		return rest
	}
	return rest[:end]
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/doctor/ -run "TestInstallHook|TestUninstallHook" -v`
Expected: 3 PASS

- [ ] **Step 5: 加 --install-hook / --uninstall-hook flags 到 doctor.go**

在 `cmd/4x/doctor.go` 的 `newDoctorCmd` 加入：

```go
var installHook, uninstallHook bool

// 在 RunE 開頭加入：
if installHook {
    home, _ := os.UserHomeDir()
    path := filepath.Join(home, ".claude", "settings.json")
    if err := doctor.InstallHook(path); err != nil {
        return fmt.Errorf("install hook: %w", err)
    }
    fmt.Println("StatusLine hook installed.")
    return nil
}
if uninstallHook {
    home, _ := os.UserHomeDir()
    path := filepath.Join(home, ".claude", "settings.json")
    if err := doctor.UninstallHook(path); err != nil {
        return fmt.Errorf("uninstall hook: %w", err)
    }
    fmt.Println("StatusLine hook uninstalled.")
    return nil
}

// 加 flags：
cmd.Flags().BoolVar(&installHook, "install-hook", false, "install Claude Code statusLine hook")
cmd.Flags().BoolVar(&uninstallHook, "uninstall-hook", false, "uninstall Claude Code statusLine hook")
```

- [ ] **Step 6: Build 並手動測試**

Run: `go build ./cmd/4x && ./bin/4x doctor --install-hook`
Run: `cat ~/.claude/settings.json | python3 -c "import sys,json; print(json.load(sys.stdin)['statusLine']['command'])"`
Expected: `4x _statusline --chain "node ~/.claude/statusline.js"`

Run: `./bin/4x doctor --uninstall-hook`
Expected: 還原為 `node ~/.claude/statusline.js`

- [ ] **Step 7: Commit**

```bash
git add internal/doctor/hook.go internal/doctor/hook_test.go cmd/4x/doctor.go
git commit -m "feat(F041): add --install-hook/--uninstall-hook for statusLine chain"
```

---

### Task 4: Codex sqlite rate limit reader

**Files:**
- Create: `internal/doctor/codex.go`
- Create: `internal/doctor/codex_test.go`
- Modify: `go.mod` (加 sqlite driver)

- [ ] **Step 1: 加入 sqlite driver 依賴**

Run: `go get modernc.org/sqlite`

若 binary size 是問題，改用 `github.com/mattn/go-sqlite3`（需 CGO）或直接用 `os/exec` 呼叫 `sqlite3` CLI。考量到 4x 目前是 pure Go（不用 CGO），用 `os/exec` 呼叫系統 `sqlite3` 最安全：

```bash
# 不需要加依賴，用 exec 呼叫 sqlite3
```

- [ ] **Step 2: 寫 Codex reader 測試**

```go
// internal/doctor/codex_test.go
package doctor

import (
	"testing"
)

func TestParseCodexRateLimits(t *testing.T) {
	raw := `websocket event: {"type":"codex.rate_limits","plan_type":"plus","rate_limits":{"allowed":true,"limit_reached":false,"primary":{"used_percent":1,"window_minutes":300,"reset_after_seconds":18000,"reset_at":1781370818},"secondary":{"used_percent":49,"window_minutes":10080,"reset_after_seconds":410869,"reset_at":1781763687}}}`

	rl, err := parseCodexRateLimits(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.FiveHour == nil || rl.FiveHour.UsedPercentage != 1 {
		t.Errorf("primary used_percent = %v, want 1", rl.FiveHour)
	}
	if rl.SevenDay == nil || rl.SevenDay.UsedPercentage != 49 {
		t.Errorf("secondary used_percent = %v, want 49", rl.SevenDay)
	}
	if rl.SevenDay.ResetsAt != 1781763687 {
		t.Errorf("secondary reset_at = %d, want 1781763687", rl.SevenDay.ResetsAt)
	}
}

func TestParseCodexRateLimits_NoMatch(t *testing.T) {
	rl, err := parseCodexRateLimits("some random log line")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.FiveHour != nil || rl.SevenDay != nil {
		t.Error("expected nil for non-matching log")
	}
}
```

- [ ] **Step 3: 跑測試確認失敗**

Run: `go test ./internal/doctor/ -run TestParseCodex -v`

- [ ] **Step 4: 實作 codex.go**

```go
// internal/doctor/codex.go
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"
)

var codexEventRe = regexp.MustCompile(`\{[^{}]*"type"\s*:\s*"codex\.rate_limits"[^{}]*\{[^}]*\}[^}]*\}[^}]*\}`)

type codexRateLimitEvent struct {
	RateLimits struct {
		Primary *struct {
			UsedPercent int   `json:"used_percent"`
			ResetAt     int64 `json:"reset_at"`
		} `json:"primary"`
		Secondary *struct {
			UsedPercent int   `json:"used_percent"`
			ResetAt     int64 `json:"reset_at"`
		} `json:"secondary"`
	} `json:"rate_limits"`
}

// parseCodexRateLimits 從 sqlite log 行中提取 rate limit
func parseCodexRateLimits(logLine string) (claudeRateLimits, error) {
	idx := findJSONStart(logLine, "codex.rate_limits")
	if idx < 0 {
		return claudeRateLimits{}, nil
	}

	jsonStr := logLine[idx:]
	var evt codexRateLimitEvent
	if err := json.Unmarshal([]byte(jsonStr), &evt); err != nil {
		return claudeRateLimits{}, nil
	}

	var result claudeRateLimits
	if evt.RateLimits.Primary != nil {
		result.FiveHour = &RateLimit{
			UsedPercentage: evt.RateLimits.Primary.UsedPercent,
			ResetsAt:       evt.RateLimits.Primary.ResetAt,
		}
	}
	if evt.RateLimits.Secondary != nil {
		result.SevenDay = &RateLimit{
			UsedPercentage: evt.RateLimits.Secondary.UsedPercent,
			ResetsAt:       evt.RateLimits.Secondary.ResetAt,
		}
	}
	return result, nil
}

func findJSONStart(s, marker string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			rest := s[i:]
			if len(rest) > len(marker) && containsMarker(rest, marker) {
				return i
			}
		}
	}
	return -1
}

func containsMarker(s, marker string) bool {
	for i := 0; i <= len(s)-len(marker); i++ {
		if s[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}

// ReadCodexRateLimits 讀取 ~/.codex/logs_2.sqlite 取最新的 rate limit event
func ReadCodexRateLimits() (claudeRateLimits, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeRateLimits{}, nil
	}
	dbPath := home + "/.codex/logs_2.sqlite"
	if _, err := os.Stat(dbPath); err != nil {
		return claudeRateLimits{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT feedback_log_body FROM logs WHERE feedback_log_body LIKE '%codex.rate_limits%' ORDER BY ts DESC LIMIT 1`
	out, err := exec.CommandContext(ctx, "sqlite3", dbPath, query).Output()
	if err != nil {
		return claudeRateLimits{}, fmt.Errorf("sqlite3 query: %w", err)
	}

	if len(out) == 0 {
		return claudeRateLimits{}, nil
	}

	return parseCodexRateLimits(string(out))
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/doctor/ -run TestParseCodex -v`
Expected: 2 PASS

- [ ] **Step 6: 手動測試實際 sqlite 讀取**

Run: `go build ./cmd/4x && go test ./internal/doctor/ -run TestParseCodex -v`

- [ ] **Step 7: Commit**

```bash
git add internal/doctor/codex.go internal/doctor/codex_test.go
git commit -m "feat(F041): add Codex sqlite rate limit reader"
```

---

### Task 5: 整合 GenerateReport — 合併所有來源

**Files:**
- Modify: `internal/doctor/doctor.go`

- [ ] **Step 1: 修改 GenerateReport 加入 rate limit 讀取**

在 `wg.Wait()` 之後、`report.Runners = results` 之前，加入 Claude 和 Codex 的 rate limit 合併：

```go
// 合併 rate limit 資料
for i := range results {
	switch results[i].Name {
	case "claude":
		rl, _ := readClaudeRateLimits(claudeUsagePath())
		results[i].RateLimit5h = rl.FiveHour
		results[i].RateLimit7d = rl.SevenDay
	case "codex":
		rl, _ := ReadCodexRateLimits()
		results[i].RateLimit5h = rl.FiveHour
		results[i].RateLimit7d = rl.SevenDay
	}
}
```

- [ ] **Step 2: Build 和測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./internal/doctor/...`
Expected: 全部通過

- [ ] **Step 3: Commit**

```bash
git add internal/doctor/doctor.go
git commit -m "feat(F041): merge rate limits into GenerateReport"
```

---

### Task 6: CLI 輸出整合百分比

**Files:**
- Modify: `cmd/4x/doctor.go`

- [ ] **Step 1: 修改 printRunnerCard 使用 RateLimit**

更新 `printRunnerCard` 和 `printBlock`，當 RateLimit 存在時優先使用其百分比：

```go
func printRunnerCard(r doctor.RunnerUsage, ccusageAvail bool) {
	status := "\033[31m✗\033[0m"
	ver := ""
	if r.Installed {
		status = "\033[32m✓\033[0m"
		if r.Version != "" {
			ver = " \033[90m" + r.Version + "\033[0m"
		}
	}
	fmt.Printf("── %s %s%s ──\n", r.Name, status, ver)

	// 5h: 優先用 rate limit 百分比，搭配 block 的 cost/tokens
	if r.RateLimit5h != nil {
		printRateBlock("5h", r.RateLimit5h, r.Block5h)
	} else if r.Block5h != nil {
		printBlock("5h", r.Block5h, 300)
	}

	// 7d: 同上
	if r.RateLimit7d != nil {
		printRateBlock("7d", r.RateLimit7d, r.Block7d)
	} else if r.Block7d != nil {
		printBlock("7d", r.Block7d, 168*60)
	}

	if r.Recent7d != nil {
		fmt.Printf("  7d   %s tokens, $%.2f (%d days)\n",
			formatTokens(r.Recent7d.TotalTokens), r.Recent7d.TotalCost, r.Recent7d.Days)
	}

	if r.RateLimit5h == nil && r.RateLimit7d == nil && r.Block5h == nil && r.Block7d == nil && r.Recent7d == nil && ccusageAvail {
		fmt.Printf("  No usage data\n")
	}

	fmt.Println()
}

func printRateBlock(label string, rl *doctor.RateLimit, block *doctor.UsageBlock) {
	pct := float64(rl.UsedPercentage)
	bar := renderBar(pct, 20)

	resetStr := ""
	if rl.ResetsAt > 0 {
		remaining := time.Until(time.Unix(rl.ResetsAt, 0))
		if remaining < 0 {
			remaining = 0
		}
		if label == "5h" {
			resetStr = fmt.Sprintf("(%s left, resets %s)", fmtDuration(remaining), time.Unix(rl.ResetsAt, 0).Local().Format("15:04"))
		} else {
			resetStr = fmt.Sprintf("(%s left, resets %s)", fmtDuration(remaining), time.Unix(rl.ResetsAt, 0).Local().Format("Jan 2"))
		}
	}

	fmt.Printf("  %s   %s %d%% used %s\n", label, bar, rl.UsedPercentage, resetStr)

	if block != nil {
		fmt.Printf("       $%.2f, %s tok", block.CostUSD, formatTokens(block.TotalTokens))
		if block.BurnRate.CostPerHour > 0 {
			fmt.Printf(", $%.0f/hr burn", block.BurnRate.CostPerHour)
		}
		fmt.Println()
	}
}
```

- [ ] **Step 2: Build 和測試**

Run: `go build -o bin/4x ./cmd/4x && ./bin/4x doctor`
Expected: Claude 顯示 `12% used`（若 hook 已安裝且有資料），Codex 顯示 `1% used` / `49% used`

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/doctor.go
git commit -m "feat(F041): CLI shows rate limit percentages"
```

---

### Task 7: Dashboard UI 更新

**Files:**
- Modify: `internal/server/static/index.html`

**注意：** 此 task 須等 HTML 重構 worktree merge 完才能做。

- [ ] **Step 1: 修改 renderBlockCard 支援 rateLimit 百分比**

更新 `renderBlockCard` 函式：當 `rateLimit5h` 或 `rateLimit7d` 存在時，用 `usedPercentage` 取代時間計算的百分比。

```javascript
function renderBlockCard(label, b, rl, totalMin, showDetails) {
  // 百分比：優先用 rate limit，fallback 到時間計算
  let pct, resetStr, remainStr;
  if (rl) {
    pct = rl.used_percentage;
    if (rl.resets_at > 0) {
      const resetDate = new Date(rl.resets_at * 1000);
      const remainMs = Math.max(0, resetDate.getTime() - Date.now());
      const remainMin = Math.floor(remainMs / 60000);
      remainStr = remainMin >= 1440
        ? Math.floor(remainMin/1440)+'d '+Math.floor((remainMin%1440)/60)+'h'
        : remainMin >= 60 ? Math.floor(remainMin/60)+'h'+String(remainMin%60).padStart(2,'0')+'m' : remainMin+'m';
      resetStr = label === '5h'
        ? resetDate.toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})
        : remainStr;
    }
  } else if (b) {
    // 既有的時間計算邏輯...
  }
  // ...渲染進度條和詳細資訊
}
```

- [ ] **Step 2: 修改 renderDoctor 傳入 rateLimit**

在 runner card 渲染中，傳入 `r.rateLimit5h` 和 `r.rateLimit7d`：

```javascript
if (r.rateLimit5h || r.block5h) {
  body += renderBlockCard('5h', r.block5h, r.rateLimit5h, 300, true);
}
if (r.rateLimit7d || r.block7d) {
  body += `<div ...>${renderBlockCard('7d', r.block7d, r.rateLimit7d, 10080, false)}</div>`;
}
```

- [ ] **Step 3: 修復 doctor-panel 殘留 bug**

在 `loadDetail` 開頭加入：

```javascript
document.getElementById('doctor-panel').classList.add('hidden');
```

- [ ] **Step 4: Build 和測試**

Run: `go build -o bin/4x ./cmd/4x`
Verify: 瀏覽器開 dashboard → Cmd+Shift+D → Claude 卡片顯示 `XX% used`

- [ ] **Step 5: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F041): dashboard shows rate limit percentages, fix doctor-panel visibility"
```

---

### Task 8: 文件更新

**Files:**
- Modify: `docs/guide/cli.md`
- Modify: `docs/guide/dashboard.md`

- [ ] **Step 1: 更新 cli.md**

在 `4x doctor` 段落加入 `--install-hook` 和 `--uninstall-hook` flags：

```markdown
| `--install-hook` | Install Claude Code statusLine hook for rate limit tracking |
| `--uninstall-hook` | Remove Claude Code statusLine hook |
```

- [ ] **Step 2: 更新 dashboard.md**

更新 Doctor Page 段落，說明 rate limit 百分比的來源和 hook 安裝流程。

- [ ] **Step 3: Commit**

```bash
git add docs/guide/cli.md docs/guide/dashboard.md
git commit -m "docs(F041): update cli and dashboard docs for rate limit hook"
```

---

### Task 9: 端到端驗證

- [ ] **Step 1: 完整 build + test**

Run: `go build -o bin/4x ./cmd/4x && go vet ./... && go test ./...`

- [ ] **Step 2: 安裝 hook 並驗證**

```bash
./bin/4x doctor --install-hook
# 確認 ~/.claude/settings.json 的 statusLine.command 是 chain wrapper
# 開一個新的 Claude Code session，等 statusLine 刷新
cat ~/.4x/usage/claude.json
# 確認有 five_hour/seven_day 百分比
```

- [ ] **Step 3: 執行 doctor 確認完整輸出**

```bash
./bin/4x doctor
# 確認 Claude 有 XX% used
# 確認 Codex 有 XX% used
# 確認 Gemini/其他有 daily summary 或 No usage data
```

- [ ] **Step 4: Dashboard 驗證**

```bash
./bin/4x live -p 4567
# 瀏覽器開 localhost:4567 → Cmd+Shift+D
# 確認 per-runner 卡片正確顯示
# 確認從 Doctor 切到 feature detail 時 doctor-panel 消失
```

- [ ] **Step 5: 還原 hook 驗證**

```bash
./bin/4x doctor --uninstall-hook
# 確認 ~/.claude/settings.json 還原為原始 statusLine command
```
