# Test Report — Round 1

## Summary
PASS — 4/4 acceptance criteria met (AC-1..AC-4). 整合測試在有 Postgres 的環境下通過。

## Results
| # | Criterion | Status | Evidence |
|---|---|---|---|
| AC-1 | GET /v1/todos 返回 200 + JSON | PASS | `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/todos` => 200 |
| AC-2 | POST /v1/todos 返回 201 + Location | PASS | `curl -i -X POST ...` 回傳 201 並含 Location header |
| AC-3 | GET/PUT/DELETE 不存在 id 返回 404 | PASS | `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/todos/does-not-exist` => 404 |
| AC-4 | 非法輸入返回 400 結構化錯誤 | PASS | POST 空 body 返回 400 並含 JSON 錯誤說明 |

## Verdict
PASS
