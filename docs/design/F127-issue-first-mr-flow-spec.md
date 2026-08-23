# F127 — Issue-first MR flow（GitHub/GitLab integration）

## 問題

`4x done` 完成時（`internal/gitops/finalize.go` 的 `MergeAndFinalize`，CLI 與 dashboard 共用唯一入口）目前只會在本地對 feature branch 做 `git merge --squash` + commit，然後清掉 worktree 與 local branch。全程沒有 push、沒有呼叫 `gh`/`glab`，也沒有 issue 或 MR/PR 建立邏輯——合併結果完全不進版控平台的 review 流程，無法讓團隊在 GitHub/GitLab 上 code review 或串接 CI gate。

同時 feature 從建立到完成也沒有對應的 issue 追蹤，無法在 GitLab/GitHub issue board 上看到 4x 驅動的工作項目。

這套行為適合 Infinite 這種多 repo、多條長壽命 release branch、且已決定不再投入新工具鏈的舊系統維持現狀，但不適合 Kairos 這種從頭設計、有乾淨分支策略的新專案。

## 目標

1. `.4x/settings.json` 新增 `issue_tracker.enabled` 開關，預設 `false`，只有明確開啟的專案才會改變行為（Infinite 完全不受影響）。
2. `4x new` 在開關開啟時，對 feature 宣告的每個 repo 自動建立 issue，並在 feature YAML 記錄 issue 參照。
3. `4x done` 在開關開啟時，改成 push feature branch + 對每個有實際變更的 repo 開 MR/PR，並在 MR body 帶 `Closes #<issue-id>` 讓平台在合併時自動關閉對應 issue。真正的 code review／合併交給 GitHub/GitLab 處理，4x 的 `done` 狀態代表「已送審」而非「已合併」。
4. 平台（GitHub / GitLab）依 repo 的 git remote hostname 自動判斷，使用者不需要手動設定要用哪個平台。

## 設計

### 資料模型

**`.4x/settings.json`**（`internal/protocol/config.go`）新增欄位：

```go
type IssueTrackerConfig struct {
    Enabled bool `json:"enabled"`
}
```

掛在 `Config` struct 的 `IssueTracker IssueTrackerConfig json:"issue_tracker,omitempty"`。不用 `*bool`／user-project 分層覆寫（如 `Notifications`），因為這個開關只在專案層有意義，零值 `false` 對既有專案完全無感。

**`internal/feature/types.go`** 的 `Feature` struct 新增：

```go
type IssueRef struct {
    Repo string `yaml:"repo" json:"repo"`
    ID   string `yaml:"id" json:"id"`
    URL  string `yaml:"url" json:"url"`
}
```

`Feature.Issues []IssueRef `yaml:"issues,omitempty" json:"issues,omitempty"`` 記錄成功建立的 issue。建立失敗的 repo 不進這個列表，改寫進既有的 `Feature.Warnings []string` 欄位（不新開 partial-failure 專用欄位）。

**MR target branch**：不新增欄位，直接沿用既有 `CaptureBaseline` 在 feature 建立時寫入 `baseline.json` 的 `protocol.BaselineRepo.Branch`（`internal/gitops/monorepo.go:194`／`multirepo.go` 對應函式）——這欄位本來是給 dirty-file baseline 比對用的，但剛好就是「這個 repo 建立 worktree 時 checkout 在哪個分支」，可以直接當 MR 的 target branch 重用，不需要額外追蹤。

**Multi-repo 平台判斷**：`Feature.Repos []string` 存的是 `WorkspaceConfig.Repos`（`.4x/settings.json` 的 `workspace.repos`）map 的 key，實際檔案路徑要透過 `RepoConfig.Path` 解析（`internal/gitops/multirepo.go` 的 `targetRepos()` 已有這個邏輯）。平台偵測直接在這個解析出來的路徑跑 `git remote get-url origin`，不需要在 settings.json 額外加 per-repo 平台欄位。

### `internal/vcshub` package（新增）

封裝「跟程式碼託管平台 API 互動」，跟 `internal/gitops`（純 git plumbing）切開職責：

