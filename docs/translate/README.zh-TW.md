<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/banner-dark.svg">
  <img src="../assets/banner-light.svg" alt="4X：設計、實作、審查、測試。" width="100%">
</picture>

**一個 agentic 開發迴圈：把 Design、Code、Review、Test 拆成彼此隔離的 AI 角色，再用確定性的 guardrail 把關。**

<p>
  <a href="https://github.com/ggwhite/4x/actions/workflows/ci.yml"><img src="https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ggwhite/4x/releases"><img src="https://img.shields.io/github/v/release/ggwhite/4x" alt="最新版本"></a>
  <a href="https://pkg.go.dev/github.com/ggwhite/4x"><img src="https://pkg.go.dev/badge/github.com/ggwhite/4x.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/ggwhite/4x"><img src="https://goreportcard.com/badge/github.com/ggwhite/4x" alt="Go Report Card"></a>
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="授權：MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey" alt="平台：macOS、Linux、Windows">
</p>

[安裝](#安裝) · [快速開始](#快速開始) · [功能](#功能) · [迴圈如何運作](#迴圈如何運作) · [Guardrail](#確定性-guardrail) · [架構](#架構) · [文件](#文件) · [FAQ](#faq)

[English](../../README.md) | **繁體中文** | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md)

</div>

---

<div align="center">
  <a href="../assets/tour.mp4"><img src="../assets/tour.webp" alt="4x 導覽，取自一次真實執行並加速播放：(1) 4x init 在一個既有的 Go 專案 greet 上完成設定，並偵測出 go build 與 go test 指令；(2) 4x new 建立 feature F001 Greet by name；(3) 4x run F001 讓 Claude Code 依序扮演 Designer、Coder、Reviewer、Tester；Tester 的檢查被 verify 白名單擋下，feature 因此 escalate 回 Designer 重寫，第二輪通過後進入 Accept 與 pending-review，並印出總花費；(4) 4x status 列出處於 pending-review 的 feature，4x done F001 將它標記完成，go run . -name Ada 印出 hello, Ada" width="900"></a>
  <br>
  <sub>▶ <a href="../assets/tour.mp4">觀看完整導覽</a> · 一次真實的 Claude Code 執行（約 4 分鐘、$2），agent 等待時間已剪短</sub>
</div>

## 為什麼

單一 agent 寫程式很快，但很脆弱。你讓同一個 AI 設計、實作、審查、測試，一口氣做完，帶著同樣的偏見。小任務還行，碰到真正的功能就撐不住。

4x 把迴圈拆開。每個角色職責專一、範圍有限，也看不到其他角色的推理過程：Designer 不寫程式碼，Coder 不評判自己的成果，Reviewer 天生就站在對立面，Tester 則依照實作前就寫好的驗收標準來驗證。決定下一步能做什麼的是 Go CLI，不是 LLM。就像 4X 策略遊戲（eXplore、eXpand、eXploit、eXterminate），各有所長的不同角色朝同一個目標前進：做出經得起上線考驗的功能。

## 功能

<table>
<tr>
<td width="42%" valign="middle">

### 四個隔離的角色

**Designer → Coder → Reviewer → Tester → Accept**，每個角色都是獨立的 runner 行程，彼此只透過 `.4x/` 裡的檔案溝通。Reviewer 判定 **FAIL** 或驗證失敗，程式碼會送去 **amending**；規格有誤則 **escalate** 回 Designer。Pipeline **profile**（`full`、`normal`、`lite`、`quick` 或自訂）決定要跑哪些階段。

</td>
<td><img src="../assets/feature-loop.webp" alt="示範執行時的 4x Live Messages 分頁：先出現 Designer 產出 task-brief.md 與 acceptance-criteria.md 的訊息，接著 Coder、Reviewer、Tester 的報告依序出現，每則都標註模型與花費；Tester 發起 escalation，Designer 重寫 brief，第二輪 Coder、Reviewer、Tester 跑完後，Acceptor 把 feature 收進 Review"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 確定性的 guardrail

