# F037: Model Tier Abstraction

## Problem

Roles 使用 vendor-specific model name（如 `"opus"`），但只有 Claude 認得。其他 runner（gemini/codex/agy/copilot）收到 `--model opus` 就直接報錯。現有的 `model_map`（per-runner 翻譯表）散落在各 runner config 中，且非強制——漏填就 pass through 導致執行失敗。

## Solution

引入抽象 tier（`opus` / `sonnet` / `haiku`），集中定義每個 tier 在每個 runner 對應的 model name。Runner 可個別覆寫。Resolution 找不到對應時直接 error，不再 pass through。

Breaking change：移除 `RunnerConfig.ModelMap`。

## settings.json 新結構

```json
{
  "model_tiers": {
    "opus":   { "claude": "opus",   "gemini": "gemini-2.5-pro",   "codex": "gpt-5.5", "agy": "opus",   "copilot": "claude-opus-4-6" },
    "sonnet": { "claude": "sonnet", "gemini": "gemini-2.5-flash", "codex": "gpt-5.5", "agy": "sonnet", "copilot": "claude-sonnet-4-6" },
    "haiku":  { "claude": "haiku",  "gemini": "gemini-2.5-flash", "codex": "gpt-5.5", "agy": "haiku",  "copilot": "claude-haiku-4-5" }
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus"
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"],
      "tiers": { "opus": "gemini-2.5-pro-preview" }
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder":    { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester":   { "model": "sonnet" }
  }
}
```

### 欄位說明

- **`model_tiers`**（頂層）：每個 tier 對每個 runner 的 model name mapping
- **`runners[name].model`**：預設 tier（role 沒設 model 時用）
- **`runners[name].tiers`**（可選）：個別 runner 的 tier 覆寫，優先於 `model_tiers`
- **`roles[name].model`**：該 role 使用的 tier name

### 移除的欄位

- `RunnerConfig.ModelMap`（`model_map`）：被 `model_tiers` + `runners.tiers` 完全取代

## Resolution 邏輯

```
resolveModel(cfg, runnerName, role) → (string, error):

1. tier = roles[role].model
   若空 → tier = runners[runnerName].model
   若仍空 → tier = "sonnet"

2. model = runners[runnerName].tiers[tier]     // runner 覆寫（最高優先）
   若空 → model = model_tiers[tier][runnerName]  // 頂層定義
   若仍空 → error: "runner %s has no model for tier %s"
```

`deep_model` 同理，改查 `roles[role].deep_model`。

**不再 pass through**：tier name 不會直接當 model name 傳給 CLI。找不到對應就 error，強制使用者填完 mapping。

## 檔案變更範圍

| 檔案 | 改動 |
|---|---|
| `internal/protocol/types.go` | `Config` 加 `ModelTiers`；`RunnerConfig` 移除 `ModelMap`，加 `Tiers` |
| `cmd/4x/run.go` | `resolveModel()` 改用新 resolution 邏輯，回傳 error |
| `.4x/settings.json` | 加 `model_tiers`，移除所有 `model_map`，更新整體結構 |
| `.4x/plugins/CLAUDE.md` | Step 3 mapping table 更新 |

### 不改的檔案

- `internal/runner/` — runner 層不碰 tier，只管拿到的 model string
- `internal/server/` — process.go 呼叫 `4x run`，resolution 在 run 內部
- `.4x/plugins/workflow.js` — 從 args.models 拿 resolved model name，不碰 tier
- Dashboard 前端

## 測試策略

1. **`resolveModel` 單元測試**：
   - role 有 model → 用 role 的 tier
   - role 沒 model → fallback 到 runner 預設 tier
   - runner 和 role 都沒設 → fallback 到 "sonnet"
   - runner `tiers` 覆寫優先於 `model_tiers`
   - tier 在兩處都找不到 → 回傳 error
   - `deep_model` 走同樣邏輯

2. **既有測試不受影響**：`runner_test.go` 的 `ModelOverride` 測試不動

3. **手動驗證**：
   - `4x run` 指定 claude → "opus" tier 解析正確
   - `4x run` 指定 gemini → 翻譯成 "gemini-2.5-pro"
