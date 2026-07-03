# F127 — Issue-first MR flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓 `4x new`/`4x done` 在專案開啟 `issue_tracker.enabled` 時，自動在 GitHub/GitLab 建立（或連結既有）issue，並在完成時 push branch + 開 MR/PR 取代本地 squash-merge，真正的 code review 交給版控平台。

**Architecture:** 新增獨立的 `internal/vcshub` package 封裝 `gh`/`glab` CLI 呼叫（依 git remote hostname 自動判斷平台），`internal/gitops` 新增 `PushAndOpenMR` 並在 `finalize.go` 依設定分支到新舊兩條路徑，`cmd/4x/new.go` 在開關開啟時串接 preflight + 建立/連結 issue。

**Tech Stack:** Go 1.26+，`os/exec` 呼叫外部 CLI（`git`/`gh`/`glab`），標準 `testing` package，table-driven tests + 可注入的 exec function 做 CLI wrapper 測試（不打真實網路）。

## Global Constraints

- 這是 4x 專案本身的 dogfooding feature，遵循 `CLAUDE.md`：Go 1.26+、gofmt、Cobra CLI、`internal/` 不對外 export、CLI 層嚴禁呼叫 LLM（本功能呼叫 `gh`/`glab` 屬外部工具而非 LLM，允許）。
- 每個 task 完成後跑 `make build && make test && make lint`（详见規格 `docs/design/F127-issue-first-mr-flow-spec.md` 的驗證慣例）。
- `issue_tracker.enabled` 預設 `false`，關閉時所有既有測試與行為必須零改動（回歸安全網）。
- `internal/gitops` 維持「只做 git plumbing」的職責邊界，forge API 呼叫全部在 `internal/vcshub`。

---

### Task 1: `.4x/settings.json` — `IssueTrackerConfig`

**Files:**
- Modify: `internal/protocol/config.go`
- Test: `internal/protocol/config_issue_tracker_test.go`（新建）

**Interfaces:**
- Produces: `protocol.IssueTrackerConfig{Enabled bool}`、`protocol.Config.IssueTracker IssueTrackerConfig`（json tag `issue_tracker,omitempty`）

- [ ] **Step 1: 寫失敗測試**

