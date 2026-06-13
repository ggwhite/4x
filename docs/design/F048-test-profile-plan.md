# F048: Test Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Designer 在 test-strategy.yaml 標記 profiles，Tester prompt 自動注入對應的測試方法論。

**Architecture:** 內建 4 個 profile `.md` 檔用 go:embed 嵌入 binary。`prompt.go` 新增 `loadProfiles` 讀 test-strategy.yaml 的 profiles 欄位，按優先序載入內容（settings.json 覆寫 > 內建），注入 `promptData`。Tester template 用 `{{range .ProfileInstructions}}` 渲染。

**Tech Stack:** Go 1.26+, go:embed, text/template, gopkg.in/yaml.v3

---

## File Structure

| 檔案 | 職責 |
|---|---|
| `internal/protocol/types.go` | 擴展 `TestStrategy`（加 `Profiles`）、`Config`（加 `TestProfiles`）、新增 `TestProfileOverride` |
| `templates/profiles/unit.md` | 內建 unit test profile |
| `templates/profiles/web.md` | 內建 web UI test profile |
| `templates/profiles/api.md` | 內建 API test profile |
| `templates/profiles/e2e.md` | 內建 e2e test profile |
| `templates/embed.go` | 加 `ProfilesFS` embed |
| `cmd/4x/prompt.go` | 新增 `loadProfiles`、`profileContent`；`promptData` 加 `ProfileInstructions` |
| `cmd/4x/prompt_test.go` | profile 載入邏輯的單元測試 |
| `templates/tester.md.tmpl` | 加 profile 注入區塊 |
| `templates/designer.md.tmpl` | 加 profiles 說明 |
| `.4x/settings.json` | tester instructions 遷移 |

---

### Task 1: 擴展 protocol 型別

**Files:**
- Modify: `internal/protocol/types.go:209-216` (TestStrategy)
- Modify: `internal/protocol/types.go:240-252` (Config)

- [ ] **Step 1: 在 `TestStrategy` 加 `Profiles` 欄位**

在 `internal/protocol/types.go` 的 `TestStrategy` struct，`Verify` 欄位後加：

```go
// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web          bool                `yaml:"web" json:"web"`
	API          bool                `yaml:"api" json:"api"`
	Gate         bool                `yaml:"gate" json:"gate"`
	CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
	Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
	VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
	Profiles     []string            `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}
```

注意：`VerifyGroups` 是 F047 加的欄位。如果 F047 尚未實作，省略該行，只加 `Profiles`。

- [ ] **Step 2: 新增 `TestProfileOverride` 型別**

在 `TestStrategy` struct 後面加：

```go
// TestProfileOverride 允許專案在 settings.json 覆寫或新增 test profile
type TestProfileOverride struct {
	Content string `json:"content,omitempty"`
	Include string `json:"include,omitempty"`
}
```

- [ ] **Step 3: 在 `Config` 加 `TestProfiles` 欄位**

在 `internal/protocol/types.go` 的 `Config` struct 末尾加：

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
	TestProfiles      map[string]TestProfileOverride `json:"test_profiles,omitempty"`
}
```

