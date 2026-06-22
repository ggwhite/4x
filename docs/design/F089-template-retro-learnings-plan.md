# F089: Template Override + Retro Learnings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓每個專案可覆寫內建 role prompt template，並透過 retro learnings 迴路自動將開發經驗回饋到後續 feature 的 prompt 品質。

**Architecture:** 兩個獨立子系統。Template override 在 `loadRoleTemplate()` 加入專案目錄優先查找。Retro learnings 新增 `internal/learning` package 負責 CRUD，Acceptor template 擴充產出 learnings，CLI prompt 層注入 selected learnings 到各 role。

**Tech Stack:** Go 1.26+, Cobra CLI, `text/template`, `embed.FS`

## Global Constraints

- CLI 層嚴禁呼叫 LLM
- `internal/` package 不 export 給外部
- 每個 subcommand 一個檔案 `cmd/4x/{cmd}.go`
- learnings 失敗不阻擋 state transition（warn only）
- 改動後至少跑 `go build ./cmd/4x && go vet ./... && go test -race ./...`

---

### Task 1: Template Override — `loadRoleTemplate` 兩階段查找

**Files:**
- Modify: `cmd/4x/prompt.go:370-387` — `loadRoleTemplate` 函式
- Test: `cmd/4x/prompt_test.go` — 新增測試

**Interfaces:**
- Consumes: `templates.FS` (embed.FS), `protocol.Workspace.DotDir()` (string)
- Produces: `loadRoleTemplate(dotDir string, r protocol.Role) (*template.Template, error)` — 簽名新增 `dotDir` 參數

- [ ] **Step 1: 寫 failing test — 專案 template 覆寫內建**

```go
func TestLoadRoleTemplate_ProjectOverride(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	dotDir := ws.DotDir()

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customContent := `CUSTOM DESIGNER PROMPT for {{.Feature.ID}}`
	writeTestFileHelper(t, filepath.Join(tmplDir, "designer.md.tmpl"), customContent)

	tmpl, err := loadRoleTemplate(dotDir, protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, promptData{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  dotDir,
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if !strings.Contains(b.String(), "CUSTOM DESIGNER PROMPT for F089-test") {
		t.Errorf("expected project override content, got:\n%s", b.String())
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestLoadRoleTemplate_ProjectOverride -v`
Expected: FAIL — `loadRoleTemplate` 簽名不匹配（多了 `dotDir` 參數）

- [ ] **Step 3: 寫 failing test — 無專案 template 時 fallback 內建**

```go
func TestLoadRoleTemplate_FallbackBuiltin(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	dotDir := ws.DotDir()

	// 不建 .4x/templates/ 目錄，應 fallback 內建
	tmpl, err := loadRoleTemplate(dotDir, protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, promptData{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  dotDir,
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}
	// 內建 designer.md.tmpl 開頭是 "You are the Designer for feature"
	if !strings.Contains(b.String(), "You are the Designer for feature") {
		t.Errorf("expected builtin template content, got:\n%s", b.String())
	}
}
```

- [ ] **Step 4: 實作 `loadRoleTemplate` 兩階段查找**

修改 `cmd/4x/prompt.go`：

```go
func loadRoleTemplate(dotDir string, r protocol.Role) (*template.Template, error) {
	filename, ok := roleTemplateFiles[r]
	if !ok {
		return nil, fmt.Errorf("unknown role: %s", r)
	}

	locale := loadTemplateFile(dotDir, "locale.tmpl")
	role := loadTemplateFile(dotDir, filename)

	if locale == "" {
		data, err := templates.FS.ReadFile("locale.tmpl")
		if err != nil {
			return nil, fmt.Errorf("read locale template: %w", err)
		}
		locale = string(data)
	}
	if role == "" {
		data, err := templates.FS.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read role template %s: %w", filename, err)
		}
		role = string(data)
	}

	return template.New(string(r)).Funcs(tmplFuncs).Parse(locale + role)
}

// loadTemplateFile 嘗試從 .4x/templates/ 讀取 template 檔案，找不到回傳空字串。
func loadTemplateFile(dotDir, filename string) string {
	path := filepath.Join(dotDir, "templates", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
```

- [ ] **Step 5: 更新所有 `loadRoleTemplate` 呼叫處**

`cmd/4x/prompt.go` 的 `newPromptCmd` 內（約 line 85）：

```go
// 原本：tmpl, err := loadRoleTemplate(r)
// 改為：
tmpl, err := loadRoleTemplate(ws.DotDir(), r)
```

搜尋 `cmd/4x/` 內所有其他呼叫 `loadRoleTemplate` 的地方（`run.go` 的 `generatePrompt` 等），逐一加上 `dotDir` 參數。

Run: `grep -rn "loadRoleTemplate" cmd/4x/`

每個呼叫點改為傳入 `ws.DotDir()`（或對應的 dotDir 變數）。

- [ ] **Step 6: 更新既有 `TestLoadRoleTemplate_DesignReviewer` 測試**

既有測試呼叫 `loadRoleTemplate(protocol.RoleDesignReviewer)` 需改為 `loadRoleTemplate("", protocol.RoleDesignReviewer)`（空字串 dotDir 會 fallback 到內建）。

- [ ] **Step 7: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -v`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/4x/prompt.go cmd/4x/prompt_test.go cmd/4x/run.go cmd/4x/deep_review_prompt.go
git commit -m "feat(F089): template override — loadRoleTemplate with project-level fallback"
```

---

### Task 2: `4x init --dump-templates`

**Files:**
- Modify: `cmd/4x/init.go` — 新增 `--dump-templates` 和 `--force` flag
- Create: `cmd/4x/init_test.go` — dump-templates 測試

