# F024 — 4x-creator Skill

## 概述

4x-creator 是一個 feature scaffold skill，負責把使用者的需求轉化成完整的、可被 `4x run` 消費的 feature YAML + design spec。填補「構想」到「執行」之間的空隙。

類似 harness-creator 之於 HARNESS.md，4x-creator 之於 `.4x/` protocol。

## 觸發方式

兩個入口：

| 入口 | 觸發時機 | 說明 |
|---|---|---|
| Brainstorming 銜接 | brainstorming 寫完 spec/plan 後 | 提示使用者可轉入 4x-creator，自動讀取剛產出的 spec/plan |
| 獨立觸發 | 使用者手動觸發 | 觸發詞：「4x create」「建立 feature」「scaffold feature」「新增 feature」 |

## 架構

### 檔案配置

```
plugins/
  shared/
    CREATOR.md             ← 通用流程指令（核心邏輯，所有 LLM 共用）
  claude-code/
    CREATOR-SKILL.md       ← Claude Code skill 封裝（frontmatter + @import shared/CREATOR.md）
  codex/
    AGENTS.md              ← 追加 creator 觸發說明段落
  gemini/
    GEMINI.md              ← 追加 creator 觸發說明段落
  agy/
    AGY.md                 ← 追加 creator 觸發說明段落
  copilot/
    AGENTS.md              ← 追加 creator 觸發說明段落
  cursor/
    .cursorrules           ← 追加 creator 觸發說明段落
  embed.go                 ← 加入 shared/CREATOR.md
```

### 設計原則

- **通用核心 + agent 封裝**：流程邏輯寫在 `shared/CREATOR.md`，各 agent plugin 只做觸發詞映射和 @import
- **透過 CLI 建立**：不直接寫 YAML，呼叫 `4x new` 確保 ID 生成與格式正確
- **不取代 brainstorming**：設計探索仍在 brainstorming 完成，4x-creator 只負責 scaffold
- **完整填充**：產出的 YAML 所有欄位都有內容，可直接 `4x run`

## 流程

### 路徑 A：有 spec/plan（brainstorming 銜接）

```
brainstorming 完成 spec/plan
  ↓
提示：「要建立 4x feature 嗎？」
  ↓ 使用者同意
讀取 docs/design/{id}-spec.md + {id}-plan.md
  ↓
呼叫 4x new "<name>" 產生 YAML 骨架
  ↓
從 spec/plan 萃取 → 填入 YAML：
  - description（從 spec 概述段）
  - subtasks（從 plan 的步驟拆解）
  - rules（從 spec 的約束/紅線）
  - repos（從 spec 的影響範圍）
  ↓
展示填充後的 YAML → 使用者確認 → 寫入
```

### 路徑 B：無 spec/plan（獨立觸發）

```
使用者說「4x create」或「建立 feature」
  ↓
問答式引導（3-5 個問題）：
  1. 這個 feature 要做什麼？（名稱 + 一句話描述）
  2. 會動到哪些模組/檔案？（repos 範圍）
  3. 怎樣算做完？（驗收標準 → subtasks）
  4. 有什麼不能做的？（紅線 → rules）
  5.（視需要）有沒有依賴其他 feature？
  ↓
呼叫 4x new "<name>" 產生 YAML 骨架
  ↓
從問答結果填入 YAML（同路徑 A）
  ↓
產生 docs/design/{id}-spec.md（從問答結果組織）
  ↓
展示 YAML + spec → 使用者確認 → 寫入
```

## 產出

| 檔案 | 說明 | 路徑 A | 路徑 B |
|---|---|---|---|
| `.4x/features/{id}.yaml` | Feature 定義（完整填充） | ✅ | ✅ |
| `docs/design/{id}-spec.md` | 設計規格 | 已存在（brainstorming 產的） | ✅ 從問答產生 |
| `docs/design/{id}-plan.md` | 實作計畫 | 已存在（brainstorming 產的） | ❌ 不產，走 writing-plans |

## YAML 填充規則

```yaml
id: F{NNN}-{slug}              # 由 4x new 產生
name: "F{NNN}: {display name}" # 由 4x new 產生
description: |                  # 從 spec 概述或問答結果
  多行描述...
status: not-started             # 固定
repos:                          # 從 spec 影響範圍或問答
  self: "."
subtasks:                       # 從 plan 步驟或問答驗收標準
  - id: {slug}
    name: "{描述}"
    status: not-started
rules:                          # 從 spec 約束或問答紅線
  - "..."
```

### 填充品質標準

- **description**：至少 2-3 句，說明 what 和 why，不只是重複 name
- **subtasks**：每個 subtask 可獨立驗證、粒度適中（2-8 個）、id 用 kebab-case
- **rules**：具體可檢查的約束（「不能修改 X」「必須通過 Y」），不寫空話
- **repos**：如果只有 self，寫 `self: "."`；多 repo 列出相對路徑

## 不做的事

- 不取代 brainstorming — 設計探索在那邊完成
- 不取代 `4x run` — 只建立 feature，不執行 workflow
- 不直接寫 YAML — 透過 `4x new` CLI 確保 ID 生成正確
- 不產 plan（路徑 B）— 留給 writing-plans skill 或 designer role

## 跨 agent 支援

### Claude Code

`CREATOR-SKILL.md` 作為 rigid skill，frontmatter 定義觸發詞，內容 @import `shared/CREATOR.md`。

### 其他 agent（Codex / Gemini / Agy / Copilot / Cursor）

在各自的指令檔追加 creator 段落，說明：
- 觸發詞：「4x create」「建立 feature」
- 流程：參照 `shared/CREATOR.md`（4x init 部署時會把 shared/ 也放到 .4x/plugins/）
- 因為沒有 skill 機制，直接在指令檔中內嵌流程或 @import
