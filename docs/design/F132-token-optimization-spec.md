# Token Optimization — Design Spec

> 基於 Superpowers v6 的 token 優化啟發，對 4x 流程做五項改進，目標：減少重複上下文搬運、跳過冗餘角色、讓模型路由更精細。

## 1. 跳過冗餘 Fixer

### 現狀

`deepTransitionAccepting()` (orchestrator/deep_review.go) 只看 profile 是否啟用 fixing phase。即使 deep-review verdict=PASS（所有 finding 已在 deep-fix 修完），fixer 仍會跑一整個 session，讀完所有報告後回「無事可做」。

### 改法

在 `deepTransitionAccepting()` 加條件：deep-review-report.md verdict=PASS 時直接跳到 accepting，不進 fixing。

```
if profile.EnablesPhase(fixing) && deepReviewVerdict != PASS {
    → fixing
} else {
    → accepting
}
```

### 改動範圍

- `internal/orchestrator/deep_review.go` — `deepTransitionAccepting()` 加 verdict 判斷
- 需要一個 helper 解析 deep-review-report.md 的 verdict（grep `**PASS**` 或結構化解析）

### 不改

- state machine transitions 表不動（deep-reviewing → accepting 已是合法轉換）
- profile 結構不動

---

## 2. Review Package

### 現狀

reviewer.md.tmpl 指示 role 自己跑 `git diff HEAD~N`，diff 展開進 LLM 上下文後每步都帶著。同一份 diff 在 coder、reviewer、deep-reviewer 被重複取得。

### 改法

#### 2a. 生成 review-package.md

orchestrator 在 coding/amending → reviewing 轉換時，用 Go 呼叫 git 生成 `review-package.md`：

```markdown
# Review Package

## Commits
<git log --oneline {baseCommit}..HEAD>

## File Changes
<git diff --stat {baseCommit}..HEAD>

## Full Diff
<git diff {baseCommit}..HEAD>
```

寫到 `.4x/run/{feature-id}/rounds/round-{n}/review-package.md`。

#### 2b. baseCommit 記錄

orchestrator 在 feature 首次進入 coding phase 時，記錄當前 HEAD 為 `baseCommit`，存在 state.json。後續所有 review 都用這個 base。amending 階段沿用同一 base（累積 diff）。

#### 2c. Template 更新

reviewer.md.tmpl / deep-reviewer.md.tmpl 改為：

```
讀取 review-package.md 取得本輪完整 diff。
不要自己跑 git diff / git log / git show 等命令來取得變更資訊。
如果 review-package.md 不存在，再 fallback 自己跑 git diff。
```

### 改動範圍

- `internal/orchestrator/orchestrator.go` — 在 phase transition hook 加 review package 生成
- `internal/protocol/types.go` — State 結構加 `BaseCommit string`
- `templates/reviewer.md.tmpl` — 改讀 review-package.md
- `templates/deep-reviewer.md.tmpl` — 同上

---

## 3. Per-Role Model 路由

### 現狀

`PhaseSpec` 已有 `Model string` 欄位。Built-in profiles 沒填，runner 是否消費這個欄位待確認。

### 改法

#### 3a. 確認消費路徑

確認 orchestrator 啟動 runner 時有把 `PhaseSpec.Model` 傳給子程序環境變數（如 `FOURX_MODEL`）。如果沒有就補上。Plugin template 需能讀取並使用這個 model 指定。

#### 3b. Built-in profiles 填入建議值

```go
var DefaultProfiles = map[string]ProfileConfig{
    "full": {Phases: []PhaseSpec{
        {Phase: "designing",       Model: "opus"},
        {Phase: "design-reviewing", Model: "opus"},
        {Phase: "coding",          Model: "sonnet"},
        {Phase: "reviewing",       Model: "opus"},
        {Phase: "testing",         Model: "sonnet"},
        {Phase: "deep-reviewing",  Model: "opus"},
        {Phase: "fixing",          Model: "sonnet"},
        {Phase: "accepting",       Model: "sonnet"},
    }},
    // lite, normal, quick 同理
}
```

#### 3c. 覆寫機制

feature YAML 的 `profile` 欄位可內聯 phase model 覆寫：

```yaml
profile: full
model_overrides:
  coding: opus    # 這個 feature 的 coder 需要強推理
```

Settings.json 的 `default_profile` 同理支援。

### 改動範圍

