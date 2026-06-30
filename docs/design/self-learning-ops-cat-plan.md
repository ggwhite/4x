# F115 — ops-category Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新增 `ops` category，讓環境、工具、帳號、部署等操作知識可被 learnings 機制捕獲。

**Spec:** `docs/design/self-learning-spec.md` — F1 段落

**Tech Stack:** Go 1.26+

---

### Task 1: 新增 ops category 定義

**Files:**
- Modify: `internal/learning/store.go`

- [ ] **Step 1: 新增 CategoryOps 常量**

在 `CategoryProcess` 下方新增：

```go
CategoryOps Category = "ops"
```

- [ ] **Step 2: 加入 ValidCategories()**

在 `ValidCategories()` 回傳的 slice 加入 `CategoryOps`。`validCategorySet` 會自動包含。

- [ ] **Step 3: 更新 roleCategoryMap**

```go
"coder":   {CategoryDesign, CategoryCodeQuality, CategoryTooling, CategoryOps},
"tester":  {CategoryTesting, CategoryTooling, CategoryOps},
"fixer":   {CategoryCodeQuality, CategoryTooling, CategoryOps},
```

其他角色不加 `ops`。

### Task 2: 更新 role template 提示

**Files:**
- Modify: `templates/coder.md.tmpl`
- Modify: `templates/tester.md.tmpl`
- Modify: `templates/fixer.md.tmpl`

- [ ] **Step 1: coder template**

在 `== Role Learnings (optional) ==` 區塊的 category 列舉，把 `design | code-quality | testing | review | tooling | process` 改為 `design | code-quality | testing | review | tooling | process | ops`。

新增一行提示：
```
- 環境、工具、帳號、workaround 等操作問題也值得記錄，用 category "ops"
```

- [ ] **Step 2: tester template**

同 Step 1 的改法。

- [ ] **Step 3: fixer template**

同 Step 1 的改法。

### Task 3: 測試

**Files:**
- Modify: `internal/learning/store_test.go`

- [ ] **Step 1: TestIsValidCategory_Ops**

```go
func TestIsValidCategory_Ops(t *testing.T) {
    if !IsValidCategory(CategoryOps) {
        t.Error("ops should be valid")
    }
}
```

- [ ] **Step 2: 更新 TestCategoriesForRole**

驗證 coder/tester/fixer 回傳包含 `CategoryOps`，其他角色不包含。

- [ ] **Step 3: TestHarvest_OpsCategory**

用 `ops` category 的 RetroLearning 呼叫 Harvest，驗證可正常寫入。

### Task 4: 驗證

- [ ] `make build && make test && make lint`
