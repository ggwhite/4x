# 快速開始

## 安裝

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

需要 Go 1.26+。

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下載 Binary

macOS、Linux、Windows（amd64 / arm64）的預編譯 binary 可在 [Releases](https://github.com/ggwhite/4x/releases) 頁面下載。

### macOS Gatekeeper

CLI 執行檔與 4x Live dashboard app 未經 Apple Developer 簽名。macOS Gatekeeper 會在首次啟動時封鎖。兩種解決方式：

**方法 A：移除隔離屬性（推薦）**

```bash
# CLI 執行檔
xattr -cr /usr/local/bin/4x

# Dashboard app
xattr -cr /Applications/4x\ Live.app
```

**方法 B：從系統設定允許**

1. 雙擊 app — macOS 顯示「無法打開，因為無法驗證開發者」
2. 開啟**系統設定 → 隱私權與安全性**
3. 向下捲動至**安全性**區段 — 會看到被封鎖的 app 訊息
4. 點擊**仍要打開**
5. 輸入密碼或使用 Touch ID 確認
6. App 將啟動；macOS 會記住你的選擇，之後不再詢問

### Windows SmartScreen

執行檔未經程式碼簽署。Chrome 和 Edge 可能封鎖下載，Windows SmartScreen 可能封鎖執行。

**瀏覽器封鎖下載：**

1. Chrome：點擊下載警告 → **保留** → **仍要保留**
2. Edge：點擊下載列的 `...` → **保留** → **顯示更多** → **仍要保留**

**SmartScreen 封鎖執行：**

1. 雙擊 exe — Windows 顯示「Windows 已保護您的電腦」
2. 點擊**詳細資訊**
3. 點擊**仍要執行**

或透過 PowerShell 解除封鎖：

```powershell
Unblock-File -Path .\4x.exe
```

### 驗證

驗證安裝：

```bash
4x --help
```

## 初始化專案

```bash
cd my-project
4x init
```

這會建立一個 `.4x/` 目錄，包含：
- `settings.json` — 專案設定、runner 定義、角色模型對應
- `plugins/` — runner 指令檔（SKILL.md、AGENTS.md 等）
- 根層級的 import 檔（CLAUDE.md、AGENTS.md、GEMINI.md 等）

4x 會自動偵測你的專案語言（Go、TypeScript、Java、Rust、Python）並預填 build/test/lint 命令。

如果 `.4x/` 已經存在，`init` 會報錯結束 — 使用 `4x sync` 來更新 plugin 檔。

## 建立 Feature

```bash
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Feature 存放在 `.4x/features/{id}.yaml`。ID 格式為 `F{NNN}-{slug}`（slug 最多 23 個字元）。

使用 `--repo` 來宣告哪些 repository 在範圍內（適用於多 repo 專案）。

## 執行迴圈

```bash
# 使用預設 runner（通常是 claude）執行
4x run F001

# 指定 runner
4x run F001 --runner claude

# 限制迭代次數
4x run F001 --max-rounds 3

# 設定逾時（秒）
4x run F001 --timeout 7200

# 預覽 prompt 但不呼叫 LLM
4x run F001 --dry-run
```

Feature ID 支援前綴匹配 — `4x run F001` 和 `4x run f001` 都可以。

迴圈執行：**Design → Code → Review → Test → Accept → Pending Review**。如果 Review 發現問題，Code 會再跑一輪。如果 Test 失敗，迴圈會反覆執行（最多 `--max-rounds` 次）。

## 檢查狀態

```bash
# 所有 feature
4x status

# 單一 feature 含詳細資訊
4x status F001

# 篩選 pending review
4x status --pending
```

## 完成 Feature

迴圈結束後，feature 會停在 `pending-review` 狀態 — 等待人類簽核。

```bash
# 審查產出
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# 標記為完成
4x done F001
```

## 版本控制

`4x init` 會建立 `.4x/.gitignore` 排除 runtime artifacts。其餘檔案應 commit：

| 路徑 | 追蹤 | 原因 |
|---|---|---|
| `.4x/settings.json` | **是** | 專案設定 — 團隊共享 |
| `.4x/features/*.yaml` | **是** | Feature 定義 |
| `.4x/learnings.json` | **是** | 跨 feature 的 retro 知識庫 |
| `.4x/candidates.json` | **是** | 自動發現的 feature 候選池 |
| `.4x/plugins/` | **是** | Runner 指令檔 |
| `.4x/run/` | **否** | Runtime artifacts（state、logs、reports）— 由 `.gitignore` 自動排除 |

對於在此功能之前 init 的既有專案，手動加 gitignore：

```bash
printf 'run/\ngate-input.json\ngate-verdicts.json\nevolve-state.json\n' > .4x/.gitignore
```

## 升級 Plugin 檔

當你更新 `4x` binary 後，重新部署內嵌的 plugin：

```bash
4x sync            # 部署新檔案
4x sync --dry-run  # 僅預覽變更
```

## 下一步

- [CLI 參考](cli.md) — 所有命令與旗標
- [核心概念](concepts.md) — 了解角色、狀態機、協定
- [設定](configuration.md) — 自訂模型、runner、語系
