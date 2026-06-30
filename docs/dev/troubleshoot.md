# Troubleshooting Playbook

> 操作知識沉澱：環境、工具、build、測試的已知坑與解法。
> 所有 session（4x role、standalone Claude Code、routine）解決新坑後 append 到這裡。

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
