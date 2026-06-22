{% raw %}
# F096 — Discovered Feature Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓 auto-discover 產出的 feature 在進入 backlog 前經過 LLM enrichment，補齊 subtasks/repos/rules/priority，取代現有的薄 feature 直接存入流程。

**Architecture:** 新增 `internal/enrich` package，提供 `Enricher.Enrich()` 方法，接收 `DiscoveredFeature` + 專案脈絡，透過 Runner 呼叫 LLM 產出結構化 JSON，解析並驗證後回傳完整 Feature。在 `autoDiscoverFeatures`（run.go）原本 SaveFeature 前插入 enrich 步驟；新增 `StatusDraft` 供半自動模式使用，搭配 `4x approve`/`4x reject` CLI 指令。

**Tech Stack:** Go 1.26+, Cobra CLI, `text/template`, `go:embed`, `encoding/json`

## Global Constraints

- CLI 層嚴禁呼叫 LLM — enrichment 透過 Runner 介面委託
- enrich 後 feature 必須含 ≥ 2 個可獨立驗證的 subtask
- 不得捏造需求，資訊不足時丟棄 candidate
- 維持向後相容：`enrichDiscoveredFeatures: false` 時走舊路徑
- 測試用 Go 標準 testing package，mock runner 回傳固定 JSON

---

### Task 1: 新增 `StatusDraft` 與 enrichment 設定欄位

**Files:**
- Modify: `internal/feature/types.go:8-16`
- Modify: `internal/protocol/types.go:320-331`
- Test: `internal/feature/types_test.go` (new)
- Test: `internal/protocol/types_test.go` (existing, add cases)

**Interfaces:**
- Produces: `feature.StatusDraft` 常量，供 Task 4、Task 5 使用
- Produces: `Config.EnrichDiscoveredFeatures` 和 `Config.EnrichAutoApprove` 欄位，供 Task 5 使用

- [ ] **Step 1: 寫 StatusDraft 的失敗測試**

建立 `internal/feature/types_test.go`：

```go
package feature

import "testing"

func TestStatusDraft_IsValidStatus(t *testing.T) {
	s := StatusDraft
	if s != "draft" {
		t.Errorf("StatusDraft = %q, want %q", s, "draft")
	}
}

func TestBatchCompleted_DraftIsFalse(t *testing.T) {
	if BatchCompleted(StatusDraft) {
		t.Error("BatchCompleted(StatusDraft) = true, want false")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/feature/ -run TestStatusDraft -v`
Expected: FAIL — `StatusDraft` undefined

- [ ] **Step 3: 在 `internal/feature/types.go` 加入 StatusDraft**

在 `StatusReadyForReview` 後面加一行：

```go
StatusDraft          Status = "draft"
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/feature/ -run "TestStatusDraft|TestBatchCompleted_Draft" -v`
Expected: PASS

- [ ] **Step 5: 寫 enrichment 設定欄位的失敗測試**

在既有的 `internal/protocol/` 測試中（或建新檔 `internal/protocol/config_enrich_test.go`）：

```go
package protocol

import (
	"encoding/json"
	"testing"
)

func TestConfig_EnrichFields_Defaults(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.EnrichDiscoveredFeatures {
		t.Error("EnrichDiscoveredFeatures zero-value should be false")
	}
	if cfg.EnrichAutoApprove {
		t.Error("EnrichAutoApprove zero-value should be false")
	}
}

func TestConfig_EnrichFields_Explicit(t *testing.T) {
	raw := `{"enrich_discovered_features": true, "enrich_auto_approve": true}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.EnrichDiscoveredFeatures {
		t.Error("EnrichDiscoveredFeatures should be true")
	}
	if !cfg.EnrichAutoApprove {
		t.Error("EnrichAutoApprove should be true")
	}
}
```

- [ ] **Step 6: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestConfig_EnrichFields -v`
Expected: FAIL — `EnrichDiscoveredFeatures` field not found

- [ ] **Step 7: 在 `internal/protocol/types.go` Config struct 加欄位**

在 `MaxDiscoveredFeatures` 後面加兩個欄位：

```go
	// EnrichDiscoveredFeatures 啟用後，auto-discover 產出的 candidate 會經 LLM enrichment
	// 補齊 subtasks/repos/rules/priority 後才存入 backlog；關閉時走舊路徑直接存薄 feature。
	EnrichDiscoveredFeatures bool `json:"enrich_discovered_features,omitempty"`
	// EnrichAutoApprove 控制 enrich 後的 feature 狀態：true → not-started（全自動），
	// false → draft（需人工 approve）。僅在 EnrichDiscoveredFeatures 開啟時有意義。
	EnrichAutoApprove bool `json:"enrich_auto_approve,omitempty"`
```

- [ ] **Step 8: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestConfig_EnrichFields -v`
Expected: PASS

- [ ] **Step 9: 跑全量測試確認無回歸**

Run: `go test -race ./internal/feature/ ./internal/protocol/`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/feature/types.go internal/feature/types_test.go internal/protocol/types.go internal/protocol/config_enrich_test.go
git commit -m "feat(F096): add StatusDraft and enrichment config fields"
```

---

### Task 2: `internal/enrich` — 回應解析與驗證

**Files:**
- Create: `internal/enrich/types.go`
- Create: `internal/enrich/parse.go`
- Test: `internal/enrich/parse_test.go` (new)

