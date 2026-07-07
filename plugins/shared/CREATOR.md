# 4x Feature Creator

建立新的 4x feature — 從使用者的需求產生完整的 feature YAML + design spec。

## 前置條件

- 專案已初始化（`.4x/` 目錄存在）
- `4x` CLI 可用

## 流程

### 判斷入口

檢查是否有現成的 spec/plan：

1. 使用者提供了 feature ID 或剛完成 brainstorming → **路徑 A**
2. 使用者只給了描述或說「建立 feature」→ **路徑 B**

### 路徑 A：有 spec/plan

1. 確認 `docs/design/{id}-spec.md` 存在，讀取內容
2. 如果 `docs/design/{id}-plan.md` 也存在，一併讀取
3. 從 spec 萃取：
   - **name**：從 spec 標題取得 feature 名稱
   - **description**：從「概述」段落取得完整描述
   - **profile**：依 Profile 選擇規則判斷（有 spec/plan 通常用 `standard`，使用者指定時從之）
   - **priority**：從 spec 的優先序或重要性段落取得（未提及時詢問使用者）
   - **repos**：從影響範圍或架構段落取得
   - **subtasks**：從 plan 的 Task 列表轉換，或從 spec 的功能列表拆解（每個含 id + name，視內容複雜度加 description）
   - **rules**：從約束、紅線、不做的事段落取得
4. 執行 `4x new "<name>"` 產生 YAML 骨架（若 spec 檔名的 slug 與自動截斷結果不同，加 `--id <slug>` 保持一致；有關聯 issue 時加 `--issue`）
5. 讀取產生的 `.4x/features/{id}.yaml`
6. 用萃取的內容覆寫 YAML 欄位（保留 `4x new` 產生的 id 和 name）
7. 展示完整 YAML 給使用者確認
8. 使用者確認後寫入

### 路徑 B：無 spec/plan

1. 問答式引導（一次問一個問題）：
   - Q1：這個 feature 要做什麼？（產生 name + description）
   - Q2：用什麼 profile 跑？（參考 Profile 選擇段落，不確定時建議 `standard`）
   - Q3：優先序多高？（產生 priority：0=critical, 1=high, 2=medium, 3=low）
   - Q4：會動到哪些模組或檔案？（產生 repos）
   - Q5：怎樣算做完？列出驗收標準（產生 subtasks，含 id:name，視需要加 description）
   - Q6：有什麼不能做的限制？（產生 rules）
   - Q7（視需要）：有沒有依賴其他 feature？有沒有關聯的 issue？
2. 執行 `4x new "<name>"` 產生 YAML 骨架（加上所有已知 flags：`--profile`、`--priority` 等）
3. 從問答結果填入所有欄位
4. 產生 `docs/design/{id}-spec.md`（從問答結果組織成 spec 格式）
5. 展示 YAML + spec 給使用者確認
6. 使用者確認後寫入

## YAML 填充規則

```yaml
id: F{NNN}-{slug}              # 由 4x new 產生，不可修改
name: "F{NNN}: {display name}" # 由 4x new 產生，不可修改
description: |                  # 至少 2-3 句，說明 what 和 why
  ...
status: not-started             # 固定值
profile: standard               # pipeline profile（見下方說明）
priority: 1                     # 0=critical, 1=high, 2=medium, 3=low（省略表示無優先序）
repos:                          # 如果只有 self，可省略
  - "."
depends:                        # 依賴的 feature ID（省略表示無依賴）
  - "F0XX-other-feature"
subtasks:                       # 2-8 個，每個可獨立驗證
  - id: {kebab-case-slug}
    name: "{具體描述}"
    status: ""                  # 4x new 產生空字串，不是 "not-started"
    description: "{選填，補充 name 不足以表達的細節}"
rules:                          # 具體可檢查的約束
  - "..."
```

### 對應 CLI flags

| YAML 欄位 | `4x new` flag | 格式 |
|---|---|---|
| description | `--desc` | 純文字 |
| profile | `--profile` | profile 名稱（見 Profile 選擇） |
| priority | `--priority` | 數字 0-3 |
| repos | `--repo` | 可重複 |
| depends | `--depends` | feature ID，可重複 |
| subtasks | `--subtask` | `"id:name"` 或 `"id:name:description"`，可重複 |
| rules | `--rule` | 可重複 |
| id (自訂 slug) | `--id` | 完整 slug，跳過自動截斷 |
| issue | `--issue` | `"repo:id-or-url"` 格式，可重複；單 repo 可省略前綴 |

### Profile 選擇

Profile 決定 feature 跑哪些 phase。讀取 `.4x/settings.json` 的 `profiles` 取得可用清單，常見的：

| Profile | 適用場景 |
|---|---|
| `full` | 完整流程含 design + deep-review + fixing |
| `normal` | 含 design 但跳過 deep-review |
| `standard` | 跳過 design，直接 code→review→test→accept（預設） |
| `quick` | 小修，review/accept 用 sonnet 加速 |
| `code-only` | 純寫 code + 快速 accept |

**判斷規則**（依序檢查）：

1. 使用者明確指定 → 用指定的
2. spec/plan 存在 → `standard`（設計已完成，不需再跑 designer）
3. 純 rename / 搬檔 / 機械式重構 → `quick` 或 `code-only`
4. 需要設計探索的新功能 → `normal` 或 `full`
5. 都不確定 → **問使用者**，不要靜默用預設

省略 `--profile` 時 CLI 會套 `settings.json` 的 `default_profile`（目前是 `standard`），但 **建議每次都明確帶 `--profile`**，避免預設值變更時行為改變。

### 填充品質標準

- **profile**：根據上方判斷規則選擇，不確定時問使用者
- **description**：說明 what 和 why，不只重複 name
- **priority**：根據緊急程度設定，問答式引導時主動詢問
- **subtasks**：每個 subtask 的 id 用 kebab-case，name 描述具體可驗證的結果；description 選填，用於補充 name 不足以表達的上下文
- **rules**：寫具體約束（「不能修改 X」「必須通過 Y」），不寫空話

## 不做的事

- 不做設計探索 — 那是 brainstorming 的事
- 不執行 feature — 那是 `4x run` 的事
- 不直接寫 YAML 檔 — 透過 `4x new` CLI 產生骨架
- 路徑 B 不產 plan — 留給 writing-plans 或 designer role