- [ ] **Step 4: 驗證編譯**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 5: 跑既有測試確認無破壞**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F048): extend TestStrategy with Profiles, add TestProfileOverride to Config"
```

---

### Task 2: 建立內建 profile 檔案與 embed

**Files:**
- Create: `templates/profiles/unit.md`
- Create: `templates/profiles/web.md`
- Create: `templates/profiles/api.md`
- Create: `templates/profiles/e2e.md`
- Modify: `templates/embed.go`

- [ ] **Step 1: 建立 `templates/profiles/unit.md`**

```markdown
Go 單元測試規範：
- 用 go test 執行，t.TempDir() 做 workspace 隔離
- 不要污染實際 .4x/ 目錄——測試在臨時目錄操作，測完自動清理
- table-driven test 處理多種輸入組合
- error case 也要測——確認 error message 有意義
- verify.json 每項 AC 都要有對應的 pass/fail 結果
```

- [ ] **Step 2: 建立 `templates/profiles/web.md`**

```markdown
Web UI 測試規範（Playwright）：
- 用 Playwright 測試 4x live dashboard
- 測試流程：先啟動 4x live（背景執行）→ 寫 Playwright 腳本(.ts) → npx playwright test 執行 → 截圖作為 evidence
- 腳本存到 .4x/e2e/{feature-id}/，檔名用描述性名稱如 run-feature.spec.ts
- 截圖存到 .4x/e2e/{feature-id}/screenshot/，檔名格式 {step}-{description}.png（如 01-dashboard-loaded.png）
- Playwright 未安裝時先跑 npx playwright install chromium
- 每個 AC 至少一張截圖作為 evidence
- 測試完畢關閉 4x live 背景進程
```

- [ ] **Step 3: 建立 `templates/profiles/api.md`**

```markdown
HTTP API 測試規範：
- 用 Go httptest 套件建立測試 server，或用 curl 打實際端點
- 驗證 status code：2xx 是 happy path、4xx 是 client error、5xx 不該出現
- 驗證 response body：JSON 結構正確、必要欄位存在、值符合預期
- 測試 edge case：空 body、缺必要欄位、無效 ID、超長輸入
- 若 API 有認證，測試未認證和認證場景
- 記錄每次 request/response 作為 evidence
```

- [ ] **Step 4: 建立 `templates/profiles/e2e.md`**

```markdown
端對端整合測試規範：
- 驗證資料流從輸入到最終狀態的完整路徑
- 多元件協作場景：啟動所有必要的服務和依賴
- 資料庫狀態驗證：操作前後確認資料一致性
- 跨服務一致性：確認 event 傳遞、狀態同步正確
- 清理：測試後恢復環境，不留殘餘資料
- 若需要外部服務，記錄如何啟動和設定
```

- [ ] **Step 5: 修改 `templates/embed.go`**

```go
package templates

import "embed"

//go:embed *.tmpl
var FS embed.FS

//go:embed profiles/*.md
var ProfilesFS embed.FS
```

- [ ] **Step 6: 驗證編譯（embed 路徑正確）**

Run: `go build ./... && go vet ./...`
Expected: 成功。若 embed 路徑有誤會在此報錯。

- [ ] **Step 7: Commit**

```bash
git add templates/profiles/ templates/embed.go
git commit -m "feat(F048): add built-in test profile files with embed"
```

---

### Task 3: 實作 `loadProfiles` 邏輯

**Files:**
- Modify: `cmd/4x/prompt.go:97-115`
- Create: `cmd/4x/prompt_test.go`

- [ ] **Step 1: 寫 `loadProfiles` 的失敗測試**

建立 `cmd/4x/prompt_test.go`：

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestLoadProfiles_BuiltinUnit(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\nverify_commands:\n  - \"echo ok\"\n")

	cfg := protocol.Config{}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Name != "unit" {
		t.Errorf("expected name 'unit', got %q", profiles[0].Name)
	}
	if profiles[0].Content == "" {
		t.Error("expected non-empty content for unit profile")
	}
}

func TestLoadProfiles_NoProfiles_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "verify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if profiles != nil {
		t.Errorf("expected nil, got %v", profiles)
	}
}

func TestLoadProfiles_SettingsOverride(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\nverify_commands:\n  - \"echo ok\"\n")

	cfg := protocol.Config{
		TestProfiles: map[string]protocol.TestProfileOverride{
			"unit": {Content: "custom unit instructions"},
		},
	}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Content != "custom unit instructions" {
		t.Errorf("expected override content, got %q", profiles[0].Content)
	}
}

func TestLoadProfiles_SettingsInclude(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - custom\nverify_commands:\n  - \"echo ok\"\n")

	includePath := filepath.Join(root, "my-profile.md")
	writeTestFileHelper(t, includePath, "custom profile content from file")

	cfg := protocol.Config{
		TestProfiles: map[string]protocol.TestProfileOverride{
			"custom": {Include: "my-profile.md"},
		},
	}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Content != "custom profile content from file" {
		t.Errorf("expected file content, got %q", profiles[0].Content)
	}
}

func TestLoadProfiles_UnknownProfile_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - nonexistent\nverify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles for unknown name, got %d", len(profiles))
	}
}

func TestLoadProfiles_MultipleProfiles(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\n  - web\nverify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "unit" || profiles[1].Name != "web" {
		t.Errorf("expected [unit, web], got [%s, %s]", profiles[0].Name, profiles[1].Name)
	}
}

func TestLoadProfiles_NoStrategyFile_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if profiles != nil {
		t.Errorf("expected nil when no test-strategy.yaml, got %v", profiles)
	}
}

func writeTestFileHelper(t *testing.T, path, content string) {
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

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./cmd/4x/ -v -run TestLoadProfiles`
Expected: 編譯失敗（`loadProfiles` 和 `profileContent` 未定義）

