Go 單元測試規範：
- 用 go test 執行，t.TempDir() 做 workspace 隔離
- 不要污染實際 .4x/ 目錄——測試在臨時目錄操作，測完自動清理
- table-driven test 處理多種輸入組合
- error case 也要測——確認 error message 有意義
- verify.json 每項 AC 都要有對應的 pass/fail 結果
