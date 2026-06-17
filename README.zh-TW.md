[English](README.md) | **繁體中文** | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

<p align="center">
  <img src="docs/assets/4x-banner.svg" alt="4X — Design. Code. Review. Test." width="480">
</p>

<p align="center">
  <img src="docs/assets/demo.gif" alt="4x demo" width="720">
</p>

**4x 是一個多角色 AI 開發框架，將軟體工程迴圈拆分為四個專業階段** — Design（設計）、Code（實作）、Review（審查）、Test（測試） — 每個階段由專屬的 AI agent 驅動。如同 4X 策略遊戲（eXplore、eXpand、eXploit、eXterminate），這個名字反映了一個由不同角色、各具優勢，合力征服複雜度的系統。

---

## 為什麼選 4x？

單一 agent 寫程式快但脆弱。你讓同一個 AI 設計、實作、審查、測試 — 同一口氣、同樣的偏見。小任務能用，真正的功能開發就會崩潰。

4x 拆開了這個迴圈。每個角色有明確的職責、有限的範圍，且無法接觸其他角色的推理過程。Designer 不寫程式碼。Coder 不評判自己的作品。Reviewer 天生就是對抗性的。Tester 根據實作前就寫好的驗收標準來驗證。

結果：功能經得起上線的考驗。

## 取捨

選擇 4x 意味著用速度和成本換取結構和正確性。請誠實評估你的專案是否需要這樣的取捨。

### 優勢

- **角色隔離消除自我審查偏見。** Coder 永遠不會評判自己的作品。Reviewer 天生就是對抗性的。單一 agent 工作流讓同一個模型寫程式碼又批准程式碼 — 4x 不會。
- **確定性的 guardrail 不依賴 AI 判斷。** 範圍鎖定、狀態機、證據要求 — 這些由 Go 寫的 CLI 強制執行，而非靠提示 LLM「請留在範圍內」。
- **基於檔案的協定使其與 LLM 無關。** 可在 Claude、Gemini、Codex 之間切換，或按角色混搭使用。沒有供應商鎖定，沒有 SDK 依賴。
- **抗崩潰的狀態。** 所有東西都存在 `.4x/` 檔案裡。Session 中斷、機器重開 — `4x run` 會從中斷處精確接續。
- **人類始終在迴圈中。** `pending-review` 閘門確保人類在標記完成前一定會審查 AI 的工作。AI 提案，你決定。
- **批次模式可擴展。** 依賴感知的排程讓你可以把幾十個 feature 排進夜間執行，早上再來 review。

### 劣勢

- **Token 成本顯著更高。** 每個 feature 至少經過 4 次以上的獨立 LLM 呼叫。Review 失敗會翻倍。預期同一任務的 token 成本是單一 agent 方式的 3-10 倍。參見 [使用技巧](docs/guide/zh-TW/usage-tips.md) 的成本估算。
- **簡單任務更慢。** 一行 bug fix 不需要 Designer、Reviewer 和 Tester。完整迴圈的開銷在瑣碎變更上是浪費。簡單修復請用單一 agent 工具。
- **設定成本。** `4x init`、feature YAML、settings 設定 — 開始前有一些儀式。不值得為一個用完即丟的腳本這樣做。
- **固定的迴圈結構。** Design → Code → Review → Test 的順序是固定的。如果你的工作流不適合四個角色，你會與框架對抗而非使用它。
- **品質取決於 prompt 品質。** 模糊的 feature 描述產生模糊的 spec，進而產生錯誤的程式碼。4x 增加了結構，但垃圾進仍然是垃圾出 — 只是多了更多步驟。

### 何時使用 4x

- 需要正確性的功能（支付、認證、資料管線）
- 受益於對抗性審查的工作（安全敏感的程式碼）
- Feature backlog 的批次處理
- 需要 AI 生成程式碼審計軌跡的團隊

### 何時不該使用 4x

- 快速的一次性修復或探索性原型
- 速度比正確性更重要的任務
- Token 預算吃緊的專案
- 你自己會 review 程式碼的個人 hacking session

## 架構

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**第一層 — CLI** 處理所有確定性的事務：範圍驗證、狀態轉換、基線快照、證據收集。它永遠不呼叫 LLM。Guardrail 不依賴 AI 判斷。

**第二層 — Runner** 將 CLI 協定橋接到你選擇的 AI 工具。Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor — 每個都使用相同的 `.4x/` 檔案協定，但使用各平台原生的能力。

**第三層 — Live** 是多專案儀表板。即時觀看你的 AI agent 工作、查看階段轉換、串流日誌。REST + SSE API。

## 安裝

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/4x
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下載 Binary