**Interfaces:**
- Produces: `EnrichResult` struct — `Feature feature.Feature`, `Discarded bool`, `Reason string`
- Produces: `parseResponse(logContent string) (*enrichResponse, error)` — 從 runner log 中提取 `[ENRICHMENT-RESULT]` block 並解析 JSON
- Produces: `validate(resp *enrichResponse) error` — 驗證 subtasks ≥ 2、必填欄位非空

- [ ] **Step 1: 寫 types.go**

建立 `internal/enrich/types.go`：

```go
package enrich

import "github.com/ggwhite/4x/internal/feature"

// EnrichResult 是 Enrich 的回傳結果
type EnrichResult struct {
	Feature   feature.Feature
	Discarded bool
	Reason    string
}

// enrichResponse 是 LLM 回傳的 JSON 結構
type enrichResponse struct {
	Subtasks    []enrichSubtask `json:"subtasks"`
	Repos       []string        `json:"repos"`
	Rules       []string        `json:"rules"`
	Priority    int             `json:"priority"`
	Description string          `json:"description"`
}

// enrichSubtask 是 LLM 回傳的 subtask JSON 結構
type enrichSubtask struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
```

- [ ] **Step 2: 寫 parse_test.go — 全部測試**

建立 `internal/enrich/parse_test.go`：

```go
package enrich

import "testing"

func TestParseResponse_ValidJSON(t *testing.T) {
	log := `some preamble
[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "step-a", "name": "Task A", "description": "Do A"},
    {"id": "step-b", "name": "Task B", "description": "Do B"}
  ],
  "repos": ["internal/protocol"],
  "rules": ["no breaking changes"],
  "priority": 2,
  "description": "Enhanced description"
}
[/ENRICHMENT-RESULT]
some epilogue`

	resp, err := parseResponse(log)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if len(resp.Subtasks) != 2 {
		t.Errorf("subtasks count = %d, want 2", len(resp.Subtasks))
	}
	if resp.Priority != 2 {
		t.Errorf("priority = %d, want 2", resp.Priority)
	}
	if resp.Description != "Enhanced description" {
		t.Errorf("description = %q", resp.Description)
	}
}

func TestParseResponse_NoMarker(t *testing.T) {
	_, err := parseResponse("no markers here")
	if err == nil {
		t.Error("expected error for missing marker")
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	log := "[ENRICHMENT-RESULT]\n{invalid json}\n[/ENRICHMENT-RESULT]"
	_, err := parseResponse(log)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidate_Pass(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err != nil {
		t.Errorf("validate() = %v, want nil", err)
	}
}

func TestValidate_InsufficientSubtasks(t *testing.T) {
	resp := &enrichResponse{
		Subtasks:    []enrichSubtask{{ID: "a", Name: "A", Description: "do A"}},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for < 2 subtasks")
	}
}

func TestValidate_EmptySubtaskID(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    3,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for empty subtask ID")
	}
}

func TestValidate_EmptyDescription(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Priority: 3,
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for empty description")
	}
}

func TestValidate_ZeroPriority(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for zero priority")
	}
}

func TestValidate_PriorityOutOfRange(t *testing.T) {
	resp := &enrichResponse{
		Subtasks: []enrichSubtask{
			{ID: "a", Name: "A", Description: "do A"},
			{ID: "b", Name: "B", Description: "do B"},
		},
		Repos:       []string{"internal/foo"},
		Priority:    6,
		Description: "desc",
	}
	if err := validate(resp); err == nil {
		t.Error("expected error for priority > 5")
	}
}
```

- [ ] **Step 3: 跑測試確認失敗**

Run: `go test ./internal/enrich/ -v`
Expected: FAIL — `parseResponse` and `validate` undefined

- [ ] **Step 4: 實作 parse.go**

建立 `internal/enrich/parse.go`：

```go
package enrich

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	enrichResultStart = "[ENRICHMENT-RESULT]"
	enrichResultEnd   = "[/ENRICHMENT-RESULT]"
)

// parseResponse 從 runner log 中提取 [ENRICHMENT-RESULT] block 並解析 JSON。
func parseResponse(logContent string) (*enrichResponse, error) {
	startIdx := strings.Index(logContent, enrichResultStart)
	if startIdx < 0 {
		return nil, fmt.Errorf("enrichment marker %s not found", enrichResultStart)
	}
	after := logContent[startIdx+len(enrichResultStart):]

	endIdx := strings.Index(after, enrichResultEnd)
	if endIdx < 0 {
		return nil, fmt.Errorf("enrichment marker %s not found", enrichResultEnd)
	}
	jsonBlock := strings.TrimSpace(after[:endIdx])

	var resp enrichResponse
	if err := json.Unmarshal([]byte(jsonBlock), &resp); err != nil {
		return nil, fmt.Errorf("invalid enrichment JSON: %w", err)
	}
	return &resp, nil
}

// validate 驗證 LLM 回傳的 enrichment 結果是否滿足最低品質要求。
func validate(resp *enrichResponse) error {
	if len(resp.Subtasks) < 2 {
		return fmt.Errorf("insufficient subtasks: got %d, need >= 2", len(resp.Subtasks))
	}
	for i, st := range resp.Subtasks {
		if st.ID == "" {
			return fmt.Errorf("subtask[%d] has empty ID", i)
		}
		if st.Name == "" {
			return fmt.Errorf("subtask[%d] has empty name", i)
		}
	}
	if resp.Description == "" {
		return fmt.Errorf("description is empty")
	}
	if resp.Priority < 1 || resp.Priority > 5 {
		return fmt.Errorf("priority %d out of range [1,5]", resp.Priority)
	}
	return nil
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/enrich/ -v`
Expected: PASS (all 8 tests)

