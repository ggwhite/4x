# Coder Report — Round 1

## What Was Done

實作 F118 learn-context：產生 learnings 的 markdown snapshot，讓 standalone session 透過 CLAUDE.md `@` include 讀到 active learnings。

### 核心功能
1. **GenerateLearningsContext** (`internal/prompt/learnings.go`) — 讀取 active entries，按 category 排序分組，產生 `.4x/learnings-context.md`。空 store 時產生只含 header 的空檔。
2. **自動刷新** — `HarvestLearnings` 寫入成功後、`ApplyConsolidateResult` 成功後自動呼叫 `GenerateLearningsContext`，失敗只 warn 不影響主流程。
3. **CLI subcommand** `4x learn context [--json]` — 手動產生 context snapshot，印出寫入路徑。
4. **Protocol 常量** `LearningsContextFile = "learnings-context.md"`。
5. **CLAUDE.md 整合** — `installPlugins` 結束後以 `ensureAppendImport` 將 `@.4x/learnings-context.md` 附加到根目錄 CLAUDE.md 尾端（不覆蓋既有內容，僅在不存在時新增）。

## Files Changed

- `internal/protocol/workspace.go` — 新增 `LearningsContextFile` 常量
- `internal/prompt/learnings.go` — 新增 `GenerateLearningsContext()` 函數，`HarvestLearnings()` 成功後呼叫
- `internal/prompt/learnings_test.go` — 新增 3 個測試（GroupsByCategory、OnlyActive、EmptyStore）
- `internal/orchestrator/orchestrator.go` — `runConsolidate` 成功後呼叫 `GenerateLearningsContext`
- `cmd/4x/learn.go` — 新增 `newLearnContextCmd()`，註冊到 `newLearnCmd()`，新增 `prompt` import
- `cmd/4x/learn_test.go` — 新增 `TestLearnContext_CLI`
- `cmd/4x/plugin_install.go` — 新增 `ensureAppendImport()`，`installPlugins` 尾部呼叫
- `docs/guide/cli.md` — 新增 `learn context` 說明、hook event `4x_learn_context`、補 `ops` category
- `docs/guide/concepts.md` — 目錄樹新增 `learnings-context.md`

## Verification

- `make build`: OK
- `make test`: 全部通過（含 -race）
- `make lint`: 0 issues
- `make check-docs-sync`: OK
- `make check-i18n`: OK
- `make check-guide-i18n`: OK（pre-existing `force-done` warnings 不影響）
