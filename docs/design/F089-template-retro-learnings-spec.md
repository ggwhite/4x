# F089 — Project-Level Template Override + Retro Learnings Loop

## 概述

兩個相關能力合為一個 feature：(1) 專案級 template 覆寫，讓每個專案可根據自身經驗迭代 prompt 品質；(2) retro learnings 迴路，讓開發過程累積的教訓自動回饋到後續 feature。

迭代迴路：跑 feature → Acceptor 產 retro learnings → 累積到 learnings.json → 下個 feature Designer 選相關 learnings → 注入到各 role prompt → prompt 品質提升。

## 設計決策

| 決策 | 選擇 | 理由 |
|------|------|------|
| Template override 粒度 | 整檔替換 | 簡單可預期，使用者用 dump-templates 複製出來再改 |
| Learnings 儲存 | 單一 `.4x/learnings.json` | 與 CLI 全權管理哲學一致，git diff 清楚 |
| Learnings 選擇機制 | Prompt-driven（Designer LLM 選） | 簡單、不需額外 CLI 邏輯，LLM 理解語意較準 |
| Category 分類 | 固定列舉 | 方便 CLI 過濾與 role 對應 |
| Learnings 注入層 | CLI prompt 層 | 各 role template 加段落，不依賴 runner |
| 失敗處理 | warn 不阻擋 | learnings 是 nice-to-have，不影響 state transition |

## Part 1：Template Override

### 機制

`prompt.go` 的 `loadRoleTemplate()` 目前直接讀 `templates.FS`（go:embed）。改為兩階段查找：

1. 先查 `.4x/templates/{filename}`（專案目錄）
2. 沒有 → fallback `templates.FS`（go:embed，現行行為）

`locale.tmpl` 也走同樣邏輯。

### 影響範圍

- 只改 `prompt.go` 的 `loadRoleTemplate()`
- 不影響 state machine、guardrail、profiles——純粹改 prompt 產生來源

### `4x init --dump-templates`

```bash
4x init --dump-templates          # 倒出所有內建 *.md.tmpl 到 .4x/templates/
4x init --dump-templates --force  # 覆蓋已存在的檔案
```

行為：

- 建立 `.4x/templates/` 目錄
- 將 `templates.FS` 中所有 `*.md.tmpl` 寫入（含 `locale.tmpl`）
- 已存在的檔案 → 跳過 + warn（除非 `--force`）
- 不倒 `profiles/` 目錄（test profile 有自己的覆寫機制 `TestProfileOverride`）

## Part 2：Retro Learnings

### Schema

檔案位置：`.4x/learnings.json`

```json
{
  "version": 1,
  "entries": [
    {
      "id": "L001",
      "source_feature": "F042-auth-refactor",
      "category": "code-quality",
      "content": "Go error wrapping 應統一用 fmt.Errorf %w，不要混用 errors.New",
      "created_at": "2026-06-20T10:30:00+08:00",
      "last_used": "2026-06-22T14:00:00+08:00",
      "used_count": 3,
      "status": "active"
    }
  ]
}
```

### 固定 Category 列舉

| Category | 說明 |
|---|---|
| `design` | 設計階段的教訓（spec 不清、拆分不當） |
| `code-quality` | 程式碼品質（命名、error handling、pattern） |
| `testing` | 測試策略、驗證方式 |
| `review` | Review 流程發現的問題 |
| `tooling` | 工具鏈、build、CI 相關 |
| `process` | 流程面（role 溝通、escalation） |

### ID 格式

`L` + 三位數自動遞增（L001, L002…）。CLI 自動分配，不讓 Acceptor 決定。

### 狀態列舉

| Status | 說明 |
|---|---|
| `active` | 可被選用 |
| `stale` | 超過 90 天未使用，CLI 在讀取時自動標記 |
| `promoted` | 已手動升級到 template/instructions，保留記錄但不再注入 |

### 數量控管

預設 100 條 active 上限。超過時 CLI warn，讓使用者 prune。不自動刪除。

### Acceptor 產出

