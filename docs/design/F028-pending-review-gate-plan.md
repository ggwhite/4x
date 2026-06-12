# F028 — Pending Review Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Feature 完成 AI loop 後停在 `pending-review` 等待 user 確認，而非自動變 `done`

**Architecture:** 在狀態機加一個 `pending-review` phase 卡在 `accepting` 和 `done` 之間；Go runner 和 plugin workflow 都走這個 phase；user 透過 `4x done` CLI 或 Dashboard 按鈕手動推進到 `done`

**Tech Stack:** Go (Cobra CLI, net/http), JavaScript (Dashboard SPA)

---

### Task 1: 新增 `PhasePendingReview` 常量與狀態機轉換

**Files:**
- Modify: `internal/protocol/types.go:8-19`
- Modify: `internal/state/machine.go:11-21`
- Modify: `internal/state/machine_test.go`
- Modify: `schemas/state.schema.json:11`

- [ ] **Step 1: 寫 machine_test.go 的新測試案例**

在 `internal/state/machine_test.go` 的 `TestCanTransition_Valid` 新增：

```go
{protocol.PhaseAccepting, protocol.PhasePendingReview},
{protocol.PhasePendingReview, protocol.PhaseDone},
{protocol.PhasePendingReview, protocol.PhaseBlocked},
{protocol.PhasePendingReview, protocol.PhaseNeedsAttention},
```

在 `TestCanTransition_Invalid` 新增：

```go
{protocol.PhaseAccepting, protocol.PhaseDone},
```

注意：移除原本 Valid 表裡的 `{protocol.PhaseAccepting, protocol.PhaseDone}`。

在 `TestPhaseToRole` 新增：

```go
{protocol.PhasePendingReview, ""},
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/state/ -v -run "TestCanTransition|TestPhaseToRole"`
Expected: FAIL — `PhasePendingReview` 未定義

- [ ] **Step 3: 新增 `PhasePendingReview` 常量**

在 `internal/protocol/types.go:17`（`PhaseAccepting` 後面）加：

```go
PhasePendingReview Phase = "pending-review"
```

- [ ] **Step 4: 更新狀態機轉換表**

在 `internal/state/machine.go` 把：

```go
protocol.PhaseAccepting:      {protocol.PhaseDone},
```

改成：

```go
protocol.PhaseAccepting:      {protocol.PhasePendingReview},
protocol.PhasePendingReview:  {protocol.PhaseDone},
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/state/ -v`
Expected: PASS

- [ ] **Step 6: 更新 state schema**

在 `schemas/state.schema.json:11` 的 phase enum 加 `"pending-review"`（放在 `"accepting"` 後面）：

```json
"enum": ["init", "designing", "coding", "reviewing", "testing", "amending", "accepting", "pending-review", "done", "blocked", "needs-attention"]
```

- [ ] **Step 7: Commit**

```bash
git add internal/protocol/types.go internal/state/machine.go internal/state/machine_test.go schemas/state.schema.json
git commit -m "feat(F028): add pending-review phase to state machine"
```

---

### Task 2: 更新 Go Runner 停在 `pending-review`

**Files:**
- Modify: `cmd/4x/run.go:217,344-359,414-415`
- Modify: `cmd/4x/transition.go:102-122`

- [ ] **Step 1: 更新 `syncFeatureStatus`**

在 `cmd/4x/transition.go` 的 `syncFeatureStatus` switch 加新 case（放在 `PhaseDone` 之前）：

```go
case protocol.PhasePendingReview:
	f.Status = "ready-for-review"
```

- [ ] **Step 2: 更新 `nextPhaseAfter`**

在 `cmd/4x/run.go` 把：

```go
case protocol.PhaseAccepting:
	return protocol.PhaseDone, "", ""
```

改成：

```go
case protocol.PhaseAccepting:
	return protocol.PhasePendingReview, "", ""
```

- [ ] **Step 3: 更新 loop break 條件**

在 `cmd/4x/run.go:217` 把：

```go
if phase == protocol.PhaseDone || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention {
```

改成：

