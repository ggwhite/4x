# F037: Model Tier Abstraction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用抽象 tier（opus/sonnet/haiku）取代 vendor-specific model name，讓任何 runner 都能正確解析 role 指定的 model。

**Architecture:** 頂層 `model_tiers` 集中定義 tier → runner model mapping，runner 可用 `tiers` 個別覆寫。`ResolveModel()` 從 `cmd/4x/run.go` 提取到 `internal/protocol/model.go`，純資料轉換函式便於單元測試。

**Tech Stack:** Go 1.22+, Go standard testing

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/protocol/types.go:200-235` | Modify | Config 加 `ModelTiers`；RunnerConfig 移除 `ModelMap`，加 `Tiers` |
| `internal/protocol/model.go` | Create | `ResolveModel()` 函式 |
| `internal/protocol/model_test.go` | Create | ResolveModel 單元測試 |
| `cmd/4x/run.go:275-286` | Modify | 刪除舊 `resolveModel()`，改呼叫 `protocol.ResolveModel()` |
| `.4x/settings.json` | Modify | 加 `model_tiers`，移除所有 `model_map` |
| `.4x/plugins/CLAUDE.md` | Modify | Step 3 mapping table 更新 |

---

### Task 1: Config 型別更新

**Files:**
- Modify: `internal/protocol/types.go:200-235`

- [ ] **Step 1: Config 加 ModelTiers 欄位**

在 `internal/protocol/types.go` 的 `Config` struct（line 200）加 `ModelTiers`：

```go
// Config 是 .4x/settings.json 的專案設定
type Config struct {
	Project           ProjectConfig            `json:"project"`
	Runners           map[string]RunnerConfig  `json:"runners"`
	Default           string                   `json:"default_runner"`
	Roles             map[string]RoleConfig    `json:"roles,omitempty"`
	Rules             []string                 `json:"rules,omitempty"`
	HubRepos          []string                 `json:"hub_repos,omitempty"`
	Isolation         string                   `json:"isolation,omitempty"`
	MaxConcurrentRuns int                      `json:"max_concurrent_runs,omitempty"`
	Commit            string                   `json:"commit,omitempty"`
	ModelTiers        map[string]map[string]string `json:"model_tiers,omitempty"`
}
```

- [ ] **Step 2: RunnerConfig 移除 ModelMap，加 Tiers**

將 `RunnerConfig`（line 226）改為：

```go
// RunnerConfig 是 LLM runner 的設定
type RunnerConfig struct {
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Model        string            `json:"model,omitempty"`
	Tiers        map[string]string `json:"tiers,omitempty"`
	Stdin        bool              `json:"stdin,omitempty"`
	Tty          bool              `json:"tty,omitempty"`
	Quiet        bool              `json:"quiet,omitempty"`
	OutputFormat string            `json:"output_format,omitempty"`
}
```

注意：`Model` 保留（作為預設 tier），`ModelMap` 移除，新增 `Tiers`。`OutputFormat` 是 F033 加的，保留。

- [ ] **Step 3: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 編譯失敗——`cmd/4x/run.go:282` 仍引用 `runnerCfg.ModelMap`

這是預期的，Task 2 會修。先確認只有這一處 break。

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F037): add ModelTiers to Config, replace ModelMap with Tiers in RunnerConfig"
```

---

### Task 2: ResolveModel 函式

**Files:**
- Create: `internal/protocol/model.go`
- Create: `internal/protocol/model_test.go`

- [ ] **Step 1: 寫 model_test.go — role 有 model，用 role 的 tier**

```go
package protocol

import "testing"

func TestResolveModel_RoleTier(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus", "gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro")
	}
}
```

- [ ] **Step 2: 跑測試確認 FAIL**

Run: `go test ./internal/protocol/ -run TestResolveModel_RoleTier -v`
Expected: FAIL — `ResolveModel` 未定義

- [ ] **Step 3: 寫 model.go 實作**

```go
package protocol

import "fmt"

const defaultTier = "sonnet"

// ResolveModel 根據 tier 抽象層解析出 runner 認識的 model name。
// 優先序：runner.Tiers[tier] > model_tiers[tier][runner] > error。
func ResolveModel(cfg Config, runnerName string, role Role) (string, error) {
	runnerCfg := cfg.Runners[runnerName]

	tier := ""
	if rc, ok := cfg.Roles[string(role)]; ok {
		tier = rc.Model
	}
	if tier == "" {
		tier = runnerCfg.Model
	}
	if tier == "" {
		tier = defaultTier
	}

	if model, ok := runnerCfg.Tiers[tier]; ok {
		return model, nil
	}
	if tierMap, ok := cfg.ModelTiers[tier]; ok {
		if model, ok := tierMap[runnerName]; ok {
			return model, nil
		}
	}

	return "", fmt.Errorf("runner %q has no model for tier %q", runnerName, tier)
}

// ResolveDeepModel 解析 deep_model 的 tier，用於 reviewer 等需要雙模型的角色。
// 若 role 未設 deep_model，回傳空字串（表示不需要 deep model）。
func ResolveDeepModel(cfg Config, runnerName string, role Role) (string, error) {
	rc, ok := cfg.Roles[string(role)]
	if !ok || rc.DeepModel == "" {
		return "", nil
	}

	tier := rc.DeepModel
	runnerCfg := cfg.Runners[runnerName]

	if model, ok := runnerCfg.Tiers[tier]; ok {
		return model, nil
	}
	if tierMap, ok := cfg.ModelTiers[tier]; ok {
		if model, ok := tierMap[runnerName]; ok {
			return model, nil
		}
	}

	return "", fmt.Errorf("runner %q has no model for deep tier %q", runnerName, tier)
}
```

