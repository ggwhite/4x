# 核心概念

## 四個角色

| 角色 | 職責 | 輸入 | 產出 | 不可做 |
|---|---|---|---|---|
| **Designer** | 分析需求，產出 spec，定義驗收標準和測試策略 | Feature 描述、程式碼庫 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` | 修改原始碼 |
| **Coder** | 實作 spec 所述的內容 | `task-brief.md`、先前的 test/review 報告 | 原始碼、`coder-report.md` | 修改驗收標準或測試腳本 |
| **Reviewer** | 抓 bug、安全問題、spec 違規 | Diff、spec、coder 報告、專案規則 | `review-report.md` | 修改原始碼 |
| **Tester** | 根據驗收標準用證據驗證 | 驗收標準、coder 報告、測試策略 | 測試腳本、`test-report.md`、`verify.json`、`final-report.md`、`commit-plan.md` | 修改原始碼 |

每個角色都是**隔離的** — Coder 在實作時永遠看不到先前的 review 回饋。Tester 根據 Designer（而非 Coder）寫的標準來驗證。

### Review：兩階段

1. **清單式審查**（標準模型）— 根據專案硬規則檢查：安全性、並行性、錯誤處理、風格
2. **對抗式審查**（深度模型）— 「這個 diff 裡藏著最糟糕的 bug 是什麼？」發現按嚴重程度分級。

### Escalation

Coder 或 Tester 可以在以下情況時 escalate 回 Designer：

| 原因 | 意義 |
|---|---|
| `spec-mismatch` | DB/API 與 spec 不符 |
| `criteria-wrong` | 驗收標準不正確 |
| `blocker` | 缺少依賴或基礎設施問題 |
| `scope-change` | 需要修改範圍外的 repo |

Escalation 會寫入 `escalation.json`。迴圈會自動路由回 Designer。

---

## 狀態機

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     ├── amending ←──────────┘
                     ↑      ↓
                     └──────┘
```

### 所有合法轉換

| 從 | 到 |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`、`designing` |
| `reviewing` | `testing`、`amending` |
| `amending` | `reviewing`、`designing` |
| `testing` | `accepting`、`amending`、`designing` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`、`coding`、`testing` |
| `needs-attention` | `designing`、`coding` |
| any | `blocked`、`needs-attention` |

### 輪次計數器

- 在 round 為 0 時進入 `coding` 會將 round 設為 1
- 進入 `amending` 會遞增 round
- 當 round >= maxRounds 或連續 3 輪以上無進展時，`ShouldStop` 會觸發

### 迴圈中的階段決策

| 階段 | 條件 | 動作 |
|---|---|---|
| `designing` | `task-brief.md` 遺失 | → `needs-attention` |
| `coding` / `amending` | `escalation.json` 含 `spec-mismatch` 或 `criteria-wrong` | → `designing` |
| `reviewing` | Verdict 行以 FAIL 開頭或含 `[CRITICAL]` | → `amending` |
| `testing` | `verify.json` 未通過或缺少 artifact | → `amending` |

---

## 檔案協定

角色透過 `.4x/` 目錄通訊，而非共享的上下文視窗。

```
.4x/
├── settings.json                    # 專案設定
├── plugins/                         # Runner 指令檔
├── batch-plan.json                  # 批次執行計畫
├── batch-stop                       # 優雅停止信號
├── features/
│   └── {id}.yaml                    # Feature 定義（正式來源）
└── {feature-id}/
    ├── state.json                   # 階段、角色、輪次、是否活躍、runner、停止原因
    ├── events.jsonl                 # 審計軌跡
    ├── baseline.json                # 編碼前快照（HEAD、branch、dirty 檔案）
    ├── task-brief.md                # Designer → Coder：spec + 架構
    ├── acceptance-criteria.md       # Designer → Tester：可測試的標準
    ├── test-strategy.yaml           # Designer → Tester：測試方法
    ├── final-report.md              # 迴圈結束摘要
    ├── commit-plan.md               # 如何將變更拆分為 commit
    ├── logs/
    │   └── round-{N}-{role}.log     # 每輪每角色的執行日誌
    └── rounds/round-{N}/
        ├── coder-report.md          # Coder 做了什麼
        ├── review-report.md         # Reviewer 的發現 + 裁決
        ├── test-report.md           # Tester 的結果
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status` 反映 `state.json` 的階段以便快速列表。`depends` 列出必須先完成的 feature ID。

---

## Guardrail

由 CLI 強制執行的確定性檢查 — 不依賴 AI 判斷。

| Guardrail | 功能 |
|---|---|
| **必要檔案** | 驗證階段對應的 artifact 是否存在（例如 designing 後的 `task-brief.md`） |
| **基線** | 擷取編碼前的狀態（HEAD、branch、dirty 檔案）；如有 dirty 檔案則警告 |
| **範圍** | 比對 `git diff --name-only HEAD` 的頂層目錄與 feature 宣告的 repo |
| **依賴** | 如果被依賴的 feature 未完成，則阻擋 `4x run` |
| **Backlog drift** | 當 `.4x/features/*.yaml` 與外部映射不同步時警告 |
| **Testing → Accepting 閘門** | 需要 `verify.json`（passed=true）、`test-report.md`、`final-report.md`、`commit-plan.md` |

可用 `4x check <feature-id>` 手動執行。

---

## Pending Review 閘門

迴圈**不會**直接進入 `done`。接受後，feature 進入 `pending-review` — 等待人類審查 AI 的工作。

```
... → accepting → pending-review → （人類審查）→ 4x done F001
```

這確保在 feature 被視為完成之前，人類一定會簽核。