```go
// internal/protocol/config_issue_tracker_test.go
package protocol

import (
	"encoding/json"
	"testing"
)

func TestConfig_IssueTracker_DefaultDisabled(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.IssueTracker.Enabled {
		t.Error("IssueTracker.Enabled zero-value should be false")
	}
}

func TestConfig_IssueTracker_ExplicitEnabled(t *testing.T) {
	raw := `{"issue_tracker": {"enabled": true}}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.IssueTracker.Enabled {
		t.Error("IssueTracker.Enabled should be true")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestConfig_IssueTracker -v`
Expected: FAIL — `cfg.IssueTracker undefined (type Config has no field or method IssueTracker)`

- [ ] **Step 3: 實作**

在 `internal/protocol/config.go`，`Config` struct 的 `Evolution *EvolutionConfig` 欄位後面加：

```go
	// IssueTracker 控制是否對外部版控平台（GitHub/GitLab）自動建立 issue 與開 MR/PR；
	// 預設 false，僅明確開啟的專案（如需要 issue-first 流程的新專案）行為改變。
	IssueTracker IssueTrackerConfig `json:"issue_tracker,omitempty"`
```

並在 `EvolutionConfig` struct 定義後面加新 struct：

```go
// IssueTrackerConfig 是 .4x/settings.json 內 issue_tracker 區段的設定。
type IssueTrackerConfig struct {
	// Enabled 開啟後，4x new 會對 feature 宣告的每個 repo 建立/連結 issue，
	// 4x done 會 push branch + 開 MR/PR 取代本地 squash-merge。
	Enabled bool `json:"enabled"`
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestConfig_IssueTracker -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/config.go internal/protocol/config_issue_tracker_test.go
git commit -m "feat(protocol): add issue_tracker.enabled settings.json field"
```

---

### Task 2: Feature YAML — `IssueRef` / `Feature.Issues`

**Files:**
- Modify: `internal/feature/types.go`
- Test: `internal/feature/types_test.go`

**Interfaces:**
- Produces: `feature.IssueRef{Repo, ID, URL string}`、`feature.Feature.Issues []IssueRef`（yaml/json tag `issues,omitempty`）

- [ ] **Step 1: 寫失敗測試**

在 `internal/feature/types_test.go` 加：

```go
func TestFeature_Issues_RoundTrip(t *testing.T) {
	f := Feature{
		ID:     "F999-test",
		Name:   "F999: Test",
		Status: StatusNotStarted,
		Issues: []IssueRef{
			{Repo: ".", ID: "42", URL: "https://github.com/example/repo/issues/42"},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() with Issues set = %v, want nil", err)
	}
	if len(f.Issues) != 1 || f.Issues[0].ID != "42" {
		t.Errorf("Issues = %+v, want single ref with ID=42", f.Issues)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/feature/ -run TestFeature_Issues_RoundTrip -v`
Expected: FAIL — `unknown field Issues in struct literal of type Feature`

- [ ] **Step 3: 實作**

在 `internal/feature/types.go`，`Feature` struct 的 `Warnings []string` 欄位後面加：

```go
	// Issues 記錄這個 feature 在各 repo 建立或連結的 issue（見 issue_tracker.enabled）。
	// Repo 為 "."（monorepo）或 workspace.repos 的 key（multi-repo）。
	Issues []IssueRef `yaml:"issues,omitempty" json:"issues,omitempty"`
```

並在 `Subtask` struct 定義前面加新 struct：

```go
// IssueRef 記錄 feature 在某個 repo 建立或連結的版控平台 issue。
type IssueRef struct {
	Repo string `yaml:"repo" json:"repo"`
	ID   string `yaml:"id" json:"id"`
	URL  string `yaml:"url" json:"url"`
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/feature/ -run TestFeature_Issues_RoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/feature/types.go internal/feature/types_test.go
git commit -m "feat(feature): add IssueRef and Feature.Issues field"
```

---

### Task 3: `internal/vcshub` — `Hub` interface 與平台偵測

**Files:**
- Create: `internal/vcshub/vcshub.go`
- Test: `internal/vcshub/vcshub_test.go`

**Interfaces:**
- Produces:
  - `vcshub.Hub` interface：`Preflight(repoPath string) error`、`CreateIssue(repoPath, title, body string) (id, url string, err error)`、`GetIssue(repoPath, ref string) (id, url string, err error)`、`OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error)`
  - `vcshub.New(repoPath string) Hub`
  - 套件私有：`execCommand func(dir, name string, args ...string) ([]byte, error)`、`lookPath func(file string) (string, error)`（供 Task 4/5 覆寫測試）、`issueIDFromURL(ref string) string`、`extractURL(s string) string`

- [ ] **Step 1: 寫失敗測試**

```go
// internal/vcshub/vcshub_test.go
package vcshub

import (
	"os/exec"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestNew_GitHubRemote_ReturnsGithubHub(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/example/repo.git")

	hub := New(dir)
	if _, ok := hub.(*githubHub); !ok {
		t.Errorf("New() = %T, want *githubHub", hub)
	}
}

func TestNew_GitLabRemote_ReturnsGlabHub(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "git@gitlab.example.com:group/repo.git")

	hub := New(dir)
	if _, ok := hub.(*glabHub); !ok {
		t.Errorf("New() = %T, want *glabHub", hub)
	}
}

func TestNew_NoRemote_DefaultsToGlabHub(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")

	hub := New(dir)
	if _, ok := hub.(*glabHub); !ok {
		t.Errorf("New() = %T, want *glabHub (fallback when remote unknown)", hub)
	}
}

func TestIssueIDFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"42", "42"},
		{"https://github.com/example/repo/issues/42", "42"},
		{"https://gitlab.example.com/group/repo/-/issues/7", "7"},
	}
	for _, c := range cases {
		if got := issueIDFromURL(c.in); got != c.want {
			t.Errorf("issueIDFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractURL(t *testing.T) {
	s := "Merge request created: https://gitlab.example.com/group/repo/-/merge_requests/9\n"
	want := "https://gitlab.example.com/group/repo/-/merge_requests/9"
	if got := extractURL(s); got != want {
		t.Errorf("extractURL() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/vcshub/... -v`
Expected: FAIL — `no non-test Go files in internal/vcshub`（package 尚未建立）

- [ ] **Step 3: 實作**

```go
// internal/vcshub/vcshub.go

// Package vcshub 封裝與版控平台（GitHub/GitLab）API 互動的邏輯——建立/查詢 issue、開 MR/PR。
// 與 internal/gitops（純 git plumbing）職責切開：gitops 不呼叫 gh/glab，vcshub 不碰 worktree/branch。
package vcshub

import (
	"os/exec"
	"regexp"
	"strings"
)

// Hub 封裝單一 repo 對應的版控平台操作。
type Hub interface {
	// Preflight 檢查 CLI 是否已安裝且已登入，供 4x new 動手前快速失敗。
	Preflight(repoPath string) error
	// CreateIssue 建立新 issue，回傳規範化 ID 與完整 URL。
	CreateIssue(repoPath, title, body string) (id, url string, err error)
	// GetIssue 驗證既有 issue 存在（ref 可為純數字 ID 或完整 URL），回傳規範化 ID 與 URL。
	GetIssue(repoPath, ref string) (id, url string, err error)
	// OpenMR 開 MR/PR；若對應 branch 已有開啟中的 MR/PR，回傳既有的 URL 而非視為錯誤（idempotent）。
	OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error)
}

// New 依 repoPath 的 git remote "origin" hostname 自動判斷平台：
// hostname 含 "github.com" 回傳 githubHub，其餘（含自架 GitLab）一律回傳 glabHub。
func New(repoPath string) Hub {
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err == nil && strings.Contains(string(out), "github.com") {
		return &githubHub{}
	}
	return &glabHub{}
}

// execCommand 是可覆寫的指令執行入口，供測試注入假輸出，避免真的呼叫 gh/glab。
var execCommand = func(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// lookPath 是可覆寫的 exec.LookPath，供測試模擬 CLI 未安裝。
var lookPath = exec.LookPath

// issueIDFromURL 從完整 issue/MR URL 取出末段的數字 ID；若 ref 本身已是純 ID（不含 URL scheme）原樣回傳。
func issueIDFromURL(ref string) string {
	if !strings.Contains(ref, "://") {
		return ref
	}
	if idx := strings.LastIndex(ref, "/"); idx != -1 {
		return ref[idx+1:]
	}
	return ref
}

var urlPattern = regexp.MustCompile(`https?://\S+`)

// extractURL 從一段 CLI 輸出文字中找出第一個 http(s) URL，找不到回傳空字串。
func extractURL(s string) string {
	return strings.TrimRight(urlPattern.FindString(s), ".,;:")
}
```

因為 `githubHub`/`glabHub` 尚未定義，這步驟編譯會失敗，屬預期中——Task 4/5 會補上。先建立最小可編譯的空殼：在同一個檔案底部暫時加入（Task 4/5 會取代為完整實作）：

```go
type githubHub struct{}
type glabHub struct{}

func (h *githubHub) Preflight(repoPath string) error                                    { return nil }
func (h *githubHub) CreateIssue(repoPath, title, body string) (string, string, error)    { return "", "", nil }
func (h *githubHub) GetIssue(repoPath, ref string) (string, string, error)               { return "", "", nil }
func (h *githubHub) OpenMR(repoPath, source, target, title, body string) (string, error) { return "", nil }

func (h *glabHub) Preflight(repoPath string) error                                    { return nil }
func (h *glabHub) CreateIssue(repoPath, title, body string) (string, string, error)    { return "", "", nil }
func (h *glabHub) GetIssue(repoPath, ref string) (string, string, error)               { return "", "", nil }
func (h *glabHub) OpenMR(repoPath, source, target, title, body string) (string, error) { return "", nil }
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/vcshub/... -v`
Expected: PASS（`TestNew_*`、`TestIssueIDFromURL`、`TestExtractURL` 全過；空殼方法不影響這些測試）

- [ ] **Step 5: Commit**

```bash
git add internal/vcshub/vcshub.go internal/vcshub/vcshub_test.go
git commit -m "feat(vcshub): add Hub interface and platform auto-detection"
```

---

### Task 4: `internal/vcshub` — `githubHub`（`gh` CLI）

**Files:**
- Create: `internal/vcshub/github.go`
- Modify: `internal/vcshub/vcshub.go`（移除 Task 3 的 `githubHub` 空殼方法與 struct 定義，移到 github.go）
- Test: `internal/vcshub/github_test.go`

**Interfaces:**
- Consumes: `execCommand`、`lookPath`、`issueIDFromURL`、`extractURL`（Task 3 產出）
- Produces: `*githubHub` 完整實作 `Hub` interface

- [ ] **Step 1: 寫失敗測試**

```go
// internal/vcshub/github_test.go
package vcshub

import (
	"errors"
	"reflect"
	"testing"
)

func withFakeExec(t *testing.T, fn func(dir, name string, args ...string) ([]byte, error)) {
	t.Helper()
	orig := execCommand
	execCommand = fn
	t.Cleanup(func() { execCommand = orig })
}

func withFakeLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

func TestGithubHub_Preflight_NotInstalled(t *testing.T) {
	withFakeLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	h := &githubHub{}
	if err := h.Preflight("/tmp/repo"); err == nil {
		t.Error("Preflight() = nil, want error when gh not installed")
	}
}

func TestGithubHub_Preflight_NotAuthenticated(t *testing.T) {
	withFakeLookPath(t, func(string) (string, error) { return "/usr/bin/gh", nil })
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("not logged in"), errors.New("exit status 1")
	})
	h := &githubHub{}
	if err := h.Preflight("/tmp/repo"); err == nil {
		t.Error("Preflight() = nil, want error when gh not authenticated")
	}
}

func TestGithubHub_Preflight_OK(t *testing.T) {
	withFakeLookPath(t, func(string) (string, error) { return "/usr/bin/gh", nil })
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Logged in to github.com"), nil
	})
	h := &githubHub{}
	if err := h.Preflight("/tmp/repo"); err != nil {
		t.Errorf("Preflight() = %v, want nil", err)
	}
}

func TestGithubHub_CreateIssue_Success(t *testing.T) {
	var gotArgs []string
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("https://github.com/example/repo/issues/42\n"), nil
	})
	h := &githubHub{}
	id, url, err := h.CreateIssue("/tmp/repo", "Fix bug", "Steps to reproduce")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if url != "https://github.com/example/repo/issues/42" {
		t.Errorf("url = %q", url)
	}
	want := []string{"gh", "issue", "create", "--title", "Fix bug", "--body", "Steps to reproduce"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestGithubHub_GetIssue_Success(t *testing.T) {
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte(`{"number":42,"url":"https://github.com/example/repo/issues/42"}`), nil
	})
	h := &githubHub{}
	id, url, err := h.GetIssue("/tmp/repo", "42")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if id != "42" || url != "https://github.com/example/repo/issues/42" {
		t.Errorf("GetIssue() = (%q, %q)", id, url)
	}
}

func TestGithubHub_GetIssue_NotFound(t *testing.T) {
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("GraphQL: Could not resolve to an issue"), errors.New("exit status 1")
	})
	h := &githubHub{}
	if _, _, err := h.GetIssue("/tmp/repo", "999"); err == nil {
		t.Error("GetIssue() = nil error, want error for nonexistent issue")
	}
}