```go
if phase == protocol.PhaseDone || phase == protocol.PhasePendingReview || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention {
```

- [ ] **Step 4: 更新結束 switch**

在 `cmd/4x/run.go:344` 的 switch 加新 case：

```go
case protocol.PhasePendingReview:
	s.Active = false
	s.StopReason = "pending-review"
	ws.WriteState(featureID, s)
	syncFeatureStatus(ws, featureID, protocol.PhasePendingReview)
	fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, s.Round, featureID)
```

- [ ] **Step 5: 更新 worktree 結束判斷**

在 `cmd/4x/run.go:138` 把：

```go
if finalState.Phase == protocol.PhaseDone {
```

改成：

```go
if finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview {
```

- [ ] **Step 6: 更新 transition.go 的 active=false 條件**

在 `cmd/4x/transition.go:74` 把：

```go
if toPhase == protocol.PhaseDone || toPhase == protocol.PhaseBlocked {
```

改成：

```go
if toPhase == protocol.PhaseDone || toPhase == protocol.PhasePendingReview || toPhase == protocol.PhaseBlocked {
```

- [ ] **Step 7: 更新 guard 白名單**

在 `internal/guard/check.go:99-101` 的 `needsDesignOutputs` map 加：

```go
protocol.PhasePendingReview: true,
```

在 `internal/guard/check.go:105` 把：

```go
if state.Phase == protocol.PhaseAccepting || state.Phase == protocol.PhaseDone {
```

改成：

```go
if state.Phase == protocol.PhaseAccepting || state.Phase == protocol.PhasePendingReview || state.Phase == protocol.PhaseDone {
```

- [ ] **Step 8: 跑完整測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/4x/run.go cmd/4x/transition.go internal/guard/check.go
git commit -m "feat(F028): runner stops at pending-review instead of done"
```

---

### Task 3: 新增 `4x done` CLI subcommand

**Files:**
- Create: `cmd/4x/done.go`
- Modify: `cmd/4x/main.go:23-36`

- [ ] **Step 1: 建立 `cmd/4x/done.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <feature-id>",
		Short: "Mark a pending-review feature as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			return markDone(ws, featureID)
		},
	}
}

// markDone 將 pending-review 的 feature 推進到 done
func markDone(ws *protocol.Workspace, featureID string) error {
	s, err := ws.ReadState(featureID)
	if err != nil {
		return fmt.Errorf("cannot read state for %s: %w", featureID, err)
	}

	if s.Phase != protocol.PhasePendingReview {
		return fmt.Errorf("feature %s is in phase %q, not pending-review", featureID, s.Phase)
	}

	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		return err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return err
	}

	syncFeatureStatus(ws, featureID, protocol.PhaseDone)

	ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})

	fmt.Printf("Feature %s marked as done.\n", featureID)
	return nil
}
```

- [ ] **Step 2: 註冊到 main.go**

在 `cmd/4x/main.go:36`（`newConfigCmd()` 後面）加：

```go
newDoneCmd(),
```

- [ ] **Step 3: 驗證編譯通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/done.go cmd/4x/main.go
git commit -m "feat(F028): add 4x done subcommand"
```

---

### Task 4: 新增 Server API `POST /api/done/:id`

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: 新增 handler 函式**

在 `internal/server/server.go` 的 `readIfExists` 函式前加：

```go
type doneRequest struct {
	ID string `json:"id"`
}

func handlePostDone(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	var req doneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	s, err := ws.ReadState(req.ID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	if s.Phase != protocol.PhasePendingReview {
		http.Error(w, fmt.Sprintf("feature is in phase %q, not pending-review", s.Phase), http.StatusBadRequest)
		return
	}

	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(req.ID, newState); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f, err := ws.LoadFeature(req.ID)
	if err == nil {
		f.Status = "done"
		ws.SaveFeature(f)
	}

	ws.AppendEvent(req.ID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"done"}`)
}
```

- [ ] **Step 2: 新增 import**

在 `internal/server/server.go` 的 import 加：

```go
"github.com/ggwhite/4x/internal/state"
```

- [ ] **Step 3: 註冊 route**

在 `NewMux` 函式的 `/api/new` handler 後面（`mux.HandleFunc("/api/messages/"` 前面）加：

```go
mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handlePostDone(ws, w, r)
})
```

- [ ] **Step 4: 驗證編譯通過**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(F028): add POST /api/done endpoint"
```

