# Coder Report — Round 1

## What Was Done

實作 `4x learn add` CLI subcommand，讓 standalone session 可以直接寫入 learning。

1. **FindSimilar 方法**：在 `Store` 新增 `FindSimilar(content string) *Entry`，以三層比對（exact → normalized → Jaccard ≥ 0.7）搜尋相似條目。
2. **learn add subcommand**：新增 `4x learn add --category <cat> --content <text> [--json]`，驗證 category 白名單、content 非空、fuzzy 重複偵測，正常寫入後印出新 ID。
3. **MCP 工具**：新增 `4x_learn_add` MCP tool（LearnAddInput + LearnAdd handler），透過 `--json` 委託 CLI。
4. **測試**：FindSimilar 四個測試（exact/normalized/jaccard/no-match）、learn add 四個測試（success/invalid-category/fuzzy-duplicate/json-output）。
5. **文件**：更新 cli.md 及五個語言翻譯（zh-TW/zh-CN/ja/ko/es），包含 learn add 用法、dedup 行為說明、ops category、MCP tools 表格。

## Files Changed

- `internal/learning/store.go` — 新增 `FindSimilar` 方法
- `internal/learning/store_test.go` — 新增 4 個 FindSimilar 測試
- `cmd/4x/learn.go` — 新增 `newLearnAddCmd()`，註冊到 `newLearnCmd()`
- `cmd/4x/learn_test.go` — 新增 4 個 learn add CLI 測試
- `internal/mcp/tools.go` — 新增 `LearnAddInput`、`LearnAdd` handler
- `internal/mcp/server.go` — 註冊 `4x_learn_add` MCP tool
- `docs/guide/cli.md` — 加入 learn add 段落和 MCP table row
- `docs/guide/zh-TW/cli.md` — 繁體中文翻譯
- `docs/guide/zh-CN/cli.md` — 簡體中文翻譯
- `docs/guide/ja/cli.md` — 日文翻譯
- `docs/guide/ko/cli.md` — 韓文翻譯
- `docs/guide/es/cli.md` — 西班牙文翻譯

## Verification

- `make build`: OK
- `make test`: OK (all packages pass with -race)
- `make lint`: OK (0 issues)
- `make check-docs`: OK (all 26 CLI commands documented)
- `make check-i18n`: OK (all locale files have matching keys)
- `make check-guide-i18n`: OK (pre-existing force-done warnings only)
