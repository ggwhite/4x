# F118 — learn-context Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 產生 learnings 的 markdown snapshot，讓 standalone session 透過 CLAUDE.md `@` include 讀到 active learnings。

**Spec:** `docs/design/self-learning-spec.md` — F4 段落

**Tech Stack:** Go 1.26+, Cobra CLI

---

### Task 1: 實作 GenerateLearningsContext

**Files:**
- Modify: `internal/prompt/learnings.go`

- [ ] **Step 1: 新增 GenerateLearningsContext 函數**

```go
func GenerateLearningsContext(ws *protocol.Workspace) error
```

流程：
1. `LoadStore` 取所有 active entries
2. 按 category 分組（sort by category name）
3. 產生 markdown，寫入 `.4x/learnings-context.md`
4. 無 active entries 時寫入空檔（只有 header + "No active learnings."）

- [ ] **Step 2: 在 HarvestLearnings 成功後呼叫**

`HarvestLearnings()` 在 `store.Save()` 成功後呼叫 `GenerateLearningsContext(ws)`。失敗只 warn。

- [ ] **Step 3: 在 ApplyConsolidateResult 成功後呼叫**

`ApplyConsolidateResult()` 回傳後，由 caller（orchestrator.go `runConsolidate`）呼叫 `GenerateLearningsContext`。

### Task 2: 新增 learn context CLI

**Files:**
- Modify: `cmd/4x/learn.go`

- [ ] **Step 1: 實作 newLearnContextCmd**

```go
func newLearnContextCmd() *cobra.Command
```

無引數。呼叫 `GenerateLearningsContext`，印出寫入路徑。支援 `--json`。

- [ ] **Step 2: 註冊到 newLearnCmd**

### Task 3: Protocol 常量

**Files:**
- Modify: `internal/protocol/workspace.go`

- [ ] **Step 1: 新增常量**

```go
LearningsContextFile = "learnings-context.md"
```

### Task 4: CLAUDE.md template 整合

**Files:**
- Modify: `cmd/4x/init.go`

- [ ] **Step 1: init 產生的 CLAUDE.md 加入 @ include**

找到 `4x init` 產生 CLAUDE.md 的位置，在適當處加入：

```
@.4x/learnings-context.md
```

加在 plugin include 之後。

### Task 5: 測試

**Files:**
- 新增或修改對應 test 檔

- [ ] **Step 1: TestGenerateLearningsContext_GroupsByCategory**

寫入多個 category 的 active entries，驗證產出的 markdown 按 category 分組。

- [ ] **Step 2: TestGenerateLearningsContext_OnlyActive**

包含 candidate/stale/promoted entries，驗證只有 active 出現在 context 檔。

- [ ] **Step 3: TestGenerateLearningsContext_EmptyStore**

空 store 也能正常產生檔案。

- [ ] **Step 4: TestLearnContext_CLI**

呼叫 CLI，驗證檔案被建立。

### Task 6: 驗證

- [ ] `make build && make test && make lint`
- [ ] `make check-docs`
