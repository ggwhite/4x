# Task Brief — REST API for todo items

## 目標
實作一個簡單且可測試的 CRUD REST API，用於管理 todo items，包含五個端點：
- GET  /v1/todos
- POST /v1/todos
- GET  /v1/todos/{id}
- PUT  /v1/todos/{id}
- DELETE /v1/todos/{id}

## 具體任務
1. 在 backend 建立資料模型與資料庫 migration（檔案：backend/db/migrations/001_init.sql）。
2. 實作資料存取層：backend/internal/todo/store.go（Postgres 實作）。
3. 實作 HTTP API handler：backend/api/todo.go（註冊 /v1/todos 路由與 handler）。
4. 新增整合測試：backend/integration/todo_e2e_test.go（使用真實 Postgres 或測試容器），並更新 CI 驗證命令。
5. 更新 README 或 docs（選做）：backend/README.md，說明如何本地啟動測試 DB 與執行測試。

## 範圍（可修改的目錄/檔案）
- backend/**
- backend/db/migrations/**
- backend/internal/todo/**
- backend/api/**
- backend/integration/**

## 非範圍
- 不得修改其他 repos（無論是 .github 工作流程或上層腳本）
- 不得修改 acceptance-criteria.md 或 test-strategy.yaml

## 備註
- 每次變更後請執行驗證指令（見 test-strategy.yaml 的 verify_commands）。
- 若遇到規格不明或無法在指定範圍內完成的需求，請在 rounds/round-1/escalation.json 提出 escalation。