```go
type Hub interface {
    Preflight(repoPath string) error
    CreateIssue(repoPath, title, body string) (id, url string, err error)
    GetIssue(repoPath, ref string) (id, url string, err error) // ref 可為純數字 ID 或完整 issue URL
    OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error)
}

func New(repoPath string) Hub // 依 git remote hostname 自動判斷回傳 githubHub 或 glabHub
```

`GetIssue` 用於連結既有 issue（見下方「連結既有 issue」）：呼叫 `gh issue view`/`glab issue view` 驗證該 issue 確實存在於這個 repo，回傳規範化的 ID 與 URL；不存在時回傳 error。

- 平台判斷：`git -C <repoPath> remote get-url origin`，hostname 含 `github.com` → `gh` CLI；其餘一律當 GitLab（含自架）→ `glab` CLI。
- `Preflight`：`exec.LookPath("gh"/"glab")` + `gh auth status`/`glab auth status`，用來在 `4x new` 動手之前快速失敗。
- `githubHub`／`glabHub` 兩個小 struct 各自組 `gh issue create`/`gh pr create` 或 `glab issue create`/`glab mr create` 的參數與輸出解析，實作同一個 `Hub` interface。

### `4x new` 流程（`cmd/4x/new.go`）

```
1. cfg.IssueTracker.Enabled 為 true 時：
   對 opts.Repos 解析出的每個 repo 路徑呼叫 vcshub.New(path).Preflight()
   任一失敗 → 直接回錯，不寫入 feature YAML（未執行 feature.Create）

2. feature.Create(ws, opts)  // 不變，寫 feature YAML

3. cfg.IssueTracker.Enabled 為 true 時，對每個 repo：
   若該 repo 有對應的 --issue 參照 → vcshub.New(path).GetIssue(path, ref) 驗證並取得規範化 id/url
   否則                          → vcshub.New(path).CreateIssue(path, f.Name, f.Description)
   成功 → 塞進 f.Issues；失敗 → 塞進 f.Warnings（partial-tolerant，不中止，GetIssue 驗證失敗也算在這裡）
   把補齊的 f.Issues / f.Warnings 寫回 YAML

4. 終端輸出列出每個成功建立/連結的 issue URL
```

Preflight 是唯一會擋下整個 `4x new` 的環節（CLI 未裝／未登入代表整個功能都不可用，寧可讓使用者先修好環境再重跑）；單一 repo 的建立/連結失敗（權限不足、`--issue` 打錯編號等只在實際呼叫時才會發現的錯誤）走 partial-tolerant，不阻擋 feature 建立。

### 連結既有 issue（`--issue` flag）

不是每個 feature 都該建新 issue——常見情境是已經有人在 GitLab/GitHub 回報的 bug 或 product ticket，`4x new` 應該連結上去而非重複建立。

`4x new` 新增 `--issue` flag，格式比照既有 `--subtask "id:name"` 的 repeatable 風格：

```
4x new "Fix login redirect" --repo old-game-server --issue "old-game-server:456"
4x new "Fix login redirect" --issue 456   # 單 repo（monorepo 或 feature 只宣告一個 repo）時可省略 repo 前綴
```

值可以是純數字 ID 或完整 issue URL（`GetIssue` 內部統一解析）。同一個 feature 可以混用：部分 repo 用 `--issue` 連結既有 issue，其他 repo 沒指定的照常自動建立新 issue。

### `4x done` 流程（`internal/gitops`）

`Ops` interface 新增一個方法：

```go
type Ops interface {
    // ...既有方法不變
    PushAndOpenMR(featureID, featureName string) MergeResult
}
```

`internal/gitops/finalize.go` 的 `MergeAndFinalize`（CLI `done.go`／`force_done.go`／batch／server 共用的唯一入口）改成條件分支：

```go
ops := New(root, ws, cfg)
var result MergeResult
if cfg.IssueTracker.Enabled {
    result = ops.PushAndOpenMR(featureID, featureName)
} else {
    result = ops.Merge(featureID, featureName)
}
```

