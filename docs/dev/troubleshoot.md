# Troubleshooting Playbook

> 操作知識沉澱：環境、工具、build、測試的已知坑與解法。
> 所有 session（4x role、standalone Claude Code、routine）解決新坑後 append 到這裡。

## 條目上限與清理

此檔案上限 **30 條**。超過時，新增前必須先清理：

1. 檢查所有條目，找出**已過時**的（問題已修復、環境已變更、版本已升級）
2. 將過時條目搬到 `docs/dev/troubleshoot-archive.md`（保留完整內容）
3. 確認剩餘條目數 < 30 後再新增

判斷過時的依據：
- 條目描述的問題已被上游修復（如 4x 新版已修、Go 版本已升級）
- 超過 6 個月且與當前環境不符
- 被更新的條目取代（同類問題合併後舊的歸檔）

## 格式

每條 entry 用以下格式：

```
### [簡短標題]
- **症狀**：觀察到什麼錯誤/現象
- **原因**：根因分析
- **解法**：具體步驟
- **來源**：哪個 feature/session 首次遇到
```

---

## Entries

### worktree 內 go build 失敗：package not found

- **症狀**：在 git worktree 內跑 `go build` 或 `go test`，報 package not found
- **原因**：專案根目錄有 `go.work`，worktree 內的 `GOWORK` 環境變數指向原目錄的 `go.work`，路徑不對
- **解法**：在 worktree 內執行前設 `GOWORK=off`，或複製 `go.work` 到 worktree 根目錄
- **來源**：多個 feature（ws-082/083/087/104）反覆遇到

### make lint 報 gofmt 格式不符

- **症狀**：`make lint` 失敗，顯示「以下檔案格式不符，請執行 gofmt -w .」
- **原因**：常見於手動對齊 const block 或改了 import 順序後
- **解法**：`gofmt -w <file>` 修正後重新 commit
- **來源**：常態性問題

### go test -race 偶爾卡住

- **症狀**：`make test` 長時間無輸出，最終 timeout
- **原因**：`internal/server` 的整合測試啟動 HTTP server，port 衝突或 goroutine leak
- **解法**：先確認無其他 `4x live` 或測試程序佔用 port 4567；必要時 `lsof -i :4567` 檢查
- **來源**：開發 server 相關功能時偶發

### build-gate 子程序找不到 go/node 等工具

- **症狀**：`4x check` 的 build-gate 報 `go: command not found`，但在 shell 裡直接跑同一指令正常
- **原因**：GUI app（dashboard）啟動的 4x 不繼承 shell profile，PATH 精簡。v0.3.5 前 `verify.executeCommand` 和 `hook.Execute` 沒有擴充 PATH
- **解法**：已在 v0.3.5 修復（`internal/envutil/env.go` 補上常見工具路徑）。若仍遇到，檢查工具安裝路徑是否在 `envutil.EnrichedEnv()` 的清單中，不在的話需要新增
- **來源**：Kairos ws-108（v0.3.4）

### go.work 環境下 go vet ./... 失敗

- **症狀**：build-gate lint 報 `pattern ./...: directory prefix . does not contain modules listed in go.work`
- **原因**：`go vet ./...` 在有 `go.work` 的 workspace 根目錄下不認得多模組結構
- **解法**：settings.json 的 lint 指令不要用 `go vet ./...`，改用逐 repo cd 的腳本（如 `./lint.sh`），或用 `go vet all`
- **來源**：Kairos ws-108（v0.3.5）

### check-docs-sync 對測試/mock/fixture 檔誤報 doc 需更新

- **症狀**：`make check-docs-sync` 因改了 `internal/server/foo_test.go`、`*_mock.go` 或 testdata fixture 而點名對應 doc（如 dashboard.md）需更新，但這些檔案根本沒有使用者文件表面
- **原因**：check-docs-sync 的 RULES 是「source prefix → doc」的粗粒度正向映射，prefix 下的測試/mock/fixture 檔會被連坐點名
- **解法**：repo 根目錄的 `.docsyncignore` 提供 suppression allowlist。每行一筆，從第一個 `#` 起截斷為註解（含行尾 inline `# 理由`）再 trim；`<glob>` 為 path-level（從所有 doc 映射排除），`<glob><TAB><doc>` 為 pair-level（僅排除該 doc）。glob 的 `*` 匹配任意字元含 `/`。**禁止** glob 恰好等於某條 RULES source prefix（會整條停用規則，腳本會 WARNING 並忽略該筆）。被抑制的 (doc, file) 會印到 stderr、不改變 exit code。relocation/delete 檢查（被刪路徑仍出現在 docs prose）不受抑制、一律照跑。production 檔的誤報刻意不 blanket 抑制——如確定安全請用 pair-level 條目逐檔加理由。
- **來源**：F153