func TestGithubHub_OpenMR_Success(t *testing.T) {
	var gotArgs []string
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("https://github.com/example/repo/pull/45\n"), nil
	})
	h := &githubHub{}
	url, err := h.OpenMR("/tmp/repo", "4x/F127", "main", "F127: Issue-first MR flow", "Closes #42")
	if err != nil {
		t.Fatalf("OpenMR: %v", err)
	}
	if url != "https://github.com/example/repo/pull/45" {
		t.Errorf("url = %q", url)
	}
	want := []string{"gh", "pr", "create", "--base", "main", "--head", "4x/F127", "--title", "F127: Issue-first MR flow", "--body", "Closes #42"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestGithubHub_OpenMR_AlreadyExists_ReturnsExistingURL(t *testing.T) {
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("a pull request for branch \"4x/F127\" into branch \"main\" already exists: https://github.com/example/repo/pull/45"),
			errors.New("exit status 1")
	})
	h := &githubHub{}
	url, err := h.OpenMR("/tmp/repo", "4x/F127", "main", "title", "body")
	if err != nil {
		t.Fatalf("OpenMR: %v, want nil (idempotent already-exists)", err)
	}
	if url != "https://github.com/example/repo/pull/45" {
		t.Errorf("url = %q, want existing PR URL", url)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/vcshub/... -run TestGithubHub -v`
Expected: FAIL（空殼方法回傳空值，例如 `TestGithubHub_CreateIssue_Success` 得到 `id=""`）

- [ ] **Step 3: 實作**

刪除 `vcshub.go` 裡 Task 3 加的 `githubHub` 空殼（struct 定義與 4 個方法），新建 `internal/vcshub/github.go`：

```go
package vcshub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type githubHub struct{}

func (h *githubHub) Preflight(repoPath string) error {
	if _, err := lookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found in PATH: %w", err)
	}
	if out, err := execCommand(repoPath, "gh", "auth", "status"); err != nil {
		return fmt.Errorf("gh auth status failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (h *githubHub) CreateIssue(repoPath, title, body string) (id, url string, err error) {
	out, err := execCommand(repoPath, "gh", "issue", "create", "--title", title, "--body", body)
	if err != nil {
		return "", "", fmt.Errorf("gh issue create failed: %s", strings.TrimSpace(string(out)))
	}
	url = strings.TrimSpace(string(out))
	return issueIDFromURL(url), url, nil
}

func (h *githubHub) GetIssue(repoPath, ref string) (id, url string, err error) {
	id = issueIDFromURL(ref)
	out, err := execCommand(repoPath, "gh", "issue", "view", id, "--json", "number,url")
	if err != nil {
		return "", "", fmt.Errorf("gh issue view %s failed: %s", id, strings.TrimSpace(string(out)))
	}
	var parsed struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if jerr := json.Unmarshal(out, &parsed); jerr != nil {
		return "", "", fmt.Errorf("parse gh issue view output: %w", jerr)
	}
	return strconv.Itoa(parsed.Number), parsed.URL, nil
}

func (h *githubHub) OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error) {
	out, err := execCommand(repoPath, "gh", "pr", "create",
		"--base", targetBranch, "--head", sourceBranch, "--title", title, "--body", body)
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(text, "already exists") {
			if u := extractURL(text); u != "" {
				return u, nil
			}
		}
		return "", fmt.Errorf("gh pr create failed: %s", text)
	}
	return text, nil
}
```

在 `vcshub.go` 移除空殼後，確認檔案結尾不再有 `githubHub`/`glabHub` 的重複定義（避免 `glabHub` 空殼與 Task 5 的實作衝突——這步只移除 `githubHub` 部分，保留 `glabHub` 空殼給 Task 5 用）。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/vcshub/... -v`
Expected: PASS 全部（含 Task 3 既有測試與本次新增的 `TestGithubHub_*`）

- [ ] **Step 5: Commit**

```bash
git add internal/vcshub/vcshub.go internal/vcshub/github.go internal/vcshub/github_test.go
git commit -m "feat(vcshub): implement githubHub via gh CLI"
```

---

