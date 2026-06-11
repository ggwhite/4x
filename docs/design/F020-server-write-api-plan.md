# F020 — Server Write API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 為 4x live server 加入 write endpoints（run/stop/new/runs）和 process manager，讓 Dashboard 能觸發操作。

**Architecture:** 新增 `internal/server/process.go` 放 ProcessManager（subprocess 生命週期管理），擴充 `server.go` 加 write handler。Feature ID 生成邏輯從 `cmd/4x/new.go` 搬到 `internal/protocol/feature.go` 以供 server 共用。

**Tech Stack:** Go stdlib（os/exec, net/http, sync），既有 protocol/workspace API

---

### Task 1: 搬移 feature ID 生成邏輯到 protocol package

`POST /api/new` 需要 `nextFeatureNumber` 和 `generateID`，目前在 `cmd/4x/new.go`（package main），server package 無法引用。搬到 protocol package 讓兩邊共用。

**Files:**
- Create: `internal/protocol/feature.go`
- Create: `internal/protocol/feature_test.go`
- Modify: `cmd/4x/new.go`

- [ ] **Step 1: 寫 feature_test.go 的測試**

```go
// internal/protocol/feature_test.go
package protocol

import "testing"

func TestGenerateFeatureID(t *testing.T) {
	tests := []struct {
		num  int
		name string
		want string
	}{
		{1, "My Feature", "F001-my-feature"},
		{25, "Server Write API", "F025-server-write-api"},
		{100, "A very long feature name that exceeds the limit", "F100-a-very-long-feature-nam"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
		}
	}
}

func TestNextFeatureNumber(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}

	// 空 workspace → 回傳 1
	n, err := NextFeatureNumber(ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	// 建一個 F003 → 回傳 4
	f := Feature{ID: "F003-test", Name: "test", Status: "not-started"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	n, err = NextFeatureNumber(ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("got %d, want 4", n)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run "TestGenerateFeatureID|TestNextFeatureNumber" -v`
Expected: FAIL — `GenerateFeatureID` 和 `NextFeatureNumber` 未定義

- [ ] **Step 3: 建立 feature.go，實作 exported 函式**

```go
// internal/protocol/feature.go
package protocol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	nonAlphaNum  = regexp.MustCompile(`[^a-z0-9]+`)
	featureNumRe = regexp.MustCompile(`^F(\d{3})-`)
)

// NextFeatureNumber 掃描現有 feature，回傳下一個可用流水號
func NextFeatureNumber(ws *Workspace) (int, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return 1, nil
	}
	max := 0
	for _, f := range features {
		if m := featureNumRe.FindStringSubmatch(f.ID); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

// GenerateFeatureID 產生 F{NNN}-{slug} 格式的 feature ID
func GenerateFeatureID(num int, name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 25 {
		slug = slug[:25]
		slug = strings.TrimRight(slug, "-")
	}
	return fmt.Sprintf("F%03d-%s", num, slug)
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run "TestGenerateFeatureID|TestNextFeatureNumber" -v`
Expected: PASS

- [ ] **Step 5: 改 cmd/4x/new.go 呼叫 protocol package**

把 `new.go` 裡的 `nextFeatureNumber`、`generateID`、`nonAlphaNum`、`featureNumRe` 全部刪除，改用 `protocol.NextFeatureNumber` 和 `protocol.GenerateFeatureID`：

```go
// cmd/4x/new.go — RunE 內
next, err := protocol.NextFeatureNumber(ws)
if err != nil {
    return err
}
id := protocol.GenerateFeatureID(next, name)
```

刪除 `new.go` 第 69-101 行（`nonAlphaNum`、`featureNumRe`、`nextFeatureNumber`、`generateID`）。

