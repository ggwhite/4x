# F024 — 4x-creator Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立跨 LLM 的 feature scaffold skill，讓使用者用自然語言建立完整的 4x feature YAML + spec

**Architecture:** 通用流程寫在 `plugins/shared/CREATOR.md`，Claude Code 用 `CREATOR-SKILL.md` 封裝觸發，其他 agent（Codex/Gemini/Agy/Copilot/Cursor）在既有指令檔追加 creator 段落。`embed.go` 加入 shared/ 檔案，`deploy.go` 部署到 `.4x/plugins/shared/`。

**Tech Stack:** Go (embed/deploy)、Markdown (skill/instruction files)

---

### Task 1: 建立 `plugins/shared/CREATOR.md` — 通用流程指令

**Files:**
- Create: `plugins/shared/CREATOR.md`

這是核心文件，定義 feature 建立的完整流程邏輯，所有 LLM 共用。

- [ ] **Step 1: 建立 `plugins/shared/` 目錄**

```bash
mkdir -p plugins/shared
```

- [ ] **Step 2: 寫入 CREATOR.md**

```bash
cat > plugins/shared/CREATOR.md
```

內容：

```markdown
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
   - **repos**：從影響範圍或架構段落取得
   - **subtasks**：從 plan 的 Task 列表轉換，或從 spec 的功能列表拆解
   - **rules**：從約束、紅線、不做的事段落取得
4. 執行 `4x new "<name>"` 產生 YAML 骨架
5. 讀取產生的 `.4x/features/{id}.yaml`
6. 用萃取的內容覆寫 YAML 欄位（保留 `4x new` 產生的 id 和 name）
7. 展示完整 YAML 給使用者確認
8. 使用者確認後寫入

### 路徑 B：無 spec/plan

1. 問答式引導（一次問一個問題）：
   - Q1：這個 feature 要做什麼？（產生 name + description）
   - Q2：會動到哪些模組或檔案？（產生 repos）
   - Q3：怎樣算做完？列出驗收標準（產生 subtasks）
   - Q4：有什麼不能做的限制？（產生 rules）
   - Q5（視需要）：有沒有依賴其他 feature？
2. 執行 `4x new "<name>"` 產生 YAML 骨架
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
repos:                          # 如果只有 self，寫 self: "."
  self: "."
subtasks:                       # 2-8 個，每個可獨立驗證
  - id: {kebab-case-slug}
    name: "{具體描述}"
    status: not-started
rules:                          # 具體可檢查的約束
  - "..."
```

### 填充品質標準

- **description**：說明 what 和 why，不只重複 name
- **subtasks**：每個 subtask 的 id 用 kebab-case，name 描述具體可驗證的結果
- **rules**：寫具體約束（「不能修改 X」「必須通過 Y」），不寫空話

## 不做的事

- 不做設計探索 — 那是 brainstorming 的事
- 不執行 feature — 那是 `4x run` 的事
- 不直接寫 YAML 檔 — 透過 `4x new` CLI 產生骨架
- 路徑 B 不產 plan — 留給 writing-plans 或 designer role
```

- [ ] **Step 3: Commit**

```bash
git add plugins/shared/CREATOR.md
git commit -m "feat(F024): add shared CREATOR.md — cross-LLM feature scaffold flow"
```

---

### Task 2: 建立 `plugins/claude-code/CREATOR-SKILL.md` — Claude Code skill 封裝

**Files:**
- Create: `plugins/claude-code/CREATOR-SKILL.md`

Claude Code 的 skill frontmatter + @import 通用流程。

- [ ] **Step 1: 寫入 CREATOR-SKILL.md**

```bash
cat > plugins/claude-code/CREATOR-SKILL.md
```

內容：

```markdown
---
name: 4x-create
description: >
  建立新的 4x feature — 從使用者需求產生完整 feature YAML + spec。
  觸發：「4x create」「建立 feature」「scaffold feature」「新增 feature」。
  也可從 brainstorming 完成後銜接。
  This is a rigid process skill — follow steps exactly.
---

@shared/CREATOR.md
```

- [ ] **Step 2: Commit**

```bash
git add plugins/claude-code/CREATOR-SKILL.md
git commit -m "feat(F024): add Claude Code CREATOR-SKILL.md with trigger words"
```

---

### Task 3: 更新 `plugins/embed.go` — 加入 shared/CREATOR.md 和 CREATOR-SKILL.md

**Files:**
- Modify: `plugins/embed.go:5`

把新檔案加入 `go:embed` 指令。

- [ ] **Step 1: 更新 embed 指令**

修改 `plugins/embed.go` 第 5 行的 `//go:embed` 行，在最後追加 `shared/CREATOR.md claude-code/CREATOR-SKILL.md`：

```go
//go:embed claude-code/SKILL.md claude-code/workflow.js claude-code/CREATOR-SKILL.md codex/AGENTS.md gemini/GEMINI.md agy/AGY.md cursor/.cursorrules copilot/AGENTS.md copilot/workflow.js shared/CREATOR.md
```

- [ ] **Step 2: 驗證編譯通過**

