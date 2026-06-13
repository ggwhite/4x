# F054: Design doc resolution unification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將 server 和 prompt 兩處的設計文件解析邏輯統一為 `protocol.ResolveDesignDoc()`。

**Architecture:** 在 `internal/protocol/` 新增 `design_doc.go`，實作帶優先序的解析函式。Server 和 prompt 的舊實作刪除，改呼叫統一函式。

**Tech Stack:** Go 標準庫

---

### Task 1: ResolveDesignDoc 實作

**Files:**
- Create: `internal/protocol/design_doc.go`
- Create: `internal/protocol/design_doc_test.go`

- [ ] **Step 1: 寫 failing test — YAML path 優先**

```go
package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDesignDoc_YAMLPathFirst(t *testing.T) {
	root := t.TempDir()
	yamlSpecPath := "custom/my-spec.md"
	abs := filepath.Join(root, yamlSpecPath)
	os.MkdirAll(filepath.Dir(abs), 0o755)
	os.WriteFile(abs, []byte("yaml spec content"), 0o644)

	// 也放一份到 docs/design/ 確認 YAML 優先
	docsPath := filepath.Join(root, "docs", "design", "F054-test-spec.md")
	os.MkdirAll(filepath.Dir(docsPath), 0o755)
	os.WriteFile(docsPath, []byte("docs spec content"), 0o644)

	f := Feature{ID: "F054-test", Spec: yamlSpecPath}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "yaml spec content" {
		t.Errorf("Content = %q, want yaml spec content", doc.Content)
	}
	if doc.Source != yamlSpecPath {
		t.Errorf("Source = %q, want %q", doc.Source, yamlSpecPath)
	}
}
```

- [ ] **Step 2: 寫 failing test — fallback 到 docs/design/{id}-{type}.md**

```go
func TestResolveDesignDoc_DocsDesignFallback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "design", "F054-test-spec.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("docs content"), 0o644)

	f := Feature{ID: "F054-test"}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "docs content" {
		t.Errorf("Content = %q, want docs content", doc.Content)
	}
	if doc.Source != "docs/design/F054-test-spec.md" {
		t.Errorf("Source = %q, want docs/design/F054-test-spec.md", doc.Source)
	}
}
```

- [ ] **Step 3: 寫 failing test — strip prefix fallback**

```go
func TestResolveDesignDoc_StripPrefixFallback(t *testing.T) {
	root := t.TempDir()
	// 只有 strip prefix 版本的檔案
	path := filepath.Join(root, "docs", "design", "test-feature-plan.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("stripped content"), 0o644)

	f := Feature{ID: "F054-test-feature"}
	doc := ResolveDesignDoc(root, f, "plan")

	if doc.Content != "stripped content" {
		t.Errorf("Content = %q, want stripped content", doc.Content)
	}
}
```

- [ ] **Step 4: 寫 failing test — 全部找不到回傳空**

```go
func TestResolveDesignDoc_NotFound(t *testing.T) {
	root := t.TempDir()
	f := Feature{ID: "F099-missing"}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "" {
		t.Errorf("Content should be empty, got %q", doc.Content)
	}
	if doc.Source != "" {
		t.Errorf("Source should be empty, got %q", doc.Source)
	}
}
```

- [ ] **Step 5: 寫 failing test — plan 用 feature.Plan 欄位**

```go
func TestResolveDesignDoc_PlanField(t *testing.T) {
	root := t.TempDir()
	planPath := "docs/design/F054-test-plan.md"
	abs := filepath.Join(root, planPath)
	os.MkdirAll(filepath.Dir(abs), 0o755)
	os.WriteFile(abs, []byte("plan content"), 0o644)

	f := Feature{ID: "F054-test", Plan: planPath}
	doc := ResolveDesignDoc(root, f, "plan")

	if doc.Content != "plan content" {
		t.Errorf("Content = %q, want plan content", doc.Content)
	}
	if doc.Source != planPath {
		t.Errorf("Source = %q, want %q", doc.Source, planPath)
	}
}
```

- [ ] **Step 6: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestResolveDesignDoc`
Expected: FAIL — `ResolveDesignDoc` 未定義

- [ ] **Step 7: 實作 design_doc.go**

```go
package protocol

import (
	"os"
	"path/filepath"
)

// DesignDoc 是設計文件（spec/plan）的解析結果
type DesignDoc struct {
	Content string
	Source  string
}

// ResolveDesignDoc 依優先序解析設計文件：
// 1. Feature YAML 的 spec/plan 欄位（當 path 讀取）
// 2. docs/design/{featureID}-{docType}.md
// 3. docs/design/{slug}-{docType}.md（strip FNNN- prefix）
func ResolveDesignDoc(root string, feature Feature, docType string) DesignDoc {
	yamlPath := ""
	switch docType {
	case "spec":
		yamlPath = feature.Spec
	case "plan":
		yamlPath = feature.Plan
	}

	if yamlPath != "" {
		abs := yamlPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, yamlPath)
		}
		if content, err := os.ReadFile(abs); err == nil {
			return DesignDoc{Content: string(content), Source: yamlPath}
		}
	}

	rel := filepath.Join("docs", "design", feature.ID+"-"+docType+".md")
	if content, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
		return DesignDoc{Content: string(content), Source: rel}
	}

	slug := stripFeaturePrefix(feature.ID)
	if slug != feature.ID {
		rel = filepath.Join("docs", "design", slug+"-"+docType+".md")
		if content, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
			return DesignDoc{Content: string(content), Source: rel}
		}
	}

	return DesignDoc{}
}