- [ ] **Step 6: 跑全部測試確認沒有 regression**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add internal/protocol/feature.go internal/protocol/feature_test.go cmd/4x/new.go
git commit -m "refactor: move feature ID generation to protocol package"
```

---

### Task 2: Config 新增 MaxConcurrentRuns 欄位

**Files:**
- Modify: `internal/protocol/types.go:194-201`

- [ ] **Step 1: 在 Config struct 加欄位**

在 `internal/protocol/types.go` 的 `Config` struct 加一個欄位：

```go
type Config struct {
	Project           ProjectConfig           `json:"project"`
	Runners           map[string]RunnerConfig `json:"runners"`
	Default           string                  `json:"default_runner"`
	Roles             map[string]RoleConfig   `json:"roles,omitempty"`
	Rules             []string                `json:"rules,omitempty"`
	HubRepos          []string                `json:"hub_repos,omitempty"`
	MaxConcurrentRuns int                     `json:"max_concurrent_runs,omitempty"`
}
```

- [ ] **Step 2: 確認 build 通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 成功

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat: add MaxConcurrentRuns to Config"
```

---

### Task 3: ProcessManager 核心邏輯

**Files:**
- Create: `internal/server/process.go`
- Create: `internal/server/process_test.go`

- [ ] **Step 1: 寫 process_test.go**

```go
// internal/server/process_test.go
package server

import (
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupPMWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "pm-test"},
		Runners: map[string]protocol.RunnerConfig{
			"test": {Command: "echo", Args: []string{"hello"}},
		},
		Default: "test",
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	f := protocol.Feature{ID: "test-feat", Name: "Test", Status: "not-started"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("test-feat"); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestProcessManager_StartAndList(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, "sleep")

	info, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.FeatureID != "test-feat" {
		t.Errorf("FeatureID = %q, want test-feat", info.FeatureID)
	}

	runs := pm.List()
	if len(runs) != 1 {
		t.Fatalf("List() = %d runs, want 1", len(runs))
	}
	if runs[0].ID != info.ID {
		t.Errorf("List()[0].ID = %q, want %q", runs[0].ID, info.ID)
	}

	pm.Shutdown()
}

func TestProcessManager_Stop(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, "sleep")

	info, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if err := pm.Stop(info.ID); err != nil {
		t.Fatal(err)
	}

	// 等 goroutine 清理
	time.Sleep(100 * time.Millisecond)

	runs := pm.List()
	if len(runs) != 0 {
		t.Errorf("List() = %d runs after stop, want 0", len(runs))
	}

	pm.Shutdown()
}

func TestProcessManager_MaxParallel(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 1, "sleep")

	_, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}

	// 第二個應該失敗
	_, err = pm.Start("test-feat", "", 5)
	if err == nil {
		t.Error("expected error for exceeding max parallel, got nil")
	}

	pm.Shutdown()
}

func TestProcessManager_Shutdown(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, "sleep")

	_, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}

	pm.Shutdown()

	// shutdown 後應該清空
	time.Sleep(100 * time.Millisecond)
	runs := pm.List()
	if len(runs) != 0 {
		t.Errorf("List() = %d runs after shutdown, want 0", len(runs))
	}
}

func TestProcessManager_StopNotFound(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 1, "sleep")
	defer pm.Shutdown()

	err := pm.Stop("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent run, got nil")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run "TestProcessManager" -v`
Expected: FAIL — `NewProcessManager` 未定義

- [ ] **Step 3: 實作 process.go**

