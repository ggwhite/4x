HTTP API 測試規範：
- 用 Go httptest 套件建立測試 server，或用 curl 打實際端點
- 驗證 status code：2xx 是 happy path、4xx 是 client error、5xx 不該出現
- 驗證 response body：JSON 結構正確、必要欄位存在、值符合預期
- 測試 edge case：空 body、缺必要欄位、無效 ID、超長輸入
- 若 API 有認證，測試未認證和認證場景
- 記錄每次 request/response 作為 evidence
