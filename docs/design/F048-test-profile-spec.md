# F048: Test Profile Semantics — Designer 標 profile，Tester 自動注入測試方法論

## 現狀

`test-strategy.yaml` 的 `web`/`api`/`coder_only` flag 只是標記，Tester 的測試方法論全靠 `settings.json` 的 `roles.tester.instructions` 手寫 8 條指示。不管 feature 是什麼類型，Tester 永遠拿到同一份指示。Designer 標了 `web: true` 但 Tester 如何做 web 測試得自行從 instructions 中推導。

## 目標

- Designer 在 `test-strategy.yaml` 標記 `profiles`（如 `[unit, web]`）
- Tester prompt 根據 profiles 自動注入對應的測試方法論
- 內建 4 個預設 profile（unit/web/api/e2e），可被 `settings.json` 覆寫或擴充
- 現有 tester instructions 中跟特定測試類型相關的內容遷入 profile，instructions 只留通用規則

## 設計

### 1. test-strategy.yaml 格式擴展

新增 `profiles` key：

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

**型別變更**（`internal/protocol/types.go`）：

```go
type TestStrategy struct {
    Web          bool                `yaml:"web" json:"web"`
    API          bool                `yaml:"api" json:"api"`
    Gate         bool                `yaml:"gate" json:"gate"`
    CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
    Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
    VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
    Profiles     []string            `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}
```

**向下相容**：`profiles` 是 `omitempty`。沒有 `profiles` 的舊 test-strategy.yaml 不受影響。

### 2. 內建 profile 檔案

四個內建 profile 放在 `templates/profiles/`，用 go:embed 嵌入 binary：

```
templates/profiles/
  unit.md
  web.md
  api.md
  e2e.md
```

**embed 擴展**（`templates/embed.go`）：

```go
//go:embed *.tmpl
var FS embed.FS

//go:embed profiles/*.md
var ProfilesFS embed.FS
```

**各 profile 內容**：

**`unit.md`** — 單元測試方法論：
- 用 `go test`，`t.TempDir()` 隔離 workspace
- 不污染實際 `.4x/` 目錄
- verify.json 每項 AC 都要有對應 pass/fail

**`web.md`** — 瀏覽器 UI 測試方法論：
- 用 Playwright 測試
- 先啟動 `4x live`（背景執行）
- 寫 `.ts` 腳本存 `.4x/e2e/{feature-id}/`
- 截圖存 `.4x/e2e/{feature-id}/screenshot/`，檔名 `{step}-{description}.png`
- Playwright 未安裝先跑 `npx playwright install chromium`

**`api.md`** — HTTP API 測試方法論：
- 用 `httptest` 或 `curl` 驗證
- 檢查 status code、response body、error case
- 測試 endpoint 的 happy path + edge case

**`e2e.md`** — 端對端整合測試方法論：
- 多服務協作場景
- 驗證資料流從輸入到最終狀態的完整路徑
- 資料庫狀態驗證、跨服務一致性

### 3. settings.json 覆寫機制

`Config` 新增 `TestProfiles` 欄位：

```go
type Config struct {
    // ... 既有欄位 ...
    TestProfiles map[string]TestProfileOverride `json:"test_profiles,omitempty"`
}

type TestProfileOverride struct {
    Content string `json:"content,omitempty"`
    Include string `json:"include,omitempty"`
}
```

使用方式：

```json
{
  "test_profiles": {
    "web": {
      "content": "用 Cypress 而非 Playwright 測試..."
    },
    "lua": {
      "include": "docs/test-profiles/lua.md"
    }
  }
}
```

**合併邏輯**：

1. 讀 `test-strategy.yaml` 的 `profiles` 陣列
2. 對每個 profile name：先查 `settings.json` 的 `test_profiles[name]`
   - 有 `content` → 直接用
   - 有 `include` → 讀檔案內容
   - 都沒有 → fallback 到內建 `templates/profiles/{name}.md`
3. 內建也找不到 → 報 warning（不阻斷）
4. 覆寫是整個取代，不做 merge

### 4. prompt 組裝與 Tester template 修改

**`promptData` 擴展**：

```go
type promptData struct {
    // ... 既有欄位 ...
    ProfileInstructions []profileContent
}

