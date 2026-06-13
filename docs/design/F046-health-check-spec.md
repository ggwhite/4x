# F046: Health Check — Auto Verify Environment Before Testing

## 現狀

Tester role 開始前沒有環境檢查，環境壞了（build 失敗、服務沒起來）會浪費整輪 test cycle。

## 需求

Testing phase 開始前自動跑 health check commands；失敗時嘗試 recovery；recovery 失敗 escalate 到 needs-attention。

## 前置依賴

F045（phase-hooks）必須先完成。F046 的 health check 在 F045 的 `pre_testing` hooks 之後、Tester role 啟動之前執行。

## Config 結構

### HealthCheck struct

```go
type HealthCheck struct {
    Commands []string `yaml:"commands" json:"commands"`
    Recovery []string `yaml:"recovery,omitempty" json:"recovery,omitempty"`
    Timeout  int      `yaml:"timeout,omitempty" json:"timeout,omitempty"` // 秒，預設 30
}
```

### 兩層配置

**settings.json（全域預設）：**
```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

**test-strategy.yaml（per-feature override）：**
```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**Merge 規則：** test-strategy.yaml 有 `health_check` 就整組覆蓋 settings.json 的，沒設就用全域的。不做欄位級 merge。

## 執行流程

```
Testing phase 開始
  ↓
F045 pre_testing hooks 跑完
  ↓
Health check commands 逐一執行（任一失敗即停）
  ├─ 全部通過 → 啟動 Tester role
  └─ 任一失敗 →
      ├─ 有 recovery commands → 逐一執行
      │   ├─ recovery 通過 → 重跑全部 health check（最多重試 1 次）
      │   │   ├─ 通過 → 啟動 Tester role
      │   │   └─ 仍失敗 → escalate to needs-attention
      │   └─ recovery 失敗 → escalate to needs-attention
      └─ 無 recovery → escalate to needs-attention
```

Recovery 最多觸發一次，避免無限循環。

## 新增程式碼

| 檔案 | 改動 |
|---|---|
| `internal/protocol/types.go` | 新增 `HealthCheck` struct；`Config` 加 `HealthCheck *HealthCheck` 欄位；`TestStrategy` 加 `HealthCheck *HealthCheck` 欄位 |
| `internal/health/health.go`（新）| `RunHealthCheck(cfg HealthCheck, executor func(cmd string) error) error` |
| `cmd/4x/run.go` | testing phase 啟動前讀取 health check config（merge 兩層），呼叫 `health.RunHealthCheck`，失敗轉 needs-attention |

## Escalation

失敗時寫 event 並 transition：

```json
{"type": "health-check-failed", "role": "tester", "message": "health check failed: <cmd> exited 1; recovery failed"}
```

Phase 轉為 `needs-attention`。

## 約束

- 不新增角色，由 CLI 層（run loop）處理
- 不改 F045 的 hook 結構
- health check config 都沒設時（全域和 per-feature 都沒有），跳過 health check 直接啟動 Tester
- 每個 command 的 timeout 預設 30 秒，可配置
