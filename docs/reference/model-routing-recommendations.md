# Model Routing Recommendations（各 role × 各 runner 建議模型）

> 更新：2026-07-08。模型 lineup 會過時，重大改版時請重查各家官方 model 清單再修訂本表。
> 依據：kairos 實測 135 次 coder 呼叫（2026-06~07）、4x repo 711 次 role 呼叫成本分析、
> Superpowers v6 實驗結論。詳細數據見 `docs/design/token-optimization-v2-plan.md`。

## 核心原則

1. **判斷密集的角色不降級**。實驗證實：次級模型寫 plan 會任務粒度坍縮、測試訊號劣化。
   省下的單價會以 retry 輪（整條 pipeline 重跑）的形式加倍賠回去。
2. **長 agentic 任務看 turns，不看單價**。kairos 實測：coder 用 Sonnet 5 平均 116 turns、
   $10.46/次；Opus 4.8 只要 66 turns、$7.54/次。每個 turn 都重付一次 context，
   世代較弱的模型「繞路」把單價優勢全部吃掉。**單價便宜 ≠ 總成本便宜。**
3. **短而有界的任務可以降級**。tester（跑指令記證據）、acceptor（讀預彙整 summary）
   的輸入輸出都有界，次級模型夠用且真的省。
4. **打回重做是最大成本來源**（retry 佔 pipeline 總成本 27.8%）。任何為了省錢
   而提高 FAIL 率的降級都是虧的。

## 角色分級

| 分級 | 角色 | 理由 |
|---|---|---|
| **T1 判斷密集** | designer, deep-reviewer | 產出品質決定下游一切；深度審查要抓架構問題與隱藏相依 |
| **T2 代碼產出** | coder, fixer | 長 agentic 任務，turns 放大模型差距；fixer 同樣寫 code 但範圍有界 |
| **T3 有界判斷** | design-reviewer, reviewer | gate 判斷重要，但輸入有界（review-package / 設計文件） |
| **T4 機械執行** | tester, acceptor, mini-coder | 跑指令、讀彙整、小範圍修改，輸入輸出皆有界 |

## 各 runner 建議模型

| Role | claude-code | codex (OpenAI) | gemini | copilot | agy (Antigravity) | cursor | opencode |
|---|---|---|---|---|---|---|---|
| designer (T1) | **Opus 4.8**（最難的用 Fable 5） | GPT-5.5 | Gemini 3 Pro / 3.1 Pro | Claude Opus 4.6（4.8 fast 尚為 preview） | Gemini 3.1 Pro | Claude Opus 4.8 或 GPT-5.5 | 依 provider 選同級（如 claude-opus-4-8） |
| design-reviewer (T3) | Opus 4.8；normal/quick 可 Sonnet 5 | GPT-5.5 | Gemini 3 Pro | Claude Opus 4.6 | Gemini 3.1 Pro | Claude Opus 4.8 | 同 designer 或降一級 |
| coder (T2) | **Opus 4.8**（實測不要用 Sonnet 5，見原則 2） | GPT-5.5（輕量任務 GPT-5.4-mini） | **Gemini 3 Flash**（見下方特例說明） | GPT-5.3-Codex 或 Claude Opus 4.6 | Gemini 3.1 Pro | Composer 2.5（便宜池）；難題切 Opus 4.8 | claude-opus-4-8 或 gpt-5.5 |
| reviewer (T3) | Opus 4.8；quick 可 Sonnet 5 | GPT-5.5 | Gemini 3 Pro | Claude Opus 4.6 | Gemini 3.1 Pro | Claude Opus 4.8 | 同 coder 同級 |
| tester (T4) | **Sonnet 5**（quick 可試 Haiku 4.5） | GPT-5.4-mini | Gemini 3 Flash | GPT-5.4-mini 或 Claude Haiku 4.5 | Gemini 3 Flash / 3.5 Flash | Composer 2.5 | 便宜級（haiku / flash / mini） |
| deep-reviewer (T1) | **Opus 4.8**（money path / 高風險用 Fable 5） | GPT-5.5 | Gemini 3 Pro / 3.1 Pro | Claude Opus 4.6 | Gemini 3.1 Pro | Claude Opus 4.8 或 GPT-5.5 | 頂級 |
| fixer (T2) | Opus 4.8 | GPT-5.5 | Gemini 3 Flash | GPT-5.3-Codex | Gemini 3.1 Pro | Composer 2.5 | 同 coder |
| acceptor (T4) | Sonnet 5 | GPT-5.4-mini | Gemini 3 Flash | Claude Haiku 4.5 | Gemini 3 Flash | Composer 2.5 | 便宜級 |
| mini-coder (T4) | Sonnet 5 | GPT-5.4-mini | Gemini 3 Flash | GPT-5.4-mini | Gemini 3 Flash | Composer 2.5 | 便宜級 |