**Interfaces:**
- Consumes: `templates.FS` (embed.FS), `protocol.DirName` (constant)
- Produces: `dumpTemplates(dotDir string, force bool) error` — 倒出內建 template 到 `.4x/templates/`

- [ ] **Step 1: 寫 failing test — dump-templates 基本功能**

```go
func TestDumpTemplates_CreatesFiles(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	dotDir := filepath.Join(root, protocol.DirName)

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	// 檢查是否倒出了 designer.md.tmpl
	path := filepath.Join(dotDir, "templates", "designer.md.tmpl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected designer.md.tmpl to exist: %v", err)
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected designer template content")
	}

	// 檢查 locale.tmpl 也被倒出
	localePath := filepath.Join(dotDir, "templates", "locale.tmpl")
	if _, err := os.Stat(localePath); err != nil {
		t.Fatalf("expected locale.tmpl to exist: %v", err)
	}
}
```

- [ ] **Step 2: 寫 failing test — 不覆蓋已存在的檔案**

```go
func TestDumpTemplates_SkipsExisting(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644)

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) != "MY CUSTOM" {
		t.Errorf("expected existing file to be preserved, got %q", string(data))
	}
}
```

- [ ] **Step 3: 寫 failing test — `--force` 覆蓋**

```go
func TestDumpTemplates_ForceOverwrites(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644)

	if err := dumpTemplates(dotDir, true); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) == "MY CUSTOM" {
		t.Error("expected force to overwrite existing file")
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected builtin content after force overwrite")
	}
}
```

- [ ] **Step 4: 實作 `dumpTemplates`**

在 `cmd/4x/init.go` 新增：

```go
// dumpTemplates 把內建 template 倒出到 .4x/templates/，供專案覆寫。
// profiles/ 子目錄不倒出（test profile 有自己的覆寫機制）。
func dumpTemplates(dotDir string, force bool) error {
	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}

	entries, err := templates.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded templates: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(tmplDir, e.Name())
		if !force {
			if _, err := os.Stat(dest); err == nil {
				slog.Warn("template exists, skipping (use --force to overwrite)", "file", e.Name())
				continue
			}
		}
		data, err := templates.FS.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("  %s\n", e.Name())
	}
	return nil
}
```

- [ ] **Step 5: 在 `newInitCmd` 加 `--dump-templates` 和 `--force` flag**

改寫 `newInitCmd`，現在它是一個無 flag 的 command。需要把 `RunE` 內的邏輯拆出來，並新增 flag 分支：

```go
func newInitCmd() *cobra.Command {
	var dumpTmpl, force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a 4x project in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			if dumpTmpl {
				dotDir := filepath.Join(cwd, protocol.DirName)
				if _, err := os.Stat(dotDir); err != nil {
					return fmt.Errorf("%s not found — run '4x init' first", protocol.DirName)
				}
				fmt.Println("Dumping built-in templates to .4x/templates/:")
				return dumpTemplates(dotDir, force)
			}

			// 原有的 init 邏輯不變...
			dotDir := filepath.Join(cwd, protocol.DirName)
			if _, err := os.Stat(dotDir); err == nil {
				return fmt.Errorf("%s already exists", protocol.DirName)
			}
			// ... 其餘原有程式碼 ...
		},
	}

	cmd.Flags().BoolVar(&dumpTmpl, "dump-templates", false, "dump built-in templates to .4x/templates/ for customization")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing template files")
	return cmd
}
```

- [ ] **Step 6: 加 import `templates` package**

在 `cmd/4x/init.go` 的 import 加上：
```go
"github.com/ggwhite/4x/templates"
```

- [ ] **Step 7: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -run TestDumpTemplates -v`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/4x/init.go cmd/4x/init_test.go
git commit -m "feat(F089): 4x init --dump-templates to export built-in templates"
```

---

### Task 3: Learnings Store — `internal/learning` package

**Files:**
- Create: `internal/learning/store.go` — 型別定義、讀寫、去重、stale 更新
- Create: `internal/learning/store_test.go` — 單元測試

**Interfaces:**
- Consumes: 無外部依賴（純資料操作）
- Produces:
  - `type Category string` + 6 個常量
  - `type Status string` + 3 個常量
  - `type Entry struct { ... }`
  - `type Store struct { Version int; Entries []Entry }`
  - `type RetroLearning struct { Category Category; Content string }`
  - `func LoadStore(path string) (Store, error)`
  - `func (s *Store) Save(path string) error`
  - `func (s *Store) Harvest(featureID string, learnings []RetroLearning) int`
  - `func (s *Store) MarkStale(staleDays int)`
  - `func (s *Store) ActiveEntries() []Entry`
  - `func (s *Store) Promote(id string) error`
  - `func (s *Store) Remove(id string) error`
  - `func (s *Store) Prune() int`
  - `func (s *Store) UpdateUsage(ids []string)`
  - `func ValidCategories() []Category`
  - `func CategoriesForRole(role string) []Category`
  - `func ParseRetroFile(path string) ([]RetroLearning, error)`

- [ ] **Step 1: 寫型別定義和常量**

建立 `internal/learning/store.go`：

