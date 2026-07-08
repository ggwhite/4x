# 開發過程中發現的範圍外功能缺口

> 任何角色（Designer/Design Reviewer/Coder/Reviewer/Tester/Fixer）在分析或開發過程中，若發現明確需要另開 feature 處理、但不阻塞目前任務的範圍外問題，append 一行到這裡（不要自行擴大目前 feature 的 scope、不要動手做、不要自己呼叫 `4x new`——這是給使用者決定優先度的候選清單）。
>
> 格式：`- YYYY-MM-DD [來源 FXXX] 發現內容 — 建議：<新 feature 名稱/範圍>`
>
> 已經開了對應 feature 的項目，在後面加 `[已開 FYYY]` 並保留紀錄（不刪除，方便追溯發現脈絡）。

- 2026-07-07 [來源 F132] `internal/orchestrator/resume.go` 的 `SmartResumePhase`（crash resume 用）在 deep-review PASS 後一律直接轉 `PhaseAccepting`，完全跳過 Fixing；而正常 live-run 路徑（`deepTransitionAccepting`）在 profile 啟用 Fixing 時，deep-review 為 CONDITIONAL PASS（有 warning）會進 Fixing。兩條路徑對「同一個 deep-review-report.md」可能推導出不同的下一個 phase，是 resume 與 live-run 行為不一致的既有缺口（F132 之前就存在，此次只是发现，未修改該路徑，維持既有行為）。— 建議：新開 feature 讓 `SmartResumePhase` 對 deep-review PASS 的判斷與 `deepTransitionAccepting`（含乾淨 PASS 判斷）對齊。 [已開 F133]
- 2026-07-07 [來源 F132] `internal/orchestrator/phase.go` 的 `NextPhaseAfter` 內 `case protocol.PhaseDeepReviewing` 分支，在目前 live-run 主迴圈中因 `routePhase` 已將 `PhaseDeepReviewing` 導向專屬的 `runDeepReviewPhase`（見 orchestrator.go `routePhase`），實際上是永遠不會被觸發的死碼，且其邏輯（一律轉 Fixing、不判斷乾淨 PASS）與 `deepTransitionAccepting` 的新行為不一致。— 建議：新開 feature 清理此死碼或補上測試以確認其用途（若真的無用途則移除，若有隱藏呼叫路徑則需對齊行為）。 [已直接修正：dead code 已刪除]
- 2026-07-07 [來源 F132] Review round 1 期間發現：`rounds/round-{n}/role-learnings.json` 是同一個 round 目錄下所有角色（coder/reviewer/tester/fixer/design-reviewer/deep-reviewer）共用同一檔名（見 `internal/protocol/workspace.go` `RoleLearningsFileName` 與各 `templates/*.tmpl`），且 `RoleLearningsFile` 結構只存一個 `role` 欄位。同一 round 內若有多個角色都寫了 role-learnings.json，後寫入者會直接覆蓋前者，`harvestRoleLearnings`（`internal/prompt/learnings.go`）每個 round 目錄只解析一份檔案，較早角色的 learnings 會在未被記錄的情況下靜默遺失（本輪即實測發生：coder 的 role-learnings.json 被 reviewer 覆寫前已先讀出保留，但若非人工注意會遺失）。— 建議：新開 feature 讓每個角色寫入獨立檔名（如 `role-learnings-{role}.json`）或改為 append 制，避免 harvest 階段的 learnings 遺失。 [已開 F134]
- 2026-07-07 [來源 F138] `.github/workflows/release.yml` 的 macos job 仍執行 `make package-macos`（ad-hoc 簽名），未接上 F138 新增的 Developer ID 簽名與公證流程；正式 GitHub Release 產出的 .dmg 仍未經公證。要在 CI 自動簽名+公證需設定 GitHub Secrets（Developer ID 憑證、App Store Connect API key）並在 workflow 匯出為對應環境變數後改呼叫 `make dashboard-release`。— 建議：新開 feature 讓 CI release 產出已公證的 .dmg。 [已開 F139]
- 2026-07-08 [source: model-routing] roles 白名單（settings 驗證）不含 fixer 與 mini-coder，無法對這兩個真實 role 做 per-role model 路由（fixer 應可設 opus、mini-coder 應可設 sonnet）；昨日 kairos 856404a 加 fixer entry 後 settings 載入失敗即此因 — suggested follow-up: 擴充 roles 白名單納入 fixer/mini-coder，並順手檢查 roleCategoryMap 是否同步
