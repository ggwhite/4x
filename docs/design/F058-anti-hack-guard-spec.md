# F058: Anti-Hack Guardrails for Metric-Driven Features

## 現狀

4x 的 guardrail 能防 agent 偷改測試（硬規則禁止）、超出 scope（checkScope），但對有量化指標的任務（效能優化、準確率提升、覆蓋率改善等），agent 可能用合規但取巧的方式灌指標——過擬合、刪難測案例、hardcode 已知答案、只跑容易的 benchmark。目前缺少三道防線：

1. **Holdout**：沒有機制讓特定路徑對 agent 不可見
2. **非 hack 論述**：reviewer 沒被要求論證改善來自真實改進
3. **殘酷指標**：沒有多維度回歸檢查，agent 可以犧牲 A 指標換 B 指標

## 設計

三道防線各在擅長的層處理：holdout 和殘酷指標在 guard 層硬擋，非 hack 論述在 template 層引導。

### 1. Feature YAML Schema 擴展

新增三個 optional 欄位，沒設定時行為完全不變：

```yaml
metrics:
  - name: test_pass_rate
    direction: higher           # higher | lower
    command: "go test ./..."
    extract: "coverage: (\\d+\\.\\d+)%"
  - name: p99_latency_ms
    direction: lower
    command: "./bench.sh"
    extract: "p99=(\\d+)"

holdout_paths:
  - "testdata/holdout/**"
  - "bench/golden/*.json"

anti_hack: true
```

Go struct 新增：

```go
type Metric struct {
    Name      string `yaml:"name" json:"name"`
    Direction string `yaml:"direction" json:"direction"`
    Command   string `yaml:"command" json:"command"`
    Extract   string `yaml:"extract" json:"extract"`
}

// Feature struct 新增
Metrics      []Metric `yaml:"metrics,omitempty" json:"metrics,omitempty"`
HoldoutPaths []string `yaml:"holdout_paths,omitempty" json:"holdout_paths,omitempty"`
AntiHack     bool     `yaml:"anti_hack,omitempty" json:"anti_hack,omitempty"`
```

`schemas/feature.schema.json` 同步新增對應定義。

### 2. Baseline Metrics Capture

在 `guard.CaptureBaseline()` 裡，若 feature 有 `metrics`，對每個 metric 執行 `command`，用 `extract` regex 提取數值，存入 `baseline.json`：

```json
{
  "repos": { "...": "..." },
  "metrics": {
    "test_pass_rate": 85.2,
    "p99_latency_ms": 118
  }
}
```

`Baseline` struct 新增 `Metrics map[string]float64 json:"metrics,omitempty"`。

若 command 執行失敗或 regex 無 match，該 metric 的 baseline 記為 `null`，後續比較時跳過（不因採集失敗擋住流程）。

### 3. Holdout Scope 檢查

新增 `checkHoldout()` 掛在 `Check()` 的 `checkScope()` 之後。

邏輯：
1. 讀 `feature.HoldoutPaths`，空則跳過
2. 取 `git diff --name-only HEAD` 的 changed files
3. 對每個 changed file，用 `filepath.Match` 比對所有 holdout glob patterns
4. 任一 match → 加入 errors（hard fail，不是 warning）

複用 checkScope 的 git diff 結果，不重複執行。

### 4. 殘酷指標比較

新檔 `internal/guard/metrics.go`：

```go
func CompareMetrics(
    baseline map[string]float64,
    current map[string]float64,
    defs []protocol.Metric,
) (pass bool, details []string)
```

規則：
- `direction=higher`：`current < baseline` → FAIL
- `direction=lower`：`current > baseline` → FAIL
- baseline 為 null 的 metric 跳過
- **任一 metric FAIL → 整體 FAIL**（不做加權平均）
- details 回傳每個 metric 的 baseline vs current 對比

掛載點：`checkTestingToAccepting` gate，在現有的 verify.json passed 檢查之後。

### 5. verify.json 擴展

`VerifyEvidence` struct 新增 `Metrics map[string]float64 json:"metrics,omitempty"`。

