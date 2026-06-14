# 4x clean — Feature Artifact 清理

## 現狀

`.4x/{feature-id}/` 目錄會隨著 feature 開發累積大量 artifacts（logs、stream.jsonl、rounds、reports）。
feature 完成後這些檔案僅供 debug 回顧，但會持續佔用磁碟空間（實測 55M+）。
目前沒有清理機制，使用者只能手動 rm。

## 需求

### CLI：`4x clean`

清理已完成（`done` / `abandoned`）feature 的 `.4x/{feature-id}/` workspace 目錄。

**刪除的內容**：
- `logs/`（含 `*.stream.jsonl`，佔空間最大宗）
- `rounds/`
- `state.json`、`events.jsonl`
- 各種 report（`final-report.md`、`coder-report.md`、`review-report.md` 等）
- 整個 `.4x/{feature-id}/` 目錄

**永遠不刪**：
- `.4x/features/*.yaml`（feature 定義）
- `.4x/settings.json`
- `.4x/plugins/`
- `.4x/e2e/`
- 非 done/abandoned 的 feature workspace

**介面**：
```
4x clean              # 列出可清理的 feature + 預估空間，確認後清除
4x clean --dry-run    # 只列出，不動作
4x clean --force      # 不問確認直接清
4x clean <featureId>  # 只清指定 feature（仍須 done/abandoned）
```

**輸出**：
```
⚠ Warning: Cleaned features will lose detailed logs, reports, and round
  history in the dashboard. Feature definitions and status are preserved.

Found 12 cleanable features (done/abandoned):
  F001-state-tests          124K
  F020-server-write-api     184K
  F064-adaptive-pipeline    3.9M
  Total: 38M

Clean all? [y/N]
```

清完後：
```
Cleaned 12 features, freed 38M
```

### Dashboard UI 調整

**佈局變更**（順帶修正既有 UI）：

1. **Main content dashboard header 的齒輪 icon** → 移除（Project Settings 已在 sidebar 有入口，重複）
2. **Global Settings icon** → 移到 tab bar 最右側，放在 `+`（Add Project）按鈕旁邊
3. **`+`（Add Project）按鈕** → icon 放大（從 text-sm 改為 text-lg 或 svg icon）
4. **Sidebar 的齒輪** → 維持開啟 Project Settings（有 active project 時）或 Global Settings（無時）

**Clean 按鈕**：

- 在 sidebar header 的 Project Settings 齒輪旁邊加一個 **Clean** 按鈕（垃圾桶 icon）
- 點擊後顯示確認 dialog，含警告文字
- 確認後呼叫 `POST /api/clean`，一次清理整個 project 所有可清理的 feature

**API endpoint**：
```
POST /api/clean
Response: { "cleaned": 12, "freed": 39845888, "freed_human": "38M", "features": ["F001-state-tests", ...] }
```

無可清理的 feature 時：
```
Response: { "cleaned": 0, "freed": 0, "freed_human": "0B", "features": [] }
```

## 安全性

- 檢查 `State.Active` / `State.Pid`——正在跑的 feature 絕不清
- 只認 `done` 和 `abandoned` 兩個 terminal phase
- `blocked` / `needs-attention` 不自動清（可能還需要 debug 資訊）
- Feature 沒有 state.json（從未跑過）→ 跳過

## 警告

CLI 和 Dashboard 都必須明確告知使用者：

> ⚠ Warning: Cleaned features will lose detailed logs, reports, and round history in the dashboard. Feature definitions and status are preserved.

## 實作位置

| 檔案 | 內容 |
|---|---|
| `cmd/4x/clean.go` | Cobra command |
| `internal/protocol/workspace.go` | `CleanFeature(id) (int64, error)` 和 `CleanableFeatures() []CleanCandidate` |
| `internal/server/server.go` | `POST /api/clean` handler |
| `internal/server/static/ui.js` | Clean 按鈕 + 確認 dialog |

## 約束

- 不引入新 package
- 不改既有 feature lifecycle（clean 不是 phase transition）
- 不刪 feature YAML 定義
- 不清 e2e 目錄（那是獨立的測試基建）