```go
package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Category 是 learning 的分類
type Category string

const (
	CategoryDesign      Category = "design"
	CategoryCodeQuality Category = "code-quality"
	CategoryTesting     Category = "testing"
	CategoryReview      Category = "review"
	CategoryTooling     Category = "tooling"
	CategoryProcess     Category = "process"
)

// ValidCategories 回傳所有合法的 category 列舉
func ValidCategories() []Category {
	return []Category{
		CategoryDesign, CategoryCodeQuality, CategoryTesting,
		CategoryReview, CategoryTooling, CategoryProcess,
	}
}

var validCategorySet = func() map[Category]bool {
	m := make(map[Category]bool, 6)
	for _, c := range ValidCategories() {
		m[c] = true
	}
	return m
}()

// IsValidCategory 檢查 category 是否在白名單中
func IsValidCategory(c Category) bool {
	return validCategorySet[c]
}

// Status 是 learning 的狀態
type Status string

const (
	StatusActive   Status = "active"
	StatusStale    Status = "stale"
	StatusPromoted Status = "promoted"
)

const (
	DefaultStaleDays   = 90
	MaxActiveEntries   = 100
	MaxSelectedPerRole = 10
)

// Entry 是 learnings.json 的一個條目
type Entry struct {
	ID            string   `json:"id"`
	SourceFeature string   `json:"source_feature"`
	Category      Category `json:"category"`
	Content       string   `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsed      time.Time `json:"last_used,omitempty"`
	UsedCount     int      `json:"used_count"`
	Status        Status   `json:"status"`
}

// Store 是 .4x/learnings.json 的完整結構
type Store struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// RetroLearning 是 Acceptor 產出的單一 learning（不含 ID 和 metadata）
type RetroLearning struct {
	Category Category `json:"category"`
	Content  string   `json:"content"`
}

// RetroFile 是 .4x/{feature-id}/retro-learnings.json 的結構
type RetroFile struct {
	Learnings []RetroLearning `json:"learnings"`
}
```

- [ ] **Step 2: 寫 failing test — LoadStore 空檔案**

建立 `internal/learning/store_test.go`：

```go
package learning

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStore_NotExist_ReturnsEmpty(t *testing.T) {
	s, err := LoadStore("/nonexistent/learnings.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("expected version 1, got %d", s.Version)
	}
	if len(s.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(s.Entries))
	}
}
```

- [ ] **Step 3: 實作 LoadStore 和 Save**

在 `store.go` 加上：

```go
// LoadStore 讀取 learnings.json；檔案不存在時回傳空 store（version=1）
func LoadStore(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1}, nil
		}
		return Store{}, fmt.Errorf("read learnings: %w", err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}, fmt.Errorf("parse learnings: %w", err)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s, nil
}

// Save 把 store 寫入 learnings.json（atomic: temp + rename）
func (s *Store) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal learnings: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "learnings-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
```

- [ ] **Step 4: 跑 LoadStore 測試確認通過**

Run: `go test -race ./internal/learning/ -run TestLoadStore -v`
Expected: PASS

- [ ] **Step 5: 寫 failing test — Harvest 新增 + 去重**

```go
func TestHarvest_AddsAndDeduplicates(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors"},
		{Category: CategoryTesting, Content: "test edge cases"},
		{Category: CategoryCodeQuality, Content: "always wrap errors"}, // 重複
	}
	added := s.Harvest("F042-test", learnings)
	if added != 2 {
		t.Errorf("expected 2 added, got %d", added)
	}
	if len(s.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.Entries))
	}
	if s.Entries[0].ID != "L001" || s.Entries[1].ID != "L002" {
		t.Errorf("unexpected IDs: %s, %s", s.Entries[0].ID, s.Entries[1].ID)
	}
	if s.Entries[0].SourceFeature != "F042-test" {
		t.Errorf("expected source_feature F042-test, got %s", s.Entries[0].SourceFeature)
	}
	if s.Entries[0].Status != StatusActive {
		t.Errorf("expected active status, got %s", s.Entries[0].Status)
	}
}

func TestHarvest_SkipsDuplicateWithExisting(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Content: "always wrap errors", Category: CategoryCodeQuality, Status: StatusActive},
	}}
	learnings := []RetroLearning{
		{Category: CategoryCodeQuality, Content: "always wrap errors"},
		{Category: CategoryTesting, Content: "new learning"},
	}
	added := s.Harvest("F043", learnings)
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
	if len(s.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.Entries))
	}
	if s.Entries[1].ID != "L002" {
		t.Errorf("expected L002, got %s", s.Entries[1].ID)
	}
}

func TestHarvest_SkipsInvalidCategory(t *testing.T) {
	s := Store{Version: 1}
	learnings := []RetroLearning{
		{Category: "invalid", Content: "should be skipped"},
		{Category: CategoryDesign, Content: "valid one"},
	}
	added := s.Harvest("F044", learnings)
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
}
```

- [ ] **Step 6: 實作 Harvest**

```go
// Harvest 把 Acceptor 產出的 learnings 追加到 store，回傳實際新增數量。
// 去重：content 完全相同即跳過。category 不在白名單或 content 空的也跳過。
func (s *Store) Harvest(featureID string, learnings []RetroLearning) int {
	existing := make(map[string]bool, len(s.Entries))
	for _, e := range s.Entries {
		existing[e.Content] = true
	}

	added := 0
	now := time.Now()
	for _, l := range learnings {
		if !IsValidCategory(l.Category) || l.Content == "" {
			continue
		}
		if existing[l.Content] {
			continue
		}
		existing[l.Content] = true
		s.Entries = append(s.Entries, Entry{
			ID:            s.nextID(),
			SourceFeature: featureID,
			Category:      l.Category,
			Content:       l.Content,
			CreatedAt:     now,
			Status:        StatusActive,
		})
		added++
	}
	return added
}