- [ ] **Step 6: Commit**

```bash
git add internal/enrich/types.go internal/enrich/parse.go internal/enrich/parse_test.go
git commit -m "feat(F096): add enrich response parsing and validation"
```

---

### Task 3: `internal/enrich` — 脈絡收集與 prompt 建構

**Files:**
- Create: `internal/enrich/context.go`
- Create: `internal/enrich/prompt.go`
- Create: `internal/enrich/enrich.md.tmpl`
- Test: `internal/enrich/context_test.go` (new)
- Test: `internal/enrich/prompt_test.go` (new)

**Interfaces:**
- Consumes: `protocol.Workspace.ListFeatures()` — 取得現有 feature 列表
- Produces: `collectContext(ws *protocol.Workspace) (*enrichContext, error)` — 收集脈絡
- Produces: `buildPrompt(candidate protocol.DiscoveredFeature, ectx *enrichContext) (string, error)` — 渲染 prompt

- [ ] **Step 1: 建立 prompt template**

建立 `internal/enrich/enrich.md.tmpl`：

```
你是 Feature Shaper。根據以下 candidate 描述和專案脈絡，產出完整 feature 規格。

## Candidate
標題：{{.Title}}
描述：{{.Description}}

## 專案脈絡

### 現有 Feature 列表
{{.FeatureList}}

### 目錄結構
{{.DirTree}}

### 相關程式碼片段
{{.CodeSnippets}}

## 輸出要求

在 [ENRICHMENT-RESULT] 和 [/ENRICHMENT-RESULT] 標記之間，以 JSON 格式回傳：

- subtasks: 至少 2 個可獨立驗證的子任務，每個有 id（kebab-case slug）、name、description
- repos: 推斷的影響路徑（對應上方目錄結構中的真實路徑）
- rules: 從描述萃取的約束條件（可為空陣列）
- priority: 1-5（1 最高）
- description: 可基於原始描述潤飾補充，但不得捏造需求

不確定的欄位不要硬填，寧可少寫。

輸出範例：
[ENRICHMENT-RESULT]
{"subtasks":[{"id":"impl-core","name":"實作核心邏輯","description":"..."},{"id":"add-tests","name":"補測試","description":"..."}],"repos":["internal/foo"],"rules":["不破壞既有 API"],"priority":3,"description":"完整描述..."}
[/ENRICHMENT-RESULT]
```

- [ ] **Step 2: 寫 context_test.go**

建立 `internal/enrich/context_test.go`：

```go
package enrich

import "testing"

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  int // 期待 keyword 數量 > 0 且 <= maxKeywords
	}{
		{"normal", "Add batch retry logic for failed features", 5},
		{"short", "Fix bug", 2},
		{"with stop words", "the a an is are of to in for and or", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kw := extractKeywords(tt.title)
			if len(kw) > maxKeywords {
				t.Errorf("extractKeywords(%q) returned %d keywords, max %d", tt.title, len(kw), maxKeywords)
			}
			if tt.want > 0 && len(kw) == 0 {
				t.Errorf("extractKeywords(%q) returned 0 keywords, want > 0", tt.title)
			}
			if tt.want == 0 && len(kw) != 0 {
				t.Errorf("extractKeywords(%q) returned %d keywords, want 0", tt.title, len(kw))
			}
		})
	}
}

func TestFormatFeatureList(t *testing.T) {
	list := formatFeatureList([]featureSummary{
		{ID: "F001-foo", Name: "F001: Foo", Description: "Foo desc"},
		{ID: "F002-bar", Name: "F002: Bar", Description: "Bar desc"},
	})
	if list == "" {
		t.Error("formatFeatureList returned empty string")
	}
}
```

- [ ] **Step 3: 跑測試確認失敗**

Run: `go test ./internal/enrich/ -run "TestExtractKeywords|TestFormatFeatureList" -v`
Expected: FAIL — functions undefined

- [ ] **Step 4: 實作 context.go**

建立 `internal/enrich/context.go`：