```bash
go build ./cmd/4x && go vet ./...
```

Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add plugins/embed.go
git commit -m "feat(F024): embed shared/CREATOR.md and CREATOR-SKILL.md"
```

---

### Task 4: 更新 `cmd/4x/deploy.go` — 部署 shared/ 和 CREATOR-SKILL.md

**Files:**
- Modify: `cmd/4x/deploy.go:21-51` (runnerDeploys function)
- Modify: `cmd/4x/deploy.go:55-81` (deployPlugins function)

`4x init` 和 `4x upgrade` 時要部署 `shared/CREATOR.md` 到 `.4x/plugins/shared/`，Claude plugin 多部署 `CREATOR-SKILL.md`。

- [ ] **Step 1: 寫 deploy 測試**

在 `cmd/4x/deploy_test.go` 新增測試（如果不存在就建立）：

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestDeployPluginsCreatesSharedDir(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}

	deployPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md not deployed: %v", err)
	}

	creatorSkill := filepath.Join(dotDir, "plugins", "CREATOR-SKILL.md")
	if _, err := os.Stat(creatorSkill); err != nil {
		t.Errorf("CREATOR-SKILL.md not deployed: %v", err)
	}
}

func TestDeployPluginsSharedDeployedForAllRunners(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"gemini": {Command: "gemini"},
		},
	}

	deployPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md should deploy for any runner: %v", err)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

```bash
go test ./cmd/4x/ -run TestDeployPlugins -v
```

Expected: FAIL — `shared/CREATOR.md not deployed`

- [ ] **Step 3: 更新 `runnerDeploys` — Claude 加入 CREATOR-SKILL.md**

在 `cmd/4x/deploy.go` 的 `runnerDeploys` function 中，`case "claude":` 追加一條：

```go
case "claude":
	return []pluginDeploy{
		{EmbedPath: "claude-code/SKILL.md", PluginName: "CLAUDE.md", RootFile: "CLAUDE.md"},
		{EmbedPath: "claude-code/workflow.js", PluginName: "workflow.js"},
		{EmbedPath: "claude-code/CREATOR-SKILL.md", PluginName: "CREATOR-SKILL.md"},
	}
```

- [ ] **Step 4: 更新 `deployPlugins` — 加入 shared/ 部署邏輯**

在 `deployPlugins` function 的 runner 迴圈**之前**，加入 shared 檔案的部署：

```go
func deployPlugins(root string, cfg protocol.Config) {
	pluginDir := filepath.Join(root, ".4x", "plugins")
	os.MkdirAll(pluginDir, 0o755)

	// 部署 shared/ — 所有 runner 共用的指令檔
	sharedDir := filepath.Join(pluginDir, "shared")
	os.MkdirAll(sharedDir, 0o755)
	sharedFiles := []string{"shared/CREATOR.md"}
	for _, sf := range sharedFiles {
		data, err := plugins.FS.ReadFile(sf)
		if err != nil {
			continue
		}
		target := filepath.Join(pluginDir, sf)
		os.WriteFile(target, data, 0o644)
	}

	for name := range cfg.Runners {
		// ... 既有邏輯不變
```

- [ ] **Step 5: 跑測試確認通過**

```bash
go test ./cmd/4x/ -run TestDeployPlugins -v
```

Expected: PASS

- [ ] **Step 6: 跑完整測試**

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/deploy.go cmd/4x/deploy_test.go
git commit -m "feat(F024): deploy shared/CREATOR.md and CREATOR-SKILL.md via 4x init"
```

---

### Task 5: 各 agent plugin 追加 creator 段落

**Files:**
- Modify: `plugins/codex/AGENTS.md`
- Modify: `plugins/gemini/GEMINI.md`
- Modify: `plugins/agy/AGY.md`
- Modify: `plugins/copilot/AGENTS.md`
- Modify: `plugins/cursor/.cursorrules`

在每個 agent 的指令檔末尾追加 creator 觸發段落。所有 agent 內容相同（因為沒有 skill 機制，直接內嵌流程參照）。

- [ ] **Step 1: 在每個 agent plugin 末尾追加以下段落**

追加到 `plugins/codex/AGENTS.md`、`plugins/gemini/GEMINI.md`、`plugins/agy/AGY.md`、`plugins/copilot/AGENTS.md`、`plugins/cursor/.cursorrules` 的末尾：

```markdown

## Feature Creator

當使用者說「4x create」「建立 feature」「scaffold feature」「新增 feature」時，讀取 `.4x/plugins/shared/CREATOR.md` 並依照其中的流程建立新 feature。

流程概要：
1. 判斷是否有現成 spec/plan（docs/design/ 下）
2. 有 → 從 spec/plan 萃取欄位，呼叫 `4x new` 產生 YAML
3. 沒有 → 問答式引導，收集需求後呼叫 `4x new` 產生 YAML + spec
4. 展示結果給使用者確認後寫入
```

- [ ] **Step 2: 驗證編譯通過**

```bash
go build ./cmd/4x && go vet ./...
```

Expected: 無錯誤（embed 路徑沒變，只是檔案內容變了）

- [ ] **Step 3: Commit**

```bash
git add plugins/codex/AGENTS.md plugins/gemini/GEMINI.md plugins/agy/AGY.md plugins/copilot/AGENTS.md plugins/cursor/.cursorrules
git commit -m "feat(F024): add Feature Creator section to all agent plugins"
```

---

### Task 6: 更新 feature YAML 狀態

**Files:**
- Modify: `.4x/features/F024-4x-creator-skill.yaml`

- [ ] **Step 1: 把 feature 和所有 subtask 標記為 done**

```yaml
status: done
```

所有 subtasks 也改為 `status: done`。

- [ ] **Step 2: Commit**

```bash
git add .4x/features/F024-4x-creator-skill.yaml
git commit -m "chore(F024): mark feature as done"
```

---

### 驗證清單

所有 Task 完成後執行：

```bash
# 編譯 + 測試
go build ./cmd/4x && go vet ./... && go test ./...

# 驗證 embed 包含新檔案
strings bin/4x | grep "Feature Creator"

# 驗證 4x init 部署
cd /tmp && mkdir test-f024 && cd test-f024 && git init
/path/to/bin/4x init
ls -la .4x/plugins/shared/CREATOR.md
ls -la .4x/plugins/CREATOR-SKILL.md
cat .4x/plugins/shared/CREATOR.md | head -5
```