// nextID 產生下一個 L-序號 ID
func (s *Store) nextID() string {
	maxNum := 0
	for _, e := range s.Entries {
		var n int
		if _, err := fmt.Sscanf(e.ID, "L%d", &n); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return fmt.Sprintf("L%03d", maxNum+1)
}
```

- [ ] **Step 7: 跑 Harvest 測試確認通過**

Run: `go test -race ./internal/learning/ -run TestHarvest -v`
Expected: ALL PASS

- [ ] **Step 8: 寫 failing test — MarkStale 和 ActiveEntries**

```go
func TestMarkStale_MarksOldEntries(t *testing.T) {
	old := time.Now().Add(-91 * 24 * time.Hour)
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, CreatedAt: old, LastUsed: old},
		{ID: "L002", Status: StatusActive, CreatedAt: time.Now()},
		{ID: "L003", Status: StatusPromoted, CreatedAt: old, LastUsed: old},
	}}
	s.MarkStale(DefaultStaleDays)
	if s.Entries[0].Status != StatusStale {
		t.Errorf("L001 should be stale, got %s", s.Entries[0].Status)
	}
	if s.Entries[1].Status != StatusActive {
		t.Errorf("L002 should still be active, got %s", s.Entries[1].Status)
	}
	if s.Entries[2].Status != StatusPromoted {
		t.Errorf("L003 (promoted) should not be changed, got %s", s.Entries[2].Status)
	}
}

func TestActiveEntries(t *testing.T) {
	s := Store{Version: 1, Entries: []Entry{
		{ID: "L001", Status: StatusActive, Category: CategoryDesign, Content: "a"},
		{ID: "L002", Status: StatusStale, Category: CategoryDesign, Content: "b"},
		{ID: "L003", Status: StatusActive, Category: CategoryTesting, Content: "c"},
		{ID: "L004", Status: StatusPromoted, Category: CategoryDesign, Content: "d"},
	}}
	active := s.ActiveEntries()
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
	if active[0].ID != "L001" || active[1].ID != "L003" {
		t.Errorf("unexpected active IDs: %v", active)
	}
}
```

- [ ] **Step 9: 實作 MarkStale, ActiveEntries, Promote, Remove, Prune**

```go
// MarkStale 掃描所有 active 條目，超過 staleDays 天未使用的標記為 stale。
// 判斷依據：LastUsed 非零時用 LastUsed，否則用 CreatedAt。
func (s *Store) MarkStale(staleDays int) {
	cutoff := time.Now().Add(-time.Duration(staleDays) * 24 * time.Hour)
	for i := range s.Entries {
		if s.Entries[i].Status != StatusActive {
			continue
		}
		ref := s.Entries[i].LastUsed
		if ref.IsZero() {
			ref = s.Entries[i].CreatedAt
		}
		if ref.Before(cutoff) {
			s.Entries[i].Status = StatusStale
		}
	}
}

// ActiveEntries 回傳所有 status==active 的條目
func (s *Store) ActiveEntries() []Entry {
	var result []Entry
	for _, e := range s.Entries {
		if e.Status == StatusActive {
			result = append(result, e)
		}
	}
	return result
}

// Promote 將指定 ID 標記為 promoted
func (s *Store) Promote(id string) error {
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			s.Entries[i].Status = StatusPromoted
			return nil
		}
	}
	return fmt.Errorf("learning %s not found", id)
}