```go
package enrich

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

const maxKeywords = 5
const maxSnippetLinesPerKeyword = 10
const maxTotalSnippetLines = 50

// enrichContext 收集的專案脈絡
type enrichContext struct {
	FeatureList  string
	DirTree      string
	CodeSnippets string
}

// featureSummary 是 feature 的精簡摘要，用於 prompt
type featureSummary struct {
	ID          string
	Name        string
	Description string
}

// collectContext 收集專案脈絡供 enrichment prompt 使用。
func collectContext(ws *protocol.Workspace, title string) (*enrichContext, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}

	summaries := make([]featureSummary, len(features))
	for i, f := range features {
		summaries[i] = featureSummary{ID: f.ID, Name: f.Name, Description: truncate(f.Description, 200)}
	}

	dirTree := collectDirTree(ws.Root)
	keywords := extractKeywords(title)
	snippets := grepSnippets(ws.Root, keywords)

	return &enrichContext{
		FeatureList:  formatFeatureList(summaries),
		DirTree:      dirTree,
		CodeSnippets: snippets,
	}, nil
}

// extractKeywords 從 title 拆出關鍵字，過濾 stop words，取前 maxKeywords 個。
func extractKeywords(title string) []string {
	stops := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"of": true, "to": true, "in": true, "for": true, "and": true,
		"or": true, "on": true, "at": true, "by": true, "with": true,
		"from": true, "this": true, "that": true, "it": true, "as": true,
		"be": true, "was": true, "were": true, "been": true, "has": true,
		"have": true, "had": true, "do": true, "does": true, "did": true,
		"but": true, "not": true, "no": true, "if": true, "then": true,
	}
	words := strings.Fields(strings.ToLower(title))
	var keywords []string
	for _, w := range words {
		clean := strings.Trim(w, ".,;:!?\"'()-")
		if clean == "" || stops[clean] || len(clean) < 3 {
			continue
		}
		keywords = append(keywords, clean)
		if len(keywords) >= maxKeywords {
			break
		}
	}
	return keywords
}

// collectDirTree 執行 find 取得專案目錄結構（排除 vendor/node_modules/.git 等）。
func collectDirTree(root string) string {
	cmd := exec.Command("find", root, "-type", "d",
		"-not", "-path", "*/vendor/*",
		"-not", "-path", "*/node_modules/*",
		"-not", "-path", "*/.git/*",
		"-not", "-path", "*/.4x/*",
		"-maxdepth", "4",
	)
	out, err := cmd.Output()
	if err != nil {
		return "(directory tree unavailable)"
	}
	return string(out)
}

// grepSnippets 對每個 keyword grep 專案目錄，每個取前 maxSnippetLinesPerKeyword 行，
// 合計不超過 maxTotalSnippetLines 行。
func grepSnippets(root string, keywords []string) string {
	if len(keywords) == 0 {
		return "(no keywords to search)"
	}
	var b strings.Builder
	totalLines := 0
	for _, kw := range keywords {
		if totalLines >= maxTotalSnippetLines {
			break
		}
		remaining := maxTotalSnippetLines - totalLines
		limit := maxSnippetLinesPerKeyword
		if remaining < limit {
			limit = remaining
		}
		cmd := exec.Command("grep", "-rn", "--include=*.go", "-m", fmt.Sprintf("%d", limit), kw, root)
		out, _ := cmd.Output()
		if len(out) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### keyword: %s\n%s\n", kw, string(out))
		totalLines += strings.Count(string(out), "\n")
	}
	if b.Len() == 0 {
		return "(no code snippets found)"
	}
	return b.String()
}

// formatFeatureList 格式化 feature 列表為 prompt 用文字。
func formatFeatureList(features []featureSummary) string {
	if len(features) == 0 {
		return "(no existing features)"
	}
	var b strings.Builder
	for _, f := range features {
		fmt.Fprintf(&b, "- %s: %s — %s\n", f.ID, f.Name, f.Description)
	}
	return b.String()
}

// truncate 截斷字串到指定長度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

- [ ] **Step 5: 跑 context 測試確認通過**

Run: `go test ./internal/enrich/ -run "TestExtractKeywords|TestFormatFeatureList" -v`
Expected: PASS

- [ ] **Step 6: 寫 prompt_test.go**

建立 `internal/enrich/prompt_test.go`：

```go
package enrich

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestBuildPrompt_ContainsCandidate(t *testing.T) {
	candidate := protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Implement retry for failed API calls",
	}
	ectx := &enrichContext{
		FeatureList:  "- F001: Foo",
		DirTree:      "internal/\ncmd/",
		CodeSnippets: "some code",
	}
	prompt, err := buildPrompt(candidate, ectx)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "Add retry logic") {
		t.Error("prompt missing candidate title")
	}
	if !strings.Contains(prompt, "Implement retry for failed API calls") {
		t.Error("prompt missing candidate description")
	}
	if !strings.Contains(prompt, "F001: Foo") {
		t.Error("prompt missing feature list")
	}
	if !strings.Contains(prompt, "[ENRICHMENT-RESULT]") {
		t.Error("prompt missing enrichment marker instruction")
	}
}
```

- [ ] **Step 7: 跑測試確認失敗**

Run: `go test ./internal/enrich/ -run TestBuildPrompt -v`
Expected: FAIL — `buildPrompt` undefined

- [ ] **Step 8: 實作 prompt.go**

建立 `internal/enrich/prompt.go`：

```go
package enrich

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
)

//go:embed enrich.md.tmpl
var promptTemplateRaw string

var promptTmpl = template.Must(template.New("enrich").Parse(promptTemplateRaw))

// promptData 是 template 渲染用的資料
type promptData struct {
	Title        string
	Description  string
	FeatureList  string
	DirTree      string
	CodeSnippets string
}

