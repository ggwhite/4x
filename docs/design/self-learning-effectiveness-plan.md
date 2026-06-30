# F119 — learning-effectiveness Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 追蹤 learning 注入後是否減少同類問題，標記無效 learning 供 review。

**Spec:** `docs/design/self-learning-spec.md` — F5 段落

**依賴:** F117 (candidate-status) — 需要 candidate→active 的升級記錄

**Tech Stack:** Go 1.26+

---

### Task 1: Entry 新增 ActivatedAt 欄位

**Files:**
- Modify: `internal/learning/store.go`

- [ ] **Step 1: 新增欄位**

```go
ActivatedAt time.Time `json:"activated_at,omitempty"`
```

加在 `CreatedAt` 下方。

- [ ] **Step 2: F117 升級邏輯設定 ActivatedAt**

在 Harvest() 的跨 feature 升級處和 UpdateLearningsUsage() 的 candidate→active 處，設定 `ActivatedAt = time.Now()`。

注意：既有 active entries 的 ActivatedAt 為零值，proxy 計算時要處理（零值視為 CreatedAt）。

### Task 2: 實作 MarkIneffective

**Files:**
- Modify: `internal/learning/store.go`

- [ ] **Step 1: 新增 Ineffective 標記**

不新增 status——用 Entry 上的 bool 欄位：

```go
Ineffective bool `json:"ineffective,omitempty"`
```

保持 status 只有 candidate/active/stale/promoted 四種。Ineffective 是正交的標記。

- [ ] **Step 2: 實作 MarkIneffective 方法**

```go
func (s *Store) MarkIneffective()
```

掃描所有 active entries，滿足以下三條件的標記 `Ineffective = true`：
1. `UsedCount >= 3`
2. `ActivatedAt`（或 CreatedAt 若零值）距今 > 30 天
3. `source_feature` 不同的最近 3 個 entries 中，有同 category 的新 learning

「最近 3 個 entries」以 `CreatedAt` 排序取最新的 3 個非本條目的 entries。

- [ ] **Step 3: 在 HarvestLearnings 後呼叫**

`HarvestLearnings()` 在 harvest + save 成功後呼叫 `MarkIneffective()`，再存一次。

### Task 3: CLI

**Files:**
- Modify: `cmd/4x/learn.go`

- [ ] **Step 1: learn list 顯示 ineffective 標記**

ineffective 的 entry 在 STATUS 欄顯示 `active!`。

- [ ] **Step 2: --ineffective flag**

`4x learn list --ineffective`：只列出 `Ineffective == true` 的條目。

### Task 4: 測試

**Files:**
- Modify: `internal/learning/store_test.go`

- [ ] **Step 1: TestMarkIneffective_MeetsAllConditions**

建構滿足三條件的 store，驗證標記。

- [ ] **Step 2: TestMarkIneffective_NotEnoughUsage**

UsedCount < 3 不標記。

- [ ] **Step 3: TestMarkIneffective_TooRecent**

ActivatedAt 在 30 天內不標記。

- [ ] **Step 4: TestMarkIneffective_NoCategoryContinuation**

最近的 entries 是不同 category → 不標記。

### Task 5: 驗證

- [ ] `make build && make test && make lint`