### 各家 lineup 備註（2026-07 現況）

- **Claude**（claude-code runner）：Fable 5 > Opus 4.8 > Sonnet 5 > Sonnet 4.6 > Haiku 4.5。
  Sonnet 5 有導入期優惠價至 2026-08-31，到期後與 Opus 價差縮小，coder 更沒理由用它。
  Fable 5 定價高於 Opus tier，只留給最難的 designer / deep-review 場景。
- **OpenAI Codex CLI**：GPT-5.5 為官方建議主力；GPT-5.4-mini 給輕量任務／subagent；
  GPT-5.3-codex-spark 是即時互動用 research preview，不適合 pipeline。
  GPT-5.2-Codex 僅剩 API-key 路徑可用（ChatGPT 登入已 deprecated）。
- **Gemini CLI**：⚠️ 特例——Gemini 3 Flash 在 agentic coding（SWE-bench Verified 78%）
  **反超 Gemini 3 Pro**，且價格不到 Pro 的 1/4，所以 gemini runner 的 coder/fixer 建議
  Flash 而非 Pro；Pro 留給 designer/reviewer 這類長推理。Gemini 3.1 Pro Preview 陸續開放。
  需 CLI 0.21.1+ 並在 /settings 開 preview features。
- **Copilot CLI**：model picker 是 CLI 專屬子集（與 IDE 不同步）。穩定頂級為
  Claude Opus 4.6 / GPT-5.4 / Gemini 3.1 Pro；Claude Opus 4.8 目前僅 fast-mode preview。
- **Antigravity（agy）**：Gemini 3.1 Pro 為主力，另有 Gemini 3 / 3.5 Flash、
  Claude Sonnet 4.6 / Opus 4.6、GPT-OSS-120B。以官方 picker 為準（本節來源為第三方整理）。
- **Cursor**：Composer 2.5 走獨立便宜額度池，日常 coder 划算；判斷密集角色切
  Claude Opus 4.8 或 GPT-5.5（frontier 池計費）。
- **OpenCode**：provider-agnostic，照所接 provider 對應上表同級模型即可。

## 設定方式

model 字串填 runner 自己認得的 model flag 值（claude runner 可用 `opus` / `sonnet` 別名，
會解析到該 tier 最新版）：

```jsonc
// .4x/settings.json — profile 的 per-phase model
"profiles": {
  "full": {
    "phases": [
      { "phase": "coding",    "model": "opus"   },
      { "phase": "testing",   "model": "sonnet" },
      { "phase": "accepting", "model": "sonnet" }
    ]
  }
}
```

單次 run 臨時覆寫（不寫回 settings）：

```bash
4x run F0XX --phase-override coding:claude:opus
```

解析優先序：`--phase-override` > feature YAML > profile PhaseSpec.Model >
roles[role].Model > runner 預設（見 `internal/protocol/override.go` 的 ResolvePhaseModel）。

⚠️ 兩個踩過的坑（2026-07-08 實查）：

1. **tier 別名 vs 固定版號**：tiers 設 `"opus": "opus"`（別名）會讓 claude CLI 自動解析到
   最新版 Opus；設 `"opus": "claude-opus-4-6"` 則釘死版本。**user 層 `~/.4x/settings.json`
   的 tiers 若釘了舊版號，會在 project settings 載入失敗時成為 fallback**，造成「以為在跑
   4.8 其實在跑 4.6」。升級模型世代後記得檢查 user 層有無過時 pin。
2. **roles 白名單目前不含 fixer / mini-coder**（F145 追蹤中），在白名單修好前無法對這兩個
   role 做 per-role model 設定，它們會 fallback 到 runner 預設 tier。

## 反面清單（實測踩過的坑）

- ❌ coder 用 Sonnet 5 省配額 —— kairos 實測反而每次貴 39%（turns 爆量）
- ❌ designer 用次級模型 —— plan 任務粒度坍縮，下游全鏈受害（Superpowers v6 實驗）
- ❌ 用壓縮 plan / task-brief 字數省 token —— 測試訊號掉 62%；要省的是重複搬運，不是資訊
- ❌ 看單價選模型 —— 總成本 = 單價 × turns × 每 turn context，agentic 任務後兩項才是主宰