### Task 5: `internal/vcshub` — `glabHub`（`glab` CLI）

**Files:**
- Create: `internal/vcshub/gitlab.go`
- Modify: `internal/vcshub/vcshub.go`（移除 Task 3 的 `glabHub` 空殼）
- Test: `internal/vcshub/gitlab_test.go`

**Interfaces:**
- Consumes: `execCommand`、`lookPath`、`issueIDFromURL`、`extractURL`（Task 3）
- Produces: `*glabHub` 完整實作 `Hub` interface

- [ ] **Step 1: 寫失敗測試**

```go
// internal/vcshub/gitlab_test.go
package vcshub

import (
	"errors"
	"reflect"
	"testing"
)

func TestGlabHub_Preflight_NotInstalled(t *testing.T) {
	withFakeLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	h := &glabHub{}
	if err := h.Preflight("/tmp/repo"); err == nil {
		t.Error("Preflight() = nil, want error when glab not installed")
	}
}

func TestGlabHub_Preflight_OK(t *testing.T) {
	withFakeLookPath(t, func(string) (string, error) { return "/usr/bin/glab", nil })
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Logged in to gitlab.example.com"), nil
	})
	h := &glabHub{}
	if err := h.Preflight("/tmp/repo"); err != nil {
		t.Errorf("Preflight() = %v, want nil", err)
	}
}

func TestGlabHub_CreateIssue_Success(t *testing.T) {
	var gotArgs []string
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("https://gitlab.example.com/group/repo/-/issues/7\n"), nil
	})
	h := &glabHub{}
	id, url, err := h.CreateIssue("/tmp/repo", "Fix bug", "Steps to reproduce")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if url != "https://gitlab.example.com/group/repo/-/issues/7" {
		t.Errorf("url = %q", url)
	}
	want := []string{"glab", "issue", "create", "--title", "Fix bug", "--description", "Steps to reproduce", "--yes"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestGlabHub_GetIssue_Success(t *testing.T) {
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Title: Fix bug\nURL: https://gitlab.example.com/group/repo/-/issues/7\n"), nil
	})
	h := &glabHub{}
	id, url, err := h.GetIssue("/tmp/repo", "7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if id != "7" || url != "https://gitlab.example.com/group/repo/-/issues/7" {
		t.Errorf("GetIssue() = (%q, %q)", id, url)
	}
}

func TestGlabHub_OpenMR_Success(t *testing.T) {
	var gotArgs []string
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("Merge request created: https://gitlab.example.com/group/repo/-/merge_requests/9\n"), nil
	})
	h := &glabHub{}
	url, err := h.OpenMR("/tmp/repo", "4x/F127", "release/219-ss", "title", "Closes #7")
	if err != nil {
		t.Fatalf("OpenMR: %v", err)
	}
	if url != "https://gitlab.example.com/group/repo/-/merge_requests/9" {
		t.Errorf("url = %q", url)
	}
	want := []string{"glab", "mr", "create", "--target-branch", "release/219-ss", "--title", "title", "--description", "Closes #7", "--yes"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestGlabHub_OpenMR_AlreadyExists_ReturnsExistingURL(t *testing.T) {
	withFakeExec(t, func(dir, name string, args ...string) ([]byte, error) {
		return []byte("a merge request already exists: https://gitlab.example.com/group/repo/-/merge_requests/9"),
			errors.New("exit status 1")
	})
	h := &glabHub{}
	url, err := h.OpenMR("/tmp/repo", "4x/F127", "main", "title", "body")
	if err != nil {
		t.Fatalf("OpenMR: %v, want nil (idempotent already-exists)", err)
	}
	if url != "https://gitlab.example.com/group/repo/-/merge_requests/9" {
		t.Errorf("url = %q, want existing MR URL", url)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/vcshub/... -run TestGlabHub -v`
Expected: FAIL（空殼方法回傳空值）

- [ ] **Step 3: 實作**

刪除 `vcshub.go` 裡剩餘的 `glabHub` 空殼，新建 `internal/vcshub/gitlab.go`：

```go
package vcshub

import (
	"fmt"
	"strings"
)

type glabHub struct{}

func (h *glabHub) Preflight(repoPath string) error {
	if _, err := lookPath("glab"); err != nil {
		return fmt.Errorf("glab CLI not found in PATH: %w", err)
	}
	if out, err := execCommand(repoPath, "glab", "auth", "status"); err != nil {
		return fmt.Errorf("glab auth status failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (h *glabHub) CreateIssue(repoPath, title, body string) (id, url string, err error) {
	out, err := execCommand(repoPath, "glab", "issue", "create", "--title", title, "--description", body, "--yes")
	if err != nil {
		return "", "", fmt.Errorf("glab issue create failed: %s", strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", "", fmt.Errorf("glab issue create: no URL in output: %s", strings.TrimSpace(string(out)))
	}
	return issueIDFromURL(url), url, nil
}

func (h *glabHub) GetIssue(repoPath, ref string) (id, url string, err error) {
	id = issueIDFromURL(ref)
	out, err := execCommand(repoPath, "glab", "issue", "view", id)
	if err != nil {
		return "", "", fmt.Errorf("glab issue view %s failed: %s", id, strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", "", fmt.Errorf("glab issue view %s: no URL in output", id)
	}
	return id, url, nil
}

func (h *glabHub) OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error) {
	out, err := execCommand(repoPath, "glab", "mr", "create",
		"--target-branch", targetBranch, "--title", title, "--description", body, "--yes")
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(text, "already exists") {
			if u := extractURL(text); u != "" {
				return u, nil
			}
		}
		return "", fmt.Errorf("glab mr create failed: %s", text)
	}
	url = extractURL(text)
	if url == "" {
		return "", fmt.Errorf("glab mr create: no URL in output: %s", text)
	}
	return url, nil
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/vcshub/... -v`
Expected: PASS 全部

- [ ] **Step 5: Commit**

```bash
git add internal/vcshub/vcshub.go internal/vcshub/gitlab.go internal/vcshub/gitlab_test.go
git commit -m "feat(vcshub): implement glabHub via glab CLI"
```

---

### Task 6: `internal/gitops` — `PushAndOpenMR`（monorepo）

**Files:**
- Modify: `internal/gitops/gitops.go`（`Ops` interface、`MergeResult.MRUrls`、`vcshubNew` var、`loadBaseline` helper）
- Modify: `internal/gitops/monorepo.go`（`PushAndOpenMR` 實作）
- Test: `internal/gitops/monorepo_test.go`

