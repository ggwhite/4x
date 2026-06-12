# 快速開始

## 安裝

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

需要 Go 1.26+。驗證安裝：

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

如果 `.4x/` 已經存在，`init` 會報錯結束 — 使用 `4x upgrade` 來更新 plugin 檔。

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

## 升級 Plugin 檔

當你更新 `4x` binary 後，重新部署內嵌的 plugin：

```bash
4x upgrade            # 部署新檔案
4x upgrade --dry-run  # 僅預覽變更
```

## 下一步

- [CLI 參考](cli.md) — 所有命令與旗標
- [核心概念](concepts.md) — 了解角色、狀態機、協定
- [設定](configuration.md) — 自訂模型、runner、語系
