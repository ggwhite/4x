# F058: Anti-Hack Guardrails — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add holdout path enforcement, cruel metrics comparison, and anti-hack reasoning prompts so metric-driven features can't be gamed by agents.

**Architecture:** Three additive layers — (1) types in `protocol/types.go` + JSON schema, (2) guard checks in `guard/` for holdout and metrics, (3) template conditionals in `templates/*.md.tmpl` for prompt injection. All opt-in via feature YAML fields.

**Tech Stack:** Go 1.26+, `text/template`, `encoding/json`, `path/filepath`, `os/exec`, `regexp`

---

### File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/protocol/types.go` | Modify | Add `Metric` struct, new fields on `Feature`, `Baseline`, `VerifyEvidence` |
| `schemas/feature.schema.json` | Modify | Add `metrics`, `holdout_paths`, `anti_hack` definitions |
| `internal/guard/check.go` | Modify | Add `checkHoldout()`, wire into `Check()`, add metrics gate to `checkTestingToAccepting()` |
| `internal/guard/metrics.go` | Create | `CompareMetrics()`, `CaptureMetricValues()` |
| `internal/guard/metrics_test.go` | Create | Tests for `CompareMetrics` and `CaptureMetricValues` |
| `internal/guard/check_test.go` | Modify | Add holdout and metrics gate tests |
| `internal/gitops/monorepo.go` | Modify | Call `CaptureMetricValues` in `CaptureBaseline` |
| `internal/gitops/multirepo.go` | Modify | Call `CaptureMetricValues` in `CaptureBaseline` |
| `templates/coder.md.tmpl` | Modify | Add holdout warning + metrics collection sections |
| `templates/reviewer.md.tmpl` | Modify | Add anti-hack reasoning section |
| `templates/deep-reviewer.md.tmpl` | Modify | Add anti-hack reasoning section |
| `templates/tester.md.tmpl` | Modify | Add metrics collection requirement |

---

### Task 1: Protocol Types — Metric struct and Feature/Baseline/VerifyEvidence fields

**Files:**
- Modify: `internal/protocol/types.go`

- [ ] **Step 1: Write test — Feature YAML with metrics unmarshals correctly**

在 `internal/protocol/` 中還沒有 feature 解析的獨立測試檔，但 `workspace.go` 的 `LoadFeature` 做 YAML unmarshal。我們在現有 test 檔加一個 case 驗證新欄位能正確解析。

先在 `internal/protocol/workspace_test.go`（若無則 `types_test.go`）加：

```go
func TestFeature_MetricFields(t *testing.T) {
	yamlData := `
id: feat-perf
name: "Perf test"
metrics:
  - name: coverage
    direction: higher
    command: "go test ./..."
    extract: "coverage: (\\d+\\.\\d+)%"
  - name: latency
    direction: lower
    command: "./bench.sh"
    extract: "p99=(\\d+)"
holdout_paths:
  - "testdata/holdout/**"
  - "bench/golden/*.json"
anti_hack: true
`
	var f protocol.Feature
	if err := yaml.Unmarshal([]byte(yamlData), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(f.Metrics))
	}
	if f.Metrics[0].Name != "coverage" || f.Metrics[0].Direction != "higher" {
		t.Errorf("metric 0 = %+v", f.Metrics[0])
	}
	if f.Metrics[1].Command != "./bench.sh" {
		t.Errorf("metric 1 command = %q", f.Metrics[1].Command)
	}
	if len(f.HoldoutPaths) != 2 {
		t.Errorf("expected 2 holdout paths, got %d", len(f.HoldoutPaths))
	}
	if !f.AntiHack {
		t.Error("expected anti_hack=true")
	}
}

func TestFeature_NoMetrics_BackwardCompat(t *testing.T) {
	yamlData := `