// buildPrompt 渲染 enrichment prompt。
func buildPrompt(candidate protocol.DiscoveredFeature, ectx *enrichContext) (string, error) {
	data := promptData{
		Title:        candidate.Title,
		Description:  candidate.Description,
		FeatureList:  ectx.FeatureList,
		DirTree:      ectx.DirTree,
		CodeSnippets: ectx.CodeSnippets,
	}
	var buf bytes.Buffer
	if err := promptTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 9: 跑測試確認通過**

Run: `go test ./internal/enrich/ -run TestBuildPrompt -v`
Expected: PASS

- [ ] **Step 10: 跑全 enrich package 測試**

Run: `go test -race ./internal/enrich/`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/enrich/context.go internal/enrich/context_test.go internal/enrich/prompt.go internal/enrich/prompt_test.go internal/enrich/enrich.md.tmpl
git commit -m "feat(F096): add enrichment context collection and prompt builder"
```

---

### Task 4: `internal/enrich` — Enricher 主流程

**Files:**
- Create: `internal/enrich/enrich.go`
- Test: `internal/enrich/enrich_test.go` (new)

**Interfaces:**
- Consumes: `runner.Runner.Run(ctx, prompt) → (*runner.Result, error)` — 呼叫 LLM
- Consumes: `parseResponse()` from Task 2
- Consumes: `validate()` from Task 2
- Consumes: `collectContext()` from Task 3
- Consumes: `buildPrompt()` from Task 3
- Consumes: `feature.StatusDraft`, `feature.StatusNotStarted` from Task 1
- Produces: `New(ws, runner, autoApprove) *Enricher`
- Produces: `(*Enricher).Enrich(ctx, candidate) (*EnrichResult, error)`

- [ ] **Step 1: 寫 enrich_test.go**

建立 `internal/enrich/enrich_test.go`：

```go
package enrich

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// mockRunner 回傳預設的 log 內容
type mockRunner struct {
	logContent string
	exitCode   int
	err        error
}

func (m *mockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	tmp, _ := os.CreateTemp("", "enrich-test-*.log")
	tmp.WriteString(m.logContent)
	tmp.Close()
	return &runner.Result{ExitCode: m.exitCode, LogFile: tmp.Name()}, nil
}

func newTestWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".4x", "features"), 0o755)
	ws, err := protocol.OpenWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

const validEnrichLog = `Thinking about the feature...
[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "impl-core", "name": "Implement core", "description": "Core logic"},
    {"id": "add-tests", "name": "Add tests", "description": "Unit tests"}
  ],
  "repos": ["internal/protocol"],
  "rules": ["no breaking changes"],
  "priority": 3,
  "description": "Enhanced: add retry logic for failed operations"
}
[/ENRICHMENT-RESULT]
Done.`

func TestEnrich_Success_AutoApprove(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: validEnrichLog}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Retry failed operations",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if result.Discarded {
		t.Fatalf("Enrich() discarded: %s", result.Reason)
	}
	f := result.Feature
	if len(f.Subtasks) != 2 {
		t.Errorf("subtasks = %d, want 2", len(f.Subtasks))
	}
	if f.Status != feature.StatusNotStarted {
		t.Errorf("status = %q, want %q", f.Status, feature.StatusNotStarted)
	}
	if f.Priority == nil || *f.Priority != 3 {
		t.Errorf("priority = %v, want 3", f.Priority)
	}
}

func TestEnrich_Success_DraftMode(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: validEnrichLog}, false)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Retry failed operations",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if result.Feature.Status != feature.StatusDraft {
		t.Errorf("status = %q, want %q", result.Feature.Status, feature.StatusDraft)
	}
}

func TestEnrich_Discarded_InsufficientSubtasks(t *testing.T) {
	log := `[ENRICHMENT-RESULT]
{"subtasks":[{"id":"only-one","name":"Only one","description":"desc"}],"repos":["x"],"priority":3,"description":"desc"}
[/ENRICHMENT-RESULT]`
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: log}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded = true")
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestEnrich_Discarded_InvalidJSON(t *testing.T) {
	log := "[ENRICHMENT-RESULT]\n{bad json}\n[/ENRICHMENT-RESULT]"
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: log}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded = true")
	}
}

func TestEnrich_RunnerError(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{err: context.DeadlineExceeded}, true)
	_, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err == nil {
		t.Error("expected error from runner failure")
	}
}

func TestEnrich_RunnerNonZeroExit(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: "no markers", exitCode: 1}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded for non-zero exit + no markers")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/enrich/ -run "TestEnrich_" -v`
Expected: FAIL — `New` undefined

- [ ] **Step 3: 實作 enrich.go**

建立 `internal/enrich/enrich.go`：

```go
package enrich

import (
	"context"
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// Enricher 負責將薄 candidate 補強為完整 feature
type Enricher struct {
	ws          *protocol.Workspace
	runner      runner.Runner
	autoApprove bool
}

// New 建立 Enricher。autoApprove 控制 enrich 後的 feature 狀態：
// true → not-started（全自動），false → draft（需人工 approve）。
func New(ws *protocol.Workspace, r runner.Runner, autoApprove bool) *Enricher {
	return &Enricher{ws: ws, runner: r, autoApprove: autoApprove}
}

// Enrich 對單一 candidate 執行 LLM enrichment。
// 成功回傳完整 Feature（不含 ID/Name，由呼叫端填入）；
// 品質不足回傳 Discarded=true + Reason；Runner 錯誤回傳 error。
func (e *Enricher) Enrich(ctx context.Context, candidate protocol.DiscoveredFeature) (*EnrichResult, error) {
	ectx, err := collectContext(e.ws, candidate.Title)
	if err != nil {
		return nil, fmt.Errorf("collect context: %w", err)
	}

	prompt, err := buildPrompt(candidate, ectx)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	res, err := e.runner.Run(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("runner: %w", err)
	}

	logContent, err := os.ReadFile(res.LogFile)
	if err != nil {
		return nil, fmt.Errorf("read runner log: %w", err)
	}

	resp, err := parseResponse(string(logContent))
	if err != nil {
		return &EnrichResult{Discarded: true, Reason: fmt.Sprintf("invalid response format: %v", err)}, nil
	}

	if err := validate(resp); err != nil {
		return &EnrichResult{Discarded: true, Reason: err.Error()}, nil
	}

	status := feature.StatusNotStarted
	if !e.autoApprove {
		status = feature.StatusDraft
	}

	priority := resp.Priority
	subtasks := make([]feature.Subtask, len(resp.Subtasks))
	for i, st := range resp.Subtasks {
		subtasks[i] = feature.Subtask{
			ID:          st.ID,
			Name:        st.Name,
			Description: st.Description,
		}
	}

	f := feature.Feature{
		Description: resp.Description,
		Status:      status,
		Priority:    &priority,
		Repos:       resp.Repos,
		Subtasks:    subtasks,
		Rules:       resp.Rules,
	}

	return &EnrichResult{Feature: f}, nil
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test -race ./internal/enrich/ -run "TestEnrich_" -v`
Expected: PASS (all 6 tests)