- [ ] **Step 3: 實作 `loadProfiles`**

在 `cmd/4x/prompt.go` 的 `includeContent` 定義後（第 115 行附近）加入：

```go
type profileContent struct {
	Name    string
	Content string
}

// loadProfiles 讀取 test-strategy.yaml 的 profiles，載入對應的測試方法論內容。
// 優先序：settings.json test_profiles 覆寫 > 內建 templates/profiles/{name}.md。
func loadProfiles(ws *protocol.Workspace, featureID string, cfg protocol.Config) []profileContent {
	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	data, err := os.ReadFile(stratPath)
	if err != nil {
		return nil
	}

	var ts protocol.TestStrategy
	if err := yaml.Unmarshal(data, &ts); err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid %s: %v\n", protocol.TestStratFile, err)
		return nil
	}

	if len(ts.Profiles) == 0 {
		return nil
	}

	var result []profileContent
	for _, name := range ts.Profiles {
		content := resolveProfileContent(ws.Root, name, cfg)
		if content == "" {
			fmt.Fprintf(os.Stderr, "warning: unknown test profile %q, skipping\n", name)
			continue
		}
		result = append(result, profileContent{Name: name, Content: content})
	}
	return result
}

func resolveProfileContent(root, name string, cfg protocol.Config) string {
	if override, ok := cfg.TestProfiles[name]; ok {
		if override.Content != "" {
			return override.Content
		}
		if override.Include != "" {
			p := override.Include
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: test profile include %s: %v\n", override.Include, err)
				return ""
			}
			return string(data)
		}
	}

	data, err := templates.ProfilesFS.ReadFile("profiles/" + name + ".md")
	if err != nil {
		return ""
	}
	return string(data)
}
```

同時在 `prompt.go` 的 import 區段加入 `"gopkg.in/yaml.v3"`（如果尚未存在）。

- [ ] **Step 4: 在 `promptData` 加 `ProfileInstructions` 欄位**

修改 `cmd/4x/prompt.go` 的 `promptData` struct：

```go
type promptData struct {
	Feature             protocol.Feature
	Project             protocol.ProjectConfig
	Role                protocol.Role
	Round               int
	Config              protocol.Config
	DotDir              string
	Locale              string
	LocaleName          string
	RoleInstructions    []string
	ProjectIncludes     []includeContent
	RoleIncludes        []includeContent
	PlanningDoc         string
	ProfileInstructions []profileContent
}
```

- [ ] **Step 5: 在 `newPromptCmd` 組裝 `promptData` 時加入 `loadProfiles`**

修改 `cmd/4x/prompt.go` 第 68-81 行的 `data := promptData{...}`，在末尾加：

```go
data := promptData{
	Feature:             feature,
	Project:             cfg.Project,
	Role:                r,
	Round:               round,
	Config:              cfg,
	DotDir:              ws.DotDir(),
	Locale:              locale,
	LocaleName:          localeName,
	RoleInstructions:    roleInstructions(cfg, r),
	ProjectIncludes:     loadIncludes(ws.Root, cfg.Project.Includes),
	RoleIncludes:        loadIncludes(ws.Root, roleInc),
	PlanningDoc:         loadPlanningDocs(ws.Root, feature.ID),
	ProfileInstructions: loadProfiles(ws, featureID, cfg),
}
```

- [ ] **Step 6: 在 `generatePrompt` 也加入 `loadProfiles`**

修改 `cmd/4x/run.go` 第 296-309 行的 `data := promptData{...}`，在末尾加：

```go
data := promptData{
	Feature:             feature,
	Project:             cfg.Project,
	Role:                role,
	Round:               round,
	Config:              cfg,
	DotDir:              runnerWs.DotDir(),
	Locale:              locale,
	LocaleName:          localeName,
	RoleInstructions:    roleInstructions(cfg, role),
	ProjectIncludes:     loadIncludes(ws.Root, cfg.Project.Includes),
	RoleIncludes:        loadIncludes(ws.Root, roleInc),
	PlanningDoc:         loadPlanningDocs(ws.Root, feature.ID),
	ProfileInstructions: loadProfiles(ws, feature.ID, cfg),
}
```