id: feat-simple
name: "Simple feature"
status: not-started
`
	var f protocol.Feature
	if err := yaml.Unmarshal([]byte(yamlData), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Metrics) != 0 {
		t.Errorf("expected no metrics, got %d", len(f.Metrics))
	}
	if len(f.HoldoutPaths) != 0 {
		t.Errorf("expected no holdout paths, got %d", len(f.HoldoutPaths))
	}
	if f.AntiHack {
		t.Error("expected anti_hack=false by default")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/protocol/ -run TestFeature_Metric -v`
Expected: FAIL — `Feature` has no field `Metrics`

- [ ] **Step 3: Add Metric struct and new fields to Feature**

在 `internal/protocol/types.go` 的 `Feature` struct 之前加：

```go
// Metric 定義量化指標的名稱、方向、採集指令與提取 regex
type Metric struct {
	Name      string `yaml:"name" json:"name"`
	Direction string `yaml:"direction" json:"direction"`
	Command   string `yaml:"command" json:"command"`
	Extract   string `yaml:"extract" json:"extract"`
}
```

在 `Feature` struct 裡、`Plan` 欄位之後加三個欄位：

```go
Metrics      []Metric `yaml:"metrics,omitempty" json:"metrics,omitempty"`
HoldoutPaths []string `yaml:"holdout_paths,omitempty" json:"holdout_paths,omitempty"`
AntiHack     bool     `yaml:"anti_hack,omitempty" json:"anti_hack,omitempty"`
```

- [ ] **Step 4: Add Metrics field to Baseline struct**

在 `internal/protocol/types.go` 的 `Baseline` struct 加：

```go
Metrics map[string]float64 `json:"metrics,omitempty"`
```

- [ ] **Step 5: Add Metrics field to VerifyEvidence struct**

在 `internal/protocol/types.go` 的 `VerifyEvidence` struct 加：

```go
Metrics map[string]float64 `json:"metrics,omitempty"`
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/protocol/ -run TestFeature_Metric -v`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass, no regressions

- [ ] **Step 8: Commit**

```bash
git add internal/protocol/types.go internal/protocol/types_test.go
git commit -m "feat(F058): add Metric struct and anti-hack fields to Feature/Baseline/VerifyEvidence"
```

---

### Task 2: Feature JSON Schema Update

**Files:**
- Modify: `schemas/feature.schema.json`

- [ ] **Step 1: Add metrics, holdout_paths, anti_hack to schema**

在 `schemas/feature.schema.json` 的 `properties` 物件裡，`depends` 之後加：

```json
"metrics": {
  "type": "array",
  "items": {
    "type": "object",
    "required": ["name", "direction", "command", "extract"],
    "properties": {
      "name": { "type": "string" },
      "direction": { "type": "string", "enum": ["higher", "lower"] },
      "command": { "type": "string" },
      "extract": { "type": "string" }
    }
  }
},
"holdout_paths": {
  "type": "array",
  "items": { "type": "string" }
},
"anti_hack": { "type": "boolean" }
```

- [ ] **Step 2: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add schemas/feature.schema.json
git commit -m "feat(F058): add metrics, holdout_paths, anti_hack to feature schema"
```

---

### Task 3: CompareMetrics — Cruel Metrics Comparison

**Files:**
- Create: `internal/guard/metrics.go`
- Create: `internal/guard/metrics_test.go`

- [ ] **Step 1: Write failing tests for CompareMetrics**

建立 `internal/guard/metrics_test.go`：

```go
package guard

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestCompareMetrics_AllImproved(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
		{Name: "latency", Direction: "lower"},
	}
	baseline := map[string]float64{"coverage": 80.0, "latency": 120.0}
	current := map[string]float64{"coverage": 85.0, "latency": 100.0}

	pass, details := CompareMetrics(baseline, current, defs)
	if !pass {
		t.Errorf("all improved should pass, details: %v", details)
	}
}

