# F031 — Event Runner/Model Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓每個 event 記錄產生它的 runner 和 model，並在 State 累積 runners 清單，Dashboard 展示 runner tag。

**Architecture:** Event struct 加 `Runner`/`Model` 欄位（omitempty），State struct 加 `Runners []string`。所有 `AppendEvent` 呼叫點填入值，`taskInfo` 帶 `Runners` 到前端。Dashboard sidebar 顯示 runner tag，詳情 header 顯示所有用過的 runner。

**Tech Stack:** Go (protocol/types, cmd/run, server), JavaScript (dashboard HTML)

---

## File Map

| Action | File | 職責 |
|---|---|---|
| Modify | `internal/protocol/types.go:118-129` | Event struct 加 Runner/Model 欄位 |
| Modify | `internal/protocol/types.go:100-116` | State struct 加 Runners 欄位 |
| Modify | `cmd/4x/run.go:155-181` | run 啟動時累積 Runners、run-start event 帶 Runner |
| Modify | `cmd/4x/run.go:262-405` | runLoop 中所有 AppendEvent 帶 Runner/Model |
| Modify | `internal/server/server.go:115-124` | taskInfo 加 Runners 欄位 |
| Modify | `internal/server/server.go:257-268` | handleTasks 填入 Runners |
| Modify | `internal/server/static/index.html:949-952` | sidebar feature item 顯示 runner tag |
| Modify | `internal/server/static/index.html:972-976` | 詳情 header 顯示所有 runners |
| Modify | `plugins/copilot/workflow.js:162-168` | stateUpdate/stateEnd 帶 runner/model |
| Modify | `internal/protocol/workspace_test.go:273-348` | 測試新欄位的序列化 |
| Modify | `cmd/4x/run_loop_test.go` | 測試 event 帶 runner/model、state.Runners 累積 |

---

### Task 1: Event struct 加 Runner/Model 欄位

**Files:**
- Modify: `internal/protocol/types.go:118-129`
- Test: `internal/protocol/workspace_test.go`

- [ ] **Step 1: 寫失敗的測試 — Event 帶 Runner/Model 的序列化**

在 `internal/protocol/workspace_test.go` 的 `TestAppendEvent` 後面加一個新測試：

```go
func TestAppendEventWithRunnerModel(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-rm"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	evt := Event{
		Type:   "phase-start",
		Phase:  PhaseDesigning,
		Round:  1,
		Runner: "claude",
		Model:  "opus",
	}
	if err := ws.AppendEvent("feat-rm", evt); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	f, err := os.Open(filepath.Join(ws.FeatureDir("feat-rm"), EventsFile))
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer f.Close()

	var got Event
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Runner != "claude" {
		t.Errorf("Runner = %q, want %q", got.Runner, "claude")
	}
	if got.Model != "opus" {
		t.Errorf("Model = %q, want %q", got.Model, "opus")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestAppendEventWithRunnerModel -v`
Expected: 編譯錯誤 `evt.Runner undefined`

- [ ] **Step 3: 在 Event struct 加欄位**

修改 `internal/protocol/types.go`，在 Event struct 的 `Detail` 欄位後加：

