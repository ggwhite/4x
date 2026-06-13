# F038: 4x doctor — Runner Health Check & LLM Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `4x doctor` subcommand 和 dashboard Doctor 頁面，一次檢視所有 runner 安裝狀態與 LLM 使用量

**Architecture:** 新增 `internal/doctor/` package 負責 runner 偵測（`command -v` + `--version`）和 ccusage shell out。CLI 層 `cmd/4x/doctor.go` 呼叫此 package 組裝報告。Server 層加 `GET /api/doctor` endpoint。Dashboard 前端在 sidebar 底部加 Doctor 連結，點擊後切換到 Doctor 頁面。

**Tech Stack:** Go 1.22+, Cobra CLI, `os/exec`, `text/tabwriter`, `encoding/json`, Tailwind CSS (CDN)

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/doctor/types.go` | `RunnerHealth`, `UsageModelBreakdown`, `UsageDailyEntry`, `DoctorReport` 型別定義 |
| Create | `internal/doctor/detect.go` | `DetectRunners()` — 偵測每個 runner 的安裝與版本 |
| Create | `internal/doctor/detect_test.go` | detect 單元測試 |
| Create | `internal/doctor/usage.go` | `FetchUsage()` — shell out ccusage 取用量 |
| Create | `internal/doctor/usage_test.go` | usage 單元測試 |
| Create | `internal/doctor/doctor.go` | `GenerateReport()` — 組合兩階段成完整報告 |
| Create | `cmd/4x/doctor.go` | Cobra subcommand — 人類友善輸出 + `--json` |
| Modify | `cmd/4x/main.go:23-39` | 註冊 `newDoctorCmd()` |
| Modify | `internal/server/server.go:30-114` | 新增 `GET /api/doctor` handler |
| Modify | `internal/server/multi.go:220+` | 多專案模式向後相容路由 |
| Modify | `internal/server/static/index.html` | Doctor 頁面 UI — sidebar 連結 + 獨立頁面渲染 |
| Modify | `docs/guide/cli.md` | 新增 `4x doctor` 文件 |
| Modify | `docs/guide/dashboard.md` | 新增 Doctor 頁面與 `/api/doctor` endpoint 文件 |

---

### Task 1: 型別定義 — `internal/doctor/types.go`

**Files:**
- Create: `internal/doctor/types.go`

- [ ] **Step 1: 建立 types.go**

```go
package doctor

// RunnerHealth 是單一 runner 的健康狀態
type RunnerHealth struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

// UsageModelBreakdown 是單一 model 的用量明細
type UsageModelBreakdown struct {
	ModelName           string  `json:"modelName"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	Cost                float64 `json:"cost"`
}

// UsageDailyEntry 是 ccusage daily 的單日資料
type UsageDailyEntry struct {
	Period              string                `json:"period"`
	Agent               string                `json:"agent"`
	InputTokens         int64                 `json:"inputTokens"`
	OutputTokens        int64                 `json:"outputTokens"`
	CacheReadTokens     int64                 `json:"cacheReadTokens"`
	CacheCreationTokens int64                 `json:"cacheCreationTokens"`
	TotalTokens         int64                 `json:"totalTokens"`
	TotalCost           float64               `json:"totalCost"`
	ModelsUsed          []string              `json:"modelsUsed"`
	Metadata            map[string]any        `json:"metadata"`
	ModelBreakdowns     []UsageModelBreakdown `json:"modelBreakdowns"`
}

// DoctorReport 是 4x doctor 的完整報告
type DoctorReport struct {
	Runners          []RunnerHealth  `json:"runners"`
	Usage            []UsageDailyEntry `json:"usage"`
	CcusageAvailable bool              `json:"ccusageAvailable"`
	CcusageHint      string            `json:"ccusageHint,omitempty"`
}
```

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/doctor/`
Expected: PASS（no output）

- [ ] **Step 3: Commit**

```bash
git add internal/doctor/types.go
git commit -m "feat(doctor): add type definitions for runner health and usage report"
```

---

### Task 2: Runner 偵測 — `internal/doctor/detect.go`

**Files:**
- Create: `internal/doctor/detect.go`
- Create: `internal/doctor/detect_test.go`

- [ ] **Step 1: 寫 detect 的測試**

```go
package doctor

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"claude", "2.1.175 (Claude Code)", "2.1.175"},
		{"codex", "codex-cli 0.139.0", "0.139.0"},
		{"gemini", "0.46.0", "0.46.0"},
		{"agy", "1.0.7", "1.0.7"},
		{"copilot", "GitHub Copilot CLI 1.0.61.\nRun 'copilot update' ...", "1.0.61"},
		{"no version", "some random text", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVersion(tt.output)
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestDetectRunners_NoRunners(t *testing.T) {
	runners := map[string]string{}
	result := DetectRunners(runners)
	if len(result) != 0 {
		t.Errorf("expected 0 runners, got %d", len(result))
	}
}
```

- [ ] **Step 2: 跑測試，確認失敗**

Run: `go test ./internal/doctor/ -run TestParseVersion -v`
Expected: FAIL — `parseVersion` 未定義

- [ ] **Step 3: 實作 detect.go**

```go
package doctor

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

var versionRe = regexp.MustCompile(`(\d+\.\d+[\.\d]*)`)

// parseVersion 從 --version 輸出中擷取第一個 semver-like 版本號
func parseVersion(output string) string {
	if len(output) > 200 {
		output = output[:200]
	}
	m := versionRe.FindString(output)
	return m
}

// DetectRunners 偵測每個 runner 的安裝狀態與版本。
// runners 是 name -> command 的對應（從 settings.json 的 RunnerConfig.Command 取得）。
func DetectRunners(runners map[string]string) []RunnerHealth {
	results := make([]RunnerHealth, 0, len(runners))
	for name, command := range runners {
		rh := RunnerHealth{Name: name, Command: command}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "sh", "-c", "command -v "+command).Output()
		cancel()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			results = append(results, rh)
			continue
		}
		rh.Installed = true

		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		verOut, err := exec.CommandContext(ctx2, command, "--version").CombinedOutput()
		cancel2()
		if err == nil {
			rh.Version = parseVersion(string(verOut))
		}

		results = append(results, rh)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}