func TestCompareMetrics_OneRegressed(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
		{Name: "latency", Direction: "lower"},
	}
	baseline := map[string]float64{"coverage": 80.0, "latency": 120.0}
	current := map[string]float64{"coverage": 85.0, "latency": 130.0}

	pass, details := CompareMetrics(baseline, current, defs)
	if pass {
		t.Error("latency regressed, should fail")
	}
	found := false
	for _, d := range details {
		if len(d) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected detail messages")
	}
}

func TestCompareMetrics_Equal(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
	}
	baseline := map[string]float64{"coverage": 80.0}
	current := map[string]float64{"coverage": 80.0}

	pass, _ := CompareMetrics(baseline, current, defs)
	if !pass {
		t.Error("equal values should pass (not a regression)")
	}
}

func TestCompareMetrics_MissingBaseline(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
		{Name: "latency", Direction: "lower"},
	}
	baseline := map[string]float64{"coverage": 80.0}
	current := map[string]float64{"coverage": 85.0, "latency": 100.0}

	pass, _ := CompareMetrics(baseline, current, defs)
	if !pass {
		t.Error("missing baseline metric should be skipped, not fail")
	}
}

func TestCompareMetrics_MissingCurrent(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
	}
	baseline := map[string]float64{"coverage": 80.0}
	current := map[string]float64{}

	pass, details := CompareMetrics(baseline, current, defs)
	if pass {
		t.Error("missing current metric should fail")
	}
	_ = details
}

func TestCompareMetrics_NilMaps(t *testing.T) {
	defs := []protocol.Metric{
		{Name: "coverage", Direction: "higher"},
	}
	pass, _ := CompareMetrics(nil, nil, defs)
	if !pass {
		t.Error("nil baseline should skip all, pass")
	}
}

