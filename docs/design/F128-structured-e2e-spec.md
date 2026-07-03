# F128 — Structured e2e-screenshot verify_type for guard

## 問題

`test-strategy.yaml` 的 `ACVerifyMap`（`internal/protocol/verify.go:72-74`）目前只有 `unit-test`/`integration`/`execution`/`inspection`/`skip` 五種 verify_type（合法值定義在 `internal/guard/check.go:265-271` 的 `acVerifyTypes`），沒有一種能宣告「這條 AC 需要 e2e 截圖佐證」。

Tester 有沒有真的截圖完全沒人檢查：guard 的 `checkACEvidence`（`check.go:279`）只驗證 evidence 文字是否符合 `executionPattern`（`check.go:273`，抓 `PASS`/`FAIL`/`exit code`/`stdout` 等字樣），跟 `VerifyEvidence.Screenshots`（`internal/protocol/verify.go:23`，型別 `[]feat.Screenshot`）完全沒有交叉驗證。結果是：像「這個功能有時候要開 e2e 截圖，有時候只要打 API 打得通」這種條件式需求，沒有任何機制能強制 tester 真的照 Designer 的意思做——即使 Designer 已經決定這條 AC 需要視覺證據。

## 目標

1. `ACVerifyMap` 新增合法值 `e2e-screenshot`，讓 Designer 在 test-strategy.yaml 針對需要視覺驗證的 AC 明確宣告。
2. Guard 在 testing→accepting 的既有關卡（`checkTestingToAccepting`，`check.go:171`）新增檢查：宣告 `e2e-screenshot` 的 AC，若沒有對應且真的存在於磁碟上的截圖檔案，判定失敗。
3. 失敗後沿用 testing phase 既有的 retry/amending 路徑（`internal/orchestrator/phase.go:80-98`），不新增任何 state machine 轉換。

## 設計

### 驗證範圍（刻意收斂）

只檢查「有沒有確實留下截圖檔案」，**不判斷截圖內容是否真的對應該條 AC**（不加 vision model 呼叫）。內容正確性留給人工或既有的 deep-review 階段判斷，這次只解決「tester 有沒有跳過該做的事」這個機械可驗證的問題。

需要打 API 驗證、不需要截圖的 AC，沿用既有的 `execution` 類型即可（`executionPattern` 本來就能吃 API 呼叫的輸出證據，例如 exit code、HTTP 狀態文字），不新增「api-only」這個額外類型。

### `acVerifyTypes` 新增合法值

`internal/guard/check.go:265-271`：

```go
var acVerifyTypes = map[string]acVerifyType{
	"unit-test":      {needsExec: true},
	"integration":    {needsExec: true},
	"execution":      {needsExec: true},
	"inspection":     {needsExec: false},
	"skip":           {needsExec: false},
	"e2e-screenshot": {needsExec: false},
}
```

`needsExec: false` 是因為 `e2e-screenshot` 的證據要求不是「文字要符合 execution pattern」，而是另一套獨立的截圖存在性檢查（見下）。既有的通用檢查（`ac.Passed` 必須 true、`ac.Evidence` 非空，`check.go:312-322`）依然適用，不受影響。

同時更新 `checkTestStrategyVerifyTypes`（`check.go:257`）的錯誤訊息，把合法值清單從 `"valid: unit-test, integration, execution, inspection, skip"` 改成含 `e2e-screenshot`，讓 Designer 寫錯字時，`4x check` 當下就能發現非法值。

### 新增 `checkScreenshotRequirement`

仿照 `checkManualChecks`（`check.go:355`）的結構，新增函式：