// Remove 移除指定 ID 的條目
func (s *Store) Remove(id string) error {
	for i, e := range s.Entries {
		if e.ID == id {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("learning %s not found", id)
}

// Prune 移除所有 stale 條目，回傳移除數量
func (s *Store) Prune() int {
	var kept []Entry
	removed := 0
	for _, e := range s.Entries {
		if e.Status == StatusStale {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	s.Entries = kept
	return removed
}

// UpdateUsage 更新指定 ID 的 last_used 和 used_count
func (s *Store) UpdateUsage(ids []string) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	now := time.Now()
	for i := range s.Entries {
		if idSet[s.Entries[i].ID] {
			s.Entries[i].LastUsed = now
			s.Entries[i].UsedCount++
		}
	}
}
```

- [ ] **Step 10: 實作 ParseRetroFile 和 CategoriesForRole**

```go
// ParseRetroFile 讀取 Acceptor 產出的 retro-learnings.json
func ParseRetroFile(path string) ([]RetroLearning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rf RetroFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse retro file: %w", err)
	}
	return rf.Learnings, nil
}

var roleCategoryMap = map[string][]Category{
	"coder":         {CategoryDesign, CategoryCodeQuality, CategoryTooling},
	"reviewer":      {CategoryCodeQuality, CategoryReview},
	"deep-reviewer": {CategoryCodeQuality, CategoryReview, CategoryDesign},
	"tester":        {CategoryTesting, CategoryTooling},
	"acceptor":      {CategoryProcess},
}

// CategoriesForRole 回傳指定 role 應注入的 category 列表
func CategoriesForRole(role string) []Category {
	return roleCategoryMap[role]
}
```

- [ ] **Step 11: 跑全部測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./internal/learning/ -v`
Expected: ALL PASS

- [ ] **Step 12: Commit**

```bash
git add internal/learning/
git commit -m "feat(F089): add internal/learning package — store, harvest, lifecycle"
```

---

### Task 4: Acceptor Template 擴充 + CLI 收割

**Files:**
- Modify: `templates/acceptor.md.tmpl` — 新增 retro learnings 產出指示
- Modify: `cmd/4x/run.go` — accepting phase 結束後呼叫 harvest
- Create: `cmd/4x/harvest_test.go` — 收割邏輯整合測試

**Interfaces:**
- Consumes: `learning.ParseRetroFile(path) ([]RetroLearning, error)`, `learning.LoadStore(path) (Store, error)`, `learning.Store.Harvest(featureID, learnings) int`, `learning.Store.Save(path) error`, `learning.Store.MarkStale(days)`, `protocol.Workspace.DotDir()`, `protocol.Workspace.FeatureDir(id)`
- Produces: `harvestLearnings(ws *protocol.Workspace, featureID string)` — 在 run loop 呼叫

- [ ] **Step 1: 擴充 `acceptor.md.tmpl`**

在 `== Constraints ==` 段落前加入：

```
== Retro Learnings ==
除了 final-report.md，你還必須產出一份 retro learnings 檔案：

  {{.DotDir}}/{{.Feature.ID}}/retro-learnings.json

回顧整個 feature 的開發過程，萃取出對未來 feature 有價值的教訓。

格式（嚴格遵守此 JSON 結構）：
{
  "learnings": [
    {
      "category": "design | code-quality | testing | review | tooling | process",
      "content": "具體、可操作的一句話教訓"
    }
  ]
}

原則：
- 只寫「未來能改善什麼」，不寫「這次做了什麼」
- 每條要具體到可直接當作 instruction 使用
- category 必須是以下之一：design, code-quality, testing, review, tooling, process
- 不超過 5 條——只留最有價值的
- 如果這次開發沒什麼特別教訓，寫空 array：{"learnings": []}

```

同時更新 Constraints 段的「You may ONLY write final-report.md」為：

```
- You may ONLY write final-report.md and retro-learnings.json
```

- [ ] **Step 2: 寫 `harvestLearnings` 函式**

在 `cmd/4x/run.go` 新增：

```go
// harvestLearnings 讀取 Acceptor 產出的 retro-learnings.json，追加到 .4x/learnings.json。
// 任何錯誤只 warn，不影響 state transition。
func harvestLearnings(ws *protocol.Workspace, featureID string) {
	retroPath := filepath.Join(ws.FeatureDir(featureID), "retro-learnings.json")
	learnings, err := learning.ParseRetroFile(retroPath)
	if err != nil {
		slog.Warn("skip learnings harvest", "feature", featureID, "error", err)
		return
	}
	if len(learnings) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), "learnings.json")
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store failed", "error", err)
		return
	}

	store.MarkStale(learning.DefaultStaleDays)
	added := store.Harvest(featureID, learnings)
	if added == 0 {
		return
	}

	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store failed", "error", err)
		return
	}

	active := len(store.ActiveEntries())
	slog.Info("harvested learnings", "feature", featureID, "added", added, "total_active", active)
	if active > learning.MaxActiveEntries {
		slog.Warn("learnings store exceeds capacity, consider running '4x learn prune'",
			"active", active, "limit", learning.MaxActiveEntries)
	}
}
```

- [ ] **Step 3: 在 run loop 的 PhasePendingReview 分支呼叫 harvest**

在 `cmd/4x/run.go` 的 switch 區塊（約 line 1117），`case protocol.PhasePendingReview:` 內，`s.Active = false` 之前插入：

```go
case protocol.PhasePendingReview:
	harvestLearnings(ws, featureID)  // 新增這行
	s.Active = false
	// ... 其餘不變
```

- [ ] **Step 4: 在 `run.go` 加 import**

```go
"github.com/ggwhite/4x/internal/learning"
```

- [ ] **Step 5: 寫整合測試**

建立 `cmd/4x/harvest_test.go`：

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestHarvestLearnings_Success(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	ws.InitFeatureDir(featureID)

	// 寫 retro-learnings.json
	retro := learning.RetroFile{
		Learnings: []learning.RetroLearning{
			{Category: learning.CategoryCodeQuality, Content: "always wrap errors"},
			{Category: learning.CategoryTesting, Content: "test edge cases"},
		},
	}
	data, _ := json.Marshal(retro)
	retroPath := filepath.Join(ws.FeatureDir(featureID), "retro-learnings.json")
	writeTestFileHelper(t, retroPath, string(data))

	harvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), "learnings.json")
	store, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(store.Entries))
	}
}

func TestHarvestLearnings_NoRetroFile(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	ws.InitFeatureDir(featureID)

	// 不寫 retro-learnings.json，不應 panic
	harvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), "learnings.json")
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Error("expected no learnings.json when no retro file")
	}
}
```

- [ ] **Step 6: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -run TestHarvestLearnings -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add templates/acceptor.md.tmpl cmd/4x/run.go cmd/4x/harvest_test.go
git commit -m "feat(F089): acceptor retro learnings output + CLI harvest on accepting"
```

---

### Task 5: Designer Learnings 注入 + selected-learnings.json

**Files:**
- Modify: `cmd/4x/prompt.go:69-91` — `promptData` 新增 `Learnings` 欄位，Designer 時載入
- Modify: `templates/designer.md.tmpl` — 新增 learnings 選擇段落
- Test: `cmd/4x/prompt_test.go` — 新增測試

**Interfaces:**
- Consumes: `learning.LoadStore(path)`, `learning.Store.ActiveEntries()`, `learning.Entry`
- Produces: `promptData.Learnings []learning.Entry` — Designer template 用

- [ ] **Step 1: 在 `promptData` 新增 Learnings 欄位**

修改 `cmd/4x/prompt.go`：

```go
type promptData struct {
	// ... 既有欄位不變 ...
	ProfileInstructions []profileContent
	// Learnings 是所有 active learnings，供 Designer template 選擇
	Learnings []learning.Entry
	// SelectedLearnings 是已選中的 learnings，供後續 role template 注入
	SelectedLearnings []learning.Entry
	// ... deep review 欄位不變 ...
}
```

- [ ] **Step 2: 在 prompt 產生時載入 learnings（Designer）**

在 `newPromptCmd` 的 `data := promptData{...}` 之後加上：

```go
if r == protocol.RoleDesigner {
	data.Learnings = loadActiveLearnings(ws.DotDir())
}
```

新增 helper：

```go
// loadActiveLearnings 讀取 learnings.json 中所有 active 條目，供 Designer prompt 注入。
func loadActiveLearnings(dotDir string) []learning.Entry {
	storePath := filepath.Join(dotDir, "learnings.json")
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings for prompt failed", "error", err)
		return nil
	}
	return store.ActiveEntries()
}
```