- `internal/protocol/profile.go` — DefaultProfiles 填 Model
- `internal/orchestrator/orchestrator.go` — 啟動 runner 時傳 Model
- `internal/protocol/types.go` — FeatureConfig 加 `ModelOverrides map[string]string`（如果沒有的話）

---

## 4. "Cannot Verify from Diff" 狀態

### 現狀

reviewer template 只有 PASS/FAIL/CONDITIONAL PASS 三種 verdict。遇到跨 scope 的問題，reviewer 會自己到處 grep/cat 探索 codebase，燒 token。

### 改法

在 reviewer.md.tmpl 和 deep-reviewer.md.tmpl 加入 finding 類型指引：

```markdown
## UNVERIFIABLE_FROM_DIFF

有些需求無法僅從當前 diff 驗證，例如：
- 跨任務整合行為
- Runtime / 效能特性
- 需要執行測試才能確認的項目
- 涉及未在本輪修改的檔案

對這類項目：
1. 標記為 `[UNVERIFIABLE]`，說明無法驗證的原因
2. **不要**為了驗證這類項目而自行跑大量 git/grep/cat 命令探索 codebase
3. 這些項目不計入 PASS/FAIL 判定（不算 warning 或 critical）
4. 它們會被帶到 acceptor 層處理
```

### 改動範圍

- `templates/reviewer.md.tmpl` — 加指引段落
- `templates/deep-reviewer.md.tmpl` — 加指引段落

### 不改

- verdict 枚舉不加新值（UNVERIFIABLE 是 finding 層級，不是 verdict 層級）
- Go code 不改（純 template 變更）

---

## 5. Acceptor 預彙整包

### 現狀

`runRoundSummarizer()` 只在 round≥3 觸發。Acceptor 自己讀 5-6 份報告，AC 在每份報告都被重述一遍。

### 改法

#### 5a. 生成 acceptance-summary.md

orchestrator 在進 accepting 前，用 Go code 解析各報告產出彙整：

```markdown
# Acceptance Summary

## AC Status
| # | Criterion | Status | Evidence Source |
|---|-----------|--------|-----------------|
| AC-1 | ... | PASS | review-report.md §Implementation |
| AC-2 | ... | PASS | test-report.md §Results |
| AC-3 | ... | UNVERIFIABLE | reviewer 標記 — 需 runtime 驗證 |

## Deep Review Findings
| # | Finding | Final Status |
|---|---------|-------------|
| 1 | ... | RESOLVED (commit abc123) |

## Unverifiable Items
- [from reviewer] ...
- [from deep-reviewer] ...

## Verification Commands Run
- go test -race ./... : PASS (94 tests)
- go vet ./... : PASS
```

寫到 `.4x/run/{feature-id}/rounds/round-{n}/acceptance-summary.md`。

#### 5b. Template 更新

acceptor.md.tmpl 改為：

```
優先讀 acceptance-summary.md 取得各 AC 的最終狀態。
只在需要深入某個 AC 的具體實作細節時才讀原始報告。
```

### 改動範圍

- `internal/orchestrator/orchestrator.go` — 進 accepting 前生成 acceptance-summary.md
- 需要 report parsing helpers（解析 review-report / test-report / deep-review-report 的結構化資訊）
- `templates/acceptor.md.tmpl` — 改讀 acceptance-summary.md

### 解析策略

報告格式是 markdown，結構相對固定（有 `## Verdict`、`| # | Criterion |` 表格）。用 regex 或簡單行掃描即可，不需完整 markdown parser。如果解析失敗，acceptor fallback 讀原始報告（不阻塞流程）。

---

## 預估效果

| 優化項 | 預估 token 節省 | 改動複雜度 |
|--------|----------------|-----------|
| 跳冗餘 Fixer | ~15-20%（省整個 session） | 低 |
| Review package | ~10% per review role | 中 |
| Per-role model | ~20-30%（Sonnet 比 Opus 便宜 ~5x） | 低 |
| Cannot verify from diff | ~5-10%（減少 reviewer 探索） | 低（純 template） |
| Acceptor 預彙整 | ~5%（減少重複讀報告） | 中 |

## 不做

- 不改 state machine transitions 表
- 不改 profile 結構（PhaseSpec 已有 Model 欄位）
- 不合併 reviewer 和 tester（4x 的分工和 Superpowers 不同，tester 跑實際命令）
- 不壓 plan/task-brief 字數（SP v6 實驗證明這會傷測試訊號）
- 不引入新的 phase 或 role