---

### Task 5: 更新 Dashboard — classify、統計卡、sidebar

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 更新 `PHASE_ICON`**

在 `internal/server/static/index.html:481` 把：

```js
const PHASE_ICON = { designing:'◆',coding:'◆',reviewing:'◆',testing:'◆',accepting:'◆',amending:'◆',done:'✓',blocked:'✕','needs-attention':'!',init:'○','not-started':'○' };
```

改成：

```js
const PHASE_ICON = { designing:'◆',coding:'◆',reviewing:'◆',testing:'◆',accepting:'◆',amending:'◆','pending-review':'⏳',done:'✓',blocked:'✕','needs-attention':'!',init:'○','not-started':'○' };
```

- [ ] **Step 2: 更新 `badge` 函式**

在 `internal/server/static/index.html:483-488` 把：

```js
function badge(status, phase, active) {
  if (active && phase && phase!=='done') return '<span class="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-semibold bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 rounded-full"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span>In Progress</span>';
  if (status==='done') return '<span class="px-2 py-0.5 text-[10px] text-zinc-500 border border-zinc-700/50 rounded-full">Done</span>';
  if (status==='blocked') return '<span class="px-2 py-0.5 text-[10px] text-red-400 border border-red-500/30 rounded-full">Blocked</span>';
  return '<span class="px-2 py-0.5 text-[10px] text-zinc-600 border border-zinc-800 rounded-full">Backlog</span>';
}
```

改成：

```js
function badge(status, phase, active) {
  if (active && phase && phase!=='done') return '<span class="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-semibold bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 rounded-full"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400 pulse-dot"></span>In Progress</span>';
  if (status==='ready-for-review') return '<span class="px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded-full">Review</span>';
  if (status==='done') return '<span class="px-2 py-0.5 text-[10px] text-zinc-500 border border-zinc-700/50 rounded-full">Done</span>';
  if (status==='blocked') return '<span class="px-2 py-0.5 text-[10px] text-red-400 border border-red-500/30 rounded-full">Blocked</span>';
  return '<span class="px-2 py-0.5 text-[10px] text-zinc-600 border border-zinc-800 rounded-full">Backlog</span>';
}
```

- [ ] **Step 3: 更新 `classify` 函式**

在 `internal/server/static/index.html:503-507` 把：

```js
function classify(tasks) {
  const g = { running: [], pending: [], todo: [], done: [] };
  (tasks||[]).forEach(t => { const a = t.active && t.phase && t.phase!=='done'; if (a) g.running.push(t); else if (t.status==='done') g.done.push(t); else if (t.status==='in-progress') g.pending.push(t); else g.todo.push(t); });
  return g;
}
```

改成：

```js
function classify(tasks) {
  const g = { running: [], review: [], pending: [], todo: [], done: [] };
  (tasks||[]).forEach(t => { const a = t.active && t.phase && t.phase!=='done'; if (a) g.running.push(t); else if (t.status==='ready-for-review') g.review.push(t); else if (t.status==='done') g.done.push(t); else if (t.status==='in-progress') g.pending.push(t); else g.todo.push(t); });
  return g;
}
```

- [ ] **Step 4: 更新統計卡（4 格 → 5 格）**

在 `renderDashboard` 函式中，把統計卡的 `grid-cols-4` 改成 `grid-cols-5`，並在 Running 和 Pending 之間插入 Review 卡。

把：

```js
<div class="grid grid-cols-4 gap-4 mb-8">
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-amber-400">${g.running.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Running</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-blue-400">${g.pending.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Pending</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-purple-400">${g.todo.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Todo</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-green-400">${g.done.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Done</div></div></div>
```

改成：