**Interfaces:**
- Consumes: `vcshub.Hub`、`vcshub.New`（Task 3-5）、既有 `protocol.Baseline`/`BaselineRepo`（`internal/gitops/gitops.go` 既有 `captureRepoBaseline` 產出的結構）、`(*protocol.Workspace).LoadFeature`
- Produces: `Ops.PushAndOpenMR(featureID, featureName string) MergeResult`、`MergeResult.MRUrls map[string]string`

- [ ] **Step 1: 寫失敗測試**

在 `internal/gitops/monorepo_test.go` 加：

```go
func TestMonoRepo_PushAndOpenMR_NoWorktree_Skipped(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	result := ops.PushAndOpenMR("nope", "Nope Feature")
	if !result.Skipped {
		t.Errorf("result = %+v, want Skipped=true", result)
	}
}

func TestMonoRepo_PushAndOpenMR_NoChanges_Skipped(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	if _, err := ops.SetupWorktree("feat-empty", nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	result := ops.PushAndOpenMR("feat-empty", "Empty Feature")
	if !result.Skipped {
		t.Errorf("result = %+v, want Skipped=true (no changes)", result)
	}
}

func TestMonoRepo_PushAndOpenMR_Success(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)

	// 建立 bare remote 讓 push 有地方可推
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare")
	runGit(t, root, "remote", "add", "origin", bareDir)
	runGit(t, root, "push", "origin", "HEAD:refs/heads/main")

	orig := vcshubNew
	defer func() { vcshubNew = orig }()
	vcshubNew = func(repoPath string) vcshub.Hub { return fakeHub{url: "https://github.com/example/repo/pull/1"} }

	wtPath, err := ops.SetupWorktree("feat-mr", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-mr", "wip(feat-mr): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := ops.CaptureBaseline("feat-mr", nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	result := ops.PushAndOpenMR("feat-mr", "Test Feature")
	if result.Error != "" {
		t.Fatalf("PushAndOpenMR error: %s", result.Error)
	}
	if result.MRUrls["."] != "https://github.com/example/repo/pull/1" {
		t.Errorf("MRUrls = %+v, want {.: pull/1 URL}", result.MRUrls)
	}
	if _, err := os.Stat(Dir(root, "feat-mr")); err == nil {
		t.Error("worktree should be cleaned up after PushAndOpenMR")
	}
}

// fakeHub 是 gitops 測試套件共用的 vcshub.Hub 假實作。onCall 為選填的觀察 hook
// （Task 7 用來記錄呼叫了哪個 repoPath），這裡的測試不需要就留 nil。
type fakeHub struct {
	url    string
	onCall func()
}

func (f fakeHub) Preflight(string) error                                     { return nil }
func (f fakeHub) CreateIssue(string, string, string) (string, string, error) { return "", "", nil }
func (f fakeHub) GetIssue(string, string) (string, string, error)            { return "", "", nil }
func (f fakeHub) OpenMR(string, string, string, string, string) (string, error) {
	if f.onCall != nil {
		f.onCall()
	}
	return f.url, nil
}
```

注意：`CaptureBaseline("feat-mr", nil)` 這裡刻意在建立 commit **之後**呼叫只是為了測試方便取得 branch 名稱；真實流程中 `CaptureBaseline` 是在 `4x new`／`4x run` 一開始、`SetupWorktree` 之前呼叫（詳見 `internal/gitops/monorepo.go:194` 既有呼叫點與其呼叫端），這裡不改變既有呼叫順序，只是測試裡為求精簡把兩者都放在同一個測試函式內。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/gitops/... -run TestMonoRepo_PushAndOpenMR -v`
Expected: FAIL — `ops.PushAndOpenMR undefined (type Ops has no field or method PushAndOpenMR)`

- [ ] **Step 3: 實作**

在 `internal/gitops/gitops.go`：

1. `Ops` interface 新增方法（`Merge` 後面）：

```go
	Merge(featureID, featureName string) MergeResult
	PushAndOpenMR(featureID, featureName string) MergeResult
```

2. `MergeResult` struct 新增欄位：

```go
type MergeResult struct {
	Skipped      bool
	Conflict     bool
	Error        string
	Files        []string
	ConflictRepo string
	StateChanged bool
	FinalState   protocol.State
	// MRUrls 記錄 PushAndOpenMR 成功開啟的 MR/PR，key 為 repo 名稱（monorepo 固定 "."，
	// multirepo 為 workspace.repos 的 key），value 為 MR/PR URL。
	MRUrls map[string]string
}
```

3. import 新增 `"encoding/json"` 與 `"github.com/ggwhite/4x/internal/vcshub"`，並在檔案內（例如 `New` 函式後面）加：

```go
// vcshubNew 是可覆寫的 vcshub.New 入口，供測試注入假的 Hub 實作。
var vcshubNew = vcshub.New

// loadBaseline 讀取 CaptureBaseline 寫入的 baseline.json；供 PushAndOpenMR 取得 MR target branch。
func loadBaseline(ws *protocol.Workspace, featureID string) (protocol.Baseline, error) {
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile))
	if err != nil {
		return protocol.Baseline{}, err
	}
	var b protocol.Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return protocol.Baseline{}, err
	}
	return b, nil
}
```

在 `internal/gitops/monorepo.go`（`Merge` 方法後面）新增：

```go
// PushAndOpenMR 是 Merge 的替代路徑（cfg.IssueTracker.Enabled 時使用）：push feature branch
// 到 origin 並開 MR/PR，取代本地 squash-merge。target branch 讀 CaptureBaseline 記錄的
// baseline branch；issue 參照讀 feature YAML 的 Issues，組進 MR body 的 "Closes #<id>"。
func (m *monoRepo) PushAndOpenMR(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}
	branch := Branch(featureID)

	if len(m.DetectChangedFiles(featureID)) == 0 {
		m.Cleanup(featureID) //nolint:errcheck // best-effort 清理，無變更時沒有東西可能失敗
		return MergeResult{Skipped: true}
	}

	if out, err := exec.Command("git", "-C", m.root, "push", "origin", branch).CombinedOutput(); err != nil {
		return MergeResult{Error: fmt.Sprintf("git push: %s", strings.TrimSpace(string(out)))}
	}

	baseline, err := loadBaseline(m.ws, featureID)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("read baseline: %v", err)}
	}
	target := "main"
	if len(baseline.Repos) > 0 && baseline.Repos[0].Branch != "" {
		target = baseline.Repos[0].Branch
	}

	feat, err := m.ws.LoadFeature(featureID)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("load feature: %v", err)}
	}
	body := featureName
	for _, ref := range feat.Issues {
		if ref.Repo == "." {
			body = fmt.Sprintf("Closes #%s\n\n%s", ref.ID, featureName)
			break
		}
	}

	url, err := vcshubNew(m.root).OpenMR(m.root, branch, target, featureName, body)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("open MR: %v", err)}
	}

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響 MR 已開的結果
	return MergeResult{MRUrls: map[string]string{".": url}}
}
```

在 `internal/gitops/monorepo_test.go` 檔案開頭補上測試需要的 import（`os/exec` 已由既有 `runGit` 使用；新增 `"github.com/ggwhite/4x/internal/vcshub"`）。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/gitops/... -v`
Expected: PASS 全部（含既有 `Merge`/`Cleanup`/`CaptureBaseline` 測試不受影響）