- [ ] **Step 4: 跑測試確認 PASS**

Run: `go test ./internal/protocol/ -run TestResolveModel_RoleTier -v`
Expected: PASS

- [ ] **Step 5: 寫測試 — role 沒 model，fallback 到 runner 預設 tier**

```go
func TestResolveModel_FallbackRunnerDefault(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Model: "opus"},
		},
		Roles: map[string]RoleConfig{
			"coder": {},
		},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}
```

- [ ] **Step 6: 跑測試確認 PASS**

Run: `go test ./internal/protocol/ -run TestResolveModel_FallbackRunnerDefault -v`
Expected: PASS

- [ ] **Step 7: 寫測試 — 都沒設，fallback 到 "sonnet"**

```go
func TestResolveModel_FallbackSonnet(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"sonnet": {"claude": "sonnet"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want %q", got, "sonnet")
	}
}
```

- [ ] **Step 8: 跑測試確認 PASS**

Run: `go test ./internal/protocol/ -run TestResolveModel_FallbackSonnet -v`
Expected: PASS

- [ ] **Step 9: 寫測試 — runner tiers 覆寫優先於 model_tiers**

```go
func TestResolveModel_RunnerTiersOverride(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-2.5-pro-preview"}},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro-preview" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro-preview")
	}
}
```

- [ ] **Step 10: 跑測試確認 PASS**

Run: `go test ./internal/protocol/ -run TestResolveModel_RunnerTiersOverride -v`
Expected: PASS

- [ ] **Step 11: 寫測試 — tier 找不到回傳 error**

```go
func TestResolveModel_NotFound(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	_, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err == nil {
		t.Error("expected error when tier mapping not found")
	}
}
```

- [ ] **Step 12: 跑測試確認 PASS**

Run: `go test ./internal/protocol/ -run TestResolveModel_NotFound -v`
Expected: PASS

- [ ] **Step 13: 寫測試 — ResolveDeepModel**

```go
func TestResolveDeepModel_Found(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"reviewer": {Model: "sonnet", DeepModel: "opus"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}

func TestResolveDeepModel_NoDeepModel(t *testing.T) {
	cfg := Config{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

- [ ] **Step 14: 跑全部 model 測試**

Run: `go test ./internal/protocol/ -run TestResolve -v`
Expected: 全部 PASS

- [ ] **Step 15: Commit**

```bash
git add internal/protocol/model.go internal/protocol/model_test.go
git commit -m "feat(F037): add ResolveModel and ResolveDeepModel with tier-based resolution"
```

---

### Task 3: run.go 改用 protocol.ResolveModel

**Files:**
- Modify: `cmd/4x/run.go:275-286` (刪舊函式)
- Modify: `cmd/4x/run.go:338` (呼叫處)

- [ ] **Step 1: 刪除舊的 resolveModel 函式**

刪除 `cmd/4x/run.go` 中的：

```go
// resolveModel 根據 role → runner config 優先序決定要傳給 CLI 的 model，
// 再透過 runner 的 model_map 翻譯成該 runner 認識的名稱
func resolveModel(cfg protocol.Config, runnerCfg protocol.RunnerConfig, role protocol.Role) string {
	model := runnerCfg.Model
	if rc, ok := cfg.Roles[string(role)]; ok && rc.Model != "" {
		model = rc.Model
	}
	if mapped, ok := runnerCfg.ModelMap[model]; ok {
		return mapped
	}
	return model
}
```

- [ ] **Step 2: 更新呼叫處（line 338）**

把 `cmd/4x/run.go` 中的：

```go
		model := resolveModel(cfg, cfg.Runners[s.Runner], role)
```

改為：

```go
		model, err := protocol.ResolveModel(cfg, s.Runner, role)
		if err != nil {
			s.Active = false
			s.StopReason = "model-error"
			ws.WriteState(featureID, s)
			return fmt.Errorf("model resolution failed: %w", err)
		}