```go
// internal/server/process.go
package server

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/ggwhite/4x/internal/protocol"
)

// RunInfo 描述一個執行中的 4x run subprocess
type RunInfo struct {
	ID        string    `json:"id"`
	FeatureID string    `json:"featureId"`
	Runner    string    `json:"runner"`
	StartTime time.Time `json:"startTime"`
	cmd       *exec.Cmd
}

// ProcessManager 管理 4x run subprocess 的生命週期
type ProcessManager struct {
	mu          sync.Mutex
	runs        map[string]*RunInfo
	maxParallel int
	ws          *protocol.Workspace
	binName     string
}

// NewProcessManager 建立 ProcessManager，binName 通常是 "4x"，測試時替換為 "sleep"
func NewProcessManager(ws *protocol.Workspace, maxParallel int, binName string) *ProcessManager {
	if maxParallel <= 0 {
		maxParallel = 1
	}
	return &ProcessManager{
		runs:        make(map[string]*RunInfo),
		maxParallel: maxParallel,
		ws:          ws,
		binName:     binName,
	}
}

// Start 啟動一個 4x run subprocess
func (pm *ProcessManager) Start(featureID, runner string, maxRounds int) (*RunInfo, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.runs) >= pm.maxParallel {
		return nil, fmt.Errorf("max concurrent runs reached (%d)", pm.maxParallel)
	}

	args := pm.buildArgs(featureID, runner, maxRounds)
	cmd := exec.Command(pm.binName, args...)
	cmd.Dir = pm.ws.Root

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start subprocess: %w", err)
	}

	id := uuid.New().String()
	info := &RunInfo{
		ID:        id,
		FeatureID: featureID,
		Runner:    runner,
		StartTime: time.Now(),
		cmd:       cmd,
	}
	pm.runs[id] = info

	go pm.pipeOutput(featureID, stdout, "run-output")
	go pm.pipeOutput(featureID, stderr, "run-error")
	go pm.wait(id)

	return info, nil
}

func (pm *ProcessManager) buildArgs(featureID, runner string, maxRounds int) []string {
	args := []string{"run", featureID}
	if runner != "" {
		args = append(args, "--runner", runner)
	}
	if maxRounds > 0 {
		args = append(args, "--max-rounds", strconv.Itoa(maxRounds))
	}
	return args
}

func (pm *ProcessManager) pipeOutput(featureID string, pipe interface{ Read([]byte) (int, error) }, eventType string) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		evt := protocol.Event{
			Type:   eventType,
			Detail: line,
			Round:  0,
		}
		pm.ws.AppendEvent(featureID, evt)
	}
}

func (pm *ProcessManager) wait(id string) {
	pm.mu.Lock()
	info, ok := pm.runs[id]
	pm.mu.Unlock()
	if !ok {
		return
	}

	info.cmd.Wait()

	pm.mu.Lock()
	delete(pm.runs, id)
	pm.mu.Unlock()
}

// Stop 終止指定的 run（SIGTERM → 5s → SIGKILL）
func (pm *ProcessManager) Stop(id string) error {
	pm.mu.Lock()
	info, ok := pm.runs[id]
	pm.mu.Unlock()

	if !ok {
		return fmt.Errorf("run %q not found", id)
	}

	return pm.killProcess(info.cmd)
}

// List 回傳所有執行中的 run
func (pm *ProcessManager) List() []RunInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var result []RunInfo
	for _, info := range pm.runs {
		result = append(result, RunInfo{
			ID:        info.ID,
			FeatureID: info.FeatureID,
			Runner:    info.Runner,
			StartTime: info.StartTime,
		})
	}
	return result
}

// Shutdown 終止所有 subprocess
func (pm *ProcessManager) Shutdown() {
	pm.mu.Lock()
	runs := make([]*RunInfo, 0, len(pm.runs))
	for _, info := range pm.runs {
		runs = append(runs, info)
	}
	pm.mu.Unlock()

	for _, info := range runs {
		pm.killProcess(info.cmd)
	}

	// 等 wait goroutine 清理
	time.Sleep(50 * time.Millisecond)
}

func (pm *ProcessManager) killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		return nil
	}
}
```

注意：需要加 `github.com/google/uuid` 依賴。

- [ ] **Step 4: 加 uuid 依賴**

Run: `go get github.com/google/uuid`

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/server/ -run "TestProcessManager" -v -timeout 30s`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/process.go internal/server/process_test.go go.mod go.sum
git commit -m "feat: add ProcessManager for subprocess lifecycle"
```

---

### Task 4: Write Endpoints — POST /api/run 和 GET /api/runs

**Files:**
- Modify: `internal/server/server.go:21,48`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫 server_test.go 新增測試**

在 `server_test.go` 加：