- [ ] **Step 5: 跑全 enrich package 測試**

Run: `go test -race ./internal/enrich/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/enrich/enrich.go internal/enrich/enrich_test.go
git commit -m "feat(F096): implement Enricher main flow with runner integration"
```

---

### Task 5: 接入 `autoDiscoverFeatures` 流程

**Files:**
- Modify: `cmd/4x/run.go:2058-2127`
- Test: `cmd/4x/run_f096_test.go` (new)

**Interfaces:**
- Consumes: `enrich.New(ws, runner, autoApprove) *Enricher` from Task 4
- Consumes: `(*Enricher).Enrich(ctx, candidate) (*EnrichResult, error)` from Task 4
- Consumes: `Config.EnrichDiscoveredFeatures`, `Config.EnrichAutoApprove` from Task 1

- [ ] **Step 1: 寫整合測試**

建立 `cmd/4x/run_f096_test.go`：

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestAutoDiscover_EnrichDisabled(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	feature := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(feature)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: false,
	}

	writeDeepReviewReport(t, ws, feature.ID, 1, "[NEW-FEATURE] Thin Feature\nSome description")

	autoDiscoverFeatures(ws, feature, cfg, 1, nil)

	features, _ := ws.ListFeatures()
	var found *feat.Feature
	for _, f := range features {
		if f.ID != feature.ID {
			found = &f
			break
		}
	}
	if found == nil {
		t.Fatal("expected discovered feature to be created (enrich disabled = old path)")
	}
	if len(found.Subtasks) > 0 {
		t.Error("enrich disabled should produce thin feature without subtasks")
	}
	if found.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want %q", found.Status, feat.StatusNotStarted)
	}
}

func TestAutoDiscover_EnrichEnabled_DraftMode(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	feature := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(feature)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: true,
		EnrichAutoApprove:        false,
	}

	writeDeepReviewReport(t, ws, feature.ID, 1, "[NEW-FEATURE] Rich Feature\nNeeds enrichment")

	r := &mockEnrichRunner{logContent: validEnrichLogForTest}
	autoDiscoverFeatures(ws, feature, cfg, 1, r)

	features, _ := ws.ListFeatures()
	var found *feat.Feature
	for _, f := range features {
		if f.ID != feature.ID {
			found = &f
			break
		}
	}
	if found == nil {
		t.Fatal("expected discovered feature to be created")
	}
	if found.Status != feat.StatusDraft {
		t.Errorf("status = %q, want %q", found.Status, feat.StatusDraft)
	}
	if len(found.Subtasks) < 2 {
		t.Errorf("subtasks = %d, want >= 2", len(found.Subtasks))
	}
}

func TestAutoDiscover_EnrichFailed_Discarded(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	feature := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(feature)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: true,
		EnrichAutoApprove:        true,
	}

	writeDeepReviewReport(t, ws, feature.ID, 1, "[NEW-FEATURE] Bad Feature\nVague description")

	r := &mockEnrichRunner{logContent: "[ENRICHMENT-RESULT]\n{\"subtasks\":[],\"priority\":0}\n[/ENRICHMENT-RESULT]"}
	autoDiscoverFeatures(ws, feature, cfg, 1, r)

	features, _ := ws.ListFeatures()
	discoveredCount := 0
	for _, f := range features {
		if f.ID != feature.ID {
			discoveredCount++
		}
	}
	if discoveredCount != 0 {
		t.Errorf("expected 0 discovered features (enrich failed), got %d", discoveredCount)
	}
}

// helpers

func writeDeepReviewReport(t *testing.T, ws *protocol.Workspace, featureID string, round int, content string) {
	t.Helper()
	dir := ws.RoundDir(featureID, round)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, protocol.DeepReviewReport), []byte(content), 0o644)
}

const validEnrichLogForTest = `[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "impl", "name": "Implement", "description": "Do it"},
    {"id": "test", "name": "Test", "description": "Test it"}
  ],
  "repos": ["internal/foo"],
  "rules": [],
  "priority": 3,
  "description": "Enriched description"
}
[/ENRICHMENT-RESULT]`
```

注意：`setupTestWorkspace`、`mockEnrichRunner` 等 helper 可能需要從既有測試中復用或新建。調整時參考 `cmd/4x/run_loop_test.go` 和 `cmd/4x/cli_test.go` 的 helper pattern。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run "TestAutoDiscover_Enrich" -v`
Expected: FAIL — `autoDiscoverFeatures` 簽名不匹配（多了 runner 參數）