- [ ] **Step 5: Commit**

```bash
git add internal/gitops/gitops.go internal/gitops/monorepo.go internal/gitops/monorepo_test.go
git commit -m "feat(gitops): add PushAndOpenMR for monorepo mode"
```

---

### Task 7: `internal/gitops` — `PushAndOpenMR`（multirepo）

**Files:**
- Modify: `internal/gitops/multirepo.go`
- Test: `internal/gitops/multirepo_test.go`

**Interfaces:**
- Consumes: `vcshubNew`、`loadBaseline`（Task 6）、既有 `(*multiRepo).targetRepos`、`DetectChangedRepos`
- Produces: `(*multiRepo).PushAndOpenMR(featureID, featureName string) MergeResult`

- [ ] **Step 1: 寫失敗測試**

在 `internal/gitops/multirepo_test.go` 加（假設既有測試已有 `setupMultiWorkspace` 或等價 helper 建立兩個 repo `repo-a`/`repo-b` 並在 `.4x/settings.json` 設好 `workspace.repos`；沿用該檔既有的 workspace 建置 helper，若名稱不同以檔案內既有名稱為準）：

```go
func TestMultiRepo_PushAndOpenMR_Success(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t) // 沿用既有 helper：repo-a、repo-b 兩個 repo

	orig := vcshubNew
	defer func() { vcshubNew = orig }()
	calls := map[string]bool{}
	vcshubNew = func(repoPath string) vcshub.Hub {
		return fakeHub{url: "https://gitlab.example.com/group/repo-a/-/merge_requests/1", onCall: func() { calls[repoPath] = true }}
	}

	wtPath, err := ops.SetupWorktree("feat-multi-mr", []string{"repo-a"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	repoAWt := filepath.Join(wtPath, "repo-a")
	if err := os.WriteFile(filepath.Join(repoAWt, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(repoAWt, "feat-multi-mr", "wip(feat-multi-mr): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := ops.CaptureBaseline("feat-multi-mr", []string{"repo-a"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	// repo-a 需要一個 bare remote 才能 push
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare")
	repoAPath := filepath.Join(root, "repo-a")
	runGit(t, repoAPath, "remote", "add", "origin", bareDir)
	runGit(t, repoAPath, "push", "origin", "HEAD:refs/heads/main")

	result := ops.PushAndOpenMR("feat-multi-mr", "Multi Feature")
	if result.Error != "" {
		t.Fatalf("PushAndOpenMR error: %s", result.Error)
	}
	if result.MRUrls["repo-a"] == "" {
		t.Errorf("MRUrls = %+v, want repo-a entry", result.MRUrls)
	}
}
```

