# F052: Feature Creation Logic Unification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 統一 feature 建立邏輯到 `internal/feature/` package，讓 CLI 和 Dashboard 共用同一個 `Create()` 函式，並擴充 Dashboard 表單支援 priority/depends/subtasks。

**Architecture:** 新建 `internal/feature/` package，定義 `Store` interface 解耦 `protocol.Workspace`。將 `Feature`/`Subtask` 等型別及 ID 產生、backlog 比對函式從 `protocol` 搬過來。CLI 和 Server 改呼叫 `feature.Create()`。Dashboard 表單擴充為漸進式設計。

**Tech Stack:** Go 1.26+, Cobra CLI, vanilla JS (dashboard)

---

## File Structure

**建立：**
- `internal/feature/types.go` — Feature, Subtask, BacklogMirror, BacklogFeature, BacklogDriftKind, BacklogDrift, Screenshot, ScreenshotGroup, DefaultScreenshotDir
- `internal/feature/store.go` — Store interface
- `internal/feature/id.go` — GenerateFeatureID, GenerateFeatureIDFromSlug, NextNumber
- `internal/feature/create.go` — Create(store, opts), CreateOpts
- `internal/feature/backlog.go` — CompareBacklogMirror standalone, appendFieldDrift, appendPriorityDrift
- `internal/feature/screenshot.go` — IsScreenshotFile, NormalizeScreenshotPath, SortScreenshots, parseScreenshotFilename, parseStepNumber
- `internal/feature/id_test.go` — ID 產生測試
- `internal/feature/create_test.go` — Create 函式測試（mock Store）

**刪除：**
- `internal/protocol/feature.go` — 內容全移至 `feature/id.go`
- `internal/protocol/feature_test.go` — 內容全移至 `feature/id_test.go`

**修改：**
- `internal/protocol/types.go` — 移除 Feature, Subtask, Backlog*, Screenshot 型別
- `internal/protocol/workspace.go` — 移除 CompareBacklogMirror standalone + drift helpers + screenshot pure helpers；Workspace methods 改用 feature 型別
- `cmd/4x/new.go` — 改用 feature.Create()
- `cmd/4x/run.go` — import 改 feature.Feature
- `cmd/4x/batch.go` — import 改 feature.Feature
- `cmd/4x/status.go` — import 改 feature.Feature
- `cmd/4x/prompt.go` — import 改 feature.Feature
- `cmd/4x/init.go` — import 改 feature.DefaultScreenshotDir
- `internal/server/server.go` — 改用 feature.Create()，擴充 newRequest
- `internal/batch/group.go` — import 改 feature.Feature
- `internal/batch/group_test.go` — import 改
- `internal/guard/check_test.go` — import 改
- `internal/server/server_test.go` — import 改
- `internal/server/process_test.go` — import 改
- `internal/server/multi_test.go` — import 改
- `cmd/4x/run_loop_test.go` — import 改
- `internal/server/static/index.html` — 擴充 New Feature modal
- `internal/server/static/ui.js` — 擴充 submitNewFeature
- `internal/server/static/locales/*.json` — 新增 i18n keys

---

### Task 1: 建立 `internal/feature/types.go`

**Files:**
- Create: `internal/feature/types.go`

- [ ] **Step 1: 建立 types.go**

```go
package feature

// Status 表示 feature 的狀態，對應 Phase 但面向 dashboard 與人類可讀
type Status string

const (
	StatusNotStarted     Status = "not-started"
	StatusInProgress     Status = "in-progress"
	StatusDone           Status = "done"
	StatusBlocked        Status = "blocked"
	StatusNeedsAttention Status = "needs-attention"
	StatusReadyForReview Status = "ready-for-review"
)

// Feature 是 features/*.yaml 的結構
type Feature struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Status      Status            `yaml:"status" json:"status"`
	Priority    *int              `yaml:"priority,omitempty" json:"priority,omitempty"`
	Repos       map[string]string `yaml:"repos,omitempty" json:"repos,omitempty"`
	Subtasks    []Subtask         `yaml:"subtasks,omitempty" json:"subtasks,omitempty"`
	Rules       []string          `yaml:"rules,omitempty" json:"rules,omitempty"`
	Depends     []string          `yaml:"depends,omitempty" json:"depends,omitempty"`
	Spec        string            `yaml:"spec,omitempty" json:"-"`
	Plan        string            `yaml:"plan,omitempty" json:"-"`
}

// Subtask 是 feature 內的子任務
type Subtask struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Status      string   `yaml:"status" json:"status"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Depends     []string `yaml:"depends,omitempty" json:"depends,omitempty"`
}

// BacklogMirror 是根目錄 feature_list.json 的 legacy mirror 結構。
type BacklogMirror struct {
	Version  int              `json:"version"`
	Features []BacklogFeature `json:"features"`
}

// BacklogFeature 表示 feature_list.json 中單一 legacy backlog entry。
type BacklogFeature struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Area        string `json:"area,omitempty"`
	Description string `json:"description,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
}

// BacklogDriftKind 表示 feature_list.json 與 .4x/features/*.yaml 的差異類型。
type BacklogDriftKind string

const (
	BacklogDriftMissing  BacklogDriftKind = "missing"
	BacklogDriftExtra    BacklogDriftKind = "extra"
	BacklogDriftMismatch BacklogDriftKind = "mismatch"
)

// BacklogDrift 表示一筆 feature_list.json legacy mirror 漂移結果。
type BacklogDrift struct {
	Kind      BacklogDriftKind `json:"kind"`
	FeatureID string           `json:"featureId"`
	Field     string           `json:"field,omitempty"`
	Canonical string           `json:"canonical,omitempty"`
	Mirror    string           `json:"mirror,omitempty"`
	Message   string           `json:"message"`
}

// Screenshot 是 tester 在 verify.json 記錄的截圖 metadata。
type Screenshot struct {
	Path        string `json:"path"`
	Step        string `json:"step"`
	Description string `json:"description"`
}

// ScreenshotGroup 表示同一 round 的截圖清單。
type ScreenshotGroup struct {
	Round       int          `json:"round"`
	Screenshots []Screenshot `json:"screenshots"`
}

// DefaultScreenshotDir 是 tester 預設截圖目錄，可用 {feature-id}、{round} 變數。
const DefaultScreenshotDir = ".4x/e2e/{feature-id}/screenshot/"
```

