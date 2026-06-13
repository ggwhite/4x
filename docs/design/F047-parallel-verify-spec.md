# F047: Parallel Verify — 多 repo build/test 平行執行

## 現狀

多 repo feature 的 verify commands 定義在 `test-strategy.yaml` 的 `verify_commands`（`[]string`），由 Tester AI agent 逐條依序執行。慢，且 AI agent 花 token 在跑 shell 等輸出上。

## 目標

- verify commands 支援分組標記（group）
- 同組內依序執行、不同組平行執行
- 由 CLI goroutine 控制，不靠 LLM
- Tester agent 改為呼叫 `4x verify` 取得結果，專注寫報告
- 人也能獨立跑 `4x verify <featureId>` 除錯

## 設計

### 1. test-strategy.yaml 格式擴展

新增 `verify_groups` key，與既有 `verify_commands` 互斥：

```yaml
# 新格式：verify_groups（組間平行、組內依序）
verify_groups:
  core:
    - "make build"
    - "make test"
  sub-repo:
    - "cd ../sub-repo && make test"
    - "cd ../sub-repo && make lint"

# 舊格式：verify_commands（全部依序，向下相容）
verify_commands:
  - "make build && make test"
```

**解析優先序**：

- `verify_groups` 存在 → 用它
- 只有 `verify_commands` → fallback 為單一 `default` group，全部依序
- 兩者同時存在 → CLI 報錯

**型別變更**（`internal/protocol/types.go`）：

```go
type TestStrategy struct {
    Web          bool                `yaml:"web" json:"web"`
    API          bool                `yaml:"api" json:"api"`
    Gate         bool                `yaml:"gate" json:"gate"`
    CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
    Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
    VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
}
```

### 2. `4x verify` CLI subcommand

新增 `cmd/4x/verify.go`：

```
4x verify <featureId> [--round N] [--timeout 5m]
```

**流程**：

1. 從 `state.json` 讀當前 round（`--round` 可覆寫）
2. 讀 `.4x/{featureId}/test-strategy.yaml`，解析 `verify_groups` 或 fallback `verify_commands`
3. 每個 group 起一個 goroutine，用 `sync.WaitGroup` 管理
4. 組內命令依序執行；任一 exit code ≠ 0 → 該組標記 fail，剩餘 commands 跳過（標記 `skipped`）
5. 某組失敗不中斷其他組——全部跑完再彙總
6. 組裝 `VerifyEvidence`，寫入 `.4x/{featureId}/rounds/round-{N}/verify.json`
7. stdout 印出摘要表格（group / command / exit code / duration）
8. exit code：全 pass → 0，任一 fail → 1

**執行細節**：

- 每個 command 用 `exec.Command("sh", "-c", cmd)` 執行
- 工作目錄（cwd）為 workspace root
- `--timeout` 預設 5 分鐘，用 `context.WithTimeout` 對整體生效

**Package 結構**：邏輯放 `internal/verify/`，CLI 只做參數解析和呼叫。

### 3. verify.json 格式擴展

`VerifyCommand` 新增 `Group` 和 `Skipped` 欄位：

```go
type VerifyCommand struct {
    Command    string    `json:"command"`
    ExitCode   int       `json:"exitCode"`
    DurationMs int64     `json:"durationMs"`
    Summary    string    `json:"summary"`
    StartedAt  time.Time `json:"startedAt"`
    FinishedAt time.Time `json:"finishedAt"`
    Group      string    `json:"group,omitempty"`
    Skipped    bool      `json:"skipped,omitempty"`
}
```

**向下相容**：

- `Group` 和 `Skipped` 都是 `omitempty`——舊格式的 verify.json 不受影響
- 讀取端（`guard/check.go`、`cmd/4x/run.go`）只看 `Passed`，不受新欄位影響

**輸出範例**：

```json
{
  "passed": false,
  "round": 3,
  "role": "tester",
  "commands": [
    {"command": "make build", "exitCode": 0, "durationMs": 2100, "group": "core"},
    {"command": "make test",  "exitCode": 1, "durationMs": 8300, "group": "core"},
    {"command": "cd ../sub && make test", "exitCode": 0, "durationMs": 5200, "group": "sub-repo"},
    {"command": "cd ../sub && make lint", "exitCode": 0, "durationMs": 1100, "group": "sub-repo"}
  ]
}
```

### 4. Tester agent prompt 修改

`templates/tester.md.tmpl` 的 workflow 改為：

```
== Workflow (strict order) ==
1. Read acceptance criteria — list every AC item
2. Run: 4x verify {featureId}
3. Read the generated verify.json for command results
4. For each AC item, collect evidence (command output, verify.json results, etc.)
5. Write test-report.md
6. If verify.json passed is true, write final-report.md and commit-plan.md
```

**關鍵變化**：

- Tester 不再自己寫 verify.json——`4x verify` 已經產生
- Tester 仍然寫 test-report.md、final-report.md、commit-plan.md
- Tester 仍可跑額外手動驗證（看 UI、檢查檔案），但 verify commands 統一由 CLI 執行
- MANDATORY 檔案清單中移除 verify.json，改為「verify.json is created by `4x verify`」

### 5. Designer 模板微調

`templates/designer.md.tmpl` 的 test-strategy.yaml format 區塊加上 `verify_groups` 範例：

```yaml
# 單 repo（維持舊格式）
verify_commands:
  - "make build && make test"

# 多 repo 或需要平行（新格式）
verify_groups:
  core:
    - "make build"
    - "make test"
  other-repo:
    - "cd ../other && make test"
```

### 6. 不改的部分

| 元件 | 原因 |
|---|---|
| `cmd/4x/run.go` | `nextPhaseAfter()` 只讀 verify.json 的 `passed`，不受誰產生影響 |
| `internal/guard/check.go` | `CheckTestingToAccepting()` 檢查 verify.json 存在且合法，不受新欄位影響 |

## 影響範圍

| 元件 | 變更類型 |
|---|---|
| `internal/protocol/types.go` | 修改：`TestStrategy` 加 `VerifyGroups`；`VerifyCommand` 加 `Group`、`Skipped` |
| `internal/verify/` | 新增：讀 yaml → 分組平行執行 → 產 verify.json |
| `cmd/4x/verify.go` | 新增：`4x verify <featureId>` subcommand |
| `templates/tester.md.tmpl` | 修改：改用 `4x verify`，不自己寫 verify.json |
| `templates/designer.md.tmpl` | 修改：加 `verify_groups` 範例格式 |

## 約束

- verify.json 格式向下相容——新欄位全部 `omitempty`
- test-strategy.yaml 向下相容——純 `verify_commands` 仍有效
- CLI 層不呼叫 LLM
- `4x verify` 的 exit code 語意清晰：0 = 全 pass，1 = 有 fail