func TestCompareMetrics_NoDefs(t *testing.T) {
	pass, _ := CompareMetrics(
		map[string]float64{"x": 1},
		map[string]float64{"x": 2},
		nil,
	)
	if !pass {
		t.Error("no metric defs should pass")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/guard/ -run TestCompareMetrics -v`
Expected: FAIL — `CompareMetrics` undefined

- [ ] **Step 3: Implement CompareMetrics**

建立 `internal/guard/metrics.go`：

```go
package guard

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// CompareMetrics 比較 baseline 與 current 指標值，任一維度退步則整體 FAIL。
// baseline 或 current 中缺少的 metric：baseline 缺少則跳過，current 缺少則 FAIL。
func CompareMetrics(baseline, current map[string]float64, defs []protocol.Metric) (pass bool, details []string) {
	if len(defs) == 0 {
		return true, nil
	}
	pass = true
	for _, m := range defs {
		bv, bOK := baseline[m.Name]
		if !bOK {
			details = append(details, fmt.Sprintf("[SKIP] %s: no baseline value", m.Name))
			continue
		}
		cv, cOK := current[m.Name]
		if !cOK {
			pass = false
			details = append(details, fmt.Sprintf("[FAIL] %s: no current value (baseline=%.4g)", m.Name, bv))
			continue
		}

		regressed := false
		switch m.Direction {
		case "higher":
			regressed = cv < bv
		case "lower":
			regressed = cv > bv
		}

		if regressed {
			pass = false
			details = append(details, fmt.Sprintf("[FAIL] %s: regressed (baseline=%.4g → current=%.4g, direction=%s)", m.Name, bv, cv, m.Direction))
		} else {
			details = append(details, fmt.Sprintf("[OK] %s: baseline=%.4g → current=%.4g", m.Name, bv, cv))
		}
	}
	return pass, details
}

// CaptureMetricValues 執行 metric 定義中的 command 並用 extract regex 提取數值。
// 失敗的 metric 不放入結果 map（後續比較時跳過）。
func CaptureMetricValues(root string, metrics []protocol.Metric) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	result := make(map[string]float64)
	for _, m := range metrics {
		val, err := runAndExtract(root, m.Command, m.Extract)
		if err != nil {
			continue
		}
		result[m.Name] = val
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func runAndExtract(dir, command, extract string) (float64, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("command %q failed: %w", command, err)
	}
	re, err := regexp.Compile(extract)
	if err != nil {
		return 0, fmt.Errorf("invalid regex %q: %w", extract, err)
	}
	matches := re.FindStringSubmatch(strings.TrimSpace(string(out)))
	if len(matches) < 2 {
		return 0, fmt.Errorf("regex %q did not match output", extract)
	}
	return strconv.ParseFloat(matches[1], 64)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/guard/ -run TestCompareMetrics -v`
Expected: all PASS

- [ ] **Step 5: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/guard/metrics.go internal/guard/metrics_test.go
git commit -m "feat(F058): add CompareMetrics and CaptureMetricValues"
```

---

### Task 4: Holdout Scope Check

**Files:**
- Modify: `internal/guard/check.go`
- Modify: `internal/guard/check_test.go`

- [ ] **Step 1: Write failing tests for checkHoldout**

在 `internal/guard/check_test.go` 末尾加：

```go
type mockDetector struct {
	repos []string
	files []string
}

func (m *mockDetector) DetectChangedRepos() []string { return m.repos }

func TestCheckHoldout_NoHoldoutPaths(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})
	ws.SaveFeature(protocol.Feature{ID: "feat-1", Name: "No holdout"})

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("no holdout paths should pass: %v", result.Errors)
	}
}

func TestCheckHoldout_Violation(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseCoding})
	dir := ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	ws.SaveFeature(protocol.Feature{
		ID:           "feat-1",
		Name:         "Holdout test",
		HoldoutPaths: []string{"testdata/holdout/*"},
	})

	detector := &fakeChangedFiles{files: []string{"testdata/holdout/secret.json", "main.go"}}
	result := Check(ws, "feat-1", detector)
	if result.Pass {
		t.Error("touching holdout path should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "holdout") && strings.Contains(e, "testdata/holdout/secret.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected holdout violation error, got: %v", result.Errors)
	}
}

func TestCheckHoldout_NoViolation(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseCoding})
	dir := ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	ws.SaveFeature(protocol.Feature{
		ID:           "feat-1",
		Name:         "Holdout test",
		HoldoutPaths: []string{"testdata/holdout/*"},
	})

	detector := &fakeChangedFiles{files: []string{"main.go", "internal/foo.go"}}
	result := Check(ws, "feat-1", detector)
	if !result.Pass {
		t.Errorf("no holdout violation should pass: %v", result.Errors)
	}
}
```

我們需要擴展 `ScopeDetector` 介面或新增一個介面，讓 holdout 檢查能拿到 changed files 而非 changed repos。定義一個 `fakeChangedFiles` mock：

```go
type fakeChangedFiles struct {
	files []string
}

func (f *fakeChangedFiles) DetectChangedRepos() []string {
	repoSet := make(map[string]bool)
	for _, file := range f.files {
		parts := strings.SplitN(file, "/", 2)
		if len(parts) > 0 {
			repoSet[parts[0]] = true
		}
	}
	var repos []string
	for r := range repoSet {
		repos = append(repos, r)
	}
	return repos
}

func (f *fakeChangedFiles) DetectChangedFiles() []string {
	return f.files
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/guard/ -run TestCheckHoldout -v`
Expected: FAIL — `fakeChangedFiles` / `DetectChangedFiles` / `checkHoldout` undefined

- [ ] **Step 3: Extend ScopeDetector with DetectChangedFiles**

在 `internal/guard/check.go` 修改 `ScopeDetector` 介面：

```go
type ScopeDetector interface {
	DetectChangedRepos() []string
	DetectChangedFiles() []string
}
```

同步更新 `internal/gitops/gitops.go` 的 `Ops` 介面（若它實作 `ScopeDetector`）或確保 `gitops` 的實作也有 `DetectChangedFiles`。

在 `internal/guard/check.go` 加一個 helper 做 fallback：

```go
func changedFiles(root string, detector ScopeDetector) []string {
	if detector != nil {
		return detector.DetectChangedFiles()
	}
	return detectChangedFiles(root)
}

func detectChangedFiles(root string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
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

- [ ] **Step 4: Implement checkHoldout**

在 `internal/guard/check.go` 加：

```go
func checkHoldout(ws *protocol.Workspace, featureID string, detector ScopeDetector, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		return
	}
	if len(feature.HoldoutPaths) == 0 {
		return
	}
	files := changedFiles(ws.Root, detector)
	for _, f := range files {
		for _, pattern := range feature.HoldoutPaths {
			matched, err := filepath.Match(pattern, f)
			if err != nil {
				continue
			}
			if matched {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf("holdout violation: %q matches holdout pattern %q", f, pattern))
			}
		}
	}
}
```

在 `Check()` 函式裡、`checkScope()` 之後加：

```go
checkHoldout(ws, featureID, detector, &r)
```

- [ ] **Step 5: Update gitops to implement DetectChangedFiles**

在 `internal/gitops/monorepo.go` 加：

```go
func (m *monoRepo) DetectChangedFiles() []string {
	return detectChangedFilesFromRoot(m.root)
}
```

在 `internal/gitops/multirepo.go` 加對應實作（收集所有 repo 的 changed files）。

在 `internal/gitops/gitops.go` 的 `Ops` 介面加 `DetectChangedFiles() []string`。

共用 helper 放 `internal/gitops/gitops.go`：

```go
func detectChangedFilesFromRoot(root string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
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

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/guard/ -run TestCheckHoldout -v`
Expected: all PASS

- [ ] **Step 7: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 8: Commit**

```bash
git add internal/guard/check.go internal/guard/check_test.go internal/gitops/gitops.go internal/gitops/monorepo.go internal/gitops/multirepo.go
git commit -m "feat(F058): add holdout scope check with DetectChangedFiles"
```

---

### Task 5: Metrics Gate in checkTestingToAccepting

**Files:**
- Modify: `internal/guard/check.go`
- Modify: `internal/guard/check_test.go`

- [ ] **Step 1: Write failing tests for metrics gate**

在 `internal/guard/check_test.go` 末尾加：

```go
func TestCheckTestingToAccepting_MetricsPass(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ws.SaveFeature(protocol.Feature{
		ID:   "feat-1",
		Name: "Metric test",
		Metrics: []protocol.Metric{
			{Name: "coverage", Direction: "higher"},
		},
	})

	baseline := protocol.Baseline{
		Metrics: map[string]float64{"coverage": 80.0},
	}
	baselineData, _ := json.Marshal(baseline)
	writeFile(t, filepath.Join(featureDir, protocol.BaselineFile), string(baselineData))

	verify := protocol.VerifyEvidence{
		Passed:  true,
		Round:   1,
		Metrics: map[string]float64{"coverage": 85.0},
	}
	verifyData, _ := json.Marshal(verify)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(verifyData))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("metrics improved should pass, got errors: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MetricsRegressed(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ws.SaveFeature(protocol.Feature{
		ID:   "feat-1",
		Name: "Metric test",
		Metrics: []protocol.Metric{
			{Name: "coverage", Direction: "higher"},
			{Name: "latency", Direction: "lower"},
		},
	})

	baseline := protocol.Baseline{
		Metrics: map[string]float64{"coverage": 80.0, "latency": 120.0},
	}
	baselineData, _ := json.Marshal(baseline)
	writeFile(t, filepath.Join(featureDir, protocol.BaselineFile), string(baselineData))

	verify := protocol.VerifyEvidence{
		Passed:  true,
		Round:   1,
		Metrics: map[string]float64{"coverage": 85.0, "latency": 130.0},
	}
	verifyData, _ := json.Marshal(verify)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(verifyData))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("latency regressed, should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "latency") && strings.Contains(e, "regressed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected metric regression error, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_NoMetrics_Unchanged(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ws.SaveFeature(protocol.Feature{ID: "feat-1", Name: "Simple"})

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("no metrics feature should pass as before: %v", result.Errors)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/guard/ -run TestCheckTestingToAccepting_Metrics -v`
Expected: FAIL — metrics comparison not wired

- [ ] **Step 3: Wire metrics gate into checkTestingToAccepting**

在 `internal/guard/check.go` 的 `checkTestingToAccepting` 函式裡，`evidence.Passed` 檢查之後加：

```go
feature, featureErr := ws.LoadFeature(featureID)
if featureErr == nil && len(feature.Metrics) > 0 {
	baselinePath := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	baselineData, baselineErr := os.ReadFile(baselinePath)
	if baselineErr == nil {
		var baseline protocol.Baseline
		if json.Unmarshal(baselineData, &baseline) == nil && baseline.Metrics != nil {
			metricsPass, details := CompareMetrics(baseline.Metrics, evidence.Metrics, feature.Metrics)
			if !metricsPass {
				r.Pass = false
				for _, d := range details {
					if strings.HasPrefix(d, "[FAIL]") {
						r.Errors = append(r.Errors, "metric "+d)
					}
				}
			}
		}
	}
}
```

注意：需要在檔案頂部加 `import` 裡確認 `strings` 已存在（已有）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/guard/ -run TestCheckTestingToAccepting -v`
Expected: all PASS

- [ ] **Step 5: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/guard/check.go internal/guard/check_test.go
git commit -m "feat(F058): wire cruel metrics comparison into testing-to-accepting gate"
```

---

### Task 6: CaptureBaseline — Metrics Capture Integration

**Files:**
- Modify: `internal/gitops/monorepo.go`
- Modify: `internal/gitops/multirepo.go`

- [ ] **Step 1: Write test for metrics capture in baseline**

在 `internal/gitops/monorepo_test.go` 加：

```go
func TestMonoRepo_CaptureBaseline_WithMetrics(t *testing.T) {
	// 建 git repo fixture（類似現有 TestMonoRepo_CaptureBaseline）
	// 建 feature YAML 包含 metrics
	// CaptureBaseline 後讀 baseline.json，確認 metrics 欄位存在
	// 因為 command 會失敗（測試環境沒有真正的 command），metrics 應為 nil（graceful skip）
}
```

這個測試驗證 CaptureBaseline 在有 metrics 時不會 panic，且 command 失敗時 graceful 處理。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitops/ -run TestMonoRepo_CaptureBaseline_WithMetrics -v`
Expected: FAIL — CaptureBaseline 不接受 metrics 參數

- [ ] **Step 3: Modify CaptureBaseline to accept and capture metrics**

修改 `Ops` 介面的 `CaptureBaseline` 簽名：

```go
CaptureBaseline(featureID string, featureRepos []string, metrics []protocol.Metric) error
```

在 `monoRepo.CaptureBaseline` 裡，寫入 baseline 前加：

```go
baseline.Metrics = guard.CaptureMetricValues(m.root, metrics)
```

`multirepo.go` 同樣修改。

更新 `cmd/4x/run.go` 中呼叫 `CaptureBaseline` 的地方，傳入 `feature.Metrics`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitops/ -run TestMonoRepo_CaptureBaseline -v`
Expected: all PASS

- [ ] **Step 5: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/gitops/gitops.go internal/gitops/monorepo.go internal/gitops/multirepo.go cmd/4x/run.go
git commit -m "feat(F058): capture metric values during baseline snapshot"
```

---

### Task 7: Template — Coder Holdout Warning + Metrics Instructions

**Files:**
- Modify: `templates/coder.md.tmpl`

- [ ] **Step 1: Add holdout warning section**

在 `templates/coder.md.tmpl` 的 `== Constraints ==` 之前加：

```
{{- if .Feature.HoldoutPaths}}

== Holdout 路徑（禁止讀寫） ==
以下路徑是 holdout 資料，你不可以讀取或修改這些檔案：
{{- range .Feature.HoldoutPaths}}
- {{.}}
{{- end}}
違反將導致 guardrail 失敗。
{{- end}}
```

- [ ] **Step 2: Add metrics collection section**

在同位置、holdout 之後加：

```
{{- if .Feature.Metrics}}

== 指標採集 ==
每輪結束時，在 verify.json 的 metrics 欄位記錄以下指標的當前值：
{{- range .Feature.Metrics}}
- {{.Name}}（{{.Direction}} is better）：`{{.Command}}`
{{- end}}
{{- end}}
```

- [ ] **Step 3: Verify template renders**

Run: `go build ./cmd/4x && bin/4x prompt F058-anti-hack-guard --role coder --round 1 2>/dev/null | head -20`
Expected: 輸出包含 holdout 和 metrics 段落（如果 feature YAML 有設定的話）

- [ ] **Step 4: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add templates/coder.md.tmpl
git commit -m "feat(F058): add holdout warning and metrics collection to coder template"
```

---

### Task 8: Template — Reviewer Anti-Hack Reasoning

**Files:**
- Modify: `templates/reviewer.md.tmpl`
- Modify: `templates/deep-reviewer.md.tmpl`

- [ ] **Step 1: Add anti-hack reasoning to reviewer template**

在 `templates/reviewer.md.tmpl` 的 `== Constraints ==` 之前加：

```
{{- if .Feature.AntiHack}}
{{- if .Feature.Metrics}}

== 非 Hack 論述（必填） ==
此 feature 啟用了 anti-hack 檢查。對於以下每個指標改善，你必須在 review report 中回答：
{{- range .Feature.Metrics}}
- {{.Name}}：改善是否來自真實的程式碼改進？排除過擬合、hardcode、刪測試案例等取巧手段。
{{- end}}
若無法確認改善非 hack，verdict 必須為 FAIL。
{{- end}}
{{- end}}
```

- [ ] **Step 2: Add anti-hack reasoning to deep-reviewer template**

在 `templates/deep-reviewer.md.tmpl` 的 `== Constraints ==` 之前加同樣內容。

- [ ] **Step 3: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add templates/reviewer.md.tmpl templates/deep-reviewer.md.tmpl
git commit -m "feat(F058): add anti-hack reasoning requirement to reviewer templates"
```

---

### Task 9: Template — Tester Metrics Collection

**Files:**
- Modify: `templates/tester.md.tmpl`

- [ ] **Step 1: Add metrics collection to tester template**

在 `templates/tester.md.tmpl` 的 `== verify.json format ==` 之前加：

```
{{- if .Feature.Metrics}}

== 指標採集（必填） ==
verify.json 必須包含 metrics 欄位，格式為 `{"metric_name": numeric_value}`。
對每個指標執行指令並用 regex 提取數值：
{{- range .Feature.Metrics}}
- {{.Name}}：`{{.Command}}` → regex `{{.Extract}}`
{{- end}}
{{- end}}
```

- [ ] **Step 2: Run full test suite**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add templates/tester.md.tmpl
git commit -m "feat(F058): add metrics collection requirement to tester template"
```

---

### Task 10: Integration Verification

- [ ] **Step 1: Build and run all tests**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: all pass, zero regressions

- [ ] **Step 2: Verify backward compatibility — existing feature without anti-hack fields**

Run: `bin/4x prompt F055-run-command-error --role coder --round 1 2>/dev/null | grep -c holdout`
Expected: 0（無 holdout 段落出現）

- [ ] **Step 3: Verify anti-hack feature prompt renders correctly**

建一個臨時 feature YAML 有 metrics + holdout_paths + anti_hack，跑 `4x prompt` 確認四個角色的 prompt 都正確注入。

- [ ] **Step 4: Run docs sync check**

Run: `make check-docs-sync && make check-i18n`
Expected: OK 或列出需更新項目，依輸出處理

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "feat(F058): integration verification and cleanup"
```