`Status` 型別和常量也一併搬到 feature package，保持 `Feature.Status` 為 named type。`protocol` 中的 `PhaseToStatus` 改為回傳 `feature.Status`。

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/feature/`
Expected: PASS（僅型別定義，無外部依賴）

- [ ] **Step 3: Commit**

```bash
git add internal/feature/types.go
git commit -m "feat(F052): add internal/feature/types.go with Feature, Subtask, Backlog, Screenshot types"
```

---

### Task 2: 建立 `internal/feature/store.go`

**Files:**
- Create: `internal/feature/store.go`

- [ ] **Step 1: 建立 store.go**

```go
package feature

// Store 抽象化 feature 的持久化操作，由 protocol.Workspace 隱式實作。
type Store interface {
	DotDir() string
	FeatureDir(featureID string) string
	RoundDir(featureID string, round int) string
	SaveFeature(f Feature) error
	LoadFeature(id string) (Feature, error)
	ListFeatures() ([]Feature, error)
	InitFeatureDir(featureID string) error
	ResolveFeatureID(prefix string) (string, error)
}
```

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/feature/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/feature/store.go
git commit -m "feat(F052): add feature.Store interface"
```

---

### Task 3: 建立 `internal/feature/id.go` + 測試

**Files:**
- Create: `internal/feature/id.go`
- Create: `internal/feature/id_test.go`

- [ ] **Step 1: 寫 id_test.go（測試先行）**

從 `internal/protocol/feature_test.go` 搬過來，package 改為 `feature`，函式名不變：

```go
package feature

import "testing"

func TestGenerateFeatureID(t *testing.T) {
	tests := []struct {
		num  int
		name string
		want string
	}{
		{1, "My Feature", "F001-my-feature"},
		{25, "Server Write API", "F025-server-write-api"},
		{100, "A very long feature name that exceeds the limit", "F100-a-very-long-feature"},
		{1000, "Four digit feature", "F1000-four-digit-feature"},
		{99999, "Five digit feature", "F99999-five-digit-feature"},
		{40, "Dashboard SPA file split — separate HTML, JS, CSS for maintainability", "F040-dashboard-spa-file"},
		{1, "single", "F001-single"},
		{1, "abcdefghijklmnopqrstuvwxyz", "F001-abcdefghijklmnopqrstuvw"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
		}
	}
}

func TestGenerateFeatureIDFromSlug(t *testing.T) {
	tests := []struct {
		num  int
		slug string
		want string
	}{
		{1, "my-custom-id", "F001-my-custom-id"},
		{40, "dashboard-spa-split", "F040-dashboard-spa-split"},
		{5, "A Very Long Custom Slug That Should Not Be Truncated", "F005-a-very-long-custom-slug-that-should-not-be-truncated"},
		{10, "UPPER--CASE", "F010-upper-case"},
		{43, "F043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
		{43, "f043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
	}
	for _, tt := range tests {
		got := GenerateFeatureIDFromSlug(tt.num, tt.slug)
		if got != tt.want {
			t.Errorf("GenerateFeatureIDFromSlug(%d, %q) = %q, want %q", tt.num, tt.slug, got, tt.want)
		}
	}
}

func TestNextNumber(t *testing.T) {
	store := &mockStore{features: map[string]Feature{}}

	n, err := NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	store.features["F003-test"] = Feature{ID: "F003-test", Name: "test"}
	n, err = NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("got %d, want 4", n)
	}

	store.features["F1000-four-digit"] = Feature{ID: "F1000-four-digit", Name: "four-digit"}
	n, err = NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1001 {
		t.Errorf("got %d, want 1001", n)
	}
}
```

注意：`mockStore` 會在 Task 4 的 `create_test.go` 建立，但 Go 同 package 的 test 檔可以互相看到。為避免編譯錯誤，先在 `id_test.go` 底部加入最小的 mock：

```go
type mockStore struct {
	features map[string]Feature
	dirs     []string
}

func (m *mockStore) DotDir() string                            { return "/tmp/test" }
func (m *mockStore) FeatureDir(id string) string               { return "/tmp/test/" + id }
func (m *mockStore) RoundDir(id string, round int) string      { return "" }
func (m *mockStore) SaveFeature(f Feature) error               { m.features[f.ID] = f; return nil }
func (m *mockStore) LoadFeature(id string) (Feature, error)    { f, ok := m.features[id]; if !ok { return Feature{}, fmt.Errorf("not found") }; return f, nil }
func (m *mockStore) ListFeatures() ([]Feature, error) {
	ff := make([]Feature, 0, len(m.features))
	for _, f := range m.features {
		ff = append(ff, f)
	}
	return ff, nil
}
func (m *mockStore) InitFeatureDir(id string) error            { m.dirs = append(m.dirs, id); return nil }
func (m *mockStore) ResolveFeatureID(prefix string) (string, error) { return prefix, nil }
```

需要在 import 加上 `"fmt"`。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/feature/ -run TestGenerateFeatureID -v`
Expected: FAIL — `GenerateFeatureID` 未定義

- [ ] **Step 3: 建立 id.go**

從 `internal/protocol/feature.go` 搬過來，package 改為 `feature`，`NextFeatureNumber` 改名為 `NextNumber`（接收 Store 而非 *Workspace）：

```go
package feature

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	nonAlphaNum            = regexp.MustCompile(`[^a-z0-9]+`)
	featureNumRe           = regexp.MustCompile(`^F(\d{3,})-`)
	featurePrefixRe        = regexp.MustCompile(`(?i)^F\d{3,}-`)
	maxFeatureIDSlugLength = 23
)

