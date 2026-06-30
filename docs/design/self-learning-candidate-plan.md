# F117 — candidate-status Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新 harvest 的 learning 預設為 candidate，跨 feature 獨立產出才自動升級為 active。

**Spec:** `docs/design/self-learning-spec.md` — F3 段落

**Tech Stack:** Go 1.26+

---

### Task 1: 新增 candidate status

**Files:**
- Modify: `internal/learning/store.go`

- [ ] **Step 1: 新增 StatusCandidate 常量**

```go
StatusCandidate Status = "candidate"
```

- [ ] **Step 2: 修改 Harvest() — 預設 candidate**

新 entry 的 `Status` 從 `StatusActive` 改為 `StatusCandidate`。

- [ ] **Step 3: 修改 Harvest() — 跨 feature 升級**

fuzzy match 命中時的邏輯從「一律 skip」改為三種：

```go
// match 到 active → skip (dedup)
// match 到同 feature candidate → skip (dedup)
// match 到不同 feature candidate → 升級該 candidate 為 active，skip 新的
```

升級時設定 matched entry 的 `Status = StatusActive`。

- [ ] **Step 4: 新增 CandidateEntries() 方法**

```go
func (s *Store) CandidateEntries() []Entry
```

回傳所有 `status == candidate` 的條目。

### Task 2: Designer 可見 candidate + select 升級

**Files:**
- Modify: `internal/prompt/learnings.go`
- Modify: `templates/designer.md.tmpl`

- [ ] **Step 1: LoadActiveLearnings 回傳 active + candidate**

改名考量：函數名 `LoadActiveLearnings` 語意不再精確，但為了向後相容保留名稱，在 doc comment 註明含 candidate。回傳的 entry 列表 active 在前、candidate 在後。

- [ ] **Step 2: Designer template 標註 candidate**

Designer 的 learnings 列表中，candidate 條目加 `[candidate]` 標記：

```
- [L045] (code-quality) 復用既有結構...
- [L058] [candidate] (ops) worktree 內須設 GOWORK=off...
```

- [ ] **Step 3: UpdateLearningsUsage 升級 candidate**

`UpdateLearningsUsage()` 內，若被 select 的 entry 是 candidate → 改為 active。

### Task 3: CLI 調整

**Files:**
- Modify: `cmd/4x/learn.go`

- [ ] **Step 1: learn list 預設含 candidate**

預設列出 active + candidate，candidate 條目 ID 後加 `*`。

- [ ] **Step 2: --status flag**

新增 `--status <active|candidate|stale|promoted>` 過濾。

### Task 4: 向後相容驗證

- [ ] **Step 1: 既有 47 條 active 保持不變**

不跑 migration，既有 entries 的 status 欄位已是 "active"，不受影響。

### Task 5: 測試

**Files:**
- Modify: `internal/learning/store_test.go`

- [ ] **Step 1: TestHarvest_NewEntryIsCandidate**

新 harvest 的 entry status 為 candidate。

- [ ] **Step 2: TestHarvest_CrossFeatureFuzzyPromotes**

feature A 產出的 candidate，被 feature B 的 fuzzy match 命中 → A 的 candidate 升為 active。

- [ ] **Step 3: TestHarvest_SameFeatureFuzzySkips**

同 feature 的 fuzzy match 只 skip，不升級。

- [ ] **Step 4: TestUpdateUsage_PromotesCandidate**

Designer select candidate → 升為 active。

- [ ] **Step 5: TestActiveEntries_ExcludesCandidate**

`ActiveEntries()` 不含 candidate。

- [ ] **Step 6: TestCandidateEntries**

`CandidateEntries()` 只含 candidate。

### Task 6: 驗證

- [ ] `make build && make test && make lint`