type profileContent struct {
    Name    string
    Content string
}
```

**`prompt.go` 新增 `loadProfiles` 函式**：

```
loadProfiles(ws, featureID, cfg) → []profileContent
  1. 讀 .4x/run/{featureId}/test-strategy.yaml
  2. 若 profiles 為空 → 回傳 nil
  3. 對每個 profile name → 按合併邏輯載入內容
  4. 回傳 []profileContent
```

在 `generatePrompt()` 和 `newPromptCmd()` 組裝 `promptData` 時呼叫 `loadProfiles`。

**`tester.md.tmpl` 變更**——在 Inputs 後、Workflow 前加 profile 注入區塊：

```
{{- if .ProfileInstructions}}
{{range .ProfileInstructions}}

== Test Profile: {{.Name}} ==
{{.Content}}
{{- end}}
{{- end}}
```

### 5. 現有 tester instructions 遷移

`settings.json` 的 `roles.tester.instructions` 8 條中：

| 條目 | 遷移目標 |
|---|---|
| 1. 根據 test-strategy 決定測試方式 | 移除（profile 機制取代） |
| 2. Go 測試：go test, t.TempDir() | 遷入 `unit.md` |
| 3. Web UI 測試：用 Playwright | 遷入 `web.md` |
| 4. Web UI 測試流程 | 遷入 `web.md` |
| 5. 腳本存路徑 | 遷入 `web.md` |
| 6. 截圖存路徑 | 遷入 `web.md` |
| 7. Playwright 安裝 | 遷入 `web.md` |
| 8. verify.json 每項 AC pass/fail | 保留（通用規則） |

遷移後 tester instructions 只剩通用規則。

### 6. Designer template 微調

`templates/designer.md.tmpl` 的 `test-strategy.yaml format` 區塊加上 profiles：

```yaml
profiles:
  - unit
verify_commands:
  - "command here"
```

附上 profile 選擇指引：

```
Available profiles: unit, web, api, e2e.
Profiles inject test methodology into Tester prompt automatically.
Examples:
  - CLI/library feature → profiles: [unit]
  - Dashboard UI feature → profiles: [unit, web]
  - REST API feature → profiles: [unit, api]
  - Multi-service feature → profiles: [unit, e2e]
```

## 影響範圍

| 元件 | 變更類型 |
|---|---|
| `internal/protocol/types.go` | 修改：`TestStrategy` 加 `Profiles`；`Config` 加 `TestProfiles`；新增 `TestProfileOverride` |
| `templates/profiles/*.md` | 新增：4 個內建 profile |
| `templates/embed.go` | 修改：加 `ProfilesFS` embed |
| `cmd/4x/prompt.go` | 修改：新增 `loadProfiles`、`profileContent`；`promptData` 加 `ProfileInstructions` |
| `templates/tester.md.tmpl` | 修改：加 profile 注入區塊 |
| `templates/designer.md.tmpl` | 修改：加 profiles 說明 |
| `.4x/settings.json` | 修改：tester instructions 遷移 |

## 不改的部分

| 元件 | 原因 |
|---|---|
| `cmd/4x/run.go` | profile 純粹是 prompt 內容注入，不影響狀態機 |
| `internal/guard/check.go` | guardrail 不涉及 profile |
| Designer output schema | 約束：不改 Designer 的 output schema |

## 約束

- test-strategy.yaml 向下相容——無 `profiles` 時行為不變
- settings.json 向下相容——無 `test_profiles` 時用內建
- 不改 Designer 的 output schema，只擴展 Tester 的 prompt 注入
- 覆寫是整個取代，不做 partial merge
