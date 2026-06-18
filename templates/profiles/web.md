Web UI 測試規範（Playwright）：
- 用 Playwright headless 模式測試 4x live dashboard（不加 --headed，不可彈出瀏覽器視窗干擾使用者）
- 必須完全隔離：在 /tmp 建立獨立 workspace（4x init），用未佔用的隨機 port 啟動獨立的 4x live instance（不可使用 port 4567 或其他使用者正在使用的 port），測試結束後 kill 該 instance 並清除 /tmp workspace
- 嚴禁影響使用者環境：不可對使用者正在執行的 4x live server 發送任何 API 請求（如註冊 project、修改設定），不可修改使用者的 ~/.4x/ 目錄內容，不可打開可見的瀏覽器視窗
- 測試流程：建隔離 workspace → 啟動獨立 4x live（背景、隨機 port）→ 寫 Playwright 腳本(.ts) → npx playwright test --headed=false 執行 → 截圖 → kill server → 清理 workspace
- 腳本存到 .4x/{feature-id}/e2e/，檔名用描述性名稱如 run-feature.spec.ts
- 截圖存到 settings.json 的 tester.screenshot_dir 指定路徑（預設 .4x/{feature-id}/e2e/screenshots/round-{round}/），檔名格式 {step}-{description}.png（如 01-dashboard-loaded.png）
- Playwright 未安裝時先跑 npx playwright install chromium
- 每個 AC 至少一張截圖作為 evidence