Acceptor 在寫 `final-report.md` 的同時，額外產出 `.4x/{feature-id}/retro-learnings.json`：

```json
{
  "learnings": [
    {
      "category": "code-quality",
      "content": "具體、可操作的一句話教訓"
    }
  ]
}
```

Prompt 指示原則：
- 只寫「未來能改善什麼」，不寫「這次做了什麼」
- 每條要具體到可直接當作 instruction 使用
- 不超過 5 條——只留最有價值的
- 如果這次開發沒什麼特別教訓，寫空 array

### CLI 收割流程

在 `4x run` 的 accepting phase 結束後：

1. 讀取 `.4x/{feature-id}/retro-learnings.json`
2. 驗證 schema（category 是否在白名單、content 非空）
3. 自動分配 ID（L-序號遞增）
4. 去重：比對 content 完全相同就跳過（不做模糊比對）
5. 追加到 `.4x/learnings.json`
6. 更新 stale 狀態（順便掃一遍 `last_used`）
7. 讀取失敗或格式錯 → warn log，不影響 state transition

### Designer 選 Learnings

Designer prompt template 增加段落，注入 `learnings.json` 中所有 `active` 條目。Designer LLM 從中挑出與當前 feature 相關的 ID，寫入 `.4x/{feature-id}/selected-learnings.json`：

```json
{
  "selected": ["L001", "L003", "L012"]
}
```

不超過 10 條。不相關就寫空 array。

### 後續 Role 注入

CLI 在產 prompt 時讀 `selected-learnings.json`，反查 `learnings.json` 拿完整內容，按 category 篩出與本 role 相關的條目注入 `promptData.SelectedLearnings`。

**Category 與 Role 對應**：

| Role | 注入的 categories |
|---|---|
| Coder | design, code-quality, tooling |
| Reviewer | code-quality, review |
| Deep-Reviewer | code-quality, review, design |
| Tester | testing, tooling |
| Acceptor | process |

各 role template 增加段落：

```
{{- if .SelectedLearnings}}

== Learnings from Past Features ==
以下是從過去經驗中挑出與本次工作相關的教訓，請納入考量：
{{range .SelectedLearnings}}
- [{{.Category}}] {{.Content}}
{{- end}}
{{- end}}
```

### used_count 更新

CLI 在 `4x run` 進入第一個非 Designer phase 時，讀 `selected-learnings.json` 並一次更新所有被選中 learning 的 `last_used` 和 `used_count`。

### Token 上限

Designer prompt 指示不超過 10 條。CLI 端也做 hard cap 10 條截斷。

## CLI 子命令：`4x learn`

```
4x learn list                     # 列出所有 learnings（id/category/status/used_count）
4x learn list --category=testing  # 依 category 篩
4x learn prune                    # 列出 stale 條目並全部移除（加 --dry-run 預覽）
4x learn promote <id>             # 標記為 promoted
4x learn remove <id>              # 直接移除
```

## 不做的事

- 不做 partial template override（整檔替換）
- 不自動 prune learnings（只 warn，讓人決定）
- 不做模糊去重（只比 content 完全相同）
- learnings 失敗不阻擋 state transition
- 不做 runner 層注入（全部在 CLI prompt 層處理）

## 完整運作流程

```
Feature N 結束
  └─ Acceptor 寫 final-report.md + retro-learnings.json
  └─ CLI 收割 retro-learnings.json → 追加到 learnings.json
       └─ 驗證 schema、分配 ID、去重、更新 stale

Feature N+1 開始
  └─ 4x run → designing phase
       └─ CLI 產 prompt：
            1. loadRoleTemplate → 先查 .4x/templates/，fallback go:embed
            2. 讀 learnings.json（active 條目）→ 注入 promptData.Learnings
       └─ Designer 產出 task-brief + selected-learnings.json
  └─ coding phase
       └─ CLI 產 prompt：
            1. template override（同上）
            2. 讀 selected-learnings.json → 按 category 篩 → 注入 promptData.SelectedLearnings
            3. 更新 used_count/last_used（第一個非 Designer phase 做一次）
  └─ 後續 role 同理
```
