# AC Verify Type — 驗證類型標記

## 問題

Tester 角色在約 20% 的 test-report 中退化成 Reviewer 行為：用 code location（`file.go:NNN`）充當 per-AC evidence，而非執行命令驗證行為。根本原因是沒有人明確告訴 Tester 每條 AC 該用什麼方式驗證。

## 目標

1. Designer 在寫 AC 時標記每條的驗證類型（`unit-test` / `integration` / `inspection` / `skip`）
2. Tester 依標記決定驗證方式，execution 類必須貼命令輸出
3. Guard 在 testing→accepting 閘門 enforce 標記與 evidence 的一致性
4. 無 Designer 階段時，預設全部從嚴（execution）

## 設計

### 驗證類型定義

| Type | 含義 | Tester 必須提供 | 範例 |
|------|------|-----------------|------|
| `unit-test` | 單元測試可驗證 | 命令 + 測試輸出 | `go test -run TestFindSimilar → PASS` |
| `integration` | 需要啟動服務或跨元件驗證 | 命令 + 執行輸出 | `curl localhost:4567/api/features → 200` |
| `inspection` | 靜態檢查即可（diff、grep、code review） | 非空 evidence | `git diff 無改動 API signature` |
| `skip` | Reviewer 已驗證或不適用 | 不需要 | — |

### acceptance-criteria.md 格式

Designer 產出的表格從三欄改為四欄：

```markdown
| # | Criterion | Verification Method | Verify |
|---|-----------|--------------------:|--------|
| AC-1 | 新增 FindSimilar() 方法 | go test -run TestFindSimilar | unit-test |
| AC-2 | learn add 寫入 learnings.json | 執行 CLI 並檢查檔案 | integration |
| AC-3 | 不改既有 API signature | diff 檢查 | inspection |
| AC-4 | Reviewer 已確認的格式問題 | — | skip |
```

### test-strategy.yaml 格式

新增 `ac_verify_map` 區塊，與 `verify_commands` / `manual_checks` 並列：

```yaml
verify_commands:
  - name: build
    command: make build
  - name: test
    command: make test

ac_verify_map:
  AC-1: unit-test
  AC-2: integration
  AC-3: inspection
  AC-4: skip
```

### ACEvidence struct 變更

`internal/protocol/types.go` 的 `ACEvidence` 加一個欄位：

```go
type ACEvidence struct {
    ID         string   `json:"id"`
    Passed     bool     `json:"passed"`
    Evidence   []string `json:"evidence"`
    VerifyType string   `json:"verify_type,omitempty"`
}
```

`VerifyType` 由 Tester 寫入，用途為記錄與事後分析。Guard 的判斷來源是 test-strategy.yaml 的 `ac_verify_map`，不依賴 Tester 自標值。

### Guard 檢查邏輯

在 `checkTestingToAccepting`（`internal/guard/check.go`）加入 evidence 品質檢查：

```
對每條 ac_results：
  1. 從 test-strategy.yaml 的 ac_verify_map 取 verify_type
     - ac_verify_map 不存在 或 該 AC 未列出 → 預設 "execution"
  2. 依 verify_type 檢查 evidence：
     - unit-test / integration / execution（預設）：
         evidence 至少一條匹配執行模式正則：
         (\$\s|PASS|FAIL|^ok\s|--- |exit code|→|stdout|stderr|\d+\.\d+s)
         全部不匹配 → FAIL：
         "AC-N: verify_type=unit-test but evidence has no execution output"
     - inspection：
         evidence 非空即可（現有檢查不變）
     - skip：
         不檢查 evidence，passed 自動視為 true
  3. verify_type 不在合法值內 → FAIL
```

合法值：`unit-test`、`integration`、`inspection`、`skip`、`execution`（內部預設值，等同 unit-test 級別嚴格度）。

所有錯誤分類為 retryable — Tester 可重跑補正 evidence。

### Template 改動

**Designer** (`templates/designer.md.tmpl`)：
- acceptance-criteria.md 表格加 `Verify` 欄
- 加指引說明四種類型的使用時機
- test-strategy.yaml 輸出加 `ac_verify_map`

**Design Reviewer** (`templates/design-reviewer.md.tmpl`)：
- checklist 加一條：AC 表格是否每條都有 Verify 標記，缺漏則 FAIL

**Tester** (`templates/tester.md.tmpl`)：
- 讀 test-strategy.yaml 的 `ac_verify_map`，無則全部視為 execution
- 填 verify.json 時寫入 `verify_type`
- 強化 "You are NOT a Reviewer"：具體說明 unit-test / integration 必須包含命令+輸出

**Reviewer**：不改動。

### 向後相容

- 舊 feature 無 `ac_verify_map` → 全部 AC 預設 `execution`，Guard 從嚴檢查
- 舊 verify.json 無 `verify_type` 欄位 → Guard 從 test-strategy.yaml 推斷，不依賴此欄位
- 舊 feature 的 run artifacts 不回溯修改

### 不改動的部分

- `4x verify` 指令：跑 build/test/lint，與 AC 驗證類型無關
- verify.json 的 `commands` / `passed` / `evidence` 現有欄位結構
- Reviewer template 與流程