```

- [ ] **Step 3: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 4: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/run.go
git commit -m "feat(F037): replace resolveModel with protocol.ResolveModel in run.go"
```

---

### Task 4: 更新 settings.json

**Files:**
- Modify: `.4x/settings.json`

- [ ] **Step 1: 加 model_tiers，移除所有 model_map，更新 runners**

將 `.4x/settings.json` 整體更新。在 `"max_concurrent_runs"` 後面加 `"model_tiers"`，移除所有 runner 的 `"model_map"`：

在頂層（`"max_concurrent_runs": 1` 之後）加：

```json
  "model_tiers": {
    "opus": {
      "claude": "opus",
      "gemini": "gemini-2.5-pro",
      "codex": "gpt-5.5",
      "agy": "opus",
      "copilot": "claude-opus-4-6",
      "cursor": ""
    },
    "sonnet": {
      "claude": "sonnet",
      "gemini": "gemini-2.5-flash",
      "codex": "gpt-5.5",
      "agy": "sonnet",
      "copilot": "claude-sonnet-4-6",
      "cursor": ""
    },
    "haiku": {
      "claude": "haiku",
      "gemini": "gemini-2.5-flash",
      "codex": "gpt-5.5",
      "agy": "haiku",
      "copilot": "claude-haiku-4-5",
      "cursor": ""
    }
  },
```

移除 `codex` runner 的 `"model_map"` 區塊：

```json
    "codex": {
      "command": "codex",
      "args": [
        "exec"
      ],
      "stdin": true,
      "quiet": true
    },
```

移除 `gemini` runner 的 `"model_map"` 區塊：

```json
    "gemini": {
      "command": "gemini",
      "args": [
        "-y",
        "-p",
        "{prompt}"
      ]
    },
```

移除 `cursor` runner 的 `"model_map"` 區塊：

```json
    "cursor": {
      "command": "agent",
      "args": [
        "-p",
        "{prompt}"
      ]
    },
```

其餘 runner（claude/agy/copilot）本來就沒有 `model_map`，不用動。

- [ ] **Step 2: 驗證 JSON 合法**

Run: `python3 -m json.tool .4x/settings.json > /dev/null`
Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add .4x/settings.json
git commit -m "feat(F037): add model_tiers, remove model_map from all runners"
```

---

### Task 5: 更新 CLAUDE.md plugin 文件

**Files:**
- Modify: `.4x/plugins/CLAUDE.md`

- [ ] **Step 1: 更新 Step 3 mapping table**

在 `.4x/plugins/CLAUDE.md` 的 "Step 3: Load Model Config" 段落，把：

```markdown
### Step 3: Load Model Config

Read `.4x/settings.json` and extract the `roles` section. Map to workflow model names:

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "opus" }
  }
}
```

Build the `models` object from config:

| Config key | models field | Default |
|---|---|---|
| `roles.designer.model` | `designer` | `opus` |
| `roles.coder.model` | `coder` | `sonnet` |
| `roles.reviewer.model` | `reviewer` | `sonnet` |
| `roles.reviewer.deep_model` | `deep_reviewer` | `opus` |
| `roles.tester.model` | `tester` | `sonnet` |
| `roles.acceptor.model` | `acceptor` | `opus` |

If no `roles` section exists, all defaults apply.
```

改為：

```markdown
### Step 3: Load Model Config

Read `.4x/settings.json` and extract the `roles` section. Roles use abstract tier names (opus/sonnet/haiku); the workflow passes these as-is — `4x run` handles tier → vendor model resolution internally.

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "opus" }
  }
}
```

Build the `models` object from config:

| Config key | models field | Default |
|---|---|---|
| `roles.designer.model` | `designer` | `opus` |
| `roles.coder.model` | `coder` | `sonnet` |
| `roles.reviewer.model` | `reviewer` | `sonnet` |
| `roles.reviewer.deep_model` | `deep_reviewer` | `opus` |
| `roles.tester.model` | `tester` | `sonnet` |
| `roles.acceptor.model` | `acceptor` | `opus` |

If no `roles` section exists, all defaults apply. Model tier resolution (mapping tier names to vendor-specific model names) happens in `4x run` via `model_tiers` config — the workflow does not need to handle this.
```

- [ ] **Step 2: Commit**

```bash
git add .4x/plugins/CLAUDE.md
git commit -m "docs(F037): update CLAUDE.md plugin to document tier-based model config"
```

---

### Task 6: 全量驗證

- [ ] **Step 1: 跑完整建置與測試**

Run: `make build && make test && make lint`
Expected: 全部 PASS

- [ ] **Step 2: 確認 docs 同步**

Run: `make check-docs 2>/dev/null || echo "no check-docs target"`
Expected: PASS 或無此 target

- [ ] **Step 3: 最終 commit（如有遺漏修正）**

只在有修正時 commit。否則跳過。