- [ ] **Step 3: 修改 `autoDiscoverFeatures` 加入 enrichment**

修改 `cmd/4x/run.go` 的 `autoDiscoverFeatures` 函式：

1. 加入 `runner.Runner` 參數（可為 nil，nil 時走舊路徑）
2. 在 create feature 迴圈中，依 `cfg.EnrichDiscoveredFeatures` 決定走 enrich 或舊路徑
3. enrich 失敗時記入 skipped 不存入

```go
func autoDiscoverFeatures(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, round int, r runner.Runner) {
	if !cfg.AutoDiscoverFeatures {
		return
	}

	// ... 既有的 parse, dedup, cap 邏輯不變 ...

	var enricher *enrich.Enricher
	if cfg.EnrichDiscoveredFeatures && r != nil {
		enricher = enrich.New(ws, r, cfg.EnrichAutoApprove)
	}

	var createdList []discoveredCreated
	var enrichFailed []protocol.DiscoveredFeature
	for _, d := range kept {
		next, nerr := feat.NextNumber(ws)
		if nerr != nil {
			slog.Warn("auto-discover: next feature number failed", "feature", feature.ID, "title", d.Title, "error", nerr)
			continue
		}
		id := feat.GenerateFeatureID(next, d.Title)

		var f feat.Feature
		if enricher != nil {
			result, err := enricher.Enrich(context.Background(), d)
			if err != nil {
				slog.Warn("auto-discover: enrichment error", "feature", feature.ID, "title", d.Title, "error", err)
				enrichFailed = append(enrichFailed, d)
				continue
			}
			if result.Discarded {
				slog.Info("auto-discover: enrichment discarded", "feature", feature.ID, "title", d.Title, "reason", result.Reason)
				enrichFailed = append(enrichFailed, d)
				continue
			}
			f = result.Feature
			f.ID = id
			f.Name = fmt.Sprintf("F%03d: %s", next, d.Title)
		} else {
			f = feat.Feature{
				ID:          id,
				Name:        fmt.Sprintf("F%03d: %s", next, d.Title),
				Description: d.Description,
				Status:      feat.StatusNotStarted,
			}
		}

		if serr := ws.SaveFeature(f); serr != nil {
			slog.Warn("auto-discover: save feature failed", "feature", feature.ID, "title", d.Title, "error", serr)
			continue
		}
		createdList = append(createdList, discoveredCreated{id: id, title: d.Title})
		ws.AppendEvent(feature.ID, protocol.Event{
			Type: "feature-discovered", Phase: protocol.PhaseDeepReviewing, Round: round, Detail: id,
		})
	}

	writeDiscoveredFeaturesReport(ws, feature.ID, createdList, skipped, capped, enrichFailed)
	fmt.Printf("[round %d] auto-discovered %d feature(s)\n", round, len(createdList))
}
```

4. 更新 `writeDiscoveredFeaturesReport` 加入 `enrichFailed` 參數：

```go
func writeDiscoveredFeaturesReport(ws *protocol.Workspace, featureID string, created []discoveredCreated, skipped, capped, enrichFailed []protocol.DiscoveredFeature) {
	// ... 既有區塊不變 ...

	b.WriteString("\n## Enrichment Failed (discarded)\n")
	if len(enrichFailed) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range enrichFailed {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	// ... 寫檔邏輯不變 ...
}
```

5. 更新所有 `autoDiscoverFeatures` 呼叫端（在 `runDeepReviewPhase` 中），加入 runner 參數。搜尋 `autoDiscoverFeatures(ws,` 找到所有呼叫點。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run "TestAutoDiscover_Enrich" -v`
Expected: PASS

- [ ] **Step 5: 跑現有測試確認無回歸**

Run: `go test -race ./cmd/4x/ -count=1`
Expected: PASS — 既有的 auto-discover 測試應該仍然通過（舊路徑不受影響）

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/run.go cmd/4x/run_f096_test.go
git commit -m "feat(F096): wire enricher into autoDiscoverFeatures flow"
```

---

### Task 6: 新增 `4x approve` 和 `4x reject` CLI 指令

**Files:**
- Create: `cmd/4x/approve.go`
- Test: `cmd/4x/approve_test.go` (new)
- Modify: `cmd/4x/main.go` (register commands)

**Interfaces:**
- Consumes: `feature.StatusDraft`, `feature.StatusNotStarted` from Task 1
- Consumes: `protocol.Workspace.SaveFeature()`, `protocol.Workspace.ListFeatures()`

- [ ] **Step 1: 寫測試**

建立 `cmd/4x/approve_test.go`：

