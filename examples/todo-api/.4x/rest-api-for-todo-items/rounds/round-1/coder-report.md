# Coder Report — Round 1

## Summary
在 backend 新增 Todo 的資料表 migration 並實作 Postgres-based store 及 HTTP handler。Build & unit tests 通過。

## Files changed
- backend/db/migrations/001_init.sql — 新增 todos table
- backend/internal/todo/store.go — 新增 Postgres 實作
- backend/api/todo.go — 新增 handler 與路由註冊
- backend/integration/todo_e2e_test.go — 新增整合測試（需 Postgres）

## Verification
- cd backend && go build ./...  -> exit 0
- cd backend && go test ./...   -> exit 0 (all tests passed)

## Notes
- 測試使用本地 Postgres（或 CI 提供的 service）；若無法啟動，請在 Tester 階段標記 SKIP 並說明原因。
