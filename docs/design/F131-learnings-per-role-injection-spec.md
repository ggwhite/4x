# F131: 修正 learnings 注入偏誤：改為每角色直接篩選

> Brainstorming 產出的設計規格。實作計畫見 `docs/design/F131-learnings-per-role-injection-plan.md`（`writing-plans` 產出後補上）。

## 背景

`.4x/learnings.json` 是跨 feature 累積的開發教訓庫，每筆 `Entry` 有 `Status`（`candidate` / `active` / `stale` / `promoted`）與 `Category`（`design` / `code-quality` / `testing` / `review` / `tooling` / `process` / `ops`）。`candidate` 代表只在單一 feature 出現過一次的教訓，`active` 代表被 `Store.Harvest()`（`internal/learning/store.go:194-243`）的 fuzzy-match 機制驗證過「在不同 feature 重複出現」的模式。

這次要修的問題來自 kairos 專案實測：`.4x/learnings.json` 累積到 399 筆（243 筆 candidate，約 6 成），但某個 feature（ws-152）round 2 的 Designer 選出的 10 筆 `selected-learnings.json` 全部是 active，一筆 candidate 都沒有。深入追查程式碼後，確認這不是單一 bug，而是三個疊加的架構問題。

## 問題分析

### 問題 1：active/candidate 位置偏誤 + 無上限

`LoadActiveLearnings()`（`internal/prompt/learnings.go:21-39`）回傳「全部」active + candidate 條目，完全沒有數量上限，且組裝順序固定是「先全部 active，再全部 candidate」：

```go
result = append(result, active...)
result = append(result, candidates...)
```

這份完整清單直接灌進 Designer prompt 的 `{{range .Learnings}}` 迴圈（`templates/designer.md.tmpl:196-215`），格式是 `- [ID] [candidate]? (Category) Content` 的純文字清單。實際「挑出最多 10 筆相關的」完全是 Designer（LLM）讀完整份清單後的自由判斷——prompt 指示是「挑真正相關的」，並未提及 status。399 筆全部塞進一次 prompt，加上 active 固定排在最前面，造成 LLM 選擇時明顯的位置偏誤。

**純粹改成「依時間排序」無法解決這個問題**：`Store.UpdateUsage()`（`internal/learning/store.go:350-362`）在每次某筆 entry 被選中使用後，會把它的 `LastUsed` 更新為現在。active 條目只要曾經被選中一次，往後任何依「最近使用/建立時間」的排序都會讓它持續排在前面——這是換一種方式重現同樣的「贏家全拿」動態，並非真正解法。

### 問題 2：跨角色 category 不匹配，下游角色可能被整批濾光

現行架構是「一人選、大家用」：只有 Designer（或 Designer 被跳過時、round 1 的 Coder 頂替）呼叫 `LoadActiveLearnings()` 讀全部清單，選出最多 10 筆寫入 `selected-learnings.json`（`internal/prompt/prompt.go:160-166`）。同一個 feature 後續所有角色（Design-Reviewer、Reviewer、Tester、Fixer、Acceptor，以及非 round-1 的 Coder）都改呼叫 `LoadSelectedLearnings()`（`internal/prompt/learnings.go:41-91`），讀取那份已選清單，再依**自己的** category 過濾一次（`catSet[e.Category]`，第 85-87 行）。

但 `roleCategoryMap`（`internal/learning/store.go:443-452`）定義的各角色 category 彼此幾乎沒有交集：

```go
"designer":        {CategoryDesign, CategoryProcess},
"design-reviewer": {CategoryDesign, CategoryReview},
"coder":           {CategoryDesign, CategoryCodeQuality, CategoryTooling, CategoryOps},
"reviewer":        {CategoryCodeQuality, CategoryReview},
"deep-reviewer":   {CategoryCodeQuality, CategoryReview, CategoryDesign},
"tester":          {CategoryTesting, CategoryTooling, CategoryOps},
"fixer":           {CategoryCodeQuality, CategoryTooling, CategoryOps},
"acceptor":        {CategoryProcess},
```

Designer 判斷「與本次 feature 相關」時天然會偏向自己視角（design/process），若它選出的 10 筆恰好都落在這兩類，Reviewer（`{code-quality, review}`）或 Tester（`{testing, tooling, ops}`）濾完極可能是 0 筆——即使全域池裡明明有相關教訓，只是從未被最初選擇它們的 Designer 挑中。

### 問題 3：兩個角色從未接上 learnings 渲染

`design-reviewer.md.tmpl` 與 `fixer.md.tmpl` 雖然 `roleCategoryMap` 已經定義了它們的 category，但這兩個模板檔完全沒有渲染 `.Learnings`/`.SelectedLearnings` 的段落——這兩個角色現況完全收不到任何過去教訓，屬於「設計時想到、但沒真正接上」的既有缺口。

## 設計

### 核心改動：`LoadLearningsForRole`

在 `internal/prompt/learnings.go` 新增：

```go
func LoadLearningsForRole(dotDir string, role protocol.Role) []learning.Entry
```

行為：

1. 讀取 `learnings.json`（沿用既有 `learning.LoadStore`）。
2. 依 `learning.CategoriesForRole(string(role))` 過濾出該角色關心的 category（不看 status）。
3. 過濾後的條目分兩桶：
   - **active 桶**：`Status == active` 且非 `Ineffective`，依「`LastUsed` 非零則用 `LastUsed`，否則用 `CreatedAt`」新到舊排序，取前 **28** 筆。
   - **candidate 桶**：`Status == candidate`，依 `CreatedAt` 新到舊排序，取前 **12** 筆——這是保底名額，不管 active 桶有多少筆都不受擠壓。