```

- [ ] **Step 4: 跑測試，確認通過**

Run: `go test ./internal/doctor/ -run TestParseVersion -v`
Expected: PASS

Run: `go test ./internal/doctor/ -run TestDetectRunners -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/detect.go internal/doctor/detect_test.go
git commit -m "feat(doctor): implement runner detection with version parsing"
```

---

### Task 3: Usage 取得 — `internal/doctor/usage.go`

**Files:**
- Create: `internal/doctor/usage.go`
- Create: `internal/doctor/usage_test.go`

- [ ] **Step 1: 寫 usage 的測試**

```go
package doctor

import "testing"

func TestParseCcusageOutput(t *testing.T) {
	validJSON := `{
		"daily": [
			{
				"agent": "all",
				"period": "2026-06-12",
				"inputTokens": 221810,
				"outputTokens": 8137,
				"cacheReadTokens": 426739,
				"cacheCreationTokens": 0,
				"totalTokens": 663361,
				"totalCost": 0.176,
				"modelsUsed": ["claude-opus-4-6"],
				"metadata": {"agents": ["claude"]},
				"modelBreakdowns": [
					{
						"modelName": "claude-opus-4-6",
						"inputTokens": 221810,
						"outputTokens": 8137,
						"cacheReadTokens": 426739,
						"cacheCreationTokens": 0,
						"cost": 0.176
					}
				]
			}
		]
	}`

	entries, err := parseCcusageOutput([]byte(validJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Period != "2026-06-12" {
		t.Errorf("period = %q, want %q", entries[0].Period, "2026-06-12")
	}
	if entries[0].TotalTokens != 663361 {
		t.Errorf("totalTokens = %d, want %d", entries[0].TotalTokens, 663361)
	}
	if entries[0].TotalCost != 0.176 {
		t.Errorf("totalCost = %f, want %f", entries[0].TotalCost, 0.176)
	}
	if len(entries[0].ModelBreakdowns) != 1 {
		t.Fatalf("expected 1 model breakdown, got %d", len(entries[0].ModelBreakdowns))
	}
	if entries[0].ModelBreakdowns[0].ModelName != "claude-opus-4-6" {
		t.Errorf("modelName = %q, want %q", entries[0].ModelBreakdowns[0].ModelName, "claude-opus-4-6")
	}
}

func TestParseCcusageOutput_Invalid(t *testing.T) {
	_, err := parseCcusageOutput([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseCcusageOutput_Empty(t *testing.T) {
	entries, err := parseCcusageOutput([]byte(`{"daily": []}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
```

- [ ] **Step 2: 跑測試，確認失敗**

Run: `go test ./internal/doctor/ -run TestParseCcusageOutput -v`
Expected: FAIL — `parseCcusageOutput` 未定義

- [ ] **Step 3: 實作 usage.go**

```go
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type ccusageResponse struct {
	Daily []UsageDailyEntry `json:"daily"`
}

// parseCcusageOutput 解析 ccusage daily --json 的 JSON 輸出
func parseCcusageOutput(data []byte) ([]UsageDailyEntry, error) {
	var resp ccusageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse ccusage output: %w", err)
	}
	return resp.Daily, nil
}

// FetchUsage 呼叫 ccusage daily --json 取得用量資料。
// 回傳 (entries, available, error)：
//   - available=false 表示 ccusage 未安裝
//   - error 只在 ccusage 可用但執行或解析失敗時回傳
func FetchUsage() ([]UsageDailyEntry, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := exec.LookPath("ccusage"); err != nil {
		npxPath, npxErr := exec.LookPath("npx")
		if npxErr != nil {
			return nil, false, nil
		}
		out, err := exec.CommandContext(ctx, npxPath, "ccusage", "daily", "--json").CombinedOutput()
		if err != nil {
			return nil, false, nil
		}
		entries, parseErr := parseCcusageOutput(out)
		if parseErr != nil {
			return nil, true, parseErr
		}
		return entries, true, nil
	}

	out, err := exec.CommandContext(ctx, "ccusage", "daily", "--json").CombinedOutput()
	if err != nil {
		return nil, true, fmt.Errorf("ccusage failed: %w", err)
	}
	entries, parseErr := parseCcusageOutput(out)
	if parseErr != nil {
		return nil, true, parseErr
	}
	return entries, true, nil
}
```

- [ ] **Step 4: 跑測試，確認通過**

Run: `go test ./internal/doctor/ -run TestParseCcusageOutput -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/usage.go internal/doctor/usage_test.go
git commit -m "feat(doctor): implement ccusage usage fetcher with JSON parsing"
```

---

### Task 4: Report 組裝 — `internal/doctor/doctor.go`

**Files:**
- Create: `internal/doctor/doctor.go`

- [ ] **Step 1: 實作 doctor.go**

```go
package doctor

import "github.com/ggwhite/4x/internal/protocol"

const ccusageInstallHint = "npm i -g ccusage"

// GenerateReport 產生完整的 doctor 報告。
// ws 可為 nil（尚未 init 時），此時 runners 為空。
func GenerateReport(ws *protocol.Workspace) DoctorReport {
	report := DoctorReport{}

	if ws != nil {
		cfg, err := ws.ReadConfig()
		if err == nil {
			runners := make(map[string]string, len(cfg.Runners))
			for name, rc := range cfg.Runners {
				runners[name] = rc.Command
			}
			report.Runners = DetectRunners(runners)
		}
	}

	entries, available, _ := FetchUsage()
	report.CcusageAvailable = available
	if available {
		report.Usage = entries
	} else {
		report.CcusageHint = ccusageInstallHint
	}

	return report
}
```

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/doctor/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/doctor/doctor.go
git commit -m "feat(doctor): add GenerateReport to assemble full doctor report"
```

---

### Task 5: CLI subcommand — `cmd/4x/doctor.go`

**Files:**
- Create: `cmd/4x/doctor.go`
- Modify: `cmd/4x/main.go:23-39`

- [ ] **Step 1: 實作 doctor.go CLI**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ggwhite/4x/internal/doctor"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check runner installation and LLM usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, _ := protocol.Find(cwd)

			report := doctor.GenerateReport(ws)

			if jsonOutput {
				data, _ := json.MarshalIndent(report, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			printRunners(report)
			printUsage(report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func printRunners(report doctor.DoctorReport) {
	installed := 0
	for _, r := range report.Runners {
		if r.Installed {
			installed++
		}
	}
	fmt.Printf("── Runners (%d/%d installed) ──\n", installed, len(report.Runners))

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  RUNNER\tCOMMAND\tSTATUS\tVERSION\n")
	fmt.Fprintf(w, "  ──────\t───────\t──────\t───────\n")
	for _, r := range report.Runners {
		status := "✗ not found"
		version := "-"
		if r.Installed {
			status = "✓ installed"
			if r.Version != "" {
				version = r.Version
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", r.Name, r.Command, status, version)
	}
	w.Flush()
	fmt.Println()
}

func printUsage(report doctor.DoctorReport) {
	if !report.CcusageAvailable {
		fmt.Printf("── Usage ──\n")
		fmt.Printf("  ccusage not found. Install with: %s\n\n", report.CcusageHint)
		return
	}

	if len(report.Usage) == 0 {
		fmt.Printf("── Usage (via ccusage) ──\n")
		fmt.Printf("  No usage data found.\n\n")
		return
	}

	fmt.Printf("── Usage (via ccusage) ──\n")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  DATE\tAGENTS\tTOKENS\tCOST\n")
	fmt.Fprintf(w, "  ────\t──────\t──────\t────\n")

	var totalTokens int64
	var totalCost float64
	for _, e := range report.Usage {
		agents := "-"
		if md, ok := e.Metadata["agents"]; ok {
			if arr, ok := md.([]any); ok {
				names := make([]string, 0, len(arr))
				for _, a := range arr {
					if s, ok := a.(string); ok {
						names = append(names, s)
					}
				}
				agents = strings.Join(names, ",")
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t$%.2f\n", e.Period, agents, formatTokens(e.TotalTokens), e.TotalCost)
		totalTokens += e.TotalTokens
		totalCost += e.TotalCost
	}
	fmt.Fprintf(w, "  \t\t─────\t─────\n")
	fmt.Fprintf(w, "  Total\t\t%s\t$%.2f\n", formatTokens(totalTokens), totalCost)
	w.Flush()
	fmt.Println()
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
```

- [ ] **Step 2: 在 main.go 註冊 command**

在 `cmd/4x/main.go` 的 `root.AddCommand(...)` 區塊加一行 `newDoctorCmd()`：

```go
	root.AddCommand(
		newInitCmd(),
		newUpgradeCmd(),
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
		newDoctorCmd(),
	)
```

- [ ] **Step 3: 確認編譯**

Run: `go build ./cmd/4x/`
Expected: PASS

- [ ] **Step 4: 手動測試 CLI**

Run: `./bin/4x doctor`
Expected: 輸出 runner 列表和 usage（或 ccusage 安裝提示）

Run: `./bin/4x doctor --json | jq .`
Expected: 有效 JSON 輸出

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/doctor.go cmd/4x/main.go
git commit -m "feat(doctor): add 4x doctor CLI subcommand"
```

---

### Task 6: Server API — `GET /api/doctor`

**Files:**
- Modify: `internal/server/server.go:30-114`
- Modify: `internal/server/multi.go`

- [ ] **Step 1: 在 server.go 的 NewMux 加 handler**

在 `server.go` 的 `NewMux` function 中，`mux.HandleFunc("/api/done", ...)` 之後加：

```go
	mux.HandleFunc("/api/doctor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleGetDoctor(ws, w)
	})
```

在 server.go 檔案底部加 handler function：

```go
func handleGetDoctor(ws *protocol.Workspace, w http.ResponseWriter) {
	report := doctor.GenerateReport(ws)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}
```

在 import 區塊加 `"github.com/ggwhite/4x/internal/doctor"`。

- [ ] **Step 2: 在 multi.go 加向後相容路由**

在 `multi.go` 的 `NewMultiMux` function 中，其他 `mux.HandleFunc("/api/...")` 向後相容路由之後加：

```go
	mux.HandleFunc("/api/doctor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			handleGetDoctor(ws, w)
			return
		}
		compatError(w, len(entries), "/api/project/{id}/api/doctor")
	})
```

- [ ] **Step 3: 確認編譯**

Run: `go build ./cmd/4x/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/multi.go
git commit -m "feat(doctor): add GET /api/doctor endpoint to dashboard server"
```

---

### Task 7: Dashboard 前端 — Doctor 頁面

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 在 sidebar 底部加 Doctor 連結**

在 `index.html` 的 sidebar `<div id="task-list">` 結束後（`</div>` 之後、`</div><!-- sidebar -->` 之前），加一個固定在底部的 Doctor 按鈕：

找到 sidebar 結尾附近的結構。在 `<div id="task-list">` 的 closing `</div>` 之後加：

```html
<div class="p-2 border-t" style="border-color:var(--border)">
  <button onclick="showDoctor()" class="w-full text-left px-3 py-2 rounded-lg text-xs transition-colors hover:bg-zinc-800/50" style="color:var(--text-3)">
    <span class="mr-2">&#9764;</span> Doctor
  </button>
</div>
```

- [ ] **Step 2: 在 main area 加 doctor-panel div**

在 `<div id="logs-panel">` 結尾 `</div>` 之後加：

```html
<div id="doctor-panel" class="hidden"></div>
```

- [ ] **Step 3: 加 JavaScript — showDoctor 和 renderDoctor**

在 `<script>` 區塊內加：

```javascript
function showDoctor() {
  current = null;
  document.getElementById('header').classList.add('hidden');
  document.getElementById('overview-panel').classList.add('hidden');
  document.getElementById('messages').classList.add('hidden');
  document.getElementById('logs-panel').classList.add('hidden');
  document.getElementById('dashboard').classList.add('hidden');
  const dp = document.getElementById('doctor-panel');
  dp.classList.remove('hidden');
  dp.innerHTML = '<div class="flex items-center justify-center py-20"><span class="text-zinc-500">Loading doctor report...</span></div>';
  fetch(apiBase() + '/api/doctor')
    .then(r => r.json())
    .then(renderDoctor)
    .catch(e => { dp.innerHTML = '<div class="text-red-400 p-4">Failed to load doctor report: ' + esc(e.message) + '</div>'; });
}

function renderDoctor(report) {
  const dp = document.getElementById('doctor-panel');
  const installed = report.runners.filter(r => r.installed).length;
  const total = report.runners.length;

  let runnersHTML = report.runners.map(r => {
    const color = r.installed ? 'emerald' : 'red';
    const icon = r.installed ? '✓' : '✗';
    return `<div class="rounded-xl border p-4 flex items-center gap-4" style="border-color:var(--border);background:var(--bg-card)">
      <div class="w-3 h-3 rounded-full bg-${color}-500"></div>
      <div class="flex-1">
        <div class="font-bold text-sm">${esc(r.name)}</div>
        <div class="text-xs" style="color:var(--text-3)">${esc(r.command)}</div>
      </div>
      <div class="text-xs font-mono" style="color:var(--text-2)">${r.version || '-'}</div>
      <div class="text-xs text-${color}-400">${icon}</div>
    </div>`;
  }).join('');

  let usageHTML = '';
  if (!report.ccusageAvailable) {
    usageHTML = `<div class="rounded-xl border p-6 text-center" style="border-color:var(--border);background:var(--bg-card)">
      <div class="text-sm" style="color:var(--text-3)">ccusage not found</div>
      <div class="text-xs mt-2" style="color:var(--text-4)">Install with: <code class="px-2 py-0.5 rounded" style="background:var(--bg-input)">${esc(report.ccusageHint)}</code></div>
    </div>`;
  } else if (!report.usage || report.usage.length === 0) {
    usageHTML = `<div class="rounded-xl border p-6 text-center" style="border-color:var(--border);background:var(--bg-card)">
      <div class="text-sm" style="color:var(--text-3)">No usage data found</div>
    </div>`;
  } else {
    let totalTokens = 0, totalCost = 0;
    const rows = report.usage.map(e => {
      const agents = e.metadata && e.metadata.agents ? e.metadata.agents.join(', ') : '-';
      totalTokens += e.totalTokens;
      totalCost += e.totalCost;
      return `<tr class="border-t" style="border-color:var(--border)">
        <td class="py-2 px-3 text-xs font-mono">${esc(e.period)}</td>
        <td class="py-2 px-3 text-xs">${esc(agents)}</td>
        <td class="py-2 px-3 text-xs text-right font-mono">${fmtTokens(e.totalTokens)}</td>
        <td class="py-2 px-3 text-xs text-right font-mono">$${e.totalCost.toFixed(2)}</td>
      </tr>`;
    }).join('');
    usageHTML = `<div class="rounded-xl border overflow-hidden" style="border-color:var(--border);background:var(--bg-card)">
      <table class="w-full"><thead><tr class="text-left text-[10px] uppercase tracking-wider" style="color:var(--text-4)">
        <th class="py-2 px-3">Date</th><th class="py-2 px-3">Agents</th>
        <th class="py-2 px-3 text-right">Tokens</th><th class="py-2 px-3 text-right">Cost</th>
      </tr></thead><tbody>${rows}
      <tr class="border-t font-bold" style="border-color:var(--accent)">
        <td class="py-2 px-3 text-xs" colspan="2">Total</td>
        <td class="py-2 px-3 text-xs text-right font-mono">${fmtTokens(totalTokens)}</td>
        <td class="py-2 px-3 text-xs text-right font-mono">$${totalCost.toFixed(2)}</td>
      </tr></tbody></table>
    </div>`;
  }

  dp.innerHTML = `<div class="mb-6 flex items-center gap-3">
      <span class="text-lg font-bold">Doctor</span>
      <span class="ml-auto px-3 py-1 text-xs border rounded-full" style="border-color:var(--border);color:var(--text-3)">${installed}/${total} runners</span>
      <button onclick="showDoctor()" class="text-xs px-3 py-1 rounded border transition-colors hover:bg-zinc-800/50" style="border-color:var(--border);color:var(--text-3)">Refresh</button>
    </div>
    <div class="text-[10px] font-bold uppercase tracking-wider mb-3" style="color:var(--text-4)">Runners</div>
    <div class="grid grid-cols-2 gap-3 mb-8">${runnersHTML}</div>
    <div class="text-[10px] font-bold uppercase tracking-wider mb-3" style="color:var(--text-4)">Usage (via ccusage)</div>
    ${usageHTML}`;
}

function fmtTokens(n) {
  if (n >= 1e9) return (n/1e9).toFixed(1) + 'B';
  if (n >= 1e6) return (n/1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n/1e3).toFixed(1) + 'K';
  return n.toString();
}
```

- [ ] **Step 4: 修改 goHome 函式**

在 `goHome()` 函式中，確保切回首頁時隱藏 doctor-panel。在 `document.getElementById('logs-panel').classList.add('hidden');` 之後加：

```javascript
document.getElementById('doctor-panel').classList.add('hidden');
```

- [ ] **Step 5: 確認編譯**

Run: `go build ./cmd/4x/`
Expected: PASS（index.html 被 embed 時一起包進去）

- [ ] **Step 6: 手動測試 dashboard**

Run: `./bin/4x live -w`
Expected: 
- Sidebar 底部出現 Doctor 按鈕
- 點擊後顯示 runner 卡片和 usage 表格
- 點擊 "4x Live" logo 可回首頁

- [ ] **Step 7: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(doctor): add Doctor page to dashboard with runner cards and usage table"
```

---

### Task 8: 文件更新

**Files:**
- Modify: `docs/guide/cli.md`
- Modify: `docs/guide/dashboard.md`

- [ ] **Step 1: 更新 cli.md**

在 `docs/guide/cli.md` 適當位置（按字母序或功能分組）加入：

```markdown
---

## `4x doctor`

Check runner installation status and LLM usage.

```
4x doctor [--json]
```

- Lists all configured runners with installation status and version
- Shows LLM usage via [ccusage](https://github.com/ryoppippi/ccusage) if available
- Works even without `4x init` (runners list will be empty)

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

**Examples:**

```bash
# Text output
4x doctor

# JSON output for scripting
4x doctor --json | jq '.runners[] | select(.installed == false)'
```
```

- [ ] **Step 2: 更新 dashboard.md**

在 `docs/guide/dashboard.md` 的 REST API 表格加一行：

```markdown
| `/api/doctor` | GET | Runner health check and LLM usage report |
```

在 Features 段落加：

```markdown
### Doctor Page

Click "Doctor" in the sidebar to view runner installation status and LLM usage.
The page shows runner health cards (green/red) and a daily usage table powered by ccusage.
```

- [ ] **Step 3: Commit**

```bash
git add docs/guide/cli.md docs/guide/dashboard.md
git commit -m "docs: add 4x doctor to CLI reference and dashboard guide"
```

---

### Task 9: 全面驗證

**Files:** 無新增（驗證現有變更）

- [ ] **Step 1: 編譯 + 靜態檢查**

Run: `go build ./cmd/4x && go vet ./...`
Expected: PASS，無 warning

- [ ] **Step 2: 跑全部測試**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 3: CLI 端對端測試**

Run: `./bin/4x doctor`
Expected: 正確列出 runner 和 usage

Run: `./bin/4x doctor --json | jq '.runners | length'`
Expected: 輸出 runner 數量（如 6）

- [ ] **Step 4: Dashboard 端對端測試**

Run: `./bin/4x live -w`
Expected:
1. Doctor 按鈕出現在 sidebar 底部
2. 點擊後顯示 runner 卡片和 usage 表格
3. 點擊 "4x Live" 可回首頁
4. 重新點 Doctor 頁面正常刷新

- [ ] **Step 5: check-docs 驗證**

Run: `make check-docs`
Expected: PASS（doctor subcommand 已寫在 cli.md）

- [ ] **Step 6: 最終 commit（如有修正）**

```bash
git add -A
git commit -m "fix: address verification issues for 4x doctor"
```