```go
func setupServerWithPM(t *testing.T) (*protocol.Workspace, *ProcessManager) {
	t.Helper()
	ws := setupServerWorkspace(t)
	pm := NewProcessManager(ws, 2, "sleep")
	return ws, pm
}

func TestPostRun(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	body := `{"featureId":"test-feat","maxRounds":3}`
	resp, err := http.Post(srv.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var info RunInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.FeatureID != "test-feat" {
		t.Errorf("FeatureID = %q, want test-feat", info.FeatureID)
	}
}

func TestPostRun_Conflict(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	pm2 := NewProcessManager(ws, 1, "sleep") // 上限 1
	defer pm2.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm2))
	defer srv.Close()

	body := `{"featureId":"test-feat"}`
	resp1, _ := http.Post(srv.URL+"/api/run", "application/json", strings.NewReader(body))
	resp1.Body.Close()

	resp2, err := http.Post(srv.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 409 {
		t.Fatalf("status = %d, want 409", resp2.StatusCode)
	}
}

func TestGetRuns(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	// 先啟動一個 run
	body := `{"featureId":"test-feat"}`
	resp, _ := http.Post(srv.URL+"/api/run", "application/json", strings.NewReader(body))
	resp.Body.Close()

	resp2, err := http.Get(srv.URL + "/api/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var runs []RunInfo
	if err := json.NewDecoder(resp2.Body).Decode(&runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run "TestPostRun|TestGetRuns" -v`
Expected: FAIL — `NewMux` 簽名不匹配

- [ ] **Step 3: 改 NewMux 簽名，加 run/runs handler**

修改 `server.go`：

`NewMux` 改簽名為 `func NewMux(ws *protocol.Workspace, pm *ProcessManager) http.Handler`。

`Start` 改簽名為 `func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error`，內部呼叫 `NewMux(ws, pm)`。

在 mux 裡加：

```go
mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handlePostRun(ws, pm, w, r)
})
mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
	handleGetRuns(pm, w)
})
```

加 handler 函式：

```go
type runRequest struct {
	FeatureID string `json:"featureId"`
	Runner    string `json:"runner"`
	MaxRounds int    `json:"maxRounds"`
}

func handlePostRun(ws *protocol.Workspace, pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.FeatureID == "" {
		http.Error(w, "featureId required", http.StatusBadRequest)
		return
	}

	if _, err := ws.LoadFeature(req.FeatureID); err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	info, err := pm.Start(req.FeatureID, req.Runner, req.MaxRounds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func handleGetRuns(pm *ProcessManager, w http.ResponseWriter) {
	runs := pm.List()
	if runs == nil {
		runs = []RunInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}
```

- [ ] **Step 4: 修正既有測試**

既有測試用 `NewMux(ws)`，改為 `NewMux(ws, nil)` 或傳一個 dummy PM。最簡單的做法：讓 `NewMux` 在 `pm == nil` 時不註冊 write endpoints，既有測試不用改。

或者改既有測試，建一個不會被用到的 PM：

```go
// setupServerWorkspace 已有，只需在測試裡改呼叫：
srv := httptest.NewServer(NewMux(ws, NewProcessManager(ws, 1, "sleep")))
```

讓所有既有的 `NewMux(ws)` 改為 `NewMux(ws, NewProcessManager(ws, 1, "sleep"))`。

- [ ] **Step 5: 跑全部 server 測試確認通過**

