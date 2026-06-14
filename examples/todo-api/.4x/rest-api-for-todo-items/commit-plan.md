# Commit Plan — REST API for todo items

1. feat(db): add todos table migration
   - backend/db/migrations/001_init.sql
2. feat(store): implement Postgres-backed todo store
   - backend/internal/todo/store.go
3. feat(api): add CRUD handlers and routes
   - backend/api/todo.go
4. test: add integration tests
   - backend/integration/todo_e2e_test.go

請依序分成多個小 commit，確認每一步的測試與 lint 都通過。
