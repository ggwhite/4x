# 4x Docs — LLM Wiki

> 本目錄是 4x 框架的**設計知識庫**。
> 所有 LLM coding agent（Claude Code / Copilot / Gemini CLI）在需要設計決策、協議規格、角色定義時，應查閱本索引找到對應文件。

## 使用方式

1. 先讀本檔（AGENTS.md）了解有哪些文件
2. 根據當前任務關鍵字，找到對應文件
3. 只讀需要的文件，不要一次全部灌入 context

## 目錄結構

```
docs/
├── AGENTS.md              # 本檔 — LLM Wiki 索引
├── architecture/          # 系統級設計（長期不變）
├── design/                # 機制設計（角色、護欄、排程等）
└── reference/             # 合約與 CLI 參考
```

## 文件索引

### guide/ — 使用說明書（人類可讀）

| 文件 | 說明 | 何時讀 |
|---|---|---|
| [README](guide/README.md) | 說明書目錄 | 找使用文件入口 |
| [getting-started](guide/getting-started.md) | 安裝、初始化、第一次跑 feature | onboarding |
| [cli](guide/cli.md) | 完整 CLI 指令參考（所有 flag） | 查指令用法、開發新 subcommand |
| [concepts](guide/concepts.md) | 四角色、狀態機、檔案協議、護欄 | 理解核心概念 |
| [configuration](guide/configuration.md) | settings.json、model override、locale | 改設定、加 runner |
| [runners](guide/runners.md) | Runner 支援矩陣、plugin 合約 | 開發或設定 runner |
| [dashboard](guide/dashboard.md) | 4x Live 多專案 dashboard | 使用或開發 dashboard |
| [batch](guide/batch.md) | Batch 依賴排程 | 使用 batch 模式 |
| [usage-tips](guide/usage-tips.md) | 使用建議、model 選擇、troubleshooting | 新手上路、排查問題 |

### architecture/ — 系統級設計

| 文件 | 行數 | 說明 | 何時讀 |
|---|---|---|---|
| [overview](architecture/overview.md) | ~90 | 專案總覽 — core properties、四角色 loop 圖、三層架構（CLI / Plugin / Dashboard） | 第一次接觸 4x、onboarding、新 session 想快速建立全貌 |
| [protocol](architecture/protocol.md) | ~490 | `.4x/` 目錄協議 — settings.json、features/*.yaml、state.json、events.jsonl、baseline.json、verify.json、所有 report 格式範本 | 讀寫 `.4x/` 檔案、實作 protocol package、開發 plugin |
| [state-machine](architecture/state-machine.md) | ~50 | 狀態機 — 10 states、16 valid transitions、round counter 規則 | 改狀態轉換邏輯、debug transition 錯誤 |

### design/ — 機制設計

| 文件 | 行數 | 說明 | 何時讀 |
|---|---|---|---|
| [roles](design/roles.md) | ~105 | 四角色定義 — Designer / Coder / Reviewer / Tester 各自的 inputs、outputs、constraints | 開發 role prompt template、改角色行為、理解角色交接 |
| [escalation](design/escalation.md) | ~30 | 升級機制 — 6 種 reason、escalation flow | 實作或 debug escalation 邏輯 |
| [guardrails](design/guardrails.md) | ~40 | `4x check` 護欄 — scope / baseline / required files / verify evidence 四項檢查 | 改 guard package、新增檢查規則 |
| [batch-mode](design/batch-mode.md) | ~85 | Batch 排程 — DAG、Union-Find、Hub/Leaf、Bridge、Chain、batch-plan.json 格式 | 改 batch package、debug 排程問題 |

### reference/ — 合約與參考

| 文件 | 行數 | 說明 | 何時讀 |
|---|---|---|---|
| [plugin-contract](reference/plugin-contract.md) | ~30 | Plugin 合約 — invocation、filesystem I/O、heartbeat、exit codes、dry-run | 開發新 plugin、改 plugin 介面 |
| [cli-reference](reference/cli-reference.md) | ~130 | CLI 指令參考 — init / new / status / check / transition / event / prompt / batch / live | 改 CLI、加新 subcommand、查參數用法 |

## 文件維護規則

- **新增文件時，必須同步更新本索引**
- `architecture/`：放系統級、長期穩定的設計文件
- `design/`：放機制設計（角色行為、護欄、排程等）
- `reference/`：放合約、CLI 參考等查閱型文件
- 未來如有 feature 分析文件，建 `features/` 目錄，檔名格式 `{feature-name}-{analysis|spec|plan}.md`