- [ ] **Step 3: 擴充 `designer.md.tmpl`**

在 `== Constraints ==` 段落前加入：

```
{{- if .Learnings}}

== Past Learnings ==
以下是過去 feature 累積的經驗教訓。從中挑出與本次 feature 相關的 ID，
寫入 selected-learnings.json：

  {{.DotDir}}/{{.Feature.ID}}/selected-learnings.json

格式：
{
  "selected": ["L001", "L003"]
}

只選真正相關的，不相關就寫空 array：{"selected": []}。不超過 10 條。

可用的 learnings：
{{range .Learnings}}
- [{{.ID}}] ({{.Category}}) {{.Content}}
{{- end}}
{{- end}}
```

- [ ] **Step 4: 寫測試確認 Designer prompt 包含 learnings**

```go
func TestPromptData_DesignerWithLearnings(t *testing.T) {
	tmpl, err := loadRoleTemplate("", protocol.RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}

	data := promptData{
		Feature: feature.Feature{ID: "F089-test", Name: "Test Feature"},
		DotDir:  "/tmp/.4x",
		Learnings: []learning.Entry{
			{ID: "L001", Category: learning.CategoryCodeQuality, Content: "always wrap errors"},
			{ID: "L002", Category: learning.CategoryDesign, Content: "split large features"},
		},
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "Past Learnings") {
		t.Error("expected Past Learnings section")
	}
	if !strings.Contains(out, "[L001]") || !strings.Contains(out, "always wrap errors") {
		t.Error("expected L001 content in prompt")
	}
	if !strings.Contains(out, "selected-learnings.json") {
		t.Error("expected selected-learnings.json instruction")
	}
}

func TestPromptData_DesignerWithoutLearnings(t *testing.T) {
	tmpl, err := loadRoleTemplate("", protocol.RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}

	data := promptData{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  "/tmp/.4x",
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Past Learnings") {
		t.Error("should not contain Past Learnings when empty")
	}
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -run TestPromptData_Designer -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/prompt.go cmd/4x/prompt_test.go templates/designer.md.tmpl
git commit -m "feat(F089): inject active learnings into Designer prompt for selection"
```

---

### Task 6: 後續 Role Learnings 注入 + used_count 更新

**Files:**
- Modify: `cmd/4x/prompt.go` — 非 Designer role 讀 `selected-learnings.json` 並按 category 篩注入
- Modify: `templates/coder.md.tmpl`, `templates/reviewer.md.tmpl`, `templates/tester.md.tmpl`, `templates/deep-reviewer.md.tmpl`, `templates/acceptor.md.tmpl` — 各加 SelectedLearnings 段落
- Modify: `cmd/4x/run.go` — 第一個非 Designer phase 時更新 used_count
- Test: `cmd/4x/prompt_test.go` — 新增測試

**Interfaces:**
- Consumes: `learning.LoadStore()`, `learning.CategoriesForRole()`, `learning.Store.UpdateUsage()`, `learning.Store.Save()`, `learning.MaxSelectedPerRole`
- Produces: `promptData.SelectedLearnings []learning.Entry` — 後續 role template 用

- [ ] **Step 1: 新增 `loadSelectedLearnings` helper**

在 `cmd/4x/prompt.go` 加上：

```go
// SelectedLearningsFile 是 Designer 產出的選擇檔案名
const SelectedLearningsFile = "selected-learnings.json"

// selectedLearningsPayload 是 selected-learnings.json 的結構
type selectedLearningsPayload struct {
	Selected []string `json:"selected"`
}

// loadSelectedLearnings 讀取 selected-learnings.json，反查 learnings.json 取完整內容，
// 按 role 的 category 白名單篩選後回傳。
func loadSelectedLearnings(dotDir, featureID string, role protocol.Role) []learning.Entry {
	selPath := filepath.Join(dotDir, featureID, SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return nil
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		slog.Warn("parse selected-learnings.json failed", "error", err)
		return nil
	}
	if len(payload.Selected) == 0 {
		return nil
	}

	storePath := filepath.Join(dotDir, "learnings.json")
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for injection failed", "error", err)
		return nil
	}

	entryMap := make(map[string]learning.Entry, len(store.Entries))
	for _, e := range store.Entries {
		entryMap[e.ID] = e
	}

	categories := learning.CategoriesForRole(string(role))
	catSet := make(map[learning.Category]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	var result []learning.Entry
	for _, id := range payload.Selected {
		if len(result) >= learning.MaxSelectedPerRole {
			break
		}
		e, ok := entryMap[id]
		if !ok || e.Status != learning.StatusActive {
			continue
		}
		if !catSet[e.Category] {
			continue
		}
		result = append(result, e)
	}
	return result
}
```

- [ ] **Step 2: 在 prompt 產生時載入 selected learnings（非 Designer）**

在 `newPromptCmd` 的 learnings 載入區塊加上：

```go
if r == protocol.RoleDesigner {
	data.Learnings = loadActiveLearnings(ws.DotDir())
} else {
	data.SelectedLearnings = loadSelectedLearnings(ws.DotDir(), featureID, r)
}
```

同樣地，在 `run.go` 的 `generatePrompt` 函式內做相同的處理（搜尋 `promptData{` 的建構處）。

- [ ] **Step 3: 在 5 個 role template 加 SelectedLearnings 段落**

在 `coder.md.tmpl`、`reviewer.md.tmpl`、`tester.md.tmpl`、`deep-reviewer.md.tmpl`、`acceptor.md.tmpl` 各自的 `== Constraints ==` 段落前加入：