// NextNumber 掃描現有 feature，回傳下一個可用流水號。
func NextNumber(store Store) (int, error) {
	features, err := store.ListFeatures()
	if err != nil {
		return 1, nil
	}
	max := 0
	for _, f := range features {
		matches := featureNumRe.FindStringSubmatch(f.ID)
		if matches == nil {
			continue
		}
		n, err := strconv.Atoi(matches[1])
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1, nil
}

// GenerateFeatureID 產生 F{NNN}-{slug} 格式的 feature ID。
// 超過長度上限時在 word boundary（"-"）截斷，避免斷在字中間。
func GenerateFeatureID(num int, name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > maxFeatureIDSlugLength {
		truncated := slug[:maxFeatureIDSlugLength]
		if idx := strings.LastIndex(truncated, "-"); idx > 0 {
			slug = truncated[:idx]
		} else {
			slug = strings.TrimRight(truncated, "-")
		}
	}
	return fmt.Sprintf("F%03d-%s", num, slug)
}

// GenerateFeatureIDFromSlug 用使用者指定的 slug 產生 feature ID，不做截斷。
// 若 slug 已帶 F{NNN}- 前綴會自動去除，避免產生 F043-f043-... 的重複前綴。
func GenerateFeatureIDFromSlug(num int, slug string) string {
	if m := featurePrefixRe.FindStringIndex(slug); m != nil {
		slug = slug[m[1]:]
	}
	slug = strings.ToLower(slug)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return fmt.Sprintf("F%03d-%s", num, slug)
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/feature/ -v`
Expected: PASS — TestGenerateFeatureID, TestGenerateFeatureIDFromSlug, TestNextNumber 全通過

- [ ] **Step 5: Commit**

```bash
git add internal/feature/id.go internal/feature/id_test.go
git commit -m "feat(F052): add feature ID generation and NextNumber with tests"
```

---

### Task 4: 建立 `internal/feature/create.go` + 測試

**Files:**
- Create: `internal/feature/create.go`
- Create: `internal/feature/create_test.go`

- [ ] **Step 1: 寫 create_test.go（測試先行）**

```go
package feature

import (
	"testing"
)

func TestCreate_BasicFields(t *testing.T) {
	store := &mockStore{features: map[string]Feature{}}
	f, err := Create(store, CreateOpts{
		Name: "My Feature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F001-my-feature" {
		t.Errorf("ID = %q, want F001-my-feature", f.ID)
	}
	if f.Name != "My Feature" {
		t.Errorf("Name = %q, want 'My Feature'", f.Name)
	}
	if f.Description != "My Feature" {
		t.Errorf("Description should default to Name, got %q", f.Description)
	}
	if f.Status != StatusNotStarted {
		t.Errorf("Status = %q, want not-started", f.Status)
	}
	if _, ok := store.features[f.ID]; !ok {
		t.Error("feature not saved to store")
	}
	if len(store.dirs) != 1 || store.dirs[0] != f.ID {
		t.Errorf("InitFeatureDir not called, dirs = %v", store.dirs)
	}
}

func TestCreate_WithDescription(t *testing.T) {
	store := &mockStore{features: map[string]Feature{}}
	f, err := Create(store, CreateOpts{
		Name:        "Auth Refactor",
		Description: "Refactor auth middleware for compliance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Description != "Refactor auth middleware for compliance" {
		t.Errorf("Description = %q", f.Description)
	}
}

func TestCreate_WithCustomID(t *testing.T) {
	store := &mockStore{features: map[string]Feature{}}
	f, err := Create(store, CreateOpts{
		Name:     "Global Settings",
		CustomID: "global-settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F001-global-settings" {
		t.Errorf("ID = %q, want F001-global-settings", f.ID)
	}
}

func TestCreate_WithAllOpts(t *testing.T) {
	store := &mockStore{features: map[string]Feature{
		"F010-existing": {ID: "F010-existing"},
	}}
	p := 2
	f, err := Create(store, CreateOpts{
		Name:        "Batch Mode",
		Description: "Add batch processing",
		Subtasks:    []Subtask{{ID: "sub-1", Name: "Subtask one"}},
		Rules:       []string{"spec: docs/batch-spec.md"},
		Depends:     []string{"F010-existing"},
		Priority:    &p,
		Repos:       map[string]string{"api": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F011-batch-mode" {
		t.Errorf("ID = %q, want F011-batch-mode (next after F010)", f.ID)
	}
	if len(f.Subtasks) != 1 || f.Subtasks[0].ID != "sub-1" {
		t.Errorf("Subtasks = %+v", f.Subtasks)
	}
	if len(f.Rules) != 1 {
		t.Errorf("Rules = %v", f.Rules)
	}
	if len(f.Depends) != 1 {
		t.Errorf("Depends = %v", f.Depends)
	}
	if f.Priority == nil || *f.Priority != 2 {
		t.Errorf("Priority = %v", f.Priority)
	}
	if f.Repos["api"] != "" {
		t.Errorf("Repos = %v", f.Repos)
	}
}

func TestCreate_SequentialNumbering(t *testing.T) {
	store := &mockStore{features: map[string]Feature{}}

	f1, _ := Create(store, CreateOpts{Name: "First"})
	if f1.ID != "F001-first" {
		t.Errorf("f1.ID = %q", f1.ID)
	}

	f2, _ := Create(store, CreateOpts{Name: "Second"})
	if f2.ID != "F002-second" {
		t.Errorf("f2.ID = %q", f2.ID)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/feature/ -run TestCreate -v`
Expected: FAIL — `Create` 未定義

- [ ] **Step 3: 建立 create.go**

```go
package feature

// CreateOpts 是建立 feature 的輸入參數
type CreateOpts struct {
	Name        string
	Description string
	CustomID    string
	Subtasks    []Subtask
	Rules       []string
	Depends     []string
	Priority    *int
	Repos       map[string]string
}

// Create 統一建立 feature 的邏輯，由 CLI 和 Server 共用。
func Create(store Store, opts CreateOpts) (Feature, error) {
	next, err := NextNumber(store)
	if err != nil {
		return Feature{}, err
	}

	var id string
	if opts.CustomID != "" {
		id = GenerateFeatureIDFromSlug(next, opts.CustomID)
	} else {
		id = GenerateFeatureID(next, opts.Name)
	}

	description := opts.Description
	if description == "" {
		description = opts.Name
	}

	f := Feature{
		ID:          id,
		Name:        opts.Name,
		Description: description,
		Status:      StatusNotStarted,
		Subtasks:    opts.Subtasks,
		Rules:       opts.Rules,
		Depends:     opts.Depends,
		Priority:    opts.Priority,
		Repos:       opts.Repos,
	}

	if err := store.SaveFeature(f); err != nil {
		return Feature{}, err
	}
	if err := store.InitFeatureDir(f.ID); err != nil {
		return Feature{}, err
	}

	return f, nil
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/feature/ -v`
Expected: PASS — 所有 TestCreate_* 和 TestGenerateFeatureID* 測試通過

- [ ] **Step 5: Commit**

```bash
git add internal/feature/create.go internal/feature/create_test.go
git commit -m "feat(F052): add unified feature.Create() with mock Store tests"
```

---

### Task 5: 建立 `internal/feature/backlog.go`

**Files:**
- Create: `internal/feature/backlog.go`

- [ ] **Step 1: 建立 backlog.go**

從 `internal/protocol/workspace.go` 搬入 `CompareBacklogMirror` standalone function 和 drift helpers：

```go
package feature

import (
	"fmt"
	"sort"
	"strconv"
)

// CompareBacklogMirror 比對 feature YAML 清單與 legacy backlog mirror，並以 feature ID 穩定排序。
func CompareBacklogMirror(features []Feature, backlogFile string, mirror BacklogMirror) []BacklogDrift {
	canonical := make(map[string]Feature, len(features))
	for _, f := range features {
		canonical[f.ID] = f
	}

	backlog := make(map[string]BacklogFeature, len(mirror.Features))
	for _, f := range mirror.Features {
		backlog[f.ID] = f
	}

	var drift []BacklogDrift
	for _, f := range features {
		entry, ok := backlog[f.ID]
		if !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftMissing,
				FeatureID: f.ID,
				Message:   fmt.Sprintf("%s missing entry for feature %q", backlogFile, f.ID),
			})
			continue
		}
		drift = appendFieldDrift(drift, backlogFile, f.ID, "name", f.Name, entry.Name)
		drift = appendFieldDrift(drift, backlogFile, f.ID, "description", f.Description, entry.Description)
		drift = appendFieldDrift(drift, backlogFile, f.ID, "status", f.Status, entry.Status)
		drift = appendPriorityDrift(drift, backlogFile, f, entry)
	}

	for _, entry := range mirror.Features {
		if _, ok := canonical[entry.ID]; !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftExtra,
				FeatureID: entry.ID,
				Message:   fmt.Sprintf("%s has extra entry %q", backlogFile, entry.ID),
			})
		}
	}

	sort.Slice(drift, func(i, j int) bool {
		if drift[i].FeatureID != drift[j].FeatureID {
			return drift[i].FeatureID < drift[j].FeatureID
		}
		if drift[i].Kind != drift[j].Kind {
			return drift[i].Kind < drift[j].Kind
		}
		return drift[i].Field < drift[j].Field
	})
	return drift
}

func appendFieldDrift(drift []BacklogDrift, backlogFile, featureID, field, canonical, mirror string) []BacklogDrift {
	if canonical == mirror {
		return drift
	}
	return append(drift, BacklogDrift{
		Kind:      BacklogDriftMismatch,
		FeatureID: featureID,
		Field:     field,
		Canonical: canonical,
		Mirror:    mirror,
		Message: fmt.Sprintf(
			"%s mismatch for feature %q field %q: canonical %q, mirror %q",
			backlogFile, featureID, field, canonical, mirror,
		),
	})
}

func appendPriorityDrift(drift []BacklogDrift, backlogFile string, f Feature, mirror BacklogFeature) []BacklogDrift {
	if f.Priority == nil && mirror.Priority == nil {
		return drift
	}
	var canonical string
	if f.Priority != nil {
		canonical = strconv.Itoa(*f.Priority)
	}
	if mirror.Priority == nil {
		if f.Priority == nil {
			return drift
		}
		return appendFieldDrift(drift, backlogFile, f.ID, "priority", canonical, "")
	}
	mirrorStr := strconv.Itoa(*mirror.Priority)
	if f.Priority == nil {
		return appendFieldDrift(drift, backlogFile, f.ID, "priority", "", mirrorStr)
	}
	return appendFieldDrift(drift, backlogFile, f.ID, "priority", canonical, mirrorStr)
}
```

注意：原始 `CompareBacklogMirror` 用硬編碼的 `BacklogFile` 常量（在 protocol 中定義）來組 message。搬到 feature 後改為接收 `backlogFile string` 參數，由呼叫端傳入 `protocol.BacklogFile`。

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/feature/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/feature/backlog.go
git commit -m "feat(F052): move CompareBacklogMirror and drift helpers to feature package"
```

---

### Task 6: 建立 `internal/feature/screenshot.go`

**Files:**
- Create: `internal/feature/screenshot.go`

- [ ] **Step 1: 建立 screenshot.go**

搬入純函式（不依賴 Workspace）：

```go
package feature

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// IsScreenshotFile 判斷檔名是否為支援的截圖格式（png/jpg/jpeg/webp）。
func IsScreenshotFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"
}

// NormalizeScreenshotPath 將截圖路徑正規化，去除前綴 ./、.4x/，清除 .. 分量，並 trim 空白。
func NormalizeScreenshotPath(path string) string {
	p := filepath.ToSlash(strings.TrimSpace(path))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, ".4x/")
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	return p
}

// SortScreenshots 按 step 數字排序截圖。
func SortScreenshots(items []Screenshot) {
	sort.Slice(items, func(i, j int) bool {
		leftN, leftOK := parseStepNumber(items[i].Step)
		rightN, rightOK := parseStepNumber(items[j].Step)
		if leftOK && rightOK && leftN != rightN {
			return leftN < rightN
		}
		if items[i].Step != items[j].Step {
			return items[i].Step < items[j].Step
		}
		return items[i].Path < items[j].Path
	})
}

// ParseScreenshotFilename 從截圖檔名解析 step 和 description。
func ParseScreenshotFilename(filename string) (string, string) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		return "", ""
	}
	idx := strings.Index(base, "-")
	if idx <= 0 {
		return "", strings.ReplaceAll(base, "-", " ")
	}
	step := base[:idx]
	desc := strings.TrimSpace(strings.ReplaceAll(base[idx+1:], "-", " "))
	return step, desc
}

func parseStepNumber(step string) (int, bool) {
	if step == "" {
		return 0, false
	}
	n, err := strconv.Atoi(step)
	return n, err == nil
}
```

注意：`sortScreenshots` 和 `parseScreenshotFilename` 改為 exported（`SortScreenshots`、`ParseScreenshotFilename`），因為 `protocol.Workspace.DiscoverScreenshots` 需要跨 package 呼叫它們。

- [ ] **Step 2: 確認編譯**

Run: `go build ./internal/feature/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/feature/screenshot.go
git commit -m "feat(F052): move screenshot helper functions to feature package"
```

---

### Task 7: 原子切換 — 更新 protocol 並遷移所有呼叫端

這是最大的一步：從 `protocol` 移除已搬遷的型別/函式，更新 `protocol/workspace.go` 改用 `feature` 型別，並更新所有呼叫端的 import。

**Files:**
- Delete: `internal/protocol/feature.go`
- Delete: `internal/protocol/feature_test.go`
- Modify: `internal/protocol/types.go` — 移除 Feature, Subtask, Backlog*, Screenshot 型別
- Modify: `internal/protocol/workspace.go` — import feature，method 改用 feature 型別，移除搬走的函式
- Modify: `cmd/4x/run.go`, `cmd/4x/batch.go`, `cmd/4x/status.go`, `cmd/4x/prompt.go`, `cmd/4x/init.go` — import 加 feature
- Modify: `internal/batch/group.go` — import 加 feature
- Modify: `internal/server/server.go` — import 加 feature
- Modify: `cmd/4x/run_loop_test.go`, `internal/batch/group_test.go`, `internal/guard/check_test.go`, `internal/server/server_test.go`, `internal/server/process_test.go`, `internal/server/multi_test.go` — import 加 feature

- [ ] **Step 1: 刪除 protocol/feature.go 和 protocol/feature_test.go**

```bash
rm internal/protocol/feature.go internal/protocol/feature_test.go
```

- [ ] **Step 2: 更新 protocol/types.go**

移除以下型別定義（保留 Phase, Role, Severity, State, Event, Baseline, VerifyEvidence, TestStrategy, ReviewIssue, ReviewResult, Escalation, Config 等所有非 feature 型別）：

刪除：`Status` type + constants (lines 44-54)、`PhaseToStatus` (57-72)、`Feature` struct (74-87)、`BacklogMirror` (89-93)、`BacklogFeature` (95-103)、`BacklogDriftKind` + constants (105-112)、`BacklogDrift` (114-122)、`Subtask` (124-131)、`Screenshot` (183-188)、`DefaultScreenshotDir` constant (290)。

`PhaseToStatus` 保留在 protocol，但改回傳 `feature.Status`：
```go
func PhaseToStatus(phase Phase) feature.Status {
    switch phase {
    case PhasePendingReview: return feature.StatusReadyForReview
    case PhaseDone:          return feature.StatusDone
    case PhaseBlocked:       return feature.StatusBlocked
    case PhaseNeedsAttention: return feature.StatusNeedsAttention
    case PhaseInit:          return feature.StatusNotStarted
    default:                 return feature.StatusInProgress
    }
}
```

`VerifyEvidence` struct 的 `Screenshots` 欄位型別改為 `feature.Screenshot`：在 import 加入 `"github.com/ggwhite/4x/internal/feature"`，欄位改為 `Screenshots []feature.Screenshot`。

- [ ] **Step 3: 更新 protocol/workspace.go**

1. import 加入 `"github.com/ggwhite/4x/internal/feature"`
2. 所有 method signature 中的 `Feature` 改為 `feature.Feature`，`Subtask` 改為 `feature.Subtask`，`BacklogMirror` 改為 `feature.BacklogMirror`，`BacklogDrift` 改為 `feature.BacklogDrift`，`Screenshot` 改為 `feature.Screenshot`，`ScreenshotGroup` 改為 `feature.ScreenshotGroup`
3. `SaveFeature(f Feature)` → `SaveFeature(f feature.Feature)`
4. `LoadFeature(id string) (Feature, error)` → `LoadFeature(id string) (feature.Feature, error)`
5. `ListFeatures() ([]Feature, error)` → `ListFeatures() ([]feature.Feature, error)`
6. `CompareBacklogMirror()` method：改呼叫 `feature.CompareBacklogMirror(features, BacklogFile, mirror)`
7. 移除 standalone `CompareBacklogMirror` function、`appendFieldDrift`、`appendPriorityDrift`
8. `DiscoverScreenshots`：回傳型別改 `[]feature.ScreenshotGroup`，內部的 `Screenshot` 改 `feature.Screenshot`，`sortScreenshots(shots)` 改 `feature.SortScreenshots(shots)`，`parseScreenshotFilename` 改 `feature.ParseScreenshotFilename`，`IsScreenshotFile` 改 `feature.IsScreenshotFile`，`NormalizeScreenshotPath` 改 `feature.NormalizeScreenshotPath`
9. 移除 `sortScreenshots`、`parseScreenshotFilename`、`parseStepNumber`、`IsScreenshotFile`、`NormalizeScreenshotPath` 函式
10. `ScreenshotDir(cfg Config)` 改回傳 `feature.DefaultScreenshotDir`：`return feature.DefaultScreenshotDir`
11. `ScreenshotGroup` type 移除（已在 feature/types.go）

- [ ] **Step 4: 更新所有非 test 呼叫端 import**

對以下檔案，加入 `"github.com/ggwhite/4x/internal/feature"` import，並替換型別引用：

| 檔案 | 替換 |
|---|---|
| `cmd/4x/run.go` | `protocol.Feature` → `feature.Feature` |
| `cmd/4x/batch.go` | `[]protocol.Feature` → `[]feature.Feature`，`protocol.Status` → `feature.Status`，`protocol.StatusDone` → `feature.StatusDone` 等 |
| `cmd/4x/status.go` | `protocol.Feature` → `feature.Feature`，`protocol.Status*` → `feature.Status*` |
| `cmd/4x/prompt.go` | `Feature protocol.Feature` → `Feature feature.Feature` |
| `cmd/4x/init.go` | `protocol.DefaultScreenshotDir` → `feature.DefaultScreenshotDir` |
| `internal/batch/group.go` | `protocol.Feature` → `feature.Feature` |
| `internal/server/server.go` | `protocol.Subtask` → `feature.Subtask`，`protocol.Feature` → `feature.Feature`，`protocol.NextFeatureNumber` → `feature.NextNumber`，`protocol.GenerateFeatureID` → `feature.GenerateFeatureID`，`protocol.DefaultScreenshotDir` → `feature.DefaultScreenshotDir` |

- [ ] **Step 5: 更新所有 test 檔 import**

| 檔案 | 替換 |
|---|---|
| `cmd/4x/run_loop_test.go` | `protocol.Feature` → `feature.Feature` |
| `internal/batch/group_test.go` | `protocol.Feature` → `feature.Feature` |
| `internal/guard/check_test.go` | `protocol.Feature` → `feature.Feature` |
| `internal/server/server_test.go` | `protocol.Feature` → `feature.Feature`，`protocol.Screenshot` → `feature.Screenshot` |
| `internal/server/process_test.go` | `protocol.Feature` → `feature.Feature` |
| `internal/server/multi_test.go` | `protocol.Feature` → `feature.Feature` |

- [ ] **Step 6: 編譯確認**

Run: `go build ./cmd/4x && go vet ./...`
Expected: PASS — 無編譯錯誤

- [ ] **Step 7: 全部測試通過**

Run: `go test ./...`
Expected: PASS — 所有 package 測試通過

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(F052): move feature types and functions from protocol to internal/feature"
```

---

### Task 8: CLI `new.go` 改用 `feature.Create()`

**Files:**
- Modify: `cmd/4x/new.go`

- [ ] **Step 1: 重構 new.go 的 RunE**

將 lines 52-112 的建立邏輯替換為 `feature.Create()` 呼叫。`parseSubtask` 的回傳型別也改為 `feature.Subtask`：

```go
// RunE 內，替換整段建立邏輯為：
name := args[0]

var parsedSubtasks []feature.Subtask
for _, s := range subtasks {
    st, err := parseSubtask(s)
    if err != nil {
        if jsonOutput {
            return jsonError(err.Error())
        }
        return err
    }
    parsedSubtasks = append(parsedSubtasks, st)
}

repoMap := make(map[string]string)
for _, r := range repos {
    repoMap[r] = ""
}

opts := feature.CreateOpts{
    Name:        name,
    Description: desc,
    CustomID:    customID,
    Subtasks:    parsedSubtasks,
    Rules:       rules,
    Depends:     depends,
    Repos:       repoMap,
}
if cmd.Flags().Changed("priority") {
    opts.Priority = &priority
}

f, err := feature.Create(ws, opts)
if err != nil {
    if jsonOutput {
        return jsonError(err.Error())
    }
    return err
}

if jsonOutput {
    result := struct {
        FeatureID string `json:"featureId"`
        Name      string `json:"name"`
        Path      string `json:"path"`
    }{
        FeatureID: f.ID,
        Name:      f.Name,
        Path:      fmt.Sprintf(".4x/features/%s.yaml", f.ID),
    }
    data, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(data))
    return nil
}

fmt.Printf("Created feature: %s (%s)\n", f.ID, name)
fmt.Printf("  File: .4x/features/%s.yaml\n", f.ID)
if len(parsedSubtasks) > 0 {
    fmt.Printf("  Subtasks: %d\n", len(parsedSubtasks))
}
fmt.Println()
fmt.Printf("Run: 4x run %s\n", f.ID)
return nil
```

注意：原本 CLI 會用 `fmt.Sprintf("F%03d: %s", next, name)` 產生 `displayName`（如 "F052: My Feature"）作為 `feature.Name`。`feature.Create()` 不做這個轉換——它直接用 `opts.Name`。所以 CLI 端要在呼叫前自己組 displayName：

```go
next, err := feature.NextNumber(ws)
if err != nil { ... }
opts.Name = fmt.Sprintf("F%03d: %s", next, name)
```

但這又要呼叫 `NextNumber` 兩次（Create 內部也會呼叫）。更好的做法：讓 CLI 繼續傳原始 name，Create 不負責 displayName 格式化。檢查 feature YAML 裡的 Name 欄位實際上存的是什麼格式——

原始 CLI 存的是 `displayName`（如 "F052: My Feature"）。但 `feature.Create()` 存的是 `opts.Name` 原樣。為維持向下相容，CLI 端應在傳入 Create 前先格式化：

```go
next, _ := feature.NextNumber(ws)
displayName := fmt.Sprintf("F%03d: %s", next, name)
opts := feature.CreateOpts{
    Name: displayName,
    ...
}
```

這意味著 CLI 仍需呼叫 `NextNumber`。但 `Create()` 內部也會呼叫 `NextNumber`，可能拿到不同號碼（如果在兩次呼叫之間有其他 feature 被建立）。解法：讓 CLI 不自己叫 `NextNumber`，而是在 `Create()` 回傳後才用 `f.ID` 取號碼來輸出。或者在 `CreateOpts` 加一個 `DisplayNameFormat` 讓 Create 處理。

最簡單的做法：`CreateOpts` 加 `NamePrefix bool`。如果為 true，Create 自動在 Name 前加 `F{NNN}: ` 前綴。不加 — Server 建立時不需要前綴（dashboard 存的是原始名稱）。

等等，讓我看一下 server 目前存的 Name 格式 — server 的 handlePostNew 存的是 `req.Name`（原始名稱，沒有前綴）。CLI 存的是 `displayName`（有前綴）。兩邊行為不同！

這是一個需要統一的地方。建議：統一用原始 name，不加前綴。dashboard 顯示時自己從 ID 取號碼。不過這會改變既有 feature 的 Name 格式——可能不適合在 F052 處理。

**決策：維持現狀差異。** CLI 端在呼叫 Create 前先格式化 Name，Server 端傳原始 name。Create 不管 Name 格式，只負責儲存。

```go
// CLI 端的處理：
displayName := fmt.Sprintf("F%03d: %s", next, name)
// 但 next 要從哪來？Create 內部也會呼叫 NextNumber...
```

更好的做法：**不在 CLI 格式化 Name**。改成 Create 後用回傳的 Feature 取 ID 的 prefix 來顯示。或者接受 Name 不再帶前綴。

看一下 dashboard 如何顯示 Name — 如果 dashboard 直接顯示 feature.Name，那改掉格式會影響既有顯示。

為安全起見，**保持 CLI 的行為不變**：CLI 自己呼叫 `feature.NextNumber()` 組 displayName 再傳入 `CreateOpts.Name`。雖然 Create 內部也呼叫 NextNumber，但兩次呼叫之間不會有競爭（CLI 是單 process），拿到同一個號碼。

- [ ] **Step 2: 更新 parseSubtask**

```go
func parseSubtask(s string) (feature.Subtask, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return feature.Subtask{}, fmt.Errorf("subtask format must be \"id:name\" or \"id:name:description\", got %q", s)
	}
	st := feature.Subtask{
		ID:   strings.TrimSpace(parts[0]),
		Name: strings.TrimSpace(parts[1]),
	}
	if len(parts) == 3 {
		st.Description = strings.TrimSpace(parts[2])
	}
	return st, nil
}
```

- [ ] **Step 3: 編譯 + 測試**

Run: `go build ./cmd/4x && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/new.go
git commit -m "refactor(F052): CLI new.go uses feature.Create()"
```

---

### Task 9: Server `handlePostNew` 改用 `feature.Create()` 並擴充 request

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: 擴充 newRequest struct**

```go
type newRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	CustomID    string            `json:"customId,omitempty"`
	Subtasks    []feature.Subtask `json:"subtasks,omitempty"`
	Rules       []string          `json:"rules,omitempty"`
	Depends     []string          `json:"depends,omitempty"`
	Priority    *int              `json:"priority,omitempty"`
}
```

- [ ] **Step 2: 重構 handlePostNew**

```go
func handlePostNew(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	var req newRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	f, err := feature.Create(ws, feature.CreateOpts{
		Name:        req.Name,
		Description: req.Description,
		CustomID:    req.CustomID,
		Subtasks:    req.Subtasks,
		Rules:       req.Rules,
		Depends:     req.Depends,
		Priority:    req.Priority,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newResponse{ID: f.ID, Name: req.Name})
}
```

- [ ] **Step 3: 編譯 + 測試**

Run: `go build ./cmd/4x && go test ./internal/server/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go
git commit -m "refactor(F052): server handlePostNew uses feature.Create(), expanded request"
```

---

### Task 10: Dashboard 表單擴充

**Files:**
- Modify: `internal/server/static/index.html`
- Modify: `internal/server/static/ui.js`
- Modify: `internal/server/static/locales/en.json`
- Modify: `internal/server/static/locales/zh-TW.json`
- Modify: `internal/server/static/locales/zh-CN.json`
- Modify: `internal/server/static/locales/ja.json`
- Modify: `internal/server/static/locales/ko.json`
- Modify: `internal/server/static/locales/es.json`

- [ ] **Step 1: 擴充 index.html — New Feature Modal**

在現有 Description textarea 和 spec hint 之間，加入 Priority select 和 Advanced 展開區：

```html
<!-- Priority（基本區） -->
<div>
  <label style="font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--text-3);display:block;margin-bottom:6px" data-i18n="field.priority">Priority</label>
  <select id="new-feat-priority" style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;box-sizing:border-box;appearance:auto">
    <option value="" data-i18n="newFeature.noPriority">No priority</option>
    <option value="0">P0 — Critical</option>
    <option value="1">P1 — High</option>
    <option value="2">P2 — Medium</option>
    <option value="3">P3 — Low</option>
  </select>
</div>

<!-- Advanced 展開 -->
<div>
  <button type="button" onclick="toggleAdvanced()" id="new-feat-adv-btn" style="background:none;border:none;color:var(--text-3);font-size:12px;cursor:pointer;padding:0;font-family:inherit">
    <span id="new-feat-adv-arrow">▶</span> <span data-i18n="newFeature.advanced">Advanced</span>
  </button>
  <div id="new-feat-advanced" style="display:none;margin-top:12px;display:flex;flex-direction:column;gap:12px" hidden>
    <div>
      <label style="font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--text-3);display:block;margin-bottom:6px" data-i18n="newFeature.customId">Custom ID</label>
      <input id="new-feat-custom-id" type="text" placeholder="Leave empty for auto-generated" data-i18n-placeholder="newFeature.customIdPlaceholder" autocomplete="off" style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;box-sizing:border-box">
    </div>
    <div>
      <label style="font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--text-3);display:block;margin-bottom:6px" data-i18n="newFeature.depends">Depends</label>
      <input id="new-feat-depends" type="text" placeholder="e.g. F001, F002" data-i18n-placeholder="newFeature.dependsPlaceholder" autocomplete="off" style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;box-sizing:border-box">
    </div>
    <div>
      <label style="font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--text-3);display:block;margin-bottom:6px" data-i18n="field.rules">Rules</label>
      <input id="new-feat-rules" type="text" placeholder="e.g. spec: docs/design/spec.md" data-i18n-placeholder="newFeature.rulesPlaceholder" autocomplete="off" style="width:100%;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none;box-sizing:border-box">
    </div>
    <div>
      <label style="font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:var(--text-3);display:block;margin-bottom:6px" data-i18n="newFeature.subtasks">Subtasks</label>
      <div id="new-feat-subtasks-list"></div>
      <button type="button" onclick="addSubtaskRow()" style="background:none;border:1px dashed var(--border);border-radius:6px;padding:6px 12px;color:var(--text-3);font-size:12px;cursor:pointer;width:100%;margin-top:4px" data-i18n="newFeature.addSubtask">+ Add Subtask</button>
    </div>
  </div>
</div>
```

- [ ] **Step 2: 更新 ui.js**

在 `openNewFeature` 加入新欄位重置，`submitNewFeature` 加入新欄位收集，新增 `toggleAdvanced` 和 `addSubtaskRow`/`removeSubtaskRow`：

```javascript
function openNewFeature() {
  document.getElementById('new-feat-name').value = '';
  document.getElementById('new-feat-desc').value = '';
  document.getElementById('new-feat-priority').value = '';
  document.getElementById('new-feat-custom-id').value = '';
  document.getElementById('new-feat-depends').value = '';
  document.getElementById('new-feat-rules').value = '';
  document.getElementById('new-feat-subtasks-list').innerHTML = '';
  const adv = document.getElementById('new-feat-advanced');
  adv.hidden = true;
  document.getElementById('new-feat-adv-arrow').textContent = '▶';
  document.getElementById('new-feature-modal').classList.add('open');
  setTimeout(() => document.getElementById('new-feat-name').focus(), 100);
}

function toggleAdvanced() {
  const el = document.getElementById('new-feat-advanced');
  const arrow = document.getElementById('new-feat-adv-arrow');
  el.hidden = !el.hidden;
  arrow.textContent = el.hidden ? '▶' : '▼';
}

function addSubtaskRow() {
  const list = document.getElementById('new-feat-subtasks-list');
  const row = document.createElement('div');
  row.style.cssText = 'display:flex;gap:6px;margin-bottom:4px;align-items:center';
  row.innerHTML = `
    <input type="text" placeholder="id" style="width:80px;background:var(--bg-input);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box" class="st-id">
    <input type="text" placeholder="name" style="flex:1;background:var(--bg-input);border:1px solid var(--border);border-radius:6px;padding:6px 8px;color:var(--text-1);font-size:12px;font-family:inherit;outline:none;box-sizing:border-box" class="st-name">
    <button type="button" onclick="this.parentElement.remove()" style="background:none;border:none;color:var(--text-3);cursor:pointer;font-size:14px;padding:2px 6px">✕</button>
  `;
  list.appendChild(row);
}

async function submitNewFeature(andRun) {
  const name = document.getElementById('new-feat-name').value.trim();
  if (!name) return;
  const description = document.getElementById('new-feat-desc').value.trim();
  const priorityVal = document.getElementById('new-feat-priority').value;
  const customId = document.getElementById('new-feat-custom-id').value.trim();
  const dependsRaw = document.getElementById('new-feat-depends').value.trim();
  const rulesRaw = document.getElementById('new-feat-rules').value.trim();

  const body = { name, description };
  if (priorityVal !== '') body.priority = parseInt(priorityVal, 10);
  if (customId) body.customId = customId;
  if (dependsRaw) body.depends = dependsRaw.split(',').map(s => s.trim()).filter(Boolean);
  if (rulesRaw) body.rules = rulesRaw.split(',').map(s => s.trim()).filter(Boolean);

  const subtaskRows = document.querySelectorAll('#new-feat-subtasks-list > div');
  if (subtaskRows.length > 0) {
    const subtasks = [];
    subtaskRows.forEach(row => {
      const id = row.querySelector('.st-id').value.trim();
      const stName = row.querySelector('.st-name').value.trim();
      if (id && stName) subtasks.push({ id, name: stName, status: 'pending' });
    });
    if (subtasks.length > 0) body.subtasks = subtasks;
  }

  closeNewFeature();
  const res = await fetch(apiBase()+'/api/new', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
  if (!res.ok) { showToast(t('toast.createFailed').replace('{error}', await res.text())); return; }
  const data = await res.json();
  await load();
  if (andRun && data.id) openRunModal(data.id);
}
```

- [ ] **Step 3: 新增 i18n keys**

在 `en.json` 新增：
```json
"field.priority": "Priority",
"newFeature.noPriority": "No priority",
"newFeature.advanced": "Advanced",
"newFeature.customId": "Custom ID",
"newFeature.customIdPlaceholder": "Leave empty for auto-generated",
"newFeature.depends": "Depends",
"newFeature.dependsPlaceholder": "e.g. F001, F002",
"newFeature.rulesPlaceholder": "e.g. spec: docs/design/spec.md",
"newFeature.subtasks": "Subtasks",
"newFeature.addSubtask": "+ Add Subtask"
```

在 `zh-TW.json` 新增：
```json
"field.priority": "優先級",
"newFeature.noPriority": "未設定",
"newFeature.advanced": "進階選項",
"newFeature.customId": "自訂 ID",
"newFeature.customIdPlaceholder": "留空自動產生",
"newFeature.depends": "依賴",
"newFeature.dependsPlaceholder": "例如 F001, F002",
"newFeature.rulesPlaceholder": "例如 spec: docs/design/spec.md",
"newFeature.subtasks": "子任務",
"newFeature.addSubtask": "+ 新增子任務"
```

其餘 locale 檔（zh-CN, ja, ko, es）同樣新增對應翻譯。

- [ ] **Step 4: 跑 i18n 檢查**

Run: `make check-i18n`
Expected: OK — 所有 locale key 同步

- [ ] **Step 5: 編譯 + 測試**

Run: `go build ./cmd/4x && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/static/index.html internal/server/static/ui.js internal/server/static/locales/
git commit -m "feat(F052): expand dashboard New Feature form with priority, depends, subtasks"
```

---

### Task 11: 文件同步與最終驗證

**Files:**
- Modify: docs（依 check-docs-sync 輸出）
- Modify: `progress.md`（如需要）

- [ ] **Step 1: 跑文件同步檢查**

Run: `make check-docs-sync`

依輸出更新需要同步的文件。

- [ ] **Step 2: 完整驗證**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: PASS — 所有 build、lint、test 通過

- [ ] **Step 3: 更新 feature YAML**

```bash
bin/4x transition F052 --to done
```

或留給使用者在 dashboard 上手動操作。

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "docs(F052): sync documentation after feature creation logic unification"
```