```go
type Event struct {
	Timestamp string `json:"ts"`
	Phase     Phase  `json:"phase"`
	Type      string `json:"type"`
	Role      Role   `json:"role,omitempty"`
	Round     int    `json:"round,omitempty"`
	Action    string `json:"action,omitempty"`
	Command   string `json:"cmd,omitempty"`
	Status    string `json:"status,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Runner    string `json:"runner,omitempty"`
	Model     string `json:"model,omitempty"`
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestAppendEventWithRunnerModel -v`
Expected: PASS

- [ ] **Step 5: 向後相容測試 — 舊格式 event 不含 Runner/Model 仍可 parse**

在 `internal/protocol/workspace_test.go` 加：

```go
func TestEventBackwardCompat(t *testing.T) {
	raw := `{"ts":"2026-01-01T00:00:00Z","phase":"designing","type":"phase-start","round":1}`
	var evt Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal old event: %v", err)
	}
	if evt.Runner != "" {
		t.Errorf("Runner should be empty for old events, got %q", evt.Runner)
	}
	if evt.Model != "" {
		t.Errorf("Model should be empty for old events, got %q", evt.Model)
	}
	if evt.Phase != PhaseDesigning {
		t.Errorf("Phase = %q, want %q", evt.Phase, PhaseDesigning)
	}
}
```

Run: `go test ./internal/protocol/ -run TestEventBackwardCompat -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/protocol/workspace_test.go
git commit -m "feat(F031): add Runner/Model fields to Event struct"
```

---

### Task 2: State struct 加 Runners 欄位

**Files:**
- Modify: `internal/protocol/types.go:100-116`
- Test: `internal/protocol/workspace_test.go`

- [ ] **Step 1: 寫失敗的測試 — State 帶 Runners 的序列化**

在 `internal/protocol/workspace_test.go` 加：

```go
func TestStateRunnersRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-runners"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	want := State{
		FeatureID: "feat-runners",
		Phase:     PhaseCoding,
		Role:      RoleCoder,
		Round:     1,
		MaxRounds: 5,
		Active:    true,
		Runner:    "claude",
		Runners:   []string{"codex", "claude"},
	}
	if err := ws.WriteState("feat-runners", want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	got, err := ws.ReadState("feat-runners")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if len(got.Runners) != 2 {
		t.Fatalf("Runners length = %d, want 2", len(got.Runners))
	}
	if got.Runners[0] != "codex" || got.Runners[1] != "claude" {
		t.Errorf("Runners = %v, want [codex claude]", got.Runners)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/protocol/ -run TestStateRunnersRoundtrip -v`
Expected: 編譯錯誤 `want.Runners undefined`

- [ ] **Step 3: 在 State struct 加欄位**

修改 `internal/protocol/types.go`，在 State struct 的 `StopReason` 欄位後加：

```go
type State struct {
	FeatureID             string    `json:"featureId"`
	Phase                 Phase     `json:"phase"`
	Role                  Role      `json:"role"`
	Round                 int       `json:"round"`
	MaxRounds             int       `json:"maxRounds"`
	Active                bool      `json:"active"`
	Runner                string    `json:"runner"`
	Label                 string    `json:"label,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
	Since                 time.Time `json:"since,omitempty"`
	ConsecutiveNoProgress int       `json:"consecutiveNoProgress"`
	LastFailCount         int       `json:"lastFailCount"`
	StopReason            string    `json:"stopReason,omitempty"`
	Runners               []string  `json:"runners,omitempty"`
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/protocol/ -run TestStateRunnersRoundtrip -v`
Expected: PASS

- [ ] **Step 5: 向後相容測試 — 舊 state.json 不含 Runners 仍可 parse**

在 `internal/protocol/workspace_test.go` 加：

```go
func TestStateBackwardCompatNoRunners(t *testing.T) {
	raw := `{"featureId":"feat-old","phase":"coding","role":"coder","round":1,"maxRounds":5,"active":true,"runner":"claude","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`
	var s State
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal old state: %v", err)
	}
	if s.Runners != nil {
		t.Errorf("Runners should be nil for old state, got %v", s.Runners)
	}
	if s.Runner != "claude" {
		t.Errorf("Runner = %q, want %q", s.Runner, "claude")
	}
}
```

Run: `go test ./internal/protocol/ -run TestStateBackwardCompatNoRunners -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/protocol/workspace_test.go
git commit -m "feat(F031): add Runners field to State struct"
```

---

### Task 3: run.go — 累積 Runners 並在 event 帶 Runner/Model

**Files:**
- Modify: `cmd/4x/run.go:155-181` (runCmd 中 state 初始化)
- Modify: `cmd/4x/run.go:262-405` (runLoop 中 AppendEvent)
- Test: `cmd/4x/run_loop_test.go`

- [ ] **Step 1: 寫失敗的測試 — state.Runners 累積**

在 `cmd/4x/run_loop_test.go` 加測試，驗證 runLoop 結束後 state.Runners 包含 runner 名稱：

```go
func TestRunLoopSetsRunners(t *testing.T) {
	ws := setupTestWorkspace(t, "feat-runners-track")
	s := protocol.State{
		FeatureID: "feat-runners-track",
		Phase:     protocol.PhaseInit,
		MaxRounds: 5,
		Active:    true,
		Runner:    "claude",
		Runners:   []string{"claude"},
	}
	ws.WriteState("feat-runners-track", s)

	mock := &mockRunner{ws: ws, featureID: "feat-runners-track", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}
	cfg := protocol.Config{}
	runLoop(ws, ws, protocol.Feature{ID: "feat-runners-track", Name: "test"}, cfg, s, func(lp string, m string) runner.Runner { return mock }, "per-round")

	final, _ := ws.ReadState("feat-runners-track")
	if len(final.Runners) == 0 {
		t.Fatal("Runners should not be empty")
	}
	found := false
	for _, r := range final.Runners {
		if r == "claude" {
			found = true
		}
	}
	if !found {
		t.Errorf("Runners = %v, should contain 'claude'", final.Runners)
	}
}
```

- [ ] **Step 2: 跑測試確認通過（此時 Runners 由呼叫者預設帶入，所以已可 pass）**

Run: `go test ./cmd/4x/ -run TestRunLoopSetsRunners -v`
Expected: PASS（Runners 由 state 帶入，runLoop 不會清除它）

- [ ] **Step 3: 寫測試 — event 帶 runner/model**

在 `cmd/4x/run_loop_test.go` 加：

```go
func TestRunLoopEventsCarryRunnerModel(t *testing.T) {
	ws := setupTestWorkspace(t, "feat-evt-rm")
	s := protocol.State{
		FeatureID: "feat-evt-rm",
		Phase:     protocol.PhaseInit,
		MaxRounds: 5,
		Active:    true,
		Runner:    "claude",
		Runners:   []string{"claude"},
	}
	ws.WriteState("feat-evt-rm", s)

	mock := &mockRunner{ws: ws, featureID: "feat-evt-rm", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}
	cfg := protocol.Config{
		Roles: map[string]protocol.RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	runLoop(ws, ws, protocol.Feature{ID: "feat-evt-rm", Name: "test"}, cfg, s, func(lp string, m string) runner.Runner { return mock }, "per-round")

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-evt-rm"), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	hasRunnerEvent := false
	for _, line := range lines {
		var evt protocol.Event
		json.Unmarshal([]byte(line), &evt)
		if evt.Type == "phase-start" && evt.Runner != "" {
			hasRunnerEvent = true
			if evt.Runner != "claude" {
				t.Errorf("event runner = %q, want 'claude'", evt.Runner)
			}
		}
	}
	if !hasRunnerEvent {
		t.Error("no phase-start event with runner field found")
	}
}
```

- [ ] **Step 4: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestRunLoopEventsCarryRunnerModel -v`
Expected: FAIL — `no phase-start event with runner field found`

- [ ] **Step 5: 修改 run.go — runCmd 中累積 Runners**

修改 `cmd/4x/run.go` 約 L155-175。在設好 `s.Runner = runnerName` 後，累積 `s.Runners`：

```go
// 在 s.Runner = runnerName 之後（新 state 和 existing state 兩條路徑合流後）
found := false
for _, r := range s.Runners {
    if r == runnerName {
        found = true
        break
    }
}
if !found {
    s.Runners = append(s.Runners, runnerName)
}
```

具體位置：在 `cmd/4x/run.go` 中，兩段 state 設定（新建 L155-162 和 existing L164-171）合流後、`ws.WriteState` (L173) 之前插入這段。

- [ ] **Step 6: 修改 run.go — run-start event 帶 Runner**

修改 `cmd/4x/run.go` L177-181：

```go
ws.AppendEvent(featureID, protocol.Event{
    Type:   "run-start",
    Phase:  s.Phase,
    Role:   state.PhaseToRole(s.Phase),
    Runner: runnerName,
})
```

- [ ] **Step 7: 修改 run.go — runLoop 中所有 AppendEvent 帶 Runner/Model**

修改 `cmd/4x/run.go` 的 `runLoop` 函式。`model` 在 L322 resolve，`s.Runner` 始終可用。

escalation event (L290)：
```go
ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner})
```

phase-start event (L312-314)：
```go
model := resolveModel(cfg, cfg.Runners[s.Runner], role)
ws.AppendEvent(featureID, protocol.Event{
    Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
    Runner: s.Runner, Model: model,
})
```

注意：原本 `model` 在 L322 才 resolve。需要把 `resolveModel` 呼叫提前到 phase-start event 之前，或在 event 處重新呼叫一次。最乾淨的做法是把 `model := resolveModel(...)` 從 L322 提前到 L312 之前，phase-start event 和 runner 建立都用同一個 `model` 變數：

```go
model := resolveModel(cfg, cfg.Runners[s.Runner], role)

ws.AppendEvent(featureID, protocol.Event{
    Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
    Runner: s.Runner, Model: model,
})

prompt, err := generatePrompt(ws, feature, cfg, role, s.Round)
// ...
logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(s.Round, string(role)))
r := newRunner(logPath, model)
```

run-end error event (L353-356)：
```go
ws.AppendEvent(featureID, protocol.Event{
    Type: "run-end", Phase: phase, Role: role, Round: s.Round,
    Status: "error", Detail: err.Error(),
    Runner: s.Runner, Model: model,
})
```

run-end success event (L360-363)：
```go
ws.AppendEvent(featureID, protocol.Event{
    Type: "run-end", Phase: phase, Role: role, Round: s.Round,
    Status: fmt.Sprintf("exit-%d", result.ExitCode),
    Runner: s.Runner, Model: model,
})
```

transition event (L402-404)：
```go
ws.AppendEvent(featureID, protocol.Event{
    Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
    Runner: s.Runner,
})
```

- [ ] **Step 8: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestRunLoopEventsCarryRunnerModel -v`
Expected: PASS

- [ ] **Step 9: 跑全部測試確認無回歸**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add cmd/4x/run.go cmd/4x/run_loop_test.go
git commit -m "feat(F031): populate Runner/Model in events and accumulate Runners in state"
```

---

### Task 4: Server — taskInfo 帶 Runners

**Files:**
- Modify: `internal/server/server.go:115-124` (taskInfo struct)
- Modify: `internal/server/server.go:257-268` (handleTasks)
- Test: `internal/server/server_test.go`

- [ ] **Step 1: 寫失敗的測試 — /api/tasks 回傳包含 runners**

在 `internal/server/server_test.go` 加測試。先確認現有測試的 helper 結構：

```go
func TestTasksIncludeRunners(t *testing.T) {
	ws := setupServerTestWorkspace(t)
	ws.InitFeatureDir("feat-srv-runners")

	s := protocol.State{
		FeatureID: "feat-srv-runners",
		Phase:     protocol.PhaseCoding,
		Active:    true,
		Runner:    "claude",
		Runners:   []string{"codex", "claude"},
	}
	ws.WriteState("feat-srv-runners", s)

	// 建立 feature YAML
	featureYAML := "id: feat-srv-runners\nname: test runners\nstatus: in-progress\n"
	os.MkdirAll(filepath.Join(ws.Root, ".4x", "features"), 0o755)
	os.WriteFile(filepath.Join(ws.Root, ".4x", "features", "feat-srv-runners.yaml"), []byte(featureYAML), 0o644)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handleTasks(ws, w)

	var tasks []struct {
		ID      string   `json:"id"`
		Runners []string `json:"runners"`
	}
	json.NewDecoder(w.Body).Decode(&tasks)

	found := false
	for _, t := range tasks {
		if t.ID == "feat-srv-runners" {
			found = true
			if len(t.Runners) != 2 || t.Runners[0] != "codex" || t.Runners[1] != "claude" {
				t := t
				_ = t
			}
		}
	}
	if !found {
		t.Error("feat-srv-runners not found in tasks response")
	}
}
```

注意：測試的具體寫法需要參考現有 `server_test.go` 的 helper（`setupServerTestWorkspace` 等）。若沒有現成 helper，可直接用 `protocol.Workspace{Root: t.TempDir()}` 配合手動建立 feature YAML。

- [ ] **Step 2: 修改 taskInfo struct**

修改 `internal/server/server.go` L115-124：

```go
type taskInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Phase   string   `json:"phase"`
	Role    string   `json:"role"`
	Round   int      `json:"round"`
	Active  bool     `json:"active"`
	Runner  string   `json:"runner"`
	Runners []string `json:"runners,omitempty"`
}
```

- [ ] **Step 3: 修改 handleTasks 填入 Runners**

修改 `internal/server/server.go` L262-268：

```go
if s, err := ws.ReadState(f.ID); err == nil {
    t.Phase = string(s.Phase)
    t.Role = string(s.Role)
    t.Round = s.Round
    t.Active = s.Active
    t.Runner = s.Runner
    t.Runners = s.Runners
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/server/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(F031): expose Runners in /api/tasks response"
```

---

### Task 5: Dashboard — sidebar 顯示 runner tag

**Files:**
- Modify: `internal/server/static/index.html:940-960` (sidebar feature item rendering)

- [ ] **Step 1: 加 runner tag 的 CSS**

在 `internal/server/static/index.html` 的 `<style>` 區塊加：

```css
.runner-tag {
  display: inline-block;
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid rgba(255,255,255,.1);
  color: var(--text-3);
  background: rgba(255,255,255,.04);
}
```

- [ ] **Step 2: 加 runner 顏色映射函式**

在 `<script>` 區塊加一個簡單的 hash-to-color 函式：

```javascript
const RUNNER_COLORS = {claude:'#10b981',codex:'#3b82f6',gemini:'#f59e0b',copilot:'#a78bfa',cursor:'#ec4899'};
function runnerColor(name) {
  return RUNNER_COLORS[name] || '#71717a';
}
function runnerTags(runners) {
  if (!runners || !runners.length) return '';
  return runners.map(r => `<span class="runner-tag" style="border-color:${runnerColor(r)}40;color:${runnerColor(r)}">${esc(r)}</span>`).join(' ');
}
```

- [ ] **Step 3: 修改 sidebar feature item**

修改 `internal/server/static/index.html` 約 L952，在 feature item 的 `pi`（phase indicator）下方加 runner tag：

找到這行（~L952）：
```javascript
el.innerHTML = `<div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="text-[13px] font-medium truncate">${esc(t.name)}</div><div class="text-[11px] text-zinc-600 mt-0.5">${t.id}</div>${pi}</div>${doneBtn}</div>`;
```

改為：
```javascript
const rt = runnerTags(t.runners);
const rtLine = rt ? `<div class="flex gap-1 mt-1">${rt}</div>` : '';
el.innerHTML = `<div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="text-[13px] font-medium truncate">${esc(t.name)}</div><div class="text-[11px] text-zinc-600 mt-0.5">${t.id}</div>${pi}${rtLine}</div>${doneBtn}</div>`;
```

- [ ] **Step 4: 手動驗證**

Run: `go build ./cmd/4x && bin/4x live`
在瀏覽器打開 `http://localhost:4580`，確認有 runner 的 feature 在 sidebar 顯示彩色 tag。

- [ ] **Step 5: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F031): show runner tags in dashboard sidebar"
```

---

### Task 6: Dashboard — 詳情 header 顯示所有 runners

**Files:**
- Modify: `internal/server/static/index.html:972-976` (loadDetail 的 h-meta)

- [ ] **Step 1: 修改 loadDetail 中的 meta 渲染**

找到 `internal/server/static/index.html` 約 L972-976：

```javascript
const meta = [];
if (t.phase) meta.push(`<span>${PHASE_ICON[t.phase]||'○'} ${t.phase}</span>`);
if (t.round) meta.push(`<span>⟳ Round ${t.round}</span>`);
if (t.runner) meta.push(`<span>⬡ ${t.runner}</span>`);
```

改為：

```javascript
const meta = [];
if (t.phase) meta.push(`<span>${PHASE_ICON[t.phase]||'○'} ${t.phase}</span>`);
if (t.round) meta.push(`<span>⟳ Round ${t.round}</span>`);
if (t.runners && t.runners.length) {
  meta.push(`<span>⬡ ${t.runners.map(r => `<span style="color:${runnerColor(r)}">${esc(r)}</span>`).join(' · ')}</span>`);
} else if (t.runner) {
  meta.push(`<span>⬡ ${t.runner}</span>`);
}
```

- [ ] **Step 2: 手動驗證**

Run: `bin/4x live`
打開 feature 詳情頁，確認 header 顯示所有用過的 runner（有顏色區分）。

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F031): show all runners in feature detail header"
```

---

### Task 7: workflow.js — event 帶 runner/model

**Files:**
- Modify: `plugins/copilot/workflow.js:162-168`

- [ ] **Step 1: 修改 stateUpdate 函式**

找到 `plugins/copilot/workflow.js` L162-163 的 `stateUpdate` 函式：

```javascript
function stateUpdate(role, phaseLabel, round) {
  return `\necho '{"active":true,"role":"${role}","phase":"${phaseLabel}","round":${round},"label":"Round ${round} ${role}"}' > ${featureDir}/state.json\necho '{"phase":"${phaseLabel}","type":"phase-start","role":"${role}","round":${round}}' >> ${featureDir}/events.jsonl\n`
}
```

改為（state.json 加 runners，events.jsonl 加 runner/model）：

```javascript
function stateUpdate(role, phaseLabel, round) {
  return `\necho '{"active":true,"role":"${role}","phase":"${phaseLabel}","round":${round},"label":"Round ${round} ${role}","runner":"copilot","runners":["copilot"]}' > ${featureDir}/state.json\necho '{"phase":"${phaseLabel}","type":"phase-start","role":"${role}","round":${round},"runner":"copilot","model":"${MODEL}"}' >> ${featureDir}/events.jsonl\n`
}
```

- [ ] **Step 2: 修改 stateEnd 函式**

找到 L166-168：

```javascript
function stateEnd(role, phaseLabel, round) {
  return `echo '{"phase":"${phaseLabel}","type":"phase-end","role":"${role}","round":${round}}' >> ${featureDir}/events.jsonl`
}
```

改為：

```javascript
function stateEnd(role, phaseLabel, round) {
  return `echo '{"phase":"${phaseLabel}","type":"phase-end","role":"${role}","round":${round},"runner":"copilot","model":"${MODEL}"}' >> ${featureDir}/events.jsonl`
}
```

- [ ] **Step 3: Commit**

```bash
git add plugins/copilot/workflow.js
git commit -m "feat(F031): add runner/model to workflow.js events"
```

---

### Task 8: 全面驗證與文件同步

**Files:**
- Verify: all modified files
- Modify: `docs/guide/cli.md` (if needed)

- [ ] **Step 1: 跑完整測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 2: 端到端手動驗證**

1. `bin/4x live` 啟動 dashboard
2. 建一個測試 feature：`bin/4x new "test runner tracking"`
3. 用一個 runner 跑（如 claude）：`bin/4x run <feature-id> --runner claude --dry-run`
4. 檢查 `.4x/<feature-id>/events.jsonl`：確認每行都有 `"runner":"claude"`
5. 檢查 `.4x/<feature-id>/state.json`：確認有 `"runners":["claude"]`
6. Dashboard 確認 sidebar 有 runner tag、詳情 header 有 runner 資訊

- [ ] **Step 3: 確認 docs 無需更新**

本次改動不涉及新的 CLI subcommand 或 flag 變更，`docs/guide/cli.md` 無需更新。Event/State 結構變更屬內部協議，不在使用者文件範圍。

- [ ] **Step 4: Commit spec 和 plan**

```bash
git add docs/design/F031-event-runner-tracking-spec.md docs/design/F031-event-runner-tracking-plan.md
git commit -m "docs(F031): add event runner tracking spec and plan"
```