後續「重讀 state 確認未被改動 → `state.FinalizeDone` → `commitSelfManaged`（F190 起取代原本的 `learning` 套件版本）」完全不變，`done` 狀態的語意變成「已開 MR」，不等待遠端實際合併。

`PushAndOpenMR`（monoRepo／multiRepo 各自實作）內部：

1. `git -C <root> push origin <branch>`（worktree 與主 repo 共用同一份 object database，push 不需要切進 worktree 目錄）。
2. 用既有的 `DetectChangedRepos(featureID)` 找出真的有變更的 repo；全部零變更時視同今天的 `MergeResult{Skipped: true}`，不開任何 MR，直接讓上層 finalize 為 done。
3. 對每個有變更的 repo：讀 `baseline.json` 對應的 `Branch` 當 target branch，讀 `Feature.Issues` 對應的 issue ID 組出 body（`Closes #<id>\n\n<feature description>`），呼叫 `vcshub.New(repoPath).OpenMR(...)`。`OpenMR` 內部處理 idempotency：`gh pr create`/`glab mr create` 對已有開啟中 MR 的 branch 重複呼叫時，CLI 本身會回傳「already exists」訊息並非 0 exit code，`OpenMR` 偵測到這個特定訊息時改解析並回傳既有 MR 的 URL 而非視為錯誤——這樣 `amending` 迴圈重跑（push 同一個 branch 多次）自然是 idempotent，不需要在 `Hub` interface 額外加「查詢既有 MR」的方法。
4. 單一 repo 開 MR 失敗記入警告，其他 repo 照常，不阻擋整體 `done`（partial-tolerant，與建 issue 對稱）。
5. 成功的 MR URL 收進 `MergeResult` 新欄位 `MRUrls map[string]string`，供 `done.go` 印出。
6. 沿用既有 `Cleanup()`：移除 worktree、刪本地 branch；remote branch 因為已 push 過所以會留著，MR 照常可見（本地分支被刪不影響遠端 MR）。

`4x force-done`、`4x batch run`（含 `--no-auto-merge` 停在 `pending-review` 的既有語意）都經過同一個 `MergeAndFinalize`，不需要各自特例處理。

## 影響範圍

| 檔案 | 變更 |
|---|---|
| `internal/protocol/config.go` | 新增 `IssueTrackerConfig`、`Config.IssueTracker` 欄位 |
| `internal/feature/types.go` | 新增 `IssueRef`、`Feature.Issues` 欄位 |
| `internal/vcshub/vcshub.go`（新增） | `Hub` interface、`New()` 平台偵測 |
| `internal/vcshub/github.go`（新增） | `githubHub`，包 `gh` CLI |
| `internal/vcshub/gitlab.go`（新增） | `glabHub`，包 `glab` CLI |
| `cmd/4x/new.go` | preflight + 建 issue／`--issue` 連結既有 issue 流程串接 |
| `internal/gitops/gitops.go` | `Ops` interface 新增 `PushAndOpenMR`；`MergeResult` 新增 `MRUrls` |
| `internal/gitops/monorepo.go` | `PushAndOpenMR` 實作 |
| `internal/gitops/multirepo.go` | `PushAndOpenMR` 實作（逐 repo 迴圈） |
| `internal/gitops/finalize.go` | `MergeAndFinalize` 依 `cfg.IssueTracker.Enabled` 分支 |
| `cmd/4x/done.go` | 印出 `MRUrls`（若有） |

## 不做的事

- 不支援 GitHub/GitLab 以外的平台（Bitbucket 等）
- 不做「等待遠端 MR 實際被合併才轉 `done`」的狀態機——`done` = 已開 MR，不新增輪詢／webhook
- 不做 issue 自動關閉：feature 被 abandon 時 issue 留著給人工處理
- 不追溯既有 feature：只有新建立的 feature 才會有 `Issues`；`issue_tracker.enabled` 事後才開啟的專案，舊 feature 跑 `4x done` 時 MR body 就不含 `Closes #`
- 不在 `AutoDiscoverFeatures`（deep review 自動發現的 candidate feature）流程觸發建 issue，只有明確的 `4x new` 才會
- 不做 per-repo 平台手動設定欄位，完全靠 git remote 自動偵測