macOS、Linux、Windows（amd64 / arm64）的預編譯 binary 可在 [Releases](https://github.com/ggwhite/4x/releases) 頁面下載。

## 快速開始

```bash
# 在你的專案中初始化
cd my-project
4x init

# 建立一個 feature
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# 執行完整迴圈
4x run F001 --runner claude

# 檢查狀態
4x status

# 審查並完成
4x done F001

# 或即時觀看
4x live -w
```

`4x run` 自動驅動 Design-Code-Review-Test 迴圈。如果 Review 發現問題，Code 會再跑一輪。如果 Test 失敗，迴圈會反覆執行。你可以用 `--max-rounds` 和 `--timeout` 旗標保持控制。

## 四個角色

| 角色 | 職責 | 產出 |
|---|---|---|
| **Designer** | 分析需求，產出 spec 和驗收標準 | `task-brief.md`、`acceptance-criteria.md` |
| **Coder** | 精確實作 spec 所述的內容 | 原始碼、`coder-report.md` |
| **Reviewer** | 抓 bug 和 spec 違規（清單式 + 對抗式） | `review-report.md` 含裁決結果 |
| **Tester** | 根據驗收標準用證據驗證 | `test-report.md`、`verify.json` |

每個角色都是**隔離的**。Coder 永遠看不到 Reviewer 先前的回饋。Tester 根據 Designer（而非 Coder）寫的標準來驗證。這種分離防止了單一 agent 工作流的盲點。

## 迴圈如何運作

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **Review 失敗**（裁決 FAIL 或 CRITICAL 發現）會將程式碼送回修改
- **Test 失敗**（驗證未通過）會將程式碼送回修改
- **Escalation**（spec 不符、標準錯誤）會路由回 Designer
- **Pending review** 閘門確保人類在標記完成前一定會審查
- **Round 預算**（預設 5）防止無限迴圈

## 確定性 Guardrail

由 CLI 強制執行，非 AI 判斷：

| Guardrail | 功能 |
|---|---|
| **範圍檢查** | 變更的檔案必須在宣告的 repo 範圍內 |
| **基線快照** | 編碼前的狀態被擷取，可安全回滾 |
| **狀態機** | 階段必須按合法順序進行 |
| **證據要求** | Tester 必須提供包含命令輸出的 verify.json |
| **測試閘門** | 需要 verify.json + test-report + final-report + commit-plan |
| **依賴閘門** | 未滿足依賴的 feature 無法啟動 |

## 批次模式

```bash
4x batch plan            # 產生依賴感知的執行計畫
4x batch run --runner claude  # 按順序執行所有合格的 feature
4x batch stop            # 完成當前 feature 後優雅停止
```

## 權限模型

**4x 以非互動模式執行 AI agent。** 在 `4x init` 期間，runner 會設定跳過權限提示的旗標（`--dangerously-skip-permissions`、`-y`、`approval: full-auto`），讓迴圈自主執行。

CLI 的確定性 guardrail（範圍鎖定、基線快照、狀態機）提供安全邊界。

**請僅在你接受自主 AI agent 執行的專案中使用 4x。**

## 文件

| 文件 | 說明 |
|---|---|
| **[使用指南](docs/guide/zh-TW/)** | 完整的使用文件 |
| [快速開始](docs/guide/zh-TW/getting-started.md) | 安裝與首次執行 |
| [CLI 參考](docs/guide/zh-TW/cli.md) | 所有命令與旗標 |
| [核心概念](docs/guide/zh-TW/concepts.md) | 角色、狀態機、協定、guardrail |
| [設定](docs/guide/zh-TW/configuration.md) | Settings、模型、語系、runner |
| [Runner 與 Plugin](docs/guide/zh-TW/runners.md) | 支援的 runner 與 plugin 合約 |
| [儀表板](docs/guide/zh-TW/dashboard.md) | 4x Live 多專案儀表板 |
| [批次模式](docs/guide/zh-TW/batch.md) | 依賴感知的批次執行 |

## 專案結構

```
4x/
  cmd/4x/              CLI 進入點 (Cobra)
  internal/
    protocol/           .4x/ 檔案格式、workspace、型別
    state/              狀態機（階段轉換）
    guard/              Guardrail 檢查（範圍、基線、證據）
    batch/              依賴 DAG、批次排程器
    runner/             子程序 runner 介面
    server/             SSE + REST server（Live 儀表板用）
  plugins/
    claude-code/        Claude Code skill + workflow
    codex/              Codex runner 指令
    gemini/             Gemini runner 指令
    agy/                Antigravity runner 指令
    copilot/            Copilot runner 指令 + workflow
    cursor/             Cursor 規則
    embed.go            go:embed 將 plugin 檔嵌入 binary
  dashboard/
    macos/              Swift 原生 app（規劃中）
  docs/
    guide/              使用者文件
    architecture/       系統層級設計文件
    design/             機制設計文件
    reference/          Plugin 合約
```

## 常見問題

**問：4x 會直接呼叫任何 LLM API 嗎？**
不會。CLI 是純 Go，零 LLM 依賴。Runner 使用各平台原生能力處理所有 AI 互動。

**問：我可以對不同角色使用不同的 LLM 嗎？**
可以。在 `.4x/settings.json` 中設定各角色的模型。用 Claude 做 Design、Gemini 做 Code — 每個都讀取相同的 `.4x/` 檔案。

**問：這跟 Devin / SWE-agent / OpenHands 有什麼不同？**
那些是一次完成所有事情的自主 agent。4x 是一個*框架*，用確定性的 guardrail 來結構化多角色協作。它更接近 AI 的 CI pipeline，而非單一自主 agent。

## 起源故事

4x 誕生於一個名為 DCT（Designer-Coder-Tester）的生產系統，該系統為一個大規模平台重寫交付了 60 多個 feature。存活下來的模式 — 角色隔離、基於檔案的協定、確定性的範圍檢查、基於證據的測試 — 成為了 4x。未能存活的部分 — LLM 專屬的 hack、共享上下文的假設、基於信任的 guardrail — 被刻意排除了。

## 貢獻

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## 授權

[MIT](LICENSE)

---

<p align="center">
  <strong>別再期望你的 AI 寫出正確的程式碼。開始驗證它。</strong>
</p>