```go
package main

import (
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
)

func TestApproveFeature_DraftToNotStarted(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	f := feat.Feature{ID: "F099-test-draft", Name: "F099: Test", Status: feat.StatusDraft}
	ws.SaveFeature(f)

	err := approveFeature(ws, "F099-test-draft")
	if err != nil {
		t.Fatalf("approveFeature() error = %v", err)
	}

	updated, err := ws.LoadFeature("F099-test-draft")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want %q", updated.Status, feat.StatusNotStarted)
	}
}

func TestApproveFeature_NonDraft_Error(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	f := feat.Feature{ID: "F099-test", Name: "F099: Test", Status: feat.StatusNotStarted}
	ws.SaveFeature(f)

	err := approveFeature(ws, "F099-test")
	if err == nil {
		t.Error("expected error for non-draft feature")
	}
}

func TestRejectFeature_DraftToAbandoned(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	f := feat.Feature{ID: "F099-test-draft", Name: "F099: Test", Status: feat.StatusDraft}
	ws.SaveFeature(f)

	err := rejectFeature(ws, "F099-test-draft")
	if err != nil {
		t.Fatalf("rejectFeature() error = %v", err)
	}

	updated, err := ws.LoadFeature("F099-test-draft")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != feat.StatusAbandoned {
		t.Errorf("status = %q, want %q", updated.Status, feat.StatusAbandoned)
	}
}

func TestRejectFeature_NonDraft_Error(t *testing.T) {
	ws, cleanup := setupTestWorkspace(t)
	defer cleanup()

	f := feat.Feature{ID: "F099-test", Name: "F099: Test", Status: feat.StatusNotStarted}
	ws.SaveFeature(f)

	err := rejectFeature(ws, "F099-test")
	if err == nil {
		t.Error("expected error for non-draft feature")
	}
}
```

注意：`ws.LoadFeature` 方法名可能是 `ReadFeature` 或其他名稱，實作時確認 `protocol.Workspace` 的實際方法簽名。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run "TestApproveFeature|TestRejectFeature" -v`
Expected: FAIL — `approveFeature`, `rejectFeature` undefined

- [ ] **Step 3: 實作 approve.go**

建立 `cmd/4x/approve.go`：

```go
package main

import (
	"fmt"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

var approveCmd = &cobra.Command{
	Use:   "approve <feature-id>",
	Short: "Approve a draft feature (draft → not-started)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, err := protocol.OpenWorkspace(".")
		if err != nil {
			return err
		}
		return approveFeature(ws, args[0])
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject <feature-id>",
	Short: "Reject a draft feature (draft → abandoned)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, err := protocol.OpenWorkspace(".")
		if err != nil {
			return err
		}
		return rejectFeature(ws, args[0])
	},
}

// approveFeature 將 draft feature 轉為 not-started。
func approveFeature(ws *protocol.Workspace, featureID string) error {
	f, err := loadFeatureByID(ws, featureID)
	if err != nil {
		return err
	}
	if f.Status != feat.StatusDraft {
		return fmt.Errorf("feature %s is %s, not draft", featureID, f.Status)
	}
	f.Status = feat.StatusNotStarted
	if err := ws.SaveFeature(f); err != nil {
		return err
	}
	fmt.Printf("approved: %s → not-started\n", featureID)
	return nil
}

// rejectFeature 將 draft feature 標記為 abandoned。
func rejectFeature(ws *protocol.Workspace, featureID string) error {
	f, err := loadFeatureByID(ws, featureID)
	if err != nil {
		return err
	}
	if f.Status != feat.StatusDraft {
		return fmt.Errorf("feature %s is %s, not draft", featureID, f.Status)
	}
	f.Status = feat.StatusAbandoned
	if err := ws.SaveFeature(f); err != nil {
		return err
	}
	fmt.Printf("rejected: %s → abandoned\n", featureID)
	return nil
}

// loadFeatureByID 從 workspace 載入單一 feature。
func loadFeatureByID(ws *protocol.Workspace, featureID string) (feat.Feature, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return feat.Feature{}, err
	}
	for _, f := range features {
		if f.ID == featureID {
			return f, nil
		}
	}
	return feat.Feature{}, fmt.Errorf("feature %s not found", featureID)
}
```

注意：如果 `protocol.Workspace` 已有 `LoadFeature(id)` 方法，直接用它取代 `loadFeatureByID`。實作時確認。

- [ ] **Step 4: 在 main.go 註冊指令**

修改 `cmd/4x/main.go`，在 `rootCmd.AddCommand(...)` 處加入：

```go
rootCmd.AddCommand(approveCmd)
rootCmd.AddCommand(rejectCmd)
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run "TestApproveFeature|TestRejectFeature" -v`
Expected: PASS

- [ ] **Step 6: 跑全量測試與 build**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/approve.go cmd/4x/approve_test.go cmd/4x/main.go
git commit -m "feat(F096): add 4x approve and 4x reject commands for draft features"
```

---

### Task 7: 文件同步與最終驗證

**Files:**
- Possibly modify: docs files (based on check-docs-sync output)
- Possibly modify: locale files (based on check-i18n output)

**Interfaces:**
- Consumes: all previous tasks

- [ ] **Step 1: 跑 check-docs-sync**

Run: `make check-docs-sync`

如果輸出 `NEEDS_UPDATE` → 只更新被點名的 doc 檔。如果 OK → 不需動作。

- [ ] **Step 2: 跑 check-i18n**

Run: `make check-i18n`

如果輸出 `ERROR: missing keys` → 補齊缺漏的 key。如果 OK → 不需動作。

- [ ] **Step 3: 跑完整驗證**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: 全部 PASS

- [ ] **Step 4: 如有文件更新，Commit**

```bash
git add -A
git commit -m "docs(F096): sync docs and i18n for enrichment feature"
```
{% endraw %}