```js
<div class="grid grid-cols-5 gap-4 mb-8">
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-emerald-400">${g.running.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Running</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-amber-400">${g.review.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Review</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-blue-400">${g.pending.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Pending</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-purple-400">${g.todo.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Todo</div></div>
<div class="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-5 text-center"><div class="text-3xl font-bold text-green-400">${g.done.length}</div><div class="text-xs text-zinc-500 mt-1 uppercase tracking-wider">Done</div></div></div>
```

- [ ] **Step 5: 更新圓餅圖**

在 `renderDashboard` 中，把圓餅圖計算和圖例加入 review。

把：

```js
const rP=total?g.running.length/total:0, pP=total?g.pending.length/total:0, tP=total?g.todo.length/total:0;
const a1=rP*360, a2=a1+pP*360, a3=a2+tP*360;
const donut = total ? `conic-gradient(#f59e0b 0deg ${a1}deg,#3b82f6 ${a1}deg ${a2}deg,#a78bfa ${a2}deg ${a3}deg,#10b981 ${a3}deg 360deg)` : 'conic-gradient(#27272a 0deg 360deg)';
```

改成：

```js
const rP=total?g.running.length/total:0, rvP=total?g.review.length/total:0, pP=total?g.pending.length/total:0, tP=total?g.todo.length/total:0;
const a1=rP*360, a2=a1+rvP*360, a3=a2+pP*360, a4=a3+tP*360;
const donut = total ? `conic-gradient(#10b981 0deg ${a1}deg,#f59e0b ${a1}deg ${a2}deg,#3b82f6 ${a2}deg ${a3}deg,#a78bfa ${a3}deg ${a4}deg,#22c55e ${a4}deg 360deg)` : 'conic-gradient(#27272a 0deg 360deg)';
```

更新圖例 — 把 Status 面板裡的四行圖例改成五行，在 Running 後加 Review：

把：

```js
<div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>Running<span class="ml-auto text-zinc-400 font-bold">${g.running.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-blue-500"></span>Pending<span class="ml-auto text-zinc-400 font-bold">${g.pending.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-purple-500"></span>Todo<span class="ml-auto text-zinc-400 font-bold">${g.todo.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-green-500"></span>Done<span class="ml-auto text-zinc-400 font-bold">${g.done.length}</span></div>
```

改成：

```js
<div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>Running<span class="ml-auto text-zinc-400 font-bold">${g.running.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>Review<span class="ml-auto text-zinc-400 font-bold">${g.review.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-blue-500"></span>Pending<span class="ml-auto text-zinc-400 font-bold">${g.pending.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-purple-500"></span>Todo<span class="ml-auto text-zinc-400 font-bold">${g.todo.length}</span></div><div class="flex items-center gap-2"><span class="w-2.5 h-2.5 rounded-full bg-green-500"></span>Done<span class="ml-auto text-zinc-400 font-bold">${g.done.length}</span></div>
```

- [ ] **Step 6: 更新 sidebar 分組渲染**

在 `load()` 函式中，把 sidebar 的四組改為五組。

把：

```js
rg('Running', groups.running, groups.running.length);
rg('Pending', groups.pending, groups.pending.length);
rg('Todo', groups.todo, groups.todo.length);
rg('Done', groups.done, groups.done.length);
```

改成：

```js
rg('Running', groups.running, groups.running.length);
rg('Review', groups.review, groups.review.length);
rg('Pending', groups.pending, groups.pending.length);
rg('Todo', groups.todo, groups.todo.length);
rg('Done', groups.done, groups.done.length);
```

- [ ] **Step 7: 在 sidebar review 組加 "Mark Done" 按鈕**

在 `rg` 函式的 `el.innerHTML` 渲染邏輯中，需要在 review 組的 feature 卡片上加 "Mark Done" 按鈕。修改 `rg` 函式內的 `items.forEach` 區塊。

在現有 `el.innerHTML = ...` 行（`internal/server/static/index.html:557`）後面，加一段判斷：

把：

```js
el.innerHTML = `<div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="text-[13px] font-medium truncate">${t.name}</div><div class="text-[11px] text-zinc-600 mt-0.5">${t.id}</div>${pi}</div></div>`;
el.onclick = () => { current=t.id; load(); loadDetail(t); };
```

改成：

```js
const doneBtn = t.status==='ready-for-review' ? `<button class="ml-auto px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors" onclick="event.stopPropagation();markDone('${t.id}')">Mark Done</button>` : '';
el.innerHTML = `<div class="flex items-start gap-2">${di}<div class="flex-1 min-w-0"><div class="text-[13px] font-medium truncate">${t.name}</div><div class="text-[11px] text-zinc-600 mt-0.5">${t.id}</div>${pi}</div>${doneBtn}</div>`;
el.onclick = () => { current=t.id; load(); loadDetail(t); };
```

- [ ] **Step 8: 新增 `markDone` JS 函式**

在 `goHome()` 函式前面加：

```js
async function markDone(fid) {
  if (!confirm('Mark '+fid+' as done?')) return;
  const res = await fetch(apiBase()+'/api/done', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id:fid})});
  if (!res.ok) { alert('Failed: '+(await res.text())); return; }
  load(); if (!current) renderDashboard(lastTasks);
}
```

- [ ] **Step 9: 更新 Review 專屬高亮區**

在 `renderDashboard` 函式中，Currently Running 區塊後面加一個 Review 區塊。

在 `${g.running.length>0?...}` 區塊的結尾（`':''}` 後面），加：

```js
${g.review.length>0?`<div class="rounded-xl border border-amber-500/20 bg-amber-950/20 p-5 mt-4"><div class="text-[10px] font-bold text-amber-500/70 uppercase tracking-wider mb-4">Pending Review</div><div class="space-y-2">${g.review.map(t=>`<div class="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-amber-900/20 rounded px-2 -mx-2 transition-colors" onclick="current='${t.id}';load();loadDetail(${JSON.stringify(t).replace(/"/g,'&quot;')})"><span class="text-amber-400 text-xs">⏳</span><span class="text-xs font-semibold text-amber-400">${t.id}</span><span class="text-xs text-zinc-400 truncate flex-1">${esc(t.name)}</span>${t.round?`<span class="text-[10px] text-zinc-600">${t.round}R</span>`:''}<button class="px-2 py-0.5 text-[10px] font-semibold text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors" onclick="event.stopPropagation();markDone('${t.id}')">Done</button></div>`).join('')}</div></div>`:''}
```

- [ ] **Step 10: 更新搜尋結果 badge**

在 `renderSearchResults` 函式的 badge 判斷中（`internal/server/static/index.html:440-443`），在 `isActive` 判斷後面加 review 判斷。

把：

```js
let b; if (isActive) b='<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(16,185,129,.15);color:#34d399;border:1px solid rgba(16,185,129,.3);border-radius:99px">In Progress</span>';
else if (t.status==='done') b='<span style="padding:2px 8px;font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:99px">Done</span>';
```

改成：

```js
let b; if (isActive) b='<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(16,185,129,.15);color:#34d399;border:1px solid rgba(16,185,129,.3);border-radius:99px">In Progress</span>';
else if (t.status==='ready-for-review') b='<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(245,158,11,.15);color:#fbbf24;border:1px solid rgba(245,158,11,.3);border-radius:99px">Review</span>';
else if (t.status==='done') b='<span style="padding:2px 8px;font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:99px">Done</span>';
```

- [ ] **Step 11: 驗證編譯（embedded HTML）**

Run: `go build ./cmd/4x`
Expected: 無錯誤

- [ ] **Step 12: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(F028): dashboard review section with Mark Done button"
```

---

### Task 6: 更新 CLI status 分類

**Files:**
- Modify: `cmd/4x/status.go:57-68,126-127`

- [ ] **Step 1: 更新 `categorize` 函式**

把：

```go
func categorize(f protocol.Feature, active bool) int {
	if f.Status == "in-progress" && active {
		return 0 // running
	}
	if f.Status == "in-progress" {
		return 1 // pending (in-progress but not actively running)
	}
	if f.Status == "done" {
		return 3
	}
	return 2 // not-started = todo
}
```

改成：

```go
func categorize(f protocol.Feature, active bool) int {
	if f.Status == "in-progress" && active {
		return 0 // running
	}
	if f.Status == "ready-for-review" {
		return 1 // review
	}
	if f.Status == "in-progress" {
		return 2 // pending (in-progress but not actively running)
	}
	if f.Status == "done" {
		return 4
	}
	return 3 // not-started = todo
}
```

- [ ] **Step 2: 更新 summary 行和 category labels**

把：

```go
fmt.Printf("Total: %d features — %d running, %d pending, %d todo, %d done\n\n",
	len(features), counts[0], counts[1], counts[2], counts[3])

categoryLabels := []struct {
	cat   int
	label string
}{
	{0, "Running"},
	{1, "Pending"},
	{2, "Todo"},
	{3, "Done"},
}
```

改成：

```go
fmt.Printf("Total: %d features — %d running, %d review, %d pending, %d todo, %d done\n\n",
	len(features), counts[0], counts[1], counts[2], counts[3], counts[4])

categoryLabels := []struct {
	cat   int
	label string
}{
	{0, "Running"},
	{1, "Review"},
	{2, "Pending"},
	{3, "Todo"},
	{4, "Done"},
}
```

- [ ] **Step 3: 更新 maxDone 的 category 值**

把 `if cl.cat == 3` 的兩處引用改成 `cl.cat == 4`：

```go
if pendingOnly && cl.cat == 4 {
```

```go
if cl.cat == 4 && len(group) > maxDone {
```

- [ ] **Step 4: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/status.go
git commit -m "feat(F028): CLI status shows review category"
```

---

### Task 7: 統一 Plugin Workflow

**Files:**
- Modify: `plugins/claude-code/workflow.js:471-509`

- [ ] **Step 1: 簡化 acceptor 結束邏輯**

把 `plugins/claude-code/workflow.js:475-502` 的整段改成：

原始 `const allPassed`、`const finalStatus`、以及 `await agent(...)` 整段（L475-509）替換為：

```js
await agent(`You are the Acceptor (Designer role).

${stateUpdate('acceptor', 'accept', round)}

Evaluate the overall results:
- Rounds completed: ${round}/${maxRounds}
- Test results: ${lastTestResult ? `${lastTestResult.passCount} pass, ${lastTestResult.failCount} fail, ${lastTestResult.skipCount} skip` : 'no test results'}

Read all round reports in ${featureDir}/rounds/.

Write:
1. ${featureDir}/final-report.md — summary of what was done, status, remaining issues
2. ${featureDir}/commit-plan.md — suggested commits (do NOT commit, just plan)

${stateEnd('acceptor', 'accept', round)}
4x transition ${featureId} --to pending-review
`, {
  label: `acceptor:${featureId}`,
  phase: 'Accept',
  model: MODEL_ACCEPTOR
})

log(`${featureId}: Complete — pending-review`)

return {
  featureId,
  status: 'pending-review',
  rounds: round,
  testResults: lastTestResult
}
```

注意：移除了 `finalStatus` 變數、三元判斷、以及直接寫 `state.json` 的 `echo` 指令。

- [ ] **Step 2: 驗證 workflow.js 語法**

Run: `node -c plugins/claude-code/workflow.js`
Expected: 無語法錯誤

- [ ] **Step 3: Commit**

```bash
git add plugins/claude-code/workflow.js
git commit -m "feat(F028): workflow uses 4x transition instead of writing state.json"
```

---

### Task 8: 全量驗證

**Files:** 無新變更

- [ ] **Step 1: 跑全部 Go 測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 驗證 CLI**

Run: `./bin/4x status` 或 `go run ./cmd/4x status`
Expected: 顯示五個 category（Running / Review / Pending / Todo / Done）

- [ ] **Step 3: 驗證 `4x done` help**

Run: `go run ./cmd/4x done --help`
Expected: 顯示 "Mark a pending-review feature as done"
