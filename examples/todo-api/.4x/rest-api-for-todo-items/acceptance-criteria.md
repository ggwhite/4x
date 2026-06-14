# Acceptance Criteria — REST API for todo items

| #   | Criterion                                                                 | Verification Method |
|-----|---------------------------------------------------------------------------|---------------------|
| AC-1 | GET /v1/todos 回傳 200 並回傳 JSON 陣列                                     | curl 200 + JSON     |
| AC-2 | POST /v1/todos 能建立一筆項目並回傳 201 與 Location header                 | curl POST -> 201    |
| AC-3 | GET/PUT/DELETE /v1/todos/{id} 對不存在的 id 回傳 404                       | curl 404            |
| AC-4 | 非法輸入回傳 400 並包含結構化錯誤訊息                                      | curl 400 + JSON     |
| AC-5 | 整合測試（integration tests）在真實 PostgreSQL 環境下皆通過               | go test ./backend/... |

每個 AC 必須在 test-report.md 中提供可驗證的證據（命令輸出、HTTP 回應片段或測試報告片段）。