```go
// checkScreenshotRequirement 驗證宣告 verify_type=e2e-screenshot 的 AC 確實留有截圖證據。
// 只檢查檔案是否存在，不判斷截圖內容是否對應該條 AC。
func checkScreenshotRequirement(ws *protocol.Workspace, ts protocol.TestStrategy, evidence protocol.VerifyEvidence, r *CheckResult) {
	resultMap := make(map[string]protocol.ACEvidence, len(evidence.ACResults))
	for _, ac := range evidence.ACResults {
		resultMap[ac.ID] = ac
	}
	for acID, vt := range ts.ACVerifyMap {
		if vt != "e2e-screenshot" {
			continue
		}
		if ac, ok := resultMap[acID]; !ok || !ac.Passed {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: verify_type=e2e-screenshot but AC missing or not passed in ac_results", acID))
			r.RetryableErrors++
			continue
		}
		if !hasExistingScreenshot(ws.Root, evidence.Screenshots) {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf(
				"%s: verify_type=e2e-screenshot but no screenshot file found — tester must capture and record a screenshot, not skip", acID))
			r.RetryableErrors++
		}
	}
}

// hasExistingScreenshot 檢查截圖清單裡是否至少一個檔案真的存在於磁碟上。
// 路徑正規化沿用 feat.NormalizeScreenshotPath（與 workspace_screenshot.go 的
// discoverFromVerify 同一套慣例），相對路徑以 root（guard 執行當下的 ws.Root，
// testing phase 底下即 worktree 根目錄）解析。
func hasExistingScreenshot(root string, shots []feat.Screenshot) bool {
	for _, s := range shots {
		path := feat.NormalizeScreenshotPath(s.Path)
		if path == "" {
			continue
		}
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, path)
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
```

不區分「哪張截圖對應哪條 AC」——同一個 round 內只要至少一張截圖檔案真的存在，所有宣告 `e2e-screenshot` 的 AC 就共用這個「有留下證據」的判斷。多條 AC 各自要求不同截圖的精確對應留待未來需要時再加，目前先解決「完全沒截圖」的漏洞（YAGNI）。

`root` 用 `ws.Root`：guard 在 testing phase 執行當下，`ws` 是從 worktree 目錄 `protocol.Find(cwd)` 解析出來的（plugin 契約要求 tester 在自己的 worktree 內跑 `4x check`），所以 `ws.Root` 自然就是當下 worktree 根目錄，跟 `feat.Screenshot.Path` 的相對路徑基準一致，不需要額外處理 main repo／worktree 的落差（這點跟 dashboard 端的 `DiscoverScreenshots` 需要額外掃 `discoverFromWorktree` 不同——guard 檢查發生在單一、確定的執行時空，不需要那層兼容）。

### 插入點

`internal/guard/check.go:242-244`，`checkTestingToAccepting` 內：

```go
	checkACEvidence(ts, evidence, r)
	checkScreenshotRequirement(ws, ts, evidence, r)
	checkManualChecks(ts, evidence, r)
	checkSelfModTestGate(ws, featureID, r)
```

失敗後 `r.RetryableErrors` 遞增，會被 `internal/orchestrator/phase.go:80-98` 既有的路徑吃到：retryable 且未達 `MaxGuardRetries`（2 次）→ 留在 testing 重跑；超過上限 → 回 `amending`。跟現有 `checkManualChecks` 失敗走的是同一條路，不需要新增任何 state machine 轉換或新的重試計數器。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/guard/check.go` | `acVerifyTypes` 新增 `e2e-screenshot`；`checkTestStrategyVerifyTypes` 錯誤訊息更新；新增 `checkScreenshotRequirement`、`hasExistingScreenshot`；`checkTestingToAccepting` 插入呼叫 |
| `docs/guide/concepts.md` | 補充 `ac_verify_map` 合法值說明（若該文件已列出既有五種值，需同步加上 `e2e-screenshot` 並說明何時該用它 vs `execution`） |

## 不做的事

- 不判斷截圖內容是否真的對應該條 AC（不加 vision model 呼叫）
- 不新增「api-only」verify_type（沿用既有 `execution`）
- 不新增 state machine 轉換，不碰 `internal/state/machine.go`
- 不處理 accepting phase 驗收失敗自動打回 amending（獨立問題，範圍已在前次討論中決定分開另開 feature）
- 不做精確的「哪張截圖對應哪條 AC」比對，只要求 round 內至少一張截圖存在