Tester 在跑完 verify commands 後，對每個 feature metric 執行 command + extract，填入 verify.json：

```json
{
  "passed": true,
  "commands": ["..."],
  "metrics": {
    "test_pass_rate": 87.5,
    "p99_latency_ms": 105
  }
}
```

### 6. Template 修改

**coder.md.tmpl** — holdout 警告 + metrics 採集指示：

```
{{if .Feature.HoldoutPaths}}
## Holdout 路徑（禁止讀寫）

以下路徑是 holdout 資料，你不可以讀取或修改這些檔案：
{{range .Feature.HoldoutPaths}}- {{.}}
{{end}}
違反將導致 guardrail 失敗。
{{end}}

{{if .Feature.Metrics}}
## 指標採集

每輪結束時，在 verify.json 的 metrics 欄位記錄以下指標的當前值：
{{range .Feature.Metrics}}- {{.Name}}（{{.Direction}} is better）：`{{.Command}}`
{{end}}
{{end}}
```

**reviewer.md.tmpl / deep-reviewer.md.tmpl** — 非 hack 論述：

```
{{if .Feature.AntiHack}}
## 非 Hack 論述（必填）

此 feature 啟用了 anti-hack 檢查。對於以下每個指標改善，你必須在 review report 中回答：
{{range .Feature.Metrics}}- {{.Name}}：改善是否來自真實的程式碼改進？排除過擬合、hardcode、刪測試案例等取巧手段。
{{end}}
若無法確認改善非 hack，verdict 必須為 FAIL。
{{end}}
```

**tester.md.tmpl** — metrics 採集要求：

```
{{if .Feature.Metrics}}
## 指標採集（必填）

verify.json 必須包含 metrics 欄位。對每個指標執行指令並提取數值：
{{range .Feature.Metrics}}- {{.Name}}：`{{.Command}}` → regex `{{.Extract}}`
{{end}}
{{end}}
```

### 7. 資料流

```
Feature YAML
  │ metrics, holdout_paths, anti_hack
  ▼
CaptureBaseline ──→ baseline.json { metrics: {name: value} }
  │
  ▼
Coder ◄── prompt: holdout 警告 + metrics 指示
  │
  ▼
Guard: checkHoldout(diff, holdout_paths) ──→ touched = HARD FAIL
  │
  ▼
Tester ──→ verify.json { metrics: {name: value} }
  │
  ▼
Guard: CompareMetrics(baseline, verify, defs) ──→ any regression = HARD FAIL
  │
  ▼
Reviewer ◄── prompt: 非 hack 論述要求
  │
  ▼
Accept or Reject
```

## 影響範圍

| 檔案 | 變更類型 |
|---|---|
| `internal/protocol/types.go` | 新增 Metric struct、Feature/Baseline/VerifyEvidence 欄位 |
| `internal/guard/check.go` | 新增 checkHoldout()、在 Check() 串接 |
| `internal/guard/metrics.go` | 新檔：CompareMetrics()、captureMetricValues() |
| `internal/guard/metrics_test.go` | 新檔：殘酷指標比較測試 |
| `internal/guard/check_test.go` | 新增 holdout 測試 |
| `templates/coder.md.tmpl` | 新增 holdout 警告 + metrics 採集段落 |
| `templates/reviewer.md.tmpl` | 新增非 hack 論述段落 |
| `templates/deep-reviewer.md.tmpl` | 新增非 hack 論述段落 |
| `templates/tester.md.tmpl` | 新增 metrics 採集要求 |
| `schemas/feature.schema.json` | 新增 metrics、holdout_paths、anti_hack 定義 |

## 約束

- 所有新欄位 optional，沒設定時行為完全不變
- 不改現有 verify.json 結構，只新增 metrics 欄位
- holdout 檢查複用 checkScope 的 git diff 結果
- 純功能開發的 feature（無 metrics）零影響
- 不在 guard 層解析自然語言（非 hack 論述由 template 引導、reviewer 產出）
- metric command 執行失敗不阻塞流程，僅跳過該 metric 的比較
