# Coder Report — Round 1

## What Was Done

實作 AC verify type 功能：Designer 在 acceptance-criteria.md 標記每條 AC 的驗證類型，Guard 在 testing→accepting 閘門依標記 enforce evidence 品質。

### 核心變更
1. **ACEvidence 加 VerifyType 欄位** — Tester 寫入，用於記錄與事後分析
2. **TestStrategy 加 ACVerifyMap** — Designer 在 test-strategy.yaml 定義每條 AC 的驗證類型
3. **Guard checkACEvidence 重構** — 從 test-strategy.yaml 讀 ac_verify_map，execution 類 AC 用正則檢查 evidence 是否含命令輸出
4. **Designer template** — acceptance-criteria.md 表格加 Verify 欄、test-strategy.yaml 加 ac_verify_map 指引
5. **Design Reviewer template** — checklist 加 AC verify type 完整性檢查
6. **Tester template** — 讀 ac_verify_map、依 verify_type 規範 evidence 格式、強化 "You are NOT a Reviewer"

### 向後相容
- 舊 feature 無 ac_verify_map → 全部 AC 預設 execution（從嚴）
- 舊 verify.json 無 verify_type → Guard 從 test-strategy.yaml 推斷
- 所有既有測試的 evidence fixture 同步更新為符合 execution pattern 的格式

## Files Changed
- `internal/protocol/types.go` — ACEvidence 加 VerifyType、TestStrategy 加 ACVerifyMap
- `internal/guard/check.go` — checkACEvidence 重構：加入 verify_type 判斷和 execution pattern 正則
- `internal/guard/check_f120_test.go` — 新增 8 個測試覆蓋各 verify_type 場景
- `internal/guard/check_test.go` — 更新 evidence fixture
- `internal/guard/check_f060_test.go` — 更新 evidence fixture
- `internal/guard/check_f108_test.go` — 更新 evidence fixture 和 error message 匹配
- `cmd/4x/run_loop_test.go` — 更新 mock runner evidence fixture
- `internal/orchestrator/phase_test.go` — 更新 evidence fixture
- `templates/designer.md.tmpl` — 加 Verify 欄、ac_verify_map 指引
- `templates/design-reviewer.md.tmpl` — 加 AC verify type 完整性 checklist
- `templates/tester.md.tmpl` — 讀 ac_verify_map、依 verify_type 規範 evidence
- `docs/guide/concepts.md` — 更新 testing→accepting gate 說明

## Verification
- `make build`: PASS
- `make test`: PASS (all packages)
- `make lint`: PASS (0 issues)
- `make check-i18n`: OK
- `make check-guide-i18n`: OK
- `4x check F120-ac-verify-type`: PASS