Run: `go test ./internal/server/ -v -timeout 30s`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add POST /api/run and GET /api/runs endpoints"
```

---

### Task 5: Write Endpoints — POST /api/stop

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫測試**

在 `server_test.go` 加：

```go
func TestPostStop(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	// 先啟動
	runBody := `{"featureId":"test-feat"}`
	resp, _ := http.Post(srv.URL+"/api/run", "application/json", strings.NewReader(runBody))
	var info RunInfo
	json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()

	// 停止
	stopBody := fmt.Sprintf(`{"id":%q}`, info.ID)
	resp2, err := http.Post(srv.URL+"/api/stop", "application/json", strings.NewReader(stopBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
}

func TestPostStop_NotFound(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	body := `{"id":"nonexistent"}`
	resp, err := http.Post(srv.URL+"/api/stop", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run "TestPostStop" -v`
Expected: FAIL — `/api/stop` 回 404（尚未註冊）

- [ ] **Step 3: 在 NewMux 加 stop handler**

```go
mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handlePostStop(pm, w, r)
})
```

```go
type stopRequest struct {
	ID string `json:"id"`
}

func handlePostStop(pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
	var req stopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	if err := pm.Stop(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -run "TestPostStop" -v -timeout 30s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add POST /api/stop endpoint"
```

---

### Task 6: Write Endpoints — POST /api/new

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: 寫測試**

```go
func TestPostNew(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	body := `{"name":"My New Feature","description":"test desc"}`
	resp, err := http.Post(srv.URL+"/api/new", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Name != "My New Feature" {
		t.Errorf("Name = %q, want My New Feature", result.Name)
	}

	// 確認 feature YAML 存在
	if _, err := ws.LoadFeature(result.ID); err != nil {
		t.Errorf("LoadFeature(%q) failed: %v", result.ID, err)
	}
}

func TestPostNew_MissingName(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	srv := httptest.NewServer(NewMux(ws, pm))
	defer srv.Close()

	body := `{"description":"no name"}`
	resp, err := http.Post(srv.URL+"/api/new", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/server/ -run "TestPostNew" -v`
Expected: FAIL

- [ ] **Step 3: 在 NewMux 加 new handler**

```go
mux.HandleFunc("/api/new", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handlePostNew(ws, w, r)
})
```

```go
type newRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type newResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

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

	next, err := protocol.NextFeatureNumber(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := protocol.GenerateFeatureID(next, req.Name)

	desc := req.Description
	if desc == "" {
		desc = req.Name
	}

	feature := protocol.Feature{
		ID:          id,
		Name:        req.Name,
		Description: desc,
		Status:      "not-started",
	}
	if err := ws.SaveFeature(feature); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newResponse{ID: id, Name: req.Name})
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -run "TestPostNew" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add POST /api/new endpoint"
```

---

### Task 7: 更新 live.go 串接 ProcessManager

**Files:**
- Modify: `cmd/4x/live.go`

- [ ] **Step 1: 改 live.go**

```go
package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/server"
	"github.com/spf13/cobra"
)

func newLiveCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "live",
		Short: "Start the 4x Live dashboard server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			cfg, _ := ws.ReadConfig()
			maxParallel := cfg.MaxConcurrentRuns
			if maxParallel <= 0 {
				maxParallel = 1
			}

			pm := server.NewProcessManager(ws, maxParallel, "4x")
			defer pm.Shutdown()

			fmt.Printf("4x Live — http://localhost:%d\n", port)
			fmt.Printf("Watching: %s\n", ws.DotDir())
			fmt.Printf("Max concurrent runs: %d\n", maxParallel)
			fmt.Println("SSE stream: /sse/events/{feature-id}")
			return server.Start(ws, pm, port)
		},
	}

	cmd.Flags().IntVar(&port, "port", 4567, "dashboard port")
	return cmd
}
```

- [ ] **Step 2: 確認 build 通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 成功

- [ ] **Step 3: 跑全部測試**

Run: `go test ./... -timeout 60s`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/live.go
git commit -m "feat: wire ProcessManager into live command"
```

---

### Task 8: 全量驗證

- [ ] **Step 1: 完整 build + vet + test**

Run: `go build ./cmd/4x && go vet ./... && go test ./... -timeout 60s`
Expected: 全部 PASS，無 warning

- [ ] **Step 2: 手動驗證（可選）**

啟動 server 並用 curl 測試：

```bash
bin/4x live --port 4580 &
curl -s localhost:4580/api/runs | jq .
curl -s -X POST localhost:4580/api/new -d '{"name":"test feature"}' | jq .
curl -s localhost:4580/api/tasks | jq .
kill %1
```

- [ ] **Step 3: 最終 commit（如有遺漏修改）**

確認 `git status` 乾淨，無遺漏。
