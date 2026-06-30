# F116 — learn-add-cli Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新增 `4x learn add` CLI，讓 standalone session 可以直接寫入 learning。

**Spec:** `docs/design/self-learning-spec.md` — F2 段落

**Tech Stack:** Go 1.26+, Cobra CLI

---

### Task 1: 新增 FindSimilar 方法

**Files:**
- Modify: `internal/learning/store.go`
- Modify: `internal/learning/store_test.go`

- [ ] **Step 1: 實作 FindSimilar**

從 `Harvest()` 的三層去重邏輯中抽取共用部分。`FindSimilar` 回傳第一個命中的 Entry 指標，未命中回傳 nil。

```go
func (s *Store) FindSimilar(content string) *Entry
```

三層比對順序：exact → normalized → Jaccard ≥ FuzzyDupThreshold。

- [ ] **Step 2: 重構 Harvest 使用 FindSimilar**

`Harvest()` 內部呼叫 `FindSimilar` 取代重複的比對邏輯。注意：Harvest 同時也要比對本次 batch 內的新 entry（不只 store 既有的），所以不能完全委託——FindSimilar 只查 store 既有 entries，batch 內的去重仍在 Harvest 內處理。

- [ ] **Step 3: 測試 FindSimilar**

```go
func TestFindSimilar_ExactMatch(t *testing.T)
func TestFindSimilar_NormalizedMatch(t *testing.T)
func TestFindSimilar_JaccardMatch(t *testing.T)
func TestFindSimilar_NoMatch(t *testing.T)
```

### Task 2: 新增 learn add subcommand

**Files:**
- Modify: `cmd/4x/learn.go`

- [ ] **Step 1: 實作 newLearnAddCmd**

```go
func newLearnAddCmd() *cobra.Command
```

Flags：
- `--category`（必填）：合法值來自 `learning.ValidCategories()`
- `--content`（必填）：learning 內容
- `--json`：JSON 輸出

流程：
1. 驗證 category 在白名單
2. 驗證 content 非空
3. `LoadStore` → `FindSimilar` → 若命中報錯帶 ID → 若未命中呼叫 `Harvest("manual", "user", ...)` → `Save`
4. 印出新 ID

- [ ] **Step 2: 註冊到 newLearnCmd**

在 `newLearnCmd()` 的 `AddCommand` 加入 `newLearnAddCmd()`。

### Task 3: 測試

**Files:**
- Modify: `cmd/4x/learn.go`（或對應的 test 檔）

- [ ] **Step 1: TestLearnAdd_Success**

正常寫入，verify store 多一條，sourceFeature="manual"，sourceRole="user"。

- [ ] **Step 2: TestLearnAdd_InvalidCategory**

報錯，列出合法值。

- [ ] **Step 3: TestLearnAdd_FuzzyDuplicate**

先寫入一條，再 add 相似內容，驗證不寫入且報出 ID。

- [ ] **Step 4: TestLearnAdd_JSON**

`--json` 輸出格式：`{"id":"L0xx","added":true}` 或 `{"error":"similar learning already exists: L0xx"}`。

### Task 4: 更新 CLI docs

**Files:**
- Modify: `docs/guide/cli.md`

- [ ] **Step 1: 加入 learn add 段落**

在 `4x learn` 段落內加入 `learn add` 的說明和範例。

### Task 5: 驗證

- [ ] `make build && make test && make lint`
- [ ] `make check-docs`