```
{{- if .SelectedLearnings}}

== Learnings from Past Features ==
以下是從過去經驗中挑出與本次工作相關的教訓，請納入考量：
{{range .SelectedLearnings}}
- [{{.Category}}] {{.Content}}
{{- end}}
{{- end}}
```

- [ ] **Step 4: 實作 used_count 更新**

在 `cmd/4x/run.go` 新增：

```go
// updateLearningsUsage 在第一個非 Designer phase 時，讀 selected-learnings.json
// 更新被選中 learning 的 last_used 和 used_count。
func updateLearningsUsage(ws *protocol.Workspace, featureID string) {
	selPath := filepath.Join(ws.FeatureDir(featureID), SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil || len(payload.Selected) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), "learnings.json")
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for usage update failed", "error", err)
		return
	}

	store.UpdateUsage(payload.Selected)
	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store after usage update failed", "error", err)
	}
}
```

在 run loop 中，coding phase 開始前呼叫一次（只呼叫一次，不是每個 phase 都呼叫）。在 state transition 後、runner 呼叫前，加一個 one-shot flag：

```go
// 在 runLoop 開頭加
var learningsUsageUpdated bool

// 在 main loop 內，runner 呼叫前
if !learningsUsageUpdated && s.Phase != protocol.PhaseDesigning && s.Phase != protocol.PhaseDesignReviewing {
	updateLearningsUsage(ws, featureID)
	learningsUsageUpdated = true
}
```

- [ ] **Step 5: 寫測試**

```go
func TestLoadSelectedLearnings_FiltersByCategory(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	ws.InitFeatureDir(featureID)

	// 建 learnings.json
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryCodeQuality, Content: "wrap errors", Status: learning.StatusActive},
		{ID: "L002", Category: learning.CategoryTesting, Content: "test edges", Status: learning.StatusActive},
		{ID: "L003", Category: learning.CategoryProcess, Content: "escalate early", Status: learning.StatusActive},
	}}
	storePath := filepath.Join(ws.DotDir(), "learnings.json")
	store.Save(storePath)

	// 建 selected-learnings.json
	sel := `{"selected": ["L001", "L002", "L003"]}`
	writeTestFileHelper(t, filepath.Join(ws.FeatureDir(featureID), SelectedLearningsFile), sel)

	// Coder 應看到 code-quality（L001）但不看到 process（L003）
	entries := loadSelectedLearnings(ws.DotDir(), featureID, protocol.RoleCoder)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for coder, got %d", len(entries))
	}
	if entries[0].ID != "L001" {
		t.Errorf("expected L001, got %s", entries[0].ID)
	}

	// Tester 應看到 testing（L002）但不看到 code-quality（L001）或 process（L003）
	entries = loadSelectedLearnings(ws.DotDir(), featureID, protocol.RoleTester)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for tester, got %d", len(entries))
	}
	if entries[0].ID != "L002" {
		t.Errorf("expected L002, got %s", entries[0].ID)
	}
}

func TestLoadSelectedLearnings_NoFile(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}

	entries := loadSelectedLearnings(ws.DotDir(), "F042-test", protocol.RoleCoder)
	if entries != nil {
		t.Errorf("expected nil when no selected-learnings.json, got %v", entries)
	}
}
```

- [ ] **Step 6: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -run TestLoadSelectedLearnings -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/prompt.go cmd/4x/prompt_test.go cmd/4x/run.go \
  templates/coder.md.tmpl templates/reviewer.md.tmpl templates/tester.md.tmpl \
  templates/deep-reviewer.md.tmpl templates/acceptor.md.tmpl
git commit -m "feat(F089): inject selected learnings into role prompts + update usage"
```

---

### Task 7: `4x learn` 子命令

**Files:**
- Create: `cmd/4x/learn.go` — learn list/prune/promote/remove 子命令
- Create: `cmd/4x/learn_test.go` — 測試
- Modify: `cmd/4x/main.go` — 註冊 `newLearnCmd()`

**Interfaces:**
- Consumes: `learning.LoadStore()`, `learning.Store.Save()`, `learning.Store.Promote()`, `learning.Store.Remove()`, `learning.Store.Prune()`, `learning.Store.MarkStale()`, `learning.ValidCategories()`
- Produces: `newLearnCmd() *cobra.Command` — 註冊到 main

- [ ] **Step 1: 建立 `cmd/4x/learn.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newLearnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learn",
		Short: "Manage retro learnings",
	}
	cmd.AddCommand(
		newLearnListCmd(),
		newLearnPruneCmd(),
		newLearnPromoteCmd(),
		newLearnRemoveCmd(),
	)
	return cmd
}

func newLearnListCmd() *cobra.Command {
	var category string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all learnings",
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCATEGORY\tSTATUS\tUSED\tCONTENT")
			for _, e := range store.Entries {
				if category != "" && string(e.Category) != category {
					continue
				}
				content := e.Content
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					e.ID, e.Category, e.Status, e.UsedCount, content)
			}
			w.Flush()

			active := len(store.ActiveEntries())
			fmt.Printf("\n%d entries (%d active)\n", len(store.Entries), active)
			return nil
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "filter by category")
	return cmd
}

func newLearnPruneCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove all stale learnings",
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}

			store.MarkStale(learning.DefaultStaleDays)

			var staleIDs []string
			for _, e := range store.Entries {
				if e.Status == learning.StatusStale {
					staleIDs = append(staleIDs, e.ID)
					fmt.Printf("  %s (%s) %s\n", e.ID, e.Category, e.Content)
				}
			}

			if len(staleIDs) == 0 {
				fmt.Println("No stale learnings found.")
				return nil
			}

			if dryRun {
				fmt.Printf("\n%d stale entries would be removed (dry-run)\n", len(staleIDs))
				return nil
			}

			removed := store.Prune()
			if err := store.Save(storePath); err != nil {
				return err
			}
			fmt.Printf("\nRemoved %d stale entries.\n", removed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without removing")
	return cmd
}

func newLearnPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <id>",
		Short: "Mark a learning as promoted (upgraded to template/instructions)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}
			if err := store.Promote(args[0]); err != nil {
				return err
			}
			if err := store.Save(storePath); err != nil {
				return err
			}
			fmt.Printf("Promoted %s\n", args[0])
			return nil
		},
	}
}

func newLearnRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a learning entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := findLearningsPath()
			if err != nil {
				return err
			}
			store, err := learning.LoadStore(storePath)
			if err != nil {
				return err
			}
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			if err := store.Save(storePath); err != nil {
				return err
			}
			fmt.Printf("Removed %s\n", args[0])
			return nil
		},
	}
}

// findLearningsPath 從 cwd 往上找 .4x/，回傳 learnings.json 路徑
func findLearningsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return "", fmt.Errorf("not in a 4x project: %w", err)
	}
	return filepath.Join(ws.DotDir(), "learnings.json"), nil
}
```

- [ ] **Step 2: 在 `main.go` 註冊**

```go
root.AddCommand(
	// ... 既有 commands ...
	newLearnCmd(),
)
```

- [ ] **Step 3: 寫測試**

建立 `cmd/4x/learn_test.go`：

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestLearnPromote(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	storePath := filepath.Join(root, protocol.DirName, "learnings.json")

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "test", Status: learning.StatusActive,
			CreatedAt: time.Now()},
	}}
	store.Save(storePath)

	// 模擬 promote
	loaded, _ := learning.LoadStore(storePath)
	if err := loaded.Promote("L001"); err != nil {
		t.Fatal(err)
	}
	loaded.Save(storePath)

	reloaded, _ := learning.LoadStore(storePath)
	if reloaded.Entries[0].Status != learning.StatusPromoted {
		t.Errorf("expected promoted, got %s", reloaded.Entries[0].Status)
	}
}

func TestLearnRemove(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	storePath := filepath.Join(root, protocol.DirName, "learnings.json")

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "a", Status: learning.StatusActive,
			CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "b", Status: learning.StatusActive,
			CreatedAt: time.Now()},
	}}
	store.Save(storePath)

	loaded, _ := learning.LoadStore(storePath)
	loaded.Remove("L001")
	loaded.Save(storePath)

	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(reloaded.Entries))
	}
	if reloaded.Entries[0].ID != "L002" {
		t.Errorf("expected L002, got %s", reloaded.Entries[0].ID)
	}
}

func TestLearnPrune(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	storePath := filepath.Join(root, protocol.DirName, "learnings.json")

	old := time.Now().Add(-91 * 24 * time.Hour)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "old", Status: learning.StatusActive,
			CreatedAt: old},
		{ID: "L002", Category: learning.CategoryDesign, Content: "new", Status: learning.StatusActive,
			CreatedAt: time.Now()},
	}}
	store.Save(storePath)

	loaded, _ := learning.LoadStore(storePath)
	loaded.MarkStale(learning.DefaultStaleDays)
	removed := loaded.Prune()
	loaded.Save(storePath)

	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(reloaded.Entries))
	}
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./cmd/4x/ -run TestLearn -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/learn.go cmd/4x/learn_test.go cmd/4x/main.go
git commit -m "feat(F089): 4x learn list/prune/promote/remove subcommands"
```

---

### Task 8: Protocol 常量 + docs 同步 + 最終驗證

**Files:**
- Modify: `internal/protocol/workspace.go` — 新增 `LearningsFile`, `RetroLearningsFile`, `SelectedLearningsFile` 常量
- Modify: `cmd/4x/run.go`, `cmd/4x/prompt.go`, `cmd/4x/learn.go` — 用常量取代硬編碼字串
- Run: `make check-docs-sync` and `make check-i18n`

**Interfaces:**
- Consumes: 前面所有 task 的產出
- Produces: 常量化 + docs 同步 + 全量驗證通過

- [ ] **Step 1: 在 `workspace.go` 新增常量**

```go
const (
	// ... 既有常量 ...
	LearningsFile         = "learnings.json"
	RetroLearningsFile    = "retro-learnings.json"
	SelectedLearningsFile = "selected-learnings.json"
)
```

- [ ] **Step 2: 替換所有硬編碼字串**

搜尋 `cmd/4x/` 內所有 `"learnings.json"`、`"retro-learnings.json"`、`"selected-learnings.json"` 字串，替換為 `protocol.LearningsFile`、`protocol.RetroLearningsFile`、`protocol.SelectedLearningsFile`。

同時移除 `cmd/4x/prompt.go` 中自行定義的 `SelectedLearningsFile` 常量。

- [ ] **Step 3: 跑 docs 同步檢查**

Run: `make check-docs-sync`

若輸出 `NEEDS_UPDATE`，更新被點名的 doc 檔。特別注意 `docs/guide/cli.md` 需要加入 `4x learn` 和 `4x init --dump-templates` 的說明。

- [ ] **Step 4: 跑 i18n 檢查**

Run: `make check-i18n`

若輸出 `ERROR: missing keys`，補齊對應語系的缺漏 key。

- [ ] **Step 5: 全量驗證**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./...`
Expected: ALL PASS, 0 vet warnings

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/workspace.go cmd/4x/run.go cmd/4x/prompt.go cmd/4x/learn.go
git commit -m "refactor(F089): extract protocol constants for learnings file names"
```

如果 docs/i18n 有更新：

```bash
git add docs/ dashboard/
git commit -m "docs(F089): update CLI guide and i18n for learn subcommand"
```