Guardrail 是 **Go 程式碼，不是 prompt**。`4x check` 檢查 **scope**、**baseline** 快照、必要檔案與 **verify 證據**；**狀態機**拒絕不合法的階段跳轉；`4x verify` 只執行專案**白名單**內的指令。Runner 怎麼說都繞不過去。

</td>
<td><img src="../assets/feature-guardrails.webp" alt="終端機：4x check F001 回報已完成的示範 feature 通過所有 guardrail 檢查；4x transition F001 --to coding --json 被拒，訊息為 invalid transition: pending-review to coding；4x verify F001 依每條驗收標準執行 test-strategy.yaml 裡的 go build、go vet、go test 檢查並印出 PASSED"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 4x Live 儀表板

`4x live` 透過 REST + **SSE** 提供**多專案儀表板**：feature 卡片隨階段變化從 Todo 移到 Running 再到 Review，並顯示每個 feature 的輪次、runner 與花費。同一套 web UI 內嵌在 binary 裡，也包裝成 **macOS（Swift）** 與 **Windows / Linux（Tauri）** 應用程式。

</td>
<td><img src="../assets/feature-live.webp" alt="示範執行時的 4x Live 專案總覽：F001 Greet by name 卡片從 Todo 移到 Running，階段標籤依序切換 designing、coding、reviewing、testing，Running 計數、狀態圓環圖與 Currently running 面板同步更新；最後卡片停在 Review 並出現 Mark Done 按鈕，Pending review 面板也列出它"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 依角色與輪次計算花費

每次 runner 呼叫都會記錄模型、耗時與 **USD 花費或 token 數**。`4x cost` 依**角色**彙總，`--by-round` 顯示有多少花在**重試**上，`--feature` 則把單一 feature 逐輪拆開，讓你看清一次 review 失敗實際花了多少。

</td>
<td><img src="../assets/feature-cost.webp" alt="終端機：4x cost 印出示範專案依角色（designer、coder、reviewer、tester、acceptor）的花費，含呼叫次數、總額、平均與占比；接著 4x cost --feature F001-greet-by-name 依輪次與角色列出同樣的呼叫"></td>
</tr>
</table>

### 更多功能

- **7 種 runner**：Claude Code、Codex、Gemini CLI、Antigravity、Copilot、Cursor 與 OpenCode 共用同一套 `.4x/` 檔案協定，可依角色或階段混搭（`--phase-override`）。
- **自適應 pipeline**：profile 顧問依 feature 複雜度建議 `full` / `normal` / `quick`；各角色的模型使用 `strong` / `fast` 兩種等級。
- **當機復原**：runner 當掉後從最後儲存的狀態接續；暫時性的 API 錯誤（網路、rate limit）會以 backoff 重試。
- **人工簽核**：每個 feature 都會停在 `pending-review`；`4x done` 將它標記完成並合併分支（啟用 `issue_tracker` 時則改為開 PR/MR）。
- **Issue-first MR 流程**：選用的 `issue_tracker` 模式。`4x new` 建立或連結 issue，`4x done` 推送分支並開 PR/MR，而不是在本機合併。GitHub 或 GitLab（含自架）依各 repo 的 remote 自動判斷。
- **相依關係**：`4x new --depends F001` 讓 feature 等到相依項目完成才能開始；儀表板會畫出相依圖。
- **自我演化**：`4x mine` / `4x gate` / `4x evolve` 從過去的執行挖掘失敗訊號，經過價值閘門篩選候選項目，再排入改進用的 feature。回顧心得（`4x learn`）會注入之後的角色 prompt。
- **MCP server** 與 **Claude Code skill**（`4x-audit`、`4x-autopilot`），見下方說明。
- **30 個以上的 CLI 指令**：`run`、`status`、`check`、`verify`、`retry`、`cost`、`live`、`doctor`、`clean`、`mcp`、`skills`、phase hook、結構化 log 等。詳見 [CLI 參考](../guide/zh-TW/cli.md)。

## 安裝