func stripFeaturePrefix(id string) string {
	if len(id) > 5 && id[0] == 'F' && id[4] == '-' {
		return id[5:]
	}
	return id
}
```

- [ ] **Step 8: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestResolveDesignDoc`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/protocol/design_doc.go internal/protocol/design_doc_test.go
git commit -m "feat(F054): add ResolveDesignDoc with unified priority resolution"
```

---

### Task 2: server.go 改用 ResolveDesignDoc

**Files:**
- Modify: `internal/server/server.go:377-378, 412-413, 1199-1217`

- [ ] **Step 1: 替換 handleTasks 裡的呼叫（第 377-380 行）**

找到：

```go
_, specSource := resolveDoc(ws.Root, f.Spec, f.ID, "spec")
_, planSource := resolveDoc(ws.Root, f.Plan, f.ID, "plan")
t.HasSpec = specSource != ""
t.HasPlan = planSource != ""
```

替換為：

```go
t.HasSpec = protocol.ResolveDesignDoc(ws.Root, f, "spec").Source != ""
t.HasPlan = protocol.ResolveDesignDoc(ws.Root, f, "plan").Source != ""
```

- [ ] **Step 2: 替換 handleOverview 裡的呼叫（第 412-413 行）**

找到：

```go
spec, specSource := resolveDoc(ws.Root, f.Spec, f.ID, "spec")
plan, planSource := resolveDoc(ws.Root, f.Plan, f.ID, "plan")
```

替換為：

```go
specDoc := protocol.ResolveDesignDoc(ws.Root, f, "spec")
planDoc := protocol.ResolveDesignDoc(ws.Root, f, "plan")
spec, specSource := specDoc.Content, specDoc.Source
plan, planSource := planDoc.Content, planDoc.Source
```

- [ ] **Step 3: 刪除舊的 resolveDoc 函式（第 1199-1217 行）**

刪除整個 `resolveDoc` 函式。

- [ ] **Step 4: 建置驗證**

Run: `go build ./... && go vet ./...`
Expected: 通過。如果 `resolveDoc` 還有其他呼叫端會報錯，逐一替換。

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go
git commit -m "refactor(F054): server uses protocol.ResolveDesignDoc"
```

---

### Task 3: prompt.go 改用 ResolveDesignDoc

**Files:**
- Modify: `cmd/4x/prompt.go:80, 125-166`

- [ ] **Step 1: 替換 loadPlanningDocs 的呼叫（第 80 行）**

找到：

```go
PlanningDoc:      loadPlanningDocs(ws.Root, feature.ID),
```

替換為：

```go
PlanningDoc:      loadPlanningDocs(ws.Root, feature),
```

- [ ] **Step 2: 用 ResolveDesignDoc 重寫 loadPlanningDocs**

刪除 `designDocPath`（第 125-140 行）、`stripFeaturePrefix`（第 142-148 行）、舊的 `loadPlanningDocs`（第 150-166 行）。

新增：

```go
func loadPlanningDocs(root string, feature protocol.Feature) string {
	var parts []string
	for _, docType := range []string{"spec", "plan"} {
		doc := protocol.ResolveDesignDoc(root, feature, docType)
		if doc.Content != "" {
			parts = append(parts, doc.Content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}
```

- [ ] **Step 3: 檢查 generatePrompt 裡的呼叫（run.go 第 309 行）**

`generatePrompt` 呼叫 `loadPlanningDocs`，確認簽名變更後 `run.go` 的呼叫也需要更新：

找到 `run.go` 中：

```go
PlanningDoc:      loadPlanningDocs(ws.Root, feature.ID),
```

替換為：

```go
PlanningDoc:      loadPlanningDocs(ws.Root, feature),
```

- [ ] **Step 4: 建置驗證**

Run: `go build ./... && go vet ./...`
Expected: 通過

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/prompt.go cmd/4x/run.go
git commit -m "refactor(F054): prompt uses protocol.ResolveDesignDoc"
```

---

### Task 4: 全量測試與文件

**Files:**
- 所有改動過的檔案

- [ ] **Step 1: 全量建置與測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部通過

- [ ] **Step 2: 跑 check-docs-sync**

Run: `make check-docs-sync`

- [ ] **Step 3: 依腳本輸出更新對應文件**

若 `NEEDS_UPDATE` 點名需要更新特定文件，更新之。否則跳過。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs(F054): update docs for design doc resolution unification"
```