- [ ] **Step 7: 確認測試通過**

Run: `go test ./cmd/4x/ -v -run TestLoadProfiles`
Expected: 7 tests PASS

- [ ] **Step 8: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/4x/prompt.go cmd/4x/prompt_test.go cmd/4x/run.go
git commit -m "feat(F048): add loadProfiles to inject test profile content into prompt"
```

---

### Task 4: 修改 Tester template

**Files:**
- Modify: `templates/tester.md.tmpl`

- [ ] **Step 1: 在 Tester template 加 profile 注入區塊**

在 `templates/tester.md.tmpl` 的 RoleIncludes 區段後（第 47 行後）、Screenshots 區段前（第 49 行前），插入：

```
{{- if .ProfileInstructions}}
{{range .ProfileInstructions}}

== Test Profile: {{.Name}} ==
{{.Content}}
{{- end}}
{{- end}}
```

完整的插入位置——在這一行之後：

```
{{- end}}
{{- end}}
```

（RoleIncludes 的結尾，第 47 行）

在這一行之前：

```
## Screenshots in verify.json
```

（第 49 行）

- [ ] **Step 2: 驗證模板語法**

Run: `go build ./cmd/4x && go test ./...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add templates/tester.md.tmpl
git commit -m "feat(F048): add profile injection block to tester template"
```

---

### Task 5: 修改 Designer template

**Files:**
- Modify: `templates/designer.md.tmpl:96-101`

- [ ] **Step 1: 修改 test-strategy.yaml format 區塊**

將 `templates/designer.md.tmpl` 第 96-101 行的：

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
profiles:
  - unit
verify_commands:
  - "command here"

Available profiles: unit, web, api, e2e.
Profiles inject test methodology into Tester prompt automatically.
Choose profiles that match the feature's testing needs:
  - CLI/library feature → profiles: [unit]
  - Dashboard UI feature → profiles: [unit, web]
  - REST API feature → profiles: [unit, api]
  - Multi-service feature → profiles: [unit, e2e]
```

- [ ] **Step 2: 驗證編譯**

Run: `go build ./cmd/4x && go test ./...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add templates/designer.md.tmpl
git commit -m "feat(F048): add profiles guide to designer template"
```

---

### Task 6: 遷移 tester instructions

**Files:**
- Modify: `.4x/settings.json`

- [ ] **Step 1: 精簡 tester instructions**

修改 `.4x/settings.json` 的 `roles.tester.instructions`，從 8 條改為只留通用規則：

```json
{
  "tester": {
    "model": "sonnet",
    "instructions": [
      "verify.json 必須每項 AC 都有對應的 pass/fail 結果"
    ]
  }
}
```

被移除的 7 條已遷入內建 profile：
- 條目 1（根據 test-strategy 決定測試方式）→ profile 機制取代
- 條目 2（Go 測試規範）→ `unit.md`
- 條目 3-7（Playwright 相關）→ `web.md`

- [ ] **Step 2: 驗證 prompt 輸出**

Run: `4x prompt <any-feature-with-test-strategy> --role tester --round 1 2>/dev/null | head -80`
Expected: 看到 `== Test Profile: unit ==` 區塊（如果該 feature 的 test-strategy.yaml 有 `profiles: [unit]`）

注意：如果現有 feature 的 test-strategy.yaml 還沒有 `profiles` 欄位，這步可以用一個測試 feature 來驗。

- [ ] **Step 3: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add .4x/settings.json
git commit -m "refactor(F048): migrate tester instructions to built-in profiles"
```

---

### Task 7: check-docs-sync 與最終驗證

**Files:**
- Possibly modify: `docs/guide/cli.md` or other docs

- [ ] **Step 1: 跑 check-docs-sync**

Run: `make check-docs-sync`
Expected: 檢查是否有 docs 需要更新

- [ ] **Step 2: 如有需要，更新相關 docs**

若 `check-docs-sync` 報 `NEEDS_UPDATE`，更新被點名的 doc 檔。

- [ ] **Step 3: 跑 check-i18n**

Run: `make check-i18n`
Expected: 無缺漏 key（此 feature 不涉及 i18n）

- [ ] **Step 4: 全部測試最終確認**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit（如有 docs 更新）**

```bash
git add docs/
git commit -m "docs(F048): update docs for test profile feature"
```