4. 交錯排列兩桶合併成一份有序清單（例如輪流各取一筆，任一桶取完就接續另一桶剩餘部分），確保清單裡不會出現「一整段 active、一整段 candidate」的區塊分布。
5. 回傳合併後的清單（總數上限 40）。

新增具名常數（避免覆用容易誤解的舊常數）：

```go
const (
    ActiveLearningsQuota    = 28
    CandidateLearningsQuota = 12
)
```

`MaxSelectedPerRole`（現有常數，值為 10）之後不再代表「選出幾筆」，因為不再有選擇動作；是否保留、改名或移除，留給實作階段依實際程式碼引用狀況決定。

### 呼叫端統一：`prompt.go`

現行 `internal/prompt/prompt.go`（約 160-166 行）的三分支特判：

```go
if role == protocol.RoleDesigner {
    data.Learnings = LoadActiveLearnings(ws.DotDir())
} else if skippedDesigner && round == 1 && role == protocol.RoleCoder {
    data.Learnings = LoadActiveLearnings(ws.DotDir())
} else {
    data.SelectedLearnings = LoadSelectedLearnings(ws.DotDir(), feature.ID, role)
}
```

改成對所有角色統一呼叫：

```go
data.Learnings = LoadLearningsForRole(ws.DotDir(), role)
```

並在同一呼叫點內，對回傳清單中每個 entry 的 ID 呼叫 `store.UpdateUsage(...)`、存回 `learnings.json`——「被注入某角色的 prompt」直接視為使用一次，不再需要等待「選擇並寫回 `selected-learnings.json`」這道間接手續。這個 usage 更新需要讀-改-寫 `learnings.json`，沿用 `learning.Store` 既有的 `LoadStore`/`Save`（atomic write）機制。

### 模板改動

`templates/{acceptor,coder,deep-reviewer,designer,reviewer,tester}.md.tmpl` 現有的雙軌渲染（Designer 用的「挑選並寫回 ID」格式 vs 其他角色用的「已選清單」格式）統一成單一段落：

```
== Past Learnings ==
以下是過去 feature 累積、與你這個角色相關的經驗教訓，僅供參考，自行判斷是否適用：

{{- range .Learnings}}
- [{{.Category}}] {{.Content}}
{{- end}}
```

不再顯示 ID、不再有「挑選並寫入 selected-learnings.json」的指示文字——因為篩選已經在 Go 端完成，角色只需要讀取、判斷、視需要採納，跟它讀取 Project Includes / Role Instructions 的方式一致。

`templates/design-reviewer.md.tmpl`、`templates/fixer.md.tmpl` 新增相同的 Past Learnings 段落（這兩個角色的 category 已在 `roleCategoryMap` 定義，只是從未接上模板渲染）。

### 移除的機制

- `LoadActiveLearnings()`、`LoadSelectedLearnings()`、`UpdateLearningsUsage()`（`internal/prompt/learnings.go`）
- `protocol.SelectedLearningsFile` 常數在 `internal/orchestrator/worktree.go`（約 35-36、68 行）的引用（worktree resume 時複製檔案清單的一部分，移除不影響正確性）
- `prompt.go` 中僅為了 learnings 選擇而存在的 `skippedDesigner`/round/role 特判分支（`SkippedDesigner` 欄位若仍被其他用途引用則保留，只移除 learnings 相關的判斷邏輯）

## 非目標（約束）

- 不改 `.4x/learnings.json` 的 `Entry` schema，不需要任何資料遷移。
- 既有的 `selected-learnings.json` 檔案直接停止讀取即可，不寫清除/遷移腳本。
- 不改動 `learning.Category` 白名單、`MarkStale`／`MarkIneffective`／`Harvest`（fuzzy match 升級 candidate→active）／`ApplyConsolidation`（AI consolidate）等既有生命週期機制。
- 不引入「依 feature 語意相關性」的 LLM 預先篩選或 embedding／關鍵字比對機制——篩選完全交給角色讀取清單時的自然判斷。

## 測試計畫

- `LoadLearningsForRole`：
  - category 過濾正確性——每個角色只看到自己 `CategoriesForRole` 內的條目。
  - candidate 保底名額不受 active 數量擠壓——即使 active 遠超過 28 筆，candidate 仍能取滿 12 筆（只要池子裡有足夠 candidate）。
  - 交錯排序——回傳清單不應出現連續一大段同一 status。
  - cap 上限——總數超過 40 時確實截斷在 40。
- `prompt.go` 呼叫端：驗證所有角色都走同一條路徑取得 `data.Learnings`，且 `UpdateUsage` 確實對注入清單中的每個 ID 生效（`LastUsed`/`UsedCount` 更新）。
- 模板：`design-reviewer.md.tmpl`、`fixer.md.tmpl` 渲染出 Past Learnings 段落的基本 smoke test。
- 既有引用 `LoadSelectedLearnings`/`SelectedLearningsFile`/`UpdateLearningsUsage` 的舊測試需要一併移除或改寫。

## 驗收標準（供後續 writing-plans / acceptance-criteria 參考）

1. `internal/learning` 或 `internal/prompt` 套件新增 `LoadLearningsForRole`，且有涵蓋上述測試計畫四項的單元測試，`go test ./...` 全數通過。
2. `prompt.go` 不再存在任何三分支特判，所有角色一律透過 `LoadLearningsForRole` 取得 learnings，且注入當下呼叫 `UpdateUsage`。
3. 8 個角色模板（含新增的 design-reviewer、fixer）都渲染統一格式的 Past Learnings 段落，且不再有 ID/candidate 標記或「挑選寫回」指示文字。
4. `LoadActiveLearnings`、`LoadSelectedLearnings`、`UpdateLearningsUsage`、`SelectedLearningsFile` 相關程式碼與引用已從 codebase 移除，`make build && make test && make lint` 全數通過。