### Homebrew（macOS / Linux）

```bash
brew install ggwhite/tap/fourx
```

### Go install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell 腳本

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下載 binary

macOS、Linux、Windows（amd64 / arm64）的預先編譯 binary 都在 [Releases](https://github.com/ggwhite/4x/releases) 頁面。

<details>
<summary><b>驗證 checksum</b>（手動下載時）</summary>

Shell 腳本會自動驗證下載檔的 checksum，不符就中止。若從 [Releases](https://github.com/ggwhite/4x/releases) 頁面手動下載壓縮檔或 binary，請在解壓縮前自行驗證：把該版本的 `checksums.txt` 下載到同一個目錄，然後執行以下其中一個指令：

```bash
# Linux (and macOS with coreutils)
sha256sum --check --ignore-missing checksums.txt

# macOS (default)
shasum -a 256 --ignore-missing --check checksums.txt
```

確認 checksum 顯示 `OK` 後，再解壓縮並執行 binary。

</details>

## 快速開始

```bash
# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication (User authentication with OAuth2)

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` 會自動驅動 Design-Code-Review-Test 迴圈。Review 發現問題，Code 就再跑一次；Test 失敗，迴圈就再迭代。你可以用 `--max-rounds`、`--profile` 與 `--timeout` 掌控整個流程。

## 四個角色

| 角色 | 職責 | 產出 |
|---|---|---|
| **Designer** | 分析需求，產出規格與驗收標準 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` |
| **Coder** | 完全照規格實作 | 原始碼、`coder-report.md` |
| **Reviewer** | 抓出 bug 與違反規格之處（檢查清單 + 對抗式審查） | 附判定結果的 `review-report.md` |
| **Tester** | 依驗收標準驗證並附上證據 | `test-report.md`、`verify.json` |

每個角色都是**隔離**的。Coder 看不到 Reviewer 先前的回饋；Tester 依照 Designer 寫的標準驗證，而不是 Coder 寫的。這種分工能避開單一 agent 工作流常見的盲點。`full` profile 會在四個核心角色之外，再加上 Design Reviewer、多角度的 Deep Reviewer、Fixer 與 Acceptor。

## 迴圈如何運作

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/loop-dark.svg">
  <img src="../assets/loop-light.svg" alt="Designer、Coder、Reviewer、Tester、Accept、Pending Review 與 Done 排成一列。Reviewer 判定 FAIL 或驗證失敗時，程式碼送去 amending，再回到 Reviewer；escalation 回到 Designer；進入 Done 前需由人以 4x done 簽核。" width="100%">
</picture>

- **Review 失敗**（判定 FAIL 或有 CRITICAL 發現）會把程式碼送回 amending
- **Test 失敗**（verify 未通過）會把程式碼送回 amending
- **Escalation**（規格不符、驗收標準有誤）會轉回 Designer
- **Pending review** 閘門確保標記完成前一定有人審查
- **輪次上限**（預設 5）避免無限迴圈

合法的狀態轉換定義在 [`internal/state/machine.go`](../../internal/state/machine.go)；完整狀態機請見 [核心概念](../guide/zh-TW/concepts.md)。

## 確定性 Guardrail

由 CLI 強制執行，不靠 AI 判斷：

| Guardrail | 作用 |
|---|---|
| **Scope 檢查** | 變更的檔案必須在宣告的 repo 範圍內 |
| **Baseline 快照** | 寫程式前先保存狀態，方便安全回滾 |
| **狀態機** | 階段必須依合法順序推進 |
| **證據要求** | Tester 必須提供含指令輸出的 `verify.json` |
| **測試閘門** | 需要 `verify.json` + 測試報告 + 最終報告 |
| **Verify 白名單** | `4x verify` 只執行 `verify_command_allowlist` 列出的指令 |
| **相依閘門** | 相依項目未完成的 feature 不能開始 |

## 架構

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/architecture-dark.svg">
  <img src="../assets/architecture-light.svg" alt="你操作 4x CLI（Go）：確定性的 guardrail，不呼叫 LLM，負責 scope 檢查、協定、狀態機與相依閘門。它透過 .4x/ 目錄的檔案協定把工作交給 runner（Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor、OpenCode）。4x Live 接收 SSE 事件，提供多專案即時監控。" width="100%">
</picture>

**第 1 層：CLI** 處理所有確定性的工作：scope 驗證、狀態轉換、baseline 快照、證據收集。它從不呼叫 LLM，所以 guardrail 不依賴 AI 的判斷。

**第 2 層：Runner** 把 CLI 協定接到你選用的 AI 工具。每個 runner 都使用同一套 `.4x/` 檔案協定，但各自發揮原生平台的能力。

**第 3 層：Live** 是多專案儀表板。即時看 agent 工作、看階段轉換、串流 log。提供 REST + SSE API。

## Autopilot

先排好多個 feature（用 `--depends` 決定順序），再交給 `4x-autopilot` 這個 Claude Code skill 驅動：它會挑出下一個可執行的 feature，執行、簽核、合併後接著做下一個，全程無人值守。它取代了原本的 `4x batch` 指令。

## MCP server

`4x mcp` 透過 stdio 啟動 Model Context Protocol server，把 CLI 以 tool 的形式提供給 MCP client，例如 `4x_status`、`4x_new`、`4x_run`、`4x_stop`、`4x_check`、`4x_verify`、`4x_done`、`4x_doctor` 與 `4x_learn_*`。

```bash
4x mcp
```

## Claude Code skill

4x 附帶兩個選用的 [Claude Code Skills](https://docs.anthropic.com/en/docs/claude-code)，讓你在 Claude Code session 裡驅動 pipeline。用 [`npx skills`](https://github.com/vercel-labs/skills) 安裝即可，不必 clone 這個 repo：

```bash
npx skills add ggwhite/4x --skill 4x-audit       # scan past run artifacts, report + optionally file gap features
npx skills add ggwhite/4x --skill 4x-autopilot   # drive the full pick → run → merge → next loop unattended
npx skills add ggwhite/4x --all                  # install both
```

- **`4x-audit`**：掃描 discovered-feature-gaps、escalation／review 報告與反覆出現的 learnings 模式，產出分類好的 HTML 報告，也可選擇批次建立 feature YAML。
- **`4x-autopilot`**：輪詢 4x 狀態，驅動 feature 走完整個生命週期（design → code → review → test → merge），過程中不停下來確認，連合併也會自動完成。**僅限 repo 擁有者**：在共用 repo 上使用前，請先讀過 skill 裡的警告。

若是從這個 repo 的 clone 安裝，可改用 `4x skills install <name>` 把 skill 以 symlink 連到 `~/.claude/skills/`。

## 權限模型

> [!WARNING]
> **4x 會以非互動模式執行 AI agent。** 請只在你能接受 AI agent 自主執行的專案中使用 4x。

<details>
<summary>這代表什麼</summary>

`4x init` 時，runner 會被設定成帶有略過權限詢問的 flag（`--dangerously-skip-permissions`、`-y`、`--yolo`），讓迴圈能自主運作。

安全邊界由 CLI 的確定性 guardrail 提供（scope 鎖定、baseline 快照、狀態機、verify 白名單）。在啟動 runner 之前，4x 也會從它的環境中濾掉敏感的環境變數（`*_TOKEN`、`*_KEY`、`*_SECRET`、`AWS_*` 等）；詳見 [Runner 與 Plugin](../guide/zh-TW/runners.md)。

</details>

## 取捨

選擇 4x，就是用速度與成本換取結構與正確性。請老實評估你的專案是否需要這樣的取捨。

<details>
<summary><b>優勢、劣勢，以及何時（不）該用 4x</b></summary>

### 優勢

- **角色隔離消除自我審查的偏見。** Coder 從不評判自己的成果，Reviewer 天生站在對立面。單一 agent 工作流讓同一個模型寫程式碼又核准程式碼；4x 不會。
- **確定性的 guardrail 不依賴 AI 判斷。** Scope 鎖定、狀態機、證據要求：這些都由 Go 寫的 CLI 強制執行，而不是靠 prompt 拜託 LLM「請待在範圍內」。
- **以檔案為基礎的協定，不綁特定 LLM。** 可在 Claude、Gemini、Codex 之間切換，或依角色混搭。沒有廠商綁定，也不依賴 SDK。
- **耐當機的狀態。** 一切都存在 `.4x/` 檔案裡。Session 掛了、機器重開機：`4x run` 會從停下的地方精準接續。
- **人始終在迴圈裡。** `pending-review` 閘門確保 AI 的成果在標記完成前一定經過人審查。AI 提案，由你拍板。
- **駕馭大規模重構。** 單一 AI session 吃不下的大型改動（拆分 god object、抽出 package、遷移 API），可以拆成彼此相依、各配合適 profile 的 feature。4x 負責跨多個階段的排序、審查與驗證，這些工作塞進單一 context window 會撐爆。
- **能擴展到整份 backlog。** 搭配相依關係與 autopilot skill，你可以在晚上排進幾十個 feature，隔天早上再來審查。

### 劣勢

- **Token 成本明顯更高。** 每個 feature 至少要經過 4 次以上獨立的 LLM 呼叫，一次 review 失敗就翻倍。同樣的任務，預期 token 成本是單一 agent 做法的 3 到 10 倍。成本估算見 [使用技巧](../guide/zh-TW/usage-tips.md)。
- **簡單任務反而更慢。** 一行的 bug 修正不需要 Designer、Reviewer 和 Tester，完整迴圈的開銷在瑣碎變更上是浪費。快速修正請用單一 agent 工具。
- **前置設定成本。** `4x init`、feature YAML、設定檔：開工前有一套儀式要走。拋棄式的腳本不值得。
- **迴圈結構固定。** Design → Code → Review → Test 的順序是固定的。如果你的工作流程塞不進四個角色，你會一直在跟框架搏鬥，而不是在使用它。
- **品質取決於 prompt 品質。** 模糊的 feature 描述產出模糊的規格，模糊的規格產出錯誤的程式碼。4x 增加了結構，但垃圾進、垃圾出依然成立，只是多了幾個步驟。

### 何時使用 4x

- 必須正確的功能（金流、認證、資料 pipeline）
- 需要對抗式審查的工作（安全敏感的程式碼）
- 消化整份 feature backlog
- 需要 AI 產生程式碼稽核軌跡的團隊

### 何時不該使用 4x

- 一次性的快速修正或探索性原型
- 速度比正確性重要的任務
- Token 預算吃緊的專案
- 反正你會自己審查程式碼的個人 hacking

</details>

## 文件

| 文件 | 說明 |
|---|---|
| **[使用者指南](../guide/zh-TW/)** | 完整的使用說明 |
| [入門](../guide/zh-TW/getting-started.md) | 安裝與第一次執行 |
| [CLI 參考](../guide/zh-TW/cli.md) | 所有指令與 flag |
| [核心概念](../guide/zh-TW/concepts.md) | 角色、狀態機、協定、guardrail |
| [設定](../guide/zh-TW/configuration.md) | 設定檔、模型、語系、runner |
| [Runner 與 Plugin](../guide/zh-TW/runners.md) | 支援的 runner 與 plugin 合約 |
| [儀表板](../guide/zh-TW/dashboard.md) | 4x Live 多專案儀表板 |
| [使用技巧](../guide/zh-TW/usage-tips.md) | 模型選擇、成本、疑難排解 |

<details>
<summary><b>專案結構</b></summary>

```
4x/
  cmd/4x/              CLI entry point (Cobra), one file per subcommand
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    orchestrator/       Run loop orchestration
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
    mcp/                MCP server (stdio)
  plugins/
    claude-code/        Claude Code runner instructions
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions
    cursor/             Cursor rules
    opencode/           OpenCode runner instructions
    embed.go            go:embed plugin files into binary
  dashboard/
    web/                Dashboard UI, embedded into the binary
    macos/              Swift native app
    tauri/              Windows / Linux app
  skills/               4x-audit, 4x-autopilot Claude Code skills
  templates/            Role prompt templates
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

</details>

## FAQ

<details>
<summary><b>4x 會直接呼叫任何 LLM API 嗎？</b></summary>

不會。CLI 是純 Go，完全不依賴 LLM。所有 AI 互動都由 runner 透過其原生平台能力處理。

</details>

<details>
<summary><b>可以讓不同角色使用不同的 LLM 嗎？</b></summary>

可以。在 `.4x/settings.json` 設定各角色的模型。Design 用 Claude、Code 用 Gemini：大家讀的都是同一批 `.4x/` 檔案。

</details>

<details>
<summary><b>這跟 Devin / SWE-agent / OpenHands 有什麼不同？</b></summary>

那些是一次包辦所有事的自主 agent。4x 是一套*框架*，用確定性的 guardrail 組織多角色協作。它比較像是給 AI 用的 CI pipeline，而不是單一的自主 agent。

</details>

## 開發

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
make build      # bin/4x
make test       # go test -race ./...
make lint       # go vet + gofmt + golangci-lint
```

完整流程請見 [CONTRIBUTING.md](../../CONTRIBUTING.md)。

<details>
<summary><b>重新錄製 README 的短片與圖表</b></summary>

所有短片都來自從原始碼編譯的真實 binary，在 `/tmp/4xdemo` 底下的拋棄式專案與 `HOME` 中錄製。錄製全程 headless：終端機用 [`vhs`](https://github.com/charmbracelet/vhs)，儀表板用 Python Playwright（headless Chromium）。需要 `vhs`、`ffmpeg`、`img2webp`（libwebp）、Python `playwright`，錄導覽片時還需要已登入的 `claude` CLI。

```bash
docs/assets/record/render.sh tour      # fresh /tmp/4xdemo, ONE real 4x run (about $2 of tokens), all clips
docs/assets/record/render.sh recut     # re-encode the clips from the last run's frames, no agent run
python3 docs/assets/diagrams/gen.py    # architecture / loop SVGs (light + dark)
python3 docs/assets/diagrams/snapshot.py /tmp/svg docs/assets/*-light.svg   # check frames + reduced motion
```

`render.sh tour` 會執行 [`setup-demo.sh`](../assets/record/setup-demo.sh)（從原始碼編譯 4x，建立一個小小的 Go 專案「greet」），在 [`capture-live.py`](../assets/record/capture-live.py) 截取 `4x live` 畫面的同時錄製 [`tour.tape`](../assets/record/tour.tape)，接著錄製各功能短片，最後由 [`tour-end.tape`](../assets/record/tour-end.tape) 簽核該 feature。[`retime.py`](../assets/record/retime.py) 會剪掉漫長的 agent 等待時間，並用 `img2webp` 編碼成 WebP。Claude Code runner 透過一個 wrapper 呼叫，略過使用者設定、hook、plugin 與 MCP server。

提交新短片前，請對每一格做 OCR（`swiftc docs/assets/record/ocr.swift -o /tmp/ocr`，接著 `magick clip.webp -coalesce f-%03d.png` 與 `/tmp/ocr f-*.png`），並 grep 文字中是否出現使用者名稱、主機名稱、email 與 token。

</details>

## 緣起

4x 誕生於一個名為 DCT（Designer-Coder-Tester）的正式環境系統，它為一次大規模平台改寫交付了 60 個以上的功能。存活下來的模式（角色隔離、以檔案為基礎的協定、確定性的 scope 檢查、以證據為本的測試）成了 4x；沒能存活的部分（針對特定 LLM 的 hack、共享 context 的假設、靠信任的 guardrail）則被刻意捨棄。

## 授權

[MIT](../../LICENSE)

---

<p align="center">
  <strong>別再祈禱 AI 寫出正確的程式碼，開始驗證它。</strong>
</p>