`fakeHub` 沿用 Task 6 在 `monorepo_test.go` 定義的共用型別（同一個 `gitops` 測試 package，`onCall` 欄位就是為了這裡的用途保留的），這裡不重複定義。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/gitops/... -run TestMultiRepo_PushAndOpenMR -v`
Expected: FAIL — `ops.PushAndOpenMR undefined` 或編譯錯誤（`multiRepo` 未實作該方法）

- [ ] **Step 3: 實作**

在 `internal/gitops/multirepo.go`（`Merge` 方法後面）新增：

```go
// PushAndOpenMR 是 multi-repo 版本的 Merge 替代路徑：對每個「真的有變更」的 repo 各自 push +
// 開 MR/PR，partial-tolerant——單一 repo 失敗記進 result.Error，其他 repo 照常完成。
func (m *multiRepo) PushAndOpenMR(featureID, featureName string) MergeResult {
	wtDir := Dir(m.root, featureID)
	if _, err := os.Stat(wtDir); err != nil {
		return MergeResult{Skipped: true}
	}
	branch := Branch(featureID)

	changed := m.DetectChangedRepos(featureID)
	if len(changed) == 0 {
		m.cleanupPartial(wtDir, featureID)
		return MergeResult{Skipped: true}
	}

	baseline, err := loadBaseline(m.ws, featureID)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("read baseline: %v", err)}
	}
	branchFor := map[string]string{}
	for _, br := range baseline.Repos {
		branchFor[br.Name] = br.Branch
	}

	feat, err := m.ws.LoadFeature(featureID)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("load feature: %v", err)}
	}
	issueFor := map[string]string{}
	for _, ref := range feat.Issues {
		issueFor[ref.Repo] = ref.ID
	}

	result := MergeResult{MRUrls: map[string]string{}}
	var errs []string
	for name, rc := range m.targetRepos(changed) {
		repoPath := filepath.Join(m.root, rc.Path)

		if out, err := exec.Command("git", "-C", repoPath, "push", "origin", branch).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: push failed: %s", name, strings.TrimSpace(string(out))))
			continue
		}

		target := branchFor[name]
		if target == "" {
			target = "main"
		}
		body := featureName
		if id := issueFor[name]; id != "" {
			body = fmt.Sprintf("Closes #%s\n\n%s", id, featureName)
		}

		url, err := vcshubNew(repoPath).OpenMR(repoPath, branch, target, featureName, body)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: open MR failed: %v", name, err))
			continue
		}
		result.MRUrls[name] = url
	}

	if len(errs) > 0 {
		result.Error = strings.Join(errs, "; ")
	}

	m.Cleanup(featureID) //nolint:errcheck // best-effort worktree 清理，失敗不影響已開的 MR
	return result
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/gitops/... -v`
Expected: PASS 全部

- [ ] **Step 5: Commit**

```bash
git add internal/gitops/multirepo.go internal/gitops/multirepo_test.go
git commit -m "feat(gitops): add PushAndOpenMR for multi-repo mode"
```

---

### Task 8: `internal/gitops/finalize.go` — 依設定分支

**Files:**
- Modify: `internal/gitops/finalize.go`
- Test: `internal/gitops/finalize_test.go`

**Interfaces:**
- Consumes: `Ops.PushAndOpenMR`（Task 6/7）、`protocol.Config.IssueTracker.Enabled`（Task 1）
- Produces: `MergeAndFinalize` 在 `cfg.IssueTracker.Enabled` 時走 `PushAndOpenMR`

- [ ] **Step 1: 寫失敗測試**

在 `internal/gitops/finalize_test.go` 加：

```go
func TestMergeAndFinalize_IssueTrackerEnabled_UsesPushAndOpenMR(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	cfg.IssueTracker.Enabled = true

	orig := vcshubNew
	defer func() { vcshubNew = orig }()
	vcshubNew = func(repoPath string) vcshub.Hub {
		return fakeHub{url: "https://github.com/example/repo/pull/9"}
	}

	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare")
	runGit(t, root, "remote", "add", "origin", bareDir)
	runGit(t, root, "push", "origin", "HEAD:refs/heads/main")

	wtPath, err := ops.SetupWorktree("feat-issue-done", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-issue-done", "wip: round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := ops.CaptureBaseline("feat-issue-done", nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}
	writeState(t, ws, "feat-issue-done", protocol.PhasePendingReview)

	result, err := MergeAndFinalize(root, ws, cfg, "feat-issue-done", "Test Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.MRUrls["."] != "https://github.com/example/repo/pull/9" {
		t.Errorf("MRUrls = %+v, want MR opened", result.MRUrls)
	}
	if result.FinalState.Phase != protocol.PhaseDone {
		t.Errorf("FinalState.Phase = %q, want done", result.FinalState.Phase)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/gitops/... -run TestMergeAndFinalize_IssueTrackerEnabled -v`
Expected: FAIL — `result.MRUrls` 為 nil（現在 `MergeAndFinalize` 一律呼叫 `ops.Merge`，沒有依 `cfg.IssueTracker.Enabled` 分支）

- [ ] **Step 3: 實作**

在 `internal/gitops/finalize.go`，把：

```go
	ops := New(root, ws, cfg)
	result := ops.Merge(featureID, featureName)
```

改成：

```go
	ops := New(root, ws, cfg)
	var result MergeResult
	if cfg.IssueTracker.Enabled {
		result = ops.PushAndOpenMR(featureID, featureName)
	} else {
		result = ops.Merge(featureID, featureName)
	}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/gitops/... -v`
Expected: PASS 全部（含既有兩個 `TestMergeAndFinalize_*` 測試——`cfg.IssueTracker.Enabled` 預設 false 時行為完全不變）

- [ ] **Step 5: Commit**

```bash
git add internal/gitops/finalize.go internal/gitops/finalize_test.go
git commit -m "feat(gitops): MergeAndFinalize routes to PushAndOpenMR when issue_tracker enabled"
```

---

### Task 9: `cmd/4x/new.go` — preflight + 建立/連結 issue

**Files:**
- Modify: `cmd/4x/new.go`
- Test: `cmd/4x/new_test.go`（若不存在則新建）

**Interfaces:**
- Consumes: `vcshub.New`、`vcshub.Hub`（Task 3-5）、`feature.IssueRef`（Task 2）、`protocol.Config.IssueTracker.Enabled`（Task 1）
- Produces: `4x new --issue "<repo>:<id或URL>"` flag；`parseIssueRef(s string) (repo, ref string)`

- [ ] **Step 1: 寫失敗測試**

```go
// cmd/4x/new_test.go
package main

import "testing"

func TestParseIssueRef(t *testing.T) {
	cases := []struct {
		in       string
		wantRepo string
		wantRef  string
	}{
		{"456", "", "456"},
		{"old-bi:456", "old-bi", "456"},
		{"https://github.com/org/repo/issues/5", "", "https://github.com/org/repo/issues/5"},
		{"old-bi:https://gitlab.example.com/o/r/-/issues/5", "old-bi", "https://gitlab.example.com/o/r/-/issues/5"},
	}
	for _, c := range cases {
		repo, ref := parseIssueRef(c.in)
		if repo != c.wantRepo || ref != c.wantRef {
			t.Errorf("parseIssueRef(%q) = (%q, %q), want (%q, %q)", c.in, repo, ref, c.wantRepo, c.wantRef)
		}
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/... -run TestParseIssueRef -v`
Expected: FAIL — `undefined: parseIssueRef`

- [ ] **Step 3: 實作**

在 `cmd/4x/new.go` 檔案開頭 import 區塊加入 `"path/filepath"`、`"regexp"`、`"github.com/ggwhite/4x/internal/protocol"`（`protocol` 已匯入，跳過重複）、`"github.com/ggwhite/4x/internal/vcshub"`。

在 `parseSubtask` 函式後面加入：

```go
var issueRefPrefixPattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+):(.+)$`)

// parseIssueRef 解析 "repo:id或URL" 或單純 "id或URL" 格式。
// 明確是 http(s) URL 時一律視為無 repo 前綴（避免 URL 的 "://" 被誤判成分隔符）。
func parseIssueRef(s string) (repo, ref string) {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return "", s
	}
	if m := issueRefPrefixPattern.FindStringSubmatch(s); m != nil {
		return m[1], m[2]
	}
	return "", s
}

// repoPath 依 repo 名稱解析出實際檔案路徑："." 或 monorepo（無 workspace.repos）時回傳 root，
// 否則查 cfg.Workspace.Repos 對應的 RepoConfig.Path。
func repoPath(root string, cfg protocol.Config, name string) string {
	if name == "." || len(cfg.Workspace.Repos) == 0 {
		return root
	}
	if rc, ok := cfg.Workspace.Repos[name]; ok {
		return filepath.Join(root, rc.Path)
	}
	return root
}
```

在 `newNewCmd()` 開頭的 `var (...)` 區塊裡，`repos []string` 那一行後面加一行 `issues []string`。並在 `cmd.Flags().StringSliceVar(&repos, "repo", nil, "repos involved (can be repeated)")` 那一行後面加：

```go
	cmd.Flags().StringSliceVar(&issues, "issue", nil,
		`link an existing issue instead of creating one, "repo:id-or-url" or "id-or-url" for single-repo features (can be repeated)`)
```

在 `RunE` 內，緊接在 `opts := feature.CreateOpts{...}` 那個 struct literal 與後面 `if cmd.Flags().Changed("priority") { ... }` 區塊之後、**`f, err := feature.Create(ws, opts)` 這一行之前**插入（此時 `cfg`、`opts` 都已建好，`opts.Repos` 就是 `--repo` flag 的值）：

```go
			featureRepos := opts.Repos
			if len(featureRepos) == 0 {
				featureRepos = []string{"."}
			}
			issueByRepo := map[string]string{}
			for _, raw := range issues {
				repo, ref := parseIssueRef(raw)
				if repo == "" {
					repo = featureRepos[0]
				}
				issueByRepo[repo] = ref
			}

			if cfg.IssueTracker.Enabled {
				for _, name := range featureRepos {
					path := repoPath(cwd, cfg, name)
					if perr := vcshub.New(path).Preflight(path); perr != nil {
						err := fmt.Errorf("issue tracker preflight failed for %s: %w", name, perr)
						if jsonOutput {
							return jsonError(err.Error())
						}
						return err
					}
				}
			}
```

在 `f, err := feature.Create(ws, opts)` 成功之後（`if err != nil { ... }` 區塊之後），`if jsonOutput { ... }` 之前，加入建立/連結 issue：

```go
			if cfg.IssueTracker.Enabled {
				for _, name := range featureRepos {
					path := repoPath(cwd, cfg, name)
					hub := vcshub.New(path)
					var id, url string
					var ierr error
					if ref, ok := issueByRepo[name]; ok {
						id, url, ierr = hub.GetIssue(path, ref)
					} else {
						id, url, ierr = hub.CreateIssue(path, f.Name, f.Description)
					}
					if ierr != nil {
						f.Warnings = append(f.Warnings, fmt.Sprintf("issue for %s: %v", name, ierr))
						continue
					}
					f.Issues = append(f.Issues, feature.IssueRef{Repo: name, ID: id, URL: url})
				}
				if serr := ws.SaveFeature(f); serr != nil {
					if jsonOutput {
						return jsonError(serr.Error())
					}
					return serr
				}
			}
```

最後在非 JSON 輸出區塊（`fmt.Println()` 之前）印出 issue URL：

```go
			for _, ref := range f.Issues {
				fmt.Printf("  Issue (%s): %s\n", ref.Repo, ref.URL)
			}
```

（放在既有 `if len(parsedSubtasks) > 0 { ... }` 區塊之後。）

`strings` 已在檔案開頭 import，`parseIssueRef` 使用的 `strings.HasPrefix` 不需額外 import。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/... -run TestParseIssueRef -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/new.go cmd/4x/new_test.go
git commit -m "feat(cli): 4x new creates/links issues when issue_tracker enabled"
```

---

### Task 10: `cmd/4x/done.go` — 印出 MR URL

**Files:**
- Modify: `cmd/4x/done.go`

**Interfaces:**
- Consumes: `gitops.MergeResult.MRUrls`（Task 6/7）

- [ ] **Step 1: 寫失敗測試**

在 `cmd/4x/done_test.go`（若不存在則新建；若已存在既有 `TestMarkDone_*` 測試，沿用檔案內既有的 workspace 建置 helper）加：

```go
func TestDoneResult_MRUrls_JSONField(t *testing.T) {
	r := doneResult{FeatureID: "F127", Phase: "done", Merged: true, MRUrls: map[string]string{".": "https://github.com/example/repo/pull/1"}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mrUrls"`) {
		t.Errorf("json output missing mrUrls field: %s", data)
	}
}
```

（需要 `encoding/json`、`strings` import；若 `done_test.go` 已存在且已 import，沿用既有 import 區塊。）

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/... -run TestDoneResult_MRUrls -v`
Expected: FAIL — `unknown field MRUrls in struct literal of type doneResult`

- [ ] **Step 3: 實作**

在 `cmd/4x/done.go` 的 `doneResult` struct 加欄位：

```go
type doneResult struct {
	FeatureID string            `json:"featureId"`
	Phase     string            `json:"phase,omitempty"`
	Merged    bool              `json:"merged"`
	Conflict  bool              `json:"conflict"`
	MRUrls    map[string]string `json:"mrUrls,omitempty"`
}
```

在 `markDone` 成功分支，把：

```go
	if jsonOutput {
		return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhaseDone), Merged: !result.Skipped})
	}
	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	return nil
```

改成：

```go
	if jsonOutput {
		return printJSON(doneResult{FeatureID: featureID, Phase: string(protocol.PhaseDone), Merged: !result.Skipped, MRUrls: result.MRUrls})
	}
	fmt.Printf("Feature %s marked as done.\n", featureID)
	if !result.Skipped {
		fmt.Printf("Merged and cleaned up branch %s.\n", gitops.Branch(featureID))
	}
	for repo, url := range result.MRUrls {
		if repo == "." {
			fmt.Printf("MR opened: %s\n", url)
		} else {
			fmt.Printf("MR opened (%s): %s\n", repo, url)
		}
	}
	return nil
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./cmd/4x/... -v`
Expected: PASS 全部

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/done.go cmd/4x/done_test.go
git commit -m "feat(cli): 4x done prints opened MR/PR URLs"
```

---

### Task 11: 全量驗證

**Files:** 無新增/修改

- [ ] **Step 1: 全量 build/test/lint**

Run: `make build && make test && make lint`
Expected: 全部通過，無新增 lint 警告

- [ ] **Step 2: 手動確認 `issue_tracker.enabled` 預設關閉時零回歸**

Run: `git stash && make test && git stash pop`（比對 stash 前後 `internal/gitops`、`internal/feature`、`internal/protocol`、`cmd/4x` 的測試數量與結果一致，僅新增測試通過，無既有測試被改壞）

- [ ] **Step 3: Commit（若前兩步有任何修正）**

```bash
git add -A
git commit -m "fix: address build/test/lint findings for F127"
```

（若前兩步全部乾淨通過，跳過此步，不建立空 commit。）

---

## 自我檢查（撰寫完畢後）

- **Spec 覆蓋**：settings.json 開關（Task 1）、Feature.Issues/Warnings（Task 2）、vcshub 平台偵測+preflight（Task 3）、gh/glab 實作（Task 4/5）、PushAndOpenMR mono/multi（Task 6/7）、finalize 分支（Task 8）、4x new 串接與 `--issue`（Task 9）、4x done 輸出（Task 10）——spec 五大設計段落與「連結既有 issue」章節全部對應到任務。
- **簽名一致性**：`Hub` interface 方法簽名在 Task 3 定義後，Task 4/5/6/7/9 全部沿用同一組（`Preflight(repoPath string) error`、`CreateIssue(repoPath, title, body string) (id, url string, err error)`、`GetIssue(repoPath, ref string) (id, url string, err error)`、`OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error)`），`vcshubNew`/`loadBaseline` 在 Task 6 定義後 Task 7/8 沿用同名。
- **不做的事**：計畫未新增輪詢/webhook、未做 issue 自動關閉、未支援 Bitbucket，與 spec「不做的事」一致。
